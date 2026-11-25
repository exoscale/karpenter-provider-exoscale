package bootstraptoken

import (
	"context"
	"testing"
	"time"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func TestIsBootstrapToken(t *testing.T) {
	tests := []struct {
		name   string
		secret *v1.Secret
		want   bool
	}{
		{
			name: "valid bootstrap token",
			secret: &v1.Secret{
				Type: v1.SecretTypeBootstrapToken,
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						constants.LabelTokenProvider: constants.ProviderName,
					},
				},
			},
			want: true,
		},
		{
			name: "wrong type",
			secret: &v1.Secret{
				Type: v1.SecretTypeOpaque,
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						constants.LabelTokenProvider: constants.ProviderName,
					},
				},
			},
			want: false,
		},
		{
			name: "wrong provider",
			secret: &v1.Secret{
				Type: v1.SecretTypeBootstrapToken,
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						constants.LabelTokenProvider: "other-provider",
					},
				},
			},
			want: false,
		},
		{
			name: "no labels",
			secret: &v1.Secret{
				Type: v1.SecretTypeBootstrapToken,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBootstrapToken(tt.secret)
			if got != tt.want {
				t.Errorf("isBootstrapToken() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsTokenExpired(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name   string
		secret *v1.Secret
		now    time.Time
		want   bool
	}{
		{
			name: "token expired",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationTokenCreated: now.Add(-11 * time.Minute).Format(time.RFC3339),
					},
				},
			},
			now:  now,
			want: true,
		},
		{
			name: "token not expired",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationTokenCreated: now.Add(-5 * time.Minute).Format(time.RFC3339),
					},
				},
			},
			now:  now,
			want: false,
		},
		{
			name: "fallback to CreationTimestamp",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Time{Time: now.Add(-11 * time.Minute)},
				},
			},
			now:  now,
			want: true,
		},
		{
			name: "unparseable annotation counts as expired",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{constants.AnnotationTokenCreated: "garbage"},
				},
			},
			now:  now,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTokenExpired(tt.secret, tt.now)
			if got != tt.want {
				t.Errorf("isTokenExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateTokenExpiryTime(t *testing.T) {
	tests := []struct {
		name       string
		secret     *v1.Secret
		wantBefore time.Duration
		wantAfter  time.Duration
	}{
		{
			name: "valid annotation",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationTokenCreated: time.Now().Add(-1 * time.Minute).Format(time.RFC3339),
					},
				},
			},
			wantBefore: constants.DefaultBootstrapTokenTTL,
			wantAfter:  8 * time.Minute,
		},
		{
			name: "fallback to CreationTimestamp",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Time{Time: time.Now().Add(-2 * time.Minute)},
				},
			},
			wantBefore: constants.DefaultBootstrapTokenTTL,
			wantAfter:  7 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateTokenExpiryTime(tt.secret)
			if got >= tt.wantBefore || got <= tt.wantAfter {
				t.Errorf("calculateTokenExpiryTime() = %v, want between %v and %v", got, tt.wantAfter, tt.wantBefore)
			}
		})
	}
}

func TestReconcileCleansUpConsumedTokens(t *testing.T) {
	registered := &karpv1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Name: "registered"}}
	registered.StatusConditions().SetTrue(karpv1.ConditionTypeRegistered)
	launching := &karpv1.NodeClaim{ObjectMeta: metav1.ObjectMeta{Name: "launching"}}

	tests := []struct {
		name      string
		nodeClaim string // value of the token's bootstrap-token annotation
		wantKept  bool
	}{
		{name: "node registered", nodeClaim: "registered", wantKept: false},
		{name: "NodeClaim deleted", nodeClaim: "gone", wantKept: false},
		{name: "node not registered yet", nodeClaim: "launching", wantKept: true},
		{name: "no NodeClaim annotation", nodeClaim: "", wantKept: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "bootstrap-token-abcdef",
					Namespace:   metav1.NamespaceSystem,
					Labels:      map[string]string{constants.LabelTokenProvider: constants.ProviderName},
					Annotations: map[string]string{constants.AnnotationTokenCreated: time.Now().Format(time.RFC3339)},
				},
				Type: v1.SecretTypeBootstrapToken,
			}
			if tt.nodeClaim != "" {
				secret.Annotations[constants.AnnotationBootstrapToken] = tt.nodeClaim
			}
			c := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(secret, registered.DeepCopy(), launching.DeepCopy()).Build()
			r := &Controller{Client: c, Recorder: events.NewFakeRecorder(10)}

			if _, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(secret)}); err != nil {
				t.Fatal(err)
			}
			err := c.Get(context.Background(), client.ObjectKeyFromObject(secret), &v1.Secret{})
			if kept := err == nil; kept != tt.wantKept {
				t.Errorf("token kept = %t, want %t (get error: %v)", kept, tt.wantKept, err)
			}
		})
	}
}
