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
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
)

var _ = Describe("Provisioning", Ordered, func() {
	const name = "provisioning"
	var (
		deployment *appsv1.Deployment
		instanceID egov3.UUID
		launched   *launches
	)

	BeforeAll(func(ctx SpecContext) {
		expectNodeClassReady(ctx, createNodeClass(ctx, name, defaultNodeClassSpec()))
		createNodePool(ctx, name)
		launched = trackLaunches(name)
		deployment = createDeployment(ctx, name, name, 1)
	})

	It("launches a single node for a pending pod", func(ctx SpecContext) {
		claim := expectSingleInitializedNodeClaim(ctx, name)
		expectPodsRunningOn(ctx, name, 1, claim.Status.NodeName)
		launched.expectLaunched(ctx, 1)

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

var _ = Describe("Provisioning a burst", func() {
	const (
		name     = "burst"
		replicas = 3
	)

	// Pods that can't share a node make Karpenter launch several nodes at once.
	It("launches one node per pod that needs its own", func(ctx SpecContext) {
		expectNodeClassReady(ctx, createNodeClass(ctx, name, defaultNodeClassSpec()))
		createNodePool(ctx, name)
		launched := trackLaunches(name)
		createDeployment(ctx, name, name, replicas, func(pod *corev1.PodSpec) {
			pod.Affinity = &corev1.Affinity{PodAntiAffinity: &corev1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{
					LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
					TopologyKey:   corev1.LabelHostname,
				}},
			}}
		})

		Eventually(func(g Gomega) {
			claims := nodeClaims(ctx, g, name)
			g.Expect(claims).To(HaveLen(replicas))
			for _, claim := range claims {
				g.Expect(claim.StatusConditions().IsTrue(karpv1.ConditionTypeInitialized)).To(BeTrue(), "%s: %+v", claim.Name, claim.Status.Conditions)
			}
		}).WithContext(ctx).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
		launched.expectLaunched(ctx, replicas)
	}, SpecTimeout(20*time.Minute))
})
