package cloudprovider

import (
	"testing"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

// TestContainerRegistryHashConstants ensures the annotation and drift
// reason symbols are exported and non-empty so users can reliably refer to
// them.
func TestContainerRegistryHashConstants(t *testing.T) {
	if constants.AnnotationContainerRegistryHash == "" {
		t.Error("AnnotationContainerRegistryHash must not be empty")
	}
	if DriftReasonUserDataChanged == "" {
		t.Error("DriftReasonUserDataChanged must not be empty")
	}
}

func TestContainerRegistryHash_FakeEqualityLogic(t *testing.T) {
	hash := "abc123"
	nc := &karpenterv1.NodeClaim{
		ObjectMeta: karpenterv1.NodeClaim{}.ObjectMeta,
		Spec:       karpenterv1.NodeClaimSpec{},
	}
	class := &apiv1.ExoscaleNodeClass{
		Status: apiv1.ExoscaleNodeClassStatus{ContainerRegistryHash: hash},
	}
	if class.Status.ContainerRegistryHash != hash {
		t.Fatalf("expected status hash %q, got %q", hash, class.Status.ContainerRegistryHash)
	}
	nc.Annotations = map[string]string{constants.AnnotationContainerRegistryHash: ""}
	if nc.Annotations[constants.AnnotationContainerRegistryHash] == class.Status.ContainerRegistryHash {
		t.Errorf("expected mismatch between empty annotation and hash %q", hash)
	}
}

func TestCPUManagerHashConstants(t *testing.T) {
	if constants.AnnotationCPUManagerHash == "" {
		t.Error("AnnotationCPUManagerHash must not be empty")
	}
	if constants.AnnotationCPUManagerHash == constants.AnnotationContainerRegistryHash {
		t.Error("AnnotationCPUManagerHash must differ from AnnotationContainerRegistryHash")
	}
}

func TestCPUManagerHash_DriftScenarios(t *testing.T) {
	class := &apiv1.ExoscaleNodeClass{
		Status: apiv1.ExoscaleNodeClassStatus{CPUManagerHash: "abc123"},
	}
	nc := &karpenterv1.NodeClaim{
		ObjectMeta: karpenterv1.NodeClaim{}.ObjectMeta,
		Spec:       karpenterv1.NodeClaimSpec{},
	}

	// Annotation missing entirely -> drift
	nc.Annotations = nil
	if got := nc.Annotations[constants.AnnotationCPUManagerHash]; got == class.Status.CPUManagerHash {
		t.Errorf("missing annotation should differ from non-empty status hash")
	}

	// Annotation present, matches -> no drift
	nc.Annotations = map[string]string{constants.AnnotationCPUManagerHash: "abc123"}
	if got := nc.Annotations[constants.AnnotationCPUManagerHash]; got != class.Status.CPUManagerHash {
		t.Errorf("matching annotation should equal status hash, got %q want %q", got, class.Status.CPUManagerHash)
	}

	// Annotation present, stale -> drift
	nc.Annotations = map[string]string{constants.AnnotationCPUManagerHash: "old"}
	if nc.Annotations[constants.AnnotationCPUManagerHash] == class.Status.CPUManagerHash {
		t.Errorf("stale annotation should differ from status hash")
	}

	// User removed CPU manager config: status empty, nodeclaim still carries prior hash -> drift
	class.Status.CPUManagerHash = ""
	nc.Annotations = map[string]string{constants.AnnotationCPUManagerHash: "abc123"}
	if nc.Annotations[constants.AnnotationCPUManagerHash] == class.Status.CPUManagerHash {
		t.Errorf("empty status hash should differ from non-empty annotation")
	}
}
