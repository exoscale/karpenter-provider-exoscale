package cloudprovider

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
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
