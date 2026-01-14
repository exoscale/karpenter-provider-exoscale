package scenarios_test

import (
	"context"
	"fmt"
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

var _ = Describe("Scenario 7: Consolidation", Ordered, func() {
	var (
		ctx         context.Context
		nodeClass   *apiv1.ExoscaleNodeClass
		nodePool    *karpenterv1.NodePool
		deployments []*appsv1.Deployment
	)

	BeforeAll(func() {
		ctx = context.Background()
		deployments = make([]*appsv1.Deployment, 0)

		By("Creating ExoscaleNodeClass")
		nodeClass = fixtures.NewNodeClass("consolidation", fixtures.DefaultNodeClassSpec())
		Expect(framework.Suite.CreateNodeClass(ctx, nodeClass)).To(Succeed())

		Eventually(framework.Suite.IsNodeClassReady(nodeClass.Name)).
			WithTimeout(2 * time.Minute).
			WithPolling(5 * time.Second).
			Should(BeTrue())

		By("Creating NodePool with standard.small instances")
		nodePool = fixtures.NewNodePoolBuilder("consolidation", nodeClass.Name).
			WithInstanceTypes("standard.small").
			WithLimits("20", "40Gi").
			Build()
		Expect(framework.Suite.CreateNodePool(ctx, nodePool)).To(Succeed())
	})

	AfterAll(func() {
		for _, d := range deployments {
			_ = framework.Suite.KubeClient.Delete(ctx, d)
		}
		if nodePool != nil {
			_ = framework.Suite.DeleteNodePool(ctx, nodePool)
		}
		if nodeClass != nil {
			_ = framework.Suite.DeleteNodeClass(ctx, nodeClass)
		}
	})

	It("should consolidate underutilized nodes", func() {
		By("Creating 3 deployments to spread across multiple nodes")
		for i := 0; i < 3; i++ {
			dep := fixtures.NewDeploymentBuilder(fmt.Sprintf("consolidation-app-%d", i)).
				WithCPU("500m").
				WithMemory("512Mi").
				Build()

			Expect(framework.Suite.KubeClient.Create(ctx, dep)).To(Succeed())
			deployments = append(deployments, dep)
		}

		By("Waiting for all pods to be running")
		for _, dep := range deployments {
			Eventually(func() bool {
				var pods corev1.PodList
				if err := framework.Suite.KubeClient.List(ctx, &pods,
					client.InNamespace(dep.Namespace),
					client.MatchingLabels{"app": dep.Name}); err != nil {
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
		}

		By("Counting initial nodes")
		var initialNodes corev1.NodeList
		Expect(framework.Suite.KubeClient.List(ctx, &initialNodes)).To(Succeed())

		karpenterNodeCount := 0
		for _, node := range initialNodes.Items {
			if _, ok := node.Labels[karpenterv1.NodePoolLabelKey]; ok {
				karpenterNodeCount++
			}
		}
		GinkgoWriter.Printf("Initial Karpenter node count: %d\n", karpenterNodeCount)

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

		By("Deleting 2 deployments to leave nodes underutilized")
		for i := 0; i < 2; i++ {
			Expect(framework.Suite.KubeClient.Delete(ctx, deployments[i])).To(Succeed())
		}
		deployments = deployments[2:]

		By("Waiting for consolidation to reduce node count")
		Eventually(func() int {
			var nodes corev1.NodeList
			if err := framework.Suite.KubeClient.List(ctx, &nodes); err != nil {
				return -1
			}

			count := 0
			for _, node := range nodes.Items {
				if _, ok := node.Labels[karpenterv1.NodePoolLabelKey]; ok {
					count++
				}
			}
			GinkgoWriter.Printf("Current Karpenter node count: %d\n", count)
			return count
		}).WithTimeout(15 * time.Minute).WithPolling(30 * time.Second).Should(BeNumerically("<", karpenterNodeCount))

		GinkgoWriter.Println("Consolidation completed successfully")
	})
})
