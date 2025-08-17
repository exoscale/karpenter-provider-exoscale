package instancetype

import (
	"testing"

	egov3 "github.com/exoscale/egoscale/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/karpenter/pkg/scheduling"
)

func TestIsInstanceTypeAuthorized(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name    string
		exoType egov3.InstanceType
		want    bool
	}{
		{
			name: "authorized true",
			exoType: egov3.InstanceType{
				Authorized: &trueVal,
			},
			want: true,
		},
		{
			name: "authorized false",
			exoType: egov3.InstanceType{
				Authorized: &falseVal,
			},
			want: false,
		},
		{
			name:    "authorized nil",
			exoType: egov3.InstanceType{},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isInstanceTypeAuthorized(tt.exoType); got != tt.want {
				t.Errorf("isInstanceTypeAuthorized() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsInstanceTypeAvailableInZone(t *testing.T) {
	tests := []struct {
		name    string
		exoType egov3.InstanceType
		zone    string
		want    bool
	}{
		{
			name: "zone in list",
			exoType: egov3.InstanceType{
				Zones: []egov3.ZoneName{"ch-gva-2", "ch-dk-2"},
			},
			zone: "ch-gva-2",
			want: true,
		},
		{
			name: "zone not in list",
			exoType: egov3.InstanceType{
				Zones: []egov3.ZoneName{"ch-gva-2", "ch-dk-2"},
			},
			zone: "de-fra-1",
			want: false,
		},
		{
			name:    "nil zones list",
			exoType: egov3.InstanceType{},
			zone:    "ch-gva-2",
			want:    true,
		},
		{
			name: "empty zones list",
			exoType: egov3.InstanceType{
				Zones: []egov3.ZoneName{},
			},
			zone: "ch-gva-2",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isInstanceTypeAvailableInZone(tt.exoType, tt.zone); got != tt.want {
				t.Errorf("isInstanceTypeAvailableInZone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildResourceList(t *testing.T) {
	tests := []struct {
		name    string
		cpus    int64
		memory  int64
		gpus    int64
		wantCPU string
		wantMem string
		wantGPU string
		hasGPU  bool
	}{
		{
			name:    "standard instance",
			cpus:    4,
			memory:  8589934592, // 8Gi
			gpus:    0,
			wantCPU: "4",
			wantMem: "8589934592",
			hasGPU:  false,
		},
		{
			name:    "gpu instance",
			cpus:    8,
			memory:  17179869184, // 16Gi
			gpus:    2,
			wantCPU: "8",
			wantMem: "17179869184",
			wantGPU: "2",
			hasGPU:  true,
		},
		{
			name:    "micro instance",
			cpus:    1,
			memory:  536870912, // 512Mi
			gpus:    0,
			wantCPU: "1",
			wantMem: "536870912",
			hasGPU:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildResourceList(tt.cpus, tt.memory, tt.gpus)

			cpu := got[corev1.ResourceCPU]
			if cpu.String() != tt.wantCPU {
				t.Errorf("CPU = %v, want %v", cpu.String(), tt.wantCPU)
			}

			mem := got[corev1.ResourceMemory]
			if mem.String() != tt.wantMem {
				t.Errorf("Memory = %v, want %v", mem.String(), tt.wantMem)
			}

			gpu, hasGPU := got[ResourceNvidiaGPU]
			if hasGPU != tt.hasGPU {
				t.Errorf("Has GPU = %v, want %v", hasGPU, tt.hasGPU)
			}
			if hasGPU && gpu.String() != tt.wantGPU {
				t.Errorf("GPU = %v, want %v", gpu.String(), tt.wantGPU)
			}
		})
	}
}

func TestMatchesInstanceTypeList(t *testing.T) {
	tests := []struct {
		name         string
		instanceName string
		typeList     []string
		want         bool
	}{
		{
			name:         "match found",
			instanceName: "standard.medium",
			typeList:     []string{"standard.small", "standard.medium", "standard.large"},
			want:         true,
		},
		{
			name:         "no match",
			instanceName: "standard.huge",
			typeList:     []string{"standard.small", "standard.medium", "standard.large"},
			want:         false,
		},
		{
			name:         "empty list allows all",
			instanceName: "any.type",
			typeList:     []string{},
			want:         true,
		},
		{
			name:         "nil list allows all",
			instanceName: "any.type",
			typeList:     nil,
			want:         true,
		},
		{
			name:         "single item match",
			instanceName: "gpu.large",
			typeList:     []string{"gpu.large"},
			want:         true,
		},
		{
			name:         "single item no match",
			instanceName: "gpu.large",
			typeList:     []string{"gpu.small"},
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesInstanceTypeList(tt.instanceName, tt.typeList); got != tt.want {
				t.Errorf("matchesInstanceTypeList() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckResourceBounds(t *testing.T) {
	small := resource.MustParse("2")
	medium := resource.MustParse("4")
	large := resource.MustParse("8")

	tests := []struct {
		name  string
		value resource.Quantity
		min   *resource.Quantity
		max   *resource.Quantity
		want  bool
	}{
		{
			name:  "within bounds",
			value: medium,
			min:   &small,
			max:   &large,
			want:  true,
		},
		{
			name:  "equal to min",
			value: small,
			min:   &small,
			max:   &large,
			want:  true,
		},
		{
			name:  "equal to max",
			value: large,
			min:   &small,
			max:   &large,
			want:  true,
		},
		{
			name:  "below min",
			value: small,
			min:   &medium,
			max:   &large,
			want:  false,
		},
		{
			name:  "above max",
			value: large,
			min:   &small,
			max:   &medium,
			want:  false,
		},
		{
			name:  "no min constraint",
			value: small,
			min:   nil,
			max:   &large,
			want:  true,
		},
		{
			name:  "no max constraint",
			value: large,
			min:   &small,
			max:   nil,
			want:  true,
		},
		{
			name:  "no constraints",
			value: medium,
			min:   nil,
			max:   nil,
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := checkResourceBounds(tt.value, tt.min, tt.max); got != tt.want {
				t.Errorf("checkResourceBounds() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchesArchitecture(t *testing.T) {
	amd64Req := scheduling.NewRequirements(
		scheduling.NewRequirement(corev1.LabelArchStable, corev1.NodeSelectorOpIn, "amd64"),
	)

	multiArchReq := scheduling.NewRequirements(
		scheduling.NewRequirement(corev1.LabelArchStable, corev1.NodeSelectorOpIn, "amd64", "arm64"),
	)

	tests := []struct {
		name         string
		requirements scheduling.Requirements
		arch         string
		want         bool
	}{
		{
			name:         "match amd64",
			requirements: amd64Req,
			arch:         "amd64",
			want:         true,
		},
		{
			name:         "no match arm64",
			requirements: amd64Req,
			arch:         "arm64",
			want:         false,
		},
		{
			name:         "empty arch allows all",
			requirements: amd64Req,
			arch:         "",
			want:         true,
		},
		{
			name:         "multi-arch match first",
			requirements: multiArchReq,
			arch:         "amd64",
			want:         true,
		},
		{
			name:         "multi-arch match second",
			requirements: multiArchReq,
			arch:         "arm64",
			want:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesArchitecture(tt.requirements, tt.arch); got != tt.want {
				t.Errorf("matchesArchitecture() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildInstanceRequirements(t *testing.T) {
	tests := []struct {
		name                   string
		instanceName           string
		hasGPU                 bool
		wantLabelsCount        int
		wantHasGPURequirements bool
	}{
		{
			name:                   "standard instance",
			instanceName:           "standard.medium",
			hasGPU:                 false,
			wantLabelsCount:        4,
			wantHasGPURequirements: false,
		},
		{
			name:                   "gpu instance",
			instanceName:           "gpu.large",
			hasGPU:                 true,
			wantLabelsCount:        6,
			wantHasGPURequirements: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildInstanceRequirements(tt.instanceName, tt.hasGPU)

			if len(got) != tt.wantLabelsCount {
				t.Errorf("Requirements count = %v, want %v", len(got), tt.wantLabelsCount)
			}

			_, hasGPUCount := got["karpenter.sh/instance-gpu-count"]
			_, hasAccelerator := got["karpenter.sh/instance-accelerator"]
			hasGPURequirements := hasGPUCount || hasAccelerator

			if hasGPURequirements != tt.wantHasGPURequirements {
				t.Errorf("Has GPU requirements = %v, want %v", hasGPURequirements, tt.wantHasGPURequirements)
			}

			if !got.Get(corev1.LabelInstanceTypeStable).Has(tt.instanceName) {
				t.Errorf("Instance type requirement missing or incorrect")
			}

			if !got.Get(corev1.LabelArchStable).Has("amd64") {
				t.Errorf("Architecture requirement missing or incorrect")
			}
		})
	}
}
