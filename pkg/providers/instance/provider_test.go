package instance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/egoscale/v3/credentials"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func TestConvertSecurityGroups(t *testing.T) {
	p := &Provider{}
	ids := []string{"sg-1", "sg-2", "sg-3"}

	got := p.convertSecurityGroups(ids)

	if len(got) != len(ids) {
		t.Errorf("convertSecurityGroups() len = %d, want %d", len(got), len(ids))
	}

	for i, id := range ids {
		if string(got[i].ID) != id {
			t.Errorf("convertSecurityGroups()[%d] = %v, want %v", i, got[i].ID, id)
		}
	}
}

func TestConvertAntiAffinityGroups(t *testing.T) {
	p := &Provider{}
	ids := []string{"aag-1", "aag-2"}

	got := p.convertAntiAffinityGroups(ids)

	if len(got) != len(ids) {
		t.Errorf("convertAntiAffinityGroups() len = %d, want %d", len(got), len(ids))
	}

	for i, id := range ids {
		if string(got[i].ID) != id {
			t.Errorf("convertAntiAffinityGroups()[%d] = %v, want %v", i, got[i].ID, id)
		}
	}
}

func TestIsNotFoundError(t *testing.T) {
	p := &Provider{}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "not found error",
			err:  egov3.ErrNotFound,
			want: true,
		},
		{
			name: "other error",
			err:  errors.New("some other error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.IsNotFoundError(tt.err)
			if got != tt.want {
				t.Errorf("isNotFoundError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindCheapestInstanceType(t *testing.T) {
	tests := []struct {
		name          string
		instanceTypes []string
		prices        map[string]float64
		want          string
	}{
		{
			name:          "single instance type",
			instanceTypes: []string{"standard.medium"},
			prices:        map[string]float64{"standard.medium": 0.05},
			want:          "standard.medium",
		},
		{
			name:          "cheapest instance type",
			instanceTypes: []string{"standard.medium", "standard.large", "standard.small"},
			prices: map[string]float64{
				"standard.medium": 0.05,
				"standard.large":  0.10,
				"standard.small":  0.03,
			},
			want: "standard.small",
		},
		{
			name:          "no prices available",
			instanceTypes: []string{"standard.medium", "standard.large"},
			prices:        map[string]float64{},
			want:          "standard.medium",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findCheapestInstanceType(tt.instanceTypes, tt.prices)
			if got != tt.want {
				t.Errorf("findCheapestInstanceType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateInstanceLabels(t *testing.T) {
	p := &Provider{
		options: &Options{
			ClusterID: "test-cluster-id",
		},
	}
	nodeClaim := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-claim",
			Labels: map[string]string{
				karpenterv1.NodePoolLabelKey: "test-nodepool",
			},
		},
	}

	labels := p.GenerateInstanceLabels(nodeClaim)

	expectedLabels := map[string]string{
		constants.InstanceLabelManagedBy:    constants.ManagedByKarpenter,
		constants.InstanceLabelClusterID:    "test-cluster-id",
		constants.InstanceLabelNodeClaim:    "test-node-claim",
		constants.InstanceLabelNodepoolName: "test-nodepool",
	}

	if !reflect.DeepEqual(labels, expectedLabels) {
		t.Errorf("GenerateInstanceLabels() = %v, want %v", labels, expectedLabels)
	}

	// Verify behavior when expected labels order change
	expectedLabels = map[string]string{
		constants.InstanceLabelNodepoolName: "test-nodepool",
		constants.InstanceLabelManagedBy:    constants.ManagedByKarpenter,
		constants.InstanceLabelNodeClaim:    "test-node-claim",
		constants.InstanceLabelClusterID:    "test-cluster-id",
	}

	if !reflect.DeepEqual(labels, expectedLabels) {
		t.Errorf("GenerateInstanceLabels() = %v, want %v", labels, expectedLabels)
	}
}

func TestUseSKSDefaultSecurityGroup(t *testing.T) {
	tests := []struct {
		name string
		opts *Options
		want bool
	}{
		{name: "enabled", opts: &Options{UseSKSDefaultSecurityGroup: true}, want: true},
		{name: "disabled", opts: &Options{UseSKSDefaultSecurityGroup: false}, want: false},
		{name: "nil options defaults to false", opts: &Options{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Provider{options: tt.opts}
			if got := p.UseSKSDefaultSecurityGroup(); got != tt.want {
				t.Errorf("UseSKSDefaultSecurityGroup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSksDefaultSGIDFromCluster(t *testing.T) {
	sgID := egov3.UUID("11111111-1111-1111-1111-111111111111")

	tests := []struct {
		name    string
		cluster *egov3.SKSCluster
		wantID  string
		wantOK  bool
	}{
		{name: "nil cluster", cluster: nil, wantOK: false},
		{name: "no default SG", cluster: &egov3.SKSCluster{}, wantOK: false},
		{name: "with default SG", cluster: &egov3.SKSCluster{DefaultSecurityGroupID: &sgID}, wantID: sgID.String(), wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := sksDefaultSGIDFromCluster(tt.cluster)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if id != tt.wantID {
				t.Errorf("id = %q, want %q", id, tt.wantID)
			}
		})
	}
}

// TestGetSKSDefaultSecurityGroupID_Disabled verifies the early return when
// the feature flag is off — no API client needed since no call is made.
func TestGetSKSDefaultSecurityGroupID_Disabled(t *testing.T) {
	p := &Provider{options: &Options{ClusterID: "22222222-2222-2222-2222-222222222222", UseSKSDefaultSecurityGroup: false}}
	id, ok, err := p.GetSKSDefaultSecurityGroupID(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false when disabled")
	}
	if id != "" {
		t.Errorf("expected empty id when disabled, got %q", id)
	}
}

func TestFindNodeClaimInstance(t *testing.T) {
	const (
		clusterID = "22222222-2222-2222-2222-222222222222"
		wantID    = "33333333-3333-3333-3333-333333333333"
	)
	labelsFor := func(nodeClaim string) map[string]string {
		return map[string]string{
			constants.InstanceLabelManagedBy: constants.ManagedByKarpenter,
			constants.InstanceLabelClusterID: clusterID,
			constants.InstanceLabelNodeClaim: nodeClaim,
		}
	}
	listed := []egov3.ListInstancesResponseInstances{
		{ID: "44444444-4444-4444-4444-444444444444", Labels: labelsFor("other"), State: egov3.InstanceStateRunning},
		{ID: "55555555-5555-5555-5555-555555555555", Labels: labelsFor("claim"), State: egov3.InstanceStateDestroying},
		{ID: wantID, Labels: labelsFor("claim"), State: egov3.InstanceStateStarting},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/instance":
			if !strings.Contains(r.URL.Query().Get("labels"), constants.InstanceLabelNodeClaim+"=claim") {
				t.Errorf("labels filter %q doesn't select the NodeClaim", r.URL.Query().Get("labels"))
			}
			_ = json.NewEncoder(w).Encode(egov3.ListInstancesResponse{Instances: listed})
		case r.Method == http.MethodGet && r.URL.Path == "/instance/"+wantID:
			_ = json.NewEncoder(w).Encode(egov3.Instance{ID: wantID})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client, err := egov3.NewClient(credentials.NewStaticCredentials("key", "secret"), egov3.ClientOptWithEndpoint(egov3.Endpoint(srv.URL)))
	if err != nil {
		t.Fatal(err)
	}
	p := &Provider{exoClient: client, options: &Options{ClusterID: clusterID}}

	got, err := p.findNodeClaimInstance(context.Background(), "claim")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.ID != wantID {
		t.Fatalf("findNodeClaimInstance() = %v, want instance %s", got, wantID)
	}

	listed = nil
	got, err = p.findNodeClaimInstance(context.Background(), "claim")
	if err != nil || got != nil {
		t.Fatalf("findNodeClaimInstance() = %v, %v, want nil, nil", got, err)
	}
}
