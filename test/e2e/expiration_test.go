package e2e_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

var _ = Describe("Expiration", func() {
	const name = "expiration"

	It("replaces a node once it reaches expireAfter", func(ctx SpecContext) {
		expectNodeClassReady(ctx, createNodeClass(ctx, name, defaultNodeClassSpec()))
		createNodePool(ctx, name, func(np *karpv1.NodePool) {
			np.Spec.Template.Spec.ExpireAfter = karpv1.MustParseNillableDuration("5m")
		})
		createDeployment(ctx, name, name, 1)

		old := expectSingleInitializedNodeClaim(ctx, name)
		oldInstance := instanceOf(ctx, old)
		replacement := expectSingleInitializedNodeClaim(ctx, name, old.Name)
		expectPodsRunningOn(ctx, name, 1, replacement.Status.NodeName)
		expectInstanceGone(ctx, oldInstance.ID, 5*time.Minute)
	}, SpecTimeout(25*time.Minute))
})
