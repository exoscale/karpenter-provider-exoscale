package nodeclass

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/template"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcileTemplate(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = apiv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name                     string
		nodeClass                *apiv1.ExoscaleNodeClass
		templateResolverFunc     func(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) (*template.Template, error)
		exoscaleClientGetTplFunc func(ctx context.Context, id egov3.UUID) (*egov3.Template, error)
		wantErr                  bool
		errContains              string
		expectedImageID          string
	}{
		{
			name: "successfully resolves template by ID",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					TemplateID: "550e8400-e29b-41d4-a716-446655440000",
				},
			},
			templateResolverFunc: func(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) (*template.Template, error) {
				return &template.Template{
					ID: "550e8400-e29b-41d4-a716-446655440000",
					Labels: map[string]string{
						corev1.LabelOSStable: "linux",
					},
				}, nil
			},
			exoscaleClientGetTplFunc: func(ctx context.Context, id egov3.UUID) (*egov3.Template, error) {
				return &egov3.Template{
					ID: egov3.UUID("550e8400-e29b-41d4-a716-446655440000"),
				}, nil
			},
			wantErr:         false,
			expectedImageID: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name: "successfully resolves template with image selector",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					ImageTemplateSelector: &apiv1.ImageTemplateSelector{
						Version: "1.28.0",
						Variant: "standard",
					},
				},
			},
			templateResolverFunc: func(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) (*template.Template, error) {
				return &template.Template{
					ID: "660e8400-e29b-41d4-a716-446655440001",
					Labels: map[string]string{
						corev1.LabelOSStable: "linux",
					},
				}, nil
			},
			exoscaleClientGetTplFunc: func(ctx context.Context, id egov3.UUID) (*egov3.Template, error) {
				return &egov3.Template{
					ID: egov3.UUID("660e8400-e29b-41d4-a716-446655440001"),
				}, nil
			},
			wantErr:         false,
			expectedImageID: "660e8400-e29b-41d4-a716-446655440001",
		},
		{
			name: "fails when template resolver returns error",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					ImageTemplateSelector: &apiv1.ImageTemplateSelector{
						Version: "invalid",
					},
				},
			},
			templateResolverFunc: func(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) (*template.Template, error) {
				return nil, errors.New("template resolution failed")
			},
			exoscaleClientGetTplFunc: func(ctx context.Context, id egov3.UUID) (*egov3.Template, error) {
				t.Error("GetTemplate should not be called when resolver fails")
				return nil, nil
			},
			wantErr:         true,
			errContains:     "failed to resolve template ID",
			expectedImageID: "",
		},
		{
			name: "fails when template not found in exoscale",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					TemplateID: "770e8400-e29b-41d4-a716-446655440002",
				},
			},
			templateResolverFunc: func(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) (*template.Template, error) {
				return &template.Template{
					ID: "770e8400-e29b-41d4-a716-446655440002",
					Labels: map[string]string{
						corev1.LabelOSStable: "linux",
					},
				}, nil
			},
			exoscaleClientGetTplFunc: func(ctx context.Context, id egov3.UUID) (*egov3.Template, error) {
				return nil, errors.New("template not found")
			},
			wantErr:         true,
			errContains:     "not found or not accessible",
			expectedImageID: "",
		},
		{
			name: "fails when exoscale client returns access error",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					TemplateID: "880e8400-e29b-41d4-a716-446655440003",
				},
			},
			templateResolverFunc: func(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) (*template.Template, error) {
				return &template.Template{
					ID: "880e8400-e29b-41d4-a716-446655440003",
					Labels: map[string]string{
						corev1.LabelOSStable: "linux",
					},
				}, nil
			},
			exoscaleClientGetTplFunc: func(ctx context.Context, id egov3.UUID) (*egov3.Template, error) {
				return nil, errors.New("access denied")
			},
			wantErr:         true,
			errContains:     "not found or not accessible",
			expectedImageID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.nodeClass).
				Build()

			// Create mock template resolver
			mockResolver := &template.MockResolver{
				ResolveFunc: tt.templateResolverFunc,
			}

			// Create mock Exoscale client
			mockExoClient := &providers.MockClient{
				GetTemplateFunc: tt.exoscaleClientGetTplFunc,
			}

			// Create reconciler
			reconciler := &ExoscaleNodeClassReconciler{
				Client:           fakeClient,
				Scheme:           scheme,
				ExoscaleClient:   mockExoClient,
				TemplateResolver: mockResolver,
				Recorder:         events.NewFakeRecorder(10),
				Zone:             "ch-gva-2",
			}

			ctx := context.Background()
			err := reconciler.reconcileTemplate(ctx, tt.nodeClass)

			if tt.wantErr {
				if err == nil {
					t.Errorf("reconcileTemplate() expected error but got none")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("reconcileTemplate() error = %v, want error containing %v", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("reconcileTemplate() unexpected error = %v", err)
				}
			}

			if got := tt.nodeClass.Status.ImageID; got != tt.expectedImageID {
				t.Errorf("reconcileTemplate() Status.ImageID = %q, want %q", got, tt.expectedImageID)
			}
		})
	}
}

func TestReconcileSecurityGroups(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = apiv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name                      string
		nodeClass                 *apiv1.ExoscaleNodeClass
		exoscaleClientGetSGFunc   func(ctx context.Context, id egov3.UUID) (*egov3.SecurityGroup, error)
		exoscaleClientListSGsFunc func(ctx context.Context, opts ...egov3.ListSecurityGroupsOpt) (*egov3.ListSecurityGroupsResponse, error)
		wantErr                   bool
		errContains               string
		expectedSecurityGroupIDs  []string
	}{
		{
			name: "successfully resolves security groups by ID (deprecated field)",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					SecurityGroups: []string{"550e8400-e29b-41d4-a716-446655440010"},
				},
			},
			exoscaleClientGetSGFunc: func(ctx context.Context, id egov3.UUID) (*egov3.SecurityGroup, error) {
				return &egov3.SecurityGroup{
					ID:   egov3.UUID("550e8400-e29b-41d4-a716-446655440010"),
					Name: "test-sg",
				}, nil
			},
			exoscaleClientListSGsFunc: func(ctx context.Context, opts ...egov3.ListSecurityGroupsOpt) (*egov3.ListSecurityGroupsResponse, error) {
				return &egov3.ListSecurityGroupsResponse{
					SecurityGroups: []egov3.SecurityGroup{},
				}, nil
			},
			wantErr:                  false,
			expectedSecurityGroupIDs: []string{"550e8400-e29b-41d4-a716-446655440010"},
		},
		{
			name: "successfully resolves security groups by ID selector",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					SecurityGroupSelectorTerms: []apiv1.SelectorTerms{
						{ID: "660e8400-e29b-41d4-a716-446655440011"},
					},
				},
			},
			exoscaleClientGetSGFunc: func(ctx context.Context, id egov3.UUID) (*egov3.SecurityGroup, error) {
				return &egov3.SecurityGroup{
					ID:   egov3.UUID("660e8400-e29b-41d4-a716-446655440011"),
					Name: "test-sg-selector",
				}, nil
			},
			exoscaleClientListSGsFunc: func(ctx context.Context, opts ...egov3.ListSecurityGroupsOpt) (*egov3.ListSecurityGroupsResponse, error) {
				return &egov3.ListSecurityGroupsResponse{
					SecurityGroups: []egov3.SecurityGroup{},
				}, nil
			},
			wantErr:                  false,
			expectedSecurityGroupIDs: []string{"660e8400-e29b-41d4-a716-446655440011"},
		},
		{
			name: "successfully resolves security groups by Name selector",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					SecurityGroupSelectorTerms: []apiv1.SelectorTerms{
						{Name: "my-security-group"},
					},
				},
			},
			exoscaleClientGetSGFunc: func(ctx context.Context, id egov3.UUID) (*egov3.SecurityGroup, error) {
				t.Error("GetSecurityGroup should not be called when using Name selector")
				return nil, nil
			},
			exoscaleClientListSGsFunc: func(ctx context.Context, opts ...egov3.ListSecurityGroupsOpt) (*egov3.ListSecurityGroupsResponse, error) {
				return &egov3.ListSecurityGroupsResponse{
					SecurityGroups: []egov3.SecurityGroup{
						{
							ID:   egov3.UUID("770e8400-e29b-41d4-a716-446655440012"),
							Name: "my-security-group",
						},
						{
							ID:   egov3.UUID("880e8400-e29b-41d4-a716-446655440013"),
							Name: "other-security-group",
						},
					},
				}, nil
			},
			wantErr:                  false,
			expectedSecurityGroupIDs: []string{"770e8400-e29b-41d4-a716-446655440012"},
		},
		{
			name: "fails when security group not found by ID",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					SecurityGroups: []string{"990e8400-e29b-41d4-a716-446655440014"},
				},
			},
			exoscaleClientGetSGFunc: func(ctx context.Context, id egov3.UUID) (*egov3.SecurityGroup, error) {
				return nil, errors.New("security group not found")
			},
			exoscaleClientListSGsFunc: func(ctx context.Context, opts ...egov3.ListSecurityGroupsOpt) (*egov3.ListSecurityGroupsResponse, error) {
				return &egov3.ListSecurityGroupsResponse{
					SecurityGroups: []egov3.SecurityGroup{},
				}, nil
			},
			wantErr:     true,
			errContains: "failed to get security group",
		},
		{
			name: "fails when security group not found by Name",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					SecurityGroupSelectorTerms: []apiv1.SelectorTerms{
						{Name: "non-existent-sg"},
					},
				},
			},
			exoscaleClientGetSGFunc: func(ctx context.Context, id egov3.UUID) (*egov3.SecurityGroup, error) {
				t.Error("GetSecurityGroup should not be called when using Name selector")
				return nil, nil
			},
			exoscaleClientListSGsFunc: func(ctx context.Context, opts ...egov3.ListSecurityGroupsOpt) (*egov3.ListSecurityGroupsResponse, error) {
				return &egov3.ListSecurityGroupsResponse{
					SecurityGroups: []egov3.SecurityGroup{
						{
							ID:   egov3.UUID("aa0e8400-e29b-41d4-a716-446655440015"),
							Name: "other-sg",
						},
					},
				}, nil
			},
			wantErr:     true,
			errContains: "security group with name non-existent-sg not found",
		},
		{
			name: "successfully resolves multiple security groups with mixed selectors",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					SecurityGroups: []string{"bb0e8400-e29b-41d4-a716-446655440016"},
					SecurityGroupSelectorTerms: []apiv1.SelectorTerms{
						{ID: "cc0e8400-e29b-41d4-a716-446655440017"},
						{Name: "named-sg"},
					},
				},
			},
			exoscaleClientGetSGFunc: func(ctx context.Context, id egov3.UUID) (*egov3.SecurityGroup, error) {
				idStr := id.String()
				switch idStr {
				case "bb0e8400-e29b-41d4-a716-446655440016":
					return &egov3.SecurityGroup{
						ID:   egov3.UUID("bb0e8400-e29b-41d4-a716-446655440016"),
						Name: "deprecated-sg",
					}, nil
				case "cc0e8400-e29b-41d4-a716-446655440017":
					return &egov3.SecurityGroup{
						ID:   egov3.UUID("cc0e8400-e29b-41d4-a716-446655440017"),
						Name: "id-selector-sg",
					}, nil
				default:
					t.Errorf("unexpected security group ID: %s", idStr)
					return nil, errors.New("unexpected ID")
				}
			},
			exoscaleClientListSGsFunc: func(ctx context.Context, opts ...egov3.ListSecurityGroupsOpt) (*egov3.ListSecurityGroupsResponse, error) {
				return &egov3.ListSecurityGroupsResponse{
					SecurityGroups: []egov3.SecurityGroup{
						{
							ID:   egov3.UUID("dd0e8400-e29b-41d4-a716-446655440018"),
							Name: "named-sg",
						},
					},
				}, nil
			},
			wantErr: false,
			expectedSecurityGroupIDs: []string{
				"bb0e8400-e29b-41d4-a716-446655440016",
				"cc0e8400-e29b-41d4-a716-446655440017",
				"dd0e8400-e29b-41d4-a716-446655440018",
			},
		},
		{
			name: "fails when ListSecurityGroups returns error",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					SecurityGroupSelectorTerms: []apiv1.SelectorTerms{
						{Name: "some-sg"},
					},
				},
			},
			exoscaleClientGetSGFunc: func(ctx context.Context, id egov3.UUID) (*egov3.SecurityGroup, error) {
				t.Error("GetSecurityGroup should not be called when using Name selector")
				return nil, nil
			},
			exoscaleClientListSGsFunc: func(ctx context.Context, opts ...egov3.ListSecurityGroupsOpt) (*egov3.ListSecurityGroupsResponse, error) {
				return nil, errors.New("API error")
			},
			wantErr:     true,
			errContains: "failed to get security group by name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.nodeClass).
				Build()

			// Create mock Exoscale client
			mockExoClient := &providers.MockClient{
				GetSecurityGroupFunc:   tt.exoscaleClientGetSGFunc,
				ListSecurityGroupsFunc: tt.exoscaleClientListSGsFunc,
			}

			// Create reconciler
			reconciler := &ExoscaleNodeClassReconciler{
				Client:         fakeClient,
				Scheme:         scheme,
				ExoscaleClient: mockExoClient,
				Recorder:       events.NewFakeRecorder(10),
				Zone:           "ch-gva-2",
			}

			ctx := context.Background()
			err := reconciler.reconcileSecurityGroups(ctx, tt.nodeClass)

			if tt.wantErr {
				if err == nil {
					t.Errorf("reconcileSecurityGroups() expected error but got none")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("reconcileSecurityGroups() error = %v, want error containing %v", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("reconcileSecurityGroups() unexpected error = %v", err)
					return
				}

				// Verify the security group IDs in status
				if len(tt.expectedSecurityGroupIDs) != len(tt.nodeClass.Status.SecurityGroups) {
					t.Errorf("reconcileSecurityGroups() got %d security groups, want %d",
						len(tt.nodeClass.Status.SecurityGroups), len(tt.expectedSecurityGroupIDs))
					return
				}

				for i, expectedID := range tt.expectedSecurityGroupIDs {
					if tt.nodeClass.Status.SecurityGroups[i] != expectedID {
						t.Errorf("reconcileSecurityGroups() security group[%d] = %v, want %v",
							i, tt.nodeClass.Status.SecurityGroups[i], expectedID)
					}
				}
			}
		})
	}
}

func TestReconcileAntiAffinityGroups(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = apiv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name                         string
		nodeClass                    *apiv1.ExoscaleNodeClass
		exoscaleClientGetAAGFunc     func(ctx context.Context, id egov3.UUID) (*egov3.AntiAffinityGroup, error)
		exoscaleClientListAAGsFunc   func(ctx context.Context) (*egov3.ListAntiAffinityGroupsResponse, error)
		wantErr                      bool
		errContains                  string
		expectedAntiAffinityGroupIDs []string
	}{
		{
			name: "successfully resolves anti-affinity groups by ID (deprecated field)",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					AntiAffinityGroups: []string{"aa0e8400-e29b-41d4-a716-446655440020"},
				},
			},
			exoscaleClientGetAAGFunc: func(ctx context.Context, id egov3.UUID) (*egov3.AntiAffinityGroup, error) {
				if id.String() != "aa0e8400-e29b-41d4-a716-446655440020" {
					t.Errorf("expected anti-affinity group ID 'aa0e8400-e29b-41d4-a716-446655440020', got '%s'", id.String())
				}
				return &egov3.AntiAffinityGroup{
					ID:   egov3.UUID("aa0e8400-e29b-41d4-a716-446655440020"),
					Name: "test-aag",
				}, nil
			},
			exoscaleClientListAAGsFunc: func(ctx context.Context) (*egov3.ListAntiAffinityGroupsResponse, error) {
				return &egov3.ListAntiAffinityGroupsResponse{
					AntiAffinityGroups: []egov3.AntiAffinityGroup{},
				}, nil
			},
			wantErr:                      false,
			expectedAntiAffinityGroupIDs: []string{"aa0e8400-e29b-41d4-a716-446655440020"},
		},
		{
			name: "successfully resolves anti-affinity groups by ID selector",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					AntiAffinityGroupSelectorTerms: []apiv1.SelectorTerms{
						{ID: "bb0e8400-e29b-41d4-a716-446655440021"},
					},
				},
			},
			exoscaleClientGetAAGFunc: func(ctx context.Context, id egov3.UUID) (*egov3.AntiAffinityGroup, error) {
				if id.String() != "bb0e8400-e29b-41d4-a716-446655440021" {
					t.Errorf("expected anti-affinity group ID 'bb0e8400-e29b-41d4-a716-446655440021', got '%s'", id.String())
				}
				return &egov3.AntiAffinityGroup{
					ID:   egov3.UUID("bb0e8400-e29b-41d4-a716-446655440021"),
					Name: "test-aag-selector",
				}, nil
			},
			exoscaleClientListAAGsFunc: func(ctx context.Context) (*egov3.ListAntiAffinityGroupsResponse, error) {
				return &egov3.ListAntiAffinityGroupsResponse{
					AntiAffinityGroups: []egov3.AntiAffinityGroup{},
				}, nil
			},
			wantErr:                      false,
			expectedAntiAffinityGroupIDs: []string{"bb0e8400-e29b-41d4-a716-446655440021"},
		},
		{
			name: "successfully resolves anti-affinity groups by Name selector",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					AntiAffinityGroupSelectorTerms: []apiv1.SelectorTerms{
						{Name: "my-anti-affinity-group"},
					},
				},
			},
			exoscaleClientGetAAGFunc: func(ctx context.Context, id egov3.UUID) (*egov3.AntiAffinityGroup, error) {
				t.Error("GetAntiAffinityGroup should not be called when using Name selector")
				return nil, nil
			},
			exoscaleClientListAAGsFunc: func(ctx context.Context) (*egov3.ListAntiAffinityGroupsResponse, error) {
				return &egov3.ListAntiAffinityGroupsResponse{
					AntiAffinityGroups: []egov3.AntiAffinityGroup{
						{
							ID:   egov3.UUID("cc0e8400-e29b-41d4-a716-446655440022"),
							Name: "my-anti-affinity-group",
						},
						{
							ID:   egov3.UUID("dd0e8400-e29b-41d4-a716-446655440023"),
							Name: "other-anti-affinity-group",
						},
					},
				}, nil
			},
			wantErr:                      false,
			expectedAntiAffinityGroupIDs: []string{"cc0e8400-e29b-41d4-a716-446655440022"},
		},
		{
			name: "fails when anti-affinity group not found by ID",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					AntiAffinityGroups: []string{"ee0e8400-e29b-41d4-a716-446655440024"},
				},
			},
			exoscaleClientGetAAGFunc: func(ctx context.Context, id egov3.UUID) (*egov3.AntiAffinityGroup, error) {
				return nil, errors.New("anti-affinity group not found")
			},
			exoscaleClientListAAGsFunc: func(ctx context.Context) (*egov3.ListAntiAffinityGroupsResponse, error) {
				return &egov3.ListAntiAffinityGroupsResponse{
					AntiAffinityGroups: []egov3.AntiAffinityGroup{},
				}, nil
			},
			wantErr:     true,
			errContains: "failed to get anti-affinity group",
		},
		{
			name: "fails when anti-affinity group not found by Name",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					AntiAffinityGroupSelectorTerms: []apiv1.SelectorTerms{
						{Name: "non-existent-aag"},
					},
				},
			},
			exoscaleClientGetAAGFunc: func(ctx context.Context, id egov3.UUID) (*egov3.AntiAffinityGroup, error) {
				t.Error("GetAntiAffinityGroup should not be called when using Name selector")
				return nil, nil
			},
			exoscaleClientListAAGsFunc: func(ctx context.Context) (*egov3.ListAntiAffinityGroupsResponse, error) {
				return &egov3.ListAntiAffinityGroupsResponse{
					AntiAffinityGroups: []egov3.AntiAffinityGroup{
						{
							ID:   egov3.UUID("ff0e8400-e29b-41d4-a716-446655440025"),
							Name: "other-aag",
						},
					},
				}, nil
			},
			wantErr:     true,
			errContains: "anti-affinity group with name non-existent-aag not found",
		},
		{
			name: "successfully resolves multiple anti-affinity groups with mixed selectors",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					AntiAffinityGroups: []string{"110e8400-e29b-41d4-a716-446655440026"},
					AntiAffinityGroupSelectorTerms: []apiv1.SelectorTerms{
						{ID: "220e8400-e29b-41d4-a716-446655440027"},
						{Name: "named-aag"},
					},
				},
			},
			exoscaleClientGetAAGFunc: func(ctx context.Context, id egov3.UUID) (*egov3.AntiAffinityGroup, error) {
				idStr := id.String()
				switch idStr {
				case "110e8400-e29b-41d4-a716-446655440026":
					return &egov3.AntiAffinityGroup{
						ID:   egov3.UUID("110e8400-e29b-41d4-a716-446655440026"),
						Name: "deprecated-aag",
					}, nil
				case "220e8400-e29b-41d4-a716-446655440027":
					return &egov3.AntiAffinityGroup{
						ID:   egov3.UUID("220e8400-e29b-41d4-a716-446655440027"),
						Name: "id-selector-aag",
					}, nil
				default:
					t.Errorf("unexpected anti-affinity group ID: %s", idStr)
					return nil, errors.New("unexpected ID")
				}
			},
			exoscaleClientListAAGsFunc: func(ctx context.Context) (*egov3.ListAntiAffinityGroupsResponse, error) {
				return &egov3.ListAntiAffinityGroupsResponse{
					AntiAffinityGroups: []egov3.AntiAffinityGroup{
						{
							ID:   egov3.UUID("330e8400-e29b-41d4-a716-446655440028"),
							Name: "named-aag",
						},
					},
				}, nil
			},
			wantErr: false,
			expectedAntiAffinityGroupIDs: []string{
				"110e8400-e29b-41d4-a716-446655440026",
				"220e8400-e29b-41d4-a716-446655440027",
				"330e8400-e29b-41d4-a716-446655440028",
			},
		},
		{
			name: "fails when ListAntiAffinityGroups returns error",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					AntiAffinityGroupSelectorTerms: []apiv1.SelectorTerms{
						{Name: "some-aag"},
					},
				},
			},
			exoscaleClientGetAAGFunc: func(ctx context.Context, id egov3.UUID) (*egov3.AntiAffinityGroup, error) {
				t.Error("GetAntiAffinityGroup should not be called when using Name selector")
				return nil, nil
			},
			exoscaleClientListAAGsFunc: func(ctx context.Context) (*egov3.ListAntiAffinityGroupsResponse, error) {
				return nil, errors.New("API error")
			},
			wantErr:     true,
			errContains: "failed to get anti-affinity group by name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.nodeClass).
				Build()

			// Create mock Exoscale client
			mockExoClient := &providers.MockClient{
				GetAntiAffinityGroupFunc:   tt.exoscaleClientGetAAGFunc,
				ListAntiAffinityGroupsFunc: tt.exoscaleClientListAAGsFunc,
			}

			// Create reconciler
			reconciler := &ExoscaleNodeClassReconciler{
				Client:         fakeClient,
				Scheme:         scheme,
				ExoscaleClient: mockExoClient,
				Recorder:       events.NewFakeRecorder(10),
				Zone:           "ch-gva-2",
			}

			ctx := context.Background()
			err := reconciler.reconcileAntiAffinityGroups(ctx, tt.nodeClass)

			if tt.wantErr {
				if err == nil {
					t.Errorf("reconcileAntiAffinityGroups() expected error but got none")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("reconcileAntiAffinityGroups() error = %v, want error containing %v", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("reconcileAntiAffinityGroups() unexpected error = %v", err)
					return
				}

				// Verify the anti-affinity group IDs in status
				if len(tt.expectedAntiAffinityGroupIDs) != len(tt.nodeClass.Status.AntiAffinityGroups) {
					t.Errorf("reconcileAntiAffinityGroups() got %d anti-affinity groups, want %d",
						len(tt.nodeClass.Status.AntiAffinityGroups), len(tt.expectedAntiAffinityGroupIDs))
					return
				}

				for i, expectedID := range tt.expectedAntiAffinityGroupIDs {
					if tt.nodeClass.Status.AntiAffinityGroups[i] != expectedID {
						t.Errorf("reconcileAntiAffinityGroups() anti-affinity group[%d] = %v, want %v",
							i, tt.nodeClass.Status.AntiAffinityGroups[i], expectedID)
					}
				}
			}
		})
	}
}

func TestReconcilePrivateNetworks(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = apiv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name                      string
		nodeClass                 *apiv1.ExoscaleNodeClass
		exoscaleClientGetPNFunc   func(ctx context.Context, id egov3.UUID) (*egov3.PrivateNetwork, error)
		exoscaleClientListPNsFunc func(ctx context.Context) (*egov3.ListPrivateNetworksResponse, error)
		wantErr                   bool
		errContains               string
		expectedPrivateNetworkIDs []string
	}{
		{
			name: "successfully resolves private networks by ID (deprecated field)",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					PrivateNetworks: []string{"440e8400-e29b-41d4-a716-446655440030"},
				},
			},
			exoscaleClientGetPNFunc: func(ctx context.Context, id egov3.UUID) (*egov3.PrivateNetwork, error) {
				return &egov3.PrivateNetwork{
					ID:   egov3.UUID("440e8400-e29b-41d4-a716-446655440030"),
					Name: "test-pn",
				}, nil
			},
			exoscaleClientListPNsFunc: func(ctx context.Context) (*egov3.ListPrivateNetworksResponse, error) {
				return &egov3.ListPrivateNetworksResponse{
					PrivateNetworks: []egov3.PrivateNetwork{},
				}, nil
			},
			wantErr:                   false,
			expectedPrivateNetworkIDs: []string{"440e8400-e29b-41d4-a716-446655440030"},
		},
		{
			name: "successfully resolves private networks by ID selector",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					PrivateNetworkSelectorTerms: []apiv1.SelectorTerms{
						{ID: "550e8400-e29b-41d4-a716-446655440031"},
					},
				},
			},
			exoscaleClientGetPNFunc: func(ctx context.Context, id egov3.UUID) (*egov3.PrivateNetwork, error) {
				return &egov3.PrivateNetwork{
					ID:   egov3.UUID("550e8400-e29b-41d4-a716-446655440031"),
					Name: "test-pn-selector",
				}, nil
			},
			exoscaleClientListPNsFunc: func(ctx context.Context) (*egov3.ListPrivateNetworksResponse, error) {
				return &egov3.ListPrivateNetworksResponse{
					PrivateNetworks: []egov3.PrivateNetwork{},
				}, nil
			},
			wantErr:                   false,
			expectedPrivateNetworkIDs: []string{"550e8400-e29b-41d4-a716-446655440031"},
		},
		{
			name: "successfully resolves private networks by Name selector",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					PrivateNetworkSelectorTerms: []apiv1.SelectorTerms{
						{Name: "my-private-network"},
					},
				},
			},
			exoscaleClientGetPNFunc: func(ctx context.Context, id egov3.UUID) (*egov3.PrivateNetwork, error) {
				t.Error("GetPrivateNetwork should not be called when using Name selector")
				return nil, nil
			},
			exoscaleClientListPNsFunc: func(ctx context.Context) (*egov3.ListPrivateNetworksResponse, error) {
				return &egov3.ListPrivateNetworksResponse{
					PrivateNetworks: []egov3.PrivateNetwork{
						{
							ID:   egov3.UUID("660e8400-e29b-41d4-a716-446655440032"),
							Name: "my-private-network",
						},
						{
							ID:   egov3.UUID("770e8400-e29b-41d4-a716-446655440033"),
							Name: "other-private-network",
						},
					},
				}, nil
			},
			wantErr:                   false,
			expectedPrivateNetworkIDs: []string{"660e8400-e29b-41d4-a716-446655440032"},
		},
		{
			name: "fails when private network not found by ID",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					PrivateNetworks: []string{"880e8400-e29b-41d4-a716-446655440034"},
				},
			},
			exoscaleClientGetPNFunc: func(ctx context.Context, id egov3.UUID) (*egov3.PrivateNetwork, error) {
				return nil, errors.New("private network not found")
			},
			exoscaleClientListPNsFunc: func(ctx context.Context) (*egov3.ListPrivateNetworksResponse, error) {
				return &egov3.ListPrivateNetworksResponse{
					PrivateNetworks: []egov3.PrivateNetwork{},
				}, nil
			},
			wantErr:     true,
			errContains: "failed to get private network",
		},
		{
			name: "fails when private network not found by Name",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					PrivateNetworkSelectorTerms: []apiv1.SelectorTerms{
						{Name: "non-existent-pn"},
					},
				},
			},
			exoscaleClientGetPNFunc: func(ctx context.Context, id egov3.UUID) (*egov3.PrivateNetwork, error) {
				t.Error("GetPrivateNetwork should not be called when using Name selector")
				return nil, nil
			},
			exoscaleClientListPNsFunc: func(ctx context.Context) (*egov3.ListPrivateNetworksResponse, error) {
				return &egov3.ListPrivateNetworksResponse{
					PrivateNetworks: []egov3.PrivateNetwork{
						{
							ID:   egov3.UUID("990e8400-e29b-41d4-a716-446655440035"),
							Name: "other-pn",
						},
					},
				}, nil
			},
			wantErr:     true,
			errContains: "private network with name non-existent-pn not found",
		},
		{
			name: "successfully resolves multiple private networks with mixed selectors",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					PrivateNetworks: []string{"aa0e8400-e29b-41d4-a716-446655440036"},
					PrivateNetworkSelectorTerms: []apiv1.SelectorTerms{
						{ID: "bb0e8400-e29b-41d4-a716-446655440037"},
						{Name: "named-pn"},
					},
				},
			},
			exoscaleClientGetPNFunc: func(ctx context.Context, id egov3.UUID) (*egov3.PrivateNetwork, error) {
				idStr := id.String()
				switch idStr {
				case "aa0e8400-e29b-41d4-a716-446655440036":
					return &egov3.PrivateNetwork{
						ID:   egov3.UUID("aa0e8400-e29b-41d4-a716-446655440036"),
						Name: "deprecated-pn",
					}, nil
				case "bb0e8400-e29b-41d4-a716-446655440037":
					return &egov3.PrivateNetwork{
						ID:   egov3.UUID("bb0e8400-e29b-41d4-a716-446655440037"),
						Name: "id-selector-pn",
					}, nil
				default:
					t.Errorf("unexpected private network ID: %s", idStr)
					return nil, errors.New("unexpected ID")
				}
			},
			exoscaleClientListPNsFunc: func(ctx context.Context) (*egov3.ListPrivateNetworksResponse, error) {
				return &egov3.ListPrivateNetworksResponse{
					PrivateNetworks: []egov3.PrivateNetwork{
						{
							ID:   egov3.UUID("cc0e8400-e29b-41d4-a716-446655440038"),
							Name: "named-pn",
						},
					},
				}, nil
			},
			wantErr: false,
			expectedPrivateNetworkIDs: []string{
				"aa0e8400-e29b-41d4-a716-446655440036",
				"bb0e8400-e29b-41d4-a716-446655440037",
				"cc0e8400-e29b-41d4-a716-446655440038",
			},
		},
		{
			name: "fails when ListPrivateNetworks returns error",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					PrivateNetworkSelectorTerms: []apiv1.SelectorTerms{
						{Name: "some-pn"},
					},
				},
			},
			exoscaleClientGetPNFunc: func(ctx context.Context, id egov3.UUID) (*egov3.PrivateNetwork, error) {
				t.Error("GetPrivateNetwork should not be called when using Name selector")
				return nil, nil
			},
			exoscaleClientListPNsFunc: func(ctx context.Context) (*egov3.ListPrivateNetworksResponse, error) {
				return nil, errors.New("API error")
			},
			wantErr:     true,
			errContains: "failed to get private network by name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.nodeClass).
				Build()

			// Create mock Exoscale client
			mockExoClient := &providers.MockClient{
				GetPrivateNetworkFunc:   tt.exoscaleClientGetPNFunc,
				ListPrivateNetworksFunc: tt.exoscaleClientListPNsFunc,
			}

			// Create reconciler
			reconciler := &ExoscaleNodeClassReconciler{
				Client:         fakeClient,
				Scheme:         scheme,
				ExoscaleClient: mockExoClient,
				Recorder:       events.NewFakeRecorder(10),
				Zone:           "ch-gva-2",
			}

			ctx := context.Background()
			err := reconciler.reconcilePrivateNetworks(ctx, tt.nodeClass)

			if tt.wantErr {
				if err == nil {
					t.Errorf("reconcilePrivateNetworks() expected error but got none")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("reconcilePrivateNetworks() error = %v, want error containing %v", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("reconcilePrivateNetworks() unexpected error = %v", err)
					return
				}

				// Verify the private network IDs in status
				if len(tt.expectedPrivateNetworkIDs) != len(tt.nodeClass.Status.PrivateNetworks) {
					t.Errorf("reconcilePrivateNetworks() got %d private networks, want %d",
						len(tt.nodeClass.Status.PrivateNetworks), len(tt.expectedPrivateNetworkIDs))
					return
				}

				for i, expectedID := range tt.expectedPrivateNetworkIDs {
					if tt.nodeClass.Status.PrivateNetworks[i] != expectedID {
						t.Errorf("reconcilePrivateNetworks() private network[%d] = %v, want %v",
							i, tt.nodeClass.Status.PrivateNetworks[i], expectedID)
					}
				}
			}
		})
	}
}

func TestReconcileVpcSubnets(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = apiv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	const (
		vpcA = "11111111-0000-0000-0000-000000000001"
		vpcB = "22222222-0000-0000-0000-000000000002"
	)

	tests := []struct {
		name                      string
		nodeClass                 *apiv1.ExoscaleNodeClass
		exoscaleClientGetVpc      func(ctx context.Context, id egov3.UUID) (*egov3.Vpc, error)
		exoscaleClientListVpcs    func(ctx context.Context) (*egov3.ListVpcsResponse, error)
		exoscaleClientListSubnets func(ctx context.Context, vpcID egov3.UUID) (*egov3.ListSubnetsResponse, error)
		exoscaleClientGetSubnet   func(ctx context.Context, vpcID, id egov3.UUID) (*egov3.Subnet, error)
		wantErr                   bool
		errContains               string
		expectedSubnetIDs         []string
		expectedVpcID             string
	}{
		{
			name: "resolves subnets by ID within VPC selected by ID",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
				Spec: apiv1.ExoscaleNodeClassSpec{
					VpcSelector: &apiv1.SelectorTerms{ID: vpcA},
					VpcSubnetSelectorTerms: []apiv1.SelectorTerms{
						{ID: "440e8400-e29b-41d4-a716-446655440030"},
					},
				},
			},
			exoscaleClientGetVpc: func(ctx context.Context, id egov3.UUID) (*egov3.Vpc, error) {
				if id.String() != vpcA {
					t.Errorf("unexpected vpc ID: %s", id)
				}
				return &egov3.Vpc{ID: egov3.UUID(vpcA), Name: "vpc-a"}, nil
			},
			exoscaleClientListVpcs: func(ctx context.Context) (*egov3.ListVpcsResponse, error) {
				t.Error("ListVpcs should not be called when vpcSelector uses ID")
				return nil, errors.New("should not be called")
			},
			exoscaleClientListSubnets: func(ctx context.Context, vpcID egov3.UUID) (*egov3.ListSubnetsResponse, error) {
				t.Error("ListSubnets should not be called when subnets are selected by ID")
				return nil, errors.New("should not be called")
			},
			exoscaleClientGetSubnet: func(ctx context.Context, vpcID, id egov3.UUID) (*egov3.Subnet, error) {
				if vpcID.String() != vpcA {
					t.Errorf("GetSubnet called with wrong vpc: %s", vpcID)
				}
				return &egov3.Subnet{ID: id, Name: "subnet-1"}, nil
			},
			wantErr:           false,
			expectedSubnetIDs: []string{"440e8400-e29b-41d4-a716-446655440030"},
			expectedVpcID:     vpcA,
		},
		{
			name: "resolves subnets by name within VPC selected by name",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
				Spec: apiv1.ExoscaleNodeClassSpec{
					VpcSelector: &apiv1.SelectorTerms{Name: "vpc-a"},
					VpcSubnetSelectorTerms: []apiv1.SelectorTerms{
						{Name: "my-subnet"},
					},
				},
			},
			exoscaleClientGetVpc: func(ctx context.Context, id egov3.UUID) (*egov3.Vpc, error) {
				return &egov3.Vpc{ID: id, Name: "vpc-a"}, nil
			},
			exoscaleClientListVpcs: func(ctx context.Context) (*egov3.ListVpcsResponse, error) {
				return &egov3.ListVpcsResponse{
					Vpcs: []egov3.ListVpcEntry{
						{ID: egov3.UUID(vpcA), Name: "vpc-a"},
						{ID: egov3.UUID(vpcB), Name: "vpc-b"},
					},
				}, nil
			},
			exoscaleClientListSubnets: func(ctx context.Context, vpcID egov3.UUID) (*egov3.ListSubnetsResponse, error) {
				if vpcID.String() != vpcA {
					t.Errorf("ListSubnets called with wrong vpc: %s (subnets must only be looked up in the selected VPC)", vpcID)
				}
				return &egov3.ListSubnetsResponse{
					Subnets: []egov3.ListSubnetEntry{
						{ID: egov3.UUID("660e8400-e29b-41d4-a716-446655440032"), Name: "my-subnet"},
						{ID: egov3.UUID("770e8400-e29b-41d4-a716-446655440033"), Name: "other-subnet"},
					},
				}, nil
			},
			exoscaleClientGetSubnet: func(ctx context.Context, vpcID, id egov3.UUID) (*egov3.Subnet, error) {
				return &egov3.Subnet{ID: id, Name: "my-subnet"}, nil
			},
			wantErr:           false,
			expectedSubnetIDs: []string{"660e8400-e29b-41d4-a716-446655440032"},
			expectedVpcID:     vpcA,
		},
		{
			name: "resolves multiple subnets with mixed selectors within the selected VPC",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
				Spec: apiv1.ExoscaleNodeClassSpec{
					VpcSelector: &apiv1.SelectorTerms{Name: "vpc-a"},
					VpcSubnetSelectorTerms: []apiv1.SelectorTerms{
						{ID: "aa0e8400-e29b-41d4-a716-446655440036"},
						{Name: "named-subnet"},
					},
				},
			},
			exoscaleClientGetVpc: func(ctx context.Context, id egov3.UUID) (*egov3.Vpc, error) {
				return &egov3.Vpc{ID: id, Name: "vpc-a"}, nil
			},
			exoscaleClientListVpcs: func(ctx context.Context) (*egov3.ListVpcsResponse, error) {
				return &egov3.ListVpcsResponse{
					Vpcs: []egov3.ListVpcEntry{{ID: egov3.UUID(vpcA), Name: "vpc-a"}},
				}, nil
			},
			exoscaleClientListSubnets: func(ctx context.Context, vpcID egov3.UUID) (*egov3.ListSubnetsResponse, error) {
				return &egov3.ListSubnetsResponse{
					Subnets: []egov3.ListSubnetEntry{
						{ID: egov3.UUID("cc0e8400-e29b-41d4-a716-446655440038"), Name: "named-subnet"},
					},
				}, nil
			},
			exoscaleClientGetSubnet: func(ctx context.Context, vpcID, id egov3.UUID) (*egov3.Subnet, error) {
				return &egov3.Subnet{ID: id}, nil
			},
			wantErr: false,
			expectedSubnetIDs: []string{
				"aa0e8400-e29b-41d4-a716-446655440036",
				"cc0e8400-e29b-41d4-a716-446655440038",
			},
			expectedVpcID: vpcA,
		},
		{
			name: "no subnets selected leaves status empty and performs no API calls",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
				Spec:       apiv1.ExoscaleNodeClassSpec{},
			},
			exoscaleClientGetVpc: func(ctx context.Context, id egov3.UUID) (*egov3.Vpc, error) {
				t.Error("GetVpc should not be called when no subnet is selected")
				return nil, errors.New("should not be called")
			},
			exoscaleClientListVpcs: func(ctx context.Context) (*egov3.ListVpcsResponse, error) {
				t.Error("ListVpcs should not be called when no subnet is selected")
				return nil, errors.New("should not be called")
			},
			exoscaleClientListSubnets: func(ctx context.Context, vpcID egov3.UUID) (*egov3.ListSubnetsResponse, error) {
				t.Error("ListSubnets should not be called when no subnet is selected")
				return nil, errors.New("should not be called")
			},
			exoscaleClientGetSubnet: func(ctx context.Context, vpcID, id egov3.UUID) (*egov3.Subnet, error) {
				t.Error("GetSubnet should not be called when no subnet is selected")
				return nil, errors.New("should not be called")
			},
			wantErr:           false,
			expectedSubnetIDs: []string{},
			expectedVpcID:     "",
		},
		{
			name: "fails when vpcSelector is missing but subnets are set",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
				Spec: apiv1.ExoscaleNodeClassSpec{
					VpcSubnetSelectorTerms: []apiv1.SelectorTerms{{ID: "440e8400-e29b-41d4-a716-446655440030"}},
				},
			},
			exoscaleClientGetVpc: func(ctx context.Context, id egov3.UUID) (*egov3.Vpc, error) {
				t.Error("GetVpc should not be called without vpcSelector")
				return nil, errors.New("should not be called")
			},
			exoscaleClientListVpcs: func(ctx context.Context) (*egov3.ListVpcsResponse, error) {
				t.Error("ListVpcs should not be called without vpcSelector")
				return nil, errors.New("should not be called")
			},
			exoscaleClientListSubnets: func(ctx context.Context, vpcID egov3.UUID) (*egov3.ListSubnetsResponse, error) {
				return nil, errors.New("should not be called")
			},
			exoscaleClientGetSubnet: func(ctx context.Context, vpcID, id egov3.UUID) (*egov3.Subnet, error) {
				return nil, errors.New("should not be called")
			},
			wantErr:     true,
			errContains: "vpcSelector is required",
		},
		{
			name: "fails when vpc not found by name",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
				Spec: apiv1.ExoscaleNodeClassSpec{
					VpcSelector:            &apiv1.SelectorTerms{Name: "non-existent-vpc"},
					VpcSubnetSelectorTerms: []apiv1.SelectorTerms{{Name: "some-subnet"}},
				},
			},
			exoscaleClientGetVpc: func(ctx context.Context, id egov3.UUID) (*egov3.Vpc, error) {
				t.Error("GetVpc should not be called when the vpc name is unknown")
				return nil, errors.New("should not be called")
			},
			exoscaleClientListVpcs: func(ctx context.Context) (*egov3.ListVpcsResponse, error) {
				return &egov3.ListVpcsResponse{
					Vpcs: []egov3.ListVpcEntry{{ID: egov3.UUID(vpcA), Name: "vpc-a"}},
				}, nil
			},
			exoscaleClientListSubnets: func(ctx context.Context, vpcID egov3.UUID) (*egov3.ListSubnetsResponse, error) {
				return nil, errors.New("should not be called")
			},
			exoscaleClientGetSubnet: func(ctx context.Context, vpcID, id egov3.UUID) (*egov3.Subnet, error) {
				return nil, errors.New("should not be called")
			},
			wantErr:     true,
			errContains: "vpc with name non-existent-vpc not found",
		},
		{
			name: "fails when GetVpc returns error for ID selector",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
				Spec: apiv1.ExoscaleNodeClassSpec{
					VpcSelector:            &apiv1.SelectorTerms{ID: vpcB},
					VpcSubnetSelectorTerms: []apiv1.SelectorTerms{{ID: "880e8400-e29b-41d4-a716-446655440034"}},
				},
			},
			exoscaleClientGetVpc: func(ctx context.Context, id egov3.UUID) (*egov3.Vpc, error) {
				return nil, errors.New("API error")
			},
			exoscaleClientListVpcs: func(ctx context.Context) (*egov3.ListVpcsResponse, error) {
				return nil, errors.New("should not be called")
			},
			exoscaleClientListSubnets: func(ctx context.Context, vpcID egov3.UUID) (*egov3.ListSubnetsResponse, error) {
				return nil, errors.New("should not be called")
			},
			exoscaleClientGetSubnet: func(ctx context.Context, vpcID, id egov3.UUID) (*egov3.Subnet, error) {
				return nil, errors.New("should not be called")
			},
			wantErr:     true,
			errContains: "failed to get vpc",
		},
		{
			name: "fails when subnet not found by ID in the selected VPC",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
				Spec: apiv1.ExoscaleNodeClassSpec{
					VpcSelector:            &apiv1.SelectorTerms{ID: vpcA},
					VpcSubnetSelectorTerms: []apiv1.SelectorTerms{{ID: "990e8400-e29b-41d4-a716-446655440035"}},
				},
			},
			exoscaleClientGetVpc: func(ctx context.Context, id egov3.UUID) (*egov3.Vpc, error) {
				return &egov3.Vpc{ID: id, Name: "vpc-a"}, nil
			},
			exoscaleClientListVpcs: func(ctx context.Context) (*egov3.ListVpcsResponse, error) {
				return nil, errors.New("should not be called")
			},
			exoscaleClientListSubnets: func(ctx context.Context, vpcID egov3.UUID) (*egov3.ListSubnetsResponse, error) {
				return nil, errors.New("should not be called")
			},
			exoscaleClientGetSubnet: func(ctx context.Context, vpcID, id egov3.UUID) (*egov3.Subnet, error) {
				return nil, errors.New("not found")
			},
			wantErr:     true,
			errContains: "failed to get subnet 990e8400-e29b-41d4-a716-446655440035 in vpc",
		},
		{
			name: "fails when subnet not found by name in the selected VPC",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
				Spec: apiv1.ExoscaleNodeClassSpec{
					VpcSelector:            &apiv1.SelectorTerms{ID: vpcA},
					VpcSubnetSelectorTerms: []apiv1.SelectorTerms{{Name: "non-existent-subnet"}},
				},
			},
			exoscaleClientGetVpc: func(ctx context.Context, id egov3.UUID) (*egov3.Vpc, error) {
				return &egov3.Vpc{ID: id, Name: "vpc-a"}, nil
			},
			exoscaleClientListVpcs: func(ctx context.Context) (*egov3.ListVpcsResponse, error) {
				return nil, errors.New("should not be called")
			},
			exoscaleClientListSubnets: func(ctx context.Context, vpcID egov3.UUID) (*egov3.ListSubnetsResponse, error) {
				return &egov3.ListSubnetsResponse{
					Subnets: []egov3.ListSubnetEntry{
						{ID: egov3.UUID("bb0e8400-e29b-41d4-a716-446655440037"), Name: "other-subnet"},
					},
				}, nil
			},
			exoscaleClientGetSubnet: func(ctx context.Context, vpcID, id egov3.UUID) (*egov3.Subnet, error) {
				return nil, errors.New("should not be called")
			},
			wantErr:     true,
			errContains: "subnet with name non-existent-subnet not found in vpc",
		},
		{
			name: "fails when subnet name is ambiguous within the selected VPC",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
				Spec: apiv1.ExoscaleNodeClassSpec{
					VpcSelector:            &apiv1.SelectorTerms{ID: vpcA},
					VpcSubnetSelectorTerms: []apiv1.SelectorTerms{{Name: "dup-subnet"}},
				},
			},
			exoscaleClientGetVpc: func(ctx context.Context, id egov3.UUID) (*egov3.Vpc, error) {
				return &egov3.Vpc{ID: id, Name: "vpc-a"}, nil
			},
			exoscaleClientListVpcs: func(ctx context.Context) (*egov3.ListVpcsResponse, error) {
				return nil, errors.New("should not be called")
			},
			exoscaleClientListSubnets: func(ctx context.Context, vpcID egov3.UUID) (*egov3.ListSubnetsResponse, error) {
				return &egov3.ListSubnetsResponse{
					Subnets: []egov3.ListSubnetEntry{
						{ID: egov3.UUID("dd0e8400-e29b-41d4-a716-446655440039"), Name: "dup-subnet"},
						{ID: egov3.UUID("ee0e8400-e29b-41d4-a716-44665544003a"), Name: "dup-subnet"},
					},
				}, nil
			},
			exoscaleClientGetSubnet: func(ctx context.Context, vpcID, id egov3.UUID) (*egov3.Subnet, error) {
				return nil, errors.New("should not be called")
			},
			wantErr:     true,
			errContains: "ambiguous",
		},
		{
			name: "subnet that only exists in another VPC is rejected and other VPC is never listed",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
				Spec: apiv1.ExoscaleNodeClassSpec{
					VpcSelector: &apiv1.SelectorTerms{Name: "karpenter-test-vpc"},
					VpcSubnetSelectorTerms: []apiv1.SelectorTerms{
						{Name: "sks-vpc"},
						{Name: "sks-vpc-2"}, // only exists in vpcB (e.g. default-ch-gva-2)
					},
				},
			},
			exoscaleClientGetVpc: func(ctx context.Context, id egov3.UUID) (*egov3.Vpc, error) {
				if id.String() != vpcA {
					t.Errorf("GetVpc called with unexpected vpc %s", id)
				}
				return &egov3.Vpc{ID: egov3.UUID(vpcA), Name: "karpenter-test-vpc"}, nil
			},
			exoscaleClientListVpcs: func(ctx context.Context) (*egov3.ListVpcsResponse, error) {
				return &egov3.ListVpcsResponse{
					Vpcs: []egov3.ListVpcEntry{
						{ID: egov3.UUID(vpcA), Name: "karpenter-test-vpc"},
						{ID: egov3.UUID(vpcB), Name: "default-ch-gva-2"},
					},
				}, nil
			},
			exoscaleClientListSubnets: func(ctx context.Context, vpcID egov3.UUID) (*egov3.ListSubnetsResponse, error) {
				if vpcID.String() != vpcA {
					t.Errorf("ListSubnets must only be called for the selected VPC, got %s (%s)", vpcID, vpcID)
					return nil, fmt.Errorf("ListSubnets called for non-selected vpc %s", vpcID)
				}
				return &egov3.ListSubnetsResponse{
					Subnets: []egov3.ListSubnetEntry{
						{ID: egov3.UUID("aa0e8400-e29b-41d4-a716-446655440036"), Name: "sks-vpc"},
					},
				}, nil
			},
			exoscaleClientGetSubnet: func(ctx context.Context, vpcID, id egov3.UUID) (*egov3.Subnet, error) {
				if vpcID.String() != vpcA {
					t.Errorf("GetSubnet must only be called for the selected VPC, got %s", vpcID)
				}
				return &egov3.Subnet{ID: id, Name: "sks-vpc"}, nil
			},
			wantErr:     true,
			errContains: "subnet with name sks-vpc-2 not found in vpc " + vpcA,
		},
		{
			name: "subnets are only resolved inside the selected VPC even when another VPC has same-named subnets",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
				Spec: apiv1.ExoscaleNodeClassSpec{
					VpcSelector: &apiv1.SelectorTerms{Name: "karpenter-test-vpc"},
					VpcSubnetSelectorTerms: []apiv1.SelectorTerms{
						{Name: "sks-vpc"},
					},
				},
			},
			exoscaleClientGetVpc: func(ctx context.Context, id egov3.UUID) (*egov3.Vpc, error) {
				return &egov3.Vpc{ID: egov3.UUID(vpcA), Name: "karpenter-test-vpc"}, nil
			},
			exoscaleClientListVpcs: func(ctx context.Context) (*egov3.ListVpcsResponse, error) {
				return &egov3.ListVpcsResponse{
					Vpcs: []egov3.ListVpcEntry{
						{ID: egov3.UUID(vpcA), Name: "karpenter-test-vpc"},
						{ID: egov3.UUID(vpcB), Name: "default-ch-gva-2"},
					},
				}, nil
			},
			exoscaleClientListSubnets: func(ctx context.Context, vpcID egov3.UUID) (*egov3.ListSubnetsResponse, error) {
				if vpcID.String() != vpcA {
					t.Errorf("ListSubnets must only be called for the selected VPC, got %s", vpcID)
					return nil, fmt.Errorf("ListSubnets called for non-selected vpc %s", vpcID)
				}
				// vpcA's sks-vpc has a different ID than vpcB's same-named sks-vpc
				return &egov3.ListSubnetsResponse{
					Subnets: []egov3.ListSubnetEntry{
						{ID: egov3.UUID("aa0e8400-e29b-41d4-a716-446655440036"), Name: "sks-vpc"},
					},
				}, nil
			},
			exoscaleClientGetSubnet: func(ctx context.Context, vpcID, id egov3.UUID) (*egov3.Subnet, error) {
				if vpcID.String() != vpcA {
					t.Errorf("GetSubnet must only be called for the selected VPC, got %s", vpcID)
				}
				return &egov3.Subnet{ID: id, Name: "sks-vpc"}, nil
			},
			wantErr:           false,
			expectedSubnetIDs: []string{"aa0e8400-e29b-41d4-a716-446655440036"},
			expectedVpcID:     vpcA,
		},
		{
			name: "fails when ListVpcs returns error",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
				Spec: apiv1.ExoscaleNodeClassSpec{
					VpcSelector:            &apiv1.SelectorTerms{Name: "vpc-a"},
					VpcSubnetSelectorTerms: []apiv1.SelectorTerms{{Name: "some-subnet"}},
				},
			},
			exoscaleClientGetVpc: func(ctx context.Context, id egov3.UUID) (*egov3.Vpc, error) {
				return nil, errors.New("should not be called")
			},
			exoscaleClientListVpcs: func(ctx context.Context) (*egov3.ListVpcsResponse, error) {
				return nil, errors.New("API error")
			},
			exoscaleClientListSubnets: func(ctx context.Context, vpcID egov3.UUID) (*egov3.ListSubnetsResponse, error) {
				return nil, errors.New("should not be called")
			},
			exoscaleClientGetSubnet: func(ctx context.Context, vpcID, id egov3.UUID) (*egov3.Subnet, error) {
				return nil, errors.New("should not be called")
			},
			wantErr:     true,
			errContains: "failed to get vpc by name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.nodeClass).
				Build()

			mockExoClient := &providers.MockClient{
				GetVpcFunc:      tt.exoscaleClientGetVpc,
				ListVpcsFunc:    tt.exoscaleClientListVpcs,
				ListSubnetsFunc: tt.exoscaleClientListSubnets,
				GetSubnetFunc:   tt.exoscaleClientGetSubnet,
			}

			reconciler := &ExoscaleNodeClassReconciler{
				Client:         fakeClient,
				Scheme:         scheme,
				ExoscaleClient: mockExoClient,
				Recorder:       events.NewFakeRecorder(10),
				Zone:           "ch-gva-2",
			}

			ctx := context.Background()
			err := reconciler.reconcileVpcSubnets(ctx, tt.nodeClass)

			if tt.wantErr {
				if err == nil {
					t.Errorf("reconcileVpcSubnets() expected error but got none")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("reconcileVpcSubnets() error = %v, want error containing %v", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("reconcileVpcSubnets() unexpected error = %v", err)
				return
			}

			if len(tt.expectedSubnetIDs) != len(tt.nodeClass.Status.VpcSubnets) {
				t.Errorf("reconcileVpcSubnets() got %d vpc subnets, want %d",
					len(tt.nodeClass.Status.VpcSubnets), len(tt.expectedSubnetIDs))
				return
			}
			for i, expectedID := range tt.expectedSubnetIDs {
				if tt.nodeClass.Status.VpcSubnets[i] != expectedID {
					t.Errorf("reconcileVpcSubnets() vpc subnet[%d] = %v, want %v",
						i, tt.nodeClass.Status.VpcSubnets[i], expectedID)
				}
			}
			if tt.nodeClass.Status.VpcID != tt.expectedVpcID {
				t.Errorf("reconcileVpcSubnets() vpcID = %v, want %v", tt.nodeClass.Status.VpcID, tt.expectedVpcID)
			}
		})
	}
}
