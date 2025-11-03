package userdata

import (
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	v1 "k8s.io/api/core/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

type Options struct {
	ClusterEndpoint             string
	ClusterDNS                  string
	ClusterDomain               string
	BootstrapToken              string
	CABundle                    []byte
	Taints                      []v1.Taint
	Labels                      map[string]string
	KubeReserved                apiv1.ResourceReservation
	SystemReserved              apiv1.ResourceReservation
	ImageGCHighThresholdPercent *int32
	ImageGCLowThresholdPercent  *int32
	ImageMinimumGCAge           string
}

func NewOptions(
	nodeClass *apiv1.ExoscaleNodeClass,
	nodeClaim *karpenterv1.NodeClaim,
) *Options {
	return &Options{
		Labels: nodeClaim.Labels,
		KubeReserved: apiv1.ResourceReservation{
			CPU:              nodeClass.Spec.KubeReserved.CPU,
			Memory:           nodeClass.Spec.KubeReserved.Memory,
			EphemeralStorage: nodeClass.Spec.KubeReserved.EphemeralStorage,
		},
		SystemReserved: apiv1.ResourceReservation{
			CPU:              nodeClass.Spec.SystemReserved.CPU,
			Memory:           nodeClass.Spec.SystemReserved.Memory,
			EphemeralStorage: nodeClass.Spec.SystemReserved.EphemeralStorage,
		},
	}
}
