package labels

import (
	"testing"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
)

func TestKarpenterFilter(t *testing.T) {
	got, err := KarpenterFilter("cluster-a-id")
	if err != nil {
		t.Fatal(err)
	}
	want := constants.InstanceLabelManagedBy + "=karpenter " + constants.InstanceLabelClusterID + "=cluster-a-id"
	if got != want {
		t.Errorf("KarpenterFilter() = %q, want %q", got, want)
	}

	if _, err := KarpenterFilter("a b"); err == nil {
		t.Error("KarpenterFilter() accepted a cluster ID containing whitespace")
	}
}
