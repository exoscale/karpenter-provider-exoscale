package labels

import (
	"net/url"
	"strings"
	"testing"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
)

func TestKarpenterFilter(t *testing.T) {
	clusterID := "cluster-a-id"
	got, err := KarpenterFilter(clusterID)
	if err != nil {
		t.Fatalf("KarpenterFilter() unexpected error = %v", err)
	}

	decoded, err := url.QueryUnescape(got)
	if err != nil {
		t.Fatalf("QueryUnescape() unexpected error = %v", err)
	}

	pairs := strings.Split(decoded, " ")
	if len(pairs) != 2 {
		t.Fatalf("decoded filter has %d pairs, want 2 (raw=%q)", len(pairs), decoded)
	}

	gotMap := make(map[string]string, len(pairs))
	for _, p := range pairs {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			t.Fatalf("decoded pair %q is not k=v form", p)
		}
		gotMap[kv[0]] = kv[1]
	}

	want := map[string]string{
		constants.InstanceLabelManagedBy: constants.ManagedByKarpenter,
		constants.InstanceLabelClusterID: clusterID,
	}
	if len(gotMap) != len(want) {
		t.Fatalf("decoded filter has %d entries, want %d", len(gotMap), len(want))
	}
	for k, v := range want {
		if gotMap[k] != v {
			t.Errorf("decoded[%q] = %q, want %q", k, gotMap[k], v)
		}
	}
}
