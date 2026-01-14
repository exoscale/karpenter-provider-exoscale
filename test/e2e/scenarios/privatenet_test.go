package scenarios_test

import (
	"context"
	"time"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/test/e2e/fixtures"
	"github.com/exoscale/karpenter-exoscale/test/e2e/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

var _ = Describe("Scenario 4: Private Network Provisioning", Ordered, func() {
	var (
		ctx       context.Context
		nodeClass *apiv1.ExoscaleNodeClass
		nodePool  *karpenterv1.NodePool
		pod       *corev1.Pod
		privNetID string
	)

	BeforeAll(func() {
		ctx = context.Background()

		By("Creating a private network")
		privNet, err := framework.Suite.CreatePrivateNetwork(ctx, "privnet-e2e")
		Expect(err).NotTo(HaveOccurred())
		privNetID = privNet.ID.String()
		GinkgoWriter.Printf("Created private network: %s\n", privNetID)

		By("Creating ExoscaleNodeClass with private network")
		nodeClass = fixtures.NewNodeClass("privnet", fixtures.NodeClassSpecWithPrivateNetwork(privNetID))
		Expect(framework.Suite.CreateNodeClass(ctx, nodeClass)).To(Succeed())

		Eventually(framework.Suite.IsNodeClassReady(nodeClass.Name)).
			WithTimeout(2 * time.Minute).
			WithPolling(5 * time.Second).
			Should(BeTrue())

		By("Creating NodePool")
		nodePool = fixtures.NewNodePoolWithInstanceTypes("privnet", nodeClass.Name, "standard.small")
		Expect(framework.Suite.CreateNodePool(ctx, nodePool)).To(Succeed())
	})

	AfterAll(func() {
		if pod != nil {
			_ = framework.Suite.KubeClient.Delete(ctx, pod)
		}
		if nodePool != nil {
			_ = framework.Suite.DeleteNodePool(ctx, nodePool)
		}
		if nodeClass != nil {
			_ = framework.Suite.DeleteNodeClass(ctx, nodeClass)
		}
	})

	It("should create an instance with private network attached", func() {
		By("Creating a pod to trigger provisioning")
		pod = fixtures.NewPodBuilder("privnet-pod").
			WithCPU("100m").
			WithMemory("128Mi").
			Build()
		Expect(framework.Suite.KubeClient.Create(ctx, pod)).To(Succeed())

		By("Waiting for the instance to be running")
		var nodeClaim *karpenterv1.NodeClaim
		Eventually(func() bool {
			var nodeClaims karpenterv1.NodeClaimList
			if err := framework.Suite.KubeClient.List(ctx, &nodeClaims); err != nil {
				return false
			}

			for _, nc := range nodeClaims.Items {
				if nc.Spec.NodeClassRef.Name == nodeClass.Name {
					nodeClaim = &nc
					return nc.Status.ProviderID != ""
				}
			}
			return false
		}).WithTimeout(framework.Suite.Config.InstanceTimeout).WithPolling(10 * time.Second).Should(BeTrue())

		Expect(nodeClaim).NotTo(BeNil())
		GinkgoWriter.Printf("NodeClaim %s has provider ID: %s\n", nodeClaim.Name, nodeClaim.Status.ProviderID)

		By("Verifying the instance has the private network attached")
		inst, err := framework.Suite.GetInstanceByNodeClaimName(ctx, nodeClaim.Name)
		Expect(err).NotTo(HaveOccurred())
		Expect(inst).NotTo(BeNil())
		framework.Suite.TrackInstance(inst.ID.String())

		hasPrivNet := false
		for _, pn := range inst.PrivateNetworks {
			if pn.ID.String() == privNetID {
				hasPrivNet = true
				break
			}
		}
		Expect(hasPrivNet).To(BeTrue(), "Instance should have private network %s attached", privNetID)
		GinkgoWriter.Printf("Instance %s has private network %s attached\n", inst.ID, privNetID)
	})
})
