package nodeclass

import (
	"context"
	"errors"
	"strings"
	"testing"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/pkg/providers"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/template"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func TestIsNodeClaimUsingNodeClass(t *testing.T) {
	tests := []struct {
		name          string
		nodeClaim     *karpenterv1.NodeClaim
		nodeClassName string
		want          bool
	}{
		{
			name: "matching nodeclass",
			nodeClaim: &karpenterv1.NodeClaim{
				Spec: karpenterv1.NodeClaimSpec{
					NodeClassRef: &karpenterv1.NodeClassReference{
						Group: "karpenter.exoscale.com",
						Kind:  "ExoscaleNodeClass",
						Name:  "test-class",
					},
				},
			},
			nodeClassName: "test-class",
			want:          true,
		},
		{
			name: "not matching",
			nodeClaim: &karpenterv1.NodeClaim{
				Spec: karpenterv1.NodeClaimSpec{
					NodeClassRef: &karpenterv1.NodeClassReference{
						Group: "karpenter.exoscale.com",
						Kind:  "ExoscaleNodeClass",
						Name:  "other-class",
					},
				},
			},
			nodeClassName: "test-class",
			want:          false,
		},
		{
			name: "nil nodeClassRef",
			nodeClaim: &karpenterv1.NodeClaim{
				Spec: karpenterv1.NodeClaimSpec{
					NodeClassRef: nil,
				},
			},
			nodeClassName: "test-class",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNodeClaimUsingNodeClass(tt.nodeClaim, tt.nodeClassName)
			if got != tt.want {
				t.Errorf("isNodeClaimUsingNodeClass() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCountActiveNodeClaims(t *testing.T) {
	now := metav1.Now()

	tests := []struct {
		name          string
		nodeClaims    []karpenterv1.NodeClaim
		nodeClassName string
		want          int
	}{
		{
			name: "two active nodeclaims",
			nodeClaims: []karpenterv1.NodeClaim{
				{
					Spec: karpenterv1.NodeClaimSpec{
						NodeClassRef: &karpenterv1.NodeClassReference{
							Group: "karpenter.exoscale.com",
							Kind:  "ExoscaleNodeClass",
							Name:  "test-class",
						},
					},
				},
				{
					Spec: karpenterv1.NodeClaimSpec{
						NodeClassRef: &karpenterv1.NodeClassReference{
							Group: "karpenter.exoscale.com",
							Kind:  "ExoscaleNodeClass",
							Name:  "test-class",
						},
					},
				},
			},
			nodeClassName: "test-class",
			want:          2,
		},
		{
			name: "excludes deleting nodeclaims",
			nodeClaims: []karpenterv1.NodeClaim{
				{
					Spec: karpenterv1.NodeClaimSpec{
						NodeClassRef: &karpenterv1.NodeClassReference{
							Group: "karpenter.exoscale.com",
							Kind:  "ExoscaleNodeClass",
							Name:  "test-class",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						DeletionTimestamp: &now,
					},
					Spec: karpenterv1.NodeClaimSpec{
						NodeClassRef: &karpenterv1.NodeClassReference{
							Group: "karpenter.exoscale.com",
							Kind:  "ExoscaleNodeClass",
							Name:  "test-class",
						},
					},
				},
			},
			nodeClassName: "test-class",
			want:          1,
		},
		{
			name:          "no matching nodeclaims",
			nodeClaims:    []karpenterv1.NodeClaim{},
			nodeClassName: "test-class",
			want:          0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countActiveNodeClaims(tt.nodeClaims, tt.nodeClassName)
			if got != tt.want {
				t.Errorf("countActiveNodeClaims() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateResourceQuantities(t *testing.T) {
	tests := []struct {
		name             string
		cpu              string
		memory           string
		ephemeralStorage string
		wantErr          bool
	}{
		{
			name:             "valid reservations",
			cpu:              "100m",
			memory:           "512Mi",
			ephemeralStorage: "1Gi",
			wantErr:          false,
		},
		{
			name:    "empty reservations",
			wantErr: false,
		},
		{
			name:    "invalid cpu quantity",
			cpu:     "invalid",
			wantErr: true,
		},
		{
			name:    "invalid memory quantity",
			memory:  "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateResourceQuantities(tt.cpu, tt.memory, tt.ephemeralStorage)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateResourceQuantities() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

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
				if id.String() != "550e8400-e29b-41d4-a716-446655440000" {
					t.Errorf("expected template ID '550e8400-e29b-41d4-a716-446655440000', got '%s'", id.String())
				}
				return &egov3.Template{
					ID: egov3.UUID("550e8400-e29b-41d4-a716-446655440000"),
				}, nil
			},
			wantErr: false,
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
				if id.String() != "660e8400-e29b-41d4-a716-446655440001" {
					t.Errorf("expected template ID '660e8400-e29b-41d4-a716-446655440001', got '%s'", id.String())
				}
				return &egov3.Template{
					ID: egov3.UUID("660e8400-e29b-41d4-a716-446655440001"),
				}, nil
			},
			wantErr: false,
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
			wantErr:     true,
			errContains: "failed to resolve template ID",
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
			wantErr:     true,
			errContains: "not found or not accessible",
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
			wantErr:     true,
			errContains: "not found or not accessible",
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
				Recorder:         record.NewFakeRecorder(10),
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
		})
	}
}
