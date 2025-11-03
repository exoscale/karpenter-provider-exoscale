package userdata

import (
	"testing"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func TestNewOptions(t *testing.T) {
	nodeClass := &apiv1.ExoscaleNodeClass{
		Spec: apiv1.ExoscaleNodeClassSpec{
			KubeReserved: apiv1.ResourceReservation{
				CPU:    "100m",
				Memory: "1Gi",
			},
			SystemReserved: apiv1.ResourceReservation{
				CPU:    "50m",
				Memory: "512Mi",
			},
		},
	}

	nodeClaim := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test-label": "test-value",
			},
		},
	}

	opts := NewOptions(nodeClass, nodeClaim)

	if opts == nil {
		t.Fatal("NewOptions() returned nil")
	}

	if opts.Labels["test-label"] != "test-value" {
		t.Errorf("NewOptions() labels not copied correctly")
	}

	if opts.KubeReserved.CPU != "100m" {
		t.Errorf("NewOptions() KubeReserved.CPU = %v, want 100m", opts.KubeReserved.CPU)
	}

	if opts.SystemReserved.Memory != "512Mi" {
		t.Errorf("NewOptions() SystemReserved.Memory = %v, want 512Mi", opts.SystemReserved.Memory)
	}
}
