package nodeclass

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/awslabs/operatorpkg/status"
	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/template"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/utils"
	labelfilter "github.com/exoscale/karpenter-provider-exoscale/pkg/utils/labels"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

const (
	Finalizer                           = "exoscale.com/nodeclass-finalizer"
	ConditionTemplateResolved           = "TemplateResolved"
	ConditionSecurityGroupsResolved     = "SecurityGroupsResolved"
	ConditionAntiAffinityGroupsResolved = "AntiAffinityGroupsResolved"
	ConditionPrivateNetworksResolved    = "PrivateNetworksResolved"
	ConditionElasticIPsResolved         = "ElasticIPsResolved"
	ConditionContainerRegistryResolved  = "ContainerRegistrySecretsResolved"
)

// ExoscaleClient is an interface for interacting with the Exoscale API
type ExoscaleClient interface {
	GetTemplate(ctx context.Context, id egov3.UUID) (*egov3.Template, error)
	GetSecurityGroup(ctx context.Context, id egov3.UUID) (*egov3.SecurityGroup, error)
	GetAntiAffinityGroup(ctx context.Context, id egov3.UUID) (*egov3.AntiAffinityGroup, error)
	GetPrivateNetwork(ctx context.Context, id egov3.UUID) (*egov3.PrivateNetwork, error)
	GetElasticIP(ctx context.Context, id egov3.UUID) (*egov3.ElasticIP, error)
	ListInstances(ctx context.Context, opts ...egov3.ListInstancesOpt) (*egov3.ListInstancesResponse, error)
	DeleteInstance(ctx context.Context, id egov3.UUID) (*egov3.Operation, error)
	ListSecurityGroups(ctx context.Context, opts ...egov3.ListSecurityGroupsOpt) (*egov3.ListSecurityGroupsResponse, error)
	ListAntiAffinityGroups(ctx context.Context) (*egov3.ListAntiAffinityGroupsResponse, error)
	ListPrivateNetworks(ctx context.Context) (*egov3.ListPrivateNetworksResponse, error)
	ListElasticIPS(ctx context.Context) (*egov3.ListElasticIPSResponse, error)
	AttachInstanceToElasticIP(ctx context.Context, id egov3.UUID, req egov3.AttachInstanceToElasticIPRequest) (*egov3.Operation, error)
}

type ExoscaleNodeClassReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	ExoscaleClient   ExoscaleClient
	TemplateResolver template.Resolver
	Recorder         events.EventRecorder
	ClusterID        string
	Zone             string
	aagCache         utils.ResourceCache[egov3.AntiAffinityGroup]
	sgCache          utils.ResourceCache[egov3.SecurityGroup]
	pnCache          utils.ResourceCache[egov3.PrivateNetwork]
	eipCache         utils.ResourceCache[egov3.ElasticIP]
}

// +kubebuilder:rbac:groups=karpenter.exoscale.com,resources=exoscalenodeclasses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=karpenter.exoscale.com,resources=exoscalenodeclasses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=karpenter.exoscale.com,resources=exoscalenodeclasses/finalizers,verbs=update
// +kubebuilder:rbac:groups=karpenter.sh,resources=nodeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

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
		r.Recorder.Eventf(nodeClass, nil, "Warning", "ValidationFailed", "ValidationFailed", "NodeClass validation failed: %v", err)

		if err := r.Status().Patch(ctx, nodeClass, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{})); err != nil {
			if errors.IsConflict(err) {
				return reconcile.Result{Requeue: true}, nil
			}

			return reconcile.Result{RequeueAfter: 30 * time.Second}, fmt.Errorf("failed to patch nodeclass status: %w", err)
		}

		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}

	r.Recorder.Eventf(nodeClass, nil, "Normal", "ValidationSucceeded", "ValidationSucceeded", "NodeClass field validation succeeded")

	// Reconcile resources from Exoscale API now and resolve them into status fields
	type reconcileStep struct {
		reconcileFn  func(context.Context, *apiv1.ExoscaleNodeClass) error
		reason       string
		errorMessage string
		condition    string
	}

	reconcileSteps := []reconcileStep{
		{
			reconcileFn:  r.reconcileTemplate,
			reason:       "TemplateResolutionFailed",
			errorMessage: "Exoscale template resolution failed",
			condition:    ConditionTemplateResolved,
		},
		{
			reconcileFn:  r.reconcileSecurityGroups,
			reason:       "SecurityGroupResolutionFailed",
			errorMessage: "Security group resolution failed",
			condition:    ConditionSecurityGroupsResolved,
		},
		{
			reconcileFn:  r.reconcileAntiAffinityGroups,
			reason:       "AntiAffinityGroupResolutionFailed",
			errorMessage: "Anti-affinity group resolution failed",
			condition:    ConditionAntiAffinityGroupsResolved,
		},
		{
			reconcileFn:  r.reconcileElasticIPs,
			reason:       "ElasticIPResolutionFailed",
			errorMessage: "Elastic IP resolution failed",
			condition:    ConditionElasticIPsResolved,
		},
		{
			reconcileFn:  r.reconcilePrivateNetworks,
			reason:       "PrivateNetworkResolutionFailed",
			errorMessage: "Private network resolution failed",
			condition:    ConditionPrivateNetworksResolved,
		},
		{
			reconcileFn:  r.reconcileContainerRegistrySecrets,
			reason:       "ContainerRegistryResolutionFailed",
			errorMessage: "Container registry secret resolution failed",
			condition:    ConditionContainerRegistryResolved,
		},
		{
			reconcileFn:  r.reconcileCPUManagerHash,
			reason:       "CPUManagerHashComputeFailed",
			errorMessage: "CPU manager hash compute failed",
			condition:    ConditionContainerRegistryResolved,
		},
	}

	nodeClass.StatusConditions().SetFalse(status.ConditionReady, "Reconciling", "Reconciling node class resources")
	var err error
	for _, step := range reconcileSteps {
		if err = step.reconcileFn(ctx, nodeClass); err != nil {
			log.FromContext(ctx).Error(err, step.errorMessage)
			nodeClass.StatusConditions().SetFalse(step.condition, step.reason, err.Error())
			// It will only record the first failure event during reconciliation loop
			// but we have all errors on each condition
			nodeClass.StatusConditions().SetFalse(status.ConditionReady, "ReconcilingFailed", "Reconciling node class resources failed")
			r.Recorder.Eventf(nodeClass, nil, "Warning", step.reason, step.reason, "%s: %v", step.errorMessage, err)
			continue
		}

		nodeClass.StatusConditions().SetTrue(step.condition)
	}

	if nodeClass.StatusConditions().IsTrue(ConditionTemplateResolved, ConditionSecurityGroupsResolved, ConditionAntiAffinityGroupsResolved,
		ConditionPrivateNetworksResolved, ConditionElasticIPsResolved, ConditionContainerRegistryResolved) {
		nodeClass.StatusConditions().SetTrue(status.ConditionReady)
		r.Recorder.Eventf(nodeClass, nil, "Normal", "Ready", "Ready", "NodeClass is ready for use")
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
		r.Recorder.Eventf(nodeClass, nil, "Warning", "DeletionBlocked", "DeletionBlocked",
			"Cannot delete NodeClass: %d active NodeClaim(s) still using this NodeClass", activeCount)
		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}

	if err := r.cleanupOrphanedInstances(ctx, nodeClass); err != nil {
		log.FromContext(ctx).Error(err, "failed to cleanup orphaned instances")
		r.Recorder.Eventf(nodeClass, nil, "Warning", "CleanupFailed", "CleanupFailed", "Failed to cleanup orphaned instances: %v", err)
	}

	nodeClass.Finalizers = slices.DeleteFunc(nodeClass.Finalizers, func(s string) bool {
		return s == Finalizer
	})
	if err := r.Update(ctx, nodeClass); err != nil {
		log.FromContext(ctx).Error(err, "failed to remove finalizer")
		return reconcile.Result{}, err
	}

	r.Recorder.Eventf(nodeClass, nil, "Normal", "Deleted", "Deleted", "ExoscaleNodeClass deleted successfully")

	return reconcile.Result{}, nil
}

func (r *ExoscaleNodeClassReconciler) cleanupOrphanedInstances(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("nodeclass", nodeClass.Name))
	log.FromContext(ctx).V(1).Info("checking for orphaned instances")

	if r.ClusterID == "" {
		return fmt.Errorf("cluster ID is not configured")
	}

	encodedLabels, err := labelfilter.KarpenterFilter(r.ClusterID)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to build labels filter")
		return fmt.Errorf("failed to build labels filter: %w", err)
	}

	instances, err := r.ExoscaleClient.ListInstances(ctx, egov3.ListInstancesWithLabels(encodedLabels))

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
		nodeClaimName, isOrphanedNodeClaim := r.orphanedNodeClaimName(inst, validNodeClaims)
		if !isOrphanedNodeClaim {
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

		r.Recorder.Eventf(nodeClass, nil, "Normal", "OrphanedInstanceDeleted", "OrphanedInstanceDeleted",
			"Deleted orphaned inst %s (NodeClaim: %s)", inst.Name, nodeClaimName)
	}

	if orphanedCount > 0 {
		log.FromContext(ctx).Info("orphaned inst cleanup completed", "deletedCount", orphanedCount)
	} else {
		log.FromContext(ctx).V(1).Info("no orphaned instances found")
	}

	return nil
}

func (r *ExoscaleNodeClassReconciler) orphanedNodeClaimName(inst egov3.ListInstancesResponseInstances, validNodeClaims map[string]bool) (string, bool) {
	if inst.Labels == nil {
		return "", false
	}

	managedBy, hasManagedBy := inst.Labels[constants.InstanceLabelManagedBy]
	if !hasManagedBy || managedBy != constants.ManagedByKarpenter {
		return "", false
	}

	clusterID, hasClusterID := inst.Labels[constants.InstanceLabelClusterID]
	if !hasClusterID || clusterID != r.ClusterID {
		return "", false
	}

	nodeClaimName, hasNodeClaim := inst.Labels[constants.InstanceLabelNodeClaim]
	if !hasNodeClaim || validNodeClaims[nodeClaimName] {
		return "", false
	}

	return nodeClaimName, true
}

func (r *ExoscaleNodeClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.ExoscaleNodeClass{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.secretToNodeClasses),
			builder.WithPredicates(predicate.NewPredicateFuncs(func(obj client.Object) bool {
				return obj.GetNamespace() == "kube-system"
			})),
		).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 5,
		}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

// secretToNodeClasses lists all ExoscaleNodeClass objects and returns a
// reconcile request for each one whose spec.containerRegistry references the
// changed Secret (by name in kube-system).
func (r *ExoscaleNodeClassReconciler) secretToNodeClasses(ctx context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok || secret.Namespace != "kube-system" {
		return nil
	}
	var list apiv1.ExoscaleNodeClassList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	var out []reconcile.Request
	for i := range list.Items {
		nc := &list.Items[i]
		if nc.Spec.ContainerRegistry == nil {
			continue
		}
		if referencesSecret(nc.Spec.ContainerRegistry, secret.Name) {
			out = append(out, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(nc)})
		}
	}
	return out
}

// referencesSecret reports whether the given kube-system Secret is named by
// any mirror TLS ref or credential ref inside the ContainerRegistry spec.
func referencesSecret(spec *apiv1.ContainerRegistrySpec, secretName string) bool {
	for _, mirror := range spec.Mirrors {
		for _, ep := range mirror.Endpoints {
			if ep.TLSSecretRef != nil && ep.TLSSecretRef.Name == secretName {
				return true
			}
		}
	}
	for _, cred := range spec.Credentials {
		if cred.Basic != nil {
			if cred.Basic.UsernameSecretRef.Name == secretName || cred.Basic.PasswordSecretRef.Name == secretName {
				return true
			}
		}
		if cred.Auth != nil && cred.Auth.AuthSecretRef.Name == secretName {
			return true
		}
		if cred.IdentityToken != nil && cred.IdentityToken.IdentityTokenSecretRef.Name == secretName {
			return true
		}
	}
	return false
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
