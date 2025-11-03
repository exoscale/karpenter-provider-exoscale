package cloudprovider

import (
	"testing"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestBuildOverhead(t *testing.T) {
	c := &CloudProvider{}

	nodeClass := &apiv1.ExoscaleNodeClass{
		Spec: apiv1.ExoscaleNodeClassSpec{
			KubeReserved: apiv1.ResourceReservation{
				CPU:              "200m",
				Memory:           "300Mi",
				EphemeralStorage: "1Gi",
			},
			SystemReserved: apiv1.ResourceReservation{
				CPU:              "100m",
				Memory:           "100Mi",
				EphemeralStorage: "3Gi",
			},
		},
	}

	overhead := c.buildOverhead(nodeClass)

	if overhead == nil {
		t.Fatal("buildOverhead() returned nil")
	}

	cpuQty := overhead.KubeReserved[v1.ResourceCPU]
	if cpuQty.String() != "200m" {
		t.Errorf("KubeReserved CPU = %v, want 200m", cpuQty.String())
	}

	memQty := overhead.SystemReserved[v1.ResourceMemory]
	if memQty.String() != "100Mi" {
		t.Errorf("SystemReserved Memory = %v, want 100Mi", memQty.String())
	}
}

func TestApplyOverhead(t *testing.T) {
	nodeClass := &apiv1.ExoscaleNodeClass{
		Spec: apiv1.ExoscaleNodeClassSpec{
			KubeReserved: apiv1.ResourceReservation{
				CPU:    "200m",
				Memory: "300Mi",
			},
			SystemReserved: apiv1.ResourceReservation{
				CPU:    "100m",
				Memory: "100Mi",
			},
		},
	}

	allocatable := v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("2"),
		v1.ResourceMemory: resource.MustParse("4Gi"),
	}

	result := applyOverhead(allocatable, nodeClass)

	cpuQty := result[v1.ResourceCPU]
	if cpuQty.String() != "1700m" {
		t.Errorf("CPU = %v, want 1700m (2000m - 200m - 100m)", cpuQty.String())
	}

	memQty := result[v1.ResourceMemory]
	expectedMem := resource.MustParse("4Gi")
	expectedMem.Sub(resource.MustParse("300Mi"))
	expectedMem.Sub(resource.MustParse("100Mi"))
	if memQty.Cmp(expectedMem) != 0 {
		t.Errorf("Memory = %v, want %v", memQty.String(), expectedMem.String())
	}
}

func TestApplyNodeClassDefaults(t *testing.T) {
	tests := []struct {
		name      string
		nodeClass apiv1.ExoscaleNodeClass
		wantCPU   string
		wantMem   string
	}{
		{
			name: "empty values get defaults",
			nodeClass: apiv1.ExoscaleNodeClass{
				Spec: apiv1.ExoscaleNodeClassSpec{},
			},
			wantCPU: "200m",
			wantMem: "300Mi",
		},
		{
			name: "existing values preserved",
			nodeClass: apiv1.ExoscaleNodeClass{
				Spec: apiv1.ExoscaleNodeClassSpec{
					KubeReserved: apiv1.ResourceReservation{
						CPU:    "500m",
						Memory: "1Gi",
					},
				},
			},
			wantCPU: "500m",
			wantMem: "1Gi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := applyNodeClassDefaults(tt.nodeClass)

			if result.Spec.KubeReserved.CPU != tt.wantCPU {
				t.Errorf("KubeReserved.CPU = %v, want %v", result.Spec.KubeReserved.CPU, tt.wantCPU)
			}
			if result.Spec.KubeReserved.Memory != tt.wantMem {
				t.Errorf("KubeReserved.Memory = %v, want %v", result.Spec.KubeReserved.Memory, tt.wantMem)
			}
		})
	}
}

func TestSubtractReservation(t *testing.T) {
	tests := []struct {
		name        string
		allocatable v1.ResourceList
		reservation apiv1.ResourceReservation
		wantCPU     string
	}{
		{
			name: "subtracts CPU correctly",
			allocatable: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("2"),
				v1.ResourceMemory: resource.MustParse("4Gi"),
			},
			reservation: apiv1.ResourceReservation{
				CPU:    "500m",
				Memory: "1Gi",
			},
			wantCPU: "1500m",
		},
		{
			name: "handles empty reservation",
			allocatable: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("2"),
			},
			reservation: apiv1.ResourceReservation{},
			wantCPU:     "2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := subtractReservation(tt.allocatable, tt.reservation)

			cpuQty := result[v1.ResourceCPU]
			if cpuQty.String() != tt.wantCPU {
				t.Errorf("CPU = %v, want %v", cpuQty.String(), tt.wantCPU)
			}
		})
	}
}
