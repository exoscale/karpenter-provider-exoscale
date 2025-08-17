package userdata

import (
	"context"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	v1 "k8s.io/api/core/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

type Provider interface {
	Generate(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass, nodeClaim *karpenterv1.NodeClaim, options *Options) (string, error)
}

type Options struct {
	ClusterName                 string
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
	KubeletMaxPods              *int32
}

type CAProvider interface {
	GetClusterCA(ctx context.Context) ([]byte, error)
}
