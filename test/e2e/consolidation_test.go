package e2e_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

var _ = Describe("Consolidation", func() {
	// Replacement only happens when the provider's prices make the smaller
	// instance type cheaper.
	It("replaces an underutilized node with a cheaper one", func(ctx SpecContext) {
		const name = "consolidation"
		expectNodeClassReady(ctx, createNodeClass(ctx, name, defaultNodeClassSpec()))
		createNodePool(ctx, name, func(np *karpv1.NodePool) {
			np.Spec.Template.Spec.Requirements[0].Values = []string{"standard.small", "standard.medium", "standard.large"}
		})
		requestCPU := func(cpu string) func(*corev1.PodSpec) {
			return func(pod *corev1.PodSpec) {
				pod.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse(cpu)
			}
		}

		// Only standard.large (4 vCPU) fits 3 CPUs.
		big := createDeployment(ctx, name+"-big", name, 1, requestCPU("3"))
		large := expectSingleInitializedNodeClaim(ctx, name)
		Expect(large.Labels).To(HaveKeyWithValue(corev1.LabelInstanceTypeStable, "standard.large"))
		// Fits next to the big pod, so the scheduler places it on the same node.
		createDeployment(ctx, name+"-small", name, 1, requestCPU("200m"))
		expectPodsRunningOn(ctx, name+"-small", 1, large.Status.NodeName)

		Expect(kube.Delete(ctx, big)).To(Succeed())
		replacement := expectSingleInitializedNodeClaim(ctx, name, large.Name)
		Expect(replacement.Labels).To(HaveKeyWithValue(corev1.LabelInstanceTypeStable, BeElementOf("standard.small", "standard.medium")))
		expectPodsRunningOn(ctx, name+"-small", 1, replacement.Status.NodeName)
	}, SpecTimeout(30*time.Minute))

	It("keeps an empty node while the disruption budget allows no disruption", func(ctx SpecContext) {
		const name = "budget"
		expectNodeClassReady(ctx, createNodeClass(ctx, name, defaultNodeClassSpec()))
		nodePool := createNodePool(ctx, name, func(np *karpv1.NodePool) {
			np.Spec.Disruption.Budgets = []karpv1.Budget{{Nodes: "0"}}
		})
		deployment := createDeployment(ctx, name, name, 1)
		expectSingleInitializedNodeClaim(ctx, name)

		scale(ctx, deployment, 0)
		Consistently(func(g Gomega) {
			g.Expect(nodeClaims(ctx, g, name)).To(HaveLen(1))
		}).WithContext(ctx).WithTimeout(2 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

		By("removing the node once the budget allows it")
		patch := client.MergeFrom(nodePool.DeepCopy())
		nodePool.Spec.Disruption.Budgets = []karpv1.Budget{{Nodes: "100%"}}
		Expect(kube.Patch(ctx, nodePool, patch)).To(Succeed())
		expectNoNodeClaims(ctx, name)
	}, SpecTimeout(25*time.Minute))
})
