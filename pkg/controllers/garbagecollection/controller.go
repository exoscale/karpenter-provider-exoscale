package garbagecollection

import (
	"context"
	"fmt"
	"time"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/instance"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

const (
	GCInterval                = 5 * time.Minute
	StuckNodeClaimTimeout     = 10 * time.Minute
	TerminationFinalizerName  = "karpenter.sh/termination"
	OrphanInstanceGracePeriod = 15 * time.Minute
)

type Controller struct {
	Client           client.Client
	InstanceProvider *instance.Provider

	// APIReader is an uncached reader used to confirm that an instance really has
	// no NodeClaim before deleting it. Reading orphan candidates straight from the
	// API server (rather than the informer cache) avoids deleting a live instance
	// off a momentarily stale cache view.
	APIReader client.Reader
}

func StartController(mgr manager.Manager, gc *Controller) error {
	ctrl, err := controller.New("garbage-collection", mgr, controller.Options{
		MaxConcurrentReconciles: 1,
		Reconciler:              gc,
	})
	if err != nil {
		return err
	}

	go func() {
		// NOTE: this controller runs on a timer instead of being event-driven
		// Hence we trigger it on a timer immediately on startup and then at regular intervals
		mgr.GetCache().WaitForCacheSync(context.Background())
		if _, err := ctrl.Reconcile(context.Background(), reconcile.Request{}); err != nil {
			mgr.GetLogger().Error(err, "garbage collection reconcile failed")
		}

		ticker := time.NewTicker(GCInterval)
		defer ticker.Stop()
		for range ticker.C {
			mgr.GetCache().WaitForCacheSync(context.Background())
			if _, err := ctrl.Reconcile(context.Background(), reconcile.Request{}); err != nil {
				mgr.GetLogger().Error(err, "garbage collection reconcile failed")
			}
		}
	}()

	return nil
}

func (c *Controller) Reconcile(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
	log.FromContext(ctx).Info("starting garbage collection")

	cloudInstances, err := c.InstanceProvider.List(ctx)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to list cloud instances: %w", err)
	}

	log.FromContext(ctx).Info("listed cloud instances", "count", len(cloudInstances))

	// Read NodeClaims straight from the API server rather than the informer cache.
	var nodeClaims karpenterv1.NodeClaimList
	if err := c.APIReader.List(ctx, &nodeClaims); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to list NodeClaims: %w", err)
	}

	nodeClaimProviderIDs := make(map[string]bool)
	nodeClaimNames := make(map[string]bool)
	for _, nc := range nodeClaims.Items {
		nodeClaimNames[nc.Name] = true

		instanceID, err := utils.ParseProviderID(nc.Status.ProviderID)
		if err != nil {
			// A NodeClaim whose ProviderID isn't set/parseable yet (e.g. still being
			// created) is matched by name below instead, so this isn't fatal.
			log.FromContext(ctx).V(1).Info("NodeClaim has no parseable provider ID yet", "nodeClaim", nc.Name)
			continue
		}
		nodeClaimProviderIDs[instanceID] = true
	}

	// Delete instances that have no matching NodeClaim. An instance is only a true
	// orphan when it matches neither by ProviderID nor by node-claim label name
	// (the name match covers a NodeClaim whose ProviderID isn't populated yet,
	// e.g. mid-creation) and is past the provisioning grace period (cheap
	// defense-in-depth against transient races at creation time).
	for _, inst := range cloudInstances {
		if hasMatchingNodeClaim(inst, nodeClaimProviderIDs, nodeClaimNames) {
			continue
		}
		if !isOrphanInstanceReapable(inst.CreatedAt, time.Now(), OrphanInstanceGracePeriod) {
			log.FromContext(ctx).V(1).Info("skipping orphaned instance within grace period",
				"instanceID", inst.ID,
				"name", inst.Name,
				"age", time.Since(inst.CreatedAt))
			continue
		}

		log.FromContext(ctx).Info("deleting orphaned instance",
			"instanceID", inst.ID,
			"name", inst.Name,
			"age", time.Since(inst.CreatedAt))

		if err := c.InstanceProvider.Delete(ctx, inst.ID); err != nil {
			if c.InstanceProvider.IsNotFoundError(err) {
				log.FromContext(ctx).Info("instance already deleted", "instanceID", inst.ID)
				continue
			}
			return reconcile.Result{}, err
		}
	}

	cloudInstanceIDs := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudInstanceIDs[inst.ID] = true
	}

	stuckCount := 0
	for _, nc := range nodeClaims.Items {
		if nc.DeletionTimestamp.IsZero() {
			continue
		}

		if !hasTerminationFinalizer(&nc) {
			continue
		}

		instanceID, err := utils.ParseProviderID(nc.Status.ProviderID)
		if err != nil {
			log.FromContext(ctx).V(1).Info("skipping NodeClaim with invalid provider ID", "nodeClaim", nc.Name)
			continue
		}

		if cloudInstanceIDs[instanceID] {
			log.FromContext(ctx).V(1).Info("skipping NodeClaim with existing instance", "nodeClaim", nc.Name, "instanceID", instanceID)
			continue
		}

		timeSinceDeletion := time.Since(nc.DeletionTimestamp.Time)
		if timeSinceDeletion < StuckNodeClaimTimeout {
			log.FromContext(ctx).V(1).Info("skipping NodeClaim not stuck long enough", "nodeClaim", nc.Name, "timeSinceDeletion", timeSinceDeletion)
			continue
		}

		stuckCount++
		log.FromContext(ctx).Info("removing finalizer from stuck NodeClaim",
			"nodeClaim", nc.Name,
			"instanceID", instanceID,
			"timeSinceDeletion", timeSinceDeletion)

		if err := c.removeTerminationFinalizer(ctx, &nc); err != nil {
			log.FromContext(ctx).Error(err, "failed to remove finalizer from stuck NodeClaim",
				"nodeClaim", nc.Name)
		}
	}

	log.FromContext(ctx).Info("garbage collection completed", "stuckNodeClaimsProcessed", stuckCount)

	return reconcile.Result{}, nil
}

// hasMatchingNodeClaim reports whether a cloud instance still corresponds to a
// known NodeClaim. It matches first on the NodeClaim ProviderID and then falls
// back to the instance's node-claim label against the set of NodeClaim names.
func hasMatchingNodeClaim(inst *instance.Instance, providerIDs, names map[string]bool) bool {
	if providerIDs[inst.ID] {
		return true
	}
	if name := inst.Labels[constants.InstanceLabelNodeClaim]; name != "" && names[name] {
		return true
	}
	return false
}

// isOrphanInstanceReapable reports whether an orphaned cloud instance (one with no
// matching NodeClaim) is old enough to be safely garbage collected. Instances that
// are younger than gracePeriod or whose creation time is unknown (zero), are
// spared, to avoid deleting instances that are still being provisioned.
func isOrphanInstanceReapable(createdAt, now time.Time, gracePeriod time.Duration) bool {
	if createdAt.IsZero() {
		return false
	}
	return now.Sub(createdAt) >= gracePeriod
}

func hasTerminationFinalizer(nc *karpenterv1.NodeClaim) bool {
	for _, f := range nc.Finalizers {
		if f == TerminationFinalizerName {
			return true
		}
	}
	return false
}

func (c *Controller) removeTerminationFinalizer(ctx context.Context, nc *karpenterv1.NodeClaim) error {
	patch := client.MergeFrom(nc.DeepCopy())

	var finalizers []string
	for _, f := range nc.Finalizers {
		if f != TerminationFinalizerName {
			finalizers = append(finalizers, f)
		}
	}
	nc.Finalizers = finalizers

	if err := c.Client.Patch(ctx, nc, patch); err != nil {
		return fmt.Errorf("patching NodeClaim finalizers: %w", err)
	}
	return nil
}
