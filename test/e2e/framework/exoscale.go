package framework

import (
	"context"
	"fmt"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	"github.com/onsi/ginkgo/v2"
)

type PrivateNetwork struct {
	ID   egov3.UUID
	Name string
}

func (s *E2ESuite) CreatePrivateNetwork(ctx context.Context, name string) (*PrivateNetwork, error) {
	fullName := s.ResourceName(name)

	req := egov3.CreatePrivateNetworkRequest{
		Name: fullName,
	}

	op, err := s.ExoClient.CreatePrivateNetwork(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create private network: %w", err)
	}

	op, err = s.ExoClient.Wait(ctx, op, egov3.OperationStateSuccess)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for private network creation: %w", err)
	}

	if op.Reference == nil {
		return nil, fmt.Errorf("operation reference is nil")
	}

	s.TrackPrivateNetwork(op.Reference.ID.String())

	ginkgo.GinkgoWriter.Printf("Created private network: %s (%s)\n", fullName, op.Reference.ID)

	return &PrivateNetwork{
		ID:   op.Reference.ID,
		Name: fullName,
	}, nil
}

func (s *E2ESuite) DeletePrivateNetwork(ctx context.Context, id string) error {
	op, err := s.ExoClient.DeletePrivateNetwork(ctx, egov3.UUID(id))
	if err != nil {
		return fmt.Errorf("failed to delete private network: %w", err)
	}

	_, err = s.ExoClient.Wait(ctx, op, egov3.OperationStateSuccess)
	if err != nil {
		return fmt.Errorf("failed to wait for private network deletion: %w", err)
	}

	ginkgo.GinkgoWriter.Printf("Deleted private network: %s\n", id)
	return nil
}

func (s *E2ESuite) GetInstanceByNodeClaimName(ctx context.Context, nodeClaimName string) (*egov3.Instance, error) {
	instances, err := s.ExoClient.ListInstances(ctx)
	if err != nil {
		return nil, err
	}

	for _, inst := range instances.Instances {
		if inst.Labels == nil {
			continue
		}
		if inst.Labels[constants.InstanceLabelNodeClaim] == nodeClaimName {
			return s.ExoClient.GetInstance(ctx, inst.ID)
		}
	}

	return nil, nil
}

func (s *E2ESuite) GetInstanceState(ctx context.Context, nodeClaimName string) (string, error) {
	inst, err := s.GetInstanceByNodeClaimName(ctx, nodeClaimName)
	if err != nil {
		return "", err
	}
	if inst == nil {
		return "", nil
	}
	return string(inst.State), nil
}

func (s *E2ESuite) WaitForInstanceRunning(ctx context.Context, nodeClaimName string, timeout time.Duration) (*egov3.Instance, error) {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout waiting for instance to become running")
		}

		inst, err := s.GetInstanceByNodeClaimName(ctx, nodeClaimName)
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		if inst != nil && inst.State == egov3.InstanceStateRunning {
			return inst, nil
		}

		time.Sleep(5 * time.Second)
	}
}

func (s *E2ESuite) DeleteInstance(ctx context.Context, instanceID string) error {
	op, err := s.ExoClient.DeleteInstance(ctx, egov3.UUID(instanceID))
	if err != nil {
		return err
	}
	_, err = s.ExoClient.Wait(ctx, op, egov3.OperationStateSuccess)
	return err
}

func (s *E2ESuite) InstanceExists(ctx context.Context, instanceID string) (bool, error) {
	_, err := s.ExoClient.GetInstance(ctx, egov3.UUID(instanceID))
	if err != nil {
		return false, nil
	}
	return true, nil
}
