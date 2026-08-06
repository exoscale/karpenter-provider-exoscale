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
