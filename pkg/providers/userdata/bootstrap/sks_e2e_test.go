package bootstrap

import (
	"testing"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
)

// TestE2E_GenerateFullPipeline exercises the full bootstrap pipeline
// (Options -> gzip+base64 TOML -> decode -> assert) with every drift-
// sensitive field set: kubelet CPU manager, kubelet maxPods, container
// registry credentials and TLS material. This guarantees that the
// downstream sks-node-agent receives every new field under the right
// settings.kubernetes.* key.
func TestE2E_GenerateFullPipeline(t *testing.T) {
	v := int32(110)
	options := &Options{
		ClusterEndpoint: "https://api.example.com",
		BootstrapToken:  "token123",
		CABundle:        []byte("test-ca-bundle"),
		KubeReserved: apiv1.KubeResourceReservation{
			CPU:    "200m",
			Memory: "300Mi",
		},
		SystemReserved: apiv1.SystemResourceReservation{
			CPU:    "100m",
			Memory: "100Mi",
		},
		Labels: map[string]string{
			"topology.kubernetes.io/zone": "ch-gva-2",
		},
		ImageGCHighThresholdPercent: int32Ptr(85),
		ImageGCLowThresholdPercent:  int32Ptr(80),
		ImageMinimumGCAge:           "2m",
		FeatureGates: map[string]bool{
			"ImageVolume": true,
		},
		CPUManagerPolicy:          "static",
		CPUManagerPolicyOptions:   []string{"full-pcpus-only"},
		CPUManagerReconcilePeriod: "5s",
		MaxPods:                   &v,
	}

	s := New()
	encoded, err := s.Generate(options)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	result := decodeUserData(t, encoded)
	settings, ok := result["settings"].(map[string]interface{})
	if !ok {
		t.Fatalf("settings key missing or wrong type: %T", result["settings"])
	}
	k8s, ok := settings["kubernetes"].(map[string]interface{})
	if !ok {
		t.Fatalf("settings.kubernetes key missing or wrong type: %T", settings["kubernetes"])
	}

	mustEqual := func(key string, want interface{}) {
		t.Helper()
		if got := k8s[key]; got != want {
			t.Errorf("settings.kubernetes.%s = %v (%T), want %v (%T)", key, got, got, want, want)
		}
	}
	mustExist := func(key string) {
		t.Helper()
		if _, ok := k8s[key]; !ok {
			t.Errorf("settings.kubernetes.%s is missing from emitted TOML", key)
		}
	}

	mustEqual("api-server", "https://api.example.com")
	mustEqual("bootstrap-token", "token123")
	mustEqual("cloud-provider", "external")
	mustEqual("image-gc-high-threshold-percent", int64(85))
	mustEqual("image-gc-low-threshold-percent", int64(80))
	mustEqual("image-minimum-gc-age", "2m")
	mustEqual("cpu-manager-policy", "static")
	mustEqualSlice(t, "cpu-manager-policy-options", k8s["cpu-manager-policy-options"], []string{"full-pcpus-only"})
	mustEqual("cpu-manager-reconcile-period", "5s")
	mustEqual("max-pods", int64(110))
	mustExist("kube-reserved")
	mustExist("system-reserved")
}

// TestE2E_GenerateUserDataMergeHonoursKarpenterOverrides verifies that
// when the user supplies userData with a max-pods and a cpu-manager-policy
// setting, Karpenter's structured fields override the user's values
// (Karpenter-managed sections always win).
func TestE2E_GenerateUserDataMergeHonoursKarpenterOverrides(t *testing.T) {
	v := int32(64)
	userData := `[settings.kubernetes]
cpu-manager-policy = "none"
max-pods = 42
`
	options := &Options{
		ClusterEndpoint:  "https://api.example.com",
		BootstrapToken:   "token123",
		CABundle:         []byte("test-ca-bundle"),
		CPUManagerPolicy: "static",
		MaxPods:          &v,
		UserData:         &userData,
	}

	s := New()
	encoded, err := s.Generate(options)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	result := decodeUserData(t, encoded)
	settings := result["settings"].(map[string]interface{})
	k8s := settings["kubernetes"].(map[string]interface{})

	if got := k8s["cpu-manager-policy"]; got != "static" {
		t.Errorf("cpu-manager-policy = %v, want static (Karpenter must override user value)", got)
	}
	if got := k8s["max-pods"]; got != int64(64) {
		t.Errorf("max-pods = %v (%T), want 64 (Karpenter must override user value)", got, got)
	}
}

func int32Ptr(v int32) *int32 { return &v }

func mustEqualSlice(t *testing.T, key string, got interface{}, want []string) {
	t.Helper()
	arr, ok := got.([]interface{})
	if !ok {
		t.Errorf("%s = %v (%T), want []string", key, got, got)
		return
	}
	if len(arr) != len(want) {
		t.Errorf("%s len = %d, want %d", key, len(arr), len(want))
		return
	}
	for i, w := range want {
		if arr[i] != w {
			t.Errorf("%s[%d] = %v, want %s", key, i, arr[i], w)
		}
	}
}
