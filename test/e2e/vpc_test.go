package e2e_test

import (
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
)

var _ = Describe("VPC", func() {
	const name = "vpc"

	It("attaches the instance to the VPC subnet selected by name", func(ctx SpecContext) {
		vpcName, subnetName := runName+"-vpc", runName+"-subnet"

		op, err := exo.CreateVpc(ctx, egov3.CreateVpcRequest{Name: vpcName})
		Expect(err).NotTo(HaveOccurred())
		op, err = exo.Wait(ctx, op, egov3.OperationStateSuccess)
		Expect(err).NotTo(HaveOccurred())
		vpcID := op.Reference.ID

		op, err = exo.CreateSubnet(ctx, vpcID, egov3.CreateSubnetRequest{
			Name:          subnetName,
			AddressSpace:  egov3.CreateSubnetRequestAddressSpacePrivate,
			Addressfamily: egov3.CreateSubnetRequestAddressfamilyInet4,
			Ipv4Block:     "10.42.0.0/24",
		})
		Expect(err).NotTo(HaveOccurred())
		op, err = exo.Wait(ctx, op, egov3.OperationStateSuccess)
		Expect(err).NotTo(HaveOccurred())
		subnetID := op.Reference.ID

		// Registered first so it runs last, once the instance left the subnet.
		DeferCleanup(func(ctx SpecContext) {
			expectNoNodeClaims(ctx, name)
			Expect(exo.DeleteSubnet(ctx, vpcID, subnetID)).To(Succeed())
			Expect(exo.DeleteVpc(ctx, vpcID)).To(Succeed())
		}, NodeTimeout(15*time.Minute))

		spec := defaultNodeClassSpec()
		spec.VpcSelector = &apiv1.SelectorTerms{Name: vpcName}
		spec.VpcSubnetSelectorTerms = []apiv1.SelectorTerms{{Name: subnetName}}
		nodeClass := createNodeClass(ctx, name, spec)
		expectNodeClassReady(ctx, nodeClass)
		Expect(nodeClass.Status.VpcID).To(Equal(vpcID.String()))
		Expect(nodeClass.Status.VpcSubnets).To(ConsistOf(subnetID.String()))

		createNodePool(ctx, name)
		createDeployment(ctx, name, name, 1)
		claim := expectSingleInitializedNodeClaim(ctx, name)

		instance := instanceOf(ctx, claim)
		Expect(instance.Vpc).NotTo(BeNil())
		Expect(instance.Vpc.ID).To(Equal(vpcID))
		Expect(instance.Vpc.Subnets).To(ContainElement(HaveField("ID", subnetID)))
	}, SpecTimeout(25*time.Minute))
})
