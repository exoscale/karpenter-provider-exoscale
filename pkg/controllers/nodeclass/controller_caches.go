package nodeclass

import (
	"context"
	"fmt"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/utils"
)

const resourceCacheTTL = 1 * time.Minute

// first returns the first element of list, or nil when list is empty.
func first[T any](list []T, err error) (*T, error) {
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return &list[0], nil
}

func (r *ExoscaleNodeClassReconciler) getCachedVpcByName(ctx context.Context, name string) (*egov3.ListVpcEntry, error) {
	return first(r.vpcCache.GetFiltered(ctx, resourceCacheTTL, "vpcs", func(ctx context.Context) ([]egov3.ListVpcEntry, error) {
		vpcs, err := r.ExoscaleClient.ListVpcs(ctx)
		if err != nil {
			return nil, err
		}
		return vpcs.Vpcs, nil
	}, func(vpc egov3.ListVpcEntry) bool {
		return vpc.Name == name
	}))
}

// subnetCacheFor returns the subnet list cache for a given VPC, creating it on
// first use. Subnets are addressed as /vpc/{vpcID}/subnet/{subnetID}, so each
// VPC gets its own cache.
func (r *ExoscaleNodeClassReconciler) subnetCacheFor(vpcID string) *utils.ResourceCache[egov3.ListSubnetEntry] {
	r.subnetCachesMu.Lock()
	defer r.subnetCachesMu.Unlock()
	if r.subnetCaches == nil {
		r.subnetCaches = make(map[string]*utils.ResourceCache[egov3.ListSubnetEntry])
	}
	cache, ok := r.subnetCaches[vpcID]
	if !ok {
		cache = &utils.ResourceCache[egov3.ListSubnetEntry]{}
		r.subnetCaches[vpcID] = cache
	}
	return cache
}

func (r *ExoscaleNodeClassReconciler) listCachedSubnets(ctx context.Context, vpcID egov3.UUID) ([]egov3.ListSubnetEntry, error) {
	return r.subnetCacheFor(vpcID.String()).Get(ctx, resourceCacheTTL, "subnets of vpc "+vpcID.String(), func(ctx context.Context) ([]egov3.ListSubnetEntry, error) {
		subnets, err := r.ExoscaleClient.ListSubnets(ctx, vpcID)
		if err != nil {
			return nil, err
		}
		return subnets.Subnets, nil
	})
}

// getCachedSubnetByName returns nil when no subnet matches.
func (r *ExoscaleNodeClassReconciler) getCachedSubnetByName(ctx context.Context, vpcID egov3.UUID, name string) (*egov3.ListSubnetEntry, error) {
	subnets, err := r.listCachedSubnets(ctx, vpcID)
	if err != nil {
		return nil, err
	}

	var matches []egov3.ListSubnetEntry
	for _, subnet := range subnets {
		if subnet.Name == name {
			matches = append(matches, subnet)
		}
	}

	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("subnet name %q is ambiguous in vpc %s (%d matches): use the subnet ID instead", name, vpcID, len(matches))
	}

	return &matches[0], nil
}

func (r *ExoscaleNodeClassReconciler) getCachedSecurityGroupByName(ctx context.Context, name string) (*egov3.SecurityGroup, error) {
	return first(r.sgCache.GetFiltered(ctx, resourceCacheTTL, "security groups", func(ctx context.Context) ([]egov3.SecurityGroup, error) {
		sgs, err := r.ExoscaleClient.ListSecurityGroups(ctx)
		if err != nil {
			return nil, err
		}
		return sgs.SecurityGroups, nil
	}, func(sg egov3.SecurityGroup) bool {
		return sg.Name == name
	}))
}

func (r *ExoscaleNodeClassReconciler) getCachedAntiAffinityGroupByName(ctx context.Context, name string) (*egov3.AntiAffinityGroup, error) {
	return first(r.aagCache.GetFiltered(ctx, resourceCacheTTL, "anti-affinity groups", func(ctx context.Context) ([]egov3.AntiAffinityGroup, error) {
		aags, err := r.ExoscaleClient.ListAntiAffinityGroups(ctx)
		if err != nil {
			return nil, err
		}
		return aags.AntiAffinityGroups, nil
	}, func(aag egov3.AntiAffinityGroup) bool {
		return aag.Name == name
	}))
}

func (r *ExoscaleNodeClassReconciler) getCachedElasticIPByAddress(ctx context.Context, address string) (*egov3.ElasticIP, error) {
	return first(r.eipCache.GetFiltered(ctx, resourceCacheTTL, "elastic IPs", func(ctx context.Context) ([]egov3.ElasticIP, error) {
		eips, err := r.ExoscaleClient.ListElasticIPS(ctx)
		if err != nil {
			return nil, err
		}
		return eips.ElasticIPS, nil
	}, func(eip egov3.ElasticIP) bool {
		return eip.IP == address
	}))
}

func (r *ExoscaleNodeClassReconciler) getCachedPrivateNetworkByName(ctx context.Context, name string) (*egov3.PrivateNetwork, error) {
	return first(r.pnCache.GetFiltered(ctx, resourceCacheTTL, "private networks", func(ctx context.Context) ([]egov3.PrivateNetwork, error) {
		nets, err := r.ExoscaleClient.ListPrivateNetworks(ctx)
		if err != nil {
			return nil, err
		}
		return nets.PrivateNetworks, nil
	}, func(net egov3.PrivateNetwork) bool {
		return net.Name == name
	}))
}
