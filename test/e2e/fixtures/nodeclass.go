package fixtures

import (
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/test/e2e/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewNodeClass(suffix string, spec apiv1.ExoscaleNodeClassSpec) *apiv1.ExoscaleNodeClass {
	return &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: framework.Suite.ResourceName(suffix),
		},
		Spec: spec,
	}
}

func DefaultNodeClassSpec() apiv1.ExoscaleNodeClassSpec {
	return apiv1.ExoscaleNodeClassSpec{
		TemplateID:     framework.Suite.Config.TemplateID,
		SecurityGroups: []string{framework.Suite.Config.SecurityGroupID},
		DiskSize:       50,
	}
}

func NodeClassSpecWithPrivateNetwork(privateNetworkID string) apiv1.ExoscaleNodeClassSpec {
	spec := DefaultNodeClassSpec()
	spec.PrivateNetworks = []string{privateNetworkID}
	return spec
}
