package userdata

import (
	"slices"
	"testing"

	v1 "k8s.io/api/core/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func TestRegisterTaints(t *testing.T) {
	taint := v1.Taint{Key: "dedicated", Value: "gpu", Effect: v1.TaintEffectNoSchedule}
	startup := v1.Taint{Key: "cni-not-ready", Effect: v1.TaintEffectNoExecute}
	nodeClaim := &karpenterv1.NodeClaim{Spec: karpenterv1.NodeClaimSpec{
		Taints:        []v1.Taint{taint},
		StartupTaints: []v1.Taint{startup},
	}}

	want := []v1.Taint{taint, startup, karpenterv1.UnregisteredNoExecuteTaint}
	if got := registerTaints(nodeClaim); !slices.Equal(got, want) {
		t.Fatalf("registerTaints() = %v, want %v", got, want)
	}
}
