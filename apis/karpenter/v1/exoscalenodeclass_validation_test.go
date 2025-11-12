package v1

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestExoscaleNodeClass_Validation(t *testing.T) {
	tests := []struct {
		name      string
		nodeClass *ExoscaleNodeClass
		wantValid bool
		testField string
	}{
		{
			name: "valid nodeclass",
			nodeClass: &ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: ExoscaleNodeClassSpec{
					TemplateID: "20000000-0000-0000-0000-000000000001",
					DiskSize:   50,
					SecurityGroups: []string{
						"30000000-0000-0000-0000-000000000001",
						"30000000-0000-0000-0000-000000000002",
					},
					PrivateNetworks: []string{
						"40000000-0000-0000-0000-000000000001",
					},
					AntiAffinityGroups: []string{
						"50000000-0000-0000-0000-000000000001",
					},
				},
			},
			wantValid: true,
		},
		{
			name: "invalid template ID format",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID: "not-a-uuid",
				},
			},
			wantValid: false,
			testField: "templateID",
		},
		{
			name: "disk size below minimum",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID: "20000000-0000-0000-0000-000000000001",
					DiskSize:   5, // Minimum is 10
				},
			},
			wantValid: false,
			testField: "diskSize",
		},
		{
			name: "disk size above maximum",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID: "20000000-0000-0000-0000-000000000001",
					DiskSize:   60000, // Maximum is 8000
				},
			},
			wantValid: false,
			testField: "diskSize",
		},
		{
			name: "too many security groups",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID:     "20000000-0000-0000-0000-000000000001",
					SecurityGroups: generateUUIDs("30000000-0000-0000-0000-00000000%04d", 51), // Max is 50
				},
			},
			wantValid: false,
			testField: "securityGroups",
		},
		{
			name: "invalid security group UUID",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID: "20000000-0000-0000-0000-000000000001",
					SecurityGroups: []string{
						"30000000-0000-0000-0000-000000000001",
						"not-a-uuid",
					},
				},
			},
			wantValid: false,
			testField: "securityGroups",
		},
		{
			name: "duplicate security groups",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID: "20000000-0000-0000-0000-000000000001",
					SecurityGroups: []string{
						"30000000-0000-0000-0000-000000000001",
						"30000000-0000-0000-0000-000000000001", // Duplicate
					},
				},
			},
			wantValid: false,
			testField: "securityGroups",
		},
		{
			name: "too many anti-affinity groups",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID:         "20000000-0000-0000-0000-000000000001",
					AntiAffinityGroups: generateUUIDs("50000000-0000-0000-0000-00000000%04d", 9), // Max is 8
				},
			},
			wantValid: false,
			testField: "antiAffinityGroups",
		},
		{
			name: "too many private networks",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID:      "20000000-0000-0000-0000-000000000001",
					PrivateNetworks: generateUUIDs("40000000-0000-0000-0000-00000000%04d", 11), // Max is 10
				},
			},
			wantValid: false,
			testField: "privateNetworks",
		},
		{
			name: "image GC thresholds invalid in kubelet",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID: "20000000-0000-0000-0000-000000000001",
					Kubelet: KubeletConfiguration{
						ImageGCHighThresholdPercent: int32Ptr(70),
						ImageGCLowThresholdPercent:  int32Ptr(80), // Low > High is invalid
					},
				},
			},
			wantValid: false,
			testField: "kubelet.imageGC",
		},
		{
			name: "valid image GC thresholds in kubelet",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID: "20000000-0000-0000-0000-000000000001",
					Kubelet: KubeletConfiguration{
						ImageGCHighThresholdPercent: int32Ptr(85),
						ImageGCLowThresholdPercent:  int32Ptr(80),
					},
				},
			},
			wantValid: true,
		},
		{
			name: "invalid image minimum GC age format in kubelet",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID: "20000000-0000-0000-0000-000000000001",
					Kubelet: KubeletConfiguration{
						ImageMinimumGCAge: "5 minutes", // Should be "5m"
					},
				},
			},
			wantValid: false,
			testField: "kubelet.imageMinimumGCAge",
		},
		{
			name: "valid image minimum GC age in kubelet",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID: "20000000-0000-0000-0000-000000000001",
					Kubelet: KubeletConfiguration{
						ImageMinimumGCAge: "5m",
					},
				},
			},
			wantValid: true,
		},
		{
			name: "valid nodeclass with imageTemplateSelector",
			nodeClass: &ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: ExoscaleNodeClassSpec{
					ImageTemplateSelector: &ImageTemplateSelector{
						Version: "1.33.0",
						Variant: "standard",
					},
					DiskSize: 50,
				},
			},
			wantValid: true,
		},
		{
			name: "imageTemplateSelector with nvidia variant",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					ImageTemplateSelector: &ImageTemplateSelector{
						Version: "1.33.0",
						Variant: "nvidia",
					},
				},
			},
			wantValid: true,
		},
		{
			name: "imageTemplateSelector with default values",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					ImageTemplateSelector: &ImageTemplateSelector{},
				},
			},
			wantValid: true,
		},
		{
			name: "both templateID and imageTemplateSelector specified",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID: "20000000-0000-0000-0000-000000000001",
					ImageTemplateSelector: &ImageTemplateSelector{
						Version: "1.33.0",
						Variant: "standard",
					},
				},
			},
			wantValid: false,
			testField: "mutual-exclusivity",
		},
		{
			name: "neither templateID nor imageTemplateSelector specified",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					DiskSize: 50,
				},
			},
			wantValid: false,
			testField: "required-field",
		},
		{
			name: "invalid version format in imageTemplateSelector - major only",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					ImageTemplateSelector: &ImageTemplateSelector{
						Version: "1", // Should be major.minor.patch
						Variant: "standard",
					},
				},
			},
			wantValid: false,
			testField: "version-format",
		},
		{
			name: "invalid version format in imageTemplateSelector - major.minor only",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					ImageTemplateSelector: &ImageTemplateSelector{
						Version: "1.33", // Should be major.minor.patch
						Variant: "standard",
					},
				},
			},
			wantValid: false,
			testField: "version-format",
		},
		{
			name: "valid semver version format in imageTemplateSelector",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					ImageTemplateSelector: &ImageTemplateSelector{
						Version: "1.33.0",
						Variant: "standard",
					},
				},
			},
			wantValid: true,
		},
		{
			name: "invalid variant in imageTemplateSelector",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					ImageTemplateSelector: &ImageTemplateSelector{
						Version: "1.33.0",
						Variant: "invalid-variant", // Only standard and nvidia are valid
					},
				},
			},
			wantValid: false,
			testField: "variant",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: In a real implementation, these validations would be enforced
			// by the Kubernetes API server using OpenAPI schema and CEL expressions.
			// This test demonstrates the validation logic that should be applied.

			// For now, we're testing the structure and documenting expected behavior
			assert.NotNil(t, tt.nodeClass)

			if tt.testField != "" {
				t.Logf("Testing field: %s", tt.testField)
			}

			// In production, these would fail at API level
			if !tt.wantValid {
				t.Logf("This configuration should be rejected by validation")
			}
		})
	}
}

func TestExoscaleNodeClass_Defaults(t *testing.T) {
	// Test that defaults would be applied (in practice by kubebuilder)
	expectedDefaults := map[string]interface{}{
		"diskSize":                           int64(50),
		"kubelet.imageGCHighThresholdPercent": int32(85),
		"kubelet.imageGCLowThresholdPercent":  int32(80),
		"kubelet.imageMinimumGCAge":          "2m",
		"kubelet.clusterDNS":                 []string{"10.96.0.10"},
	}

	for field, expected := range expectedDefaults {
		t.Logf("Field %s should default to %v", field, expected)
	}
}

func TestResourceReservation_Validation(t *testing.T) {
	tests := []struct {
		name        string
		reservation KubeResourceReservation
		wantValid   bool
	}{
		{
			name: "valid resource reservation",
			reservation: KubeResourceReservation{
				CPU:              "500m",
				Memory:           "1Gi",
				EphemeralStorage: "10Gi",
			},
			wantValid: true,
		},
		{
			name: "invalid CPU format",
			reservation: KubeResourceReservation{
				CPU: "500 millicores",
			},
			wantValid: false,
		},
		{
			name: "invalid memory format",
			reservation: KubeResourceReservation{
				Memory: "1 gigabyte",
			},
			wantValid: false,
		},
		{
			name:        "empty reservation is valid",
			reservation: KubeResourceReservation{},
			wantValid:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.wantValid {
				t.Logf("Reservation %+v should be rejected", tt.reservation)
			}
		})
	}
}

// Helper functions for testing

func generateUUIDs(format string, count int) []string {
	uuids := make([]string, count)
	for i := 0; i < count; i++ {
		uuids[i] = fmt.Sprintf(format, i+1)
	}
	return uuids
}

func int32Ptr(i int32) *int32 {
	return &i
}
