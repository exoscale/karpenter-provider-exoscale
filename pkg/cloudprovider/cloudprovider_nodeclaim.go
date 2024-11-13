package cloudprovider

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	egov3 "github.com/exoscale/egoscale/v3"
)

func nodeClaimFromInstance(nodePool *egov3.SKSNodepool, instance *egov3.Instance) *karpenterv1.NodeClaim {
	return &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodePool.Name,
		},
		Spec: karpenterv1.NodeClaimSpec{}, // No need to populate this
		Status: karpenterv1.NodeClaimStatus{
			ProviderID: string(nodePool.ID),
			NodeName:   instance.Name,
			ImageID:    instance.Template.ID.String(),
			Capacity: v1.ResourceList{
				v1.ResourceCPU:    *resource.NewQuantity(int64(instance.InstanceType.Cpus), resource.DecimalSI),
				v1.ResourceMemory: *resource.NewQuantity(int64(instance.InstanceType.Memory), resource.BinarySI),
			},
		},
	}
}
