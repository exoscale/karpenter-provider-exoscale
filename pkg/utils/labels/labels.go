// Package labels provides helpers to build and encode the labels filter
// used to scope Exoscale API calls (e.g. ListInstances) to the resources
// owned by this Karpenter provider.
package labels

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
)

// KarpenterFilter returns the canonical labels that identify instances
// managed by the Karpenter provider for the given cluster ID.
func KarpenterFilter(clusterID string) map[string]string {
	return map[string]string{
		constants.InstanceLabelManagedBy: constants.ManagedByKarpenter,
		constants.InstanceLabelClusterID: clusterID,
	}
}

// EncodeFilter renders a labels map into the URL-encoded JSON form expected
// by the Exoscale API `labels` query parameter.
//
// An empty map yields an empty string, which omits the filter.
func EncodeFilter(labels map[string]string) (string, error) {
	if len(labels) == 0 {
		return "", nil
	}

	raw, err := json.Marshal(labels)
	if err != nil {
		return "", fmt.Errorf("marshal labels: %w", err)
	}

	return url.QueryEscape(string(raw)), nil
}
