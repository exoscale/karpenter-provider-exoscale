package v1

import (
	"slices"
)

// InstanceData represents the required instance data for drift detection
type InstanceData struct {
	TemplateID         string
	SecurityGroups     []string
	PrivateNetworks    []string
	AntiAffinityGroups []string
}

// HasTemplateDrifted checks if the instance template has drifted from the NodeClass spec
func (in *ExoscaleNodeClass) HasTemplateDrifted(instanceData *InstanceData) (bool, string) {
	if instanceData.TemplateID == "" {
		return false, ""
	}

	if instanceData.TemplateID != in.Spec.TemplateID {
		return true, "TemplateID"
	}

	return false, ""
}

// compareStringSlices compares two string slices for equality (order-independent)
func compareStringSlices(expected, actual []string) bool {
	if len(expected) != len(actual) {
		return false
	}

	expectedCopy := slices.Clone(expected)
	actualCopy := slices.Clone(actual)

	slices.Sort(expectedCopy)
	slices.Sort(actualCopy)

	return slices.Equal(expectedCopy, actualCopy)
}

func (in *ExoscaleNodeClass) HasSecurityGroupsDrifted(instanceData *InstanceData) (bool, string) {
	if !compareStringSlices(in.Spec.SecurityGroups, instanceData.SecurityGroups) {
		return true, "SecurityGroups"
	}
	return false, ""
}

func (in *ExoscaleNodeClass) HasPrivateNetworksDrifted(instanceData *InstanceData) (bool, string) {
	if !compareStringSlices(in.Spec.PrivateNetworks, instanceData.PrivateNetworks) {
		return true, "PrivateNetworks"
	}
	return false, ""
}

func (in *ExoscaleNodeClass) HasAntiAffinityGroupsDrifted(instanceData *InstanceData) (bool, string) {
	if !compareStringSlices(in.Spec.AntiAffinityGroups, instanceData.AntiAffinityGroups) {
		return true, "AntiAffinityGroups"
	}
	return false, ""
}
