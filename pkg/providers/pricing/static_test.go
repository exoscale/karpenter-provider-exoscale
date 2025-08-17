package pricing

import (
	"testing"
)

func TestNormalizeInstanceType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"standard micro", "running_micro", "standard.micro"},
		{"standard tiny", "running_tiny", "standard.tiny"},
		{"standard small", "running_small", "standard.small"},
		{"standard medium", "running_medium", "standard.medium"},
		{"standard large", "running_large", "standard.large"},
		{"standard extra-large", "running_extra_large", "standard.extra-large"},
		{"standard huge", "running_huge", "standard.huge"},
		{"standard mega", "running_mega", "standard.mega"},
		{"standard titan", "running_titan", "standard.titan"},
		{"standard jumbo", "running_jumbo", "standard.jumbo"},
		{"standard colossus", "running_colossus", "standard.colossus"},

		{"cpu extra-large", "running_cpu_extra_large", "cpu.extra-large"},
		{"cpu huge", "running_cpu_huge", "cpu.huge"},
		{"cpu mega", "running_cpu_mega", "cpu.mega"},
		{"cpu titan", "running_cpu_titan", "cpu.titan"},

		{"memory extra-large", "running_memory_extra_large", "memory.extra-large"},
		{"memory huge", "running_memory_huge", "memory.huge"},
		{"memory mega", "running_memory_mega", "memory.mega"},
		{"memory titan", "running_memory_titan", "memory.titan"},

		{"storage extra-large", "running_storage_extra_large", "storage.extra-large"},
		{"storage huge", "running_storage_huge", "storage.huge"},
		{"storage mega", "running_storage_mega", "storage.mega"},
		{"storage titan", "running_storage_titan", "storage.titan"},
		{"storage jumbo", "running_storage_jumbo", "storage.jumbo"},

		{"gpu small", "running_gpu_small", "gpu.small"},
		{"gpu medium", "running_gpu_medium", "gpu.medium"},
		{"gpu large", "running_gpu_large", "gpu.large"},
		{"gpu huge", "running_gpu_huge", "gpu.huge"},

		{"gpu2 small", "running_gpu2_small", "gpu2.small"},
		{"gpu2 medium", "running_gpu2_medium", "gpu2.medium"},
		{"gpu2 large", "running_gpu2_large", "gpu2.large"},
		{"gpu2 huge", "running_gpu2_huge", "gpu2.huge"},

		{"gpu3 small", "running_gpu3_small", "gpu3.small"},
		{"gpu3 medium", "running_gpu3_medium", "gpu3.medium"},
		{"gpu3 large", "running_gpu3_large", "gpu3.large"},
		{"gpu3 huge", "running_gpu3_huge", "gpu3.huge"},

		{"gpu_a5000 small", "running_gpu_a5000_small", "gpua5000.small"},
		{"gpu_a5000 medium", "running_gpu_a5000_medium", "gpua5000.medium"},
		{"gpu_a5000 large", "running_gpu_a5000_large", "gpua5000.large"},

		{"gpu_3080ti small", "running_gpu_3080ti_small", "gpu3080ti.small"},
		{"gpu_3080ti medium", "running_gpu_3080ti_medium", "gpu3080ti.medium"},
		{"gpu_3080ti large", "running_gpu_3080ti_large", "gpu3080ti.large"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeInstanceType(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeInstanceType(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeInstanceType_Coverage(t *testing.T) {
	allPricesJSONTypes := map[string]string{
		"running_micro":       "standard.micro",
		"running_tiny":        "standard.tiny",
		"running_small":       "standard.small",
		"running_medium":      "standard.medium",
		"running_large":       "standard.large",
		"running_extra_large": "standard.extra-large",
		"running_huge":        "standard.huge",
		"running_mega":        "standard.mega",
		"running_titan":       "standard.titan",
		"running_jumbo":       "standard.jumbo",
		"running_colossus":    "standard.colossus",

		"running_cpu_extra_large": "cpu.extra-large",
		"running_cpu_huge":        "cpu.huge",
		"running_cpu_mega":        "cpu.mega",
		"running_cpu_titan":       "cpu.titan",

		"running_memory_extra_large": "memory.extra-large",
		"running_memory_huge":        "memory.huge",
		"running_memory_mega":        "memory.mega",
		"running_memory_titan":       "memory.titan",

		"running_storage_extra_large": "storage.extra-large",
		"running_storage_huge":        "storage.huge",
		"running_storage_mega":        "storage.mega",
		"running_storage_titan":       "storage.titan",
		"running_storage_jumbo":       "storage.jumbo",

		"running_gpu_small":  "gpu.small",
		"running_gpu_medium": "gpu.medium",
		"running_gpu_large":  "gpu.large",
		"running_gpu_huge":   "gpu.huge",

		"running_gpu2_small":  "gpu2.small",
		"running_gpu2_medium": "gpu2.medium",
		"running_gpu2_large":  "gpu2.large",
		"running_gpu2_huge":   "gpu2.huge",

		"running_gpu3_small":  "gpu3.small",
		"running_gpu3_medium": "gpu3.medium",
		"running_gpu3_large":  "gpu3.large",
		"running_gpu3_huge":   "gpu3.huge",

		"running_gpu_a5000_small":  "gpua5000.small",
		"running_gpu_a5000_medium": "gpua5000.medium",
		"running_gpu_a5000_large":  "gpua5000.large",

		"running_gpu_3080ti_small":  "gpu3080ti.small",
		"running_gpu_3080ti_medium": "gpu3080ti.medium",
		"running_gpu_3080ti_large":  "gpu3080ti.large",
	}

	for input, expected := range allPricesJSONTypes {
		t.Run(input, func(t *testing.T) {
			result := normalizeInstanceType(input)
			if result != expected {
				t.Errorf("normalizeInstanceType(%q) = %q, want %q", input, result, expected)
			}
		})
	}
}
