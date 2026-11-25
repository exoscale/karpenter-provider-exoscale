package e2e_test

import (
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
)

var _ = Describe("Provisioning", Ordered, func() {
	const name = "provisioning"
	var (
		deployment *appsv1.Deployment
		instanceID egov3.UUID
	)

	BeforeAll(func(ctx SpecContext) {
		expectNodeClassReady(ctx, createNodeClass(ctx, name, defaultNodeClassSpec()))
		createNodePool(ctx, name)
		deployment = createDeployment(ctx, name, name, 1)
	})

	It("launches a node for a pending pod", func(ctx SpecContext) {
		claim := expectSingleInitializedNodeClaim(ctx, name)
		expectPodsRunningOn(ctx, name, 1, claim.Status.NodeName)

		instance := instanceOf(ctx, claim)
		instanceID = instance.ID
		Expect(instance.Labels).To(HaveKeyWithValue(constants.InstanceLabelClusterID, clusterID))

		By("deleting the node's bootstrap token once it registered")
		Eventually(func(g Gomega) {
			var tokens corev1.SecretList
			g.Expect(kube.List(ctx, &tokens, client.InNamespace(metav1.NamespaceSystem),
				client.MatchingLabels{constants.LabelTokenProvider: constants.ProviderName})).To(Succeed())
			for _, token := range tokens.Items {
				g.Expect(token.Annotations).NotTo(HaveKeyWithValue(constants.AnnotationBootstrapToken, claim.Name))
			}
		}).WithContext(ctx).WithTimeout(2 * time.Minute).Should(Succeed())
	}, SpecTimeout(15*time.Minute))

	It("removes the node once it is empty", func(ctx SpecContext) {
		Expect(instanceID).NotTo(BeEmpty(), "previous spec did not launch a node")
		scale(ctx, deployment, 0)
		expectNoNodeClaims(ctx, name)
		expectInstanceGone(ctx, instanceID, 5*time.Minute)
	}, SpecTimeout(15*time.Minute))
})
