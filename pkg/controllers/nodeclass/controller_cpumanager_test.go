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

func newCPUManagerReconciler(t *testing.T) *ExoscaleNodeClassReconciler {
	t.Helper()
	return newContainerRegistryReconciler(t) // schema/fake-client setup is identical
}

func TestReconcileCPUManagerHash_Defaults(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{ObjectMeta: metav1.ObjectMeta{Name: "nc"}}
	r := newCPUManagerReconciler(t)
	if err := r.reconcileCPUManagerHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nc.Status.CPUManagerHash != "" {
		t.Errorf("hash = %q, want empty string for default kubelet", nc.Status.CPUManagerHash)
	}
}

func TestReconcileCPUManagerHash_NonePolicy(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{
				CPUManagerPolicy: "none",
			},
		},
	}
	r := newCPUManagerReconciler(t)
	if err := r.reconcileCPUManagerHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nc.Status.CPUManagerHash != "" {
		t.Errorf("hash = %q, want empty string for policy=none", nc.Status.CPUManagerHash)
	}
}

func TestReconcileCPUManagerHash_DefaultPeriod(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{
				CPUManagerPolicy:          "static",
				CPUManagerReconcilePeriod: "10s",
			},
		},
	}
	r := newCPUManagerReconciler(t)
	if err := r.reconcileCPUManagerHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "327e67896c556d020fd6bfd892cc770f486abd8313160a70db010efc48561f63"
	if nc.Status.CPUManagerHash != want {
		t.Errorf("hash = %q, want %q (period=10s is default and must be omitted)", nc.Status.CPUManagerHash, want)
	}
}

func TestReconcileCPUManagerHash_StaticOnly(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{
				CPUManagerPolicy: "static",
			},
		},
	}
	r := newCPUManagerReconciler(t)
	if err := r.reconcileCPUManagerHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "327e67896c556d020fd6bfd892cc770f486abd8313160a70db010efc48561f63"
	if nc.Status.CPUManagerHash != want {
		t.Errorf("hash = %q, want %q", nc.Status.CPUManagerHash, want)
	}
}

func TestReconcileCPUManagerHash_OptionsIgnoredUnlessStatic(t *testing.T) {
	withOptions := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{
				CPUManagerPolicy:        "none",
				CPUManagerPolicyOptions: []string{"full-pcpus-only"},
			},
		},
	}
	noOptions := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{
				CPUManagerPolicy: "none",
			},
		},
	}
	r := newCPUManagerReconciler(t)
	if err := r.reconcileCPUManagerHash(context.Background(), withOptions); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := r.reconcileCPUManagerHash(context.Background(), noOptions); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if withOptions.Status.CPUManagerHash != "" {
		t.Errorf("withOptions hash = %q, want empty (policy!=static, options must be ignored)", withOptions.Status.CPUManagerHash)
	}
	if noOptions.Status.CPUManagerHash != "" {
		t.Errorf("noOptions hash = %q, want empty", noOptions.Status.CPUManagerHash)
	}
	if withOptions.Status.CPUManagerHash != noOptions.Status.CPUManagerHash {
		t.Errorf("options should be ignored when policy!=static: withOptions=%q noOptions=%q",
			withOptions.Status.CPUManagerHash, noOptions.Status.CPUManagerHash)
	}
}

func TestReconcileCPUManagerHash_OptionsSorted(t *testing.T) {
	nc1 := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{
				CPUManagerPolicy:        "static",
				CPUManagerPolicyOptions: []string{"align-by-socket", "full-pcpus-only"},
			},
		},
	}
	nc2 := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{
				CPUManagerPolicy:        "static",
				CPUManagerPolicyOptions: []string{"full-pcpus-only", "align-by-socket"},
			},
		},
	}
	r := newCPUManagerReconciler(t)
	if err := r.reconcileCPUManagerHash(context.Background(), nc1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := r.reconcileCPUManagerHash(context.Background(), nc2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "76eb404f241890764af5804252faa9c72f0bf50f48ee83158dc27864be7fa0f9"
	if nc1.Status.CPUManagerHash != want {
		t.Errorf("nc1 hash = %q, want %q", nc1.Status.CPUManagerHash, want)
	}
	if nc2.Status.CPUManagerHash != want {
		t.Errorf("nc2 hash = %q, want %q", nc2.Status.CPUManagerHash, want)
	}
}

func TestReconcileCPUManagerHash_NonDefaultPeriod(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{
				CPUManagerReconcilePeriod: "5s",
			},
		},
	}
	r := newCPUManagerReconciler(t)
	if err := r.reconcileCPUManagerHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "6b327824e0de64d912454cbe52d004a96864ae046df126a39b93d56359f3e7bb"
	if nc.Status.CPUManagerHash != want {
		t.Errorf("hash = %q, want %q (period=5s)", nc.Status.CPUManagerHash, want)
	}
}

func TestReconcileCPUManagerHash_DeterministicAcrossRuns(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{
				CPUManagerPolicy:          "static",
				CPUManagerPolicyOptions:   []string{"full-pcpus-only", "align-by-socket"},
				CPUManagerReconcilePeriod: "5s",
			},
		},
	}
	r := newCPUManagerReconciler(t)
	if err := r.reconcileCPUManagerHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "8d85f586c48eec0cf8ce04cfee910372f5c9eca796736da5588855718d40f398"
	if nc.Status.CPUManagerHash != want {
		t.Errorf("hash = %q, want %q", nc.Status.CPUManagerHash, want)
	}
	first := nc.Status.CPUManagerHash
	if err := r.reconcileCPUManagerHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nc.Status.CPUManagerHash != first {
		t.Errorf("hash not deterministic: first=%q second=%q", first, nc.Status.CPUManagerHash)
	}
}

func TestReconcileCPUManagerHash_ClearResetsToEmpty(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Status:     apiv1.ExoscaleNodeClassStatus{CPUManagerHash: "stale"},
	}
	r := newCPUManagerReconciler(t)
	if err := r.reconcileCPUManagerHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nc.Status.CPUManagerHash != "" {
		t.Errorf("hash = %q, want empty string after clearing fields", nc.Status.CPUManagerHash)
	}
}
