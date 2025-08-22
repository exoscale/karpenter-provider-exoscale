package v1_test

import (
	"testing"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/stretchr/testify/assert"
)

func TestExoscaleNodeClass_HasTemplateDrifted(t *testing.T) {
	tests := []struct {
		name           string
		nodeClassSpec  string
		instanceData   *apiv1.InstanceData
		expectedDrift  bool
		expectedReason string
	}{
		{
			name:          "No drift - same template",
			nodeClassSpec: "template-123",
			instanceData: &apiv1.InstanceData{
				TemplateID: "template-123",
			},
			expectedDrift:  false,
			expectedReason: "",
		},
		{
			name:          "Drift detected - different template",
			nodeClassSpec: "template-123",
			instanceData: &apiv1.InstanceData{
				TemplateID: "template-456",
			},
			expectedDrift:  true,
			expectedReason: "TemplateID",
		},
		{
			name:          "No drift - empty template ID",
			nodeClassSpec: "template-123",
			instanceData: &apiv1.InstanceData{
				TemplateID: "",
			},
			expectedDrift:  false,
			expectedReason: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nc := &apiv1.ExoscaleNodeClass{
				Spec: apiv1.ExoscaleNodeClassSpec{
					TemplateID: tt.nodeClassSpec,
				},
			}

			drifted, reason := nc.HasTemplateDrifted(tt.instanceData)

			assert.Equal(t, tt.expectedDrift, drifted)
			assert.Equal(t, tt.expectedReason, reason)
		})
	}
}

func TestExoscaleNodeClass_HasSecurityGroupsDrifted(t *testing.T) {
	tests := []struct {
		name           string
		nodeClassSpec  []string
		instanceData   *apiv1.InstanceData
		expectedDrift  bool
		expectedReason string
	}{
		{
			name:          "No drift - same security groups",
			nodeClassSpec: []string{"sg-1", "sg-2"},
			instanceData: &apiv1.InstanceData{
				SecurityGroups: []string{"sg-1", "sg-2"},
			},
			expectedDrift:  false,
			expectedReason: "",
		},
		{
			name:          "No drift - different order",
			nodeClassSpec: []string{"sg-1", "sg-2"},
			instanceData: &apiv1.InstanceData{
				SecurityGroups: []string{"sg-2", "sg-1"},
			},
			expectedDrift:  false,
			expectedReason: "",
		},
		{
			name:          "Drift detected - different groups",
			nodeClassSpec: []string{"sg-1", "sg-2"},
			instanceData: &apiv1.InstanceData{
				SecurityGroups: []string{"sg-1", "sg-3"},
			},
			expectedDrift:  true,
			expectedReason: "SecurityGroups",
		},
		{
			name:          "Drift detected - missing group",
			nodeClassSpec: []string{"sg-1", "sg-2"},
			instanceData: &apiv1.InstanceData{
				SecurityGroups: []string{"sg-1"},
			},
			expectedDrift:  true,
			expectedReason: "SecurityGroups",
		},
		{
			name:          "Drift detected - extra group",
			nodeClassSpec: []string{"sg-1"},
			instanceData: &apiv1.InstanceData{
				SecurityGroups: []string{"sg-1", "sg-2"},
			},
			expectedDrift:  true,
			expectedReason: "SecurityGroups",
		},
		{
			name:          "No drift - both empty",
			nodeClassSpec: []string{},
			instanceData: &apiv1.InstanceData{
				SecurityGroups: []string{},
			},
			expectedDrift:  false,
			expectedReason: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nc := &apiv1.ExoscaleNodeClass{
				Spec: apiv1.ExoscaleNodeClassSpec{
					SecurityGroups: tt.nodeClassSpec,
				},
			}

			drifted, reason := nc.HasSecurityGroupsDrifted(tt.instanceData)

			assert.Equal(t, tt.expectedDrift, drifted)
			assert.Equal(t, tt.expectedReason, reason)
		})
	}
}

func TestExoscaleNodeClass_HasPrivateNetworksDrifted(t *testing.T) {
	tests := []struct {
		name           string
		nodeClassSpec  []string
		instanceData   *apiv1.InstanceData
		expectedDrift  bool
		expectedReason string
	}{
		{
			name:          "No drift - same networks",
			nodeClassSpec: []string{"net-1", "net-2"},
			instanceData: &apiv1.InstanceData{
				PrivateNetworks: []string{"net-1", "net-2"},
			},
			expectedDrift:  false,
			expectedReason: "",
		},
		{
			name:          "Drift detected - different networks",
			nodeClassSpec: []string{"net-1", "net-2"},
			instanceData: &apiv1.InstanceData{
				PrivateNetworks: []string{"net-1", "net-3"},
			},
			expectedDrift:  true,
			expectedReason: "PrivateNetworks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nc := &apiv1.ExoscaleNodeClass{
				Spec: apiv1.ExoscaleNodeClassSpec{
					PrivateNetworks: tt.nodeClassSpec,
				},
			}

			drifted, reason := nc.HasPrivateNetworksDrifted(tt.instanceData)

			assert.Equal(t, tt.expectedDrift, drifted)
			assert.Equal(t, tt.expectedReason, reason)
		})
	}
}

func TestExoscaleNodeClass_HasAntiAffinityGroupsDrifted(t *testing.T) {
	tests := []struct {
		name           string
		nodeClassSpec  []string
		instanceData   *apiv1.InstanceData
		expectedDrift  bool
		expectedReason string
	}{
		{
			name:          "No drift - same groups",
			nodeClassSpec: []string{"aag-1", "aag-2"},
			instanceData: &apiv1.InstanceData{
				AntiAffinityGroups: []string{"aag-1", "aag-2"},
			},
			expectedDrift:  false,
			expectedReason: "",
		},
		{
			name:          "Drift detected - different groups",
			nodeClassSpec: []string{"aag-1"},
			instanceData: &apiv1.InstanceData{
				AntiAffinityGroups: []string{"aag-2"},
			},
			expectedDrift:  true,
			expectedReason: "AntiAffinityGroups",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nc := &apiv1.ExoscaleNodeClass{
				Spec: apiv1.ExoscaleNodeClassSpec{
					AntiAffinityGroups: tt.nodeClassSpec,
				},
			}

			drifted, reason := nc.HasAntiAffinityGroupsDrifted(tt.instanceData)

			assert.Equal(t, tt.expectedDrift, drifted)
			assert.Equal(t, tt.expectedReason, reason)
		})
	}
}
