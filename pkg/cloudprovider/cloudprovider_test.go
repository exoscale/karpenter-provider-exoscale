package cloudprovider

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func TestRegisteredRequiresConcreteKubeletStatus(t *testing.T) {
	providerID := "exoscale://test-instance"
	tests := []struct {
		name      string
		node      *corev1.Node
		wantReady bool
	}{
		{
			name: "node is not created yet",
		},
		{
			name: "provider placeholder is not kubelet registration",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{ProviderID: providerID},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionUnknown,
							Reason: "Initialization",
						},
					},
				},
			},
		},
		{
			name: "concrete kubelet status completes registration",
			node: &corev1.Node{
				Spec: corev1.NodeSpec{ProviderID: providerID},
				Status: corev1.NodeStatus{
					NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.34.11"},
				},
			},
			wantReady: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatalf("adding core scheme: %v", err)
			}

			builder := fake.NewClientBuilder().WithScheme(scheme).WithIndex(
				&corev1.Node{}, "spec.providerID", func(obj client.Object) []string {
					return []string{obj.(*corev1.Node).Spec.ProviderID}
				},
			)
			if tt.node != nil {
				tt.node.Name = "test-node"
				builder = builder.WithObjects(tt.node)
			}
			provider := &CloudProvider{kubeClient: builder.Build()}
			nodeClaim := &karpenterv1.NodeClaim{
				Status: karpenterv1.NodeClaimStatus{ProviderID: providerID},
			}

			result, err := provider.Registered(context.Background(), nodeClaim)
			if err != nil {
				t.Fatalf("Registered() error = %v", err)
			}
			if gotReady := result.RequeueAfter == 0; gotReady != tt.wantReady {
				t.Fatalf("Registered() ready = %v, want %v (result=%+v)", gotReady, tt.wantReady, result)
			}
			if !tt.wantReady && result.RequeueAfter != kubeletRegistrationRequeue {
				t.Fatalf("Registered() requeue = %v, want %v", result.RequeueAfter, kubeletRegistrationRequeue)
			}
		})
	}
}

func TestDrainNodeHonorsContextCancellation(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding core scheme: %v", err)
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "test-node"}}
	stuckPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "stuck",
			Namespace:         "default",
			Finalizers:        []string{"test/block"},
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
		},
		Spec: corev1.PodSpec{NodeName: "test-node"},
	}
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(node, stuckPod).WithIndex(
		&corev1.Pod{}, "spec.nodeName", func(obj client.Object) []string {
			return []string{obj.(*corev1.Pod).Spec.NodeName}
		},
	).Build()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := (&CloudProvider{kubeClient: kubeClient}).drainNode(ctx, "test-node")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("drainNode() error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("drainNode() took %v after context expired", elapsed)
	}

	var got corev1.Node
	if err := kubeClient.Get(context.Background(), client.ObjectKey{Name: "test-node"}, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Spec.Unschedulable {
		t.Fatal("node was not cordoned")
	}
}
