package mocks

import (
	"context"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/stretchr/testify/mock"
)

type MockTemplateResolver struct {
	mock.Mock
}

func (m *MockTemplateResolver) ResolveTemplateID(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) (string, error) {
	args := m.Called(ctx, nodeClass)
	return args.String(0), args.Error(1)
}
