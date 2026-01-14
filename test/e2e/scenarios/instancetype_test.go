package scenarios_test

import (
	"context"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/test/e2e/fixtures"
	"github.com/exoscale/karpenter-exoscale/test/e2e/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

var _ = Describe("Scenario 8: Instance Type Selection", Ordered, func() {
	var (
		ctx       context.Context
		nodeClass *apiv1.ExoscaleNodeClass
		nodePool  *karpenterv1.NodePool
		pod       *corev1.Pod
	)

	BeforeAll(func() {
		ctx = context.Background()

		By("Creating ExoscaleNodeClass")
		nodeClass = fixtures.NewNodeClass("instancetype", fixtures.DefaultNodeClassSpec())
		Expect(framework.Suite.CreateNodeClass(ctx, nodeClass)).To(Succeed())

		Eventually(framework.Suite.IsNodeClassReady(nodeClass.Name)).
			WithTimeout(2 * time.Minute).
			WithPolling(5 * time.Second).
			Should(BeTrue())

		By("Creating NodePool with multiple instance types")
		nodePool = fixtures.NewNodePoolBuilder("instancetype", nodeClass.Name).
			WithInstanceTypes("standard.small", "standard.medium", "standard.large").
			WithLimits("20", "40Gi").
			Build()
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

	It("should select an appropriate instance type based on resource requests", func() {
		By("Creating a pod with high resource requests (exceeds standard.small)")
		pod = fixtures.NewPodBuilder("large-resource-pod").
			WithCPU("4").
			WithMemory("8Gi").
			Build()
		Expect(framework.Suite.KubeClient.Create(ctx, pod)).To(Succeed())

		By("Waiting for a NodeClaim to be created")
		var nodeClaim *karpenterv1.NodeClaim
		Eventually(func() bool {
			var nodeClaims karpenterv1.NodeClaimList
			if err := framework.Suite.KubeClient.List(ctx, &nodeClaims); err != nil {
				return false
			}

			for _, nc := range nodeClaims.Items {
				if nc.Spec.NodeClassRef.Name == nodeClass.Name {
					nodeClaim = &nc
					return true
				}
			}
			return false
		}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())

		Expect(nodeClaim).NotTo(BeNil())
		GinkgoWriter.Printf("NodeClaim created: %s\n", nodeClaim.Name)

		By("Waiting for the instance to be running")
		Eventually(framework.Suite.GetInstanceStateFunc(nodeClaim.Name)).
			WithTimeout(framework.Suite.Config.InstanceTimeout).
			WithPolling(10 * time.Second).
			Should(Equal(string(egov3.InstanceStateRunning)))

		By("Verifying that a larger instance type was selected (not standard.small)")
		inst, err := framework.Suite.GetInstanceByNodeClaimName(ctx, nodeClaim.Name)
		Expect(err).NotTo(HaveOccurred())
		Expect(inst).NotTo(BeNil())
		framework.Suite.TrackInstance(inst.ID.String())

		instanceType := string(inst.InstanceType.Family) + "." + string(inst.InstanceType.Size)
		GinkgoWriter.Printf("Selected instance type: %s\n", instanceType)

		Expect(instanceType).NotTo(Equal("standard.small"),
			"Should not have selected standard.small for high resource pod")
		Expect(instanceType).To(Or(
			Equal("standard.medium"),
			Equal("standard.large"),
		), "Should have selected medium or large instance type")

		By("Waiting for the pod to be running")
		Eventually(func() corev1.PodPhase {
			var p corev1.Pod
			if err := framework.Suite.KubeClient.Get(ctx,
				client.ObjectKey{Name: pod.Name, Namespace: pod.Namespace}, &p); err != nil {
				return ""
			}
			return p.Status.Phase
		}).WithTimeout(framework.Suite.Config.NodeJoinTimeout).WithPolling(10 * time.Second).Should(Equal(corev1.PodRunning))

		GinkgoWriter.Printf("Pod is running on instance type: %s\n", instanceType)
	})
})
