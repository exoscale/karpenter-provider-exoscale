package garbagecollection

import (
	"testing"
	"time"

	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/instance"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func TestHasMatchingNodeClaim(t *testing.T) {
	providerIDs := map[string]bool{"i-matched-by-id": true}
	names := map[string]bool{"claim-existing": true}

	tests := []struct {
		name string
		inst *instance.Instance
		want bool
	}{
		{
			name: "matched by provider id",
			inst: &instance.Instance{ID: "i-matched-by-id"},
			want: true,
		},
		{
			name: "matched by node-claim label when provider id not yet known",
			inst: &instance.Instance{
				ID:     "i-new",
				Labels: map[string]string{constants.InstanceLabelNodeClaim: "claim-existing"},
			},
			want: true,
		},
		{
			name: "node-claim label points to a missing claim",
			inst: &instance.Instance{
				ID:     "i-orphan",
				Labels: map[string]string{constants.InstanceLabelNodeClaim: "claim-gone"},
			},
			want: false,
		},
		{
			name: "no provider id match and no node-claim label",
			inst: &instance.Instance{ID: "i-orphan-nolabel"},
			want: false,
		},
		{
			name: "empty node-claim label is ignored",
			inst: &instance.Instance{
				ID:     "i-orphan-emptylabel",
				Labels: map[string]string{constants.InstanceLabelNodeClaim: ""},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasMatchingNodeClaim(tt.inst, providerIDs, names); got != tt.want {
				t.Errorf("hasMatchingNodeClaim() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsOrphanInstanceReapable(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	grace := OrphanInstanceGracePeriod

	tests := []struct {
		name      string
		createdAt time.Time
		want      bool
	}{
		{
			name:      "freshly created instance is spared",
			createdAt: now.Add(-30 * time.Second),
			want:      false,
		},
		{
			name:      "instance just under grace period is spared",
			createdAt: now.Add(-grace + time.Second),
			want:      false,
		},
		{
			name:      "instance exactly at grace period is reapable",
			createdAt: now.Add(-grace),
			want:      true,
		},
		{
			name:      "old instance is reapable",
			createdAt: now.Add(-time.Hour),
			want:      true,
		},
		{
			name:      "unknown creation time is spared",
			createdAt: time.Time{},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isOrphanInstanceReapable(tt.createdAt, now, grace); got != tt.want {
				t.Errorf("isOrphanInstanceReapable(%v, %v, %v) = %v, want %v", tt.createdAt, now, grace, got, tt.want)
			}
		})
	}
}

func TestHasTerminationFinalizer(t *testing.T) {
	tests := []struct {
		name      string
		nodeClaim *karpenterv1.NodeClaim
		want      bool
	}{
		{
			name: "has termination finalizer",
			nodeClaim: &karpenterv1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{TerminationFinalizerName},
				},
			},
			want: true,
		},
		{
			name: "has termination finalizer among others",
			nodeClaim: &karpenterv1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"other-finalizer", TerminationFinalizerName},
				},
			},
			want: true,
		},
		{
			name: "no termination finalizer",
			nodeClaim: &karpenterv1.NodeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"other-finalizer"},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasTerminationFinalizer(tt.nodeClaim)
			if got != tt.want {
				t.Errorf("hasTerminationFinalizer() = %v, want %v", got, tt.want)
			}
		})
	}
}
