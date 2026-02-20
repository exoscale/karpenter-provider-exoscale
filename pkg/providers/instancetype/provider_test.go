package instancetype

import (
	"testing"

	exov1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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

func TestRawInstanceSizes(t *testing.T) {
	sizes := rawInstanceSizes()

	if len(sizes) == 0 {
		t.Error("rawInstanceSizes() returned empty map")
	}

	expectedSizes := []string{"micro", "tiny", "small", "medium", "large", "huge", "mega", "extra_large", "colossus", "jumbo", "titan"}
	for _, size := range expectedSizes {
		if _, ok := sizes[size]; !ok {
			t.Errorf("rawInstanceSizes() missing size %s", size)
		}
	}
}

func TestRawInstanceFamilies(t *testing.T) {
	families := rawInstanceFamilies()

	if len(families) == 0 {
		t.Error("rawInstanceFamilies() returned empty map")
	}

	expectedFamilies := []string{"", "cpu", "storage", "memory", "gpu", "gpu2", "gpu3", "gpu_a5000", "gpu_3080ti"}
	for _, family := range expectedFamilies {
		if _, ok := families[family]; !ok {
			t.Errorf("rawInstanceFamilies() missing family %s", family)
		}
	}
}

func TestExtractPrice(t *testing.T) {
	rawFamilies := rawInstanceFamilies()
	rawSizes := rawInstanceSizes()

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, _, err := extractPrice(rawFamilies, rawSizes, tt.rawKey, "0.123")

			if err != nil {
				t.Errorf("extractPrice() unexpected error = %v", err)
				return
			}

			if gotName != tt.wantName {
				t.Errorf("extractPrice() name = %v, want %v", gotName, tt.wantName)
			}
		})
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

func mustParseQty(s string) resource.Quantity {
	return resource.MustParse(s)
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
				// Check ephemeral storage capacity reflects DiskSize
				diskQty := it.Capacity[corev1.ResourceEphemeralStorage]
				if diskQty.Value() != tt.wantDiskBytes {
					t.Errorf("[%s] EphemeralStorage capacity = %v, want %v", it.Name, diskQty.Value(), tt.wantDiskBytes)
				}

				// Check KubeReserved
				assertResource(t, it.Name, "KubeReserved CPU", it.Overhead.KubeReserved[corev1.ResourceCPU], tt.wantKubeReservedCPU)
				assertResource(t, it.Name, "KubeReserved Memory", it.Overhead.KubeReserved[corev1.ResourceMemory], tt.wantKubeReservedMem)
				assertResource(t, it.Name, "KubeReserved EphemeralStorage", it.Overhead.KubeReserved[corev1.ResourceEphemeralStorage], tt.wantKubeReservedEph)

				// Check SystemReserved
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
		want = mustParseQty(wantStr)
	}
	if got.Cmp(want) != 0 {
		t.Errorf("[%s] %s = %v, want %v", instanceName, label, got.String(), want.String())
	}
}
