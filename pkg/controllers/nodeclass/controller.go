package nodeclass

import (
	"context"
	"fmt"
	"slices"
	"sync"
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
	ConditionVpcSubnetsResolved         = "VpcSubnetsResolved"
	ConditionElasticIPsResolved         = "ElasticIPsResolved"
	ConditionContainerRegistryResolved  = "ContainerRegistrySecretsResolved"
)

type ExoscaleClient interface {
	GetTemplate(ctx context.Context, id egov3.UUID) (*egov3.Template, error)
	GetSecurityGroup(ctx context.Context, id egov3.UUID) (*egov3.SecurityGroup, error)
	GetAntiAffinityGroup(ctx context.Context, id egov3.UUID) (*egov3.AntiAffinityGroup, error)
	GetPrivateNetwork(ctx context.Context, id egov3.UUID) (*egov3.PrivateNetwork, error)
	GetElasticIP(ctx context.Context, id egov3.UUID) (*egov3.ElasticIP, error)
	GetVpc(ctx context.Context, id egov3.UUID) (*egov3.Vpc, error)
	GetSubnet(ctx context.Context, vpcID, id egov3.UUID) (*egov3.Subnet, error)
	ListInstances(ctx context.Context, opts ...egov3.ListInstancesOpt) (*egov3.ListInstancesResponse, error)
	DeleteInstance(ctx context.Context, id egov3.UUID) (*egov3.Operation, error)
	ListSecurityGroups(ctx context.Context, opts ...egov3.ListSecurityGroupsOpt) (*egov3.ListSecurityGroupsResponse, error)
	ListAntiAffinityGroups(ctx context.Context) (*egov3.ListAntiAffinityGroupsResponse, error)
	ListPrivateNetworks(ctx context.Context) (*egov3.ListPrivateNetworksResponse, error)
	ListElasticIPS(ctx context.Context) (*egov3.ListElasticIPSResponse, error)
	ListVpcs(ctx context.Context) (*egov3.ListVpcsResponse, error)
	ListSubnets(ctx context.Context, vpcID egov3.UUID) (*egov3.ListSubnetsResponse, error)
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
	vpcCache         utils.ResourceCache[egov3.ListVpcEntry]
	subnetCachesMu   sync.Mutex
	subnetCaches     map[string]*utils.ResourceCache[egov3.ListSubnetEntry]
}

// +kubebuilder:rbac:groups=karpenter.exoscale.com,resources=exoscalenodeclasses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=karpenter.exoscale.com,resources=exoscalenodeclasses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=karpenter.exoscale.com,resources=exoscalenodeclasses/finalizers,verbs=update
// +kubebuilder:rbac:groups=karpenter.sh,resources=nodeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups="";events.k8s.io,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *ExoscaleNodeClassReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("nodeclass", req.NamespacedName))
	log.FromContext(ctx).V(1).Info("reconciling ExoscaleNodeClass")

	nodeClass := &apiv1.ExoscaleNodeClass{}
	if err := r.Get(ctx, req.NamespacedName, nodeClass); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	if !nodeClass.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, nodeClass)
	}

	stored := nodeClass.DeepCopy()

	if !slices.Contains(nodeClass.Finalizers, Finalizer) {
		nodeClass.Finalizers = append(nodeClass.Finalizers, Finalizer)
		if err := r.Update(ctx, nodeClass); err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{RequeueAfter: time.Minute}, nil
	}

	requeueAfter := 5 * time.Minute
	if err := validateSpec(nodeClass); err != nil {
		log.FromContext(ctx).Error(err, "validation failed")
		nodeClass.StatusConditions().SetFalse(status.ConditionReady, "ValidationFailed", "Validation failed: "+err.Error())
		r.Recorder.Eventf(nodeClass, nil, "Warning", "ValidationFailed", "ValidationFailed", "NodeClass validation failed: %v", err)
		requeueAfter = 30 * time.Second
	} else {
		r.Recorder.Eventf(nodeClass, nil, "Normal", "ValidationSucceeded", "ValidationSucceeded", "NodeClass field validation succeeded")
		r.reconcileResources(ctx, nodeClass)
	}

	if !equality.Semantic.DeepEqual(stored, nodeClass) {
		if err := r.Status().Patch(ctx, nodeClass, client.MergeFromWithOptions(stored, client.MergeFromWithOptimisticLock{})); err != nil {
			if errors.IsConflict(err) {
				return reconcile.Result{}, nil
			}
			return reconcile.Result{}, fmt.Errorf("failed to patch nodeclass status: %w", err)
		}
	}

	return reconcile.Result{RequeueAfter: requeueAfter}, nil
}

// reconcileResources resolves the Exoscale resources referenced by the spec into status.
func (r *ExoscaleNodeClassReconciler) reconcileResources(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) {
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
			reconcileFn:  r.reconcileVpcSubnets,
			reason:       "VpcSubnetResolutionFailed",
			errorMessage: "VPC subnet resolution failed",
			condition:    ConditionVpcSubnetsResolved,
		},
		{
			reconcileFn:  r.reconcileConfigurationHash,
			reason:       "ConfigurationHashComputeFailed",
			errorMessage: "configuration hash compute failed",
			condition:    ConditionContainerRegistryResolved,
		},
	}

	nodeClass.StatusConditions().SetFalse(status.ConditionReady, "Reconciling", "Reconciling node class resources")
	ready := true
	for _, step := range reconcileSteps {
		if err := step.reconcileFn(ctx, nodeClass); err != nil {
			log.FromContext(ctx).Error(err, step.errorMessage)
			nodeClass.StatusConditions().SetFalse(step.condition, step.reason, err.Error())
			nodeClass.StatusConditions().SetFalse(status.ConditionReady, "ReconcilingFailed", "Reconciling node class resources failed")
			r.Recorder.Eventf(nodeClass, nil, "Warning", step.reason, step.reason, "%s: %v", step.errorMessage, err)
			ready = false
			continue
		}
		nodeClass.StatusConditions().SetTrue(step.condition)
	}

	if ready {
		nodeClass.StatusConditions().SetTrue(status.ConditionReady)
		r.Recorder.Eventf(nodeClass, nil, "Normal", "Ready", "Ready", "NodeClass is ready for use")
	}
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
	if !slices.Contains(nodeClass.Finalizers, Finalizer) {
		return reconcile.Result{}, nil
	}

	log.FromContext(ctx).Info("handling ExoscaleNodeClass deletion")

	nodeClaims := &karpenterv1.NodeClaimList{}
	if err := r.List(ctx, nodeClaims); err != nil {
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
		return reconcile.Result{}, err
	}

	r.Recorder.Eventf(nodeClass, nil, "Normal", "Deleted", "Deleted", "ExoscaleNodeClass deleted successfully")

	return reconcile.Result{}, nil
}

func (r *ExoscaleNodeClassReconciler) cleanupOrphanedInstances(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	if r.ClusterID == "" {
		return fmt.Errorf("cluster ID is not configured")
	}

	encodedLabels, err := labelfilter.KarpenterFilter(r.ClusterID)
	if err != nil {
		return fmt.Errorf("failed to build labels filter: %w", err)
	}

	instances, err := r.ExoscaleClient.ListInstances(ctx, egov3.ListInstancesWithLabels(encodedLabels))
	if err != nil {
		return fmt.Errorf("failed to list instances: %w", err)
	}

	nodeClaimList := &karpenterv1.NodeClaimList{}
	if err := r.List(ctx, nodeClaimList); err != nil {
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

		log.FromContext(ctx).Info("found orphaned instance",
			"instanceID", inst.ID,
			"instanceName", inst.Name,
			"nodeClaimName", nodeClaimName)

		if _, err := r.ExoscaleClient.DeleteInstance(ctx, inst.ID); err != nil {
			log.FromContext(ctx).Error(err, "failed to delete orphaned instance",
				"instanceID", inst.ID,
				"instanceName", inst.Name)
			continue
		}

		log.FromContext(ctx).Info("deleted orphaned instance",
			"instanceID", inst.ID,
			"instanceName", inst.Name)
		orphanedCount++

		r.Recorder.Eventf(nodeClass, nil, "Normal", "OrphanedInstanceDeleted", "OrphanedInstanceDeleted",
			"Deleted orphaned instance %s (NodeClaim: %s)", inst.Name, nodeClaimName)
	}

	if orphanedCount > 0 {
		log.FromContext(ctx).Info("orphaned instance cleanup completed", "deletedCount", orphanedCount)
	} else {
		log.FromContext(ctx).V(1).Info("no orphaned instances found")
	}

	return nil
}

func (r *ExoscaleNodeClassReconciler) orphanedNodeClaimName(inst egov3.ListInstancesResponseInstances, validNodeClaims map[string]bool) (string, bool) {
	nodeClaimName, hasNodeClaim := inst.Labels[constants.InstanceLabelNodeClaim]
	if !hasNodeClaim || validNodeClaims[nodeClaimName] ||
		inst.Labels[constants.InstanceLabelManagedBy] != constants.ManagedByKarpenter ||
		inst.Labels[constants.InstanceLabelClusterID] != r.ClusterID {
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

func (r *ExoscaleNodeClassReconciler) secretToNodeClasses(ctx context.Context, obj client.Object) []reconcile.Request {
	var list apiv1.ExoscaleNodeClassList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	var out []reconcile.Request
	for i := range list.Items {
		nc := &list.Items[i]
		if nc.Spec.ContainerRegistry != nil && referencesSecret(nc.Spec.ContainerRegistry, obj.GetName()) {
			out = append(out, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(nc)})
		}
	}
	return out
}

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

func validateSpec(nodeClass *apiv1.ExoscaleNodeClass) error {
	kr := nodeClass.Spec.Kubelet.KubeReserved
	if err := validateResourceQuantities(kr.CPU, kr.Memory, kr.EphemeralStorage); err != nil {
		return fmt.Errorf("invalid kubelet.kubeReserved: %w", err)
	}

	sr := nodeClass.Spec.Kubelet.SystemReserved
	if err := validateResourceQuantities(sr.CPU, sr.Memory, sr.EphemeralStorage); err != nil {
		return fmt.Errorf("invalid kubelet.systemReserved: %w", err)
	}

	// Mirror of the CEL rules on the CRD (defense in depth for objects that
	// bypass apiserver admission, e.g. older CRDs): vpcSelector is mandatory
	// when vpcSubnetSelectorTerms is set, and vice versa.
	if len(nodeClass.Spec.VpcSubnetSelectorTerms) > 0 && nodeClass.Spec.VpcSelector == nil {
		return fmt.Errorf("spec.vpcSelector is required when spec.vpcSubnetSelectorTerms is set")
	}
	if nodeClass.Spec.VpcSelector != nil && len(nodeClass.Spec.VpcSubnetSelectorTerms) == 0 {
		return fmt.Errorf("spec.vpcSelector requires at least one entry in spec.vpcSubnetSelectorTerms")
	}
	if sel := nodeClass.Spec.VpcSelector; sel != nil && (sel.ID == "") == (sel.Name == "") {
		return fmt.Errorf("spec.vpcSelector must set exactly one of id or name")
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
