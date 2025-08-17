package repair

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type PodFilter func(*v1.Pod) bool

func isSystemPod(pod *v1.Pod) bool {
	return pod.Namespace == "kube-system" || pod.Namespace == "kube-node-lease"
}

func isHighPriorityPod(pod *v1.Pod) bool {
	return pod.Spec.Priority != nil && *pod.Spec.Priority >= 1000000000
}

func shouldEvictPod(pod *v1.Pod, filter PodFilter) bool {
	if isSystemPod(pod) {
		return false
	}
	if isHighPriorityPod(pod) {
		return false
	}
	if filter != nil && !filter(pod) {
		return false
	}
	return true
}

func filterPodsForEviction(pods []v1.Pod, filter PodFilter) []v1.Pod {
	var result []v1.Pod
	for _, pod := range pods {
		if shouldEvictPod(&pod, filter) {
			result = append(result, pod)
		}
	}
	return result
}

func createEviction(pod *v1.Pod) *policyv1.Eviction {
	return &policyv1.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
	}
}

func (r *NodeRepairController) evictSinglePod(ctx context.Context, pod *v1.Pod) error {
	eviction := createEviction(pod)
	return r.SubResource("eviction").Create(ctx, pod, eviction)
}

func (r *NodeRepairController) evictPods(ctx context.Context, node *v1.Node, filter PodFilter) (int, error) {
	logger := log.FromContext(ctx).WithValues("node", node.Name)

	podList := &v1.PodList{}
	if err := r.List(ctx, podList, client.MatchingFields{"spec.nodeName": node.Name}); err != nil {
		return 0, fmt.Errorf("failed to list pods: %w", err)
	}

	podsToEvict := filterPodsForEviction(podList.Items, filter)

	evicted := 0
	for _, pod := range podsToEvict {
		if err := r.evictSinglePod(ctx, &pod); err != nil {
			if !errors.IsNotFound(err) {
				logger.V(1).Info("failed to evict pod", "pod", pod.Name, "error", err)
			}
		} else {
			evicted++
		}
	}

	return evicted, nil
}

func (r *NodeRepairController) evictNonCriticalPods(ctx context.Context, node *v1.Node) error {
	evicted, err := r.evictPods(ctx, node, nil)
	if err != nil {
		return err
	}
	log.FromContext(ctx).Info("evicted non-critical pods", "count", evicted)
	return nil
}

func createDiskCleanupFilter() PodFilter {
	return func(pod *v1.Pod) bool {
		return len(pod.Spec.Containers) > 2 || len(pod.Spec.InitContainers) > 1
	}
}

func (r *NodeRepairController) evictPodsForDiskCleanup(ctx context.Context, node *v1.Node) error {
	filter := createDiskCleanupFilter()
	evicted, err := r.evictPods(ctx, node, filter)
	if err != nil {
		return err
	}
	log.FromContext(ctx).Info("evicted pods for disk cleanup", "count", evicted)
	return nil
}

func countPodContainers(pod *v1.Pod) int {
	return len(pod.Spec.Containers) + len(pod.Spec.InitContainers)
}

func createPIDPressureFilter() PodFilter {
	return func(pod *v1.Pod) bool {
		return countPodContainers(pod) > 3
	}
}

func (r *NodeRepairController) evictPodsForPIDPressure(ctx context.Context, node *v1.Node) error {
	filter := createPIDPressureFilter()
	evicted, err := r.evictPods(ctx, node, filter)
	if err != nil {
		return err
	}
	log.FromContext(ctx).Info("evicted pods for PID pressure", "count", evicted)
	return nil
}
