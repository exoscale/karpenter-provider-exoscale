package e2e_test

import (
	"maps"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
)

var _ = Describe("Instance labels", func() {
	const name = "instance-labels"

	// The drift check rewrites missing Karpenter labels instead of replacing the node.
	It("restores a removed instance label without replacing the node", func(ctx SpecContext) {
		expectNodeClassReady(ctx, createNodeClass(ctx, name, defaultNodeClassSpec()))
		createNodePool(ctx, name)
		createDeployment(ctx, name, name, 1)
		claim := expectSingleInitializedNodeClaim(ctx, name)
		instance := instanceOf(ctx, claim)
		Expect(instance.Labels).To(HaveKeyWithValue(constants.InstanceLabelNodepoolName, name))

		// Keep the managed-by and cluster-id labels: without them the provider
		// no longer lists the instance as one of its own.
		labels := maps.Clone(instance.Labels)
		delete(labels, constants.InstanceLabelNodepoolName)
		op, err := exo.UpdateInstance(ctx, instance.ID, egov3.UpdateInstanceRequest{Labels: labels})
		Expect(err).NotTo(HaveOccurred())
		_, err = exo.Wait(ctx, op, egov3.OperationStateSuccess)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			current, err := exo.GetInstance(ctx, instance.ID)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(current.Labels).To(HaveKeyWithValue(constants.InstanceLabelNodepoolName, name))
		}).WithContext(ctx).WithTimeout(15 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
		Expect(expectSingleInitializedNodeClaim(ctx, name).Name).To(Equal(claim.Name), "node was replaced")
	}, SpecTimeout(30*time.Minute))
})
