package e2e_test

import (
	"context"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	utilversion "k8s.io/apimachinery/pkg/util/version"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-provider-exoscale/test/e2e/infra"
)

var _ = Describe("Drift", func() {
	const name = "drift"

	// kubelet.maxPods is part of the NodeClass configuration hash, so changing
	// it marks existing NodeClaims as drifted (user-data drift).
	It("replaces a node when its NodeClass configuration changes", func(ctx SpecContext) {
		nodeClass := createNodeClass(ctx, name, defaultNodeClassSpec())
		expectNodeClassReady(ctx, nodeClass)
		createNodePool(ctx, name)
		createDeployment(ctx, name, name, 1)

		old := expectSingleInitializedNodeClaim(ctx, name)
		oldInstance := instanceOf(ctx, old)
		oldHash := nodeClass.Status.ConfigurationHash

		patch := client.MergeFrom(nodeClass.DeepCopy())
		nodeClass.Spec.Kubelet.MaxPods = new(int32(50))
		Expect(kube.Patch(ctx, nodeClass, patch)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(kube.Get(ctx, client.ObjectKeyFromObject(nodeClass), nodeClass)).To(Succeed())
			g.Expect(nodeClass.Status.ConfigurationHash).NotTo(Equal(oldHash))
		}).WithContext(ctx).WithTimeout(2 * time.Minute).Should(Succeed())

		replacement := expectSingleInitializedNodeClaim(ctx, name, old.Name)
		expectPodsRunningOn(ctx, name, 1, replacement.Status.NodeName)
		expectInstanceGone(ctx, oldInstance.ID, 5*time.Minute)
	}, SpecTimeout(25*time.Minute))

	// The cluster-upgrade path: nodes launched from the previous version's
	// template move to the current one. Kubelets may lag the control plane.
	It("replaces a node when the template version changes", func(ctx SpecContext) {
		const name = "template-drift"
		older := previousVersion(ctx)
		if older == "" {
			Skip("SKS offers no version older than " + k8sVersion)
		}
		current, err := exo.GetActiveNodepoolTemplate(ctx, k8sVersion, egov3.GetActiveNodepoolTemplateVariantStandard)
		Expect(err).NotTo(HaveOccurred())

		nodeClass := createNodeClass(ctx, name, apiv1.ExoscaleNodeClassSpec{
			ImageTemplateSelector: &apiv1.ImageTemplateSelector{Version: older},
			DiskSize:              50,
		})
		expectNodeClassReady(ctx, nodeClass)
		createNodePool(ctx, name)
		createDeployment(ctx, name, name, 1)

		old := expectSingleInitializedNodeClaim(ctx, name)
		Expect(old.Status.ImageID).NotTo(Equal(current.ActiveTemplate.String()))
		replacement := expectNodeReplaced(ctx, nodeClass, name, old, func(spec *apiv1.ExoscaleNodeClassSpec) {
			spec.ImageTemplateSelector.Version = k8sVersion
		})
		Expect(replacement.Status.ImageID).To(Equal(current.ActiveTemplate.String()))
		Expect(instanceOf(ctx, replacement).Template.ID).To(Equal(current.ActiveTemplate))
	}, SpecTimeout(25*time.Minute))
})

// previousVersion returns the newest SKS version older than the cluster's, or "".
func previousVersion(ctx context.Context) string {
	versions, err := infra.Versions(ctx, exo)
	Expect(err).NotTo(HaveOccurred())
	current := utilversion.MustParseGeneric(k8sVersion)
	var older *utilversion.Version
	for _, v := range versions {
		parsed := utilversion.MustParseGeneric(v)
		if parsed.LessThan(current) && (older == nil || older.LessThan(parsed)) {
			older = parsed
		}
	}
	if older == nil {
		return ""
	}
	return older.String()
}
