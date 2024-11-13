package cloudprovider

import (
	"context"
	"fmt"

	egov3 "github.com/exoscale/egoscale/v3"
)

func (c *CloudProvider) getInstanceFromExoscaleNodePool(ctx context.Context, nodePool *egov3.SKSNodepool) (*egov3.Instance, error) {
	instancePool, err := c.exoClient.GetInstancePool(ctx, nodePool.InstancePool.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance pool %s for nodePool %s: %w", nodePool.InstancePool.ID, nodePool.ID, err)
	}

	if len(instancePool.Instances) == 0 {
		return nil, fmt.Errorf("no instances found in instance pool %s for nodePool %s", nodePool.InstancePool.ID, nodePool.ID)
	}

	return &instancePool.Instances[0], nil
}
