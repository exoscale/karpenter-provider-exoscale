package scenarios_test

import (
	"context"
	"time"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/test/e2e/fixtures"
	"github.com/exoscale/karpenter-exoscale/test/e2e/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

var _ = Describe("Scenario 2: Scale-up on Resource Pressure", Ordered, func() {
	var (
		ctx        context.Context
		nodeClass  *apiv1.ExoscaleNodeClass
		nodePool   *karpenterv1.NodePool
		deployment *appsv1.Deployment
	)

	BeforeAll(func() {
		ctx = context.Background()

		By("Creating ExoscaleNodeClass")
		nodeClass = fixtures.NewNodeClass("scaleup", fixtures.DefaultNodeClassSpec())
		Expect(framework.Suite.CreateNodeClass(ctx, nodeClass)).To(Succeed())

		Eventually(framework.Suite.IsNodeClassReady(nodeClass.Name)).
			WithTimeout(2 * time.Minute).
			WithPolling(5 * time.Second).
			Should(BeTrue())

		By("Creating NodePool with standard.small instances")
		nodePool = fixtures.NewNodePoolWithInstanceTypes("scaleup", nodeClass.Name, "standard.small")
		Expect(framework.Suite.CreateNodePool(ctx, nodePool)).To(Succeed())

		By("Waiting for initial nodes to be ready")
		Eventually(func() int {
			var nodes corev1.NodeList
			if err := framework.Suite.KubeClient.List(ctx, &nodes); err != nil {
				return 0
			}
			readyCount := 0
			for _, node := range nodes.Items {
				for _, cond := range node.Status.Conditions {
					if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
						readyCount++
						break
					}
				}
			}
			return readyCount
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(BeNumerically(">=", 1))
	})

	AfterAll(func() {
		if deployment != nil {
			_ = framework.Suite.KubeClient.Delete(ctx, deployment)
		}
		if nodePool != nil {
			_ = framework.Suite.DeleteNodePool(ctx, nodePool)
		}
		if nodeClass != nil {
			_ = framework.Suite.DeleteNodeClass(ctx, nodeClass)
		}
	})

	It("should create a new node when resource pressure increases", func() {
		By("Getting initial node count")
		var initialNodeClaims karpenterv1.NodeClaimList
		Expect(framework.Suite.KubeClient.List(ctx, &initialNodeClaims)).To(Succeed())
		initialCount := len(initialNodeClaims.Items)
		GinkgoWriter.Printf("Initial NodeClaim count: %d\n", initialCount)

		By("Creating a deployment with high CPU request")
		deployment = fixtures.NewDeploymentBuilder("resource-pressure").
			WithCPU("2").
			WithMemory("4Gi").
			Build()
		Expect(framework.Suite.KubeClient.Create(ctx, deployment)).To(Succeed())

		By("Waiting for new NodeClaim to be created")
		Eventually(func() int {
			var nodeClaims karpenterv1.NodeClaimList
			if err := framework.Suite.KubeClient.List(ctx, &nodeClaims); err != nil {
				return -1
			}
			return len(nodeClaims.Items)
		}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeNumerically(">", initialCount))

		By("Waiting for the pod to be scheduled and running")
		Eventually(func() bool {
			var pods corev1.PodList
			if err := framework.Suite.KubeClient.List(ctx, &pods,
				client.InNamespace(deployment.Namespace),
				client.MatchingLabels{"app": deployment.Name}); err != nil {
				return false
			}

			if len(pods.Items) == 0 {
				return false
			}

			for _, pod := range pods.Items {
				if pod.Status.Phase != corev1.PodRunning {
					return false
				}
			}
			return true
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())

		By("Tracking created instances for cleanup")
		var nodeClaims karpenterv1.NodeClaimList
		Expect(framework.Suite.KubeClient.List(ctx, &nodeClaims)).To(Succeed())
		for _, nc := range nodeClaims.Items {
			inst, err := framework.Suite.GetInstanceByNodeClaimName(ctx, nc.Name)
			if err == nil && inst != nil {
				framework.Suite.TrackInstance(inst.ID.String())
				GinkgoWriter.Printf("Tracked instance %s for NodeClaim %s\n", inst.ID, nc.Name)
			}
		}
	})

	It("should remove the node when the deployment is deleted", func() {
		By("Getting current NodeClaim count")
		var currentNodeClaims karpenterv1.NodeClaimList
		Expect(framework.Suite.KubeClient.List(ctx, &currentNodeClaims)).To(Succeed())
		countBefore := len(currentNodeClaims.Items)
		GinkgoWriter.Printf("NodeClaim count before deletion: %d\n", countBefore)

		By("Deleting the deployment")
		Expect(framework.Suite.KubeClient.Delete(ctx, deployment)).To(Succeed())
		deployment = nil

		By("Waiting for consolidation to remove the extra node")
		Eventually(func() int {
			var nodeClaims karpenterv1.NodeClaimList
			if err := framework.Suite.KubeClient.List(ctx, &nodeClaims); err != nil {
				return -1
			}
			return len(nodeClaims.Items)
		}).WithTimeout(15 * time.Minute).WithPolling(30 * time.Second).Should(BeNumerically("<", countBefore))
	})
})
