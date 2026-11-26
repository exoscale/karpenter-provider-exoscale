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
)

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
