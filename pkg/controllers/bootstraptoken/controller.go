package bootstraptoken

import (
	"context"
	"time"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
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

const (
	AnnotationNodeRegistered = "exoscale.com/node-registered"
)

type Controller struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=karpenter.sh,resources=nodeclaims,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *Controller) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("secret", req.NamespacedName))

	if req.NamespacedName.Name == "cleanup-tokens" {
		return r.cleanupTokens(ctx)
	}

	secret := &v1.Secret{}
	if err := r.Get(ctx, req.NamespacedName, secret); err != nil {
		if errors.IsNotFound(err) {
			log.FromContext(ctx).V(1).Info("secret not found, ignoring")
			return reconcile.Result{}, nil
		}
		log.FromContext(ctx).Error(err, "failed to get secret")
		return reconcile.Result{}, err
	}

	if !r.isBootstrapToken(secret) {
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

	return r.monitorToken(ctx, secret)
}

func (r *Controller) isBootstrapToken(secret *v1.Secret) bool {
	if secret.Type != v1.SecretTypeBootstrapToken {
		return false
	}

	if provider, ok := secret.Labels[constants.LabelTokenProvider]; !ok || provider != constants.ProviderName {
		return false
	}

	return true
}

func isTokenExpired(secret *v1.Secret, checkTime time.Time) bool {
	createdStr, ok := secret.Annotations[constants.AnnotationTokenCreated]
	if !ok {
		return checkTime.Sub(secret.CreationTimestamp.Time) > constants.DefaultBootstrapTokenTTL
	}

	created, err := time.Parse(time.RFC3339, createdStr)
	if err != nil {
		return true
	}

	return checkTime.Sub(created) > constants.DefaultBootstrapTokenTTL
}

func isNodeAlreadyMarkedRegistered(secret *v1.Secret) bool {
	if secret.Annotations == nil {
		return false
	}
	registeredStr, ok := secret.Annotations[AnnotationNodeRegistered]
	return ok && registeredStr == "true"
}

func isNodeUsingBootstrapToken(node *v1.Node, tokenSecretName string) bool {
	if bootstrapToken, ok := node.Annotations[constants.AnnotationBootstrapToken]; ok {
		return bootstrapToken == tokenSecretName
	}
	return false
}

func (r *Controller) isNodeRegistered(ctx context.Context, secret *v1.Secret) bool {
	if isNodeAlreadyMarkedRegistered(secret) {
		return true
	}

	nodeList := &v1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		log.FromContext(ctx).Error(err, "failed to list nodes")
		return false
	}

	for _, node := range nodeList.Items {
		if isNodeUsingBootstrapToken(&node, secret.Name) {
			if node.Status.Phase != "" && node.Status.Phase != v1.NodePending {
				log.FromContext(ctx).Info("node has registered with kubelet, marking token for cleanup", "node", node.Name, "phase", node.Status.Phase)
				r.markNodeRegistered(ctx, secret)
				return true
			}
			if len(node.Status.Conditions) > 0 {
				log.FromContext(ctx).Info("node has conditions (kubelet registered), marking token for cleanup", "node", node.Name)
				r.markNodeRegistered(ctx, secret)
				return true
			}
			log.FromContext(ctx).V(1).Info("node found but kubelet hasn't registered yet (Unknown), keeping token", "node", node.Name)
			return false
		}
	}

	return false
}

func (r *Controller) markNodeRegistered(ctx context.Context, secret *v1.Secret) {
	if secret.Annotations == nil {
		secret.Annotations = make(map[string]string)
	}
	secret.Annotations[AnnotationNodeRegistered] = "true"

	if err := r.Update(ctx, secret); err != nil {
		log.FromContext(ctx).Error(err, "failed to mark token as registered")
	}
}

func (r *Controller) cleanupToken(ctx context.Context, secret *v1.Secret) (reconcile.Result, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("token", secret.Name))
	log.FromContext(ctx).Info("cleaning up bootstrap token")

	r.Recorder.Eventf(secret, "Normal", "TokenCleanup", "Cleaning up bootstrap token")

	if err := r.Delete(ctx, secret); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		log.FromContext(ctx).Error(err, "failed to delete bootstrap token")
		r.Recorder.Eventf(secret, "Warning", "CleanupFailed", "Failed to cleanup token: %v", err)
		return reconcile.Result{RequeueAfter: 30 * time.Second}, err
	}

	return reconcile.Result{}, nil
}

func calculateTokenExpiryTime(secret *v1.Secret) time.Duration {
	createdStr, ok := secret.Annotations[constants.AnnotationTokenCreated]
	var timeUntilExpiry = constants.DefaultBootstrapTokenTTL

	if ok {
		if created, err := time.Parse(time.RFC3339, createdStr); err == nil {
			timeUntilExpiry = constants.DefaultBootstrapTokenTTL - time.Since(created)
		}
	} else {
		timeUntilExpiry = constants.DefaultBootstrapTokenTTL - time.Since(secret.CreationTimestamp.Time)
	}

	if timeUntilExpiry < 30*time.Second {
		timeUntilExpiry = 30 * time.Second
	}

	return timeUntilExpiry
}

func (r *Controller) monitorToken(ctx context.Context, secret *v1.Secret) (reconcile.Result, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("token", secret.Name))

	timeUntilExpiry := calculateTokenExpiryTime(secret)

	log.FromContext(ctx).V(2).Info("monitoring bootstrap token", "expiry", timeUntilExpiry)
	return reconcile.Result{RequeueAfter: timeUntilExpiry}, nil
}

func (r *Controller) cleanupTokens(ctx context.Context) (reconcile.Result, error) {
	log.FromContext(ctx).V(1).Info("performing periodic bootstrap token cleanup")

	secretList := &v1.SecretList{}
	if err := r.List(ctx, secretList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			constants.LabelTokenProvider: constants.ProviderName,
		}),
	}); err != nil {
		log.FromContext(ctx).Error(err, "failed to list bootstrap token secrets")
		return reconcile.Result{RequeueAfter: constants.DefaultOperationTimeout}, err
	}

	cleaned := 0
	for _, secret := range secretList.Items {
		if secret.Type != v1.SecretTypeBootstrapToken {
			continue
		}

		shouldCleanup := isTokenExpired(&secret, time.Now()) || r.isNodeRegistered(ctx, &secret)

		if shouldCleanup {
			if err := r.Delete(ctx, &secret); err != nil && !errors.IsNotFound(err) {
				log.FromContext(ctx).Error(err, "failed to cleanup token during periodic cleanup", "token", secret.Name)
			} else {
				cleaned++
				log.FromContext(ctx).V(2).Info("cleaned up token during periodic cleanup", "token", secret.Name)
			}
		}
	}

	if cleaned > 0 {
		log.FromContext(ctx).Info("periodic cleanup completed", "tokens_cleaned", cleaned)
	}

	return reconcile.Result{RequeueAfter: constants.DefaultOperationTimeout}, nil
}

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	// NOTE: we watch only kube-system namespace because bootstrap tokens are always created there
	secretPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			secret, ok := e.Object.(*v1.Secret)
			if !ok {
				return false
			}
			if secret.Namespace != metav1.NamespaceSystem {
				return false
			}
			return r.isBootstrapToken(secret)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			secret, ok := e.ObjectNew.(*v1.Secret)
			if !ok {
				return false
			}
			if secret.Namespace != metav1.NamespaceSystem {
				return false
			}
			return r.isBootstrapToken(secret)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
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
	node, ok := obj.(*v1.Node)
	if !ok {
		return []reconcile.Request{}
	}

	tokenSecretName, ok := node.Annotations[constants.AnnotationBootstrapToken]
	if !ok {
		return []reconcile.Request{}
	}

	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      tokenSecretName,
				Namespace: metav1.NamespaceSystem,
			},
		},
	}
}
