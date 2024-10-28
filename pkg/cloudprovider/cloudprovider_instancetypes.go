package cloudprovider

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/scheduling"
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
func ConstructInstanceTypes(cfgInstanceOverhead cloudprovider.InstanceTypeOverhead) []*cloudprovider.InstanceType {
	var instanceTypes []*cloudprovider.InstanceType

	opts := InstanceTypeOptions{
		Name:             "extra-big",
		Architecture:     "amd64",
		OperatingSystems: sets.Set[string]{
			"linux": {},
		},
		Resources: v1.ResourceList{
			v1.ResourceCPU:              resource.MustParse("1"),
			v1.ResourceMemory:           resource.MustParse("3Gi"),
			v1.ResourcePods:             resource.MustParse("1"),
			v1.ResourceEphemeralStorage: resource.MustParse("20G"),
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

func PriceFromResources(resources v1.ResourceList) float64 {
	price := 0.0
	for k, v := range resources {
		switch k {
		case v1.ResourceCPU:
			price += 0.025 * v.AsApproximateFloat64()
		case v1.ResourceMemory:
			price += 0.001 * v.AsApproximateFloat64() / (1e9)
			// case ResourceGPUVendorA, ResourceGPUVendorB:
			// 	price += 1.0
		}
	}
	return price
}
