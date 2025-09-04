package instance

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/internal/testing/mocks"
	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-exoscale/pkg/errors"
	"github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
)

func TestProvider_Create(t *testing.T) {
	ctx := context.Background()
	mockClient := &mocks.MockExoscaleClient{}
	mockInstanceTypeProvider := &mocks.MockInstanceTypeProvider{}

	mockInstanceTypeProvider.On("Get", mock.Anything, "standard.medium").Return(&cloudprovider.InstanceType{
		Name: "standard.medium",
	}, nil)

	cacheInstance := cache.New(30*time.Second, 60*time.Second)

	provider := &DefaultProvider{
		exoClient:            mockClient,
		zone:                 "ch-gva-2",
		clusterID:            "test-cluster",
		cache:                cacheInstance,
		instanceTypeProvider: mockInstanceTypeProvider,
		instancePrefix:       "karpenter",
	}

	nodeClass := &apiv1.ExoscaleNodeClass{
		Spec: apiv1.ExoscaleNodeClassSpec{
			TemplateID:      string(mocks.DefaultTemplateID),
			SecurityGroups:  []string{string(mocks.DefaultSecurityGroupID)},
			PrivateNetworks: []string{string(mocks.PrivateNetworkID1)},
		},
	}

	nodeClaim := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nodeclaim",
		},
		Spec: karpenterv1.NodeClaimSpec{
			Requirements: []karpenterv1.NodeSelectorRequirementWithMinValues{
				{
					NodeSelectorRequirement: corev1.NodeSelectorRequirement{
						Key:      corev1.LabelInstanceTypeStable,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"standard.medium"},
					},
				},
			},
		},
	}

	operation := &egov3.Operation{
		ID:    mocks.OperationID1,
		State: egov3.OperationStateSuccess,
		Reference: &egov3.OperationReference{
			ID: mocks.InstanceID1,
		},
	}

	instance := &egov3.Instance{
		ID:    mocks.InstanceID1,
		Name:  "test-cluster-test-nodeclaim",
		State: egov3.InstanceStateRunning,
	}

	mockInstanceTypeProvider.On("GetInstanceTypeID", "standard.medium").Return(string(mocks.StandardMediumTypeID), true)
	mockInstanceTypeProvider.On("Get", mock.Anything, "standard.medium").Return(&cloudprovider.InstanceType{
		Name: "standard.medium",
	}, nil)

	mockClient.On("CreateInstance", mock.Anything, mock.MatchedBy(func(req egov3.CreateInstanceRequest) bool {
		return req.Labels[constants.LabelManagedBy] == constants.ManagedByKarpenter &&
			req.Labels[constants.LabelClusterID] == "test-cluster" &&
			req.Labels[constants.LabelNodeClaim] == "test-nodeclaim"
	})).Return(operation, nil)
	mockClient.On("Wait", mock.Anything, operation, mock.Anything).Return(operation, nil)
	mockClient.On("GetInstance", mock.Anything, mocks.InstanceID1).Return(instance, nil)
	mockClient.On("AttachInstanceToPrivateNetwork", mock.Anything, mocks.PrivateNetworkID1, mock.Anything).Return(operation, nil)

	result, err := provider.Create(ctx, nodeClass, nodeClaim, "test-user-data", nil)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, string(mocks.InstanceID1), result.ID)
	assert.Equal(t, "test-cluster-test-nodeclaim", result.Name)
	mockClient.AssertExpectations(t)
}

func TestProvider_Delete(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		ctx := context.Background()
		mockClient := &mocks.MockExoscaleClient{}

		provider := &DefaultProvider{
			instanceTypeProvider: &mocks.MockInstanceTypeProvider{},
			exoClient:            mockClient,
			zone:                 "ch-gva-2",
			clusterID:            "test-cluster",
			cache:                cache.New(30*time.Second, 60*time.Second),
			instancePrefix:       "karpenter",
		}

		operation := &egov3.Operation{
			ID:    mocks.OperationID1,
			State: egov3.OperationStateSuccess,
		}

		mockClient.On("DeleteInstance", mock.Anything, mocks.InstanceID1).Return(operation, nil)
		mockClient.On("Wait", mock.Anything, operation, mock.Anything).
			Return(operation, nil)

		err := provider.Delete(ctx, string(mocks.InstanceID1))

		assert.NoError(t, err)
		mockClient.AssertExpectations(t)
	})

	t.Run("InstanceNotFound", func(t *testing.T) {
		ctx := context.Background()
		mockClient := &mocks.MockExoscaleClient{}

		provider := &DefaultProvider{
			instanceTypeProvider: &mocks.MockInstanceTypeProvider{},
			exoClient:            mockClient,
			zone:                 "ch-gva-2",
			clusterID:            "test-cluster",
			cache:                cache.New(30*time.Second, 60*time.Second),
			instancePrefix:       "karpenter",
		}

		mockClient.On("DeleteInstance", mock.Anything, mocks.InstanceID1).Return(nil, egov3.ErrNotFound)

		err := provider.Delete(ctx, string(mocks.InstanceID1))

		assert.Error(t, err)
		assert.True(t, errors.IsInstanceNotFoundError(err))
		mockClient.AssertExpectations(t)
	})

	t.Run("DeleteFails", func(t *testing.T) {
		ctx := context.Background()
		mockClient := &mocks.MockExoscaleClient{}

		provider := &DefaultProvider{
			instanceTypeProvider: &mocks.MockInstanceTypeProvider{},
			exoClient:            mockClient,
			zone:                 "ch-gva-2",
			clusterID:            "test-cluster",
			cache:                cache.New(30*time.Second, 60*time.Second),
			instancePrefix:       "karpenter",
		}

		// Network error when deleting
		mockClient.On("DeleteInstance", mock.Anything, mocks.InstanceID1).
			Return(nil, stderrors.New("network error"))

		err := provider.Delete(ctx, string(mocks.InstanceID1))

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete instance")
		mockClient.AssertExpectations(t)
	})
}

func TestProvider_Get(t *testing.T) {
	ctx := context.Background()
	mockClient := &mocks.MockExoscaleClient{}

	provider := &DefaultProvider{
		instanceTypeProvider: &mocks.MockInstanceTypeProvider{},
		exoClient:            mockClient,
		zone:                 "ch-gva-2",
		clusterID:            "test-cluster",
		cache:                cache.New(30*time.Second, 60*time.Second),
		instancePrefix:       "karpenter",
	}

	createdAt := time.Now()
	instance := &egov3.Instance{
		ID:        mocks.InstanceID1,
		Name:      "test-instance",
		State:     egov3.InstanceStateRunning,
		CreatedAT: createdAt,
	}

	mockClient.On("GetInstance", mock.Anything, mocks.InstanceID1).Return(instance, nil)

	result, err := provider.Get(ctx, string(mocks.InstanceID1))

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, string(mocks.InstanceID1), result.ID)
	assert.Equal(t, "test-instance", result.Name)
	assert.Equal(t, egov3.InstanceStateRunning, result.State)
	mockClient.AssertExpectations(t)
}

func TestProvider_List(t *testing.T) {
	ctx := context.Background()
	mockClient := &mocks.MockExoscaleClient{}

	provider := &DefaultProvider{
		instanceTypeProvider: &mocks.MockInstanceTypeProvider{},
		exoClient:            mockClient,
		zone:                 "ch-gva-2",
		clusterID:            "25f39e1e-c8fa-4143-9e5f-63a9e45115fa",
		cache:                cache.New(30*time.Second, 60*time.Second),
		instancePrefix:       "karpenter",
	}

	instances := []egov3.ListInstancesResponseInstances{
		{
			ID:   mocks.InstanceID1,
			Name: "test-cluster-node-1",
			Labels: map[string]string{
				constants.LabelManagedBy: constants.ManagedByKarpenter,
				constants.LabelClusterID: "25f39e1e-c8fa-4143-9e5f-63a9e45115fa",
			},
		},
		{
			ID:   mocks.InstanceID2,
			Name: "test-cluster-node-2",
			Labels: map[string]string{
				constants.LabelManagedBy: constants.ManagedByKarpenter,
				constants.LabelClusterID: "25f39e1e-c8fa-4143-9e5f-63a9e45115fa",
			},
		},
		{
			ID:   mocks.InstanceID3,
			Name: "other-cluster-node",
			Labels: map[string]string{
				constants.LabelManagedBy: constants.ManagedByKarpenter,
				constants.LabelClusterID: "other-cluster",
			},
		},
	}

	listResponse := &egov3.ListInstancesResponse{
		Instances: instances,
	}

	mockClient.On("ListInstances", mock.Anything, mock.Anything).Return(listResponse, nil)

	fullInstance1 := &egov3.Instance{
		ID:   mocks.InstanceID1,
		Name: "test-cluster-node-1",
		Labels: map[string]string{
			constants.LabelManagedBy: constants.ManagedByKarpenter,
			constants.LabelClusterID: "25f39e1e-c8fa-4143-9e5f-63a9e45115fa",
		},
		AntiAffinityGroups: []egov3.AntiAffinityGroup{},
	}
	fullInstance2 := &egov3.Instance{
		ID:   mocks.InstanceID2,
		Name: "test-cluster-node-2",
		Labels: map[string]string{
			constants.LabelManagedBy: constants.ManagedByKarpenter,
			constants.LabelClusterID: "25f39e1e-c8fa-4143-9e5f-63a9e45115fa",
		},
		AntiAffinityGroups: []egov3.AntiAffinityGroup{},
	}

	mockClient.On("GetInstance", mock.Anything, mocks.InstanceID1).Return(fullInstance1, nil)
	mockClient.On("GetInstance", mock.Anything, mocks.InstanceID2).Return(fullInstance2, nil)

	result, err := provider.List(ctx)

	assert.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, string(mocks.InstanceID1), result[0].ID)
	assert.Equal(t, string(mocks.InstanceID2), result[1].ID)
	mockClient.AssertExpectations(t)
}

func TestProvider_buildInstanceLabels(t *testing.T) {
	ctx := context.Background()
	mockClient := &mocks.MockExoscaleClient{}
	mockInstanceTypeProvider := &mocks.MockInstanceTypeProvider{}

	provider := &DefaultProvider{
		instanceTypeProvider: mockInstanceTypeProvider,
		exoClient:            mockClient,
		zone:                 "ch-gva-2",
		clusterID:            "25f39e1e-c8fa-4143-9e5f-63a9e45115fa",
		cache:                cache.New(30*time.Second, 60*time.Second),
		instancePrefix:       "karpenter",
	}

	nodeClass := &apiv1.ExoscaleNodeClass{
		Spec: apiv1.ExoscaleNodeClassSpec{
			TemplateID:     string(mocks.DefaultTemplateID),
			SecurityGroups: []string{string(mocks.DefaultSecurityGroupID)},
		},
	}

	nodeClaim := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nodeclaim",
		},
		Spec: karpenterv1.NodeClaimSpec{
			Requirements: []karpenterv1.NodeSelectorRequirementWithMinValues{
				{
					NodeSelectorRequirement: corev1.NodeSelectorRequirement{
						Key:      corev1.LabelInstanceTypeStable,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"standard.medium"},
					},
				},
			},
		},
	}

	tags := map[string]string{
		"custom-tag-1": "value1",
		"custom-tag-2": "value2",
	}

	operation := &egov3.Operation{
		ID:    mocks.OperationID1,
		State: egov3.OperationStateSuccess,
		Reference: &egov3.OperationReference{
			ID: mocks.InstanceID1,
		},
	}

	instance := &egov3.Instance{
		ID:    mocks.InstanceID1,
		Name:  "karpenter-test-nodeclaim",
		State: egov3.InstanceStateRunning,
	}

	expectedLabels := map[string]string{
		constants.LabelManagedBy: constants.ManagedByKarpenter,
		constants.LabelClusterID: "25f39e1e-c8fa-4143-9e5f-63a9e45115fa",
		constants.LabelNodeClaim: "test-nodeclaim",
		"custom-tag-1":           "value1",
		"custom-tag-2":           "value2",
	}

	mockInstanceTypeProvider.On("GetInstanceTypeID", "standard.medium").Return(string(mocks.StandardMediumTypeID), true)
	mockInstanceTypeProvider.On("Get", mock.Anything, "standard.medium").Return(&cloudprovider.InstanceType{
		Name: "standard.medium",
	}, nil)

	mockClient.On("CreateInstance", mock.Anything, mock.MatchedBy(func(req egov3.CreateInstanceRequest) bool {
		for key, expectedValue := range expectedLabels {
			if actualValue, ok := req.Labels[key]; !ok || actualValue != expectedValue {
				return false
			}
		}

		if len(req.Labels) != len(expectedLabels) {
			return false
		}

		return req.Name == "karpenter-test-nodeclaim" &&
			req.Template.ID == mocks.DefaultTemplateID &&
			req.UserData == "test-user-data"
	})).Return(operation, nil)

	mockClient.On("Wait", mock.Anything, operation, mock.Anything).Return(operation, nil)
	mockClient.On("GetInstance", mock.Anything, mocks.InstanceID1).Return(instance, nil)

	result, err := provider.Create(ctx, nodeClass, nodeClaim, "test-user-data", tags)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	mockClient.AssertExpectations(t)
}

func TestProvider_Create_ErrorOnCreateInstance(t *testing.T) {
	ctx := context.Background()
	mockClient := &mocks.MockExoscaleClient{}
	mockInstanceTypeProvider := &mocks.MockInstanceTypeProvider{}
	mockInstanceTypeProvider.On("Get", mock.Anything, "standard.medium").Return(&cloudprovider.InstanceType{
		Name: "standard.medium",
	}, nil)

	provider := &DefaultProvider{
		exoClient:            mockClient,
		zone:                 "ch-gva-2",
		clusterID:            "test-cluster",
		cache:                cache.New(30*time.Second, 60*time.Second),
		instanceTypeProvider: mockInstanceTypeProvider,
		instancePrefix:       "karpenter",
	}

	nodeClass := &apiv1.ExoscaleNodeClass{
		Spec: apiv1.ExoscaleNodeClassSpec{
			TemplateID: string(mocks.DefaultTemplateID),
		},
	}

	nodeClaim := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nodeclaim",
		},
		Spec: karpenterv1.NodeClaimSpec{
			Requirements: []karpenterv1.NodeSelectorRequirementWithMinValues{
				{
					NodeSelectorRequirement: corev1.NodeSelectorRequirement{
						Key:      corev1.LabelInstanceTypeStable,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"standard.medium"},
					},
				},
			},
		},
	}

	// Mock the instancetype provider to return a UUID for the instance type
	mockInstanceTypeProvider.On("GetInstanceTypeID", "standard.medium").Return(string(mocks.StandardMediumTypeID), true)

	mockClient.On("CreateInstance", mock.Anything, mock.Anything).
		Return(nil, stderrors.New("API error"))

	result, err := provider.Create(ctx, nodeClass, nodeClaim, "userData", nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to create instance")
	mockClient.AssertExpectations(t)
}

func TestProvider_Get_NotFound(t *testing.T) {
	ctx := context.Background()
	mockClient := &mocks.MockExoscaleClient{}

	provider := &DefaultProvider{
		instanceTypeProvider: &mocks.MockInstanceTypeProvider{},
		exoClient:            mockClient,
		zone:                 "ch-gva-2",
		clusterID:            "test-cluster",
		cache:                cache.New(30*time.Second, 60*time.Second),
		instancePrefix:       "karpenter",
	}

	mockClient.On("GetInstance", mock.Anything, mocks.InstanceID1).
		Return(nil, egov3.ErrNotFound)

	result, err := provider.Get(ctx, string(mocks.InstanceID1))

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, errors.IsInstanceNotFoundError(err))
	mockClient.AssertExpectations(t)
}

func TestProvider_UpdateTags(t *testing.T) {
	tests := []struct {
		name                string
		instanceID          string
		existingLabels      map[string]string
		newTags             map[string]string
		expectedLabels      map[string]string
		getInstanceError    error
		updateInstanceError error
		waitError           error
		expectError         bool
		expectedErrorText   string
	}{
		{
			name:       "successful update with new tags",
			instanceID: string(mocks.InstanceID1),
			existingLabels: map[string]string{
				constants.LabelManagedBy: constants.ManagedByKarpenter,
				constants.LabelClusterID: "25f39e1e-c8fa-4143-9e5f-63a9e45115fa",
				constants.LabelNodeClaim: "test-claim",
			},
			newTags: map[string]string{
				"env":       "production",
				"team":      "platform",
				"component": "worker",
			},
			expectedLabels: map[string]string{
				constants.LabelManagedBy: constants.ManagedByKarpenter,
				constants.LabelClusterID: "25f39e1e-c8fa-4143-9e5f-63a9e45115fa",
				constants.LabelNodeClaim: "test-claim",
				"env":                    "production",
				"team":                   "platform",
				"component":              "worker",
			},
		},
		{
			name:       "successful update overriding existing tag",
			instanceID: string(mocks.InstanceID1),
			existingLabels: map[string]string{
				constants.LabelManagedBy: constants.ManagedByKarpenter,
				constants.LabelClusterID: "25f39e1e-c8fa-4143-9e5f-63a9e45115fa",
				"env":                    "staging",
			},
			newTags: map[string]string{
				"env": "production",
			},
			expectedLabels: map[string]string{
				constants.LabelManagedBy: constants.ManagedByKarpenter,
				constants.LabelClusterID: "25f39e1e-c8fa-4143-9e5f-63a9e45115fa",
				"env":                    "production",
			},
		},
		{
			name:       "successful update with empty tags",
			instanceID: string(mocks.InstanceID1),
			existingLabels: map[string]string{
				constants.LabelManagedBy: constants.ManagedByKarpenter,
				constants.LabelClusterID: "25f39e1e-c8fa-4143-9e5f-63a9e45115fa",
			},
			newTags: map[string]string{},
			expectedLabels: map[string]string{
				constants.LabelManagedBy: constants.ManagedByKarpenter,
				constants.LabelClusterID: "25f39e1e-c8fa-4143-9e5f-63a9e45115fa",
			},
		},
		{
			name:              "error when instance not found",
			instanceID:        string(mocks.InstanceID1),
			getInstanceError:  stderrors.New("instance not found"),
			expectError:       true,
			expectedErrorText: "failed to get instance",
		},
		{
			name:       "error when update fails",
			instanceID: string(mocks.InstanceID1),
			existingLabels: map[string]string{
				constants.LabelManagedBy: constants.ManagedByKarpenter,
			},
			newTags: map[string]string{
				"env": "production",
			},
			updateInstanceError: stderrors.New("update failed"),
			expectError:         true,
			expectedErrorText:   "failed to update instance labels",
		},
		{
			name:       "error when wait fails",
			instanceID: string(mocks.InstanceID1),
			existingLabels: map[string]string{
				constants.LabelManagedBy: constants.ManagedByKarpenter,
			},
			newTags: map[string]string{
				"env": "production",
			},
			waitError:         stderrors.New("wait failed"),
			expectError:       true,
			expectedErrorText: "failed waiting for instance label update",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			mockClient := &mocks.MockExoscaleClient{}

			provider := &DefaultProvider{
				instanceTypeProvider: &mocks.MockInstanceTypeProvider{},
				exoClient:            mockClient,
				zone:                 "ch-gva-2",
				clusterID:            "test-cluster",
				cache:                cache.New(30*time.Second, 60*time.Second),
				instancePrefix:       "karpenter",
			}

			if tt.getInstanceError != nil {
				mockClient.On("GetInstance", mock.Anything, egov3.UUID(tt.instanceID)).
					Return(nil, tt.getInstanceError)
			} else {
				instance := &egov3.Instance{
					ID:     egov3.UUID(tt.instanceID),
					Name:   "test-instance",
					Labels: tt.existingLabels,
				}
				mockClient.On("GetInstance", mock.Anything, egov3.UUID(tt.instanceID)).
					Return(instance, nil)

				if tt.updateInstanceError != nil {
					mockClient.On("UpdateInstance", mock.Anything, egov3.UUID(tt.instanceID), mock.Anything).Return(nil, tt.updateInstanceError)
				} else {
					operation := &egov3.Operation{
						ID:    mocks.OperationID1,
						State: egov3.OperationStateSuccess,
					}

					mockClient.On("UpdateInstance", mock.Anything, egov3.UUID(tt.instanceID), mock.Anything).Return(operation, nil)

					if tt.waitError != nil {
						mockClient.On("Wait", mock.Anything, operation, mock.Anything).
							Return(nil, tt.waitError)
					} else {
						mockClient.On("Wait", mock.Anything, operation, mock.Anything).
							Return(operation, nil)
					}
				}
			}

			err := provider.UpdateTags(ctx, tt.instanceID, tt.newTags)

			if tt.expectError {
				assert.Error(t, err)
				if tt.expectedErrorText != "" {
					assert.Contains(t, err.Error(), tt.expectedErrorText)
				}
			} else {
				assert.NoError(t, err)
			}

			mockClient.AssertExpectations(t)
		})
	}
}

func TestProvider_UpdateTags_CacheInvalidation(t *testing.T) {
	ctx := context.Background()
	mockClient := &mocks.MockExoscaleClient{}

	cacheInstance := cache.New(30*time.Second, 60*time.Second)
	provider := &DefaultProvider{
		instanceTypeProvider: &mocks.MockInstanceTypeProvider{},
		exoClient:            mockClient,
		zone:                 "ch-gva-2",
		clusterID:            "test-cluster",
		cache:                cacheInstance,
		instancePrefix:       "karpenter",
	}

	instanceID := string(mocks.InstanceID1)

	cachedInstance := &Instance{
		ID:   instanceID,
		Name: "test-instance",
		Labels: map[string]string{
			"old": "label",
		},
	}
	cacheInstance.Set(instanceID, cachedInstance, 30*time.Second)

	_, found := cacheInstance.Get(instanceID)
	assert.True(t, found)

	instance := &egov3.Instance{
		ID:   egov3.UUID(instanceID),
		Name: "test-instance",
		Labels: map[string]string{
			"old": "label",
		},
	}
	operation := &egov3.Operation{
		ID:    mocks.OperationID1,
		State: egov3.OperationStateSuccess,
	}

	mockClient.On("GetInstance", mock.Anything, egov3.UUID(instanceID)).Return(instance, nil)
	mockClient.On("UpdateInstance", mock.Anything, egov3.UUID(instanceID), mock.Anything).Return(operation, nil)
	mockClient.On("Wait", mock.Anything, operation, mock.Anything).Return(operation, nil)

	err := provider.UpdateTags(ctx, instanceID, map[string]string{"new": "label"})

	assert.NoError(t, err)

	_, found = cacheInstance.Get(instanceID)
	assert.False(t, found)

	mockClient.AssertExpectations(t)
}

func TestProvider_Create_AntiAffinityGroupCapacity(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name                      string
		antiAffinityGroups        []string
		mockAntiAffinityGroups    map[string]*egov3.AntiAffinityGroup
		expectedAntiAffinityCount int
		expectError               bool
	}{
		{
			name:               "anti-affinity group with capacity",
			antiAffinityGroups: []string{"aag-1"},
			mockAntiAffinityGroups: map[string]*egov3.AntiAffinityGroup{
				"aag-1": {
					ID:        egov3.UUID("aag-1"),
					Name:      "test-aag-1",
					Instances: make([]egov3.Instance, 3),
				},
			},
			expectedAntiAffinityCount: 1,
			expectError:               false,
		},
		{
			name:               "anti-affinity group at capacity",
			antiAffinityGroups: []string{"aag-1"},
			mockAntiAffinityGroups: map[string]*egov3.AntiAffinityGroup{
				"aag-1": {
					ID:        egov3.UUID("aag-1"),
					Name:      "test-aag-1",
					Instances: make([]egov3.Instance, constants.MaxInstancesPerAntiAffinityGroup),
				},
			},
			expectedAntiAffinityCount: 0,
			expectError:               true,
		},
		{
			name:               "one group at capacity in list",
			antiAffinityGroups: []string{"aag-1", "aag-2", "aag-3"},
			mockAntiAffinityGroups: map[string]*egov3.AntiAffinityGroup{
				"aag-1": {
					ID:        egov3.UUID("aag-1"),
					Name:      "test-aag-1",
					Instances: make([]egov3.Instance, constants.MaxInstancesPerAntiAffinityGroup),
				},
				"aag-2": {
					ID:        egov3.UUID("aag-2"),
					Name:      "test-aag-2",
					Instances: make([]egov3.Instance, 5),
				},
				"aag-3": {
					ID:        egov3.UUID("aag-3"),
					Name:      "test-aag-3",
					Instances: make([]egov3.Instance, 3),
				},
			},
			expectedAntiAffinityCount: 0,
			expectError:               true,
		},
		{
			name:               "all groups have capacity",
			antiAffinityGroups: []string{"aag-1", "aag-2"},
			mockAntiAffinityGroups: map[string]*egov3.AntiAffinityGroup{
				"aag-1": {
					ID:        egov3.UUID("aag-1"),
					Name:      "test-aag-1",
					Instances: make([]egov3.Instance, 3),
				},
				"aag-2": {
					ID:        egov3.UUID("aag-2"),
					Name:      "test-aag-2",
					Instances: make([]egov3.Instance, 5),
				},
			},
			expectedAntiAffinityCount: 2,
			expectError:               false,
		},
		{
			name:                      "no anti-affinity groups",
			antiAffinityGroups:        []string{},
			mockAntiAffinityGroups:    map[string]*egov3.AntiAffinityGroup{},
			expectedAntiAffinityCount: 0,
			expectError:               false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mocks.MockExoscaleClient{}
			mockInstanceTypeProvider := &mocks.MockInstanceTypeProvider{}

			mockInstanceTypeProvider.On("Get", mock.Anything, "standard.medium").Return(&cloudprovider.InstanceType{
				Name: "standard.medium",
			}, nil)
			mockInstanceTypeProvider.On("GetInstanceTypeID", "standard.medium").Return("it-123", true)

			provider := &DefaultProvider{
				exoClient:            mockClient,
				zone:                 "ch-gva-2",
				clusterID:            "test-cluster",
				cache:                cache.New(30*time.Second, 60*time.Second),
				instanceTypeProvider: mockInstanceTypeProvider,
				instancePrefix:       "karpenter",
			}

			foundFullGroup := false
			for _, groupID := range tt.antiAffinityGroups {
				group, exists := tt.mockAntiAffinityGroups[groupID]
				if exists {
					if foundFullGroup {
						mockClient.On("GetAntiAffinityGroup", mock.Anything, egov3.UUID(groupID)).Return(group, nil).Maybe()
					} else {
						mockClient.On("GetAntiAffinityGroup", mock.Anything, egov3.UUID(groupID)).Return(group, nil)
						if len(group.Instances) >= constants.MaxInstancesPerAntiAffinityGroup {
							foundFullGroup = true
						}
					}
				}
			}

			operation := &egov3.Operation{
				ID:        egov3.UUID("op-123"),
				State:     egov3.OperationStateSuccess,
				Reference: &egov3.OperationReference{ID: mocks.InstanceID1},
			}

			instance := &egov3.Instance{
				ID:    mocks.InstanceID1,
				Name:  "test-cluster-test-nodeclaim",
				State: egov3.InstanceStateRunning,
				Manager: &egov3.Manager{
					ID:   mocks.InstanceID1,
					Type: "instance",
				},
				CreatedAT: time.Now(),
				Labels: map[string]string{
					constants.LabelManagedBy: constants.ManagedByKarpenter,
					constants.LabelClusterID: "25f39e1e-c8fa-4143-9e5f-63a9e45115fa",
					constants.LabelNodeClaim: "test-nodeclaim",
				},
				InstanceType: &egov3.InstanceType{
					ID:     egov3.UUID("it-123"),
					Family: "standard",
					Size:   "medium",
				},
			}

			if !tt.expectError {
				mockClient.On("CreateInstance", mock.Anything, mock.MatchedBy(func(req egov3.CreateInstanceRequest) bool {
					return len(req.AntiAffinityGroups) == tt.expectedAntiAffinityCount
				})).Return(operation, nil)

				mockClient.On("Wait", mock.Anything, operation, mock.Anything).Return(operation, nil)
				mockClient.On("GetInstance", mock.Anything, mocks.InstanceID1).Return(instance, nil)
			}

			nodeClass := &apiv1.ExoscaleNodeClass{
				Spec: apiv1.ExoscaleNodeClassSpec{
					TemplateID:         string(mocks.DefaultTemplateID),
					AntiAffinityGroups: tt.antiAffinityGroups,
				},
			}

			nodeClaim := &karpenterv1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclaim",
				},
				Spec: karpenterv1.NodeClaimSpec{
					Requirements: []karpenterv1.NodeSelectorRequirementWithMinValues{
						{
							NodeSelectorRequirement: corev1.NodeSelectorRequirement{
								Key:      corev1.LabelInstanceTypeStable,
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"standard.medium"},
							},
						},
					},
				},
			}

			result, err := provider.Create(ctx, nodeClass, nodeClaim, "test-user-data", nil)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}

			mockClient.AssertExpectations(t)
		})
	}
}
