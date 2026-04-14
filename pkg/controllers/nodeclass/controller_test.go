package nodeclass

import (
	"context"
	"reflect"
	"strings"
	"testing"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/karpenter/pkg/apis"
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
		errContains      string
	}{
		{
			name:             "valid reservations with all fields",
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
			name:        "invalid cpu quantity",
			cpu:         "invalid",
			wantErr:     true,
			errContains: "invalid CPU reservation",
		},
		{
			name:        "invalid memory quantity",
			memory:      "invalid",
			wantErr:     true,
			errContains: "invalid memory reservation",
		},
		{
			name:             "invalid ephemeral storage quantity",
			ephemeralStorage: "invalid",
			wantErr:          true,
			errContains:      "invalid ephemeral storage reservation",
		},
		{
			name:    "valid cpu only",
			cpu:     "2",
			wantErr: false,
		},
		{
			name:    "valid memory only",
			memory:  "8Gi",
			wantErr: false,
		},
		{
			name:             "valid ephemeral storage only",
			ephemeralStorage: "100Gi",
			wantErr:          false,
		},
		{
			name:    "valid cpu with different units",
			cpu:     "1500m",
			wantErr: false,
		},
		{
			name:    "valid memory with different units",
			memory:  "1024Mi",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateResourceQuantities(tt.cpu, tt.memory, tt.ephemeralStorage)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateResourceQuantities() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("validateResourceQuantities() error = %v, want error containing %v", err, tt.errContains)
			}
		})
	}
}

func TestValidateSpec(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = apiv1.AddToScheme(scheme)

	tests := []struct {
		name        string
		nodeClass   *apiv1.ExoscaleNodeClass
		wantErr     bool
		errContains string
	}{
		{
			name: "valid spec with all valid reservations",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					Kubelet: apiv1.KubeletConfiguration{
						KubeReserved: apiv1.KubeResourceReservation{
							CPU:              "100m",
							Memory:           "512Mi",
							EphemeralStorage: "1Gi",
						},
						SystemReserved: apiv1.SystemResourceReservation{
							CPU:              "50m",
							Memory:           "256Mi",
							EphemeralStorage: "500Mi",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid spec with empty reservations",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					Kubelet: apiv1.KubeletConfiguration{},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid kubeReserved CPU",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					Kubelet: apiv1.KubeletConfiguration{
						KubeReserved: apiv1.KubeResourceReservation{
							CPU: "invalid",
						},
					},
				},
			},
			wantErr:     true,
			errContains: "invalid kubelet.kubeReserved",
		},
		{
			name: "invalid kubeReserved memory",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					Kubelet: apiv1.KubeletConfiguration{
						KubeReserved: apiv1.KubeResourceReservation{
							Memory: "invalid",
						},
					},
				},
			},
			wantErr:     true,
			errContains: "invalid kubelet.kubeReserved",
		},
		{
			name: "invalid kubeReserved ephemeral storage",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					Kubelet: apiv1.KubeletConfiguration{
						KubeReserved: apiv1.KubeResourceReservation{
							EphemeralStorage: "invalid",
						},
					},
				},
			},
			wantErr:     true,
			errContains: "invalid kubelet.kubeReserved",
		},
		{
			name: "invalid systemReserved CPU",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					Kubelet: apiv1.KubeletConfiguration{
						SystemReserved: apiv1.SystemResourceReservation{
							CPU: "invalid",
						},
					},
				},
			},
			wantErr:     true,
			errContains: "invalid kubelet.systemReserved",
		},
		{
			name: "invalid systemReserved memory",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					Kubelet: apiv1.KubeletConfiguration{
						SystemReserved: apiv1.SystemResourceReservation{
							Memory: "invalid",
						},
					},
				},
			},
			wantErr:     true,
			errContains: "invalid kubelet.systemReserved",
		},
		{
			name: "invalid systemReserved ephemeral storage",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					Kubelet: apiv1.KubeletConfiguration{
						SystemReserved: apiv1.SystemResourceReservation{
							EphemeralStorage: "invalid",
						},
					},
				},
			},
			wantErr:     true,
			errContains: "invalid kubelet.systemReserved",
		},
		{
			name: "valid partial kubeReserved",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					Kubelet: apiv1.KubeletConfiguration{
						KubeReserved: apiv1.KubeResourceReservation{
							CPU:    "100m",
							Memory: "512Mi",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid partial systemReserved",
			nodeClass: &apiv1.ExoscaleNodeClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-nodeclass",
				},
				Spec: apiv1.ExoscaleNodeClassSpec{
					Kubelet: apiv1.KubeletConfiguration{
						SystemReserved: apiv1.SystemResourceReservation{
							CPU: "50m",
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciler := &ExoscaleNodeClassReconciler{
				Scheme: scheme,
			}

			err := reconciler.validateSpec(tt.nodeClass)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSpec() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("validateSpec() error = %v, want error containing %v", err, tt.errContains)
			}
		})
	}
}

func TestCleanupOrphanedInstancesOnlyDeletesInstancesForCurrentCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = apiv1.AddToScheme(scheme)
	gv := schema.GroupVersion{Group: apis.Group, Version: "v1"}
	scheme.AddKnownTypes(gv, &karpenterv1.NodeClaim{}, &karpenterv1.NodeClaimList{})
	metav1.AddToGroupVersion(scheme, gv)

	nodeClass := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "worker-node",
		},
	}
	activeNodeClaim := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-b-worker-active",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(nodeClass, activeNodeClaim).
		Build()

	deletedIDs := make([]egov3.UUID, 0)
	reconciler := &ExoscaleNodeClassReconciler{
		Client:    fakeClient,
		Scheme:    scheme,
		Recorder:  record.NewFakeRecorder(10),
		ClusterID: "cluster-b-id",
		ExoscaleClient: &providers.MockClient{
			ListInstancesFunc: func(ctx context.Context, opts ...egov3.ListInstancesOpt) (*egov3.ListInstancesResponse, error) {
				return &egov3.ListInstancesResponse{
					Instances: []egov3.ListInstancesResponseInstances{
						{
							ID:   egov3.UUID("11111111-1111-1111-1111-111111111111"),
							Name: "k-cluster-b-worker-orphan",
							Labels: map[string]string{
								constants.InstanceLabelManagedBy: constants.ManagedByKarpenter,
								constants.InstanceLabelClusterID: "cluster-b-id",
								constants.InstanceLabelNodeClaim: "cluster-b-worker-missing",
							},
						},
						{
							ID:   egov3.UUID("22222222-2222-2222-2222-222222222222"),
							Name: "k-cluster-a-worker",
							Labels: map[string]string{
								constants.InstanceLabelManagedBy: constants.ManagedByKarpenter,
								constants.InstanceLabelClusterID: "cluster-a-id",
								constants.InstanceLabelNodeClaim: "cluster-a-worker",
							},
						},
						{
							ID:   egov3.UUID("33333333-3333-3333-3333-333333333333"),
							Name: "k-cluster-b-worker-active",
							Labels: map[string]string{
								constants.InstanceLabelManagedBy: constants.ManagedByKarpenter,
								constants.InstanceLabelClusterID: "cluster-b-id",
								constants.InstanceLabelNodeClaim: "cluster-b-worker-active",
							},
						},
					},
				}, nil
			},
			DeleteInstanceFunc: func(ctx context.Context, id egov3.UUID) (*egov3.Operation, error) {
				deletedIDs = append(deletedIDs, id)
				return &egov3.Operation{}, nil
			},
		},
	}

	err := reconciler.cleanupOrphanedInstances(context.Background(), nodeClass)
	if err != nil {
		t.Fatalf("cleanupOrphanedInstances() unexpected error = %v", err)
	}

	wantDeleted := []egov3.UUID{
		egov3.UUID("11111111-1111-1111-1111-111111111111"),
	}
	if !reflect.DeepEqual(deletedIDs, wantDeleted) {
		t.Fatalf("cleanupOrphanedInstances() deleted IDs = %v, want %v", deletedIDs, wantDeleted)
	}
}
