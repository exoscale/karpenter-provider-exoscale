package instancetype

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"

	egov3 "github.com/exoscale/egoscale/v3"
	v1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/scheduling"
)

const ResourceNvidiaGPU = "nvidia.com/gpu"

//go:embed prices.json
var pricesJSON []byte

type Provider struct {
	client *egov3.Client
	zone   string

	mu                  sync.RWMutex
	instanceTypesByName map[string]*cloudprovider.InstanceType
	instanceTypesByID   map[string]*cloudprovider.InstanceType
	prices              map[string]float64 // EUR prices
}

func NewExoscaleProvider(ctx context.Context, client *egov3.Client, zone string) (*Provider, error) {
	p := &Provider{
		client: client,
		zone:   zone,
		prices: make(map[string]float64),
	}

	prices, err := p.loadPrices(ctx)
	if err != nil {
		return nil, err
	}

	p.prices = prices
	return p, nil
}

func (p *Provider) GetByName(name string) (*cloudprovider.InstanceType, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if instanceType, ok := p.instanceTypesByName[name]; ok {
		return instanceType, nil
	}

	return nil, fmt.Errorf("instance type with name '%s' not found", name)
}

func (p *Provider) GetByID(id string) (*cloudprovider.InstanceType, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if instanceType, ok := p.instanceTypesByID[id]; ok {
		return instanceType, nil
	}

	return nil, fmt.Errorf("instance type with id '%s' not found", id)
}

func (p *Provider) GetIDForName(name string) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for ID, instanceType := range p.instanceTypesByID {
		if instanceType.Name == name {
			return ID, true
		}
	}

	return "", false
}

func (p *Provider) List(nodeClass *v1.ExoscaleNodeClass) ([]*cloudprovider.InstanceType, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	baseTypes := lo.Values(p.instanceTypesByName)
	instanceTypes := make([]*cloudprovider.InstanceType, len(baseTypes))

	parseResource := func(value string) resource.Quantity {
		if qty, err := resource.ParseQuantity(value); err == nil {
			return qty
		}
		return resource.Quantity{}
	}

	for i, base := range baseTypes {
		capacity := base.Capacity.DeepCopy()
		diskSizeBytes := nodeClass.Spec.DiskSize * 1024 * 1024 * 1024
		capacity[corev1.ResourceEphemeralStorage] = *resource.NewQuantity(diskSizeBytes, resource.BinarySI)

		overhead := &cloudprovider.InstanceTypeOverhead{
			KubeReserved: corev1.ResourceList{
				corev1.ResourceCPU:              parseResource(nodeClass.Spec.KubeReserved.CPU),
				corev1.ResourceMemory:           parseResource(nodeClass.Spec.KubeReserved.Memory),
				corev1.ResourceEphemeralStorage: parseResource(nodeClass.Spec.KubeReserved.EphemeralStorage),
			},
			SystemReserved: corev1.ResourceList{
				corev1.ResourceCPU:              parseResource(nodeClass.Spec.SystemReserved.CPU),
				corev1.ResourceMemory:           parseResource(nodeClass.Spec.SystemReserved.Memory),
				corev1.ResourceEphemeralStorage: parseResource(nodeClass.Spec.SystemReserved.EphemeralStorage),
			},
		}

		instanceTypes[i] = &cloudprovider.InstanceType{
			Name:         base.Name,
			Requirements: base.Requirements,
			Offerings:    base.Offerings.DeepCopy(),
			Capacity:     capacity,
			Overhead:     overhead,
		}
	}

	return instanceTypes, nil
}

func (p *Provider) Refresh(ctx context.Context) error {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("provider", "instancetype"))

	exoTypes, err := p.client.ListInstanceTypes(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch instance types from Exoscale API: %w", err)
	}

	instanceTypesByName, instanceTypesByID := p.buildInstanceTypes(ctx, exoTypes)

	p.mu.Lock()
	defer p.mu.Unlock()

	p.instanceTypesByName = instanceTypesByName
	p.instanceTypesByID = instanceTypesByID

	log.FromContext(ctx).Info("refreshed instance types", "count", len(instanceTypesByName))
	return nil
}

func (p *Provider) buildInstanceTypes(ctx context.Context, exoTypes *egov3.ListInstanceTypesResponse) (
	map[string]*cloudprovider.InstanceType,
	map[string]*cloudprovider.InstanceType,
) {
	instanceTypesByName := make(map[string]*cloudprovider.InstanceType)
	instanceTypesByID := make(map[string]*cloudprovider.InstanceType)

	for _, exoType := range exoTypes.InstanceTypes {
		if exoType.Authorized == nil || !*exoType.Authorized {
			log.FromContext(ctx).V(1).Info("skipping unauthorized instance type", "family", exoType.Family, "size", exoType.Size)
			continue
		}

		if exoType.Zones == nil || !slices.Contains(exoType.Zones, egov3.ZoneName(p.zone)) {
			log.FromContext(ctx).V(1).Info("skipping instance type not available in zone", "family", exoType.Family, "size", exoType.Size, "zone", p.zone)
			continue
		}

		instanceFamily := string(exoType.Family)
		instanceSize := string(exoType.Size)

		name := instanceFamily + "." + instanceSize
		instanceTypesByName[name] = p.createInstanceType(instanceFamily, instanceSize, name, exoType)
		instanceTypesByID[string(exoType.ID)] = instanceTypesByName[name]
	}

	return instanceTypesByName, instanceTypesByID
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

func buildInstanceRequirements(instanceFamily, instanceSize, name, zone string, hasGPU bool) scheduling.Requirements {
	reqs := []*scheduling.Requirement{
		scheduling.NewRequirement(corev1.LabelTopologyRegion, corev1.NodeSelectorOpIn, zone),
		scheduling.NewRequirement(corev1.LabelInstanceTypeStable, corev1.NodeSelectorOpIn, name),
		scheduling.NewRequirement(corev1.LabelArchStable, corev1.NodeSelectorOpIn, "amd64"),
		scheduling.NewRequirement(corev1.LabelOSStable, corev1.NodeSelectorOpIn, string(corev1.Linux)),

		scheduling.NewRequirement(karpenterv1.CapacityTypeLabelKey, corev1.NodeSelectorOpIn, karpenterv1.CapacityTypeOnDemand),
		scheduling.NewRequirement(constants.LabelInstanceFamily, corev1.NodeSelectorOpIn, instanceFamily),
		scheduling.NewRequirement(constants.LabelInstanceSize, corev1.NodeSelectorOpIn, instanceSize),
	}

	if hasGPU {
		reqs = append(reqs, scheduling.NewRequirement("karpenter.sh/instance-gpu-count", corev1.NodeSelectorOpExists))
		reqs = append(reqs, scheduling.NewRequirement("karpenter.sh/instance-accelerator", corev1.NodeSelectorOpIn, "nvidia"))
	}

	return scheduling.NewRequirements(reqs...)
}

func (p *Provider) createInstanceType(instanceFamily, instanceSize, name string, exoType egov3.InstanceType) *cloudprovider.InstanceType {
	resources := buildResourceList(exoType.Cpus, exoType.Memory, exoType.Gpus)
	requirements := buildInstanceRequirements(instanceFamily, instanceSize, name, p.zone, exoType.Gpus > 0)

	price := math.MaxFloat64
	if priceVal, ok := p.prices[name]; ok {
		price = priceVal
	}

	offerings := cloudprovider.Offerings{
		&cloudprovider.Offering{
			Requirements: scheduling.NewRequirements(
				scheduling.NewRequirement(karpenterv1.CapacityTypeLabelKey, corev1.NodeSelectorOpIn, karpenterv1.CapacityTypeOnDemand),
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
		Overhead:     &cloudprovider.InstanceTypeOverhead{},
	}
}

func rawInstanceSizes() map[string]string {
	sizes := map[string]string{
		"extra_large": "extra-large",
		"colossus":    "colossus",
		"jumbo":       "jumbo",
		"titan":       "titan",
		"micro":       "micro",
		"tiny":        "tiny",
		"small":       "small",
		"medium":      "medium",
		"large":       "large",
		"huge":        "huge",
		"mega":        "mega",
	}
	return sizes
}

// rawInstanceFamilies returns a map where keys are in-file families and values are actual families
func rawInstanceFamilies() map[string]string {
	return map[string]string{
		"":           "standard",
		"cpu":        "cpu",
		"storage":    "storage",
		"memory":     "memory",
		"gpu":        "gpua30",
		"gpu2":       "gpu2",
		"gpu3":       "gpu3",
		"gpu_a5000":  "gpua5000",
		"gpu_3080ti": "gpu3080ti",
	}
}

func (p *Provider) loadPrices(ctx context.Context) (map[string]float64, error) {
	logger := log.FromContext(ctx)

	type RawPricingData struct {
		CHF map[string]string `json:"chf"`
		EUR map[string]string `json:"eur"`
		USD map[string]string `json:"usd"`
	}

	var rawPriceData RawPricingData
	if err := json.Unmarshal(pricesJSON, &rawPriceData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal pricing data: %w", err)
	}

	rawFamilies := rawInstanceFamilies()
	rawSizes := rawInstanceSizes()
	prices := make(map[string]float64)

	// Parse EUR prices (default currency)
	for rawKey, rawPrice := range rawPriceData.EUR {
		if !strings.HasPrefix(rawKey, "running_") {
			continue
		}

		name, price, err := extractPrice(rawFamilies, rawSizes, rawKey, rawPrice)
		if err != nil {
			logger.Info("unable to parse price", "key", rawKey, "price", rawPrice)
			continue
		}

		logger.Info("found new price", "name", name, "price", price)

		prices[name] = price
	}

	return prices, nil
}

func extractPrice(
	rawFamilies map[string]string,
	rawSizes map[string]string,
	rawKey, rawPrice string,
) (string, float64, error) {
	var instanceFamily string
	var instanceSize string

	normalizedKey, _ := strings.CutPrefix(rawKey, "running_")

	for size := range rawSizes {
		if strings.HasSuffix(normalizedKey, size) {
			instanceSize = size
			// pattern is 'running_<instance-family>_<instance-size>'
			// or 'running_<instance-size>' (implicit 'standard' family)
			instanceFamily = strings.TrimSuffix(normalizedKey, size)
			instanceFamily = strings.TrimSuffix(instanceFamily, "_")
			break
		}
	}

	instanceSize = rawSizes[instanceSize]
	instanceFamily = rawFamilies[instanceFamily]

	instancePrice, err := strconv.ParseFloat(rawPrice, 64)
	if err != nil {
		return "", 0, fmt.Errorf("invalid instance price %s", rawPrice)
	}

	return instanceFamily + "." + instanceSize, instancePrice, nil
}
