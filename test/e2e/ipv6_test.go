package e2e_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
)

var _ = Describe("IPv6", func() {
	const name = "ipv6"

	It("gives the instance an IPv6 address and replaces the node when it is turned off", func(ctx SpecContext) {
		spec := defaultNodeClassSpec()
		spec.EnableIPv6 = true
		nodeClass := createNodeClass(ctx, name, spec)
		expectNodeClassReady(ctx, nodeClass)
		createNodePool(ctx, name)
		createDeployment(ctx, name, name, 1)

		claim := expectSingleInitializedNodeClaim(ctx, name)
		Expect(instanceOf(ctx, claim).Ipv6Address).NotTo(BeEmpty())

		replacement := expectNodeReplaced(ctx, nodeClass, name, claim, func(spec *apiv1.ExoscaleNodeClassSpec) {
			spec.EnableIPv6 = false
		})
		Expect(instanceOf(ctx, replacement).Ipv6Address).To(BeEmpty())
	}, SpecTimeout(30*time.Minute))
})
