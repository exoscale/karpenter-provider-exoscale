package e2e_test

import (
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	eventsv1 "k8s.io/api/events/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
)

var _ = Describe("NodeClass deletion", func() {
	const name = "nodeclass-deletion"

	It("waits for its NodeClaims, then removes orphaned instances", func(ctx SpecContext) {
		nodeClass := createNodeClass(ctx, name, defaultNodeClassSpec())
		expectNodeClassReady(ctx, nodeClass)
		nodePool := createNodePool(ctx, name)
		createDeployment(ctx, name, name, 1)
		expectSingleInitializedNodeClaim(ctx, name)

		Expect(kube.Delete(ctx, nodeClass)).To(Succeed())

		By("staying while a NodeClaim still uses it")
		Eventually(func(g Gomega) {
			var events eventsv1.EventList
			g.Expect(kube.List(ctx, &events)).To(Succeed())
			g.Expect(events.Items).To(ContainElement(And(
				HaveField("Regarding.Name", name),
				HaveField("Reason", "DeletionBlocked"),
			)))
		}).WithContext(ctx).WithTimeout(2 * time.Minute).Should(Succeed())
		Consistently(func(g Gomega) {
			g.Expect(kube.Get(ctx, client.ObjectKeyFromObject(nodeClass), nodeClass)).To(Succeed())
			g.Expect(nodeClaims(ctx, g, name)).To(HaveLen(1))
		}).WithContext(ctx).WithTimeout(time.Minute).WithPolling(10 * time.Second).Should(Succeed())

		// Its NodeClaim label names a NodeClaim that doesn't exist. Created just
		// before the finalizer runs, so the garbage collector's 15-minute grace
		// period can't be what removes it. Any NodeClass finalizer cleans up
		// cluster-wide, so another spec's may get there first; that's fine.
		orphan := createKarpenterInstance(ctx, runName+"-nodeclass-orphan", egov3.Labels{
			constants.InstanceLabelNodeClaim: name + "-missing",
		})

		By("finishing once the NodePool and its NodeClaims are gone")
		Expect(kube.Delete(ctx, nodePool)).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(kube.Get(ctx, client.ObjectKeyFromObject(nodeClass), nodeClass))
		}).WithContext(ctx).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())
		expectInstanceGone(ctx, orphan, 5*time.Minute)
	}, SpecTimeout(25*time.Minute))
})
