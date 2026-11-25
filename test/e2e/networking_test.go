package e2e_test

import (
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
)

var _ = Describe("Networking", func() {
	const name = "networking"

	// Attaching the elastic IP right after creation hits the API's 400 while the
	// instance is still starting, which the provider retries.
	It("attaches the selected private network and elastic IP", func(ctx SpecContext) {
		// infra.Destroy finds both by the run name if this spec's cleanup doesn't run.
		op, err := exo.CreatePrivateNetwork(ctx, egov3.CreatePrivateNetworkRequest{Name: runName})
		Expect(err).NotTo(HaveOccurred())
		op, err = exo.Wait(ctx, op, egov3.OperationStateSuccess)
		Expect(err).NotTo(HaveOccurred())
		networkID := op.Reference.ID

		op, err = exo.CreateElasticIP(ctx, egov3.CreateElasticIPRequest{Description: runName})
		Expect(err).NotTo(HaveOccurred())
		op, err = exo.Wait(ctx, op, egov3.OperationStateSuccess)
		Expect(err).NotTo(HaveOccurred())
		elasticIPID := op.Reference.ID

		// Registered first so it runs last: the instance must be gone before
		// the network and the elastic IP can be deleted.
		DeferCleanup(func(ctx SpecContext) {
			expectNoNodeClaims(ctx, name)
			_, _ = exo.DeleteElasticIP(ctx, elasticIPID)
			_, _ = exo.DeletePrivateNetwork(ctx, networkID)
		}, NodeTimeout(15*time.Minute))

		spec := defaultNodeClassSpec()
		spec.PrivateNetworkSelectorTerms = []apiv1.SelectorTerms{{ID: networkID.String()}}
		spec.ElasticIPSelectorTerms = []apiv1.SelectorTerms{{ID: elasticIPID.String()}}
		nodeClass := createNodeClass(ctx, name, spec)
		expectNodeClassReady(ctx, nodeClass)
		Expect(nodeClass.Status.PrivateNetworks).To(ConsistOf(networkID.String()))
		Expect(nodeClass.Status.ElasticIPs).To(ConsistOf(elasticIPID.String()))

		createNodePool(ctx, name)
		createDeployment(ctx, name, name, 1)
		claim := expectSingleInitializedNodeClaim(ctx, name)

		instance := instanceOf(ctx, claim)
		Expect(instance.PrivateNetworks).To(ContainElement(HaveField("ID", networkID)))
		Expect(instance.ElasticIPS).To(ContainElement(HaveField("ID", elasticIPID)))
	}, SpecTimeout(25*time.Minute))
})
