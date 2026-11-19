package v1

import (
	"os"
	"strings"
	"testing"
)

// TestCRD_VpcSelectorRequiredCELRule guards the generated CRD against losing
// the CEL rule that makes spec.vpcSelector mandatory when
// spec.vpcSubnetSelectorTerms is set (and vice versa). controller-gen
// type-checks the rules at generation time; this test ensures the rules
// actually land in the committed CRD manifest.
func TestCRD_VpcSelectorRequiredCELRule(t *testing.T) {
	data, err := os.ReadFile("../../../config/crd/bases/karpenter.exoscale.com_exoscalenodeclasses.yaml")
	if err != nil {
		t.Fatalf("reading CRD manifest: %v", err)
	}

	// YAML line-wrapping must not break rule matching: compare on
	// whitespace-normalized content.
	normalized := strings.Join(strings.Fields(string(data)), " ")

	rules := []struct {
		message string
		rule    string
	}{
		{
			message: "vpcSelector is required when vpcSubnetSelectorTerms is set",
			rule:    "!has(self.vpcSubnetSelectorTerms) || size(self.vpcSubnetSelectorTerms) == 0 || has(self.vpcSelector)",
		},
		{
			message: "vpcSelector requires at least one entry in vpcSubnetSelectorTerms",
			rule:    "!has(self.vpcSelector) || (has(self.vpcSubnetSelectorTerms) && size(self.vpcSubnetSelectorTerms) > 0)",
		},
	}

	for _, r := range rules {
		if !strings.Contains(normalized, r.message) {
			t.Errorf("CRD manifest is missing CEL message %q", r.message)
		}
		if !strings.Contains(normalized, strings.Join(strings.Fields(r.rule), " ")) {
			t.Errorf("CRD manifest is missing CEL rule %q", r.rule)
		}
	}
}
