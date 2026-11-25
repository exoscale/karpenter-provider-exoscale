package e2e_test

import (
	"context"
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
		// Created before the NodePool too: its cleanup must run after the
		// NodePool's, which is what removes the nodes.
		otherIP := createExoscaleResource(ctx, name, func() (*egov3.Operation, error) {
			return exo.CreateElasticIP(ctx, egov3.CreateElasticIPRequest{Description: runName + "-eip-b"})
		}, func(ctx context.Context, id egov3.UUID) (*egov3.Operation, error) {
			return exo.DeleteElasticIP(ctx, id)
		})

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

		By("replacing the node when another elastic IP is selected")
		replacement := expectNodeReplaced(ctx, nodeClass, name, claim, func(spec *apiv1.ExoscaleNodeClassSpec) {
			spec.ElasticIPSelectorTerms = []apiv1.SelectorTerms{{ID: otherIP.String()}}
		})
		ips := instanceOf(ctx, replacement).ElasticIPS
		Expect(ips).To(ContainElement(HaveField("ID", otherIP)))
		Expect(ips).NotTo(ContainElement(HaveField("ID", elasticIPID)))
	}, SpecTimeout(35*time.Minute))
})
