package instance

import (
	"context"

	egov3 "github.com/exoscale/egoscale/v3"
)

// EgoscaleClient is an interface that matches the methods we use from egoscale.Client
// This allows us to mock the client for testing
type EgoscaleClient interface {
	CreateInstance(ctx context.Context, req egov3.CreateInstanceRequest) (*egov3.Operation, error)
	DeleteInstance(ctx context.Context, id egov3.UUID) (*egov3.Operation, error)
	GetInstance(ctx context.Context, id egov3.UUID) (*egov3.Instance, error)
	ListInstances(ctx context.Context, opts ...egov3.ListInstancesOpt) (*egov3.ListInstancesResponse, error)
	AttachInstanceToPrivateNetwork(ctx context.Context, instanceID egov3.UUID, req egov3.AttachInstanceToPrivateNetworkRequest) (*egov3.Operation, error)
	UpdateInstance(ctx context.Context, instanceID egov3.UUID, req egov3.UpdateInstanceRequest) (*egov3.Operation, error)
	Wait(ctx context.Context, op *egov3.Operation, states ...egov3.OperationState) (*egov3.Operation, error)
	GetTemplate(ctx context.Context, id egov3.UUID) (*egov3.Template, error)
	GetSecurityGroup(ctx context.Context, id egov3.UUID) (*egov3.SecurityGroup, error)
	GetPrivateNetwork(ctx context.Context, id egov3.UUID) (*egov3.PrivateNetwork, error)
	GetAntiAffinityGroup(ctx context.Context, id egov3.UUID) (*egov3.AntiAffinityGroup, error)
	RebootInstance(ctx context.Context, id egov3.UUID) (*egov3.Operation, error)
}
