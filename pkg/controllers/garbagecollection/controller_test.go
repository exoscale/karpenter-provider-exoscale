package garbagecollection

import (
	"context"
	"errors"
	"testing"
	"time"

	internaltesting "github.com/exoscale/karpenter-exoscale/internal/testing"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instance"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/events"
)

type env struct {
	ctx              context.Context
	controller       *Controller
	k8sClient        client.Client
	instanceProvider *internaltesting.MockInstanceProvider
}

func setup(t *testing.T, objects ...client.Object) *env {
	t.Helper()

	scheme := runtime.NewScheme()

	gv := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	scheme.AddKnownTypes(gv, &karpenterv1.NodeClaim{}, &karpenterv1.NodeClaimList{})

	builder := fake.NewClientBuilder().WithScheme(scheme)
	if len(objects) > 0 {
		builder = builder.WithObjects(objects...)
	}
	k8sClient := builder.Build()

	mockProvider := &internaltesting.MockInstanceProvider{}

	controller := &Controller{
		client:           k8sClient,
		instanceProvider: mockProvider,
		events:           events.NewRecorder(record.NewFakeRecorder(100)),
	}

	return &env{
		ctx:              context.Background(),
		controller:       controller,
		k8sClient:        k8sClient,
		instanceProvider: mockProvider,
	}
}

func createNodeClaim(name, providerID string) *karpenterv1.NodeClaim {
	return &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Status: karpenterv1.NodeClaimStatus{
			ProviderID: providerID,
		},
	}
}

func createInstance(id, name string, createdAt time.Time) *instance.Instance {
	return &instance.Instance{
		ID:        id,
		Name:      name,
		CreatedAt: createdAt,
	}
}

func TestReconcile(t *testing.T) {
	tests := []struct {
		name              string
		instances         []*instance.Instance
		nodeClaims        []struct{ name, providerID string }
		listInstanceError error
		listClaimError    error
		deleteErrors      map[string]error
		expectedDeletes   []string
		expectedError     string
	}{
		{
			name: "no orphaned instances",
			instances: []*instance.Instance{
				createInstance("11111111-1111-1111-1111-111111111111", "node-1", time.Now().Add(-5*time.Minute)),
				createInstance("22222222-2222-2222-2222-222222222222", "node-2", time.Now().Add(-10*time.Minute)),
			},
			nodeClaims: []struct{ name, providerID string }{
				{"claim-1", "exoscale://11111111-1111-1111-1111-111111111111"},
				{"claim-2", "exoscale://22222222-2222-2222-2222-222222222222"},
			},
		},
		{
			name: "delete orphaned instance",
			instances: []*instance.Instance{
				createInstance("11111111-1111-1111-1111-111111111111", "node-1", time.Now().Add(-5*time.Minute)),
				createInstance("22222222-2222-2222-2222-222222222222", "node-2", time.Now().Add(-20*time.Minute)),
			},
			nodeClaims: []struct{ name, providerID string }{
				{"claim-1", "exoscale://11111111-1111-1111-1111-111111111111"},
			},
			expectedDeletes: []string{"22222222-2222-2222-2222-222222222222"},
		},
		{
			name: "multiple orphaned instances",
			instances: []*instance.Instance{
				createInstance("11111111-1111-1111-1111-111111111111", "node-1", time.Now().Add(-30*time.Minute)),
				createInstance("22222222-2222-2222-2222-222222222222", "node-2", time.Now().Add(-25*time.Minute)),
				createInstance("33333333-3333-3333-3333-333333333333", "node-3", time.Now().Add(-20*time.Minute)),
				createInstance("44444444-4444-4444-4444-444444444444", "node-4", time.Now().Add(-5*time.Minute)),
			},
			expectedDeletes: []string{"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "33333333-3333-3333-3333-333333333333", "44444444-4444-4444-4444-444444444444"},
		},
		{
			name:              "list instances error",
			listInstanceError: errors.New("API error"),
			expectedError:     "failed to list cloud instances",
		},
		{
			name: "list nodeclaims error",
			instances: []*instance.Instance{
				createInstance("66666666-6666-6666-6666-666666666666", "node-1", time.Now()),
			},
			listClaimError: errors.New("API error"),
			expectedError:  "failed to list NodeClaims",
		},
		{
			name: "delete fails with regular error",
			instances: []*instance.Instance{
				createInstance("55555555-5555-5555-5555-555555555555", "node-1", time.Now().Add(-30*time.Minute)),
			},
			deleteErrors: map[string]error{
				"55555555-5555-5555-5555-555555555555": errors.New("delete failed"),
			},
			expectedDeletes: []string{"55555555-5555-5555-5555-555555555555"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []client.Object
			for _, nc := range tt.nodeClaims {
				objects = append(objects, createNodeClaim(nc.name, nc.providerID))
			}

			env := setup(t, objects...)

			if tt.listInstanceError != nil {
				env.instanceProvider.On("List", mock.Anything).Return(nil, tt.listInstanceError)
			} else {
				env.instanceProvider.On("List", mock.Anything).Return(tt.instances, nil)
			}

			if tt.listClaimError != nil {
				origClient := env.k8sClient
				env.controller.client = &errorClient{
					Client:    origClient,
					listError: tt.listClaimError,
				}
			}

			for _, instanceID := range tt.expectedDeletes {
				if deleteErr, exists := tt.deleteErrors[instanceID]; exists {
					env.instanceProvider.On("Delete", mock.Anything, instanceID).Return(deleteErr)
				} else {
					env.instanceProvider.On("Delete", mock.Anything, instanceID).Return(nil)
				}
			}

			result, err := env.controller.Reconcile(env.ctx, reconcile.Request{})

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, reconcile.Result{}, result)
				env.instanceProvider.AssertExpectations(t)
			}
		})
	}
}

// errorClient wraps a client to inject errors for testing
type errorClient struct {
	client.Client
	listError error
}

func (e *errorClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if e.listError != nil {
		return e.listError
	}
	return e.Client.List(ctx, list, opts...)
}

func TestNewController(t *testing.T) {
	fakeRecorder := record.NewFakeRecorder(100)
	controller := &Controller{
		client:           nil,
		instanceProvider: &internaltesting.MockInstanceProvider{},
		events:           events.NewRecorder(fakeRecorder),
	}

	assert.NotNil(t, controller.instanceProvider)
	assert.NotNil(t, controller.events)
}
