package template

import (
	egov3 "github.com/exoscale/egoscale/v3"
	v1 "k8s.io/api/core/v1"
)

type Template struct {
	ID     string
	Labels map[string]string
}

func FromExoscaleTemplate(template *egov3.Template) Template {
	return Template{
		ID: template.ID.String(),
		Labels: map[string]string{
			v1.LabelOSStable: "linux",
		},
	}
}
