package nodeclass

import (
	"context"
	"fmt"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/utils"
)

const (
	resourceCacheTTL = 1 * time.Minute
)

// getCachedVpcByName returns the VPC with the given name. Returns nil when no
// VPC matches.
func (r *ExoscaleNodeClassReconciler) getCachedVpcByName(ctx context.Context, name string) (*egov3.ListVpcEntry, error) {
	vpcList, err := r.vpcCache.GetFiltered(ctx, resourceCacheTTL, "vpcs", func(ctx context.Context) ([]egov3.ListVpcEntry, error) {
		vpcs, err := r.ExoscaleClient.ListVpcs(ctx)
		if err != nil {
			return nil, err
		}
		return vpcs.Vpcs, nil
	}, func(vpc egov3.ListVpcEntry) bool {
		return vpc.Name == name
	})
	if err != nil {
		return nil, err
	}

	if len(vpcList) == 0 {
		return nil, nil
	}

	return &vpcList[0], nil
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

// listCachedSubnets returns all subnets of the given VPC, using a per-VPC cache.
func (r *ExoscaleNodeClassReconciler) listCachedSubnets(ctx context.Context, vpcID egov3.UUID) ([]egov3.ListSubnetEntry, error) {
	return r.subnetCacheFor(vpcID.String()).Get(ctx, resourceCacheTTL, "subnets of vpc "+vpcID.String(), func(ctx context.Context) ([]egov3.ListSubnetEntry, error) {
		subnets, err := r.ExoscaleClient.ListSubnets(ctx, vpcID)
		if err != nil {
			return nil, err
		}
		return subnets.Subnets, nil
	})
}

// getCachedSubnetByName returns the subnet with the given name within the given
// VPC. Subnet names are unique within a VPC; an error is returned when several
// subnets share the name. Returns nil when no subnet matches.
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

func (r *ExoscaleNodeClassReconciler) getCachedElasticIPByAddress(ctx context.Context, address string) (*egov3.ElasticIP, error) {
	eipList, err := r.eipCache.GetFiltered(ctx, resourceCacheTTL, "elastic IPs", func(ctx context.Context) ([]egov3.ElasticIP, error) {
		eips, err := r.ExoscaleClient.ListElasticIPS(ctx)
		if err != nil {
			return nil, err
		}
		return eips.ElasticIPS, nil
	}, func(eip egov3.ElasticIP) bool {
		return eip.IP == address
	})
	if err != nil {
		return nil, err
	}

	if len(eipList) == 0 {
		return nil, nil
	}

	return &eipList[0], nil
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
