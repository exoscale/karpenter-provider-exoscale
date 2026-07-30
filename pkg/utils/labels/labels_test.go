package labels

import (
	"net/url"
	"testing"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
)

func TestKarpenterFilter(t *testing.T) {
	clusterID := "cluster-a-id"
	got := KarpenterFilter(clusterID)

	expected := map[string]string{
		constants.InstanceLabelManagedBy: constants.ManagedByKarpenter,
		constants.InstanceLabelClusterID: clusterID,
	}

	if len(got) != len(expected) {
		t.Fatalf("KarpenterFilter() returned %d entries, want %d", len(got), len(expected))
	}
	for k, v := range expected {
		if got[k] != v {
			t.Errorf("KarpenterFilter()[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestEncodeFilter(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]string
		want    string
		wantErr bool
	}{
		{
			name:  "empty map",
			input: nil,
			want:  "",
		},
		{
			name:  "single key",
			input: map[string]string{"foo": "bar"},
			want:  url.QueryEscape(`{"foo":"bar"}`),
		},
		{
			name: "multiple keys with special characters",
			input: map[string]string{
				"exoscale.com/managed-by": "karpenter",
				"exoscale.com/cluster-id": "cluster/with spaces",
			},
			want: url.QueryEscape(`{"exoscale.com/cluster-id":"cluster/with spaces","exoscale.com/managed-by":"karpenter"}`),
		},
		{
			name:  "values with quotes and unicode",
			input: map[string]string{"k": `a"b\u00e9`},
			want:  url.QueryEscape(`{"k":"a\"b\\u00e9"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncodeFilter(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("EncodeFilter() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("EncodeFilter() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEncodeFilter_KarpenterFilterIsRoundTripSafe(t *testing.T) {
	encoded, err := EncodeFilter(KarpenterFilter("cluster-a-id"))
	if err != nil {
		t.Fatalf("EncodeFilter() unexpected error: %v", err)
	}

	decoded, err := url.QueryUnescape(encoded)
	if err != nil {
		t.Fatalf("QueryUnescape() unexpected error: %v", err)
	}

	if decoded != `{"exoscale.com/cluster-id":"cluster-a-id","exoscale.com/managed-by":"karpenter"}` {
		t.Errorf("decoded filter = %q, want exact JSON", decoded)
	}
}
