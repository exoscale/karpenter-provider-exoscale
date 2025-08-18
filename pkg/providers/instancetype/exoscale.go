package instancetype

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	egov3 "github.com/exoscale/egoscale/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpentercore "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/scheduling"
)

const ResourceNvidiaGPU = "nvidia.com/gpu"

//go:embed prices.json
var pricesJSON []byte

type rawPricingData struct {
	CHF map[string]string `json:"chf"`
	EUR map[string]string `json:"eur"`
	USD map[string]string `json:"usd"`
}

type exoscaleProvider struct {
	client ExoscaleClient
	zone   string

	mu              sync.RWMutex
	instanceTypes   []*cloudprovider.InstanceType
	instanceTypeMap map[string]*cloudprovider.InstanceType
	instanceIDMap   map[string]string
	prices          map[string]float64 // EUR prices
}

func NewExoscaleProvider(client ExoscaleClient, zone string) Provider {
	p := &exoscaleProvider{
		client:          client,
		zone:            zone,
		instanceTypeMap: make(map[string]*cloudprovider.InstanceType),
		instanceIDMap:   make(map[string]string),
		prices:          make(map[string]float64),
	}
	// Load static prices on initialization
	_ = p.loadPrices()
	return p
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
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("provider", "instancetype"))

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

	log.FromContext(ctx).Info("refreshed instance types", "count", len(instanceTypes), "zone", p.zone)
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

	var instanceTypes []*cloudprovider.InstanceType
	instanceTypeMap := make(map[string]*cloudprovider.InstanceType)
	instanceIDMap := make(map[string]string)

	for _, exoType := range exoTypes.InstanceTypes {
		if !isInstanceTypeAuthorized(exoType) {
			log.FromContext(ctx).V(1).Info("skipping unauthorized instance type", "family", exoType.Family, "size", exoType.Size)
			continue
		}

		if !isInstanceTypeAvailableInZone(exoType, p.zone) {
			log.FromContext(ctx).V(1).Info("skipping instance type not available in zone", "family", exoType.Family, "size", exoType.Size, "zone", p.zone)
			continue
		}

		instanceType, name := p.createInstanceType(exoType)
		instanceTypes = append(instanceTypes, instanceType)
		instanceTypeMap[name] = instanceType
		instanceIDMap[name] = string(exoType.ID)
	}

	// Sort by price (cheapest first) for better cost optimization
	sort.Slice(instanceTypes, func(i, j int) bool {
		priceI := 0.0
		priceJ := 0.0
		if len(instanceTypes[i].Offerings) > 0 {
			priceI = instanceTypes[i].Offerings[0].Price
		}
		if len(instanceTypes[j].Offerings) > 0 {
			priceJ = instanceTypes[j].Offerings[0].Price
		}
		return priceI < priceJ
	})

	return instanceTypes, instanceTypeMap, instanceIDMap
}

func buildResourceList(cpus, memory, gpus int64) corev1.ResourceList {
	resources := corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewQuantity(cpus, resource.DecimalSI),
		corev1.ResourceMemory: *resource.NewQuantity(memory, resource.BinarySI),
		corev1.ResourcePods:   *resource.NewQuantity(110, resource.DecimalSI),
	}

	if gpus > 0 {
		resources[ResourceNvidiaGPU] = *resource.NewQuantity(gpus, resource.DecimalSI)
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

func (p *exoscaleProvider) createInstanceType(exoType egov3.InstanceType) (*cloudprovider.InstanceType, string) {
	family := string(exoType.Family)
	if family == "" {
		family = "standard"
	}
	name := family + "." + string(exoType.Size)
	resources := buildResourceList(exoType.Cpus, exoType.Memory, exoType.Gpus)
	requirements := buildInstanceRequirements(name, exoType.Gpus > 0)

	price := 0.0
	priceLookupKey := normalizeInstanceType(name)
	if priceVal, ok := p.prices[priceLookupKey]; ok {
		price = priceVal
	}

	offerings := cloudprovider.Offerings{
		&cloudprovider.Offering{
			Requirements: scheduling.NewRequirements(
				scheduling.NewRequirement(karpentercore.CapacityTypeLabelKey, corev1.NodeSelectorOpIn, karpentercore.CapacityTypeOnDemand),
			),
			Price:     price,
			Available: true,
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
	if len(typeList) == 0 {
		return true
	}
	return slices.Contains(typeList, instanceName)
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

func (p *exoscaleProvider) loadPrices() error {
	var rawData rawPricingData
	if err := json.Unmarshal(pricesJSON, &rawData); err != nil {
		return fmt.Errorf("failed to unmarshal pricing data: %w", err)
	}

	// Parse EUR prices (default currency)
	for rawKey, rawPrice := range rawData.EUR {
		price, err := strconv.ParseFloat(rawPrice, 64)
		if err != nil {
			continue // Skip invalid prices
		}
		normalizedKey := normalizeInstanceType(rawKey)
		p.prices[normalizedKey] = price
	}

	return nil
}

func normalizeInstanceType(rawKey string) string {
	if strings.Contains(rawKey, ".") {
		if strings.HasPrefix(rawKey, "gpua30.") {
			// In gva2, gpu became gpua30, however the original price file didn't changed
			return strings.Replace(rawKey, "gpua30.", "gpu.", 1)
		}
		return rawKey
	}

	// Process raw price keys from prices.json
	key := strings.TrimPrefix(rawKey, "running_")
	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, "_", "-")

	sizes := []string{"extra-large", "colossus", "jumbo", "titan", "micro", "tiny", "small", "medium", "large", "huge", "mega"}

	for _, size := range sizes {
		if key == size {
			return "standard." + size
		}

		suffix := "-" + size
		if strings.HasSuffix(key, suffix) {
			family := strings.TrimSuffix(key, suffix)
			family = strings.ReplaceAll(family, "-", "")
			return family + "." + size
		}
	}

	return key
}
