package e2e_test

import (
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
)

// These only check NodeClass status; no instance is launched.
var _ = Describe("Template selection", func() {
	var activeTemplate string

	BeforeEach(func(ctx SpecContext) {
		resp, err := exo.GetActiveNodepoolTemplate(ctx, k8sVersion, egov3.GetActiveNodepoolTemplateVariantStandard)
		Expect(err).NotTo(HaveOccurred())
		activeTemplate = resp.ActiveTemplate.String()
	})

	It("resolves the active SKS template for a pinned version", func(ctx SpecContext) {
		nodeClass := createNodeClass(ctx, "template-version", apiv1.ExoscaleNodeClassSpec{
			ImageTemplateSelector: &apiv1.ImageTemplateSelector{Version: k8sVersion},
			DiskSize:              50,
		})
		expectNodeClassReady(ctx, nodeClass)
		Expect(nodeClass.Status.ImageID).To(Equal(activeTemplate))
	})

	It("uses an explicit templateID", func(ctx SpecContext) {
		nodeClass := createNodeClass(ctx, "template-id", apiv1.ExoscaleNodeClassSpec{TemplateID: activeTemplate, DiskSize: 50})
		expectNodeClassReady(ctx, nodeClass)
		Expect(nodeClass.Status.ImageID).To(Equal(activeTemplate))
	})

	It("is not ready when the templateID does not exist", func(ctx SpecContext) {
		nodeClass := createNodeClass(ctx, "template-missing", apiv1.ExoscaleNodeClassSpec{
			TemplateID: "00000000-0000-0000-0000-000000000000",
			DiskSize:   50,
		})
		Eventually(func(g Gomega) {
			g.Expect(kube.Get(ctx, client.ObjectKeyFromObject(nodeClass), nodeClass)).To(Succeed())
			g.Expect(nodeClass.StatusConditions().Root().IsFalse()).To(BeTrue(), "%+v", nodeClass.Status.Conditions)
		}).WithContext(ctx).WithTimeout(2 * time.Minute).Should(Succeed())
	})
})
