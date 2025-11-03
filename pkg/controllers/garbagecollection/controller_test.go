package garbagecollection

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

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
