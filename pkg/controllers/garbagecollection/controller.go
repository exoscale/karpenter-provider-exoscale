package garbagecollection

import (
	"context"
	"fmt"
	"time"

	"github.com/exoscale/karpenter-exoscale/pkg/errors"
	"github.com/exoscale/karpenter-exoscale/pkg/metrics"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instance"
	"github.com/exoscale/karpenter-exoscale/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/events"
)

const (
	GCInterval       = 5 * time.Minute
	OrphanedDuration = 15 * time.Minute
)

type Controller struct {
	client           client.Client
	instanceProvider instance.Provider
	events           events.Recorder
}

func NewController(mgr manager.Manager, instanceProvider instance.Provider) error {
	gc := &Controller{
		client:           mgr.GetClient(),
		instanceProvider: instanceProvider,
		events:           events.NewRecorder(mgr.GetEventRecorderFor("karpenter-exoscale.garbage-collection")),
	}

	ctrl, err := controller.New("garbage-collection", mgr, controller.Options{
		MaxConcurrentReconciles: 1,
		Reconciler:              gc,
	})
	if err != nil {
		return err
	}

	go func() {
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
	logger := log.FromContext(ctx)
	logger.V(1).Info("starting garbage collection")

	cloudInstances, err := c.instanceProvider.List(ctx)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to list cloud instances: %w", err)
	}

	var nodeClaims karpenterv1.NodeClaimList
	if err := c.client.List(ctx, &nodeClaims); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to list NodeClaims: %w", err)
	}

	nodeClaimProviderIDs := make(map[string]bool)
	for _, nc := range nodeClaims.Items {
		if nc.Status.ProviderID != "" {
			instanceID, err := utils.ParseProviderID(nc.Status.ProviderID)
			if err != nil {
				logger.Error(err, "invalid provider ID in NodeClaim", "nodeClaim", nc.Name)
				continue
			}
			nodeClaimProviderIDs[instanceID] = true
		}
	}

	orphanedCount := 0
	deletedCount := 0
	for _, inst := range cloudInstances {
		if nodeClaimProviderIDs[inst.ID] {
			continue
		}

		orphanedDuration := time.Since(inst.CreatedAt)
		if orphanedDuration < OrphanedDuration {
			logger.V(2).Info("instance is orphaned but not old enough for deletion",
				"instanceID", inst.ID,
				"name", inst.Name,
				"age", orphanedDuration,
				"threshold", OrphanedDuration)
			orphanedCount++
			continue
		}

		logger.Info("deleting orphaned instance",
			"instanceID", inst.ID,
			"name", inst.Name,
			"age", orphanedDuration)

		if err := c.instanceProvider.Delete(ctx, inst.ID); err != nil {
			if errors.IsInstanceNotFoundError(err) {
				// Instance already deleted, ignore
				continue
			}
			logger.Error(err, "failed to delete orphaned instance",
				"instanceID", inst.ID,
				"name", inst.Name)
			continue
		}
		deletedCount++
		metrics.OrphanedInstancesCleanedTotal.Inc(map[string]string{})
	}

	metrics.OrphanedInstancesCount.Set(float64(orphanedCount), map[string]string{})

	if deletedCount > 0 || orphanedCount > 0 {
		logger.Info("garbage collection completed",
			"orphaned", orphanedCount,
			"deleted", deletedCount,
			"total", len(cloudInstances))
	} else {
		logger.V(1).Info("garbage collection completed, no orphaned instances found",
			"total", len(cloudInstances))
	}

	return reconcile.Result{}, nil
}
