package nodeclaim

import (
	"context"
	"testing"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/karpenter-exoscale/internal/testing/mocks"
	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-exoscale/pkg/errors"
	"github.com/exoscale/karpenter-exoscale/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/events"
)

type env struct {
	ctx        context.Context
	k8sClient  client.Client
	controller *NodeClaimReconciler
	exo        *mocks.MockExoscaleClient
}

func setup(t *testing.T, objects ...client.Object) *env {
	t.Helper()

	s := runtime.NewScheme()
	require.NoError(t, scheme.AddToScheme(s))

	gv := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	s.AddKnownTypes(gv, &karpenterv1.NodeClaim{}, &karpenterv1.NodeClaimList{})

	builder := fake.NewClientBuilder().WithScheme(s)
	if len(objects) > 0 {
		builder = builder.WithObjects(objects...)
	}
	k8sClient := builder.Build()
	exo := &mocks.MockExoscaleClient{}

	controller := &NodeClaimReconciler{
		Client:         k8sClient,
		Scheme:         s,
		ExoscaleClient: exo,
		Recorder:       events.NewRecorder(record.NewFakeRecorder(100)),
		Zone:           "ch-gva-2",
		ClusterName:    "test-cluster",
	}

	return &env{
		ctx:        context.Background(),
		k8sClient:  k8sClient,
		controller: controller,
		exo:        exo,
	}
}

func createNodeClaim(name, providerID string, withFinalizer bool, withDeletionTimestamp bool, withNodeName string) *karpenterv1.NodeClaim {
	nc := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Status: karpenterv1.NodeClaimStatus{
			ProviderID: providerID,
			NodeName:   withNodeName,
		},
	}

	if withFinalizer {
		nc.Finalizers = []string{Finalizer}
	}

	if withDeletionTimestamp {
		now := metav1.Now()
		nc.DeletionTimestamp = &now
	}

	return nc
}

func createNode(name, providerID string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: corev1.NodeSpec{
			ProviderID: providerID,
		},
	}
}

func TestReconcile(t *testing.T) {
	tests := []struct {
		name                string
		nodeClaim           *karpenterv1.NodeClaim
		node                *corev1.Node
		instanceExists      bool
		instanceLabelsMatch bool
		expectedRequeue     bool
		expectedFinalizer   bool
	}{
		{
			name:              "new nodeclaim gets finalizer",
			nodeClaim:         createNodeClaim("test-claim", "", false, false, ""),
			expectedRequeue:   true,
			expectedFinalizer: true,
		},
		{
			name:                "nodeclaim with provider id ensures tags",
			nodeClaim:           createNodeClaim("test-claim", "exoscale://550e8400-e29b-41d4-a716-446655440000", true, false, ""),
			instanceExists:      true,
			instanceLabelsMatch: true,
			expectedRequeue:     true,
		},
		{
			name:                "nodeclaim with provider id but instance not found",
			nodeClaim:           createNodeClaim("test-claim", "exoscale://550e8400-e29b-41d4-a716-446655440001", true, false, ""),
			instanceExists:      false,
			instanceLabelsMatch: false,
			expectedRequeue:     false,
		},
		{
			name:      "nodeclaim not found",
			nodeClaim: nil,
		},
		{
			name:            "deletion with node",
			nodeClaim:       createNodeClaim("test-claim", "exoscale://550e8400-e29b-41d4-a716-446655440002", true, true, "test-node"),
			node:            createNode("test-node", "exoscale://550e8400-e29b-41d4-a716-446655440002"),
			instanceExists:  true,
			expectedRequeue: true,
		},
		{
			name:            "deletion without node",
			nodeClaim:       createNodeClaim("test-claim", "exoscale://550e8400-e29b-41d4-a716-446655440003", true, true, ""),
			instanceExists:  true,
			expectedRequeue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setup(t)

			if tt.nodeClaim != nil {
				require.NoError(t, env.k8sClient.Create(env.ctx, tt.nodeClaim))
			}

			if tt.node != nil {
				require.NoError(t, env.k8sClient.Create(env.ctx, tt.node))
			}

			if tt.nodeClaim != nil && tt.nodeClaim.Status.ProviderID != "" {
				instanceID, _ := utils.ParseProviderID(tt.nodeClaim.Status.ProviderID)

				if tt.instanceExists {
					instance := &egov3.Instance{
						ID:     egov3.UUID(instanceID),
						Labels: map[string]string{},
					}

					if tt.instanceLabelsMatch {
						instance.Labels = map[string]string{
							constants.LabelManagedBy:   constants.ManagedByKarpenter,
							constants.LabelClusterName: "test-cluster",
							constants.LabelNodeClaim:   tt.nodeClaim.Name,
						}
					}

					env.exo.On("GetInstance", mock.Anything, egov3.UUID(instanceID)).Return(instance, nil)
				} else {
					env.exo.On("GetInstance", mock.Anything, egov3.UUID(instanceID)).Return(nil, errors.NewInstanceNotFoundError(instanceID))
				}
			}

			var req reconcile.Request
			if tt.nodeClaim != nil {
				req = reconcile.Request{NamespacedName: client.ObjectKeyFromObject(tt.nodeClaim)}
			} else {
				req = reconcile.Request{NamespacedName: client.ObjectKey{Name: "non-existent"}}
			}

			result, err := env.controller.Reconcile(env.ctx, req)

			assert.NoError(t, err)

			if tt.expectedRequeue {
				assert.True(t, result.RequeueAfter > 0)
			} else if !tt.expectedRequeue && tt.nodeClaim != nil {
				assert.Equal(t, reconcile.Result{}, result)
			}

			if tt.nodeClaim != nil && tt.expectedFinalizer {
				var updatedNC karpenterv1.NodeClaim
				err := env.k8sClient.Get(env.ctx, client.ObjectKeyFromObject(tt.nodeClaim), &updatedNC)
				require.NoError(t, err)
				assert.Contains(t, updatedNC.Finalizers, Finalizer)
			}
		})
	}
}

func TestNodeToNodeClaim(t *testing.T) {
	tests := []struct {
		name           string
		node           *corev1.Node
		expectedResult int
	}{
		{
			name: "node with karpenter annotation",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Annotations: map[string]string{
						"karpenter.sh/node-claim": "test-claim",
					},
				},
			},
			expectedResult: 1,
		},
		{
			name: "node with no matches",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
				Spec: corev1.NodeSpec{
					ProviderID: "exoscale://inst-123",
				},
			},
			expectedResult: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setup(t)

			requests := env.controller.nodeToNodeClaim(tt.node)
			assert.Len(t, requests, tt.expectedResult)
		})
	}
}
