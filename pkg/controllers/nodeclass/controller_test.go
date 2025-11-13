package nodeclass

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
