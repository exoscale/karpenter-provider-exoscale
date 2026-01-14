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
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

var _ = Describe("Scenario 3: Taints, Labels & NodePool Selection", Ordered, func() {
	var (
		ctx         context.Context
		nodeClass   *apiv1.ExoscaleNodeClass
		defaultPool *karpenterv1.NodePool
		gpuPool     *karpenterv1.NodePool
		gpuPod      *corev1.Pod
	)

	BeforeAll(func() {
		ctx = context.Background()

		By("Creating ExoscaleNodeClass")
		nodeClass = fixtures.NewNodeClass("nodeselect", fixtures.DefaultNodeClassSpec())
		Expect(framework.Suite.CreateNodeClass(ctx, nodeClass)).To(Succeed())

		Eventually(framework.Suite.IsNodeClassReady(nodeClass.Name)).
			WithTimeout(2 * time.Minute).
			WithPolling(5 * time.Second).
			Should(BeTrue())

		By("Creating default NodePool (NodePool A)")
		defaultPool = fixtures.NewNodePoolWithInstanceTypes("default-pool", nodeClass.Name, "standard.small")
		Expect(framework.Suite.CreateNodePool(ctx, defaultPool)).To(Succeed())

		By("Creating GPU-like NodePool (NodePool B) with taints and labels")
		gpuPool = fixtures.NewGPUNodePool("gpu-pool", nodeClass.Name)
		Expect(framework.Suite.CreateNodePool(ctx, gpuPool)).To(Succeed())
	})

	AfterAll(func() {
		if gpuPod != nil {
			_ = framework.Suite.KubeClient.Delete(ctx, gpuPod)
		}
		if gpuPool != nil {
			_ = framework.Suite.DeleteNodePool(ctx, gpuPool)
		}
		if defaultPool != nil {
			_ = framework.Suite.DeleteNodePool(ctx, defaultPool)
		}
		if nodeClass != nil {
			_ = framework.Suite.DeleteNodeClass(ctx, nodeClass)
		}
	})

	It("should create a node from the GPU NodePool when pod has matching tolerations and selector", func() {
		By("Creating a pod with GPU tolerations and node selector")
		gpuPod = fixtures.NewPodBuilder("gpu-workload").
			WithNodeSelector(fixtures.GPUNodeSelector()).
			WithTolerations(fixtures.GPUTolerations()).
			WithCPU("500m").
			WithMemory("512Mi").
			Build()
		Expect(framework.Suite.KubeClient.Create(ctx, gpuPod)).To(Succeed())

		By("Waiting for a NodeClaim to be created from the GPU pool")
		var gpuNodeClaim *karpenterv1.NodeClaim
		Eventually(func() bool {
			var nodeClaims karpenterv1.NodeClaimList
			if err := framework.Suite.KubeClient.List(ctx, &nodeClaims); err != nil {
				return false
			}

			for _, nc := range nodeClaims.Items {
				if nc.Labels != nil && nc.Labels["team"] == "data-science" {
					gpuNodeClaim = &nc
					return true
				}
			}
			return false
		}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())

		Expect(gpuNodeClaim).NotTo(BeNil())
		GinkgoWriter.Printf("Found GPU NodeClaim: %s\n", gpuNodeClaim.Name)

		By("Waiting for the node to be ready")
		Eventually(func() bool {
			if gpuNodeClaim.Status.NodeName == "" {
				if err := framework.Suite.KubeClient.Get(ctx,
					client.ObjectKey{Name: gpuNodeClaim.Name}, gpuNodeClaim); err != nil {
					return false
				}
				return gpuNodeClaim.Status.NodeName != ""
			}

			var node corev1.Node
			if err := framework.Suite.KubeClient.Get(ctx,
				client.ObjectKey{Name: gpuNodeClaim.Status.NodeName}, &node); err != nil {
				return false
			}

			for _, cond := range node.Status.Conditions {
				if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
					return true
				}
			}
			return false
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())

		By("Tracking the created instance for cleanup")
		inst, err := framework.Suite.GetInstanceByNodeClaimName(ctx, gpuNodeClaim.Name)
		Expect(err).NotTo(HaveOccurred())
		Expect(inst).NotTo(BeNil())
		framework.Suite.TrackInstance(inst.ID.String())
		GinkgoWriter.Printf("Tracked instance %s for cleanup\n", inst.ID)
	})

	It("should have the correct labels on the node", func() {
		var nodeClaims karpenterv1.NodeClaimList
		Expect(framework.Suite.KubeClient.List(ctx, &nodeClaims)).To(Succeed())

		var gpuNode *corev1.Node
		for _, nc := range nodeClaims.Items {
			if nc.Labels != nil && nc.Labels["team"] == "data-science" {
				if nc.Status.NodeName != "" {
					var node corev1.Node
					Expect(framework.Suite.KubeClient.Get(ctx,
						client.ObjectKey{Name: nc.Status.NodeName}, &node)).To(Succeed())
					gpuNode = &node
					break
				}
			}
		}

		Expect(gpuNode).NotTo(BeNil(), "GPU node should exist")
		Expect(gpuNode.Labels["team"]).To(Equal("data-science"))
		Expect(gpuNode.Labels["accelerator"]).To(Equal("gpu"))
	})

	It("should have the correct taints on the node", func() {
		var nodeClaims karpenterv1.NodeClaimList
		Expect(framework.Suite.KubeClient.List(ctx, &nodeClaims)).To(Succeed())

		var gpuNode *corev1.Node
		for _, nc := range nodeClaims.Items {
			if nc.Labels != nil && nc.Labels["team"] == "data-science" {
				if nc.Status.NodeName != "" {
					var node corev1.Node
					Expect(framework.Suite.KubeClient.Get(ctx,
						client.ObjectKey{Name: nc.Status.NodeName}, &node)).To(Succeed())
					gpuNode = &node
					break
				}
			}
		}

		Expect(gpuNode).NotTo(BeNil(), "GPU node should exist")

		hasTaint := false
		for _, taint := range gpuNode.Spec.Taints {
			if taint.Key == "gpu" && taint.Value == "true" && taint.Effect == corev1.TaintEffectNoSchedule {
				hasTaint = true
				break
			}
		}
		Expect(hasTaint).To(BeTrue(), "Node should have gpu=true:NoSchedule taint")
	})

	It("should clean up the node when the pod is deleted", func() {
		By("Deleting the GPU pod")
		Expect(framework.Suite.KubeClient.Delete(ctx, gpuPod)).To(Succeed())
		gpuPod = nil

		By("Waiting for the GPU node to be removed")
		Eventually(func() bool {
			var nodeClaims karpenterv1.NodeClaimList
			if err := framework.Suite.KubeClient.List(ctx, &nodeClaims); err != nil {
				return false
			}

			for _, nc := range nodeClaims.Items {
				if nc.Labels != nil && nc.Labels["team"] == "data-science" {
					return false
				}
			}
			return true
		}).WithTimeout(15 * time.Minute).WithPolling(30 * time.Second).Should(BeTrue())
	})
})
