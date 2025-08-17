package repair

import (
	"context"
	"fmt"
	"time"

	"github.com/exoscale/karpenter-exoscale/pkg/cloudprovider"
	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instance"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	karpentercloudprovider "sigs.k8s.io/karpenter/pkg/cloudprovider"
)

type NodeRepairController struct {
	client.Client
	Scheme         *runtime.Scheme
	CloudProvider  *cloudprovider.CloudProvider
	ExoscaleClient instance.EgoscaleClient
	Recorder       record.EventRecorder
}

// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups="",resources=nodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list
// +kubebuilder:rbac:groups="",resources=pods/eviction,verbs=create
// +kubebuilder:rbac:groups=karpenter.sh,resources=nodeclaims,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=karpenter.sh,resources=nodeclaims/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *NodeRepairController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("node", req.NamespacedName)
	logger.V(1).Info("checking node health")

	node := &v1.Node{}
	if err := r.Get(ctx, req.NamespacedName, node); err != nil {
		if errors.IsNotFound(err) {
			logger.V(1).Info("node not found, ignoring")
			return reconcile.Result{}, nil
		}
		logger.Error(err, "failed to get node")
		return reconcile.Result{}, err
	}

	if !r.isManagedNode(node) {
		logger.V(2).Info("node not managed by Karpenter, skipping")
		return reconcile.Result{}, nil
	}

	nodeClaim, err := r.getNodeClaim(ctx, node)
	if err != nil {
		logger.Error(err, "failed to get NodeClaim")
		return reconcile.Result{}, err
	}
	if nodeClaim == nil {
		logger.V(2).Info("no NodeClaim found for node")
		return reconcile.Result{}, nil
	}

	repairPolicies := r.CloudProvider.RepairPolicies()
	needsRepair, reason := r.evaluateRepairPolicies(node, repairPolicies)

	if !needsRepair {
		logger.V(2).Info("node is healthy")
		if err := r.clearRepairAnnotations(ctx, nodeClaim); err != nil {
			logger.Error(err, "failed to clear repair annotations")
		}
		return reconcile.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	logger.Info("node needs repair", "reason", reason)

	shouldRepair, waitTime := r.shouldAttemptRepair(nodeClaim)
	if !shouldRepair {
		if waitTime > 0 {
			logger.V(2).Info("waiting before next repair attempt", "wait", waitTime)
			return reconcile.Result{RequeueAfter: waitTime}, nil
		}
		logger.Info("maximum repair attempts reached, marking node for termination")
		return r.markNodeForTermination(ctx, node, nodeClaim, reason)
	}

	return r.attemptRepair(ctx, node, nodeClaim, reason)
}

func (r *NodeRepairController) isManagedNode(node *v1.Node) bool {
	if _, ok := node.Labels["karpenter.sh/nodepool"]; ok {
		return true
	}
	if val, ok := node.Labels[constants.LabelManagedBy]; ok && val == constants.ManagedByKarpenter {
		return true
	}
	return false
}

func (r *NodeRepairController) getNodeClaim(ctx context.Context, node *v1.Node) (*karpenterv1.NodeClaim, error) {
	if node.Spec.ProviderID != "" {
		nodeClaimList := &karpenterv1.NodeClaimList{}
		if err := r.List(ctx, nodeClaimList); err != nil {
			return nil, fmt.Errorf("failed to list NodeClaims: %w", err)
		}

		for _, nc := range nodeClaimList.Items {
			if nc.Status.ProviderID == node.Spec.ProviderID {
				return &nc, nil
			}
		}
	}

	nodeClaimList := &karpenterv1.NodeClaimList{}
	if err := r.List(ctx, nodeClaimList); err != nil {
		return nil, fmt.Errorf("failed to list NodeClaims: %w", err)
	}

	for _, nc := range nodeClaimList.Items {
		if nc.Status.NodeName == node.Name {
			return &nc, nil
		}
	}

	return nil, nil
}

func findNodeCondition(conditions []v1.NodeCondition, conditionType v1.NodeConditionType) *v1.NodeCondition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

func checkPolicyViolation(condition *v1.NodeCondition, policy karpentercloudprovider.RepairPolicy) (violated bool, duration time.Duration) {
	if condition == nil || condition.Status != policy.ConditionStatus {
		return false, 0
	}
	duration = time.Since(condition.LastTransitionTime.Time)
	return duration >= policy.TolerationDuration, duration
}

func formatPolicyViolation(policy karpentercloudprovider.RepairPolicy, duration time.Duration) string {
	return fmt.Sprintf("condition %s is %s for %v",
		policy.ConditionType, policy.ConditionStatus, duration)
}

func (r *NodeRepairController) evaluateRepairPolicies(node *v1.Node, policies []karpentercloudprovider.RepairPolicy) (bool, string) {
	for _, policy := range policies {
		condition := findNodeCondition(node.Status.Conditions, policy.ConditionType)
		if violated, duration := checkPolicyViolation(condition, policy); violated {
			return true, formatPolicyViolation(policy, duration)
		}
	}
	return false, ""
}

func parseRepairAttempts(annotations map[string]string) int {
	if annotations == nil {
		return 0
	}
	attemptsStr, ok := annotations[AnnotationRepairAttempts]
	if !ok {
		return 0
	}
	attempts := 0
	if _, err := fmt.Sscanf(attemptsStr, "%d", &attempts); err != nil {
		return 0
	}
	return attempts
}

func parseLastRepairTime(annotations map[string]string) (time.Time, bool) {
	if annotations == nil {
		return time.Time{}, false
	}
	lastRepairStr, ok := annotations[AnnotationLastRepairTime]
	if !ok {
		return time.Time{}, false
	}
	lastRepair, err := time.Parse(time.RFC3339, lastRepairStr)
	return lastRepair, err == nil
}

func calculateRepairEligibility(attempts int, lastRepair time.Time, hasLastRepair bool) (bool, time.Duration) {
	if attempts >= MaxRepairAttempts {
		return false, 0
	}
	if hasLastRepair {
		timeSinceLastRepair := time.Since(lastRepair)
		if timeSinceLastRepair < RepairCooldown {
			return false, RepairCooldown - timeSinceLastRepair
		}
	}
	return true, 0
}

func (r *NodeRepairController) shouldAttemptRepair(nodeClaim *karpenterv1.NodeClaim) (bool, time.Duration) {
	attempts := parseRepairAttempts(nodeClaim.Annotations)
	lastRepair, hasLastRepair := parseLastRepairTime(nodeClaim.Annotations)
	return calculateRepairEligibility(attempts, lastRepair, hasLastRepair)
}

func (r *NodeRepairController) attemptRepair(ctx context.Context, node *v1.Node, nodeClaim *karpenterv1.NodeClaim, reason string) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("node", node.Name, "nodeClaim", nodeClaim.Name)
	logger.Info("attempting node repair", "reason", reason)

	if nodeClaim.Annotations == nil {
		nodeClaim.Annotations = make(map[string]string)
	}

	attempts := 0
	if attemptsStr, ok := nodeClaim.Annotations[AnnotationRepairAttempts]; ok {
		if _, err := fmt.Sscanf(attemptsStr, "%d", &attempts); err != nil {
			attempts = 0
		}
	}
	attempts++

	nodeClaim.Annotations[AnnotationRepairAttempts] = fmt.Sprintf("%d", attempts)
	nodeClaim.Annotations[AnnotationLastRepairTime] = time.Now().Format(time.RFC3339)

	if err := r.Update(ctx, nodeClaim); err != nil {
		logger.Error(err, "failed to update NodeClaim annotations")
		return reconcile.Result{}, err
	}

	r.Recorder.Eventf(node, "Normal", "NodeRepairStarted",
		"Starting repair attempt %d/%d for reason: %s", attempts, MaxRepairAttempts, reason)

	if err := r.performRepairActions(ctx, node, nodeClaim, reason); err != nil {
		logger.Error(err, "repair actions failed")
		r.Recorder.Eventf(node, "Warning", "NodeRepairFailed",
			"Repair attempt %d/%d failed: %v", attempts, MaxRepairAttempts, err)
		return reconcile.Result{RequeueAfter: RepairCooldown}, err
	}

	r.Recorder.Eventf(node, "Normal", "NodeRepairCompleted",
		"Repair attempt %d/%d completed", attempts, MaxRepairAttempts)

	return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
}

func createUnschedulableTaint() v1.Taint {
	return v1.Taint{
		Key:       "node.kubernetes.io/unschedulable",
		Effect:    v1.TaintEffectNoSchedule,
		TimeAdded: &metav1.Time{Time: time.Now()},
	}
}

func hasTaint(taints []v1.Taint, key string) bool {
	for _, t := range taints {
		if t.Key == key {
			return true
		}
	}
	return false
}

func addTerminationAnnotations(annotations map[string]string, reason string) map[string]string {
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations["karpenter.sh/do-not-consolidate"] = "true"
	annotations["exoscale.com/termination-reason"] = reason
	return annotations
}

func (r *NodeRepairController) markNodeForTermination(ctx context.Context, node *v1.Node, nodeClaim *karpenterv1.NodeClaim, reason string) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("node", node.Name, "nodeClaim", nodeClaim.Name)
	logger.Info("marking node for termination", "reason", reason)

	taint := createUnschedulableTaint()

	if !hasTaint(node.Spec.Taints, taint.Key) {
		node.Spec.Taints = append(node.Spec.Taints, taint)
		if err := r.Update(ctx, node); err != nil {
			logger.Error(err, "failed to taint node")
			return reconcile.Result{}, err
		}
	}

	nodeClaim.Annotations = addTerminationAnnotations(nodeClaim.Annotations, reason)

	if err := r.Update(ctx, nodeClaim); err != nil {
		logger.Error(err, "failed to update NodeClaim")
		return reconcile.Result{}, err
	}

	r.Recorder.Eventf(node, "Warning", "NodeRepairExhausted",
		"All repair attempts exhausted, node marked for termination: %s", reason)
	r.Recorder.Eventf(nodeClaim, "Warning", "NodeClaimTerminationRequested",
		"NodeClaim marked for termination due to: %s", reason)

	return reconcile.Result{}, nil
}

func (r *NodeRepairController) clearRepairAnnotations(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) error {
	if nodeClaim.Annotations == nil {
		return nil
	}

	changed := false
	if _, ok := nodeClaim.Annotations[AnnotationRepairAttempts]; ok {
		delete(nodeClaim.Annotations, AnnotationRepairAttempts)
		changed = true
	}
	if _, ok := nodeClaim.Annotations[AnnotationLastRepairTime]; ok {
		delete(nodeClaim.Annotations, AnnotationLastRepairTime)
		changed = true
	}

	if changed {
		return r.Update(ctx, nodeClaim)
	}

	return nil
}

func (r *NodeRepairController) SetupWithManager(mgr ctrl.Manager) error {
	nodePredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return r.isManagedNode(e.Object.(*v1.Node))
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return r.isManagedNode(e.ObjectNew.(*v1.Node))
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Node{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 10,
		}).
		WithEventFilter(nodePredicate).
		Watches(
			&karpenterv1.NodeClaim{},
			handler.EnqueueRequestsFromMapFunc(r.nodeClaimToNode),
		).
		Complete(r)
}

func (r *NodeRepairController) nodeClaimToNode(_ context.Context, obj client.Object) []reconcile.Request {
	nodeClaim, ok := obj.(*karpenterv1.NodeClaim)
	if !ok || nodeClaim.Status.NodeName == "" {
		return []reconcile.Request{}
	}

	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Name: nodeClaim.Status.NodeName,
			},
		},
	}
}
