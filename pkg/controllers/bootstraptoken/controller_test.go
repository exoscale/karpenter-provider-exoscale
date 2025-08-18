package bootstraptoken

import (
	"context"
	"testing"
	"time"

	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-exoscale/pkg/utils"
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
	controller *Controller
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

	controller := &Controller{
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
			Namespace: metav1.NamespaceSystem,
			Labels: map[string]string{
				constants.LabelTokenProvider: constants.ProviderName,
			},
			Annotations: map[string]string{
				constants.AnnotationBootstrapToken: nodeClaimName,
				constants.AnnotationTokenCreated:   time.Now().Format(time.RFC3339),
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
	expiredTime := time.Now().Add(-constants.DefaultBootstrapTokenTTL - time.Hour)
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceSystem,
			Labels: map[string]string{
				constants.LabelTokenProvider: constants.ProviderName,
			},
			Annotations: map[string]string{
				constants.AnnotationBootstrapToken: nodeClaimName,
				constants.AnnotationTokenCreated:   expiredTime.Format(time.RFC3339),
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
			Namespace: metav1.NamespaceSystem,
			Labels: map[string]string{
				constants.LabelTokenProvider: constants.ProviderName,
			},
			Annotations: map[string]string{
				constants.AnnotationBootstrapToken: nodeClaimName,
				constants.AnnotationTokenCreated:   time.Now().Format(time.RFC3339),
				AnnotationNodeRegistered:           "true",
			},
		},
		Type: "bootstrap.kubernetes.io/token",
		Data: map[string][]byte{
			"token-id":     []byte("abc123"),
			"token-secret": []byte("def456"),
		},
	}
}

func createNode(name, providerID, bootstrapToken string) *v1.Node {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.NodeSpec{
			ProviderID: providerID,
		},
	}

	if bootstrapToken != "" {
		node.Annotations = map[string]string{
			"exoscale.com/bootstrap-token": bootstrapToken,
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
						Namespace: metav1.NamespaceSystem,
					},
					Type: "Opaque",
				}
				require.NoError(t, s.controller.Create(s.ctx, regularSecret))
			},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "regular-secret",
					Namespace: metav1.NamespaceSystem,
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
					Namespace: metav1.NamespaceSystem,
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
					Namespace: metav1.NamespaceSystem,
				},
			},
			expectedResult: reconcile.Result{},
			verify: func(t *testing.T, s *env) {
				var deletedSecret v1.Secret
				err := s.controller.Get(s.ctx, types.NamespacedName{Name: "expired-token", Namespace: metav1.NamespaceSystem}, &deletedSecret)
				assert.True(t, errors.IsNotFound(err))
			},
		},
		{
			name: "CleansUpTokenForRegisteredNode",
			setup: func(s *env) {
				nodeClaimName := "standard-abc123"
				token := createBootstrapSecret("bootstrap-token", nodeClaimName)
				require.NoError(t, s.controller.Create(s.ctx, token))

				node := createNode("test-standard-abc123", "exoscale://test-instance", "bootstrap-token")
				require.NoError(t, s.controller.Create(s.ctx, node))
			},
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "bootstrap-token",
					Namespace: metav1.NamespaceSystem,
				},
			},
			expectedResult: reconcile.Result{},
			verify: func(t *testing.T, s *env) {
				var deletedSecret v1.Secret
				err := s.controller.Get(s.ctx, types.NamespacedName{Name: "bootstrap-token", Namespace: metav1.NamespaceSystem}, &deletedSecret)
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
					Namespace: metav1.NamespaceSystem,
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
				err := s.controller.Get(s.ctx, types.NamespacedName{Name: "expired-token", Namespace: metav1.NamespaceSystem}, &deletedSecret)
				assert.True(t, errors.IsNotFound(err))

				err = s.controller.Get(s.ctx, types.NamespacedName{Name: "registered-token", Namespace: metav1.NamespaceSystem}, &deletedSecret)
				assert.True(t, errors.IsNotFound(err))

				var activeSecret v1.Secret
				err = s.controller.Get(s.ctx, types.NamespacedName{Name: "active-token", Namespace: metav1.NamespaceSystem}, &activeSecret)
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
				assert.True(t, result.RequeueAfter <= constants.DefaultBootstrapTokenTTL)
			} else if tt.name == "PerformCleanupRequest" {
				assert.Equal(t, constants.DefaultOperationTimeout, result.RequeueAfter)
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
						constants.LabelTokenProvider: constants.ProviderName,
					},
					Annotations: map[string]string{
						constants.AnnotationBootstrapToken: "test-nodeclaim",
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
						constants.LabelTokenProvider: constants.ProviderName,
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
						constants.LabelTokenProvider: "different-provider",
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
				expiredTime := time.Now().Add(-constants.DefaultBootstrapTokenTTL - time.Hour)
				return &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							constants.AnnotationTokenCreated: expiredTime.Format(time.RFC3339),
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
							constants.AnnotationTokenCreated: recentTime.Format(time.RFC3339),
						},
					},
				}
			},
			expected: false,
		},
		{
			name: "ExpiredByCreationTimestamp",
			secret: func() *v1.Secret {
				expiredTime := time.Now().Add(-constants.DefaultBootstrapTokenTTL - time.Hour)
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
							constants.AnnotationTokenCreated: "invalid-time-format",
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

func TestIsNodeUsingBootstrapToken(t *testing.T) {
	tests := []struct {
		name            string
		node            *v1.Node
		tokenSecretName string
		expected        bool
	}{
		{
			name: "MatchingToken",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Annotations: map[string]string{
						"exoscale.com/bootstrap-token": "bootstrap-token-abc123",
					},
				},
			},
			tokenSecretName: "bootstrap-token-abc123",
			expected:        true,
		},
		{
			name: "DifferentToken",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Annotations: map[string]string{
						"exoscale.com/bootstrap-token": "bootstrap-token-xyz789",
					},
				},
			},
			tokenSecretName: "bootstrap-token-abc123",
			expected:        false,
		},
		{
			name: "NoAnnotation",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
			},
			tokenSecretName: "bootstrap-token-abc123",
			expected:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNodeUsingBootstrapToken(tt.node, tt.tokenSecretName)
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
						constants.AnnotationBootstrapToken: "test-nodeclaim",
						AnnotationNodeRegistered:           "true",
					},
				},
			},
			expected: true,
		},
		{
			name: "NodeFound",
			setup: func(s *env) {
				node := createNode("test-standard-abc123", "exoscale://test-instance", "bootstrap-token-xyz")
				require.NoError(t, s.controller.Create(s.ctx, node))
			},
			secret: &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bootstrap-token-xyz",
					Namespace: metav1.NamespaceSystem,
					Annotations: map[string]string{
						constants.AnnotationBootstrapToken: "standard-abc123",
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
						constants.AnnotationBootstrapToken: "non-existent-nodeclaim",
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
					Namespace: metav1.NamespaceSystem,
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
				threeMinutesAgo := time.Now().Add(-3 * time.Minute)
				return &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							constants.AnnotationTokenCreated: threeMinutesAgo.Format(time.RFC3339),
						},
					},
				}
			},
			expectedExpiry: func() time.Duration {
				return constants.DefaultBootstrapTokenTTL - 3*time.Minute
			},
			delta: 5,
		},
		{
			name: "WithCreationTimestamp",
			secret: func() *v1.Secret {
				twoMinutesAgo := time.Now().Add(-2 * time.Minute)
				return &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						CreationTimestamp: metav1.Time{Time: twoMinutesAgo},
					},
				}
			},
			expectedExpiry: func() time.Duration {
				return constants.DefaultBootstrapTokenTTL - 2*time.Minute
			},
			delta: 5,
		},
		{
			name: "MinimumRequeue",
			secret: func() *v1.Secret {
				almostExpired := time.Now().Add(-constants.DefaultBootstrapTokenTTL + 10*time.Second)
				return &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							constants.AnnotationTokenCreated: almostExpired.Format(time.RFC3339),
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
		verify        func(*testing.T, *env, *utils.BootstrapToken, error)
	}{
		{
			name:          "Success",
			nodeClaimName: "test-nodeclaim",
			verify: func(t *testing.T, s *env, token *utils.BootstrapToken, err error) {
				require.NoError(t, err)
				assert.NotNil(t, token)
				assert.NotEmpty(t, token.TokenID)
				assert.NotEmpty(t, token.TokenSecret)
				assert.Contains(t, token.SecretName, "bootstrap-token-")
				assert.Len(t, token.TokenID, 6)
				assert.Len(t, token.TokenSecret, 16)

				// Verify the secret was actually created
				secret := &v1.Secret{}
				err = s.k8sClient.Get(s.ctx, client.ObjectKey{
					Name:      token.SecretName,
					Namespace: metav1.NamespaceSystem,
				}, secret)
				require.NoError(t, err)
				assert.Equal(t, v1.SecretType("bootstrap.kubernetes.io/token"), secret.Type)
				assert.Equal(t, constants.ProviderName, secret.Labels[constants.LabelTokenProvider])
				assert.Equal(t, "test-nodeclaim", secret.Annotations[constants.AnnotationBootstrapToken])
				assert.Equal(t, []byte(token.TokenID), secret.Data["token-id"])
				assert.Equal(t, []byte(token.TokenSecret), secret.Data["token-secret"])
				assert.Equal(t, []byte("true"), secret.Data["usage-bootstrap-authentication"])
				assert.Equal(t, []byte("true"), secret.Data["usage-bootstrap-signing"])
				assert.Equal(t, []byte(constants.BootstrapTokenExtraGroups), secret.Data["auth-extra-groups"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := setup(t)
			token, err := s.controller.CreateBootstrapToken(s.ctx, tt.nodeClaimName)
			tt.verify(t, s, token, err)
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
				assert.Equal(t, "bootstrap-token-abc123", secret.Name)
				assert.Equal(t, metav1.NamespaceSystem, secret.Namespace)
				assert.Equal(t, v1.SecretType("bootstrap.kubernetes.io/token"), secret.Type)
				assert.Equal(t, constants.ProviderName, secret.Labels[constants.LabelTokenProvider])
				assert.Equal(t, "test-nodeclaim", secret.Annotations[constants.AnnotationBootstrapToken])
				assert.NotEmpty(t, secret.Annotations[constants.AnnotationTokenCreated])

				assert.Equal(t, []byte("abc123"), secret.Data["token-id"])
				assert.Equal(t, []byte("def456"), secret.Data["token-secret"])
				assert.Contains(t, string(secret.Data["description"]), "test-nodeclaim")
				assert.Equal(t, []byte("true"), secret.Data["usage-bootstrap-authentication"])
				assert.Equal(t, []byte("true"), secret.Data["usage-bootstrap-signing"])
				assert.Equal(t, []byte(constants.BootstrapTokenExtraGroups), secret.Data["auth-extra-groups"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret := utils.BuildBootstrapTokenSecret(tt.tokenID, tt.tokenSecret, tt.nodeClaimName, constants.DefaultBootstrapTokenTTL, constants.BootstrapTokenExtraGroups)
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
			name: "NodeWithBootstrapToken",
			object: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Annotations: map[string]string{
						"exoscale.com/bootstrap-token": "bootstrap-token-abc123",
					},
				},
				Spec: v1.NodeSpec{
					ProviderID: "exoscale://test-instance",
				},
			},
			expectedResults: 1,
			verify: func(t *testing.T, requests []reconcile.Request) {
				require.Len(t, requests, 1)
				assert.Equal(t, "bootstrap-token-abc123", requests[0].Name)
				assert.Equal(t, metav1.NamespaceSystem, requests[0].Namespace)
			},
		},
		{
			name: "NodeWithoutBootstrapToken",
			object: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
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
