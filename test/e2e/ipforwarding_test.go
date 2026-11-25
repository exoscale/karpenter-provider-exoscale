package e2e_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
)

var _ = Describe("IP forwarding", func() {
	const name = "ip-forwarding"

	// ip-forwarding can't be changed on a running instance, so the node is replaced.
	It("sets ip-forwarding on the instance and replaces the node when it changes", func(ctx SpecContext) {
		spec := defaultNodeClassSpec()
		spec.EnableIPForwarding = true
		nodeClass := createNodeClass(ctx, name, spec)
		expectNodeClassReady(ctx, nodeClass)
		createNodePool(ctx, name)
		createDeployment(ctx, name, name, 1)

		claim := expectSingleInitializedNodeClaim(ctx, name)
		Expect(instanceOf(ctx, claim).IPForwarding).To(HaveValue(BeTrue()))

		replacement := expectNodeReplaced(ctx, nodeClass, name, claim, func(spec *apiv1.ExoscaleNodeClassSpec) {
			spec.EnableIPForwarding = false
		})
		// The API may omit the field when it is off.
		Expect(instanceOf(ctx, replacement).IPForwarding).To(Or(BeNil(), HaveValue(BeFalse())))
	}, SpecTimeout(30*time.Minute))
})
