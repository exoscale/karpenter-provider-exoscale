package e2e_test

import (
	"encoding/base64"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// staticPod is a key Karpenter never generates, so only the user data can set it.
const staticPod = `apiVersion: v1
kind: Pod
metadata:
  name: user-data-static
  namespace: default
spec:
  containers:
    - name: pause
      image: registry.k8s.io/pause:3.10
`

var _ = Describe("User data", func() {
	const name = "user-data"

	// spec.userData is merged under the generated node configuration: keys only
	// the user sets are kept, keys Karpenter also sets take Karpenter's value.
	// NodeClass fields with a CRD default, such as the image GC thresholds,
	// always count as set by Karpenter.
	It("merges spec.userData under the generated configuration", func(ctx SpecContext) {
		spec := defaultNodeClassSpec()
		spec.Kubelet.MaxPods = new(int32(42))
		spec.UserData = new(fmt.Sprintf(`
[settings.kubernetes]
max-pods = 30

[settings.kubernetes.node-labels]
"e2e.karpenter.exoscale.com/user-data" = "merged"

[settings.kubernetes.static-pods.user-data]
enabled = true
manifest = %q
`, base64.StdEncoding.EncodeToString([]byte(staticPod))))
		expectNodeClassReady(ctx, createNodeClass(ctx, name, spec))
		createNodePool(ctx, name)
		createDeployment(ctx, name, name, 1)
		claim := expectSingleInitializedNodeClaim(ctx, name)

		// The kubelet applies node labels only when it creates the Node itself.
		var node corev1.Node
		Expect(kube.Get(ctx, client.ObjectKey{Name: claim.Status.NodeName}, &node)).To(Succeed())
		Expect(node.Labels).To(HaveKeyWithValue("e2e.karpenter.exoscale.com/user-data", "merged"), "user-only node label kept")

		By("running the static pod from the user data")
		Eventually(func() error {
			return kube.Get(ctx, client.ObjectKey{Namespace: "default", Name: "user-data-static-" + claim.Status.NodeName}, &corev1.Pod{})
		}).WithContext(ctx).WithTimeout(2*time.Minute).Should(Succeed(), "mirror pod of the static pod")

		Expect(kubeletConfig(ctx, claim.Status.NodeName).MaxPods).To(Equal(int32(42)), "NodeClass maxPods wins over user data")
	}, SpecTimeout(20*time.Minute))
})
