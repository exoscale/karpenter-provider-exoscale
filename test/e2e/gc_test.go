package e2e_test

import (
	"context"
	"maps"
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
// period elapses while the other specs run. It has no NodeClaim label: NodeClass
// finalizers only remove instances whose label names a missing NodeClaim, so
// only the garbage collector can delete this one.
func createOrphanInstance(ctx context.Context) {
	orphanID = createKarpenterInstance(ctx, runName+"-orphan", nil)
	orphanCreated = time.Now()
}

// createKarpenterInstance creates an instance carrying the labels Karpenter
// puts on the instances it manages in this cluster, plus extra.
func createKarpenterInstance(ctx context.Context, name string, extra egov3.Labels) egov3.UUID {
	instanceType, err := infra.InstanceType(ctx, exo, "standard", "small")
	Expect(err).NotTo(HaveOccurred())
	tmpl, err := exo.GetActiveNodepoolTemplate(ctx, k8sVersion, egov3.GetActiveNodepoolTemplateVariantStandard)
	Expect(err).NotTo(HaveOccurred())

	labels := egov3.Labels{
		constants.InstanceLabelManagedBy: constants.ManagedByKarpenter,
		constants.InstanceLabelClusterID: clusterID,
	}
	maps.Copy(labels, extra)
	op, err := exo.CreateInstance(ctx, egov3.CreateInstanceRequest{
		Name:         name,
		InstanceType: &egov3.InstanceType{ID: instanceType},
		Template:     &egov3.Template{ID: tmpl.ActiveTemplate},
		DiskSize:     50,
		Labels:       labels,
	})
	Expect(err).NotTo(HaveOccurred())
	op, err = exo.Wait(ctx, op, egov3.OperationStateSuccess)
	Expect(err).NotTo(HaveOccurred())
	return op.Reference.ID
}

var _ = Describe("Garbage collection", func() {
	It("deletes a Karpenter instance that has no NodeClaim", func(ctx SpecContext) {
		deadline := orphanCreated.Add(garbagecollection.OrphanInstanceGracePeriod + garbagecollection.GCInterval + 5*time.Minute)
		expectInstanceGone(ctx, orphanID, time.Until(deadline))
	}, SpecTimeout(30*time.Minute))
})
