package template

import (
	"context"
	"testing"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

type mockClient struct {
	mock.Mock
}

func (m *mockClient) GetActiveNodepoolTemplate(ctx context.Context, version string, variant egov3.GetActiveNodepoolTemplateVariant) (*egov3.GetActiveNodepoolTemplateResponse, error) {
	args := m.Called(ctx, version, variant)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egov3.GetActiveNodepoolTemplateResponse), args.Error(1)
}

func TestResolveTemplateID(t *testing.T) {
	ctx := context.Background()
	testTemplateID := "550e8400-e29b-41d4-a716-446655440000"

	tests := []struct {
		name          string
		nodeClass     *apiv1.ExoscaleNodeClass
		mockSetup     func(*mockClient)
		expectedID    string
		expectedError string
	}{
		{
			name: "explicit templateID specified",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
				Spec: apiv1.ExoscaleNodeClassSpec{
					TemplateID: testTemplateID,
				},
			},
			mockSetup:  func(m *mockClient) {},
			expectedID: testTemplateID,
		},
		{
			name: "imageTemplateSelector with explicit version and variant",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
				Spec: apiv1.ExoscaleNodeClassSpec{
					ImageTemplateSelector: &apiv1.ImageTemplateSelector{
						Version: "1.30.0",
						Variant: "nvidia",
					},
				},
			},
			mockSetup: func(m *mockClient) {
				m.On("GetActiveNodepoolTemplate", mock.Anything, "1.30.0", egov3.GetActiveNodepoolTemplateVariantNvidia).
					Return(&egov3.GetActiveNodepoolTemplateResponse{
						ActiveTemplate: egov3.UUID(testTemplateID),
					}, nil)
			},
			expectedID: testTemplateID,
		},
		{
			name: "imageTemplateSelector with explicit version defaults to standard variant",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
				Spec: apiv1.ExoscaleNodeClassSpec{
					ImageTemplateSelector: &apiv1.ImageTemplateSelector{
						Version: "1.29.5",
					},
				},
			},
			mockSetup: func(m *mockClient) {
				m.On("GetActiveNodepoolTemplate", mock.Anything, "1.29.5", egov3.GetActiveNodepoolTemplateVariantStandard).
					Return(&egov3.GetActiveNodepoolTemplateResponse{
						ActiveTemplate: egov3.UUID(testTemplateID),
					}, nil)
			},
			expectedID: testTemplateID,
		},
		{
			name: "imageTemplateSelector empty requires cluster version detection",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
				Spec: apiv1.ExoscaleNodeClassSpec{
					ImageTemplateSelector: &apiv1.ImageTemplateSelector{},
				},
			},
			mockSetup:     func(m *mockClient) {},
			expectedError: "failed to detect cluster version",
		},
		{
			name: "imageTemplateSelector with only variant requires cluster version detection",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
				Spec: apiv1.ExoscaleNodeClassSpec{
					ImageTemplateSelector: &apiv1.ImageTemplateSelector{
						Variant: "nvidia",
					},
				},
			},
			mockSetup:     func(m *mockClient) {},
			expectedError: "failed to detect cluster version",
		},
		{
			name: "neither templateID nor imageTemplateSelector specified",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
				Spec:       apiv1.ExoscaleNodeClassSpec{},
			},
			mockSetup:     func(m *mockClient) {},
			expectedError: "neither templateID nor imageTemplateSelector is specified",
		},
		{
			name: "API error when looking up template",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
				Spec: apiv1.ExoscaleNodeClassSpec{
					ImageTemplateSelector: &apiv1.ImageTemplateSelector{
						Version: "1.30.0",
						Variant: "standard",
					},
				},
			},
			mockSetup: func(m *mockClient) {
				m.On("GetActiveNodepoolTemplate", mock.Anything, "1.30.0", egov3.GetActiveNodepoolTemplateVariantStandard).
					Return(nil, assert.AnError)
			},
			expectedError: "failed to resolve template",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := new(mockClient)
			tt.mockSetup(mc)

			resolver := &DefaultResolver{
				client:     mc,
				zone:       "ch-gva-2",
				kubeConfig: &rest.Config{},
			}

			templateID, err := resolver.ResolveTemplateID(ctx, tt.nodeClass)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedID, templateID)
			}

			mc.AssertExpectations(t)
		})
	}
}

func TestExtractSemVer(t *testing.T) {
	tests := []struct {
		name       string
		gitVersion string
		want       string
		wantErr    bool
	}{
		{
			name:       "with v prefix and metadata",
			gitVersion: "v1.33.2-exo.1",
			want:       "1.33.2",
			wantErr:    false,
		},
		{
			name:       "with v prefix only",
			gitVersion: "v1.30.0",
			want:       "1.30.0",
			wantErr:    false,
		},
		{
			name:       "without v prefix",
			gitVersion: "1.29.5",
			want:       "1.29.5",
			wantErr:    false,
		},
		{
			name:       "with multiple metadata segments",
			gitVersion: "v1.28.3-exo.1234.5",
			want:       "1.28.3",
			wantErr:    false,
		},
		{
			name:       "invalid - missing patch version",
			gitVersion: "v1.30",
			want:       "",
			wantErr:    true,
		},
		{
			name:       "invalid - non-numeric version",
			gitVersion: "vX.Y.Z",
			want:       "",
			wantErr:    true,
		},
		{
			name:       "invalid - empty string",
			gitVersion: "",
			want:       "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractSemVer(tt.gitVersion)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
