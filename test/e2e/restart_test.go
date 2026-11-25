package e2e_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
)

// Serial: it stops the shared Karpenter process, which only process 1 owns.
var _ = Describe("Karpenter restart", Serial, func() {
	const name = "restart"

	It("finishes a launch interrupted by a restart without leaking instances", func(ctx SpecContext) {
		expectNodeClassReady(ctx, createNodeClass(ctx, name, defaultNodeClassSpec()))
		createNodePool(ctx, name)
		createDeployment(ctx, name, name, 1)

		By("restarting Karpenter once the instance exists but before the node joined")
		Eventually(func(g Gomega) {
			claims := nodeClaims(ctx, g, name)
			g.Expect(claims).To(HaveLen(1))
			g.Expect(claims[0].Status.ProviderID).NotTo(BeEmpty())
		}).WithContext(ctx).WithTimeout(5 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
		karpenter.stop()
		karpenter.start()

		claim := expectSingleInitializedNodeClaim(ctx, name)
		expectPodsRunningOn(ctx, name, 1, claim.Status.NodeName)

		instances, err := exo.ListInstances(ctx)
		Expect(err).NotTo(HaveOccurred())
		var launched []string
		for _, i := range instances.Instances {
			if i.Labels[constants.InstanceLabelClusterID] == clusterID && i.Labels[constants.InstanceLabelNodepoolName] == name {
				launched = append(launched, i.ID.String())
			}
		}
		Expect(launched).To(ConsistOf(instanceOf(ctx, claim).ID.String()), "instances launched for NodePool %s", name)
	}, SpecTimeout(25*time.Minute))
})
