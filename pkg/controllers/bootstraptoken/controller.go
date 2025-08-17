package bootstraptoken

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/exoscale/karpenter-exoscale/pkg/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	AnnotationBootstrapToken = "exoscale.com/bootstrap-token"
	AnnotationTokenCreated   = "exoscale.com/token-created"
	AnnotationNodeRegistered = "exoscale.com/node-registered"
	LabelTokenProvider       = "exoscale.com/token-provider"

	BootstrapTokenTimeout = 30 * time.Minute

	CleanupInterval = 5 * time.Minute

	TokenProviderName = "karpenter-exoscale"
)

type BootstrapTokenController struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=karpenter.sh,resources=nodeclaims,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *BootstrapTokenController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("secret", req.NamespacedName)

	if req.NamespacedName.Name == "cleanup-tokens" {
		return r.performCleanup(ctx)
	}

	secret := &v1.Secret{}
	if err := r.Get(ctx, req.NamespacedName, secret); err != nil {
		if errors.IsNotFound(err) {
			logger.V(1).Info("secret not found, ignoring")
			return reconcile.Result{}, nil
		}
		logger.Error(err, "failed to get secret")
		return reconcile.Result{}, err
	}

	if !r.isBootstrapToken(secret) {
		return reconcile.Result{}, nil
	}

	if r.isTokenExpired(secret) {
		logger.Info("bootstrap token expired, cleaning up")
		return r.cleanupToken(ctx, secret)
	}

	if r.isNodeRegistered(ctx, secret) {
		logger.Info("node registered, cleaning up bootstrap token")
		return r.cleanupToken(ctx, secret)
	}

	return r.monitorToken(ctx, secret)
}

func (r *BootstrapTokenController) isBootstrapToken(secret *v1.Secret) bool {
	if secret.Type != "bootstrap.kubernetes.io/token" {
		return false
	}

	if provider, ok := secret.Labels[LabelTokenProvider]; !ok || provider != TokenProviderName {
		return false
	}

	return true
}

func (r *BootstrapTokenController) isTokenExpired(secret *v1.Secret) bool {
	createdStr, ok := secret.Annotations[AnnotationTokenCreated]
	if !ok {
		if time.Since(secret.CreationTimestamp.Time) > BootstrapTokenTimeout {
			return true
		}
		return false
	}

	created, err := time.Parse(time.RFC3339, createdStr)
	if err != nil {
		return true
	}

	return time.Since(created) > BootstrapTokenTimeout
}

func isNodeAlreadyMarkedRegistered(secret *v1.Secret) bool {
	if secret.Annotations == nil {
		return false
	}
	registeredStr, ok := secret.Annotations[AnnotationNodeRegistered]
	return ok && registeredStr == "true"
}

func isNodeMatchingNodeClaim(node *v1.Node, nodeClaimName string) bool {
	if strings.Contains(node.Spec.ProviderID, nodeClaimName) {
		return true
	}
	if nodeClaim, ok := node.Annotations["karpenter.sh/node-claim"]; ok && nodeClaim == nodeClaimName {
		return true
	}
	return false
}

func (r *BootstrapTokenController) isNodeRegistered(ctx context.Context, secret *v1.Secret) bool {
	nodeClaimName, ok := secret.Annotations[AnnotationBootstrapToken]
	if !ok {
		return false
	}

	if isNodeAlreadyMarkedRegistered(secret) {
		return true
	}

	nodeList := &v1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		log.FromContext(ctx).Error(err, "failed to list nodes")
		return false
	}

	for _, node := range nodeList.Items {
		if isNodeMatchingNodeClaim(&node, nodeClaimName) {
			r.markNodeRegistered(ctx, secret)
			return true
		}
	}

	return false
}

func (r *BootstrapTokenController) markNodeRegistered(ctx context.Context, secret *v1.Secret) {
	if secret.Annotations == nil {
		secret.Annotations = make(map[string]string)
	}
	secret.Annotations[AnnotationNodeRegistered] = "true"

	if err := r.Update(ctx, secret); err != nil {
		log.FromContext(ctx).Error(err, "failed to mark token as registered")
	}
}

func (r *BootstrapTokenController) cleanupToken(ctx context.Context, secret *v1.Secret) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("token", secret.Name)
	logger.Info("cleaning up bootstrap token")

	r.Recorder.Eventf(secret, "Normal", "TokenCleanup", "Cleaning up bootstrap token")

	if err := r.Delete(ctx, secret); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		logger.Error(err, "failed to delete bootstrap token")
		r.Recorder.Eventf(secret, "Warning", "CleanupFailed", "Failed to cleanup token: %v", err)
		return reconcile.Result{RequeueAfter: 30 * time.Second}, err
	}

	return reconcile.Result{}, nil
}

func calculateTokenExpiryTime(secret *v1.Secret) time.Duration {
	createdStr, ok := secret.Annotations[AnnotationTokenCreated]
	var timeUntilExpiry = BootstrapTokenTimeout

	if ok {
		if created, err := time.Parse(time.RFC3339, createdStr); err == nil {
			timeUntilExpiry = BootstrapTokenTimeout - time.Since(created)
		}
	} else {
		timeUntilExpiry = BootstrapTokenTimeout - time.Since(secret.CreationTimestamp.Time)
	}

	if timeUntilExpiry < 30*time.Second {
		timeUntilExpiry = 30 * time.Second
	}

	return timeUntilExpiry
}

func (r *BootstrapTokenController) monitorToken(ctx context.Context, secret *v1.Secret) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithValues("token", secret.Name)

	timeUntilExpiry := calculateTokenExpiryTime(secret)

	logger.V(2).Info("monitoring bootstrap token", "expiry", timeUntilExpiry)
	return reconcile.Result{RequeueAfter: timeUntilExpiry}, nil
}

func (r *BootstrapTokenController) performCleanup(ctx context.Context) (reconcile.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("performing periodic bootstrap token cleanup")

	secretList := &v1.SecretList{}
	labelSelector := labels.SelectorFromSet(labels.Set{
		LabelTokenProvider: TokenProviderName,
	})

	if err := r.List(ctx, secretList, &client.ListOptions{
		LabelSelector: labelSelector,
	}); err != nil {
		logger.Error(err, "failed to list bootstrap token secrets")
		return reconcile.Result{RequeueAfter: CleanupInterval}, err
	}

	cleaned := 0
	for _, secret := range secretList.Items {
		if secret.Type != "bootstrap.kubernetes.io/token" {
			continue
		}

		shouldCleanup := r.isTokenExpired(&secret) || r.isNodeRegistered(ctx, &secret)

		if shouldCleanup {
			if err := r.Delete(ctx, &secret); err != nil && !errors.IsNotFound(err) {
				logger.Error(err, "failed to cleanup token during periodic cleanup", "token", secret.Name)
			} else {
				cleaned++
				logger.V(2).Info("cleaned up token during periodic cleanup", "token", secret.Name)
			}
		}
	}

	if cleaned > 0 {
		logger.Info("periodic cleanup completed", "tokens_cleaned", cleaned)
	}

	return reconcile.Result{RequeueAfter: CleanupInterval}, nil
}

func buildBootstrapTokenSecretObject(tokenID, tokenSecret, nodeClaimName string) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("bootstrap-token-%s", tokenID),
			Namespace: "kube-system",
			Labels: map[string]string{
				LabelTokenProvider: TokenProviderName,
			},
			Annotations: map[string]string{
				AnnotationBootstrapToken: nodeClaimName,
				AnnotationTokenCreated:   time.Now().Format(time.RFC3339),
			},
		},
		Type: "bootstrap.kubernetes.io/token",
		Data: map[string][]byte{
			"token-id":                       []byte(tokenID),
			"token-secret":                   []byte(tokenSecret),
			"description":                    []byte(fmt.Sprintf("Bootstrap token for Karpenter NodeClaim %s", nodeClaimName)),
			"expiration":                     []byte(time.Now().Add(BootstrapTokenTimeout).Format(time.RFC3339)),
			"usage-bootstrap-authentication": []byte("true"),
			"usage-bootstrap-signing":        []byte("true"),
			"auth-extra-groups":              []byte("system:bootstrappers:karpenter-exoscale"),
		},
	}
}

func (r *BootstrapTokenController) CreateBootstrapToken(ctx context.Context, nodeClaimName string) (*v1.Secret, error) {
	logger := log.FromContext(ctx).WithValues("nodeClaim", nodeClaimName)
	logger.Info("creating bootstrap token")

	tokenID, err := utils.GenerateBootstrapTokenID()
	if err != nil {
		logger.Error(err, "failed to generate token ID")
		return nil, fmt.Errorf("failed to generate token ID: %w", err)
	}

	tokenSecret, err := utils.GenerateBootstrapTokenSecret()
	if err != nil {
		logger.Error(err, "failed to generate token secret")
		return nil, fmt.Errorf("failed to generate token secret: %w", err)
	}

	secret := buildBootstrapTokenSecretObject(tokenID, tokenSecret, nodeClaimName)

	if err := r.Create(ctx, secret); err != nil {
		logger.Error(err, "failed to create bootstrap token")
		return nil, fmt.Errorf("failed to create bootstrap token: %w", err)
	}

	r.Recorder.Eventf(secret, "Normal", "TokenCreated", "Bootstrap token created for NodeClaim %s", nodeClaimName)

	return secret, nil
}

func (r *BootstrapTokenController) SetupWithManager(mgr ctrl.Manager) error {
	secretPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return r.isBootstrapToken(e.Object.(*v1.Secret))
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return r.isBootstrapToken(e.ObjectNew.(*v1.Secret))
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Secret{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 5,
		}).
		WithEventFilter(secretPredicate).
		Watches(
			&v1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.nodeToTokens),
		).
		Complete(r)
}

func (r *BootstrapTokenController) nodeToTokens(ctx context.Context, obj client.Object) []reconcile.Request {
	node, ok := obj.(*v1.Node)
	if !ok {
		return []reconcile.Request{}
	}

	secretList := &v1.SecretList{}
	labelSelector := labels.SelectorFromSet(labels.Set{
		LabelTokenProvider: TokenProviderName,
	})

	if err := r.List(ctx, secretList, &client.ListOptions{
		Namespace:     "kube-system",
		LabelSelector: labelSelector,
	}); err != nil {
		return []reconcile.Request{}
	}

	var requests []reconcile.Request
	for _, secret := range secretList.Items {
		if secret.Type != "bootstrap.kubernetes.io/token" {
			continue
		}

		nodeClaimName, ok := secret.Annotations[AnnotationBootstrapToken]
		if !ok {
			continue
		}

		if strings.Contains(node.Spec.ProviderID, nodeClaimName) ||
			(node.Annotations != nil && node.Annotations["karpenter.sh/node-claim"] == nodeClaimName) {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      secret.Name,
					Namespace: secret.Namespace,
				},
			})
		}
	}

	return requests
}
