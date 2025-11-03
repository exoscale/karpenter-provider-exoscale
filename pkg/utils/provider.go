package utils

import (
	"fmt"
	"regexp"
	"strings"
)

const ExoscaleProviderIDPrefix = "exoscale://"

var uuidRegex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func ParseProviderID(providerID string) (string, error) {
	instanceID, found := strings.CutPrefix(providerID, ExoscaleProviderIDPrefix)
	if !found {
		return "", fmt.Errorf("invalid provider ID format: %s (must start with %s)", providerID, ExoscaleProviderIDPrefix)
	}

	if instanceID == "" {
		return "", fmt.Errorf("invalid provider ID format: %s (missing instance ID)", providerID)
	}

	if !uuidRegex.MatchString(instanceID) {
		return "", fmt.Errorf("invalid provider ID format: %s (instance ID must be a valid UUID)", providerID)
	}

	return instanceID, nil
}
