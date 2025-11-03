package template

import (
	"testing"

	egov3 "github.com/exoscale/egoscale/v3"
	v1 "k8s.io/api/core/v1"
)

func TestFromExoscaleTemplate(t *testing.T) {
	templateID := "test-template-id"
	exoTemplate := &egov3.Template{
		ID: egov3.UUID(templateID),
	}

	got := FromExoscaleTemplate(exoTemplate)

	if got.ID != templateID {
		t.Errorf("FromExoscaleTemplate() ID = %v, want %v", got.ID, templateID)
	}

	if got.Labels[v1.LabelOSStable] != "linux" {
		t.Errorf("FromExoscaleTemplate() OS label = %v, want linux", got.Labels[v1.LabelOSStable])
	}
}
