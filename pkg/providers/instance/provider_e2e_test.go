package instance

import (
	"context"
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

// TestE2E_DriftLifecycle exercises the full drift pipeline against a real
// (fake) Kubernetes API:
//
//  1. Resolve a NodeClass's configuration hash (here pre-computed as the
//     reconciler would).
//  2. Patch the NodeClaim annotation via patchNodeClaimAnnotations.
//  3. Compare the annotation against the NodeClass status, mirroring the
//     check CloudProvider.IsDrifted performs.
//
// We then mutate the status (as the reconciler would after a spec change),
// re-patch, and assert that the comparison now reports drift — proving
// the end-to-end signal flows from spec change to annotation comparison.
func TestE2E_DriftLifecycle(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = apiv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	// karpenterv1 has no AddToScheme; register the NodeClaim type directly
	// so the fake client can Get/Patch it.
	scheme.AddKnownTypes(schema.GroupVersion{Group: "karpenter.sh", Version: "v1"},
		&karpenterv1.NodeClaim{}, &karpenterv1.NodeClaimList{})
	metav1.AddToGroupVersion(scheme, schema.GroupVersion{Group: "karpenter.sh", Version: "v1"})

	const initialHash = "deadbeef0000000000000000000000000000000000000000000000000000beef"
	const rotatedHash = "feedface0000000000000000000000000000000000000000000000000000face"

	nodeClass := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{},
		},
		Status: apiv1.ExoscaleNodeClassStatus{
			ConfigurationHash: initialHash,
		},
	}
	nodeClaim := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "nc-1"},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(nodeClass, nodeClaim).
		WithStatusSubresource(&apiv1.ExoscaleNodeClass{}, &karpenterv1.NodeClaim{}).
		Build()
	p := &Provider{kubeClient: c}

	// 1. Patch the annotation on the NodeClaim.
	if err := p.patchNodeClaimAnnotations(context.Background(), nodeClaim, nodeClass); err != nil {
		t.Fatalf("patchNodeClaimAnnotations() error = %v", err)
	}

	// 2. Read the patched annotation back and verify it equals the status hash.
	got := &karpenterv1.NodeClaim{}
	if err := c.Get(context.Background(), client.ObjectKey{Name: nodeClaim.Name}, got); err != nil {
		t.Fatalf("re-reading NodeClaim: %v", err)
	}
	if got.Annotations[constants.AnnotationConfigurationHash] != initialHash {
		t.Errorf("annotation = %q, want %q",
			got.Annotations[constants.AnnotationConfigurationHash], initialHash)
	}
	if configurationHashDrifted(got, nodeClass) {
		t.Errorf("drift detected after a clean patch, want none")
	}

	// 3. User changes spec; the reconciler recomputes the status hash and
	//    it now differs from what the NodeClaim carries. Drift must fire.
	nodeClass.Status.ConfigurationHash = rotatedHash
	if !configurationHashDrifted(got, nodeClass) {
		t.Errorf("drift NOT detected after status hash change, want detected")
	}

	// 4. patchNodeClaimAnnotations rewrites the annotation; the comparison
	//    must then no longer report drift.
	if err := p.patchNodeClaimAnnotations(context.Background(), got, nodeClass); err != nil {
		t.Fatalf("patchNodeClaimAnnotations() (second call) error = %v", err)
	}
	updated := &karpenterv1.NodeClaim{}
	if err := c.Get(context.Background(), client.ObjectKey{Name: nodeClaim.Name}, updated); err != nil {
		t.Fatalf("re-reading NodeClaim: %v", err)
	}
	if updated.Annotations[constants.AnnotationConfigurationHash] != rotatedHash {
		t.Errorf("annotation not updated after second patch: got %q want %q",
			updated.Annotations[constants.AnnotationConfigurationHash], rotatedHash)
	}
	if configurationHashDrifted(updated, nodeClass) {
		t.Errorf("drift detected after second patch, want none")
	}
}

// configurationHashDrifted mirrors the section of CloudProvider.IsDrifted
// that decides whether the configuration hash matches. Keeping it inline
// here lets the test assert the wiring without spinning up the full
// CloudProvider + Exoscale API mocks.
func configurationHashDrifted(nc *karpenterv1.NodeClaim, nodeClass *apiv1.ExoscaleNodeClass) bool {
	expected := nodeClass.Status.ConfigurationHash
	got := ""
	if nc.Annotations != nil {
		got = nc.Annotations[constants.AnnotationConfigurationHash]
	}
	return got != expected
}
