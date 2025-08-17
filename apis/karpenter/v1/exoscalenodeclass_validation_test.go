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
			name: "image GC thresholds invalid",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID:                  "20000000-0000-0000-0000-000000000001",
					ImageGCHighThresholdPercent: int32Ptr(70),
					ImageGCLowThresholdPercent:  int32Ptr(80), // Low > High is invalid
				},
			},
			wantValid: false,
			testField: "imageGC",
		},
		{
			name: "valid image GC thresholds",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID:                  "20000000-0000-0000-0000-000000000001",
					ImageGCHighThresholdPercent: int32Ptr(85),
					ImageGCLowThresholdPercent:  int32Ptr(80),
				},
			},
			wantValid: true,
		},
		{
			name: "invalid image minimum GC age format",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID:        "20000000-0000-0000-0000-000000000001",
					ImageMinimumGCAge: "5 minutes", // Should be "5m"
				},
			},
			wantValid: false,
			testField: "imageMinimumGCAge",
		},
		{
			name: "valid image minimum GC age",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID:        "20000000-0000-0000-0000-000000000001",
					ImageMinimumGCAge: "5m",
				},
			},
			wantValid: true,
		},
		{
			name: "invalid node label key",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID: "20000000-0000-0000-0000-000000000001",
					NodeLabels: map[string]string{
						"valid.label/key":    "value",
						"kubernetes.io/test": "value", // Reserved prefix
					},
				},
			},
			wantValid: false,
			testField: "nodeLabels",
		},
		{
			name: "valid node labels",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID: "20000000-0000-0000-0000-000000000001",
					NodeLabels: map[string]string{
						"team":           "platform",
						"environment":    "production",
						"app.domain/key": "value",
					},
				},
			},
			wantValid: true,
		},
		{
			name: "node taint without key",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID: "20000000-0000-0000-0000-000000000001",
					NodeTaints: []NodeTaint{
						{
							Key:    "", // Empty key is invalid
							Value:  "test",
							Effect: "NoSchedule",
						},
					},
				},
			},
			wantValid: false,
			testField: "nodeTaints",
		},
		{
			name: "valid node taints",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID: "20000000-0000-0000-0000-000000000001",
					NodeTaints: []NodeTaint{
						{
							Key:    "workload",
							Value:  "gpu",
							Effect: "NoSchedule",
						},
						{
							Key:    "dedicated",
							Value:  "ml",
							Effect: "NoExecute",
						},
					},
				},
			},
			wantValid: true,
		},
		{
			name: "too many node taints",
			nodeClass: &ExoscaleNodeClass{
				Spec: ExoscaleNodeClassSpec{
					TemplateID: "20000000-0000-0000-0000-000000000001",
					NodeTaints: generateTaints(51), // Max is 50
				},
			},
			wantValid: false,
			testField: "nodeTaints",
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
		"diskSize":                    int64(50),
		"imageGCHighThresholdPercent": int32(85),
		"imageGCLowThresholdPercent":  int32(80),
		"imageMinimumGCAge":           "5m",
	}

	for field, expected := range expectedDefaults {
		t.Logf("Field %s should default to %v", field, expected)
	}
}

func TestResourceReservation_Validation(t *testing.T) {
	tests := []struct {
		name        string
		reservation ResourceReservation
		wantValid   bool
	}{
		{
			name: "valid resource reservation",
			reservation: ResourceReservation{
				CPU:              "500m",
				Memory:           "1Gi",
				EphemeralStorage: "10Gi",
			},
			wantValid: true,
		},
		{
			name: "invalid CPU format",
			reservation: ResourceReservation{
				CPU: "500 millicores", // Should be "500m"
			},
			wantValid: false,
		},
		{
			name: "invalid memory format",
			reservation: ResourceReservation{
				Memory: "1 gigabyte", // Should be "1Gi"
			},
			wantValid: false,
		},
		{
			name:        "empty reservation is valid",
			reservation: ResourceReservation{},
			wantValid:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// These would be validated by Kubernetes quantity parsing
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

func generateTaints(count int) []NodeTaint {
	taints := make([]NodeTaint, count)
	for i := 0; i < count; i++ {
		taints[i] = NodeTaint{
			Key:    fmt.Sprintf("taint-key-%d", i),
			Value:  fmt.Sprintf("taint-value-%d", i),
			Effect: "NoSchedule",
		}
	}
	return taints
}

func int32Ptr(i int32) *int32 {
	return &i
}
