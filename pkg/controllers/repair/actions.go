package repair

import (
	"context"
	"fmt"
	"strings"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/karpenter-exoscale/pkg/metrics"
	"github.com/exoscale/karpenter-exoscale/pkg/utils"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	karpentermetrics "sigs.k8s.io/karpenter/pkg/metrics"
)

func (r *NodeRepairController) performRepairActions(ctx context.Context, node *v1.Node, nodeClaim *karpenterv1.NodeClaim, reason string) error {
	logger := log.FromContext(ctx).WithValues("node", node.Name)

	metrics.RepairActionsTotal.Inc(map[string]string{karpentermetrics.ReasonLabel: reason})

	switch {
	case strings.Contains(reason, "NodeReady"):
		logger.Info("NodeReady condition failed - attempting instance soft reboot")
		return r.rebootNode(ctx, nodeClaim)

	case strings.Contains(reason, "DiskPressure"):
		logger.Info("DiskPressure detected - attempting to evict pods and clean disk")
		if err := r.evictPodsForDiskCleanup(ctx, node); err != nil {
			logger.Error(err, "failed to evict pods for disk cleanup")
		}

		logger.Info("performing node reboot to clean up disk space")
		return r.rebootNode(ctx, nodeClaim)

	case strings.Contains(reason, "MemoryPressure"):
		logger.Info("MemoryPressure detected - evicting non-critical pods")
		if err := r.evictNonCriticalPods(ctx, node); err != nil {
			logger.Error(err, "failed to evict non-critical pods")
			logger.Info("falling back to node reboot")
			return r.rebootNode(ctx, nodeClaim)
		}
		return nil

	case strings.Contains(reason, "PIDPressure"):
		logger.Info("PIDPressure detected - evicting pods to reduce process count")
		if err := r.evictPodsForPIDPressure(ctx, node); err != nil {
			logger.Error(err, "failed to evict pods for PID pressure")
			logger.Info("falling back to node reboot")
			return r.rebootNode(ctx, nodeClaim)
		}
		return nil

	case strings.Contains(reason, "NetworkUnavailable"):
		logger.Info("NetworkUnavailable detected - restarting instance to reset network")
		return r.rebootNode(ctx, nodeClaim)

	default:
		logger.Info("attempting node reboot for general recovery", "reason", reason)
		return r.rebootNode(ctx, nodeClaim)
	}
}

func (r *NodeRepairController) rebootNode(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) error {
	instanceID, err := utils.ParseProviderID(nodeClaim.Status.ProviderID)
	if err != nil {
		return fmt.Errorf("failed to parse provider ID: %w", err)
	}

	logger := log.FromContext(ctx).WithValues("instanceID", instanceID)
	logger.Info("attempting to reboot instance")

	operation, err := r.ExoscaleClient.RebootInstance(ctx, egov3.UUID(instanceID))
	if err != nil {
		return fmt.Errorf("failed to reboot instance: %w", err)
	}

	if operation != nil {
		logger.V(1).Info("reboot operation initiated", "operationID", operation.ID)

		if err := r.waitForInstanceState(ctx, instanceID, egov3.InstanceStateRunning, 5*time.Minute); err != nil {
			return fmt.Errorf("failed waiting for instance to be running after reboot: %w", err)
		}
	}

	return nil
}

func (r *NodeRepairController) waitForInstanceState(ctx context.Context, instanceID string, targetState egov3.InstanceState, timeout time.Duration) error {
	logger := log.FromContext(ctx).WithValues("instanceID", instanceID, "targetState", targetState)
	logger.V(1).Info("waiting for instance state")

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for instance state %s", targetState)
			}

			instance, err := r.ExoscaleClient.GetInstance(ctx, egov3.UUID(instanceID))
			if err != nil {
				logger.V(2).Info("failed to get instance status", "error", err)
				continue
			}

			if instance.State == targetState {
				logger.V(1).Info("instance reached target state")
				return nil
			}

			logger.V(2).Info("instance state", "current", instance.State, "target", targetState)
		}
	}
}
