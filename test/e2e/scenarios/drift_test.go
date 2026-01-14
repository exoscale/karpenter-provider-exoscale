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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

var _ = Describe("Scenario 6: Drift Detection", Ordered, func() {
	var (
		ctx       context.Context
		nodeClass *apiv1.ExoscaleNodeClass
		nodePool  *karpenterv1.NodePool
		pod       *corev1.Pod
	)

	BeforeAll(func() {
		ctx = context.Background()

		By("Creating ExoscaleNodeClass with initial security group")
		nodeClass = fixtures.NewNodeClass("drift", fixtures.DefaultNodeClassSpec())
		Expect(framework.Suite.CreateNodeClass(ctx, nodeClass)).To(Succeed())

		Eventually(framework.Suite.IsNodeClassReady(nodeClass.Name)).
			WithTimeout(2 * time.Minute).
			WithPolling(5 * time.Second).
			Should(BeTrue())

		By("Creating NodePool")
		nodePool = fixtures.NewNodePoolWithInstanceTypes("drift", nodeClass.Name, "standard.small")
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

	It("should detect drift when NodeClass configuration changes", func() {
		By("Creating a pod to trigger node provisioning")
		pod = fixtures.NewPodBuilder("drift-pod").
			WithCPU("100m").
			WithMemory("128Mi").
			Build()
		Expect(framework.Suite.KubeClient.Create(ctx, pod)).To(Succeed())

		By("Waiting for a node to be provisioned and ready")
		var nodeClaim *karpenterv1.NodeClaim
		Eventually(func() bool {
			var nodeClaims karpenterv1.NodeClaimList
			if err := framework.Suite.KubeClient.List(ctx, &nodeClaims); err != nil {
				return false
			}

			for _, nc := range nodeClaims.Items {
				if nc.Spec.NodeClassRef.Name == nodeClass.Name && nc.Status.ProviderID != "" {
					nodeClaim = &nc
					return true
				}
			}
			return false
		}).WithTimeout(framework.Suite.Config.InstanceTimeout).WithPolling(10 * time.Second).Should(BeTrue())

		Expect(nodeClaim).NotTo(BeNil())
		GinkgoWriter.Printf("NodeClaim %s is ready with provider ID: %s\n", nodeClaim.Name, nodeClaim.Status.ProviderID)

		By("Waiting for the node to become ready")
		Eventually(func() bool {
			if err := framework.Suite.KubeClient.Get(ctx,
				client.ObjectKey{Name: nodeClaim.Name}, nodeClaim); err != nil {
				return false
			}
			if nodeClaim.Status.NodeName == "" {
				return false
			}

			var node corev1.Node
			if err := framework.Suite.KubeClient.Get(ctx,
				client.ObjectKey{Name: nodeClaim.Status.NodeName}, &node); err != nil {
				return false
			}

			for _, cond := range node.Status.Conditions {
				if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
					return true
				}
			}
			return false
		}).WithTimeout(framework.Suite.Config.NodeJoinTimeout).WithPolling(10 * time.Second).Should(BeTrue())

		By("Tracking the created instance for cleanup")
		inst, err := framework.Suite.GetInstanceByNodeClaimName(ctx, nodeClaim.Name)
		Expect(err).NotTo(HaveOccurred())
		Expect(inst).NotTo(BeNil())
		framework.Suite.TrackInstance(inst.ID.String())
		GinkgoWriter.Printf("Tracked instance %s for cleanup\n", inst.ID)

		By("Updating NodeClass to change configuration (add disk size)")
		Expect(framework.Suite.KubeClient.Get(ctx,
			client.ObjectKey{Name: nodeClass.Name}, nodeClass)).To(Succeed())

		originalDiskSize := nodeClass.Spec.DiskSize
		nodeClass.Spec.DiskSize = originalDiskSize + 10
		Expect(framework.Suite.KubeClient.Update(ctx, nodeClass)).To(Succeed())
		GinkgoWriter.Printf("Updated NodeClass disk size from %d to %d\n", originalDiskSize, nodeClass.Spec.DiskSize)

		By("Checking that the NodeClaim gets a Drifted condition")
		Eventually(func() bool {
			var nc karpenterv1.NodeClaim
			if err := framework.Suite.KubeClient.Get(ctx,
				client.ObjectKey{Name: nodeClaim.Name}, &nc); err != nil {
				return false
			}

			for _, cond := range nc.Status.Conditions {
				if cond.Type == string(karpenterv1.ConditionTypeDrifted) {
					GinkgoWriter.Printf("NodeClaim %s has Drifted condition: %s - %s\n",
						nc.Name, cond.Status, cond.Message)
					return cond.Status == metav1.ConditionTrue
				}
			}
			return false
		}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())

		GinkgoWriter.Println("Drift was detected successfully")
	})
})
