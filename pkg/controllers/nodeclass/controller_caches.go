package nodeclass

import (
	"context"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
)

const (
	resourceCacheTTL = 1 * time.Minute
)

func (r *ExoscaleNodeClassReconciler) getCachedSecurityGroupByName(ctx context.Context, name string) (*egov3.SecurityGroup, error) {
	sgList, err := r.sgCache.GetFiltered(ctx, resourceCacheTTL, "security groups", func(ctx context.Context) ([]egov3.SecurityGroup, error) {
		sgs, err := r.ExoscaleClient.ListSecurityGroups(ctx)
		if err != nil {
			return nil, err
		}
		return sgs.SecurityGroups, nil
	}, func(sg egov3.SecurityGroup) bool {
		return sg.Name == name
	})
	if err != nil {
		return nil, err
	}

	if len(sgList) == 0 {
		return nil, nil
	}

	return &sgList[0], nil
}

func (r *ExoscaleNodeClassReconciler) getCachedAntiAffinityGroupByName(ctx context.Context, name string) (*egov3.AntiAffinityGroup, error) {
	aagList, err := r.aagCache.GetFiltered(ctx, resourceCacheTTL, "anti-affinity groups", func(ctx context.Context) ([]egov3.AntiAffinityGroup, error) {
		aags, err := r.ExoscaleClient.ListAntiAffinityGroups(ctx)
		if err != nil {
			return nil, err
		}
		return aags.AntiAffinityGroups, nil
	}, func(aag egov3.AntiAffinityGroup) bool {
		return aag.Name == name
	})
	if err != nil {
		return nil, err
	}

	if len(aagList) == 0 {
		return nil, nil
	}

	return &aagList[0], nil
}

func (r *ExoscaleNodeClassReconciler) getCachedPrivateNetworkByName(ctx context.Context, name string) (*egov3.PrivateNetwork, error) {
	netList, err := r.pnCache.GetFiltered(ctx, resourceCacheTTL, "private networks", func(ctx context.Context) ([]egov3.PrivateNetwork, error) {
		nets, err := r.ExoscaleClient.ListPrivateNetworks(ctx)
		if err != nil {
			return nil, err
		}
		return nets.PrivateNetworks, nil
	}, func(net egov3.PrivateNetwork) bool {
		return net.Name == name
	})
	if err != nil {
		return nil, err
	}

	if len(netList) == 0 {
		return nil, nil
	}

	return &netList[0], nil
}
