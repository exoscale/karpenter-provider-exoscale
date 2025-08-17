package testing

import (
	"context"
	"testing"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/internal/testing/mocks"
	"github.com/exoscale/karpenter-exoscale/pkg/cloudprovider"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instance"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instancetype"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/pricing"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/userdata"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	karpentercloudprovider "sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/events"
)

type mockClient struct {
	mock.Mock
	client.Client
}

func (m *mockClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	args := m.Called(ctx, list, opts)
	return args.Error(0)
}

type MockInstanceProvider struct {
	mock.Mock
}

func (m *MockInstanceProvider) Create(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass, nodeClaim *karpenterv1.NodeClaim, userData string, tags map[string]string) (*instance.Instance, error) {
	args := m.Called(ctx, nodeClass, nodeClaim, userData, tags)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	switch v := args.Get(0).(type) {
	case *instance.Instance:
		return v, args.Error(1)
	case *mocks.TestInstance:
		return &instance.Instance{
			ID:                 v.ID,
			Name:               v.Name,
			State:              v.State,
			InstanceType:       v.InstanceType,
			Template:           v.Template,
			Zone:               v.Zone,
			Labels:             v.Labels,
			CreatedAt:          v.CreatedAt,
			SecurityGroups:     v.SecurityGroups,
			PrivateNetworks:    v.PrivateNetworks,
			AntiAffinityGroups: v.AntiAffinityGroups,
		}, args.Error(1)
	default:
		return nil, args.Error(1)
	}
}

func (m *MockInstanceProvider) Get(ctx context.Context, instanceID string) (*instance.Instance, error) {
	args := m.Called(ctx, instanceID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	switch v := args.Get(0).(type) {
	case *instance.Instance:
		return v, args.Error(1)
	case *mocks.TestInstance:
		return &instance.Instance{
			ID:                 v.ID,
			Name:               v.Name,
			State:              v.State,
			InstanceType:       v.InstanceType,
			Template:           v.Template,
			Zone:               v.Zone,
			Labels:             v.Labels,
			CreatedAt:          v.CreatedAt,
			SecurityGroups:     v.SecurityGroups,
			PrivateNetworks:    v.PrivateNetworks,
			AntiAffinityGroups: v.AntiAffinityGroups,
		}, args.Error(1)
	default:
		return nil, args.Error(1)
	}
}

func (m *MockInstanceProvider) List(ctx context.Context) ([]*instance.Instance, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	switch v := args.Get(0).(type) {
	case []*instance.Instance:
		return v, args.Error(1)
	case []*mocks.TestInstance:
		result := make([]*instance.Instance, len(v))
		for i, testInst := range v {
			result[i] = &instance.Instance{
				ID:                 testInst.ID,
				Name:               testInst.Name,
				State:              testInst.State,
				InstanceType:       testInst.InstanceType,
				Template:           testInst.Template,
				Zone:               testInst.Zone,
				Labels:             testInst.Labels,
				CreatedAt:          testInst.CreatedAt,
				SecurityGroups:     testInst.SecurityGroups,
				PrivateNetworks:    testInst.PrivateNetworks,
				AntiAffinityGroups: testInst.AntiAffinityGroups,
			}
		}
		return result, args.Error(1)
	default:
		return nil, args.Error(1)
	}
}

func (m *MockInstanceProvider) Delete(ctx context.Context, instanceID string) error {
	args := m.Called(ctx, instanceID)
	return args.Error(0)
}

func (m *MockInstanceProvider) UpdateTags(ctx context.Context, id string, tags map[string]string) error {
	args := m.Called(ctx, id, tags)
	return args.Error(0)
}

type MockInstanceTypeProvider struct {
	mock.Mock
}

func (m *MockInstanceTypeProvider) List(ctx context.Context, filters *instancetype.Filters) ([]*karpentercloudprovider.InstanceType, error) {
	args := m.Called(ctx, filters)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*karpentercloudprovider.InstanceType), args.Error(1)
}

func (m *MockInstanceTypeProvider) Refresh(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockInstanceTypeProvider) Get(ctx context.Context, name string) (*karpentercloudprovider.InstanceType, error) {
	args := m.Called(ctx, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*karpentercloudprovider.InstanceType), args.Error(1)
}

func (m *MockInstanceTypeProvider) GetInstanceTypeID(name string) (string, bool) {
	args := m.Called(name)
	return args.String(0), args.Bool(1)
}

type MockPricingProvider struct {
	mock.Mock
}

func (m *MockPricingProvider) GetPrice(ctx context.Context, instanceType string, currency pricing.Currency) (float64, error) {
	args := m.Called(ctx, instanceType, currency)
	return args.Get(0).(float64), args.Error(1)
}

func (m *MockPricingProvider) GetAllPrices(ctx context.Context, currency pricing.Currency) (map[string]float64, error) {
	args := m.Called(ctx, currency)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]float64), args.Error(1)
}

type MockUserDataProvider struct {
	mock.Mock
}

func (m *MockUserDataProvider) Generate(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass, nodeClaim *karpenterv1.NodeClaim, options *userdata.Options) (string, error) {
	args := m.Called(ctx, nodeClass, nodeClaim, options)
	return args.String(0), args.Error(1)
}

type CloudProviderTestEnvironment struct {
	T                    *testing.T
	Ctx                  context.Context
	CloudProvider        *cloudprovider.CloudProvider
	KubeClient           client.Client
	EventRecorder        events.Recorder
	MockKubeClient       client.Client
	MockExoClient        *mocks.MockExoscaleClient
	MockInstanceProvider *MockInstanceProvider
	MockInstanceTypes    *MockInstanceTypeProvider
	MockPricing          *MockPricingProvider
	MockUserData         *MockUserDataProvider
}

// SetupCloudProviderTestEnvironment creates a test environment with real CloudProvider and mocked dependencies
func SetupCloudProviderTestEnvironment(t *testing.T) *CloudProviderTestEnvironment {
	ctx := context.Background()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, apiv1.AddToScheme(scheme))

	gv := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	scheme.AddKnownTypes(gv,
		&karpenterv1.NodeClaim{}, &karpenterv1.NodeClaimList{},
		&karpenterv1.NodePool{}, &karpenterv1.NodePoolList{})

	mockExoClient := &mocks.MockExoscaleClient{}
	mockInstanceProvider := &MockInstanceProvider{}
	mockInstanceTypes := &MockInstanceTypeProvider{}
	mockPricing := &MockPricingProvider{}
	mockUserData := &MockUserDataProvider{}

	kubeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	fakeRecorder := record.NewFakeRecorder(100)
	eventRecorder := events.NewRecorder(fakeRecorder)

	cp := cloudprovider.NewCloudProvider(
		kubeClient,
		mockExoClient,
		"https://api-ch-gva-2.exoscale.com/v1",
		eventRecorder,
		mockInstanceTypes,
		mockPricing,
		mockInstanceProvider,
		mockUserData,
		"ch-gva-2",
		"test-cluster",
		"10.96.0.10",
		"cluster.local",
	)

	return &CloudProviderTestEnvironment{
		T:                    t,
		Ctx:                  ctx,
		CloudProvider:        cp,
		KubeClient:           kubeClient,
		EventRecorder:        eventRecorder,
		MockKubeClient:       &mockClient{},
		MockExoClient:        mockExoClient,
		MockInstanceProvider: mockInstanceProvider,
		MockInstanceTypes:    mockInstanceTypes,
		MockPricing:          mockPricing,
		MockUserData:         mockUserData,
	}
}
