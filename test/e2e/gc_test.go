package e2e_test

import (
	"context"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/controllers/garbagecollection"
	"github.com/exoscale/karpenter-provider-exoscale/test/e2e/infra"
)

var (
	orphanID      egov3.UUID
	orphanCreated time.Time
)

// createOrphanInstance runs from BeforeSuite so the garbage collector's grace
// period elapses while the other specs run.
func createOrphanInstance(ctx context.Context) {
	instanceType, err := infra.InstanceType(ctx, exo, "standard", "small")
	Expect(err).NotTo(HaveOccurred())
	tmpl, err := exo.GetActiveNodepoolTemplate(ctx, k8sVersion, egov3.GetActiveNodepoolTemplateVariantStandard)
	Expect(err).NotTo(HaveOccurred())

	op, err := exo.CreateInstance(ctx, egov3.CreateInstanceRequest{
		Name:         runName + "-orphan",
		InstanceType: &egov3.InstanceType{ID: instanceType},
		Template:     &egov3.Template{ID: tmpl.ActiveTemplate},
		DiskSize:     50,
		// Labels of a Karpenter instance whose NodeClaim no longer exists.
		Labels: egov3.Labels{
			constants.InstanceLabelManagedBy: constants.ManagedByKarpenter,
			constants.InstanceLabelClusterID: cluster.ID.String(),
			constants.InstanceLabelNodeClaim: "e2e-orphan",
		},
	})
	Expect(err).NotTo(HaveOccurred())
	op, err = exo.Wait(ctx, op, egov3.OperationStateSuccess)
	Expect(err).NotTo(HaveOccurred())
	orphanID, orphanCreated = op.Reference.ID, time.Now()
}

var _ = Describe("Garbage collection", func() {
	It("deletes a Karpenter instance that has no NodeClaim", func(ctx SpecContext) {
		deadline := orphanCreated.Add(garbagecollection.OrphanInstanceGracePeriod + garbagecollection.GCInterval + 5*time.Minute)
		expectInstanceGone(ctx, orphanID, time.Until(deadline))
	}, SpecTimeout(30*time.Minute))
})
