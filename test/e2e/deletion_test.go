package e2e_test

import (
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/utils"
)

var _ = Describe("Deletion", func() {
	It("replaces a node whose NodeClaim is deleted", func(ctx SpecContext) {
		const name = "deletion"
		expectNodeClassReady(ctx, createNodeClass(ctx, name, defaultNodeClassSpec()))
		createNodePool(ctx, name)
		createDeployment(ctx, name, name, 1)

		old := expectSingleInitializedNodeClaim(ctx, name)
		oldInstance := instanceOf(ctx, old)
		Expect(kube.Delete(ctx, &old)).To(Succeed())

		replacement := expectSingleInitializedNodeClaim(ctx, name, old.Name)
		expectPodsRunningOn(ctx, name, 1, replacement.Status.NodeName)
		expectInstanceGone(ctx, oldInstance.ID, 5*time.Minute)
	}, SpecTimeout(25*time.Minute))

	// Karpenter core calls CloudProvider.Delete without draining for a
	// NodeClaim that never registered.
	It("deletes the instance of a NodeClaim that never registered", func(ctx SpecContext) {
		const name = "unregistered"
		expectNodeClassReady(ctx, createNodeClass(ctx, name, defaultNodeClassSpec()))

		claim := &karpv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: karpv1.NodeClaimSpec{
				NodeClassRef: nodeClaimSpec(name).NodeClassRef,
				Requirements: nodeClaimSpec(name).Requirements,
			},
		}
		Expect(kube.Create(ctx, claim)).To(Succeed())
		DeferCleanup(func(ctx SpecContext) { _ = kube.Delete(ctx, claim) })

		// The provider ID is set seconds after launch; registration takes minutes.
		Eventually(func(g Gomega) {
			g.Expect(kube.Get(ctx, client.ObjectKeyFromObject(claim), claim)).To(Succeed())
			g.Expect(claim.Status.ProviderID).NotTo(BeEmpty())
		}).WithContext(ctx).WithTimeout(5 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
		Expect(claim.StatusConditions().IsTrue(karpv1.ConditionTypeRegistered)).To(BeFalse(), "node registered before the test could delete it")
		id, err := utils.ParseProviderID(claim.Status.ProviderID)
		Expect(err).NotTo(HaveOccurred())

		Expect(kube.Delete(ctx, claim)).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(kube.Get(ctx, client.ObjectKeyFromObject(claim), &karpv1.NodeClaim{}))
		}).WithContext(ctx).WithTimeout(5 * time.Minute).Should(BeTrue())
		expectInstanceGone(ctx, egov3.UUID(id), 5*time.Minute)
	}, SpecTimeout(15*time.Minute))
})
