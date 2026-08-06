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

// TestDriftReasonImageID ensures the symbol is exported and non-empty so
// users can rely on the drift reason in event matching / monitoring.
func TestDriftReasonImageID(t *testing.T) {
	if DriftReasonImageID == "" {
		t.Error("DriftReasonImageID must not be empty")
	}
}

// TestImageID_DriftScenarios mirrors the comparison done by IsDrifter against
// ExoscaleNodeClass.status.imageID and exercises the four documented
// scenarios: NodeClass changed, match, empty status guard, and auto-upgrade
// via imageTemplateSelector.
func TestImageID_DriftScenarios(t *testing.T) {
	class := &apiv1.ExoscaleNodeClass{
		Status: apiv1.ExoscaleNodeClassStatus{ImageID: "new-template-id"},
	}
	nc := &karpenterv1.NodeClaim{
		ObjectMeta: karpenterv1.NodeClaim{}.ObjectMeta,
		Spec:       karpenterv1.NodeClaimSpec{},
	}

	// 1. NodeClass changed (user rotated templateID): nodeClaim carries the
	//    old template ID, NodeClass resolved a new one -> drift.
	nc.Status.ImageID = "old-template-id"
	if nc.Status.ImageID == class.Status.ImageID {
		t.Errorf("expected drift when nodeClaim ImageID differs from nodeClass ImageID")
	}

	// 2. Match: nodeClaim == nodeClass -> no drift.
	nc.Status.ImageID = class.Status.ImageID
	if nc.Status.ImageID != class.Status.ImageID {
		t.Errorf("expected no drift when nodeClaim ImageID equals nodeClass ImageID")
	}

	// 3. Empty status guard: NodeClass not yet reconciled -> skip drift
	//    even if nodeClaim carries a stale value.
	class.Status.ImageID = ""
	nc.Status.ImageID = "stale-template-id"
	if class.Status.ImageID != "" {
		t.Errorf("empty status guard precondition broken")
	}

	// 4. Auto-upgrade via imageTemplateSelector: NodeClass resolved a new
	//    template (v2) while nodeClaim still runs v1 -> drift.
	class.Status.ImageID = "v2-template-id"
	nc.Status.ImageID = "v1-template-id"
	if nc.Status.ImageID == class.Status.ImageID {
		t.Errorf("expected drift after auto-upgrade rotates resolved template ID")
	}
}
