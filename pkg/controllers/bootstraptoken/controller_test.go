package bootstraptoken

import (
	"testing"
	"time"

	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsBootstrapToken(t *testing.T) {
	c := &Controller{}

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
			got := c.isBootstrapToken(tt.secret)
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

func TestIsNodeAlreadyMarkedRegistered(t *testing.T) {
	tests := []struct {
		name   string
		secret *v1.Secret
		want   bool
	}{
		{
			name: "marked registered",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationNodeRegistered: "true",
					},
				},
			},
			want: true,
		},
		{
			name: "not marked registered",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationNodeRegistered: "false",
					},
				},
			},
			want: false,
		},
		{
			name:   "no annotation",
			secret: &v1.Secret{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNodeAlreadyMarkedRegistered(tt.secret)
			if got != tt.want {
				t.Errorf("isNodeAlreadyMarkedRegistered() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsNodeUsingBootstrapToken(t *testing.T) {
	tests := []struct {
		name            string
		node            *v1.Node
		tokenSecretName string
		want            bool
	}{
		{
			name: "node using token",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationBootstrapToken: "test-token",
					},
				},
			},
			tokenSecretName: "test-token",
			want:            true,
		},
		{
			name: "node using different token",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationBootstrapToken: "other-token",
					},
				},
			},
			tokenSecretName: "test-token",
			want:            false,
		},
		{
			name:            "no annotation",
			node:            &v1.Node{},
			tokenSecretName: "test-token",
			want:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNodeUsingBootstrapToken(tt.node, tt.tokenSecretName)
			if got != tt.want {
				t.Errorf("isNodeUsingBootstrapToken() = %v, want %v", got, tt.want)
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
