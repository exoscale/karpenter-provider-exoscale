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
	"github.com/exoscale/karpenter-exoscale/pkg/providers/template"
	"github.com/exoscale/karpenter-exoscale/pkg/utils"
	"k8s.io/apimachinery/pkg/api/equality"
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
	Finalizer = "exoscale.com/nodeclass-finalizer"
)

type ExoscaleNodeClassReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	ExoscaleClient   *egov3.Client
	TemplateResolver *template.Provider
	Recorder         record.EventRecorder
	Zone             string
	aagCache         utils.ResourceCache[egov3.AntiAffinityGroup]
	sgCache          utils.ResourceCache[egov3.SecurityGroup]
	pnCache          utils.ResourceCache[egov3.PrivateNetwork]
}

// +kubebuilder:rbac:groups=karpenter.exoscale.com,resources=exoscalenodeclasses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=karpenter.exoscale.com,resources=exoscalenodeclasses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=karpenter.exoscale.com,resources=exoscalenodeclasses/finalizers,verbs=update
// +kubebuilder:rbac:groups=karpenter.sh,resources=nodeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *ExoscaleNodeClassReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("nodeclass", req.NamespacedName))
	log.FromContext(ctx).V(1).Info("reconciling ExoscaleNodeClass")

	nodeClass := &apiv1.ExoscaleNodeClass{}
	if err := r.Get(ctx, req.NamespacedName, nodeClass); err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).V(1).Info("ExoscaleNodeClass not found, ignoring")
			return reconcile.Result{}, nil
		}
		log.FromContext(ctx).Error(err, "failed to get ExoscaleNodeClass")
		return reconcile.Result{}, err
	}

	if !nodeClass.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, nodeClass)
	}

	stored := nodeClass.DeepCopy()

	if !slices.Contains(nodeClass.Finalizers, Finalizer) {
		nodeClass.Finalizers = append(nodeClass.Finalizers, Finalizer)
		if err := r.Update(ctx, nodeClass); err != nil {
			log.FromContext(ctx).Error(err, "failed to add finalizer")
			return reconcile.Result{}, err
		}
		return reconcile.Result{RequeueAfter: time.Minute}, nil
	}

	if nodeClass.Status.Conditions == nil {
		nodeClass.Status.Conditions = []status.Condition{}
	}

	if err := r.validateSpec(nodeClass); err != nil {
		log.FromContext(ctx).Error(err, "validation failed")
		nodeClass.StatusConditions().SetFalse(status.ConditionReady, "ValidationFailed", "Validation failed: "+err.Error())
		r.Recorder.Eventf(nodeClass, "Warning", "ValidationFailed", "NodeClass validation failed: %v", err)

		if err := r.Status().Patch(ctx, nodeClass, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{})); err != nil {
			if errors.IsConflict(err) {
				return reconcile.Result{Requeue: true}, nil
			}

			return reconcile.Result{RequeueAfter: 30 * time.Second}, fmt.Errorf("failed to patch nodeclass status: %w", err)
		}

		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}

	r.Recorder.Event(nodeClass, "Normal", "ValidationSucceeded", "NodeClass field validation succeeded")

	// Reconcile resources from Exoscale API now and resolve them into status fields
	type reconcileStep struct {
		reconcileFn  func(context.Context, *apiv1.ExoscaleNodeClass) error
		reason       string
		errorMessage string
	}

	reconcileSteps := []reconcileStep{
		{
			reconcileFn:  r.reconcileTemplate,
			reason:       "TemplateReconciliationFailed",
			errorMessage: "Exoscale template reconciliation failed",
		},
		{
			reconcileFn:  r.reconcileSecurityGroups,
			reason:       "SecurityGroupResolutionFailed",
			errorMessage: "Security group resolution failed",
		},
		{
			reconcileFn:  r.reconcileAntiAffinityGroups,
			reason:       "AntiAffinityGroupResolutionFailed",
			errorMessage: "Anti-affinity group resolution failed",
		},
		{
			reconcileFn:  r.reconcilePrivateNetworks,
			reason:       "PrivateNetworkResolutionFailed",
			errorMessage: "Private network resolution failed",
		},
	}

	var err error
	for _, step := range reconcileSteps {
		if err = step.reconcileFn(ctx, nodeClass); err != nil {
			log.FromContext(ctx).Error(err, step.errorMessage)
			nodeClass.StatusConditions().SetFalse(status.ConditionReady, step.reason, step.errorMessage+": "+err.Error())
			r.Recorder.Eventf(nodeClass, "Warning", step.reason, "%s: %v", step.errorMessage, err)
			break
		}
	}

	if err == nil {
		nodeClass.StatusConditions().SetTrue(status.ConditionReady)
		r.Recorder.Event(nodeClass, "Normal", "Ready", "NodeClass is ready for use")
	}

	// if resource is different, patch it
	if !equality.Semantic.DeepEqual(stored, nodeClass) {
		if err := r.Status().Patch(ctx, nodeClass, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{})); err != nil {
			if errors.IsConflict(err) {
				return reconcile.Result{Requeue: true}, nil
			}

			return reconcile.Result{RequeueAfter: 30 * time.Second}, fmt.Errorf("failed to patch nodeclass status: %w", err)
		}
	}

	return reconcile.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *ExoscaleNodeClassReconciler) reconcileSecurityGroups(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	sgIDs := []string{}

	// Deprecated field, use selector instead now
	for _, sgID := range nodeClass.Spec.SecurityGroups {
		log.FromContext(ctx).V(1).Info("resolving security group", "securityGroupID", sgID)
		sg, err := r.ExoscaleClient.GetSecurityGroup(ctx, egov3.UUID(sgID))
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get security group", "securityGroupID", sgID)
			return fmt.Errorf("failed to get security group %s: %w", sgID, err)
		}
		sgIDs = append(sgIDs, sg.ID.String())
	}

	for _, selector := range nodeClass.Spec.SecurityGroupSelectorTerms {
		var sg *egov3.SecurityGroup
		var err error
		if selector.ID != "" {
			log.FromContext(ctx).V(1).Info("resolving security group by ID", "securityGroupID", selector.ID)
			sg, err = r.ExoscaleClient.GetSecurityGroup(ctx, egov3.UUID(selector.ID))
		} else if selector.Name != "" {
			log.FromContext(ctx).V(1).Info("resolving security group by Name", "securityGroupName", selector.Name)
			// Discover security group by name
			sg, err = r.getCachedSecurityGroupByName(ctx, selector.Name)
			if err != nil {
				log.FromContext(ctx).Error(err, "failed to get security group by name")
				return fmt.Errorf("failed to get security group by name: %w", err)
			}

			if sg == nil {
				err = fmt.Errorf("security group with name %s not found", selector.Name)
			}
		}
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get security group", "selector", selector)
			return fmt.Errorf("failed to get security group for selector %+v: %w", selector, err)
		}

		sgIDs = append(sgIDs, sg.ID.String())
	}

	nodeClass.Status.SecurityGroups = sgIDs
	return nil
}

func (r *ExoscaleNodeClassReconciler) reconcileAntiAffinityGroups(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	aagIDs := []string{}

	// Deprecated field, use selector instead now
	for _, aagID := range nodeClass.Spec.AntiAffinityGroups {
		log.FromContext(ctx).V(1).Info("resolving anti-affinity group", "antiAffinityGroupID", aagID)
		aag, err := r.ExoscaleClient.GetAntiAffinityGroup(ctx, egov3.UUID(aagID))
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get anti-affinity group", "antiAffinityGroupID", aagID)
			return fmt.Errorf("failed to get anti-affinity group %s: %w", aagID, err)
		}
		aagIDs = append(aagIDs, aag.ID.String())
	}

	for _, selector := range nodeClass.Spec.AntiAffinityGroupSelectorTerms {
		var aag *egov3.AntiAffinityGroup
		var err error
		if selector.ID != "" {
			log.FromContext(ctx).V(1).Info("resolving anti-affinity group by ID", "antiAffinityGroupID", selector.ID)
			aag, err = r.ExoscaleClient.GetAntiAffinityGroup(ctx, egov3.UUID(selector.ID))
		} else if selector.Name != "" {
			log.FromContext(ctx).V(1).Info("resolving anti-affinity group by Name", "antiAffinityGroupName", selector.Name)
			// Discover anti-affinity group by name
			aag, err = r.getCachedAntiAffinityGroupByName(ctx, selector.Name)
			if err != nil {
				log.FromContext(ctx).Error(err, "failed to get anti-affinity group by name")
				return fmt.Errorf("failed to get anti-affinity group by name: %w", err)
			}

			if aag == nil {
				err = fmt.Errorf("anti-affinity group with name %s not found", selector.Name)
			}
		}
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get anti-affinity group", "selector", selector)
			return fmt.Errorf("failed to get anti-affinity group for selector %+v: %w", selector, err)
		}

		aagIDs = append(aagIDs, aag.ID.String())
	}

	nodeClass.Status.AntiAffinityGroups = aagIDs
	return nil
}

func (r *ExoscaleNodeClassReconciler) reconcilePrivateNetworks(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	privNetIDs := []string{}

	// Deprecated field, use selector instead now
	for _, netID := range nodeClass.Spec.PrivateNetworks {
		log.FromContext(ctx).V(1).Info("resolving private network", "privateNetworkID", netID)
		net, err := r.ExoscaleClient.GetPrivateNetwork(ctx, egov3.UUID(netID))
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get private network", "privateNetworkID", netID)
			return fmt.Errorf("failed to get private network %s: %w", netID, err)
		}
		privNetIDs = append(privNetIDs, net.ID.String())
	}

	for _, selector := range nodeClass.Spec.PrivateNetworkSelectorTerms {
		var net *egov3.PrivateNetwork
		var err error
		if selector.ID != "" {
			log.FromContext(ctx).V(1).Info("resolving private network by ID", "privateNetworkID", selector.ID)
			net, err = r.ExoscaleClient.GetPrivateNetwork(ctx, egov3.UUID(selector.ID))
		} else if selector.Name != "" {
			log.FromContext(ctx).V(1).Info("resolving private network by Name", "privateNetworkName", selector.Name)
			// Discover private network by name
			net, err = r.getCachedPrivateNetworkByName(ctx, selector.Name)
			if err != nil {
				log.FromContext(ctx).Error(err, "failed to get private network by name")
				return fmt.Errorf("failed to get private network by name: %w", err)
			}

			if net == nil {
				err = fmt.Errorf("private network with name %s not found", selector.Name)
			}
		}
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get private network", "selector", selector)
			return fmt.Errorf("failed to get private network for selector %+v: %w", selector, err)
		}

		privNetIDs = append(privNetIDs, net.ID.String())
	}

	nodeClass.Status.PrivateNetworks = privNetIDs
	return nil
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
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("nodeclass", nodeClass.Name))

	if !slices.Contains(nodeClass.Finalizers, Finalizer) {
		return reconcile.Result{}, nil
	}

	log.FromContext(ctx).Info("handling ExoscaleNodeClass deletion")

	nodeClaims := &karpenterv1.NodeClaimList{}
	if err := r.List(ctx, nodeClaims); err != nil {
		log.FromContext(ctx).Error(err, "failed to list NodeClaims")
		return reconcile.Result{}, err
	}

	activeCount := countActiveNodeClaims(nodeClaims.Items, nodeClass.Name)

	if activeCount > 0 {
		log.FromContext(ctx).Info("NodeClass still in use by active NodeClaims", "activeNodeClaims", activeCount)
		r.Recorder.Eventf(nodeClass, "Warning", "DeletionBlocked",
			"Cannot delete NodeClass: %d active NodeClaim(s) still using this NodeClass", activeCount)
		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}

	if err := r.cleanupOrphanedInstances(ctx, nodeClass); err != nil {
		log.FromContext(ctx).Error(err, "failed to cleanup orphaned instances")
		r.Recorder.Eventf(nodeClass, "Warning", "CleanupFailed", "Failed to cleanup orphaned instances: %v", err)
	}

	nodeClass.Finalizers = slices.DeleteFunc(nodeClass.Finalizers, func(s string) bool {
		return s == Finalizer
	})
	if err := r.Update(ctx, nodeClass); err != nil {
		log.FromContext(ctx).Error(err, "failed to remove finalizer")
		return reconcile.Result{}, err
	}

	r.Recorder.Event(nodeClass, "Normal", "Deleted", "ExoscaleNodeClass deleted successfully")

	return reconcile.Result{}, nil
}

func (r *ExoscaleNodeClassReconciler) cleanupOrphanedInstances(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("nodeclass", nodeClass.Name))
	log.FromContext(ctx).V(1).Info("checking for orphaned instances")

	instances, err := r.ExoscaleClient.ListInstances(ctx)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to list instances")
		return fmt.Errorf("failed to list instances: %w", err)
	}

	nodeClaimList := &karpenterv1.NodeClaimList{}
	if err := r.List(ctx, nodeClaimList); err != nil {
		log.FromContext(ctx).Error(err, "failed to list NodeClaims")
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

		managedBy, hasManagedBy := inst.Labels[constants.InstanceLabelManagedBy]
		if !hasManagedBy || managedBy != constants.ManagedByKarpenter {
			continue
		}

		nodeClaimName, hasNodeClaim := inst.Labels[constants.InstanceLabelNodeClaim]
		if !hasNodeClaim || validNodeClaims[nodeClaimName] {
			continue
		}

		log.FromContext(ctx).Info("found orphaned inst",
			"instanceID", inst.ID,
			"instanceName", inst.Name,
			"nodeClaimName", nodeClaimName)

		if _, err := r.ExoscaleClient.DeleteInstance(ctx, inst.ID); err != nil {
			log.FromContext(ctx).Error(err, "failed to delete orphaned inst",
				"instanceID", inst.ID,
				"instanceName", inst.Name)
			continue
		}

		log.FromContext(ctx).Info("deleted orphaned inst",
			"instanceID", inst.ID,
			"instanceName", inst.Name)
		orphanedCount++

		r.Recorder.Eventf(nodeClass, "Normal", "OrphanedInstanceDeleted",
			"Deleted orphaned inst %s (NodeClaim: %s)", inst.Name, nodeClaimName)
	}

	if orphanedCount > 0 {
		log.FromContext(ctx).Info("orphaned inst cleanup completed", "deletedCount", orphanedCount)
	} else {
		log.FromContext(ctx).V(1).Info("no orphaned instances found")
	}

	return nil
}

func (r *ExoscaleNodeClassReconciler) reconcileTemplate(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("nodeclass", nodeClass.Name))

	t, err := r.TemplateResolver.ResolveTemplate(ctx, nodeClass)
	if err != nil {
		return fmt.Errorf("failed to resolve template ID: %w", err)
	}

	log.FromContext(ctx).V(1).Info("verifying template", "templateID", t.ID)
	if _, err := r.ExoscaleClient.GetTemplate(ctx, egov3.UUID(t.ID)); err != nil {
		return fmt.Errorf("template %s not found or not accessible: %w", t.ID, err)
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

func (r *ExoscaleNodeClassReconciler) validateSpec(nodeClass *apiv1.ExoscaleNodeClass) error {
	kr := nodeClass.Spec.Kubelet.KubeReserved
	if err := validateResourceQuantities(kr.CPU, kr.Memory, kr.EphemeralStorage); err != nil {
		return fmt.Errorf("invalid kubelet.kubeReserved: %w", err)
	}

	sr := nodeClass.Spec.Kubelet.SystemReserved
	if err := validateResourceQuantities(sr.CPU, sr.Memory, sr.EphemeralStorage); err != nil {
		return fmt.Errorf("invalid kubelet.systemReserved: %w", err)
	}

	return nil
}

func validateResourceQuantities(cpu, memory, ephemeralStorage string) error {
	resources := map[string]string{
		"CPU":               cpu,
		"memory":            memory,
		"ephemeral storage": ephemeralStorage,
	}

	for name, value := range resources {
		if value != "" {
			if _, err := resource.ParseQuantity(value); err != nil {
				return fmt.Errorf("invalid %s reservation: %w", name, err)
			}
		}
	}
	return nil
}
