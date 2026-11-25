package instance

import (
	"context"
	"reflect"
	"testing"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

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
}

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

func TestPatchNodeClaimAnnotations(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = apiv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	// karpenterv1 has no AddToScheme; register the NodeClaim type directly
	// so the fake client can Get/Patch it.
	scheme.AddKnownTypes(schema.GroupVersion{Group: "karpenter.sh", Version: "v1"},
		&karpenterv1.NodeClaim{}, &karpenterv1.NodeClaimList{})
	metav1.AddToGroupVersion(scheme, schema.GroupVersion{Group: "karpenter.sh", Version: "v1"})

	nodeClass := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Status:     apiv1.ExoscaleNodeClassStatus{ConfigurationHash: "initial"},
	}
	nodeClaim := &karpenterv1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Name: "nc-1"}}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(nodeClass, nodeClaim).Build()
	p := &Provider{kubeClient: c}

	for _, hash := range []string{"initial", "rotated"} {
		nodeClass.Status.ConfigurationHash = hash
		if err := p.patchNodeClaimAnnotations(context.Background(), nodeClaim, nodeClass); err != nil {
			t.Fatalf("patchNodeClaimAnnotations() error = %v", err)
		}
		got := &karpenterv1.NodeClaim{}
		if err := c.Get(context.Background(), client.ObjectKey{Name: nodeClaim.Name}, got); err != nil {
			t.Fatal(err)
		}
		if got.Annotations[constants.AnnotationConfigurationHash] != hash {
			t.Errorf("annotation = %q, want %q", got.Annotations[constants.AnnotationConfigurationHash], hash)
		}
	}
}
