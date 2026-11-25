package e2e_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("User data", func() {
	const name = "user-data"

	// spec.userData is merged under the generated node configuration: keys only
	// the user sets are kept, keys Karpenter also sets take Karpenter's value.
	It("merges spec.userData under the generated configuration", func(ctx SpecContext) {
		spec := defaultNodeClassSpec()
		spec.Kubelet.MaxPods = new(int32(42))
		spec.UserData = new(`
[settings.kubernetes]
max-pods = 30

[settings.kubernetes.node-labels]
"e2e.karpenter.exoscale.com/user-data" = "merged"
`)
		expectNodeClassReady(ctx, createNodeClass(ctx, name, spec))
		createNodePool(ctx, name)
		createDeployment(ctx, name, name, 1)
		claim := expectSingleInitializedNodeClaim(ctx, name)

		var node corev1.Node
		Expect(kube.Get(ctx, client.ObjectKey{Name: claim.Status.NodeName}, &node)).To(Succeed())
		Expect(node.Labels).To(HaveKeyWithValue("e2e.karpenter.exoscale.com/user-data", "merged"), "user-only key kept")
		Expect(node.Status.Capacity.Pods().Value()).To(Equal(int64(42)), "NodeClass maxPods wins over user data")
	}, SpecTimeout(20*time.Minute))
})
