package cloudprovider_test

import (
	"errors"
	"testing"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	internaltesting "github.com/exoscale/karpenter-exoscale/internal/testing"
	"github.com/exoscale/karpenter-exoscale/internal/testing/mocks"
	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	kerrors "github.com/exoscale/karpenter-exoscale/pkg/errors"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instance"
	"github.com/exoscale/karpenter-exoscale/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	karpentercloudprovider "sigs.k8s.io/karpenter/pkg/cloudprovider"
)

var (
	testInstanceType = &egov3.InstanceType{
		ID:     mocks.StandardMediumTypeID,
		Family: "standard",
		Size:   "small",
		Cpus:   2,
		Memory: 4096,
	}

	testInstance = &instance.Instance{
		ID:               string(mocks.InstanceID1),
		InstanceType:     testInstanceType,
		InstanceTypeName: "standard.small",
		Labels:           map[string]string{"exoscale.com/node-claim": "test-node-claim"},
	}

	testInstances = []*instance.Instance{
		{
			ID:     "instance-1",
			Labels: map[string]string{"exoscale.com/node-claim": "claim-1"},
		},
		{
			ID:     "instance-2",
			Labels: map[string]string{"exoscale.com/node-claim": "claim-2"},
		},
	}

	testInstanceTypes = []*karpentercloudprovider.InstanceType{
		{
			Name: "standard.small",
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		},
		{
			Name: "standard.medium",
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
		},
	}
)

func TestCloudProvider_Create(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)

		nodeClaim := mocks.CreateNodeClaim("test-claim", "test-nodeclass", "small-instance")
		nodeClaim.Status.ProviderID = ""

		nodeClass := mocks.CreateNodeClass("test-nodeclass", "standard")
		require.NoError(t, env.KubeClient.Create(env.Ctx, nodeClass))

		env.MockUserData.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return("base64-encoded-userdata", nil)
		env.MockInstanceProvider.On("Create", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(testInstance, nil)

		result, err := env.CloudProvider.Create(env.Ctx, nodeClaim)

		require.NoError(t, err)
		assert.Equal(t, utils.FormatProviderID(string(mocks.InstanceID1)), result.Status.ProviderID)
		assert.Equal(t, "standard.small", result.Labels[corev1.LabelInstanceTypeStable])
		assert.Equal(t, "ch-gva-2", result.Labels[corev1.LabelTopologyZone])
		assert.Equal(t, "test-cluster", result.Labels[constants.LabelClusterName])
		assert.Equal(t, karpenterv1.CapacityTypeOnDemand, result.Labels[karpenterv1.CapacityTypeLabelKey])

		assert.Equal(t, "test-cluster-test-claim", result.Status.NodeName, "NodeName should be set to <cluster>-<nodeclaim>")
		assert.NotEmpty(t, result.Status.Capacity, "Capacity should be set")
		assert.NotEmpty(t, result.Status.Allocatable, "Allocatable should be set")
		assert.Equal(t, result.Status.Capacity, result.Status.Allocatable, "Allocatable should match Capacity initially")
	})

	t.Run("NodeClassNotFound", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)

		nodeClaim := mocks.CreateNodeClaim("test-claim", "test-nodeclass", "small-instance")
		nodeClaim.Status.ProviderID = ""

		_, err := env.CloudProvider.Create(env.Ctx, nodeClaim)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get node class")
	})

	t.Run("UserDataGenerationFails", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)

		nodeClaim := mocks.CreateNodeClaim("test-claim", "test-nodeclass", "small-instance")
		nodeClaim.Status.ProviderID = ""

		nodeClass := mocks.CreateNodeClass("test-nodeclass", "gpu")
		require.NoError(t, env.KubeClient.Create(env.Ctx, nodeClass))

		env.MockUserData.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return("", errors.New("failed to resolve instance type"))

		_, err := env.CloudProvider.Create(env.Ctx, nodeClaim)

		assert.Error(t, err)
		var createErr *karpentercloudprovider.CreateError
		assert.ErrorAs(t, err, &createErr)
		assert.Contains(t, err.Error(), "creating nodeclaim")
	})

	t.Run("InstanceCreationFails", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)

		nodeClaim := mocks.CreateNodeClaim("test-claim", "test-nodeclass", "small-instance")
		nodeClaim.Status.ProviderID = ""

		nodeClass := mocks.CreateNodeClass("test-nodeclass", "standard")
		require.NoError(t, env.KubeClient.Create(env.Ctx, nodeClass))

		env.MockUserData.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return("base64-encoded-userdata", nil)
		env.MockInstanceProvider.On("Create", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(nil, errors.New("API error creating instance"))

		_, err := env.CloudProvider.Create(env.Ctx, nodeClaim)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "API error creating instance")
	})
}

func TestCloudProvider_Delete(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)
		nodeClaim := mocks.CreateNodeClaim("test-claim", "test-nodeclass", "small-instance")
		nodeClaim.Status.ProviderID = utils.FormatProviderID(string(mocks.InstanceID1))

		env.MockInstanceProvider.On("Delete", mock.Anything, string(mocks.InstanceID1)).Return(nil)

		err := env.CloudProvider.Delete(env.Ctx, nodeClaim)
		assert.NoError(t, err)
	})

	t.Run("InvalidProviderID", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)
		nodeClaim := mocks.CreateNodeClaim("test-claim", "test-nodeclass", "small-instance")
		nodeClaim.Status.ProviderID = "invalid-provider-id"

		err := env.CloudProvider.Delete(env.Ctx, nodeClaim)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse provider ID")
	})

	t.Run("InstanceNotFound", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)
		nodeClaim := mocks.CreateNodeClaim("test-claim", "test-nodeclass", "small-instance")
		nodeClaim.Status.ProviderID = utils.FormatProviderID(string(mocks.InstanceID1))

		env.MockInstanceProvider.On("Delete", mock.Anything, string(mocks.InstanceID1)).
			Return(kerrors.NewInstanceNotFoundError(string(mocks.InstanceID1)))

		err := env.CloudProvider.Delete(env.Ctx, nodeClaim)
		assert.Error(t, err)
		assert.True(t, karpentercloudprovider.IsNodeClaimNotFoundError(err))
	})
}

func TestCloudProvider_Get(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)
		providerID := utils.FormatProviderID(string(mocks.InstanceID1))

		env.MockInstanceProvider.On("Get", mock.Anything, string(mocks.InstanceID1)).Return(testInstance, nil)

		result, err := env.CloudProvider.Get(env.Ctx, providerID)

		require.NoError(t, err)
		assert.Equal(t, "test-node-claim", result.Name)
		assert.Equal(t, utils.FormatProviderID(string(mocks.InstanceID1)), result.Status.ProviderID)
	})

	t.Run("InvalidProviderID", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)

		_, err := env.CloudProvider.Get(env.Ctx, "invalid-provider-id")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse provider ID")
	})

	t.Run("InstanceNotFound", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)
		providerID := utils.FormatProviderID(string(mocks.InstanceID1))

		env.MockInstanceProvider.On("Get", mock.Anything, string(mocks.InstanceID1)).
			Return(nil, karpentercloudprovider.NewNodeClaimNotFoundError(errors.New("instance not found")))

		_, err := env.CloudProvider.Get(env.Ctx, providerID)

		assert.Error(t, err)
		assert.True(t, karpentercloudprovider.IsNodeClaimNotFoundError(err))
	})
}

func TestCloudProvider_List(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)

		env.MockInstanceProvider.On("List", mock.Anything).Return(testInstances, nil)

		results, err := env.CloudProvider.List(env.Ctx)

		require.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "claim-1", results[0].Name)
		assert.Equal(t, "claim-2", results[1].Name)
	})

	t.Run("Empty", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)

		env.MockInstanceProvider.On("List", mock.Anything).Return([]*instance.Instance{}, nil)

		results, err := env.CloudProvider.List(env.Ctx)

		require.NoError(t, err)
		assert.Len(t, results, 0)
	})

	t.Run("APIError", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)

		env.MockInstanceProvider.On("List", mock.Anything).Return(nil, errors.New("API error"))

		_, err := env.CloudProvider.List(env.Ctx)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list instances")
	})
}

func TestCloudProvider_GetInstanceTypes(t *testing.T) {
	t.Run("NoNodePool", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)

		env.MockInstanceTypes.On("List", mock.Anything, mock.Anything).Return(testInstanceTypes, nil)

		results, err := env.CloudProvider.GetInstanceTypes(env.Ctx, nil)

		require.NoError(t, err)
		assert.Len(t, results, 2)
	})

	t.Run("WithNodePoolOverhead", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)

		nodePool := &karpenterv1.NodePool{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nodepool"},
			Spec: karpenterv1.NodePoolSpec{
				Template: karpenterv1.NodeClaimTemplate{
					Spec: karpenterv1.NodeClaimTemplateSpec{
						NodeClassRef: &karpenterv1.NodeClassReference{
							Group: "karpenter.exoscale.com",
							Kind:  "ExoscaleNodeClass",
							Name:  "test-nodeclass",
						},
					},
				},
			},
		}

		nodeClass := mocks.CreateNodeClass("test-nodeclass", "gpu")
		nodeClass.Spec.KubeReserved = apiv1.ResourceReservation{CPU: "100m"}
		require.NoError(t, env.KubeClient.Create(env.Ctx, nodeClass))

		env.MockInstanceTypes.On("List", mock.Anything, mock.Anything).Return(testInstanceTypes[:1], nil)

		results, err := env.CloudProvider.GetInstanceTypes(env.Ctx, nodePool)

		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.NotNil(t, results[0].Overhead)
		assert.Equal(t, resource.MustParse("100m"), results[0].Overhead.KubeReserved[corev1.ResourceCPU])
	})

	t.Run("APIError", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)

		env.MockInstanceTypes.On("List", mock.Anything, mock.Anything).Return(nil, errors.New("API error"))

		_, err := env.CloudProvider.GetInstanceTypes(env.Ctx, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list instance types")
	})
}

func TestCloudProvider_CalculateInstanceOverhead(t *testing.T) {
	t.Run("CustomValues", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)
		nodeClass := &apiv1.ExoscaleNodeClass{
			Spec: apiv1.ExoscaleNodeClassSpec{
				KubeReserved: apiv1.ResourceReservation{
					CPU:              "250m",
					Memory:           "512Mi",
					EphemeralStorage: "2Gi",
				},
				SystemReserved: apiv1.ResourceReservation{
					CPU:    "150m",
					Memory: "256Mi",
				},
			},
		}

		overhead := env.CloudProvider.CalculateInstanceOverhead(env.Ctx, nodeClass)

		assert.NotNil(t, overhead.KubeReserved)
		assert.NotNil(t, overhead.SystemReserved)
		assert.Equal(t, resource.MustParse("250m"), overhead.KubeReserved[corev1.ResourceCPU])
		assert.Equal(t, resource.MustParse("512Mi"), overhead.KubeReserved[corev1.ResourceMemory])
		assert.Equal(t, resource.MustParse("2Gi"), overhead.KubeReserved[corev1.ResourceEphemeralStorage])
		assert.Equal(t, resource.MustParse("150m"), overhead.SystemReserved[corev1.ResourceCPU])
		assert.Equal(t, resource.MustParse("256Mi"), overhead.SystemReserved[corev1.ResourceMemory])
	})

	t.Run("DefaultValues", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)
		nodeClass := &apiv1.ExoscaleNodeClass{
			Spec: apiv1.ExoscaleNodeClassSpec{},
		}

		overhead := env.CloudProvider.CalculateInstanceOverhead(env.Ctx, nodeClass)

		assert.NotNil(t, overhead.KubeReserved)
		assert.NotNil(t, overhead.SystemReserved)
	})
}

func TestCloudProvider_ApplyNodePoolOverhead(t *testing.T) {
	types := []*karpentercloudprovider.InstanceType{{
		Name: "standard.small",
		Capacity: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: resource.MustParse("4Gi"),
		},
	}}

	t.Run("NoNodePool", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)

		result := env.CloudProvider.ApplyNodePoolOverhead(env.Ctx, nil, types)

		require.Len(t, result, 1)
		assert.Nil(t, result[0].Overhead)
	})

	t.Run("NodeClassNotFound", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)

		nodePool := &karpenterv1.NodePool{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nodepool"},
			Spec: karpenterv1.NodePoolSpec{
				Template: karpenterv1.NodeClaimTemplate{
					Spec: karpenterv1.NodeClaimTemplateSpec{
						NodeClassRef: &karpenterv1.NodeClassReference{
							Group: "karpenter.exoscale.com",
							Kind:  "ExoscaleNodeClass",
							Name:  "test-nodeclass",
						},
					},
				},
			},
		}

		result := env.CloudProvider.ApplyNodePoolOverhead(env.Ctx, nodePool, types)

		require.Len(t, result, 1)
		assert.Nil(t, result[0].Overhead)
	})

	t.Run("Success", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)

		nodePool := &karpenterv1.NodePool{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nodepool"},
			Spec: karpenterv1.NodePoolSpec{
				Template: karpenterv1.NodeClaimTemplate{
					Spec: karpenterv1.NodeClaimTemplateSpec{
						NodeClassRef: &karpenterv1.NodeClassReference{
							Group: "karpenter.exoscale.com",
							Kind:  "ExoscaleNodeClass",
							Name:  "test-nodeclass",
						},
					},
				},
			},
		}

		nodeClass := mocks.CreateNodeClass("test-nodeclass", "standard")
		nodeClass.Spec.KubeReserved = apiv1.ResourceReservation{CPU: "100m"}
		require.NoError(t, env.KubeClient.Create(env.Ctx, nodeClass))

		result := env.CloudProvider.ApplyNodePoolOverhead(env.Ctx, nodePool, types)

		require.Len(t, result, 1)
		assert.NotNil(t, result[0].Overhead)
		assert.Equal(t, resource.MustParse("100m"), result[0].Overhead.KubeReserved[corev1.ResourceCPU])
	})
}

func TestCloudProvider_GetNodeClass(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)

		nodeClass := mocks.CreateNodeClass("test-nodeclass", "gpu")
		nodeClass.Spec.TemplateID = string(mocks.DefaultTemplateID)
		require.NoError(t, env.KubeClient.Create(env.Ctx, nodeClass))

		nodeClaim := mocks.CreateNodeClaim("test-claim", "test-nodeclass", "small-instance")

		result, err := env.CloudProvider.GetNodeClass(env.Ctx, nodeClaim)

		require.NoError(t, err)
		assert.Equal(t, "test-nodeclass", result.Name)
		assert.Equal(t, string(mocks.DefaultTemplateID), result.Spec.TemplateID)
	})

	t.Run("NotFound", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)

		nodeClaim := mocks.CreateNodeClaim("test-claim", "test-nodeclass", "small-instance")

		_, err := env.CloudProvider.GetNodeClass(env.Ctx, nodeClaim)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get node class")
	})

	t.Run("NoNodeClassRef", func(t *testing.T) {
		env := internaltesting.SetupCloudProviderTestEnvironment(t)

		nodeClaim := mocks.CreateNodeClaim("test-claim", "test-nodeclass", "small-instance")
		nodeClaim.Spec.NodeClassRef = nil

		_, err := env.CloudProvider.GetNodeClass(env.Ctx, nodeClaim)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nodeClassRef not specified")
	})
}

func TestCloudProvider_InterfaceMethods(t *testing.T) {
	env := internaltesting.SetupCloudProviderTestEnvironment(t)

	assert.Equal(t, "exoscale", env.CloudProvider.Name())

	policies := env.CloudProvider.RepairPolicies()
	assert.Len(t, policies, 5)
	assert.Contains(t, policies, karpentercloudprovider.RepairPolicy{
		ConditionType:      corev1.NodeReady,
		ConditionStatus:    corev1.ConditionFalse,
		TolerationDuration: 15 * time.Minute,
	})

	classes := env.CloudProvider.GetSupportedNodeClasses()
	assert.Len(t, classes, 1)
	assert.Equal(t, &apiv1.ExoscaleNodeClass{}, classes[0])
}
