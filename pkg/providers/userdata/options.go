package userdata

import (
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	v1 "k8s.io/api/core/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

type Options struct {
	ClusterEndpoint             string
	ClusterDomain               string
	BootstrapToken              string
	CABundle                    []byte
	Taints                      []v1.Taint
	Labels                      map[string]string
	ClusterDNS                  []string
	KubeReserved                apiv1.KubeResourceReservation
	SystemReserved              apiv1.SystemResourceReservation
	ImageGCHighThresholdPercent *int32
	ImageGCLowThresholdPercent  *int32
	ImageMinimumGCAge           string
}

func NewOptions(
	nodeClass *apiv1.ExoscaleNodeClass,
	nodeClaim *karpenterv1.NodeClaim,
) *Options {
	return &Options{
		Labels:                      nodeClaim.Labels,
		Taints:                      append([]v1.Taint{}, nodeClaim.Spec.Taints...),
		ClusterDNS:                  nodeClass.Spec.Kubelet.ClusterDNS,
		KubeReserved:                nodeClass.Spec.Kubelet.KubeReserved,
		SystemReserved:              nodeClass.Spec.Kubelet.SystemReserved,
		ImageGCHighThresholdPercent: nodeClass.Spec.Kubelet.ImageGCHighThresholdPercent,
		ImageGCLowThresholdPercent:  nodeClass.Spec.Kubelet.ImageGCLowThresholdPercent,
		ImageMinimumGCAge:           nodeClass.Spec.Kubelet.ImageMinimumGCAge,
	}
}
