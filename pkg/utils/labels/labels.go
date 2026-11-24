// Package labels builds the Exoscale API `labels` filter that scopes calls
// such as ListInstances to resources owned by this provider.
package labels

import (
	"fmt"
	"strings"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
)

// KarpenterFilter returns the space-separated `k=v` filter matching instances
// managed by Karpenter for clusterID.
func KarpenterFilter(clusterID string) (string, error) {
	if strings.ContainsAny(clusterID, " \n\t") {
		return "", fmt.Errorf("cluster ID %q contains whitespace", clusterID)
	}
	return constants.InstanceLabelManagedBy + "=" + constants.ManagedByKarpenter + " " +
		constants.InstanceLabelClusterID + "=" + clusterID, nil
}
