package utils

import (
	"fmt"
	"net"
	"regexp"
	"slices"
	"strings"

	v1 "k8s.io/api/core/v1"
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

func ContainsIPv6Address(addresses []v1.NodeAddress) bool {
	return slices.ContainsFunc(addresses, func(addr v1.NodeAddress) bool {
		ip := net.ParseIP(addr.Address)
		return ip != nil && ip.To4() == nil
	})
}
