package bootstraptoken

import (
	"context"
	"time"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
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

	consumed, err := r.tokenConsumed(ctx, secret)
	if err != nil {
		return reconcile.Result{}, err
	}
	if consumed {
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

// tokenConsumed reports whether the NodeClaim the token was issued for has
// registered its node, or no longer exists. Tokens without the annotation
// are left to expire.
func (r *Controller) tokenConsumed(ctx context.Context, secret *v1.Secret) (bool, error) {
	name := secret.Annotations[constants.AnnotationBootstrapToken]
	if name == "" {
		return false, nil
	}
	nodeClaim := &karpv1.NodeClaim{}
	if err := r.Get(ctx, types.NamespacedName{Name: name}, nodeClaim); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}
	return nodeClaim.StatusConditions().IsTrue(karpv1.ConditionTypeRegistered), nil
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
			&karpv1.NodeClaim{},
			handler.EnqueueRequestsFromMapFunc(r.nodeClaimToTokens),
		).
		Complete(r)
}

func (r *Controller) nodeClaimToTokens(ctx context.Context, obj client.Object) []reconcile.Request {
	var secrets v1.SecretList
	if err := r.List(ctx, &secrets, client.InNamespace(metav1.NamespaceSystem), client.MatchingLabels{constants.LabelTokenProvider: constants.ProviderName}); err != nil {
		log.FromContext(ctx).Error(err, "failed to list bootstrap tokens")
		return nil
	}
	var requests []reconcile.Request
	for _, secret := range secrets.Items {
		if secret.Annotations[constants.AnnotationBootstrapToken] == obj.GetName() {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&secret)})
		}
	}
	return requests
}
