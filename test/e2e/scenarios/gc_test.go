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
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

var _ = Describe("Scenario 5: Garbage Collection", Ordered, func() {
	var (
		ctx        context.Context
		nodeClass  *apiv1.ExoscaleNodeClass
		nodePool   *karpenterv1.NodePool
		nodeClaim  *karpenterv1.NodeClaim
		instanceID egov3.UUID
	)

	BeforeAll(func() {
		ctx = context.Background()

		By("Creating ExoscaleNodeClass")
		nodeClass = fixtures.NewNodeClass("gc", fixtures.DefaultNodeClassSpec())
		Expect(framework.Suite.CreateNodeClass(ctx, nodeClass)).To(Succeed())

		Eventually(framework.Suite.IsNodeClassReady(nodeClass.Name)).
			WithTimeout(2 * time.Minute).
			WithPolling(5 * time.Second).
			Should(BeTrue())

		By("Creating NodePool")
		nodePool = fixtures.NewNodePoolWithInstanceTypes("gc", nodeClass.Name, "standard.small")
		Expect(framework.Suite.CreateNodePool(ctx, nodePool)).To(Succeed())
	})

	AfterAll(func() {
		if nodePool != nil {
			_ = framework.Suite.DeleteNodePool(ctx, nodePool)
		}
		if nodeClass != nil {
			_ = framework.Suite.DeleteNodeClass(ctx, nodeClass)
		}

		if instanceID != "" {
			exists, _ := framework.Suite.InstanceExists(ctx, instanceID.String())
			if exists {
				framework.Suite.TrackInstance(instanceID.String())
			}
		}
	})

	It("should clean up orphaned instances", func() {
		By("Creating a NodeClaim manually")
		nodeClaim = fixtures.NewNodeClaim("gc-claim", nodeClass.Name, fixtures.StandardRequirements())
		nodeClaimName := nodeClaim.Name
		Expect(framework.Suite.CreateNodeClaim(ctx, nodeClaim)).To(Succeed())

		By("Waiting for the instance to be created and running")
		Eventually(framework.Suite.GetInstanceStateFunc(nodeClaimName)).
			WithTimeout(framework.Suite.Config.InstanceTimeout).
			WithPolling(10 * time.Second).
			Should(Equal(string(egov3.InstanceStateRunning)))

		inst, err := framework.Suite.GetInstanceByNodeClaimName(ctx, nodeClaimName)
		Expect(err).NotTo(HaveOccurred())
		Expect(inst).NotTo(BeNil())
		instanceID = inst.ID
		GinkgoWriter.Printf("Instance created: %s\n", instanceID)

		By("Deleting the NodeClaim from Kubernetes (leaving orphaned instance)")
		Expect(framework.Suite.DeleteNodeClaim(ctx, nodeClaim)).To(Succeed())
		nodeClaim = nil

		By("Waiting for the NodeClaim to be deleted from Kubernetes")
		Eventually(func() bool {
			var nc karpenterv1.NodeClaim
			err := framework.Suite.KubeClient.Get(ctx,
				client.ObjectKey{Name: nodeClaimName}, &nc)
			return err != nil
		}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(BeTrue())

		By("Waiting for GC controller to delete the orphaned instance (GC runs every 5 min)")
		Eventually(framework.Suite.InstanceExistsFunc(instanceID.String())).
			WithTimeout(10 * time.Minute).
			WithPolling(30 * time.Second).
			Should(BeFalse())

		GinkgoWriter.Println("Orphaned instance was cleaned up by GC controller")
		instanceID = ""
	})
})
