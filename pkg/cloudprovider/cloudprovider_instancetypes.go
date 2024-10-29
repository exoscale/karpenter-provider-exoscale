package cloudprovider

import (
	"context"
	"fmt"
	"slices"

	exov3 "github.com/exoscale/egoscale/v3"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/scheduling"
)

const (
	ResourceGPUVendorA = "nvidia.com/gpu-vendor-a" // TODO: find the right name
)

type InstanceTypeOptions struct {
	Name               string
	Offerings          cloudprovider.Offerings
	Architecture       string
	OperatingSystems   sets.Set[string]
	Resources          v1.ResourceList
	InstanceTypeLabels map[string]string
}

func MakeInstanceTypeLabels(cpu, mem int) map[string]string {
	return map[string]string{}
}

// InstanceTypesAssorted create many unique instance types with varying CPU/memory/architecture/OS/zone/capacity type.
func ConstructInstanceTypes(exoClient *exov3.Client, zone string, cfgInstanceOverhead cloudprovider.InstanceTypeOverhead) []*cloudprovider.InstanceType {

	exoInstanceTypes, err := exoClient.ListInstanceTypes(context.TODO())
	if err != nil {
		panic(err)
	}

	var instanceTypes []*cloudprovider.InstanceType

	for _, instanceType := range exoInstanceTypes.InstanceTypes {
		// We only propose authorized instance types
		if instanceType.Authorized == nil || !*instanceType.Authorized {
			// TODO log this
			continue
		}

		// And instance present in the zone
		if instanceType.Zones != nil && !slices.Contains(instanceType.Zones, exov3.ZoneName(zone)) {
			// TODO log this ?
			continue
		}

		opts := InstanceTypeOptions{
			Name:             fmt.Sprintf("%s-%s", string(instanceType.Family), string(instanceType.Size)),
			Architecture:     "amd64",
			OperatingSystems: sets.New(string(v1.Linux)),
			Resources: v1.ResourceList{
				v1.ResourceCPU:     resource.MustParse(fmt.Sprintf("%d", instanceType.Cpus)),
				v1.ResourceMemory:  resource.MustParse(fmt.Sprintf("%d", instanceType.Memory)),
				ResourceGPUVendorA: resource.MustParse(fmt.Sprintf("%d", instanceType.Gpus)),
				// TODO, GPU ?
			},
			InstanceTypeLabels: map[string]string{},
		}

		price := PriceFromResources(opts.Resources)

		opts.Offerings = cloudprovider.Offerings{}
		opts.Offerings = append(opts.Offerings, cloudprovider.Offering{
			Requirements: scheduling.NewRequirements(),
			Price:        price,
			Available:    true,
		})

		instanceTypes = append(instanceTypes, newInstanceType(opts, cfgInstanceOverhead))
	}

	return instanceTypes
}

func newInstanceType(options InstanceTypeOptions, cfgInstanceOverhead cloudprovider.InstanceTypeOverhead) *cloudprovider.InstanceType {
	requirements := scheduling.NewRequirements(
		scheduling.NewRequirement(v1.LabelInstanceTypeStable, v1.NodeSelectorOpIn, options.Name),
		scheduling.NewRequirement(v1.LabelArchStable, v1.NodeSelectorOpIn, options.Architecture),
		scheduling.NewRequirement(v1.LabelOSStable, v1.NodeSelectorOpIn, sets.List(options.OperatingSystems)...),
	)

	return &cloudprovider.InstanceType{
		Name:         options.Name,
		Requirements: requirements,
		Offerings:    options.Offerings,
		Capacity:     options.Resources,
		Overhead:     &cfgInstanceOverhead,
	}
}

// It's totally faked and not the real prices, we should expose prices from the API
func PriceFromResources(resources v1.ResourceList) float64 {
	price := 0.0
	for k, v := range resources {
		switch k {
		case v1.ResourceCPU:
			price += 0.025 * v.AsApproximateFloat64()
		case v1.ResourceMemory:
			price += 0.001 * v.AsApproximateFloat64() / (1e9)
		case ResourceGPUVendorA:
			price += 1.0
		}
	}
	return price
}
