package mocks

import (
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

var (
	DefaultTemplateID      = egov3.UUID("20000000-0000-0000-0000-000000000001")
	DefaultSecurityGroupID = egov3.UUID("30000000-0000-0000-0000-000000000001")

	PrivateNetworkID1 = egov3.UUID("40000000-0000-0000-0000-000000000001")

	DefaultAntiAffinityGroupID = egov3.UUID("50000000-0000-0000-0000-000000000001")

	InstanceID1 = egov3.UUID("00000000-0000-0000-0000-000000000001")
	InstanceID2 = egov3.UUID("00000000-0000-0000-0000-000000000002")
	InstanceID3 = egov3.UUID("00000000-0000-0000-0000-000000000003")

	OperationID1 = egov3.UUID("10000000-0000-0000-0000-000000000001")

	StandardMediumTypeID = egov3.UUID("60000000-0000-0000-0000-000000000002")
)

func CreateNodeClass(name string, scenario string) *apiv1.ExoscaleNodeClass {
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.ExoscaleNodeClassSpec{
			TemplateID: string(DefaultTemplateID),
			DiskSize:   50,
		},
	}

	switch scenario {
	case "standard":
		nc.Spec.SecurityGroups = []string{string(DefaultSecurityGroupID), string(DefaultSecurityGroupID)}
		nc.Spec.PrivateNetworks = []string{string(PrivateNetworkID1)}
		nc.Spec.AntiAffinityGroups = []string{string(DefaultAntiAffinityGroupID)}
		nc.Spec.NodeLabels = map[string]string{
			"node-type": "standard",
			"workload":  "general",
		}
		nc.Spec.KubeReserved = apiv1.ResourceReservation{
			CPU:              "100m",
			Memory:           "512Mi",
			EphemeralStorage: "1Gi",
		}
	case "gpu":
		nc.Spec.SecurityGroups = []string{string(DefaultSecurityGroupID)}
		nc.Spec.PrivateNetworks = []string{string(PrivateNetworkID1)}
		nc.Spec.AntiAffinityGroups = []string{string(DefaultAntiAffinityGroupID)}
		nc.Spec.DiskSize = 100
		nc.Spec.NodeLabels = map[string]string{
			"node-type":      "gpu",
			"workload":       "gpu-compute",
			"nvidia.com/gpu": "true",
		}
		nc.Spec.NodeTaints = []apiv1.NodeTaint{
			{
				Key:    "nvidia.com/gpu",
				Value:  "true",
				Effect: "NoSchedule",
			},
		}
		nc.Spec.KubeReserved = apiv1.ResourceReservation{
			CPU:              "200m",
			Memory:           "1Gi",
			EphemeralStorage: "2Gi",
		}
	default:
		nc.Spec.SecurityGroups = []string{string(DefaultSecurityGroupID)}
	}

	return nc
}

func CreateNodeClaim(name string, nodeClassName string, scenario string) *karpenterv1.NodeClaim {
	nc := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: karpenterv1.NodeClaimSpec{
			NodeClassRef: &karpenterv1.NodeClassReference{
				Group: "karpenter.exoscale.com",
				Kind:  "ExoscaleNodeClass",
				Name:  nodeClassName,
			},
		},
	}

	switch scenario {
	case "small-instance":
		nc.Spec.Requirements = []karpenterv1.NodeSelectorRequirementWithMinValues{
			{
				NodeSelectorRequirement: corev1.NodeSelectorRequirement{
					Key:      corev1.LabelInstanceTypeStable,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{"standard.small"},
				},
			},
			{
				NodeSelectorRequirement: corev1.NodeSelectorRequirement{
					Key:      corev1.LabelOSStable,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{"linux"},
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
	case "large-instance":
		nc.Spec.Requirements = []karpenterv1.NodeSelectorRequirementWithMinValues{
			{
				NodeSelectorRequirement: corev1.NodeSelectorRequirement{
					Key:      corev1.LabelInstanceTypeStable,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{"standard.large", "standard.extra-large"},
				},
			},
		}
		nc.Spec.Resources = karpenterv1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			},
		}
	case "gpu-instance":
		nc.Spec.Requirements = []karpenterv1.NodeSelectorRequirementWithMinValues{
			{
				NodeSelectorRequirement: corev1.NodeSelectorRequirement{
					Key:      corev1.LabelInstanceTypeStable,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{"gpu.small", "gpu.medium"},
				},
			},
			{
				NodeSelectorRequirement: corev1.NodeSelectorRequirement{
					Key:      "karpenter.sh/capacity-type",
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{"on-demand"},
				},
			},
		}
		nc.Spec.Taints = []corev1.Taint{
			{
				Key:    "nvidia.com/gpu",
				Value:  "true",
				Effect: corev1.TaintEffectNoSchedule,
			},
		}
	default:
		nc.Spec.Requirements = []karpenterv1.NodeSelectorRequirementWithMinValues{
			{
				NodeSelectorRequirement: corev1.NodeSelectorRequirement{
					Key:      corev1.LabelInstanceTypeStable,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{"standard.small", "standard.medium"},
				},
			},
		}
	}

	return nc
}

// TestInstance represents a test instance similar to provider instance.Instance
// This avoids circular dependency with the instance package
type TestInstance struct {
	ID                 string
	Name               string
	State              egov3.InstanceState
	InstanceType       *egov3.InstanceType
	Template           *egov3.Template
	Zone               string
	Labels             map[string]string
	CreatedAt          time.Time
	SecurityGroups     []string
	PrivateNetworks    []string
	AntiAffinityGroups []string
}
