package fixtures

import (
	"github.com/exoscale/karpenter-exoscale/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func NewNodeClaim(suffix string, nodeClassName string, requirements []karpenterv1.NodeSelectorRequirementWithMinValues) *karpenterv1.NodeClaim {
	return &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: framework.Suite.ResourceName(suffix),
		},
		Spec: karpenterv1.NodeClaimSpec{
			NodeClassRef: &karpenterv1.NodeClassReference{
				Group: "karpenter.exoscale.com",
				Kind:  "ExoscaleNodeClass",
				Name:  nodeClassName,
			},
			Requirements: requirements,
		},
	}
}

func StandardRequirements() []karpenterv1.NodeSelectorRequirementWithMinValues {
	return []karpenterv1.NodeSelectorRequirementWithMinValues{
		{
			NodeSelectorRequirement: corev1.NodeSelectorRequirement{
				Key:      corev1.LabelInstanceTypeStable,
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{"standard.small"},
			},
		},
		{
			NodeSelectorRequirement: corev1.NodeSelectorRequirement{
				Key:      corev1.LabelArchStable,
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{"amd64"},
			},
		},
	}
}

func InstanceTypeRequirements(instanceTypes ...string) []karpenterv1.NodeSelectorRequirementWithMinValues {
	return []karpenterv1.NodeSelectorRequirementWithMinValues{
		{
			NodeSelectorRequirement: corev1.NodeSelectorRequirement{
				Key:      corev1.LabelInstanceTypeStable,
				Operator: corev1.NodeSelectorOpIn,
				Values:   instanceTypes,
			},
		},
		{
			NodeSelectorRequirement: corev1.NodeSelectorRequirement{
				Key:      corev1.LabelArchStable,
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{"amd64"},
			},
		},
	}
}
