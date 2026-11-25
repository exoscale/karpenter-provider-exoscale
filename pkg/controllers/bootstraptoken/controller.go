package bootstraptoken

import (
	"context"
	"time"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type Controller struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=karpenter.sh,resources=nodeclaims,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="";events.k8s.io,resources=events,verbs=create;patch

func (r *Controller) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("secret", req.NamespacedName))

	secret := &v1.Secret{}
	if err := r.Get(ctx, req.NamespacedName, secret); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	if !isBootstrapToken(secret) {
		return reconcile.Result{}, nil
	}

	if isTokenExpired(secret, time.Now()) {
		log.FromContext(ctx).Info("bootstrap token expired, cleaning up")
		return r.cleanupToken(ctx, secret)
	}

	if r.isNodeRegistered(ctx, secret) {
		log.FromContext(ctx).Info("node registered, cleaning up bootstrap token")
		return r.cleanupToken(ctx, secret)
	}

	return reconcile.Result{RequeueAfter: calculateTokenExpiryTime(secret)}, nil
}

func isBootstrapToken(secret *v1.Secret) bool {
	return secret.Type == v1.SecretTypeBootstrapToken && secret.Labels[constants.LabelTokenProvider] == constants.ProviderName
}

// tokenCreatedAt returns false when the created annotation is unparseable.
func tokenCreatedAt(secret *v1.Secret) (time.Time, bool) {
	createdStr, ok := secret.Annotations[constants.AnnotationTokenCreated]
	if !ok {
		return secret.CreationTimestamp.Time, true
	}
	created, err := time.Parse(time.RFC3339, createdStr)
	return created, err == nil
}

func isTokenExpired(secret *v1.Secret, checkTime time.Time) bool {
	created, ok := tokenCreatedAt(secret)
	return !ok || checkTime.Sub(created) > constants.DefaultBootstrapTokenTTL
}

func (r *Controller) isNodeRegistered(ctx context.Context, secret *v1.Secret) bool {
	nodeList := &v1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		log.FromContext(ctx).Error(err, "failed to list nodes")
		return false
	}

	for _, node := range nodeList.Items {
		if node.Annotations[constants.AnnotationBootstrapToken] == secret.Name {
			if node.Status.Phase != "" && node.Status.Phase != v1.NodePending {
				log.FromContext(ctx).Info("node has registered with kubelet", "node", node.Name, "phase", node.Status.Phase)
				return true
			}
			if len(node.Status.Conditions) > 0 {
				log.FromContext(ctx).Info("node has conditions (kubelet registered)", "node", node.Name)
				return true
			}
			log.FromContext(ctx).V(1).Info("node found but kubelet hasn't registered yet (Unknown), keeping token", "node", node.Name)
			return false
		}
	}

	return false
}

func (r *Controller) cleanupToken(ctx context.Context, secret *v1.Secret) (reconcile.Result, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("token", secret.Name))
	log.FromContext(ctx).Info("cleaning up bootstrap token")

	r.Recorder.Eventf(secret, nil, "Normal", "TokenCleanup", "Cleanup", "Cleaning up bootstrap token")

	if err := r.Delete(ctx, secret); client.IgnoreNotFound(err) != nil {
		r.Recorder.Eventf(secret, nil, "Warning", "CleanupFailed", "CleanupFailed", "Failed to cleanup token: %v", err)
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// calculateTokenExpiryTime is only called for unexpired tokens, so tokenCreatedAt succeeds.
func calculateTokenExpiryTime(secret *v1.Secret) time.Duration {
	created, _ := tokenCreatedAt(secret)
	return max(constants.DefaultBootstrapTokenTTL-time.Since(created), 30*time.Second)
}

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	// Bootstrap tokens always live in kube-system.
	ours := func(obj client.Object) bool {
		secret, ok := obj.(*v1.Secret)
		return ok && secret.Namespace == metav1.NamespaceSystem && isBootstrapToken(secret)
	}
	secretPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool { return ours(e.Object) },
		UpdateFunc: func(e event.UpdateEvent) bool { return ours(e.ObjectNew) },
		DeleteFunc: func(event.DeleteEvent) bool { return false },
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Secret{}, builder.WithPredicates(secretPredicate)).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 5,
		}).
		Watches(
			&v1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.nodeToTokens),
		).
		Complete(r)
}

func (r *Controller) nodeToTokens(_ context.Context, obj client.Object) []reconcile.Request {
	tokenSecretName, ok := obj.GetAnnotations()[constants.AnnotationBootstrapToken]
	if !ok {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: tokenSecretName, Namespace: metav1.NamespaceSystem}}}
}
