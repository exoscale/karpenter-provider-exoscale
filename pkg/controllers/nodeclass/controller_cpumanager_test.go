package nodeclass

import (
	"context"
	"testing"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SHA256 values below are computed by feeding the same key\0value\0
// representation into crypto/sha256 — see hashEntries in
// controller_reconcilers.go for the exact algorithm. They are pinned to
// guarantee the hash format never drifts silently.

func newConfigurationHashReconciler(t *testing.T) *ExoscaleNodeClassReconciler {
	t.Helper()
	return newContainerRegistryReconciler(t) // schema/fake-client setup is identical
}

func TestReconcileConfigurationHash_Defaults(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{ObjectMeta: metav1.ObjectMeta{Name: "nc"}}
	r := newConfigurationHashReconciler(t)
	if err := r.reconcileConfigurationHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nc.Status.ConfigurationHash != "" {
		t.Errorf("hash = %q, want empty string for default kubelet", nc.Status.ConfigurationHash)
	}
}

func TestReconcileConfigurationHash_NonePolicy(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{
				CPUManagerPolicy: "none",
			},
		},
	}
	r := newConfigurationHashReconciler(t)
	if err := r.reconcileConfigurationHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nc.Status.ConfigurationHash != "" {
		t.Errorf("hash = %q, want empty string for policy=none", nc.Status.ConfigurationHash)
	}
}

func TestReconcileConfigurationHash_DefaultCPUManagerPeriod(t *testing.T) {
	// Default period (10s) must not contribute to the hash.
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{
				CPUManagerPolicy:          "static",
				CPUManagerReconcilePeriod: "10s",
			},
		},
	}
	r := newConfigurationHashReconciler(t)
	if err := r.reconcileConfigurationHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "81fa371d1438816b8fc550b18f4f32c69380be9c472037a074bb88766c9bfbef"
	if nc.Status.ConfigurationHash != want {
		t.Errorf("hash = %q, want %q (period=10s is default and must be omitted)", nc.Status.ConfigurationHash, want)
	}
}

func TestReconcileConfigurationHash_StaticOnly(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{
				CPUManagerPolicy: "static",
			},
		},
	}
	r := newConfigurationHashReconciler(t)
	if err := r.reconcileConfigurationHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nc.Status.ConfigurationHash == "" {
		t.Error("hash = empty, want non-empty for policy=static")
	}
}

func TestReconcileConfigurationHash_StaticWithOptions(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{
				CPUManagerPolicy:        "static",
				CPUManagerPolicyOptions: []string{"full-pcpus-only", "align-by-socket"},
			},
		},
	}
	r := newConfigurationHashReconciler(t)
	if err := r.reconcileConfigurationHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nc.Status.ConfigurationHash == "" {
		t.Error("hash = empty, want non-empty")
	}
}

func TestReconcileConfigurationHash_OptionsDeterministic(t *testing.T) {
	// Same options in different orders must produce the same hash.
	make := func(opts []string) *apiv1.ExoscaleNodeClass {
		return &apiv1.ExoscaleNodeClass{
			ObjectMeta: metav1.ObjectMeta{Name: "nc"},
			Spec: apiv1.ExoscaleNodeClassSpec{
				Kubelet: apiv1.KubeletConfiguration{
					CPUManagerPolicy:        "static",
					CPUManagerPolicyOptions: opts,
				},
			},
		}
	}
	a := make([]string{"full-pcpus-only", "align-by-socket"})
	b := make([]string{"align-by-socket", "full-pcpus-only"})
	r := newConfigurationHashReconciler(t)
	if err := r.reconcileConfigurationHash(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileConfigurationHash(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if a.Status.ConfigurationHash != b.Status.ConfigurationHash {
		t.Errorf("hash not order-independent: %q vs %q", a.Status.ConfigurationHash, b.Status.ConfigurationHash)
	}
}

func TestReconcileConfigurationHash_OptionsDroppedWhenPolicyNone(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{
				CPUManagerPolicy:        "none",
				CPUManagerPolicyOptions: []string{"full-pcpus-only"},
			},
		},
	}
	r := newConfigurationHashReconciler(t)
	if err := r.reconcileConfigurationHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nc.Status.ConfigurationHash != "" {
		t.Errorf("hash = %q, want empty (options dropped when policy!=static)", nc.Status.ConfigurationHash)
	}
}

func TestReconcileConfigurationHash_ClearResetsToEmpty(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Status:     apiv1.ExoscaleNodeClassStatus{ConfigurationHash: "stale"},
	}
	r := newConfigurationHashReconciler(t)
	if err := r.reconcileConfigurationHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nc.Status.ConfigurationHash != "" {
		t.Errorf("hash = %q, want empty string after clearing fields", nc.Status.ConfigurationHash)
	}
}

func TestReconcileConfigurationHash_MaxPods(t *testing.T) {
	mk := func(maxPods *int32) *apiv1.ExoscaleNodeClass {
		return &apiv1.ExoscaleNodeClass{
			ObjectMeta: metav1.ObjectMeta{Name: "nc"},
			Spec:       apiv1.ExoscaleNodeClassSpec{Kubelet: apiv1.KubeletConfiguration{MaxPods: maxPods}},
		}
	}

	t.Run("unset contributes nothing", func(t *testing.T) {
		nc := mk(nil)
		r := newConfigurationHashReconciler(t)
		if err := r.reconcileConfigurationHash(context.Background(), nc); err != nil {
			t.Fatal(err)
		}
		if nc.Status.ConfigurationHash != "" {
			t.Errorf("hash = %q, want empty", nc.Status.ConfigurationHash)
		}
	})

	t.Run("value changes the hash", func(t *testing.T) {
		v110 := int32(110)
		v250 := int32(250)
		a := mk(&v110)
		b := mk(&v250)
		r := newConfigurationHashReconciler(t)
		if err := r.reconcileConfigurationHash(context.Background(), a); err != nil {
			t.Fatal(err)
		}
		if err := r.reconcileConfigurationHash(context.Background(), b); err != nil {
			t.Fatal(err)
		}
		if a.Status.ConfigurationHash == "" || b.Status.ConfigurationHash == "" {
			t.Fatalf("both hashes must be non-empty, got %q and %q", a.Status.ConfigurationHash, b.Status.ConfigurationHash)
		}
		if a.Status.ConfigurationHash == b.Status.ConfigurationHash {
			t.Errorf("expected different hashes for 110 vs 250, both = %q", a.Status.ConfigurationHash)
		}
	})

	t.Run("removing the override clears the hash", func(t *testing.T) {
		v := int32(110)
		nc := mk(&v)
		r := newConfigurationHashReconciler(t)
		if err := r.reconcileConfigurationHash(context.Background(), nc); err != nil {
			t.Fatal(err)
		}
		if nc.Status.ConfigurationHash == "" {
			t.Fatal("hash = empty after setting maxPods, want non-empty")
		}
		nc.Spec.Kubelet.MaxPods = nil
		if err := r.reconcileConfigurationHash(context.Background(), nc); err != nil {
			t.Fatal(err)
		}
		if nc.Status.ConfigurationHash != "" {
			t.Errorf("hash = %q, want empty after removing maxPods", nc.Status.ConfigurationHash)
		}
	})
}
