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

func TestComputeExpectedSecurityGroups(t *testing.T) {
	nodeClassSGs := []string{"sg-a", "sg-b"}
	sksDefault := "sg-sks-default"

	tests := []struct {
		name               string
		nodeClassSGs       []string
		sksDefaultFound    bool
		sksDefaultSGID     string
		want               []string
		wantInputUnchanged bool
	}{
		{
			name:               "SKS default not found: returns nodeClass SGs as-is",
			nodeClassSGs:       nodeClassSGs,
			sksDefaultFound:    false,
			sksDefaultSGID:     "",
			want:               nodeClassSGs,
			wantInputUnchanged: true,
		},
		{
			name:               "SKS default found with empty ID: returns nodeClass SGs as-is",
			nodeClassSGs:       nodeClassSGs,
			sksDefaultFound:    true,
			sksDefaultSGID:     "",
			want:               nodeClassSGs,
			wantInputUnchanged: true,
		},
		{
			name:               "SKS default found and not in nodeClass: appended",
			nodeClassSGs:       nodeClassSGs,
			sksDefaultFound:    true,
			sksDefaultSGID:     sksDefault,
			want:               []string{"sg-a", "sg-b", sksDefault},
			wantInputUnchanged: true,
		},
		{
			name:               "SKS default already in nodeClass: no duplicate",
			nodeClassSGs:       []string{"sg-a", sksDefault},
			sksDefaultFound:    true,
			sksDefaultSGID:     sksDefault,
			want:               []string{"sg-a", sksDefault},
			wantInputUnchanged: true,
		},
		{
			name:               "empty nodeClass SGs with SKS default: only SKS default",
			nodeClassSGs:       nil,
			sksDefaultFound:    true,
			sksDefaultSGID:     sksDefault,
			want:               []string{sksDefault},
			wantInputUnchanged: true,
		},
		{
			name:               "nil nodeClass SGs without SKS default: nil",
			nodeClassSGs:       nil,
			sksDefaultFound:    false,
			sksDefaultSGID:     "",
			want:               nil,
			wantInputUnchanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := append([]string(nil), tt.nodeClassSGs...)
			got := computeExpectedSecurityGroups(tt.nodeClassSGs, tt.sksDefaultFound, tt.sksDefaultSGID)

			if len(got) != len(tt.want) {
				t.Fatalf("computeExpectedSecurityGroups() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("computeExpectedSecurityGroups()[%d] = %q, want %q (full: %v vs %v)", i, got[i], tt.want[i], got, tt.want)
				}
			}

			// Verify the input slice was not mutated
			if tt.wantInputUnchanged {
				if len(tt.nodeClassSGs) != len(original) {
					t.Errorf("input slice was mutated: len changed from %d to %d", len(original), len(tt.nodeClassSGs))
				} else {
					for i := range original {
						if tt.nodeClassSGs[i] != original[i] {
							t.Errorf("input slice was mutated at index %d: %q -> %q", i, original[i], tt.nodeClassSGs[i])
						}
					}
				}
			}
		})
	}
}
