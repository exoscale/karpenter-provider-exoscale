package nodeclass

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/awslabs/operatorpkg/status"
	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instance"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

const (
	ConditionTypeReady = "Ready"

	ReasonNotReady = "NotReady"

	Finalizer = "exoscale.com/nodeclass-finalizer"

	MinDiskSize           = 10   // GB
	MaxDiskSize           = 8000 // GB
	MaxAntiAffinityGroups = 8
)

type ExoscaleNodeClassReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	ExoscaleClient instance.EgoscaleClient
	Recorder       record.EventRecorder
	Zone           string
}

// +kubebuilder:rbac:groups=karpenter.exoscale.com,resources=exoscalenodeclasses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=karpenter.exoscale.com,resources=exoscalenodeclasses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=karpenter.exoscale.com,resources=exoscalenodeclasses/finalizers,verbs=update
// +kubebuilder:rbac:groups=karpenter.sh,resources=nodeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *ExoscaleNodeClassReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("nodeclass", req.NamespacedName)
	logger.V(1).Info("reconciling ExoscaleNodeClass")

	nodeClass := &apiv1.ExoscaleNodeClass{}
	if err := r.Get(ctx, req.NamespacedName, nodeClass); err != nil {
		if errors.IsNotFound(err) {
			logger.V(1).Info("ExoscaleNodeClass not found, ignoring")
			return reconcile.Result{}, nil
		}
		logger.Error(err, "failed to get ExoscaleNodeClass")
		return reconcile.Result{}, err
	}

	if !nodeClass.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, nodeClass)
	}

	if !slices.Contains(nodeClass.Finalizers, Finalizer) {
		nodeClass.Finalizers = append(nodeClass.Finalizers, Finalizer)
		if err := r.Update(ctx, nodeClass); err != nil {
			logger.Error(err, "failed to add finalizer")
			return reconcile.Result{}, err
		}
		return reconcile.Result{RequeueAfter: time.Minute}, nil
	}

	if nodeClass.Status.Conditions == nil {
		nodeClass.Status.Conditions = []status.Condition{}
	}

	if err := r.validate(nodeClass); err != nil {
		logger.Error(err, "validation failed")
		nodeClass.StatusConditions().SetFalse(ConditionTypeReady, ReasonNotReady, "Validation failed: "+err.Error())
		r.Recorder.Eventf(nodeClass, "Warning", "ValidationFailed", "NodeClass validation failed: %v", err)

		if err := r.Status().Update(ctx, nodeClass); err != nil {
			logger.Error(err, "failed to update status")
			return reconcile.Result{}, err
		}

		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}

	r.Recorder.Event(nodeClass, "Normal", "ValidationSucceeded", "NodeClass validation succeeded")

	if err := r.verifyExoscaleResources(ctx, nodeClass); err != nil {
		logger.Error(err, "Exoscale resource verification failed")
		nodeClass.StatusConditions().SetFalse(ConditionTypeReady, ReasonNotReady, "Resource verification failed: "+err.Error())
		r.Recorder.Eventf(nodeClass, "Warning", "ResourceVerificationFailed", "Exoscale resource verification failed: %v", err)

		if err := r.Status().Update(ctx, nodeClass); err != nil {
			logger.Error(err, "failed to update status")
			return reconcile.Result{}, err
		}

		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}

	nodeClass.StatusConditions().SetTrue(ConditionTypeReady)
	r.Recorder.Event(nodeClass, "Normal", "Ready", "NodeClass is ready for use")

	if err := r.Status().Update(ctx, nodeClass); err != nil {
		logger.Error(err, "failed to update status")
		return reconcile.Result{}, err
	}

	return reconcile.Result{RequeueAfter: 5 * time.Minute}, nil
}

func isNodeClaimUsingNodeClass(nc *karpenterv1.NodeClaim, nodeClassName string) bool {
	return nc.Spec.NodeClassRef != nil &&
		nc.Spec.NodeClassRef.Group == "karpenter.exoscale.com" &&
		nc.Spec.NodeClassRef.Kind == "ExoscaleNodeClass" &&
		nc.Spec.NodeClassRef.Name == nodeClassName
}

func countActiveNodeClaims(nodeClaims []karpenterv1.NodeClaim, nodeClassName string) int {
	count := 0
	for _, nc := range nodeClaims {
		if isNodeClaimUsingNodeClass(&nc, nodeClassName) && nc.DeletionTimestamp == nil {
			count++
		}
	}
	return count
}

func (r *ExoscaleNodeClassReconciler) handleDeletion(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("nodeclass", nodeClass.Name)

	if !slices.Contains(nodeClass.Finalizers, Finalizer) {
		return reconcile.Result{}, nil
	}

	logger.Info("handling ExoscaleNodeClass deletion")

	nodeClaims := &karpenterv1.NodeClaimList{}
	if err := r.List(ctx, nodeClaims); err != nil {
		logger.Error(err, "failed to list NodeClaims")
		return reconcile.Result{}, err
	}

	activeCount := countActiveNodeClaims(nodeClaims.Items, nodeClass.Name)

	if activeCount > 0 {
		logger.Info("NodeClass still in use by active NodeClaims", "activeNodeClaims", activeCount)
		r.Recorder.Eventf(nodeClass, "Warning", "DeletionBlocked",
			"Cannot delete NodeClass: %d active NodeClaim(s) still using this NodeClass", activeCount)
		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}

	if err := r.cleanupOrphanedInstances(ctx, nodeClass); err != nil {
		logger.Error(err, "failed to cleanup orphaned instances")
		r.Recorder.Eventf(nodeClass, "Warning", "CleanupFailed", "Failed to cleanup orphaned instances: %v", err)
	}

	nodeClass.Finalizers = slices.DeleteFunc(nodeClass.Finalizers, func(s string) bool {
		return s == Finalizer
	})
	if err := r.Update(ctx, nodeClass); err != nil {
		logger.Error(err, "failed to remove finalizer")
		return reconcile.Result{}, err
	}

	r.Recorder.Event(nodeClass, "Normal", "Deleted", "ExoscaleNodeClass deleted successfully")

	return reconcile.Result{}, nil
}

func (r *ExoscaleNodeClassReconciler) cleanupOrphanedInstances(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	logger := log.FromContext(ctx).WithValues("nodeclass", nodeClass.Name)
	logger.V(1).Info("checking for orphaned instances")

	instances, err := r.ExoscaleClient.ListInstances(ctx)
	if err != nil {
		logger.Error(err, "failed to list instances")
		return fmt.Errorf("failed to list instances: %w", err)
	}

	nodeClaimList := &karpenterv1.NodeClaimList{}
	if err := r.List(ctx, nodeClaimList); err != nil {
		logger.Error(err, "failed to list NodeClaims")
		return fmt.Errorf("failed to list NodeClaims: %w", err)
	}

	validNodeClaims := make(map[string]bool)
	for _, nc := range nodeClaimList.Items {
		validNodeClaims[nc.Name] = true
	}

	orphanedCount := 0
	for _, inst := range instances.Instances {
		if inst.Labels == nil {
			continue
		}

		managedBy, hasManagedBy := inst.Labels[constants.LabelManagedBy]
		if !hasManagedBy || managedBy != constants.ManagedByKarpenter {
			continue
		}

		nodeClaimName, hasNodeClaim := inst.Labels[constants.LabelNodeClaim]
		if !hasNodeClaim || validNodeClaims[nodeClaimName] {
			continue
		}

		logger.Info("found orphaned inst",
			"instanceID", inst.ID,
			"instanceName", inst.Name,
			"nodeClaimName", nodeClaimName)

		if _, err := r.ExoscaleClient.DeleteInstance(ctx, inst.ID); err != nil {
			logger.Error(err, "failed to delete orphaned inst",
				"instanceID", inst.ID,
				"instanceName", inst.Name)
			continue
		}

		logger.Info("deleted orphaned inst",
			"instanceID", inst.ID,
			"instanceName", inst.Name)
		orphanedCount++

		r.Recorder.Eventf(nodeClass, "Normal", "OrphanedInstanceDeleted",
			"Deleted orphaned inst %s (NodeClaim: %s)", inst.Name, nodeClaimName)
	}

	if orphanedCount > 0 {
		logger.Info("orphaned inst cleanup completed", "deletedCount", orphanedCount)
	} else {
		logger.V(1).Info("no orphaned instances found")
	}

	return nil
}

func (r *ExoscaleNodeClassReconciler) verifyExoscaleResources(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	logger := log.FromContext(ctx).WithValues("nodeclass", nodeClass.Name)

	logger.V(1).Info("verifying template", "templateID", nodeClass.Spec.TemplateID)
	if _, err := r.ExoscaleClient.GetTemplate(ctx, egov3.UUID(nodeClass.Spec.TemplateID)); err != nil {
		return fmt.Errorf("template %s not found or not accessible: %w", nodeClass.Spec.TemplateID, err)
	}

	for _, sgID := range nodeClass.Spec.SecurityGroups {
		logger.V(1).Info("verifying security group", "securityGroupID", sgID)
		if _, err := r.ExoscaleClient.GetSecurityGroup(ctx, egov3.UUID(sgID)); err != nil {
			return fmt.Errorf("security group %s not found or not accessible: %w", sgID, err)
		}
	}

	for _, netID := range nodeClass.Spec.PrivateNetworks {
		logger.V(1).Info("verifying private network", "networkID", netID)
		if _, err := r.ExoscaleClient.GetPrivateNetwork(ctx, egov3.UUID(netID)); err != nil {
			return fmt.Errorf("private network %s not found or not accessible: %w", netID, err)
		}
	}

	for _, aagID := range nodeClass.Spec.AntiAffinityGroups {
		logger.V(1).Info("verifying anti-affinity group", "antiAffinityGroupID", aagID)
		aag, err := r.ExoscaleClient.GetAntiAffinityGroup(ctx, egov3.UUID(aagID))
		if err != nil {
			return fmt.Errorf("anti-affinity group %s not found or not accessible: %w", aagID, err)
		}

		// Exoscale has a limit of 8 instances per anti-affinity group
		if len(aag.Instances) >= 8 {
			logger.Info("anti-affinity group at capacity", "antiAffinityGroupID", aagID, "instances", len(aag.Instances))
		}
	}
	return nil
}

func (r *ExoscaleNodeClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.ExoscaleNodeClass{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 5,
		}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

func (r *ExoscaleNodeClassReconciler) validate(nodeClass *apiv1.ExoscaleNodeClass) error {

	if nodeClass.Spec.TemplateID != "" {
		if _, err := egov3.ParseUUID(nodeClass.Spec.TemplateID); err != nil {
			return fmt.Errorf("invalid template ID: %w", err)
		}
	} else {
		return fmt.Errorf("template ID is required")
	}

	if nodeClass.Spec.DiskSize < MinDiskSize || nodeClass.Spec.DiskSize > MaxDiskSize {
		return fmt.Errorf("disk size must be between %d and %d GB, got %d", MinDiskSize, MaxDiskSize, nodeClass.Spec.DiskSize)
	}

	for _, sg := range nodeClass.Spec.SecurityGroups {
		if _, err := egov3.ParseUUID(sg); err != nil {
			return fmt.Errorf("invalid security group ID %s: %w", sg, err)
		}
	}

	for _, net := range nodeClass.Spec.PrivateNetworks {
		if _, err := egov3.ParseUUID(net); err != nil {
			return fmt.Errorf("invalid private network ID %s: %w", net, err)
		}
	}

	if len(nodeClass.Spec.AntiAffinityGroups) > MaxAntiAffinityGroups {
		return fmt.Errorf("maximum %d anti-affinity groups allowed, got %d", MaxAntiAffinityGroups, len(nodeClass.Spec.AntiAffinityGroups))
	}
	for _, aag := range nodeClass.Spec.AntiAffinityGroups {
		if _, err := egov3.ParseUUID(aag); err != nil {
			return fmt.Errorf("invalid anti-affinity group ID %s: %w", aag, err)
		}
	}

	for _, taint := range nodeClass.Spec.NodeTaints {
		if taint.Key == "" {
			return fmt.Errorf("taint key cannot be empty")
		}
		if taint.Effect != "NoSchedule" && taint.Effect != "PreferNoSchedule" && taint.Effect != "NoExecute" {
			return fmt.Errorf("invalid taint effect %s", taint.Effect)
		}
	}

	if err := validateResourceReservation(nodeClass.Spec.KubeReserved); err != nil {
		return fmt.Errorf("invalid kubeReserved: %w", err)
	}
	if err := validateResourceReservation(nodeClass.Spec.SystemReserved); err != nil {
		return fmt.Errorf("invalid systemReserved: %w", err)
	}

	if nodeClass.Spec.ImageGCHighThresholdPercent != nil {
		if *nodeClass.Spec.ImageGCHighThresholdPercent < 0 || *nodeClass.Spec.ImageGCHighThresholdPercent > 100 {
			return fmt.Errorf("imageGCHighThresholdPercent must be between 0 and 100")
		}
	}
	if nodeClass.Spec.ImageGCLowThresholdPercent != nil {
		if *nodeClass.Spec.ImageGCLowThresholdPercent < 0 || *nodeClass.Spec.ImageGCLowThresholdPercent > 100 {
			return fmt.Errorf("imageGCLowThresholdPercent must be between 0 and 100")
		}
	}
	if nodeClass.Spec.ImageGCHighThresholdPercent != nil && nodeClass.Spec.ImageGCLowThresholdPercent != nil {
		if *nodeClass.Spec.ImageGCLowThresholdPercent >= *nodeClass.Spec.ImageGCHighThresholdPercent {
			return fmt.Errorf("imageGCLowThresholdPercent must be less than imageGCHighThresholdPercent")
		}
	}
	return nil
}

func validateResourceReservation(reservation apiv1.ResourceReservation) error {
	if reservation.CPU != "" {
		if _, err := resource.ParseQuantity(reservation.CPU); err != nil {
			return fmt.Errorf("invalid CPU reservation: %w", err)
		}
	}
	if reservation.Memory != "" {
		if _, err := resource.ParseQuantity(reservation.Memory); err != nil {
			return fmt.Errorf("invalid memory reservation: %w", err)
		}
	}
	if reservation.EphemeralStorage != "" {
		if _, err := resource.ParseQuantity(reservation.EphemeralStorage); err != nil {
			return fmt.Errorf("invalid ephemeral storage reservation: %w", err)
		}
	}
	return nil
}
