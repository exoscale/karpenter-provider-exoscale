package repair

import (
	"context"
	"testing"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/karpenter-exoscale/internal/testing/mocks"
	"github.com/exoscale/karpenter-exoscale/pkg/cloudprovider"
	"github.com/exoscale/karpenter-exoscale/pkg/constants"
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
	karpentercloudprovider "sigs.k8s.io/karpenter/pkg/cloudprovider"
)

type env struct {
	ctx        context.Context
	k8sClient  client.Client
	controller *NodeRepairController
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

	cloudProvider := cloudprovider.NewCloudProvider(
		k8sClient,
		exo,
		"https://test-cluster.example.com",
		nil,
		nil,
		nil,
		nil,
		nil,
		"ch-gva-2",
		"test-cluster",
		"10.96.0.10",
		"cluster.local",
	)

	controller := &NodeRepairController{
		Client:         k8sClient,
		Scheme:         s,
		CloudProvider:  cloudProvider,
		ExoscaleClient: exo,
		Recorder:       record.NewFakeRecorder(100),
	}

	return &env{
		ctx:        context.Background(),
		k8sClient:  k8sClient,
		controller: controller,
		exo:        exo,
	}
}

func createNode(name, providerID string, withKarpenterLabels bool, conditions []corev1.NodeCondition, taints []corev1.Taint) *corev1.Node {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: corev1.NodeSpec{
			ProviderID: providerID,
			Taints:     taints,
		},
		Status: corev1.NodeStatus{
			Conditions: conditions,
		},
	}

	if withKarpenterLabels {
		node.Labels = map[string]string{
			constants.LabelManagedBy: constants.ManagedByKarpenter,
		}
	}

	return node
}

func createNodeClaim(name, providerID, nodeName string, annotations map[string]string, withDeletionTimestamp bool) *karpenterv1.NodeClaim {
	nc := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
		Status: karpenterv1.NodeClaimStatus{
			ProviderID: providerID,
			NodeName:   nodeName,
		},
	}

	if withDeletionTimestamp {
		now := metav1.Now()
		nc.DeletionTimestamp = &now
	}

	return nc
}

func createNodeCondition(conditionType corev1.NodeConditionType, status corev1.ConditionStatus, lastTransitionTime time.Time) corev1.NodeCondition {
	return corev1.NodeCondition{
		Type:               conditionType,
		Status:             status,
		LastTransitionTime: metav1.Time{Time: lastTransitionTime},
	}
}

func TestReconcile(t *testing.T) {
	tests := []struct {
		name                 string
		node                 *corev1.Node
		nodeClaim            *karpenterv1.NodeClaim
		instanceExists       bool
		rebootSuccess        bool
		expectedRequeue      bool
		expectedRepairAction bool
		expectedTaint        bool
	}{
		{
			name: "node not found",
			node: nil,
		},
		{
			name: "unmanaged node",
			node: createNode("test-node", "exoscale://550e8400-e29b-41d4-a716-446655440000", false, nil, nil),
		},
		{
			name:            "managed node with no repair needed",
			node:            createNode("test-node", "exoscale://550e8400-e29b-41d4-a716-446655440001", true, []corev1.NodeCondition{createNodeCondition(corev1.NodeReady, corev1.ConditionTrue, time.Now())}, nil),
			nodeClaim:       createNodeClaim("test-claim", "exoscale://550e8400-e29b-41d4-a716-446655440001", "test-node", nil, false),
			expectedRequeue: true,
		},
		{
			name: "node needs repair - first attempt",
			node: createNode("test-node", "exoscale://550e8400-e29b-41d4-a716-446655440002", true, []corev1.NodeCondition{
				createNodeCondition(corev1.NodeReady, corev1.ConditionFalse, time.Now().Add(-20*time.Minute)), // Beyond the 15 minute tolerance
			}, nil),
			nodeClaim:            createNodeClaim("test-claim", "exoscale://550e8400-e29b-41d4-a716-446655440002", "test-node", nil, false),
			instanceExists:       true,
			rebootSuccess:        true,
			expectedRequeue:      true,
			expectedRepairAction: true,
		},
		{
			name: "node needs repair - max attempts reached",
			node: createNode("test-node", "exoscale://550e8400-e29b-41d4-a716-446655440003", true, []corev1.NodeCondition{
				createNodeCondition(corev1.NodeReady, corev1.ConditionFalse, time.Now().Add(-20*time.Minute)),
			}, nil),
			nodeClaim: createNodeClaim("test-claim", "exoscale://550e8400-e29b-41d4-a716-446655440003", "test-node", map[string]string{
				AnnotationRepairAttempts: "3",
			}, false),
			expectedRequeue: false,
			expectedTaint:   true,
		},
		{
			name: "node needs repair - cooldown period active",
			node: createNode("test-node", "exoscale://550e8400-e29b-41d4-a716-446655440004", true, []corev1.NodeCondition{
				createNodeCondition(corev1.NodeReady, corev1.ConditionFalse, time.Now().Add(-20*time.Minute)),
			}, nil),
			nodeClaim: createNodeClaim("test-claim", "exoscale://550e8400-e29b-41d4-a716-446655440004", "test-node", map[string]string{
				AnnotationRepairAttempts: "1",
				AnnotationLastRepairTime: time.Now().Add(-5 * time.Minute).Format(time.RFC3339),
			}, false),
			expectedRequeue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setup(t)

			if tt.node != nil {
				require.NoError(t, env.k8sClient.Create(env.ctx, tt.node))
			}

			if tt.nodeClaim != nil {
				require.NoError(t, env.k8sClient.Create(env.ctx, tt.nodeClaim))
			}

			if tt.expectedRepairAction && tt.nodeClaim != nil {
				instanceID, _ := utils.ParseProviderID(tt.nodeClaim.Status.ProviderID)
				if tt.rebootSuccess {
					env.exo.On("RebootInstance", mock.Anything, egov3.UUID(instanceID)).Return(&egov3.Operation{ID: "op-123"}, nil)
					env.exo.On("GetInstance", mock.Anything, egov3.UUID(instanceID)).Return(&egov3.Instance{
						State: egov3.InstanceStateRunning,
					}, nil)
				} else {
					env.exo.On("RebootInstance", mock.Anything, egov3.UUID(instanceID)).Return(nil, assert.AnError)
				}
			}

			var req reconcile.Request
			if tt.node != nil {
				req = reconcile.Request{NamespacedName: client.ObjectKeyFromObject(tt.node)}
			} else {
				req = reconcile.Request{NamespacedName: client.ObjectKey{Name: "non-existent"}}
			}

			result, err := env.controller.Reconcile(env.ctx, req)

			assert.NoError(t, err)

			if tt.expectedRequeue {
				assert.True(t, result.RequeueAfter > 0)
			} else if !tt.expectedRequeue && tt.node != nil {
				assert.Equal(t, reconcile.Result{}, result)
			}

			if tt.expectedTaint && tt.node != nil {
				var updatedNode corev1.Node
				err := env.k8sClient.Get(env.ctx, client.ObjectKeyFromObject(tt.node), &updatedNode)
				require.NoError(t, err)

				found := false
				for _, taint := range updatedNode.Spec.Taints {
					if taint.Key == "node.kubernetes.io/unschedulable" {
						found = true
						break
					}
				}
				assert.True(t, found, "Expected unschedulable taint to be added")
			}
		})
	}
}

func TestIsManagedNode(t *testing.T) {
	tests := []struct {
		name     string
		node     *corev1.Node
		expected bool
	}{
		{
			name: "node with karpenter nodepool label",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"karpenter.sh/nodepool": "test-nodepool",
					},
				},
			},
			expected: true,
		},
		{
			name: "node with exoscale managed-by label",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						constants.LabelManagedBy: constants.ManagedByKarpenter,
					},
				},
			},
			expected: true,
		},
		{
			name: "unmanaged node",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"some-other-label": "value",
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setup(t)
			result := env.controller.isManagedNode(tt.node)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEvaluateRepairPolicies(t *testing.T) {
	tests := []struct {
		name           string
		conditions     []corev1.NodeCondition
		policies       []karpentercloudprovider.RepairPolicy
		expectedRepair bool
		expectedReason string
	}{
		{
			name: "no policies defined",
			conditions: []corev1.NodeCondition{
				createNodeCondition(corev1.NodeReady, corev1.ConditionFalse, time.Now()),
			},
			policies:       []karpentercloudprovider.RepairPolicy{},
			expectedRepair: false,
		},
		{
			name: "condition within tolerance",
			conditions: []corev1.NodeCondition{
				createNodeCondition(corev1.NodeReady, corev1.ConditionFalse, time.Now().Add(-2*time.Minute)),
			},
			policies: []karpentercloudprovider.RepairPolicy{
				{
					ConditionType:      corev1.NodeReady,
					ConditionStatus:    corev1.ConditionFalse,
					TolerationDuration: 5 * time.Minute,
				},
			},
			expectedRepair: false,
		},
		{
			name: "condition exceeds tolerance",
			conditions: []corev1.NodeCondition{
				createNodeCondition(corev1.NodeReady, corev1.ConditionFalse, time.Now().Add(-10*time.Minute)),
			},
			policies: []karpentercloudprovider.RepairPolicy{
				{
					ConditionType:      corev1.NodeReady,
					ConditionStatus:    corev1.ConditionFalse,
					TolerationDuration: 5 * time.Minute,
				},
			},
			expectedRepair: true,
			expectedReason: "condition Ready is False for",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setup(t)
			node := &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: tt.conditions,
				},
			}

			needsRepair, reason := env.controller.evaluateRepairPolicies(node, tt.policies)
			assert.Equal(t, tt.expectedRepair, needsRepair)

			if tt.expectedReason != "" {
				assert.Contains(t, reason, tt.expectedReason)
			}
		})
	}
}

func TestShouldAttemptRepair(t *testing.T) {
	tests := []struct {
		name           string
		annotations    map[string]string
		expectedRepair bool
		expectedWait   time.Duration
	}{
		{
			name:           "no repair attempts yet",
			annotations:    nil,
			expectedRepair: true,
			expectedWait:   0,
		},
		{
			name: "first repair attempt",
			annotations: map[string]string{
				AnnotationRepairAttempts: "1",
				AnnotationLastRepairTime: time.Now().Add(-15 * time.Minute).Format(time.RFC3339),
			},
			expectedRepair: true,
			expectedWait:   0,
		},
		{
			name: "max attempts reached",
			annotations: map[string]string{
				AnnotationRepairAttempts: "3",
			},
			expectedRepair: false,
			expectedWait:   0,
		},
		{
			name: "cooldown period active",
			annotations: map[string]string{
				AnnotationRepairAttempts: "1",
				AnnotationLastRepairTime: time.Now().Add(-5 * time.Minute).Format(time.RFC3339),
			},
			expectedRepair: false,
			expectedWait:   5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setup(t)
			nodeClaim := &karpenterv1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tt.annotations,
				},
			}

			shouldRepair, waitTime := env.controller.shouldAttemptRepair(nodeClaim)
			assert.Equal(t, tt.expectedRepair, shouldRepair)

			if tt.expectedWait > 0 {
				assert.True(t, waitTime > 0)
				assert.True(t, waitTime <= tt.expectedWait)
			} else {
				assert.Equal(t, time.Duration(0), waitTime)
			}
		})
	}
}

func TestNodeClaimToNode(t *testing.T) {
	tests := []struct {
		name           string
		nodeClaim      *karpenterv1.NodeClaim
		expectedResult int
	}{
		{
			name: "nodeclaim with node name",
			nodeClaim: &karpenterv1.NodeClaim{
				Status: karpenterv1.NodeClaimStatus{
					NodeName: "test-node",
				},
			},
			expectedResult: 1,
		},
		{
			name: "nodeclaim without node name",
			nodeClaim: &karpenterv1.NodeClaim{
				Status: karpenterv1.NodeClaimStatus{},
			},
			expectedResult: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := setup(t)
			requests := env.controller.nodeClaimToNode(env.ctx, tt.nodeClaim)
			assert.Len(t, requests, tt.expectedResult)
		})
	}
}
