package garbagecollection

import (
	"context"
	"fmt"
	"time"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/instance"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/utils"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

const (
	GCInterval                = 5 * time.Minute
	StuckNodeClaimTimeout     = 10 * time.Minute
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

	// Timer-driven, not event-driven: run once at startup, then every GCInterval.
	go wait.Until(func() {
		mgr.GetCache().WaitForCacheSync(context.Background())
		if _, err := ctrl.Reconcile(context.Background(), reconcile.Request{}); err != nil {
			mgr.GetLogger().Error(err, "garbage collection reconcile failed")
		}
	}, GCInterval, wait.NeverStop)

	return nil
}

func (c *Controller) Reconcile(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
	log.FromContext(ctx).Info("starting garbage collection")

	cloudInstances, err := c.InstanceProvider.List(ctx)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to list cloud instances: %w", err)
	}

	log.FromContext(ctx).Info("listed cloud instances", "count", len(cloudInstances))

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

	// The name match covers a NodeClaim whose ProviderID isn't populated yet; the
	// grace period guards against races with instances still being created.
	cloudInstanceIDs := make(map[string]bool)
	for _, inst := range cloudInstances {
		cloudInstanceIDs[inst.ID] = true
		if hasMatchingNodeClaim(inst, nodeClaimProviderIDs, nodeClaimNames) {
			continue
		}
		if !isOrphanInstanceReapable(inst.CreatedAt, time.Now()) {
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

	stuckCount := 0
	for _, nc := range nodeClaims.Items {
		if nc.DeletionTimestamp.IsZero() || !controllerutil.ContainsFinalizer(&nc, karpenterv1.TerminationFinalizer) {
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

		patch := client.MergeFrom(nc.DeepCopy())
		controllerutil.RemoveFinalizer(&nc, karpenterv1.TerminationFinalizer)
		if err := c.Client.Patch(ctx, &nc, patch); err != nil {
			log.FromContext(ctx).Error(err, "failed to remove finalizer from stuck NodeClaim",
				"nodeClaim", nc.Name)
		}
	}

	log.FromContext(ctx).Info("garbage collection completed", "stuckNodeClaimsProcessed", stuckCount)

	return reconcile.Result{}, nil
}

func hasMatchingNodeClaim(inst *instance.Instance, providerIDs, names map[string]bool) bool {
	return providerIDs[inst.ID] || names[inst.Labels[constants.InstanceLabelNodeClaim]]
}

// isOrphanInstanceReapable spares instances with an unknown (zero) creation time.
func isOrphanInstanceReapable(createdAt, now time.Time) bool {
	return !createdAt.IsZero() && now.Sub(createdAt) >= OrphanInstanceGracePeriod
}
