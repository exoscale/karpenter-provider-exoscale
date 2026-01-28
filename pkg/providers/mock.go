package providers

import (
	"context"

	egov3 "github.com/exoscale/egoscale/v3"
)

// MockClient is a mock implementation of the ExoscaleClient interface for testing
type MockClient struct {
	GetTemplateFunc            func(ctx context.Context, id egov3.UUID) (*egov3.Template, error)
	GetSecurityGroupFunc       func(ctx context.Context, id egov3.UUID) (*egov3.SecurityGroup, error)
	GetAntiAffinityGroupFunc   func(ctx context.Context, id egov3.UUID) (*egov3.AntiAffinityGroup, error)
	GetPrivateNetworkFunc      func(ctx context.Context, id egov3.UUID) (*egov3.PrivateNetwork, error)
	ListInstancesFunc          func(ctx context.Context, opts ...egov3.ListInstancesOpt) (*egov3.ListInstancesResponse, error)
	DeleteInstanceFunc         func(ctx context.Context, id egov3.UUID) (*egov3.Operation, error)
	ListSecurityGroupsFunc     func(ctx context.Context, opts ...egov3.ListSecurityGroupsOpt) (*egov3.ListSecurityGroupsResponse, error)
	ListAntiAffinityGroupsFunc func(ctx context.Context) (*egov3.ListAntiAffinityGroupsResponse, error)
	ListPrivateNetworksFunc    func(ctx context.Context) (*egov3.ListPrivateNetworksResponse, error)
}

func (m *MockClient) GetTemplate(ctx context.Context, id egov3.UUID) (*egov3.Template, error) {
	if m.GetTemplateFunc != nil {
		return m.GetTemplateFunc(ctx, id)
	}
	return nil, nil
}

func (m *MockClient) GetSecurityGroup(ctx context.Context, id egov3.UUID) (*egov3.SecurityGroup, error) {
	if m.GetSecurityGroupFunc != nil {
		return m.GetSecurityGroupFunc(ctx, id)
	}
	return nil, nil
}

func (m *MockClient) GetAntiAffinityGroup(ctx context.Context, id egov3.UUID) (*egov3.AntiAffinityGroup, error) {
	if m.GetAntiAffinityGroupFunc != nil {
		return m.GetAntiAffinityGroupFunc(ctx, id)
	}
	return nil, nil
}

func (m *MockClient) GetPrivateNetwork(ctx context.Context, id egov3.UUID) (*egov3.PrivateNetwork, error) {
	if m.GetPrivateNetworkFunc != nil {
		return m.GetPrivateNetworkFunc(ctx, id)
	}
	return nil, nil
}

func (m *MockClient) ListInstances(ctx context.Context, opts ...egov3.ListInstancesOpt) (*egov3.ListInstancesResponse, error) {
	if m.ListInstancesFunc != nil {
		return m.ListInstancesFunc(ctx, opts...)
	}
	return nil, nil
}

func (m *MockClient) DeleteInstance(ctx context.Context, id egov3.UUID) (*egov3.Operation, error) {
	if m.DeleteInstanceFunc != nil {
		return m.DeleteInstanceFunc(ctx, id)
	}
	return nil, nil
}

func (m *MockClient) ListSecurityGroups(ctx context.Context, opts ...egov3.ListSecurityGroupsOpt) (*egov3.ListSecurityGroupsResponse, error) {
	if m.ListSecurityGroupsFunc != nil {
		return m.ListSecurityGroupsFunc(ctx, opts...)
	}
	return nil, nil
}

func (m *MockClient) ListAntiAffinityGroups(ctx context.Context) (*egov3.ListAntiAffinityGroupsResponse, error) {
	if m.ListAntiAffinityGroupsFunc != nil {
		return m.ListAntiAffinityGroupsFunc(ctx)
	}
	return nil, nil
}

func (m *MockClient) ListPrivateNetworks(ctx context.Context) (*egov3.ListPrivateNetworksResponse, error) {
	if m.ListPrivateNetworksFunc != nil {
		return m.ListPrivateNetworksFunc(ctx)
	}
	return nil, nil
}
