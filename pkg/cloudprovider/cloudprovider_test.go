package cloudprovider

import (
	"testing"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
)

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
