package e2e_test

import (
	"context"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
)

var _ = Describe("Security groups", func() {
	const name = "security-groups"

	// The SKS default security group stays attached (USE_SKS_DEFAULT_SECURITY_GROUP
	// is on, as in production), so nodes can still reach the cluster.
	It("attaches the selected security groups and replaces the node when they change", func(ctx SpecContext) {
		sgs := make([]egov3.UUID, 2)
		for i, suffix := range []string{"-sg-a", "-sg-b"} {
			sgs[i] = createExoscaleResource(ctx, name, func() (*egov3.Operation, error) {
				return exo.CreateSecurityGroup(ctx, egov3.CreateSecurityGroupRequest{Name: runName + suffix})
			}, func(ctx context.Context, id egov3.UUID) (*egov3.Operation, error) {
				return exo.DeleteSecurityGroup(ctx, id)
			})
		}
		sks, err := exo.GetSKSCluster(ctx, egov3.UUID(clusterID))
		Expect(err).NotTo(HaveOccurred())
		defaultSG := *sks.DefaultSecurityGroupID

		spec := defaultNodeClassSpec()
		spec.SecurityGroupSelectorTerms = []apiv1.SelectorTerms{{ID: sgs[0].String()}}
		nodeClass := createNodeClass(ctx, name, spec)
		expectNodeClassReady(ctx, nodeClass)
		Expect(nodeClass.Status.SecurityGroups).To(ConsistOf(sgs[0].String()))

		createNodePool(ctx, name)
		createDeployment(ctx, name, name, 1)
		claim := expectSingleInitializedNodeClaim(ctx, name)
		Expect(instanceOf(ctx, claim).SecurityGroups).To(ConsistOf(HaveField("ID", sgs[0]), HaveField("ID", defaultSG)))

		By("not flagging the SKS default security group as drift")
		Consistently(func(g Gomega) {
			g.Expect(kube.Get(ctx, client.ObjectKeyFromObject(&claim), &claim)).To(Succeed())
			g.Expect(claim.StatusConditions().IsTrue(karpv1.ConditionTypeDrifted)).To(BeFalse(), "%+v", claim.Status.Conditions)
		}).WithContext(ctx).WithTimeout(time.Minute).WithPolling(10 * time.Second).Should(Succeed())

		By("selecting another security group by name")
		replacement := expectNodeReplaced(ctx, nodeClass, name, claim, func(spec *apiv1.ExoscaleNodeClassSpec) {
			spec.SecurityGroupSelectorTerms = []apiv1.SelectorTerms{{Name: runName + "-sg-b"}}
		})
		Expect(instanceOf(ctx, replacement).SecurityGroups).To(ConsistOf(HaveField("ID", sgs[1]), HaveField("ID", defaultSG)))
	}, SpecTimeout(30*time.Minute))
})
