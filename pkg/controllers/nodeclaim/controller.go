package nodeclaim

import (
	"context"
	"slices"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-exoscale/pkg/errors"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instance"
	"github.com/exoscale/karpenter-exoscale/pkg/utils"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/events"
)

const (
	Finalizer = "karpenter.exoscale.com/nodeclaim"
)

type NodeClaimReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	ExoscaleClient instance.EgoscaleClient
	Recorder       events.Recorder
	Zone           string
	ClusterName    string
}

// +kubebuilder:rbac:groups=karpenter.sh,resources=nodeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=karpenter.sh,resources=nodeclaims/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=karpenter.sh,resources=nodeclaims/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

func (r *NodeClaimReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	nodeClaim := &karpenterv1.NodeClaim{}
	if err := r.Get(ctx, req.NamespacedName, nodeClaim); err != nil {
		if k8serrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	if !nodeClaim.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, nodeClaim)
	}

	if !slices.Contains(nodeClaim.Finalizers, Finalizer) {
		nodeClaim.Finalizers = append(nodeClaim.Finalizers, Finalizer)
		if err := r.Update(ctx, nodeClaim); err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{RequeueAfter: time.Minute}, nil
	}

	if nodeClaim.Status.ProviderID != "" {
		return r.ensureInstanceTags(ctx, nodeClaim)
	}

	return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *NodeClaimReconciler) handleDeletion(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("nodeclaim", nodeClaim.Name)

	if !slices.Contains(nodeClaim.Finalizers, Finalizer) {
		return reconcile.Result{}, nil
	}

	if nodeClaim.Status.NodeName != "" {
		node := &v1.Node{}
		if err := r.Get(ctx, types.NamespacedName{Name: nodeClaim.Status.NodeName}, node); err == nil {
			if err := r.Delete(ctx, node); err != nil && !k8serrors.IsNotFound(err) {
				logger.Error(err, "failed to delete node")
				return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
			}
		}
	}

	nodeClaim.Finalizers = slices.DeleteFunc(nodeClaim.Finalizers, func(s string) bool { return s == Finalizer })
	if err := r.Update(ctx, nodeClaim); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *NodeClaimReconciler) ensureInstanceTags(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("nodeclaim", nodeClaim.Name)

	instanceID, err := utils.ParseProviderID(nodeClaim.Status.ProviderID)
	if err != nil {
		return reconcile.Result{}, err
	}

	exoInstance, err := r.ExoscaleClient.GetInstance(ctx, egov3.UUID(instanceID))
	if err != nil {
		if r.isInstanceNotFound(err) {
			logger.Info("instance not found")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}

	expectedLabels := map[string]string{
		constants.LabelManagedBy:   constants.ManagedByKarpenter,
		constants.LabelClusterName: r.ClusterName,
		constants.LabelNodeClaim:   nodeClaim.Name,
	}

	needsUpdate := false
	for k, v := range expectedLabels {
		if currentValue, ok := exoInstance.Labels[k]; !ok || currentValue != v {
			needsUpdate = true
			break
		}
	}

	if needsUpdate {
		logger.V(1).Info("instance labels need update")
	}

	return reconcile.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *NodeClaimReconciler) isInstanceNotFound(err error) bool {
	return errors.IsInstanceNotFoundError(err)
}

func (r *NodeClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&karpenterv1.NodeClaim{}).
		Watches(
			&v1.Node{},
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				return r.nodeToNodeClaim(obj)
			}),
		).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 10,
		}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

func (r *NodeClaimReconciler) nodeToNodeClaim(obj client.Object) []reconcile.Request {
	node := obj.(*v1.Node)

	if nodeClaimName, ok := node.Annotations["karpenter.sh/node-claim"]; ok {
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{
				Name: nodeClaimName,
			},
		}}
	}

	nodeClaimList := &karpenterv1.NodeClaimList{}
	if err := r.List(context.Background(), nodeClaimList); err != nil {
		return nil
	}

	for _, nodeClaim := range nodeClaimList.Items {
		if nodeClaim.Status.ProviderID == node.Spec.ProviderID {
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Name: nodeClaim.Name,
				},
			}}
		}
	}

	return nil
}
