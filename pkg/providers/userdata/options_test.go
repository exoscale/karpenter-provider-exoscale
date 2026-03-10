package userdata

import (
	"testing"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func TestNewOptions(t *testing.T) {
	nodeClass := &apiv1.ExoscaleNodeClass{
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{
				ClusterDNS: []string{"10.96.0.10", "10.96.0.11"},
				KubeReserved: apiv1.KubeResourceReservation{
					CPU:    "100m",
					Memory: "1Gi",
				},
				SystemReserved: apiv1.SystemResourceReservation{
					CPU:    "50m",
					Memory: "512Mi",
				},
			},
		},
	}

	nodeClaim := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test-label": "test-value",
			},
		},
		Spec: karpenterv1.NodeClaimSpec{
			Taints: []corev1.Taint{
				{
					Key:    "test-taint",
					Value:  "test-value",
					Effect: corev1.TaintEffectNoSchedule,
				},
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

	if len(opts.ClusterDNS) != 2 || opts.ClusterDNS[0] != "10.96.0.10" || opts.ClusterDNS[1] != "10.96.0.11" {
		t.Errorf("NewOptions() ClusterDNS = %v, want [10.96.0.10 10.96.0.11]", opts.ClusterDNS)
	}

	if len(opts.Taints) != 1 || opts.Taints[0].Key != "test-taint" {
		t.Errorf("NewOptions() Taints not copied correctly, got %v", opts.Taints)
	}

	if opts.KubeReserved.CPU != "100m" {
		t.Errorf("NewOptions() KubeReserved.CPU = %v, want 100m", opts.KubeReserved.CPU)
	}

	if opts.SystemReserved.Memory != "512Mi" {
		t.Errorf("NewOptions() SystemReserved.Memory = %v, want 512Mi", opts.SystemReserved.Memory)
	}
}
