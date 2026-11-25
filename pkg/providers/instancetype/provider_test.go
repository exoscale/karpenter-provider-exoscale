package instancetype

import (
	"context"
	"strings"
	"testing"

	egov3 "github.com/exoscale/egoscale/v3"
	exov1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/scheduling"
)

func TestBuildResourceList(t *testing.T) {
	tests := []struct {
		name   string
		cpus   int64
		memory int64
		gpus   int64
	}{
		{
			name:   "standard instance",
			cpus:   4,
			memory: 8 * 1024 * 1024 * 1024,
			gpus:   0,
		},
		{
			name:   "gpu instance",
			cpus:   8,
			memory: 16 * 1024 * 1024 * 1024,
			gpus:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildResourceList(tt.cpus, tt.memory, tt.gpus)

			cpuQty := got[corev1.ResourceCPU]
			if cpuQty.Value() != tt.cpus {
				t.Errorf("buildResourceList() CPU = %v, want %v", cpuQty.Value(), tt.cpus)
			}

			memQty := got[corev1.ResourceMemory]
			if memQty.Value() != tt.memory {
				t.Errorf("buildResourceList() Memory = %v, want %v", memQty.Value(), tt.memory)
			}

			if tt.gpus > 0 {
				gpuQty := got[ResourceNvidiaGPU]
				if gpuQty.Value() != tt.gpus {
					t.Errorf("buildResourceList() GPU = %v, want %v", gpuQty.Value(), tt.gpus)
				}
			}

			podsQty := got[corev1.ResourcePods]
			if podsQty.Value() != 110 {
				t.Errorf("buildResourceList() Pods = %v, want 110", podsQty.Value())
			}
		})
	}
}

func TestExtractPrice(t *testing.T) {
	tests := []struct {
		name     string
		rawKey   string
		wantName string
	}{
		{
			name:     "standard family implicit",
			rawKey:   "running_medium",
			wantName: "standard.medium",
		},
		{
			name:     "cpu family",
			rawKey:   "running_cpu_medium",
			wantName: "cpu.medium",
		},
		{
			name:     "gpu family",
			rawKey:   "running_gpu_large",
			wantName: "gpua30.large",
		},
		{
			name:     "gpu with underscore",
			rawKey:   "running_gpu_a5000_medium",
			wantName: "gpua5000.medium",
		},
		{
			name:     "extra large, which also ends in large",
			rawKey:   "running_extra_large",
			wantName: "standard.extra-large",
		},
		{
			name:     "family with extra large",
			rawKey:   "running_memory_extra_large",
			wantName: "memory.extra-large",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Sizes are matched by iterating a map, so repeat to cover iteration orders.
			for range 50 {
				gotName, _, err := extractPrice(tt.rawKey, "0.123")
				if err != nil {
					t.Fatalf("extractPrice() unexpected error = %v", err)
				}
				if gotName != tt.wantName {
					t.Fatalf("extractPrice() name = %v, want %v", gotName, tt.wantName)
				}
			}
		})
	}
}

func TestLoadPricesMapsEveryKeyToAnInstanceType(t *testing.T) {
	prices, err := loadPrices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for name := range prices {
		if family, size, _ := strings.Cut(name, "."); family == "" || size == "" {
			t.Errorf("price key mapped to %q", name)
		}
	}
	if _, ok := prices["standard.extra-large"]; !ok {
		t.Error("standard.extra-large has no price")
	}
}

// newTestProvider builds a Provider pre-populated with a fixed set of instance types
// (no API calls needed).
func newTestProvider() *Provider {
	baseCapacity := buildResourceList(4, 8*1024*1024*1024, 0)
	requirements := scheduling.NewRequirements(
		scheduling.NewRequirement(corev1.LabelInstanceTypeStable, corev1.NodeSelectorOpIn, "standard.medium"),
	)
	instanceTypes := map[string]*cloudprovider.InstanceType{
		"standard.medium": {
			Name:         "standard.medium",
			Requirements: requirements,
			Capacity:     baseCapacity,
			Overhead:     &cloudprovider.InstanceTypeOverhead{},
		},
		"standard.large": {
			Name:         "standard.large",
			Requirements: requirements,
			Capacity:     buildResourceList(8, 16*1024*1024*1024, 0),
			Overhead:     &cloudprovider.InstanceTypeOverhead{},
		},
	}
	return &Provider{
		instanceTypesByName: instanceTypes,
		instanceTypesByID:   make(map[string]*cloudprovider.InstanceType),
		prices:              make(map[string]float64),
	}
}

func TestList(t *testing.T) {
	tests := []struct {
		name                  string
		nodeClass             *exov1.ExoscaleNodeClass
		wantCount             int
		wantDiskBytes         int64
		wantKubeReservedCPU   string
		wantKubeReservedMem   string
		wantKubeReservedEph   string
		wantSystemReservedCPU string
		wantSystemReservedMem string
		wantSystemReservedEph string
	}{
		{
			name: "both KubeReserved and SystemReserved fully set",
			nodeClass: &exov1.ExoscaleNodeClass{
				Spec: exov1.ExoscaleNodeClassSpec{
					DiskSize: 100,
					Kubelet: exov1.KubeletConfiguration{
						KubeReserved: exov1.KubeResourceReservation{
							CPU:              "200m",
							Memory:           "300Mi",
							EphemeralStorage: "1Gi",
						},
						SystemReserved: exov1.SystemResourceReservation{
							CPU:              "100m",
							Memory:           "100Mi",
							EphemeralStorage: "3Gi",
						},
					},
				},
			},
			wantCount:             2,
			wantDiskBytes:         100 * 1024 * 1024 * 1024,
			wantKubeReservedCPU:   "200m",
			wantKubeReservedMem:   "300Mi",
			wantKubeReservedEph:   "1Gi",
			wantSystemReservedCPU: "100m",
			wantSystemReservedMem: "100Mi",
			wantSystemReservedEph: "3Gi",
		},
		{
			name: "only KubeReserved set, SystemReserved empty",
			nodeClass: &exov1.ExoscaleNodeClass{
				Spec: exov1.ExoscaleNodeClassSpec{
					DiskSize: 50,
					Kubelet: exov1.KubeletConfiguration{
						KubeReserved: exov1.KubeResourceReservation{
							CPU:              "200m",
							Memory:           "300Mi",
							EphemeralStorage: "1Gi",
						},
					},
				},
			},
			wantCount:             2,
			wantDiskBytes:         50 * 1024 * 1024 * 1024,
			wantKubeReservedCPU:   "200m",
			wantKubeReservedMem:   "300Mi",
			wantKubeReservedEph:   "1Gi",
			wantSystemReservedCPU: "0",
			wantSystemReservedMem: "0",
			wantSystemReservedEph: "0",
		},
		{
			name: "only SystemReserved set, KubeReserved empty",
			nodeClass: &exov1.ExoscaleNodeClass{
				Spec: exov1.ExoscaleNodeClassSpec{
					DiskSize: 50,
					Kubelet: exov1.KubeletConfiguration{
						SystemReserved: exov1.SystemResourceReservation{
							CPU:              "100m",
							Memory:           "100Mi",
							EphemeralStorage: "3Gi",
						},
					},
				},
			},
			wantCount:             2,
			wantDiskBytes:         50 * 1024 * 1024 * 1024,
			wantKubeReservedCPU:   "0",
			wantKubeReservedMem:   "0",
			wantKubeReservedEph:   "0",
			wantSystemReservedCPU: "100m",
			wantSystemReservedMem: "100Mi",
			wantSystemReservedEph: "3Gi",
		},
		{
			name: "neither KubeReserved nor SystemReserved set",
			nodeClass: &exov1.ExoscaleNodeClass{
				Spec: exov1.ExoscaleNodeClassSpec{
					DiskSize: 50,
				},
			},
			wantCount:             2,
			wantDiskBytes:         50 * 1024 * 1024 * 1024,
			wantKubeReservedCPU:   "0",
			wantKubeReservedMem:   "0",
			wantKubeReservedEph:   "0",
			wantSystemReservedCPU: "0",
			wantSystemReservedMem: "0",
			wantSystemReservedEph: "0",
		},
		{
			name: "partial KubeReserved: only CPU set",
			nodeClass: &exov1.ExoscaleNodeClass{
				Spec: exov1.ExoscaleNodeClassSpec{
					DiskSize: 50,
					Kubelet: exov1.KubeletConfiguration{
						KubeReserved: exov1.KubeResourceReservation{
							CPU: "500m",
						},
						SystemReserved: exov1.SystemResourceReservation{
							Memory:           "200Mi",
							EphemeralStorage: "2Gi",
						},
					},
				},
			},
			wantCount:             2,
			wantDiskBytes:         50 * 1024 * 1024 * 1024,
			wantKubeReservedCPU:   "500m",
			wantKubeReservedMem:   "0",
			wantKubeReservedEph:   "0",
			wantSystemReservedCPU: "0",
			wantSystemReservedMem: "200Mi",
			wantSystemReservedEph: "2Gi",
		},
	}

	p := newTestProvider()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := p.List(tt.nodeClass)
			if err != nil {
				t.Fatalf("List() unexpected error: %v", err)
			}

			if len(got) != tt.wantCount {
				t.Fatalf("List() returned %d instance types, want %d", len(got), tt.wantCount)
			}

			for _, it := range got {
				diskQty := it.Capacity[corev1.ResourceEphemeralStorage]
				if diskQty.Value() != tt.wantDiskBytes {
					t.Errorf("[%s] EphemeralStorage capacity = %v, want %v", it.Name, diskQty.Value(), tt.wantDiskBytes)
				}

				assertResource(t, it.Name, "KubeReserved CPU", it.Overhead.KubeReserved[corev1.ResourceCPU], tt.wantKubeReservedCPU)
				assertResource(t, it.Name, "KubeReserved Memory", it.Overhead.KubeReserved[corev1.ResourceMemory], tt.wantKubeReservedMem)
				assertResource(t, it.Name, "KubeReserved EphemeralStorage", it.Overhead.KubeReserved[corev1.ResourceEphemeralStorage], tt.wantKubeReservedEph)

				assertResource(t, it.Name, "SystemReserved CPU", it.Overhead.SystemReserved[corev1.ResourceCPU], tt.wantSystemReservedCPU)
				assertResource(t, it.Name, "SystemReserved Memory", it.Overhead.SystemReserved[corev1.ResourceMemory], tt.wantSystemReservedMem)
				assertResource(t, it.Name, "SystemReserved EphemeralStorage", it.Overhead.SystemReserved[corev1.ResourceEphemeralStorage], tt.wantSystemReservedEph)
			}
		})
	}
}

// assertResource compares a resource.Quantity against a string representation.
// An empty or zero wantStr means the quantity must be zero.
func assertResource(t *testing.T, instanceName, label string, got resource.Quantity, wantStr string) {
	t.Helper()
	var want resource.Quantity
	if wantStr != "" && wantStr != "0" {
		want = resource.MustParse(wantStr)
	}
	if got.Cmp(want) != 0 {
		t.Errorf("[%s] %s = %v, want %v", instanceName, label, got.String(), want.String())
	}
}

func TestCreateInstanceTypeOfferingPrice(t *testing.T) {
	p := &Provider{zone: "ch-gva-2", prices: map[string]float64{"standard.medium": 0.04666}}

	it := p.createInstanceType("standard", "medium", "standard.medium", egov3.InstanceType{Cpus: 2, Memory: 4 * 1024 * 1024 * 1024})

	price, ok := it.OfferingPrice("ch-gva-2", karpenterv1.CapacityTypeOnDemand)
	if !ok || price != 0.04666 {
		t.Errorf("OfferingPrice() = %v, %v, want 0.04666, true", price, ok)
	}
	if zone := it.Requirements.Get(corev1.LabelTopologyZone).Any(); zone != "ch-gva-2" {
		t.Errorf("zone requirement = %q, want ch-gva-2", zone)
	}
}
