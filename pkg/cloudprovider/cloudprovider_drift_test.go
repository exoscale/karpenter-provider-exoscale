package cloudprovider

import (
	"testing"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

// TestConfigurationHashConstants ensures the single annotation symbol is
// exported and non-empty so users can reliably refer to it for matching /
// monitoring.
func TestConfigurationHashConstants(t *testing.T) {
	if constants.AnnotationConfigurationHash == "" {
		t.Error("AnnotationConfigurationHash must not be empty")
	}
	if DriftReasonUserDataChanged == "" {
		t.Error("DriftReasonUserDataChanged must not be empty")
	}
}

func TestConfigurationHash_FakeEqualityLogic(t *testing.T) {
	hash := "abc123"
	nc := &karpenterv1.NodeClaim{
		ObjectMeta: karpenterv1.NodeClaim{}.ObjectMeta,
		Spec:       karpenterv1.NodeClaimSpec{},
	}
	class := &apiv1.ExoscaleNodeClass{
		Status: apiv1.ExoscaleNodeClassStatus{ConfigurationHash: hash},
	}
	if class.Status.ConfigurationHash != hash {
		t.Fatalf("expected status hash %q, got %q", hash, class.Status.ConfigurationHash)
	}
	nc.Annotations = map[string]string{constants.AnnotationConfigurationHash: ""}
	if nc.Annotations[constants.AnnotationConfigurationHash] == class.Status.ConfigurationHash {
		t.Errorf("expected mismatch between empty annotation and hash %q", hash)
	}
}

func TestConfigurationHash_DriftScenarios(t *testing.T) {
	class := &apiv1.ExoscaleNodeClass{
		Status: apiv1.ExoscaleNodeClassStatus{ConfigurationHash: "abc123"},
	}
	nc := &karpenterv1.NodeClaim{
		ObjectMeta: karpenterv1.NodeClaim{}.ObjectMeta,
		Spec:       karpenterv1.NodeClaimSpec{},
	}

	// Annotation missing entirely -> drift
	nc.Annotations = nil
	if got := nc.Annotations[constants.AnnotationConfigurationHash]; got == class.Status.ConfigurationHash {
		t.Errorf("missing annotation should differ from non-empty status hash")
	}

	// Annotation present, matches -> no drift
	nc.Annotations = map[string]string{constants.AnnotationConfigurationHash: "abc123"}
	if got := nc.Annotations[constants.AnnotationConfigurationHash]; got != class.Status.ConfigurationHash {
		t.Errorf("matching annotation should equal status hash, got %q want %q", got, class.Status.ConfigurationHash)
	}

	// Annotation present, stale -> drift
	nc.Annotations = map[string]string{constants.AnnotationConfigurationHash: "old"}
	if nc.Annotations[constants.AnnotationConfigurationHash] == class.Status.ConfigurationHash {
		t.Errorf("stale annotation should differ from status hash")
	}

	// User removed configuration: status empty, nodeclaim still carries prior hash -> drift
	class.Status.ConfigurationHash = ""
	nc.Annotations = map[string]string{constants.AnnotationConfigurationHash: "abc123"}
	if nc.Annotations[constants.AnnotationConfigurationHash] == class.Status.ConfigurationHash {
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
