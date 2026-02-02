package template

import (
	"context"

	egov3 "github.com/exoscale/egoscale/v3"
	v1 "k8s.io/api/core/v1"
)

// Client is an interface for getting templates from Exoscale
type Client interface {
	GetTemplate(ctx context.Context, id egov3.UUID) (*egov3.Template, error)
}

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
