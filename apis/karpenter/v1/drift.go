package v1

import (
	"slices"
)

type InstanceData struct {
	TemplateID         string
	SecurityGroups     []string
	PrivateNetworks    []string
	AntiAffinityGroups []string
}

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

func (in *ExoscaleNodeClass) HasTemplateIDDrifted(expectedImageID string, instanceData *InstanceData) (bool, string) {
	if expectedImageID != "" && instanceData.TemplateID != expectedImageID {
		return true, "TemplateID"
	}
	return false, ""
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
