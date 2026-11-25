package e2e_test

import (
	"context"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
)

var _ = Describe("Anti-affinity groups", func() {
	const name = "anti-affinity"

	It("places the node in the anti-affinity group selected by name", func(ctx SpecContext) {
		aag := createExoscaleResource(ctx, name, func() (*egov3.Operation, error) {
			return exo.CreateAntiAffinityGroup(ctx, egov3.CreateAntiAffinityGroupRequest{Name: runName + "-aag"})
		}, func(ctx context.Context, id egov3.UUID) (*egov3.Operation, error) {
			return exo.DeleteAntiAffinityGroup(ctx, id)
		})

		spec := defaultNodeClassSpec()
		spec.AntiAffinityGroupSelectorTerms = []apiv1.SelectorTerms{{Name: runName + "-aag"}}
		nodeClass := createNodeClass(ctx, name, spec)
		expectNodeClassReady(ctx, nodeClass)
		Expect(nodeClass.Status.AntiAffinityGroups).To(ConsistOf(aag.String()))

		createNodePool(ctx, name)
		createDeployment(ctx, name, name, 1)
		claim := expectSingleInitializedNodeClaim(ctx, name)
		Expect(instanceOf(ctx, claim).AntiAffinityGroups).To(ConsistOf(HaveField("ID", aag)))
	}, SpecTimeout(20*time.Minute))
})
