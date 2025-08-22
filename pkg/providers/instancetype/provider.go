package instancetype

import (
	"context"

	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
)

type Provider interface {
	Get(ctx context.Context, name string) (*cloudprovider.InstanceType, error)
	List(ctx context.Context, filters *Filters) ([]*cloudprovider.InstanceType, error)
	Refresh(ctx context.Context) error
	GetInstanceTypeID(name string) (string, bool)
}

type Filters struct {
	InstanceTypes []string
	MinCPU        *resource.Quantity
	MaxCPU        *resource.Quantity
	MinMemory     *resource.Quantity
	MaxMemory     *resource.Quantity
	MinGPU        *resource.Quantity
	MaxGPU        *resource.Quantity
	Architecture  string
}
