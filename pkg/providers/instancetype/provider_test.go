package instancetype

import (
	"testing"

	v1 "k8s.io/api/core/v1"
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

			cpuQty := got[v1.ResourceCPU]
			if cpuQty.Value() != tt.cpus {
				t.Errorf("buildResourceList() CPU = %v, want %v", cpuQty.Value(), tt.cpus)
			}

			memQty := got[v1.ResourceMemory]
			if memQty.Value() != tt.memory {
				t.Errorf("buildResourceList() Memory = %v, want %v", memQty.Value(), tt.memory)
			}

			if tt.gpus > 0 {
				gpuQty := got[ResourceNvidiaGPU]
				if gpuQty.Value() != tt.gpus {
					t.Errorf("buildResourceList() GPU = %v, want %v", gpuQty.Value(), tt.gpus)
				}
			}

			podsQty := got[v1.ResourcePods]
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
