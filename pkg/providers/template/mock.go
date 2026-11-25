package template

import (
	"context"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
)

type MockResolver struct {
	ResolveFunc func(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) (*Template, error)
}

func (m *MockResolver) ResolveTemplate(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) (*Template, error) {
	if m.ResolveFunc != nil {
		return m.ResolveFunc(ctx, nodeClass)
	}
	return nil, nil
}
