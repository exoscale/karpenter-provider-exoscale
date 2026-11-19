package nodeclass

import (
	"context"
	"fmt"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
)

const (
	resourceCacheTTL = 1 * time.Minute
)

// vpcSubnet pairs a subnet with the VPC it belongs to. The Exoscale API
// addresses subnets as /vpc/{vpcID}/subnet/{subnetID}, so resolving a subnet
// by ID or name requires knowing its parent VPC. The cache therefore stores a
// flattened list of all subnets across all VPCs.
type vpcSubnet struct {
	VpcID  egov3.UUID
	Subnet egov3.ListSubnetEntry
}

// listAllVpcSubnets fetches every subnet of every VPC and returns them flattened.
func (r *ExoscaleNodeClassReconciler) listAllVpcSubnets(ctx context.Context) ([]vpcSubnet, error) {
	vpcs, err := r.ExoscaleClient.ListVpcs(ctx)
	if err != nil {
		return nil, err
	}

	var all []vpcSubnet
	for _, vpc := range vpcs.Vpcs {
		subnets, err := r.ExoscaleClient.ListSubnets(ctx, vpc.ID)
		if err != nil {
			return nil, err
		}
		for _, subnet := range subnets.Subnets {
			all = append(all, vpcSubnet{VpcID: vpc.ID, Subnet: subnet})
		}
	}
	return all, nil
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

// getCachedVpcSubnetByID returns the subnet with the given ID, along with the
// ID of the VPC it belongs to. Subnet IDs are UUIDs and therefore globally
// unique, so a single match is expected. Returns nil when no subnet matches.
func (r *ExoscaleNodeClassReconciler) getCachedVpcSubnetByID(ctx context.Context, id string) (*vpcSubnet, error) {
	subnetList, err := r.subnetCache.GetFiltered(ctx, resourceCacheTTL, "vpc subnets", r.listAllVpcSubnets, func(s vpcSubnet) bool {
		return s.Subnet.ID.String() == id
	})
	if err != nil {
		return nil, err
	}

	if len(subnetList) == 0 {
		return nil, nil
	}

	return &subnetList[0], nil
}

// getCachedVpcSubnetByName returns the subnet with the given name. Subnet
// names are only unique within a VPC, so an error is returned when the name
// matches subnets in several VPCs (use the subnet ID selector in that case).
func (r *ExoscaleNodeClassReconciler) getCachedVpcSubnetByName(ctx context.Context, name string) (*vpcSubnet, error) {
	subnetList, err := r.subnetCache.GetFiltered(ctx, resourceCacheTTL, "vpc subnets", r.listAllVpcSubnets, func(s vpcSubnet) bool {
		return s.Subnet.Name == name
	})
	if err != nil {
		return nil, err
	}

	if len(subnetList) == 0 {
		return nil, nil
	}

	if len(subnetList) > 1 {
		vpcIDs := make([]string, 0, len(subnetList))
		for _, s := range subnetList {
			vpcIDs = append(vpcIDs, s.VpcID.String())
		}
		return nil, fmt.Errorf("vpc subnet name %q is ambiguous, found in multiple VPCs %v: use the subnet ID instead", name, vpcIDs)
	}

	return &subnetList[0], nil
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
