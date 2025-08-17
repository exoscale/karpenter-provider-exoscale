package instance

import (
	"context"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

type Provider interface {
	Create(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass,
		nodeClaim *karpenterv1.NodeClaim, userData string, tags map[string]string) (*Instance, error)
	Get(ctx context.Context, id string) (*Instance, error)
	List(ctx context.Context) ([]*Instance, error)
	Delete(ctx context.Context, id string) error
	UpdateTags(ctx context.Context, id string, tags map[string]string) error
}

type Instance struct {
	ID                 string
	Name               string
	State              egov3.InstanceState
	InstanceType       *egov3.InstanceType
	Template           *egov3.Template
	Zone               string
	Labels             map[string]string
	SecurityGroups     []string
	PrivateNetworks    []string
	AntiAffinityGroups []string
	CreatedAt          time.Time
}

func FromExoscaleInstance(instance *egov3.Instance, zone string) *Instance {
	if instance == nil {
		return nil
	}

	i := &Instance{
		ID:           instance.ID.String(),
		Name:         instance.Name,
		State:        instance.State,
		InstanceType: instance.InstanceType,
		Template:     instance.Template,
		Zone:         zone,
		Labels:       instance.Labels,
		CreatedAt:    instance.CreatedAT,
	}

	for _, sg := range instance.SecurityGroups {
		i.SecurityGroups = append(i.SecurityGroups, string(sg.ID))
	}

	for _, pn := range instance.PrivateNetworks {
		i.PrivateNetworks = append(i.PrivateNetworks, string(pn.ID))
	}

	for _, aag := range instance.AntiAffinityGroups {
		i.AntiAffinityGroups = append(i.AntiAffinityGroups, string(aag.ID))
	}

	return i
}
