package cloudprovider

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type env struct {
	ctx       context.Context
	k8sClient client.Client
	cp        *CloudProvider
}

func setup(t *testing.T, objects ...client.Object) *env {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	builder := fakeclient.NewClientBuilder().WithScheme(scheme)
	if len(objects) > 0 {
		builder = builder.WithObjects(objects...)
	}
	kubeClient := builder.Build()

	cp := &CloudProvider{
		kubeClient:      kubeClient,
		clusterName:     "test-cluster",
		clusterEndpoint: "https://api.test-cluster.local",
	}

	return &env{
		ctx:       context.Background(),
		k8sClient: kubeClient,
		cp:        cp,
	}
}

func (env *env) createSecret(name, namespace string, data map[string][]byte) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Type: corev1.SecretType("bootstrap.kubernetes.io/token"),
		Data: data,
	}
	err := env.k8sClient.Create(env.ctx, secret)
	if err != nil {
		panic(fmt.Sprintf("failed to create test secret: %v", err))
	}
	return secret
}

func (env *env) getSecret(name, namespace string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	err := env.k8sClient.Get(env.ctx, client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, secret)
	return secret, err
}

func TestExtractTokenID(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		wantTokenID string
		wantError   bool
	}{
		{"valid token", "abc123.defghijklmnopqrs", "abc123", false},
		{"invalid format", "invalid-token", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenID, err := extractTokenID(tt.token)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantTokenID, tokenID)
			}
		})
	}
}

func TestDeleteBootstrapTokenSecret(t *testing.T) {
	tests := []struct {
		name         string
		token        string
		createSecret bool
	}{
		{"existing secret", "test01.abcdefghijklmnop", true},
		{"nonexistent secret", "nonexistent.tokensecretvalue", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setup(t)

			if tt.createSecret {
				parts := strings.Split(tt.token, ".")
				if len(parts) == 2 {
					env.createSecret("bootstrap-token-"+parts[0], "kube-system", map[string][]byte{
						"token-id":     []byte(parts[0]),
						"token-secret": []byte(parts[1]),
					})
				}
			}

			err := env.cp.deleteBootstrapTokenSecret(env.ctx, tt.token)

			assert.NoError(t, err)
			if tt.createSecret && strings.Contains(tt.token, ".") {
				parts := strings.Split(tt.token, ".")
				_, err := env.getSecret("bootstrap-token-"+parts[0], "kube-system")
				assert.True(t, apierrors.IsNotFound(err))
			}
		})
	}
}

func TestBuildBootstrapTokenSecret(t *testing.T) {
	secret := buildBootstrapTokenSecret("test01", "abcdefghijklmnop", "test-node-claim")

	assert.Equal(t, "bootstrap-token-test01", secret.Name)
	assert.Equal(t, "kube-system", secret.Namespace)
	assert.Equal(t, corev1.SecretType("bootstrap.kubernetes.io/token"), secret.Type)
	assert.Equal(t, "karpenter-exoscale", secret.Labels[LabelTokenProvider])
	assert.Equal(t, "test-node-claim", secret.Annotations[AnnotationBootstrapToken])
	assert.NotEmpty(t, secret.Annotations[AnnotationTokenCreated])
	assert.Equal(t, []byte("test01"), secret.Data["token-id"])
	assert.Equal(t, []byte("abcdefghijklmnop"), secret.Data["token-secret"])
	assert.Equal(t, []byte("true"), secret.Data["usage-bootstrap-authentication"])
	assert.Equal(t, []byte("true"), secret.Data["usage-bootstrap-signing"])
	assert.Contains(t, string(secret.Data["description"]), "test-node-claim")

	expiration, err := time.Parse(time.RFC3339, string(secret.Data["expiration"]))
	require.NoError(t, err)
	ttl := expiration.Sub(time.Now())
	assert.Greater(t, ttl, 4*time.Minute)
	assert.Less(t, ttl, 6*time.Minute)
}

func TestApplyBootstrapTokenSecret(t *testing.T) {
	env := setup(t)

	secret, token, err := env.cp.applyBootstrapTokenSecret(env.ctx, "test-node-claim")

	require.NoError(t, err)
	assert.NotNil(t, secret)

	parts := strings.Split(token, ".")
	require.Len(t, parts, 2)
	assert.Len(t, parts[0], 6)
	assert.Len(t, parts[1], 16)

	saved, err := env.getSecret(secret.Name, "kube-system")
	require.NoError(t, err)
	assert.Equal(t, "test-node-claim", saved.Annotations[AnnotationBootstrapToken])

	expiration, err := time.Parse(time.RFC3339, string(secret.Data["expiration"]))
	require.NoError(t, err)
	ttl := expiration.Sub(time.Now())
	assert.Greater(t, ttl, 4*time.Minute)
	assert.Less(t, ttl, 6*time.Minute)
}

func TestApplyBootstrapTokenSecret_Uniqueness(t *testing.T) {
	env := setup(t)

	_, token1, err1 := env.cp.applyBootstrapTokenSecret(env.ctx, "node-claim-1")
	_, token2, err2 := env.cp.applyBootstrapTokenSecret(env.ctx, "node-claim-2")

	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.NotEqual(t, token1, token2)
}

func TestGetBootstrapTokenSecretName(t *testing.T) {
	result := getBootstrapTokenSecretName("abc123")
	assert.Equal(t, "bootstrap-token-abc123", result)
}
