package cloudprovider_test

import (
	"errors"
	"testing"

	egov3 "github.com/exoscale/egoscale/v3"
	internaltesting "github.com/exoscale/karpenter-exoscale/internal/testing"
	"github.com/exoscale/karpenter-exoscale/internal/testing/mocks"
	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instance"
	"github.com/exoscale/karpenter-exoscale/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	karpentercloudprovider "sigs.k8s.io/karpenter/pkg/cloudprovider"
)

func createBaseInstance() *instance.Instance {
	return &instance.Instance{
		ID:   string(mocks.InstanceID1),
		Name: "test-instance",
		Template: &egov3.Template{
			ID: mocks.DefaultTemplateID,
		},
		InstanceType: &egov3.InstanceType{
			ID:     mocks.StandardMediumTypeID,
			Family: "standard",
			Size:   "small",
		},
		SecurityGroups:     []string{string(mocks.DefaultSecurityGroupID)},
		PrivateNetworks:    []string{string(mocks.PrivateNetworkID1)},
		AntiAffinityGroups: []string{string(mocks.DefaultAntiAffinityGroupID)},
		Zone:               "ch-gva-2",
		Labels: map[string]string{
			constants.LabelManagedBy:   constants.ManagedByKarpenter,
			constants.LabelClusterName: "test-cluster",
			constants.LabelNodeClaim:   "test-node-claim",
		},
	}
}

func setupDriftInstanceGetMock(env *internaltesting.CloudProviderTestEnvironment, inst *instance.Instance, err error) {
	env.MockInstanceProvider.On("Get", mock.Anything, string(mocks.InstanceID1)).
		Return(inst, err)
}

type driftModifier func(*instance.Instance)

func withTemplateDrift(templateID string) driftModifier {
	return func(inst *instance.Instance) {
		if templateID == "" {
			inst.Template = nil
		} else {
			inst.Template.ID = egov3.UUID(templateID)
		}
	}
}

func withSecurityGroupsDrift(groups []string) driftModifier {
	return func(inst *instance.Instance) {
		inst.SecurityGroups = groups
	}
}

func withPrivateNetworksDrift(networks []string) driftModifier {
	return func(inst *instance.Instance) {
		inst.PrivateNetworks = networks
	}
}

func withAntiAffinityGroupsDrift(groups []string) driftModifier {
	return func(inst *instance.Instance) {
		inst.AntiAffinityGroups = groups
	}
}

func TestCloudProvider_IsDrifted(t *testing.T) {
	tests := []struct {
		name            string
		setupNodeClass  bool
		driftModifiers  []driftModifier
		instanceError   error
		invalidProvider bool
		expectedReason  karpentercloudprovider.DriftReason
		expectedError   string
	}{
		{"NoDrift", true, nil, nil, false, "", ""},
		{"NodeClassNotFound", false, nil, nil, false, "", "failed to get node class"},
		{"InvalidProviderID", true, nil, nil, true, "", "failed to parse provider ID"},
		{"InstanceNotFound", true, nil, errors.New("instance not found"), false, "", "failed to get instance"},
		{"TemplateDrift", true, []driftModifier{withTemplateDrift("different-template-id")}, nil, false, "TemplateID", ""},
		{"SecurityGroupsDrift", true, []driftModifier{withSecurityGroupsDrift([]string{"different-sg-id"})}, nil, false, "SecurityGroups", ""},
		{"PrivateNetworksDrift", true, []driftModifier{withPrivateNetworksDrift([]string{"different-network-id"})}, nil, false, "PrivateNetworks", ""},
		{"AntiAffinityGroupsDrift", true, []driftModifier{withAntiAffinityGroupsDrift([]string{"different-aag-id"})}, nil, false, "AntiAffinityGroups", ""},
		{"MultipleDriftTypes", true, []driftModifier{withTemplateDrift("different-template-id"), withSecurityGroupsDrift([]string{"different-sg-id"})}, nil, false, "TemplateID", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := internaltesting.SetupCloudProviderTestEnvironment(t)

			if tt.setupNodeClass {
				nodeClass := mocks.CreateNodeClass("test-nodeclass", "standard")
				nodeClass.Spec.TemplateID = string(mocks.DefaultTemplateID)
				nodeClass.Spec.SecurityGroups = []string{string(mocks.DefaultSecurityGroupID)}
				nodeClass.Spec.PrivateNetworks = []string{string(mocks.PrivateNetworkID1)}
				nodeClass.Spec.AntiAffinityGroups = []string{string(mocks.DefaultAntiAffinityGroupID)}
				require.NoError(t, env.KubeClient.Create(env.Ctx, nodeClass))
			}

			nodeClaim := mocks.CreateNodeClaim("test-claim", "test-nodeclass", "small-instance")
			nodeClaim.Status.ProviderID = utils.FormatProviderID(string(mocks.InstanceID1))
			if tt.invalidProvider {
				nodeClaim.Status.ProviderID = "invalid-provider-id"
			}

			if !tt.invalidProvider && tt.expectedError != "failed to get node class" {
				testInstance := createBaseInstance()
				for _, modifier := range tt.driftModifiers {
					modifier(testInstance)
				}
				setupDriftInstanceGetMock(env, testInstance, tt.instanceError)
			}

			reason, err := env.CloudProvider.IsDrifted(env.Ctx, nodeClaim)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedReason, reason)
			}
		})
	}
}

func TestToStringSet(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected map[string]bool
	}{
		{"Empty", []string{}, map[string]bool{}},
		{"Single", []string{"item-1"}, map[string]bool{"item-1": true}},
		{"Multiple", []string{"item-1", "item-2", "item-3"}, map[string]bool{"item-1": true, "item-2": true, "item-3": true}},
		{"Duplicate", []string{"item-1", "item-2", "item-1"}, map[string]bool{"item-1": true, "item-2": true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, utils.ToStringSet(tt.input))
		})
	}
}

func TestToStringSetFiltered(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected map[string]bool
	}{
		{"Empty", []string{}, map[string]bool{}},
		{"Single", []string{"item-1"}, map[string]bool{"item-1": true}},
		{"Multiple", []string{"item-1", "item-2", "item-3"}, map[string]bool{"item-1": true, "item-2": true, "item-3": true}},
		{"WithEmpty", []string{"item-1", "", "item-2", ""}, map[string]bool{"item-1": true, "item-2": true}},
		{"OnlyEmpty", []string{"", "", ""}, map[string]bool{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, utils.ToStringSetFiltered(tt.input))
		})
	}
}

func TestCompareSets(t *testing.T) {
	tests := []struct {
		name     string
		expected map[string]bool
		actual   map[string]bool
		equal    bool
	}{
		{"BothEmpty", map[string]bool{}, map[string]bool{}, true},
		{"Identical", map[string]bool{"a": true, "b": true}, map[string]bool{"a": true, "b": true}, true},
		{"DifferentOrder", map[string]bool{"a": true, "b": true, "c": true}, map[string]bool{"c": true, "a": true, "b": true}, true},
		{"ExpectedEmpty", map[string]bool{}, map[string]bool{"a": true}, false},
		{"ActualEmpty", map[string]bool{"a": true}, map[string]bool{}, false},
		{"Different", map[string]bool{"a": true, "b": true}, map[string]bool{"a": true, "c": true}, false},
		{"ExtraInActual", map[string]bool{"a": true}, map[string]bool{"a": true, "b": true}, false},
		{"MissingInActual", map[string]bool{"a": true, "b": true}, map[string]bool{"a": true}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.equal, utils.CompareSets(tt.expected, tt.actual))
		})
	}
}
