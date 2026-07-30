// Package labels provides helpers to build and encode the labels filter
// used to scope Exoscale API calls (e.g. ListInstances) to the resources
// owned by this Karpenter provider.
package labels

import (
	"fmt"
	"strings"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
)

// KarpenterFilter returns the URL-encoded `k=v k2=v2` form of the
// canonical labels that identify instances managed by the Karpenter
// provider for the given cluster ID, ready to be passed to the Exoscale
// API `labels` query parameter.
//
// An empty cluster ID yields an empty string, which omits the filter.
func KarpenterFilter(clusterID string) (string, error) {
	labels := map[string]string{
		constants.InstanceLabelManagedBy: constants.ManagedByKarpenter,
		constants.InstanceLabelClusterID: clusterID,
	}
	return encode(labels)
}

// encode renders a labels map into the URL-encoded `k=v k2=v2` form
// expected by the Exoscale API `labels` query parameter.
//
// Pairs are joined with a single space and the resulting string is then
// URL-escaped so it can be appended directly to a query.
//
// An empty map yields an empty string, which omits the filter.
func encode(labels map[string]string) (string, error) {
	if len(labels) == 0 {
		return "", nil
	}

	pairs := make([]string, 0, len(labels))
	for k, v := range labels {
		if strings.ContainsAny(v, " \n\t") {
			return "", fmt.Errorf("label value for %q contains an unescaped whitespace", k)
		}
		if strings.Contains(k, " ") {
			return "", fmt.Errorf("label key %q contains an unescaped whitespace", k)
		}
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
	}

	return strings.Join(pairs, " "), nil
}
