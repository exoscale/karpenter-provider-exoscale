package fixtures

import (
	"github.com/exoscale/karpenter-exoscale/test/e2e/framework"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DeploymentBuilder struct {
	name         string
	namespace    string
	replicas     int32
	cpuRequest   string
	memRequest   string
	nodeSelector map[string]string
	tolerations  []corev1.Toleration
}

func NewDeploymentBuilder(suffix string) *DeploymentBuilder {
	return &DeploymentBuilder{
		name:       framework.Suite.ResourceName(suffix),
		namespace:  "default",
		replicas:   1,
		cpuRequest: "100m",
		memRequest: "128Mi",
	}
}

func (b *DeploymentBuilder) WithNamespace(ns string) *DeploymentBuilder {
	b.namespace = ns
	return b
}

func (b *DeploymentBuilder) WithReplicas(replicas int32) *DeploymentBuilder {
	b.replicas = replicas
	return b
}

func (b *DeploymentBuilder) WithCPU(cpu string) *DeploymentBuilder {
	b.cpuRequest = cpu
	return b
}

func (b *DeploymentBuilder) WithMemory(mem string) *DeploymentBuilder {
	b.memRequest = mem
	return b
}

func (b *DeploymentBuilder) WithNodeSelector(selector map[string]string) *DeploymentBuilder {
	b.nodeSelector = selector
	return b
}

func (b *DeploymentBuilder) WithTolerations(tolerations []corev1.Toleration) *DeploymentBuilder {
	b.tolerations = tolerations
	return b
}

func (b *DeploymentBuilder) Build() *appsv1.Deployment {
	labels := map[string]string{
		"app":         b.name,
		"e2e-test-id": framework.Suite.Config.TestRunID,
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      b.name,
			Namespace: b.namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &b.replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					NodeSelector: b.nodeSelector,
					Tolerations:  b.tolerations,
					Containers: []corev1.Container{
						{
							Name:  "pause",
							Image: "registry.k8s.io/pause:3.9",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse(b.cpuRequest),
									corev1.ResourceMemory: resource.MustParse(b.memRequest),
								},
							},
						},
					},
				},
			},
		},
	}
}

type PodBuilder struct {
	name         string
	namespace    string
	cpuRequest   string
	memRequest   string
	nodeSelector map[string]string
	tolerations  []corev1.Toleration
}

func NewPodBuilder(suffix string) *PodBuilder {
	return &PodBuilder{
		name:       framework.Suite.ResourceName(suffix),
		namespace:  "default",
		cpuRequest: "100m",
		memRequest: "128Mi",
	}
}

func (b *PodBuilder) WithNamespace(ns string) *PodBuilder {
	b.namespace = ns
	return b
}

func (b *PodBuilder) WithCPU(cpu string) *PodBuilder {
	b.cpuRequest = cpu
	return b
}

func (b *PodBuilder) WithMemory(mem string) *PodBuilder {
	b.memRequest = mem
	return b
}

func (b *PodBuilder) WithNodeSelector(selector map[string]string) *PodBuilder {
	b.nodeSelector = selector
	return b
}

func (b *PodBuilder) WithTolerations(tolerations []corev1.Toleration) *PodBuilder {
	b.tolerations = tolerations
	return b
}

func (b *PodBuilder) Build() *corev1.Pod {
	labels := map[string]string{
		"app":         b.name,
		"e2e-test-id": framework.Suite.Config.TestRunID,
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      b.name,
			Namespace: b.namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			NodeSelector: b.nodeSelector,
			Tolerations:  b.tolerations,
			Containers: []corev1.Container{
				{
					Name:  "pause",
					Image: "registry.k8s.io/pause:3.9",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(b.cpuRequest),
							corev1.ResourceMemory: resource.MustParse(b.memRequest),
						},
					},
				},
			},
		},
	}
}

func GPUTolerations() []corev1.Toleration {
	return []corev1.Toleration{
		{
			Key:      "gpu",
			Value:    "true",
			Effect:   corev1.TaintEffectNoSchedule,
			Operator: corev1.TolerationOpEqual,
		},
	}
}

func GPUNodeSelector() map[string]string {
	return map[string]string{
		"team": "data-science",
	}
}

func HighResourcePod(suffix string) *corev1.Pod {
	return NewPodBuilder(suffix).
		WithCPU("2").
		WithMemory("4Gi").
		Build()
}
