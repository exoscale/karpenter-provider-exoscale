package instancetype

import (
	"context"
	"fmt"
	"slices"
	"sync"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/karpenter-exoscale/pkg/metrics"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/pricing"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpentercore "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/scheduling"
)

const (
	ResourceNvidiaGPU = "nvidia.com/gpu"
)

type exoscaleProvider struct {
	client          ExoscaleClient
	zone            string
	pricingProvider pricing.Provider

	mu              sync.RWMutex
	instanceTypes   []*cloudprovider.InstanceType
	instanceTypeMap map[string]*cloudprovider.InstanceType
	instanceIDMap   map[string]string
}

func NewExoscaleProvider(client ExoscaleClient, zone string, pricingProvider pricing.Provider) Provider {
	return &exoscaleProvider{
		client:          client,
		zone:            zone,
		pricingProvider: pricingProvider,
		instanceTypeMap: make(map[string]*cloudprovider.InstanceType),
		instanceIDMap:   make(map[string]string),
	}
}

func (p *exoscaleProvider) Get(_ context.Context, name string) (*cloudprovider.InstanceType, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if instanceType, ok := p.instanceTypeMap[name]; ok {
		return instanceType, nil
	}

	return nil, fmt.Errorf("instance type %s not found", name)
}

func (p *exoscaleProvider) List(ctx context.Context, filters *Filters) ([]*cloudprovider.InstanceType, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.instanceTypes) == 0 {
		p.mu.RUnlock()
		if err := p.Refresh(ctx); err != nil {
			return nil, err
		}
		p.mu.RLock()
	}

	if filters == nil {
		result := make([]*cloudprovider.InstanceType, len(p.instanceTypes))
		copy(result, p.instanceTypes)
		return result, nil
	}

	var result []*cloudprovider.InstanceType
	for _, instanceType := range p.instanceTypes {
		if p.matchesFilters(instanceType, filters) {
			result = append(result, instanceType)
		}
	}

	return result, nil
}

func (p *exoscaleProvider) Refresh(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("instance-type-provider")

	exoTypes, err := p.client.ListInstanceTypes(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch instance types from Exoscale API: %w", err)
	}

	instanceTypes, instanceTypeMap, instanceIDMap := p.buildInstanceTypes(ctx, exoTypes)

	p.mu.Lock()
	defer p.mu.Unlock()

	p.instanceTypes = instanceTypes
	p.instanceTypeMap = instanceTypeMap
	p.instanceIDMap = instanceIDMap

	metrics.InstanceTypesDiscovered.Set(float64(len(instanceTypes)), map[string]string{
		metrics.ZoneLabel: p.zone,
	})

	for name, instanceType := range instanceTypeMap {
		if cpuQuantity, ok := instanceType.Capacity[corev1.ResourceCPU]; ok {
			cpuCores, _ := cpuQuantity.AsInt64()
			metrics.InstanceTypeCPU.Set(float64(cpuCores), map[string]string{
				metrics.InstanceTypeLabel: name,
			})
		}
		if memQuantity, ok := instanceType.Capacity[corev1.ResourceMemory]; ok {
			memBytes, _ := memQuantity.AsInt64()
			metrics.InstanceTypeMemory.Set(float64(memBytes), map[string]string{
				metrics.InstanceTypeLabel: name,
			})
		}
		if len(instanceType.Offerings) > 0 && instanceType.Offerings[0].Price > 0 {
			metrics.InstanceTypePriceEstimate.Set(instanceType.Offerings[0].Price,
				map[string]string{
					metrics.InstanceTypeLabel: name,
					metrics.ZoneLabel:         p.zone,
				},
			)
		}
	}

	logger.Info("refreshed instance types", "count", len(instanceTypes), "zone", p.zone)
	return nil
}

func isInstanceTypeAuthorized(exoType egov3.InstanceType) bool {
	return exoType.Authorized != nil && *exoType.Authorized
}

func isInstanceTypeAvailableInZone(exoType egov3.InstanceType, zone string) bool {
	if exoType.Zones == nil {
		return true
	}
	return slices.Contains(exoType.Zones, egov3.ZoneName(zone))
}

func (p *exoscaleProvider) buildInstanceTypes(ctx context.Context, exoTypes *egov3.ListInstanceTypesResponse) ([]*cloudprovider.InstanceType, map[string]*cloudprovider.InstanceType, map[string]string) {
	logger := log.FromContext(ctx).WithName("instance-type-provider")

	var instanceTypes []*cloudprovider.InstanceType
	instanceTypeMap := make(map[string]*cloudprovider.InstanceType)
	instanceIDMap := make(map[string]string)

	for _, exoType := range exoTypes.InstanceTypes {
		if !isInstanceTypeAuthorized(exoType) {
			logger.V(1).Info("skipping unauthorized instance type", "family", exoType.Family, "size", exoType.Size)
			continue
		}

		if !isInstanceTypeAvailableInZone(exoType, p.zone) {
			logger.V(1).Info("skipping instance type not available in zone", "family", exoType.Family, "size", exoType.Size, "zone", p.zone)
			continue
		}

		instanceType, name := p.createInstanceType(ctx, exoType)
		instanceTypes = append(instanceTypes, instanceType)
		instanceTypeMap[name] = instanceType
		instanceIDMap[name] = string(exoType.ID)
	}

	return instanceTypes, instanceTypeMap, instanceIDMap
}

func buildResourceList(cpus, memory, gpus int64) corev1.ResourceList {
	resources := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%d", cpus)),
		corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%d", memory)),
	}
	if gpus > 0 {
		resources[ResourceNvidiaGPU] = resource.MustParse(fmt.Sprintf("%d", gpus))
	}
	return resources
}

func buildInstanceRequirements(name string, hasGPU bool) scheduling.Requirements {
	reqs := []*scheduling.Requirement{
		scheduling.NewRequirement(corev1.LabelInstanceTypeStable, corev1.NodeSelectorOpIn, name),
		scheduling.NewRequirement(corev1.LabelArchStable, corev1.NodeSelectorOpIn, "amd64"),
		scheduling.NewRequirement(corev1.LabelOSStable, corev1.NodeSelectorOpIn, string(corev1.Linux)),
		scheduling.NewRequirement(karpentercore.CapacityTypeLabelKey, corev1.NodeSelectorOpIn, karpentercore.CapacityTypeOnDemand),
	}

	if hasGPU {
		reqs = append(reqs, scheduling.NewRequirement("karpenter.sh/instance-gpu-count", corev1.NodeSelectorOpExists))
		reqs = append(reqs, scheduling.NewRequirement("karpenter.sh/instance-accelerator", corev1.NodeSelectorOpIn, "nvidia"))
	}

	return scheduling.NewRequirements(reqs...)
}

func (p *exoscaleProvider) createInstanceType(ctx context.Context, exoType egov3.InstanceType) (*cloudprovider.InstanceType, string) {
	name := fmt.Sprintf("%s.%s", exoType.Family, exoType.Size)
	resources := buildResourceList(exoType.Cpus, exoType.Memory, exoType.Gpus)
	requirements := buildInstanceRequirements(name, exoType.Gpus > 0)

	price := 0.0
	if p.pricingProvider != nil {
		if p, err := p.pricingProvider.GetPrice(ctx, name, pricing.EUR); err == nil {
			price = p
		}
	}

	offerings := cloudprovider.Offerings{
		&cloudprovider.Offering{
			Requirements: scheduling.NewRequirements(),
			Price:        price,
			Available:    true,
		},
	}

	return &cloudprovider.InstanceType{
		Name:         name,
		Requirements: requirements,
		Offerings:    offerings,
		Capacity:     resources,
		Overhead:     nil,
	}, name
}

func matchesInstanceTypeList(instanceName string, typeList []string) bool {
	if typeList == nil || len(typeList) == 0 {
		return true
	}
	for _, name := range typeList {
		if instanceName == name {
			return true
		}
	}
	return false
}

func checkResourceBounds(value resource.Quantity, min, max *resource.Quantity) bool {
	if min != nil && value.Cmp(*min) < 0 {
		return false
	}
	if max != nil && value.Cmp(*max) > 0 {
		return false
	}
	return true
}

func matchesArchitecture(requirements scheduling.Requirements, arch string) bool {
	if arch == "" {
		return true
	}
	return requirements.Get(corev1.LabelArchStable).Has(arch)
}

func (p *exoscaleProvider) matchesFilters(instanceType *cloudprovider.InstanceType, filters *Filters) bool {
	if !matchesInstanceTypeList(instanceType.Name, filters.InstanceTypes) {
		return false
	}

	if !checkResourceBounds(instanceType.Capacity[corev1.ResourceCPU], filters.MinCPU, filters.MaxCPU) {
		return false
	}

	if !checkResourceBounds(instanceType.Capacity[corev1.ResourceMemory], filters.MinMemory, filters.MaxMemory) {
		return false
	}

	if !checkResourceBounds(instanceType.Capacity[ResourceNvidiaGPU], filters.MinGPU, filters.MaxGPU) {
		return false
	}

	if !matchesArchitecture(instanceType.Requirements, filters.Architecture) {
		return false
	}

	return true
}

func (p *exoscaleProvider) GetInstanceTypeID(name string) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	id, ok := p.instanceIDMap[name]
	return id, ok
}
