package fixtures

import (
	"github.com/exoscale/karpenter-exoscale/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

type NodePoolBuilder struct {
	name          string
	nodeClassName string
	requirements  []karpenterv1.NodeSelectorRequirementWithMinValues
	taints        []corev1.Taint
	labels        map[string]string
	cpuLimit      string
	memLimit      string
}

func NewNodePoolBuilder(suffix string, nodeClassName string) *NodePoolBuilder {
	return &NodePoolBuilder{
		name:          framework.Suite.ResourceName(suffix),
		nodeClassName: nodeClassName,
		requirements: []karpenterv1.NodeSelectorRequirementWithMinValues{
			{
				NodeSelectorRequirement: corev1.NodeSelectorRequirement{
					Key:      corev1.LabelArchStable,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{"amd64"},
				},
			},
		},
		cpuLimit: "10",
		memLimit: "20Gi",
	}
}

func (b *NodePoolBuilder) WithRequirements(reqs []karpenterv1.NodeSelectorRequirementWithMinValues) *NodePoolBuilder {
	b.requirements = reqs
	return b
}

func (b *NodePoolBuilder) WithInstanceTypes(types ...string) *NodePoolBuilder {
	found := false
	for i, req := range b.requirements {
		if req.Key == corev1.LabelInstanceTypeStable {
			b.requirements[i].Values = types
			found = true
			break
		}
	}
	if !found {
		b.requirements = append(b.requirements, karpenterv1.NodeSelectorRequirementWithMinValues{
			NodeSelectorRequirement: corev1.NodeSelectorRequirement{
				Key:      corev1.LabelInstanceTypeStable,
				Operator: corev1.NodeSelectorOpIn,
				Values:   types,
			},
		})
	}
	return b
}

func (b *NodePoolBuilder) WithTaints(taints []corev1.Taint) *NodePoolBuilder {
	b.taints = taints
	return b
}

func (b *NodePoolBuilder) WithLabels(labels map[string]string) *NodePoolBuilder {
	b.labels = labels
	return b
}

func (b *NodePoolBuilder) WithLimits(cpu, mem string) *NodePoolBuilder {
	b.cpuLimit = cpu
	b.memLimit = mem
	return b
}

func (b *NodePoolBuilder) Build() *karpenterv1.NodePool {
	nodePool := &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name: b.name,
		},
		Spec: karpenterv1.NodePoolSpec{
			Template: karpenterv1.NodeClaimTemplate{
				ObjectMeta: karpenterv1.ObjectMeta{
					Labels: b.labels,
				},
				Spec: karpenterv1.NodeClaimTemplateSpec{
					NodeClassRef: &karpenterv1.NodeClassReference{
						Group: "karpenter.exoscale.com",
						Kind:  "ExoscaleNodeClass",
						Name:  b.nodeClassName,
					},
					Requirements: b.requirements,
					Taints:       b.taints,
				},
			},
			Limits: karpenterv1.Limits{
				corev1.ResourceCPU:    resource.MustParse(b.cpuLimit),
				corev1.ResourceMemory: resource.MustParse(b.memLimit),
			},
		},
	}

	return nodePool
}

func NewNodePool(suffix string, nodeClassName string) *karpenterv1.NodePool {
	return NewNodePoolBuilder(suffix, nodeClassName).Build()
}

func NewNodePoolWithRequirements(suffix string, nodeClassName string, requirements []karpenterv1.NodeSelectorRequirementWithMinValues) *karpenterv1.NodePool {
	return NewNodePoolBuilder(suffix, nodeClassName).
		WithRequirements(requirements).
		Build()
}

func NewNodePoolWithInstanceTypes(suffix string, nodeClassName string, instanceTypes ...string) *karpenterv1.NodePool {
	return NewNodePoolBuilder(suffix, nodeClassName).
		WithInstanceTypes(instanceTypes...).
		Build()
}

func NewGPUNodePool(suffix string, nodeClassName string) *karpenterv1.NodePool {
	return NewNodePoolBuilder(suffix, nodeClassName).
		WithInstanceTypes("standard.medium").
		WithTaints([]corev1.Taint{
			{
				Key:    "gpu",
				Value:  "true",
				Effect: corev1.TaintEffectNoSchedule,
			},
		}).
		WithLabels(map[string]string{
			"team":        "data-science",
			"accelerator": "gpu",
		}).
		Build()
}
