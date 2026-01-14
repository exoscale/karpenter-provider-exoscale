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

var _ = Describe("Scenario 1: Cluster Bootstrap & Initial Scheduling", Ordered, func() {
	var (
		ctx       context.Context
		nodeClass *apiv1.ExoscaleNodeClass
		nodePool  *karpenterv1.NodePool
	)

	BeforeAll(func() {
		ctx = context.Background()

		By("Creating ExoscaleNodeClass")
		nodeClass = fixtures.NewNodeClass("bootstrap", fixtures.DefaultNodeClassSpec())
		Expect(framework.Suite.CreateNodeClass(ctx, nodeClass)).To(Succeed())

		Eventually(framework.Suite.IsNodeClassReady(nodeClass.Name)).
			WithTimeout(2 * time.Minute).
			WithPolling(5 * time.Second).
			Should(BeTrue())

		By("Creating NodePool with standard.small instances")
		nodePool = fixtures.NewNodePoolWithInstanceTypes("bootstrap", nodeClass.Name, "standard.small")
		Expect(framework.Suite.CreateNodePool(ctx, nodePool)).To(Succeed())
	})

	AfterAll(func() {
		if nodePool != nil {
			_ = framework.Suite.DeleteNodePool(ctx, nodePool)
		}
		if nodeClass != nil {
			_ = framework.Suite.DeleteNodeClass(ctx, nodeClass)
		}
	})

	It("should schedule kube-system pods on Karpenter-provisioned nodes", func() {
		By("Waiting for kube-system pods to be scheduled")

		Eventually(func() bool {
			var pods corev1.PodList
			err := framework.Suite.KubeClient.List(ctx, &pods, client.InNamespace(framework.KarpenterNamespace))
			if err != nil {
				return false
			}

			pendingCount := 0
			for _, pod := range pods.Items {
				if pod.Status.Phase == corev1.PodPending {
					pendingCount++
				}
			}

			GinkgoWriter.Printf("kube-system pods: total=%d, pending=%d\n", len(pods.Items), pendingCount)
			return pendingCount == 0 && len(pods.Items) > 0
		}).WithTimeout(15 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())
	})

	It("should have created NodeClaims for standard.small to handle kube-system pods", func() {
		By("Checking NodeClaim count")

		var createdNodeClaims []string
		Eventually(func() int {
			var nodeClaims karpenterv1.NodeClaimList
			err := framework.Suite.KubeClient.List(ctx, &nodeClaims)
			if err != nil {
				return -1
			}

			count := 0
			createdNodeClaims = []string{}
			for _, nc := range nodeClaims.Items {
				for _, req := range nc.Spec.Requirements {
					if req.Key == corev1.LabelInstanceTypeStable {
						for _, v := range req.Values {
							if v == "standard.small" {
								count++
								createdNodeClaims = append(createdNodeClaims, nc.Name)
								break
							}
						}
					}
				}
			}
			return count
		}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(And(BeNumerically(">=", 2), BeNumerically("<=", 3)))

		By("Tracking created instances for cleanup")
		for _, ncName := range createdNodeClaims {
			inst, err := framework.Suite.GetInstanceByNodeClaimName(ctx, ncName)
			if err == nil && inst != nil {
				framework.Suite.TrackInstance(inst.ID.String())
				GinkgoWriter.Printf("Tracked instance %s for NodeClaim %s\n", inst.ID, ncName)
			}
		}
	})

	It("should have all kube-system pods in Running state", func() {
		By("Verifying pod states")

		Eventually(func() bool {
			var pods corev1.PodList
			err := framework.Suite.KubeClient.List(ctx, &pods, client.InNamespace(framework.KarpenterNamespace))
			if err != nil {
				return false
			}

			for _, pod := range pods.Items {
				if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodSucceeded {
					GinkgoWriter.Printf("Pod %s is in phase %s\n", pod.Name, pod.Status.Phase)
					return false
				}
			}
			return true
		}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())
	})
})
