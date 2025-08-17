package bootstraptoken

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type env struct {
	ctx        context.Context
	k8sClient  client.Client
	controller *BootstrapTokenController
}

func setup(t *testing.T, objects ...client.Object) *env {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, v1.AddToScheme(scheme))

	builder := fake.NewClientBuilder().WithScheme(scheme)
	if len(objects) > 0 {
		builder = builder.WithObjects(objects...)
	}
	k8sClient := builder.Build()

	controller := &BootstrapTokenController{
		Client:   k8sClient,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(100),
	}

	return &env{
		ctx:        context.Background(),
		k8sClient:  k8sClient,
		controller: controller,
	}
}

func createBootstrapSecret(name, nodeClaimName string) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
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
			"token-id":     []byte("abc123"),
			"token-secret": []byte("def456"),
		},
	}
}

func createExpiredBootstrapSecret(name, nodeClaimName string) *v1.Secret {
	expiredTime := time.Now().Add(-BootstrapTokenTimeout - time.Hour)
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "kube-system",
			Labels: map[string]string{
				LabelTokenProvider: TokenProviderName,
			},
			Annotations: map[string]string{
				AnnotationBootstrapToken: nodeClaimName,
				AnnotationTokenCreated:   expiredTime.Format(time.RFC3339),
			},
		},
		Type: "bootstrap.kubernetes.io/token",
		Data: map[string][]byte{
			"token-id":     []byte("abc123"),
			"token-secret": []byte("def456"),
		},
	}
}

func createRegisteredBootstrapSecret(name, nodeClaimName string) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "kube-system",
			Labels: map[string]string{
				LabelTokenProvider: TokenProviderName,
			},
			Annotations: map[string]string{
				AnnotationBootstrapToken: nodeClaimName,
				AnnotationTokenCreated:   time.Now().Format(time.RFC3339),
				AnnotationNodeRegistered: "true",
			},
		},
		Type: "bootstrap.kubernetes.io/token",
		Data: map[string][]byte{
			"token-id":     []byte("abc123"),
			"token-secret": []byte("def456"),
		},
	}
}

func createNode(name, providerID, nodeClaimName string) *v1.Node {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.NodeSpec{
			ProviderID: providerID,
		},
	}

	if nodeClaimName != "" {
		node.Annotations = map[string]string{
			"karpenter.sh/node-claim": nodeClaimName,
		}
	}

	return node
}

func TestReconcile(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(*env)
		request        reconcile.Request
		expectedResult reconcile.Result
		expectedError  bool
		verify         func(*testing.T, *env)
	}{
		{
			name: "IgnoresNonBootstrapSecrets",
			setup: func(s *env) {
				regularSecret := &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "regular-secret",
						Namespace: "kube-system",
					},
					Type: "Opaque",
				}
				require.NoError(t, s.controller.Create(s.ctx, regularSecret))
			},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "regular-secret",
					Namespace: "kube-system",
				},
			},
			expectedResult: reconcile.Result{},
		},
		{
			name:  "HandlesSecretNotFound",
			setup: func(s *env) {},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "non-existent-secret",
					Namespace: "kube-system",
				},
			},
			expectedResult: reconcile.Result{},
		},
		{
			name: "CleansUpExpiredToken",
			setup: func(s *env) {
				expiredSecret := createExpiredBootstrapSecret("expired-token", "test-nodeclaim")
				require.NoError(t, s.controller.Create(s.ctx, expiredSecret))
			},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "expired-token",
					Namespace: "kube-system",
				},
			},
			expectedResult: reconcile.Result{},
			verify: func(t *testing.T, s *env) {
				var deletedSecret v1.Secret
				err := s.controller.Get(s.ctx, types.NamespacedName{Name: "expired-token", Namespace: "kube-system"}, &deletedSecret)
				assert.True(t, errors.IsNotFound(err))
			},
		},
		{
			name: "CleansUpTokenForRegisteredNode",
			setup: func(s *env) {
				nodeClaimName := "test-nodeclaim"
				token := createBootstrapSecret("bootstrap-token", nodeClaimName)
				require.NoError(t, s.controller.Create(s.ctx, token))

				node := createNode("test-node", "exoscale://test-instance", nodeClaimName)
				require.NoError(t, s.controller.Create(s.ctx, node))
			},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "bootstrap-token",
					Namespace: "kube-system",
				},
			},
			expectedResult: reconcile.Result{},
			verify: func(t *testing.T, s *env) {
				var deletedSecret v1.Secret
				err := s.controller.Get(s.ctx, types.NamespacedName{Name: "bootstrap-token", Namespace: "kube-system"}, &deletedSecret)
				assert.True(t, errors.IsNotFound(err))
			},
		},
		{
			name: "MonitorsActiveToken",
			setup: func(s *env) {
				token := createBootstrapSecret("active-token", "test-nodeclaim")
				require.NoError(t, s.controller.Create(s.ctx, token))
			},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "active-token",
					Namespace: "kube-system",
				},
			},
			verify: func(t *testing.T, s *env) {
				// Result is checked inline due to dynamic RequeueAfter value
			},
		},
		{
			name: "PerformCleanupRequest",
			setup: func(s *env) {
				expiredToken := createExpiredBootstrapSecret("expired-token", "expired-nodeclaim")
				require.NoError(t, s.controller.Create(s.ctx, expiredToken))

				registeredToken := createRegisteredBootstrapSecret("registered-token", "registered-nodeclaim")
				require.NoError(t, s.controller.Create(s.ctx, registeredToken))

				activeToken := createBootstrapSecret("active-token", "active-nodeclaim")
				require.NoError(t, s.controller.Create(s.ctx, activeToken))
			},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: "cleanup-tokens",
				},
			},
			verify: func(t *testing.T, s *env) {
				var deletedSecret v1.Secret
				err := s.controller.Get(s.ctx, types.NamespacedName{Name: "expired-token", Namespace: "kube-system"}, &deletedSecret)
				assert.True(t, errors.IsNotFound(err))

				err = s.controller.Get(s.ctx, types.NamespacedName{Name: "registered-token", Namespace: "kube-system"}, &deletedSecret)
				assert.True(t, errors.IsNotFound(err))

				var activeSecret v1.Secret
				err = s.controller.Get(s.ctx, types.NamespacedName{Name: "active-token", Namespace: "kube-system"}, &activeSecret)
				assert.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := setup(t)

			if tt.setup != nil {
				tt.setup(s)
			}

			result, err := s.controller.Reconcile(s.ctx, tt.request)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tt.name == "MonitorsActiveToken" {
				assert.True(t, result.RequeueAfter > 0)
				assert.True(t, result.RequeueAfter <= BootstrapTokenTimeout)
			} else if tt.name == "PerformCleanupRequest" {
				assert.Equal(t, CleanupInterval, result.RequeueAfter)
			} else {
				assert.Equal(t, tt.expectedResult, result)
			}

			if tt.verify != nil {
				tt.verify(t, s)
			}
		})
	}
}

func TestIsBootstrapToken(t *testing.T) {
	tests := []struct {
		name     string
		secret   *v1.Secret
		expected bool
	}{
		{
			name: "ValidToken",
			secret: &v1.Secret{
				Type: "bootstrap.kubernetes.io/token",
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						LabelTokenProvider: TokenProviderName,
					},
					Annotations: map[string]string{
						AnnotationBootstrapToken: "test-nodeclaim",
					},
				},
			},
			expected: true,
		},
		{
			name: "WrongType",
			secret: &v1.Secret{
				Type: "Opaque",
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						LabelTokenProvider: TokenProviderName,
					},
				},
			},
			expected: false,
		},
		{
			name: "WrongProvider",
			secret: &v1.Secret{
				Type: "bootstrap.kubernetes.io/token",
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						LabelTokenProvider: "different-provider",
					},
				},
			},
			expected: false,
		},
		{
			name: "MissingProviderLabel",
			secret: &v1.Secret{
				Type: "bootstrap.kubernetes.io/token",
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := setup(t)
			result := s.controller.isBootstrapToken(tt.secret)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsTokenExpired(t *testing.T) {
	tests := []struct {
		name     string
		secret   func() *v1.Secret
		expected bool
	}{
		{
			name: "ExpiredWithCreatedAnnotation",
			secret: func() *v1.Secret {
				expiredTime := time.Now().Add(-BootstrapTokenTimeout - time.Hour)
				return &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							AnnotationTokenCreated: expiredTime.Format(time.RFC3339),
						},
					},
				}
			},
			expected: true,
		},
		{
			name: "NotExpiredWithCreatedAnnotation",
			secret: func() *v1.Secret {
				recentTime := time.Now().Add(-time.Minute)
				return &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							AnnotationTokenCreated: recentTime.Format(time.RFC3339),
						},
					},
				}
			},
			expected: false,
		},
		{
			name: "ExpiredByCreationTimestamp",
			secret: func() *v1.Secret {
				expiredTime := time.Now().Add(-BootstrapTokenTimeout - time.Hour)
				return &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.Time{Time: expiredTime},
					},
				}
			},
			expected: true,
		},
		{
			name: "InvalidCreatedAnnotation",
			secret: func() *v1.Secret {
				return &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							AnnotationTokenCreated: "invalid-time-format",
						},
					},
				}
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := setup(t)
			result := s.controller.isTokenExpired(tt.secret())
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsNodeAlreadyMarkedRegistered(t *testing.T) {
	tests := []struct {
		name     string
		secret   *v1.Secret
		expected bool
	}{
		{
			name: "Registered",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationNodeRegistered: "true",
					},
				},
			},
			expected: true,
		},
		{
			name: "NotRegistered",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationNodeRegistered: "false",
					},
				},
			},
			expected: false,
		},
		{
			name:     "NoAnnotations",
			secret:   &v1.Secret{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNodeAlreadyMarkedRegistered(tt.secret)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsNodeMatchingNodeClaim(t *testing.T) {
	tests := []struct {
		name          string
		node          *v1.Node
		nodeClaimName string
		expected      bool
	}{
		{
			name: "ProviderIDMatch",
			node: &v1.Node{
				Spec: v1.NodeSpec{
					ProviderID: "exoscale://test-nodeclaim-123",
				},
			},
			nodeClaimName: "test-nodeclaim",
			expected:      true,
		},
		{
			name: "AnnotationMatch",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"karpenter.sh/node-claim": "test-nodeclaim",
					},
				},
				Spec: v1.NodeSpec{
					ProviderID: "exoscale://different-id",
				},
			},
			nodeClaimName: "test-nodeclaim",
			expected:      true,
		},
		{
			name: "NoMatch",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"karpenter.sh/node-claim": "different-nodeclaim",
					},
				},
				Spec: v1.NodeSpec{
					ProviderID: "exoscale://different-id",
				},
			},
			nodeClaimName: "test-nodeclaim",
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNodeMatchingNodeClaim(tt.node, tt.nodeClaimName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsNodeRegistered(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*env)
		secret   *v1.Secret
		expected bool
	}{
		{
			name: "AlreadyMarked",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationBootstrapToken: "test-nodeclaim",
						AnnotationNodeRegistered: "true",
					},
				},
			},
			expected: true,
		},
		{
			name: "NodeFound",
			setup: func(s *env) {
				nodeClaimName := "test-nodeclaim"
				node := createNode("test-node", "exoscale://test-instance", nodeClaimName)
				require.NoError(t, s.controller.Create(s.ctx, node))
			},
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-token",
					Namespace: "kube-system",
					Annotations: map[string]string{
						AnnotationBootstrapToken: "test-nodeclaim",
					},
				},
			},
			expected: true,
		},
		{
			name: "NodeNotFound",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						AnnotationBootstrapToken: "non-existent-nodeclaim",
					},
				},
			},
			expected: false,
		},
		{
			name: "MissingBootstrapTokenAnnotation",
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := setup(t)

			if tt.setup != nil {
				tt.setup(s)
			}

			result := s.controller.isNodeRegistered(s.ctx, tt.secret)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMarkNodeRegistered(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*env) *v1.Secret
		verify func(*testing.T, *env, *v1.Secret)
	}{
		{
			name: "Success",
			setup: func(s *env) *v1.Secret {
				secret := createBootstrapSecret("test-token", "test-nodeclaim")
				require.NoError(t, s.controller.Create(s.ctx, secret))
				return secret
			},
			verify: func(t *testing.T, s *env, secret *v1.Secret) {
				var updatedSecret v1.Secret
				err := s.controller.Get(s.ctx, types.NamespacedName{
					Name:      "test-token",
					Namespace: "kube-system",
				}, &updatedSecret)

				require.NoError(t, err)
				assert.Equal(t, "true", updatedSecret.Annotations[AnnotationNodeRegistered])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := setup(t)
			secret := tt.setup(s)
			s.controller.markNodeRegistered(s.ctx, secret)
			tt.verify(t, s, secret)
		})
	}
}

func TestCalculateTokenExpiryTime(t *testing.T) {
	tests := []struct {
		name           string
		secret         func() *v1.Secret
		expectedExpiry func() time.Duration
		delta          float64
	}{
		{
			name: "WithCreatedAnnotation",
			secret: func() *v1.Secret {
				tenMinutesAgo := time.Now().Add(-10 * time.Minute)
				return &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							AnnotationTokenCreated: tenMinutesAgo.Format(time.RFC3339),
						},
					},
				}
			},
			expectedExpiry: func() time.Duration {
				return BootstrapTokenTimeout - 10*time.Minute
			},
			delta: 5,
		},
		{
			name: "WithCreationTimestamp",
			secret: func() *v1.Secret {
				fiveMinutesAgo := time.Now().Add(-5 * time.Minute)
				return &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.Time{Time: fiveMinutesAgo},
					},
				}
			},
			expectedExpiry: func() time.Duration {
				return BootstrapTokenTimeout - 5*time.Minute
			},
			delta: 5,
		},
		{
			name: "MinimumRequeue",
			secret: func() *v1.Secret {
				almostExpired := time.Now().Add(-BootstrapTokenTimeout + 10*time.Second)
				return &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							AnnotationTokenCreated: almostExpired.Format(time.RFC3339),
						},
					},
				}
			},
			expectedExpiry: func() time.Duration {
				return 30 * time.Second
			},
			delta: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expiry := calculateTokenExpiryTime(tt.secret())
			expected := tt.expectedExpiry()

			if tt.delta > 0 {
				assert.InDelta(t, expected.Seconds(), expiry.Seconds(), tt.delta)
			} else {
				assert.Equal(t, expected, expiry)
			}
		})
	}
}

func TestCreateBootstrapToken(t *testing.T) {
	tests := []struct {
		name          string
		nodeClaimName string
		verify        func(*testing.T, *v1.Secret, error)
	}{
		{
			name:          "Success",
			nodeClaimName: "test-nodeclaim",
			verify: func(t *testing.T, secret *v1.Secret, err error) {
				require.NoError(t, err)
				assert.NotNil(t, secret)
				assert.Equal(t, v1.SecretType("bootstrap.kubernetes.io/token"), secret.Type)
				assert.Equal(t, TokenProviderName, secret.Labels[LabelTokenProvider])
				assert.Equal(t, "test-nodeclaim", secret.Annotations[AnnotationBootstrapToken])
				assert.Contains(t, secret.Name, "bootstrap-token-")
				assert.Equal(t, "kube-system", secret.Namespace)

				assert.Contains(t, secret.Data, "token-id")
				assert.Contains(t, secret.Data, "token-secret")
				assert.Contains(t, secret.Data, "description")
				assert.Contains(t, secret.Data, "expiration")
				assert.Equal(t, []byte("true"), secret.Data["usage-bootstrap-authentication"])
				assert.Equal(t, []byte("true"), secret.Data["usage-bootstrap-signing"])
				assert.Equal(t, []byte("system:bootstrappers:karpenter-exoscale"), secret.Data["auth-extra-groups"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := setup(t)
			secret, err := s.controller.CreateBootstrapToken(s.ctx, tt.nodeClaimName)
			tt.verify(t, secret, err)
		})
	}
}

func TestBuildBootstrapTokenSecretObject(t *testing.T) {
	tests := []struct {
		name          string
		tokenID       string
		tokenSecret   string
		nodeClaimName string
		verify        func(*testing.T, *v1.Secret)
	}{
		{
			name:          "CorrectStructure",
			tokenID:       "abc123",
			tokenSecret:   "def456",
			nodeClaimName: "test-nodeclaim",
			verify: func(t *testing.T, secret *v1.Secret) {
				assert.Equal(t, fmt.Sprintf("bootstrap-token-%s", "abc123"), secret.Name)
				assert.Equal(t, "kube-system", secret.Namespace)
				assert.Equal(t, v1.SecretType("bootstrap.kubernetes.io/token"), secret.Type)
				assert.Equal(t, TokenProviderName, secret.Labels[LabelTokenProvider])
				assert.Equal(t, "test-nodeclaim", secret.Annotations[AnnotationBootstrapToken])
				assert.NotEmpty(t, secret.Annotations[AnnotationTokenCreated])

				assert.Equal(t, []byte("abc123"), secret.Data["token-id"])
				assert.Equal(t, []byte("def456"), secret.Data["token-secret"])
				assert.Contains(t, string(secret.Data["description"]), "test-nodeclaim")
				assert.Equal(t, []byte("true"), secret.Data["usage-bootstrap-authentication"])
				assert.Equal(t, []byte("true"), secret.Data["usage-bootstrap-signing"])
				assert.Equal(t, []byte("system:bootstrappers:karpenter-exoscale"), secret.Data["auth-extra-groups"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret := buildBootstrapTokenSecretObject(tt.tokenID, tt.tokenSecret, tt.nodeClaimName)
			tt.verify(t, secret)
		})
	}
}

func TestNodeToTokens(t *testing.T) {
	tests := []struct {
		name            string
		setup           func(*env)
		object          client.Object
		expectedResults int
		verify          func(*testing.T, []reconcile.Request)
	}{
		{
			name: "MatchingNode",
			setup: func(s *env) {
				nodeClaimName := "test-nodeclaim"
				token := createBootstrapSecret("test-token", nodeClaimName)
				require.NoError(t, s.controller.Create(s.ctx, token))
			},
			object: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Annotations: map[string]string{
						"karpenter.sh/node-claim": "test-nodeclaim",
					},
				},
				Spec: v1.NodeSpec{
					ProviderID: "exoscale://test-instance",
				},
			},
			expectedResults: 1,
			verify: func(t *testing.T, requests []reconcile.Request) {
				require.Len(t, requests, 1)
				assert.Equal(t, "test-token", requests[0].Name)
				assert.Equal(t, "kube-system", requests[0].Namespace)
			},
		},
		{
			name: "NoMatchingTokens",
			setup: func(s *env) {
				token := createBootstrapSecret("test-token", "different-nodeclaim")
				require.NoError(t, s.controller.Create(s.ctx, token))
			},
			object: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Annotations: map[string]string{
						"karpenter.sh/node-claim": "test-nodeclaim",
					},
				},
				Spec: v1.NodeSpec{
					ProviderID: "exoscale://test-instance",
				},
			},
			expectedResults: 0,
		},
		{
			name:            "InvalidObjectType",
			object:          &v1.Secret{},
			expectedResults: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := setup(t)

			if tt.setup != nil {
				tt.setup(s)
			}

			requests := s.controller.nodeToTokens(s.ctx, tt.object)
			assert.Len(t, requests, tt.expectedResults)

			if tt.verify != nil {
				tt.verify(t, requests)
			}
		})
	}
}
