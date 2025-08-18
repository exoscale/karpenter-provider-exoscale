package mocks

import (
	"context"

	"github.com/exoscale/karpenter-exoscale/pkg/providers/instancetype"
	"github.com/stretchr/testify/mock"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
)

type MockInstanceTypeProvider struct {
	mock.Mock
}

func (m *MockInstanceTypeProvider) Get(ctx context.Context, name string) (*cloudprovider.InstanceType, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*cloudprovider.InstanceType), args.Error(1)
}

func (m *MockInstanceTypeProvider) List(ctx context.Context, filters *instancetype.Filters) ([]*cloudprovider.InstanceType, error) {
	args := m.Called(ctx, filters)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*cloudprovider.InstanceType), args.Error(1)
}

func (m *MockInstanceTypeProvider) Refresh(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockInstanceTypeProvider) GetInstanceTypeID(name string) (string, bool) {
	args := m.Called(name)
	return args.String(0), args.Bool(1)
}
