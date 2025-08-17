package nodeclass

import (
	"context"
	"errors"
	"testing"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/internal/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

type env struct {
	ctx        context.Context
	k8sClient  client.Client
	controller *ExoscaleNodeClassReconciler
	exo        *mocks.MockExoscaleClient
}

func setup(t *testing.T, objects ...client.Object) *env {
	t.Helper()

	s := runtime.NewScheme()
	require.NoError(t, scheme.AddToScheme(s))
	require.NoError(t, apiv1.AddToScheme(s))

	gv := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	s.AddKnownTypes(gv, &karpenterv1.NodeClaim{}, &karpenterv1.NodeClaimList{})

	builder := fake.NewClientBuilder().WithScheme(s)
	if len(objects) > 0 {
		builder = builder.WithObjects(objects...)
	}
	k8sClient := builder.Build()
	exo := &mocks.MockExoscaleClient{}

	controller := &ExoscaleNodeClassReconciler{
		Client:         k8sClient,
		Scheme:         s,
		ExoscaleClient: exo,
		Recorder:       record.NewFakeRecorder(100),
		Zone:           "ch-gva-2",
	}

	return &env{
		ctx:        context.Background(),
		k8sClient:  k8sClient,
		controller: controller,
		exo:        exo,
	}
}

func createNodeClass(name string, withFinalizer bool, withDeletionTimestamp bool) *apiv1.ExoscaleNodeClass {
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiv1.ExoscaleNodeClassSpec{
			TemplateID:         string(mocks.DefaultTemplateID),
			DiskSize:           50,
			SecurityGroups:     []string{string(mocks.DefaultSecurityGroupID)},
			PrivateNetworks:    []string{string(mocks.PrivateNetworkID1)},
			AntiAffinityGroups: []string{string(mocks.DefaultAntiAffinityGroupID)},
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

func createNodeClaim(name, nodeClassRef string) *karpenterv1.NodeClaim {
	return &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: karpenterv1.NodeClaimSpec{
			NodeClassRef: &karpenterv1.NodeClassReference{
				Group: "karpenter.exoscale.com",
				Kind:  "ExoscaleNodeClass",
				Name:  nodeClassRef,
			},
		},
	}
}

func TestReconcile(t *testing.T) {
	tests := []struct {
		name                 string
		nodeClass            *apiv1.ExoscaleNodeClass
		nodeClaims           []*karpenterv1.NodeClaim
		templateExists       bool
		securityGroupExists  bool
		privateNetworkExists bool
		antiAffinityExists   bool
		expectedRequeue      bool
		expectedFinalizer    bool
		expectedReady        bool
	}{
		{
			name:                 "new nodeclass gets finalizer",
			nodeClass:            createNodeClass("test-nc", false, false),
			templateExists:       true,
			securityGroupExists:  true,
			privateNetworkExists: true,
			antiAffinityExists:   true,
			expectedRequeue:      true,
			expectedFinalizer:    true,
		},
		{
			name:                 "successful validation and verification",
			nodeClass:            createNodeClass("test-nc", false, false),
			templateExists:       true,
			securityGroupExists:  true,
			privateNetworkExists: true,
			antiAffinityExists:   true,
			expectedRequeue:      true,
			expectedFinalizer:    true,
		},
		{
			name:              "template not found",
			nodeClass:         createNodeClass("test-nc", false, false),
			templateExists:    false,
			expectedRequeue:   true,
			expectedFinalizer: true,
		},
		{
			name:      "nodeclass not found",
			nodeClass: nil,
		},
		{
			name:            "deletion with active nodeclaims",
			nodeClass:       createNodeClass("test-nc", true, true),
			nodeClaims:      []*karpenterv1.NodeClaim{createNodeClaim("claim-1", "test-nc")},
			expectedRequeue: true,
		},
		{
			name:                 "deletion without active nodeclaims",
			nodeClass:            createNodeClass("test-nc", true, true),
			templateExists:       true,
			securityGroupExists:  true,
			privateNetworkExists: true,
			antiAffinityExists:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []client.Object
			if tt.nodeClass != nil {
				objects = append(objects, tt.nodeClass)
			}
			for _, nc := range tt.nodeClaims {
				objects = append(objects, nc)
			}

			env := setup(t, objects...)

			if tt.nodeClass != nil && tt.nodeClass.DeletionTimestamp != nil {
				env.exo.On("ListInstances", mock.Anything, mock.MatchedBy(func(opts []egov3.ListInstancesOpt) bool {
					return len(opts) == 0 || opts == nil
				})).Return(&egov3.ListInstancesResponse{
					Instances: []egov3.ListInstancesResponseInstances{},
				}, nil).Maybe()
			}

			if tt.nodeClass != nil && len(tt.nodeClass.Finalizers) > 0 {
				if tt.templateExists {
					env.exo.On("GetTemplate", mock.Anything, mocks.DefaultTemplateID).Return(&egov3.Template{}, nil)
				} else {
					env.exo.On("GetTemplate", mock.Anything, mocks.DefaultTemplateID).Return(nil, errors.New("template not found"))
				}

				if tt.securityGroupExists {
					env.exo.On("GetSecurityGroup", mock.Anything, mocks.DefaultSecurityGroupID).Return(&egov3.SecurityGroup{}, nil)
				} else {
					env.exo.On("GetSecurityGroup", mock.Anything, mocks.DefaultSecurityGroupID).Return(nil, errors.New("security group not found"))
				}

				if tt.privateNetworkExists {
					env.exo.On("GetPrivateNetwork", mock.Anything, mocks.PrivateNetworkID1).Return(&egov3.PrivateNetwork{}, nil)
				} else {
					env.exo.On("GetPrivateNetwork", mock.Anything, mocks.PrivateNetworkID1).Return(nil, errors.New("private network not found"))
				}

				if tt.antiAffinityExists {
					env.exo.On("GetAntiAffinityGroup", mock.Anything, mocks.DefaultAntiAffinityGroupID).Return(&egov3.AntiAffinityGroup{Instances: []egov3.Instance{}}, nil)
				} else {
					env.exo.On("GetAntiAffinityGroup", mock.Anything, mocks.DefaultAntiAffinityGroupID).Return(nil, errors.New("anti-affinity group not found"))
				}
			}

			var req reconcile.Request
			if tt.nodeClass != nil {
				req = reconcile.Request{NamespacedName: client.ObjectKey{Name: tt.nodeClass.Name}}
			} else {
				req = reconcile.Request{NamespacedName: client.ObjectKey{Name: "non-existent"}}
			}

			result, err := env.controller.Reconcile(env.ctx, req)

			assert.NoError(t, err)

			if tt.expectedRequeue {
				assert.True(t, result.RequeueAfter > 0)
			} else if !tt.expectedRequeue && tt.nodeClass != nil {
				assert.Equal(t, reconcile.Result{}, result)
			}

			if tt.nodeClass != nil {
				var updatedNC apiv1.ExoscaleNodeClass
				err := env.k8sClient.Get(env.ctx, client.ObjectKey{Name: tt.nodeClass.Name}, &updatedNC)
				if tt.expectedFinalizer {
					require.NoError(t, err)
					assert.Contains(t, updatedNC.Finalizers, Finalizer)
				}

				if tt.expectedReady && err == nil {
					condition := updatedNC.StatusConditions().Get(ConditionTypeReady)
					assert.NotNil(t, condition)
					assert.Equal(t, "True", string(condition.Status))
				}
			}
		})
	}
}

func TestIsNodeClaimUsingNodeClass(t *testing.T) {
	tests := []struct {
		name          string
		nodeClaim     *karpenterv1.NodeClaim
		nodeClassName string
		expected      bool
	}{
		{
			name:          "matching nodeclass",
			nodeClaim:     createNodeClaim("test-claim", "test-nc"),
			nodeClassName: "test-nc",
			expected:      true,
		},
		{
			name:          "different nodeclass",
			nodeClaim:     createNodeClaim("test-claim", "different-nc"),
			nodeClassName: "test-nc",
			expected:      false,
		},
		{
			name: "nil nodeclass ref",
			nodeClaim: &karpenterv1.NodeClaim{
				Spec: karpenterv1.NodeClaimSpec{},
			},
			nodeClassName: "test-nc",
			expected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNodeClaimUsingNodeClass(tt.nodeClaim, tt.nodeClassName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCountActiveNodeClaims(t *testing.T) {
	tests := []struct {
		name          string
		nodeClaims    []karpenterv1.NodeClaim
		nodeClassName string
		expected      int
	}{
		{
			name: "multiple active claims",
			nodeClaims: []karpenterv1.NodeClaim{
				*createNodeClaim("claim-1", "test-nc"),
				*createNodeClaim("claim-2", "test-nc"),
				*createNodeClaim("claim-3", "different-nc"),
			},
			nodeClassName: "test-nc",
			expected:      2,
		},
		{
			name: "claims with deletion timestamp",
			nodeClaims: []karpenterv1.NodeClaim{
				func() karpenterv1.NodeClaim {
					nc := *createNodeClaim("claim-1", "test-nc")
					now := metav1.Now()
					nc.DeletionTimestamp = &now
					return nc
				}(),
				*createNodeClaim("claim-2", "test-nc"),
			},
			nodeClassName: "test-nc",
			expected:      1,
		},
		{
			name:          "no matching claims",
			nodeClaims:    []karpenterv1.NodeClaim{*createNodeClaim("claim-1", "different-nc")},
			nodeClassName: "test-nc",
			expected:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countActiveNodeClaims(tt.nodeClaims, tt.nodeClassName)
			assert.Equal(t, tt.expected, result)
		})
	}
}
