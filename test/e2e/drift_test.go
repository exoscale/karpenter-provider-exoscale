package e2e_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Drift", func() {
	const name = "drift"

	// kubelet.maxPods is part of the NodeClass configuration hash, so changing
	// it marks existing NodeClaims as drifted (user-data drift).
	It("replaces a node when its NodeClass configuration changes", func(ctx SpecContext) {
		nodeClass := createNodeClass(ctx, name, defaultNodeClassSpec())
		expectNodeClassReady(ctx, nodeClass)
		createNodePool(ctx, name)
		createDeployment(ctx, name, name, 1)

		old := expectSingleInitializedNodeClaim(ctx, name)
		oldInstance := instanceOf(ctx, old)
		oldHash := nodeClass.Status.ConfigurationHash

		patch := client.MergeFrom(nodeClass.DeepCopy())
		nodeClass.Spec.Kubelet.MaxPods = new(int32(50))
		Expect(kube.Patch(ctx, nodeClass, patch)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(kube.Get(ctx, client.ObjectKeyFromObject(nodeClass), nodeClass)).To(Succeed())
			g.Expect(nodeClass.Status.ConfigurationHash).NotTo(Equal(oldHash))
		}).WithContext(ctx).WithTimeout(2 * time.Minute).Should(Succeed())

		replacement := expectSingleInitializedNodeClaim(ctx, name, old.Name)
		expectPodsRunningOn(ctx, name, 1, replacement.Status.NodeName)
		expectInstanceGone(ctx, oldInstance.ID, 5*time.Minute)
	}, SpecTimeout(25*time.Minute))
})
