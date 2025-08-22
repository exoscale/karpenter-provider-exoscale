package mocks

import (
	"context"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/stretchr/testify/mock"
)

// MockExoscaleClient is a mock implementation of the egoscale v3 Client
// for testing purposes. It implements the same methods as the real client.
type MockExoscaleClient struct {
	mock.Mock
}

func (m *MockExoscaleClient) CreateInstance(ctx context.Context, req egov3.CreateInstanceRequest) (*egov3.Operation, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egov3.Operation), args.Error(1)
}

func (m *MockExoscaleClient) DeleteInstance(ctx context.Context, id egov3.UUID) (*egov3.Operation, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egov3.Operation), args.Error(1)
}

func (m *MockExoscaleClient) GetInstance(ctx context.Context, id egov3.UUID) (*egov3.Instance, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egov3.Instance), args.Error(1)
}

func (m *MockExoscaleClient) ListInstances(ctx context.Context, opts ...egov3.ListInstancesOpt) (*egov3.ListInstancesResponse, error) {
	args := m.Called(ctx, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egov3.ListInstancesResponse), args.Error(1)
}

func (m *MockExoscaleClient) AttachInstanceToPrivateNetwork(ctx context.Context, id egov3.UUID, req egov3.AttachInstanceToPrivateNetworkRequest) (*egov3.Operation, error) {
	args := m.Called(ctx, id, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egov3.Operation), args.Error(1)
}

func (m *MockExoscaleClient) UpdateInstance(ctx context.Context, instanceID egov3.UUID, req egov3.UpdateInstanceRequest) (*egov3.Operation, error) {
	args := m.Called(ctx, instanceID, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egov3.Operation), args.Error(1)
}

func (m *MockExoscaleClient) Wait(ctx context.Context, op *egov3.Operation, states ...egov3.OperationState) (*egov3.Operation, error) {
	args := m.Called(ctx, op, states)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egov3.Operation), args.Error(1)
}

func (m *MockExoscaleClient) GetInstanceType(ctx context.Context, id egov3.UUID) (*egov3.InstanceType, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egov3.InstanceType), args.Error(1)
}

func (m *MockExoscaleClient) ListInstanceTypes(ctx context.Context) (*egov3.ListInstanceTypesResponse, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egov3.ListInstanceTypesResponse), args.Error(1)
}

func (m *MockExoscaleClient) RebootInstance(ctx context.Context, id egov3.UUID) (*egov3.Operation, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egov3.Operation), args.Error(1)
}

func (m *MockExoscaleClient) StartInstance(ctx context.Context, id egov3.UUID, req egov3.StartInstanceRequest) (*egov3.Operation, error) {
	args := m.Called(ctx, id, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egov3.Operation), args.Error(1)
}

func (m *MockExoscaleClient) ResetInstance(ctx context.Context, id egov3.UUID, req egov3.ResetInstanceRequest) (*egov3.Operation, error) {
	args := m.Called(ctx, id, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egov3.Operation), args.Error(1)
}

func (m *MockExoscaleClient) GetTemplate(ctx context.Context, id egov3.UUID) (*egov3.Template, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egov3.Template), args.Error(1)
}

func (m *MockExoscaleClient) GetSecurityGroup(ctx context.Context, id egov3.UUID) (*egov3.SecurityGroup, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egov3.SecurityGroup), args.Error(1)
}

func (m *MockExoscaleClient) GetPrivateNetwork(ctx context.Context, id egov3.UUID) (*egov3.PrivateNetwork, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egov3.PrivateNetwork), args.Error(1)
}

func (m *MockExoscaleClient) GetAntiAffinityGroup(ctx context.Context, id egov3.UUID) (*egov3.AntiAffinityGroup, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egov3.AntiAffinityGroup), args.Error(1)
}
