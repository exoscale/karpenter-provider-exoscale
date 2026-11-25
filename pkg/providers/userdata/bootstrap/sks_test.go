package bootstrap

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"reflect"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestBuildConfigWithFeatureGates(t *testing.T) {
	options := &Options{
		ClusterEndpoint: "https://api.exoscale-cloud.com",
		BootstrapToken:  "token123",
		CABundle:        []byte("test-ca-bundle"),
		FeatureGates: map[string]bool{
			"ImageVolume": true,
			"MemoryQoS":   true,
			"TestFeature": false,
		},
	}

	config := buildConfig(options)

	if !reflect.DeepEqual(config.Settings.Kubernetes.FeatureGates, options.FeatureGates) {
		t.Errorf("FeatureGates = %v, want %v", config.Settings.Kubernetes.FeatureGates, options.FeatureGates)
	}
}

func TestBuildConfigWithoutFeatureGates(t *testing.T) {
	options := &Options{
		ClusterEndpoint: "https://api.exoscale-cloud.com",
		BootstrapToken:  "token123",
		CABundle:        []byte("test-ca-bundle"),
	}

	config := buildConfig(options)

	if config.Settings.Kubernetes.FeatureGates != nil {
		t.Error("FeatureGates should be nil when not provided")
	}
}

func TestDeepMerge(t *testing.T) {
	tests := []struct {
		name string
		dst  map[string]any
		src  map[string]any
		want map[string]any
	}{
		{
			name: "src overwrites scalar values",
			dst:  map[string]any{"a": "old"},
			src:  map[string]any{"a": "new"},
			want: map[string]any{"a": "new"},
		},
		{
			name: "src adds new keys",
			dst:  map[string]any{"a": "1"},
			src:  map[string]any{"b": "2"},
			want: map[string]any{"a": "1", "b": "2"},
		},
		{
			name: "nested maps are merged recursively",
			dst: map[string]any{
				"settings": map[string]any{
					"kubernetes": map[string]any{
						"node-labels": map[string]any{"env": "prod"},
					},
					"kubelet-device-plugins": map[string]any{
						"nvidia": map[string]any{"device-sharing-strategy": "time-slicing"},
					},
				},
			},
			src: map[string]any{
				"settings": map[string]any{
					"kubernetes": map[string]any{
						"api-server": "https://api.example.com",
					},
				},
			},
			want: map[string]any{
				"settings": map[string]any{
					"kubernetes": map[string]any{
						"api-server":  "https://api.example.com",
						"node-labels": map[string]any{"env": "prod"},
					},
					"kubelet-device-plugins": map[string]any{
						"nvidia": map[string]any{"device-sharing-strategy": "time-slicing"},
					},
				},
			},
		},
		{
			name: "src overwrites nested scalar in map",
			dst: map[string]any{
				"settings": map[string]any{
					"kubernetes": map[string]any{
						"api-server": "https://user-provided.example.com",
					},
				},
			},
			src: map[string]any{
				"settings": map[string]any{
					"kubernetes": map[string]any{
						"api-server": "https://karpenter-managed.example.com",
					},
				},
			},
			want: map[string]any{
				"settings": map[string]any{
					"kubernetes": map[string]any{
						"api-server": "https://karpenter-managed.example.com",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deepMerge(tt.dst, tt.src)
			gotBytes, _ := toml.Marshal(got)
			wantBytes, _ := toml.Marshal(tt.want)
			if string(gotBytes) != string(wantBytes) {
				t.Errorf("deepMerge() =\n%s\nwant:\n%s", gotBytes, wantBytes)
			}
		})
	}
}

func decodeUserData(t *testing.T, encoded string) map[string]any {
	t.Helper()
	compressed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("failed to base64 decode: %v", err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read gzip data: %v", err)
	}
	var result map[string]any
	if err := toml.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal TOML: %v", err)
	}
	return result
}

func TestGenerateWithUserData(t *testing.T) {
	customUserData := `[settings.kubelet-device-plugins.nvidia]
device-sharing-strategy = "time-slicing"

[settings.kubelet-device-plugins.nvidia.time-slicing]
replicas = 4
`

	options := &Options{
		ClusterEndpoint: "https://api.example.com",
		BootstrapToken:  "token123",
		CABundle:        []byte("test-ca-bundle"),
		ClusterDNS:      []string{"10.96.0.10"},
		UserData:        &customUserData,
	}

	encoded, err := Generate(options)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	result := decodeUserData(t, encoded)

	settings, ok := result["settings"].(map[string]any)
	if !ok {
		t.Fatal("settings not found in result")
	}
	k8s, ok := settings["kubernetes"].(map[string]any)
	if !ok {
		t.Fatal("settings.kubernetes not found in result")
	}
	if k8s["api-server"] != "https://api.example.com" {
		t.Errorf("api-server = %v, want https://api.example.com", k8s["api-server"])
	}

	plugins, ok := settings["kubelet-device-plugins"].(map[string]any)
	if !ok {
		t.Fatal("settings.kubelet-device-plugins not found in merged result")
	}
	nvidia, ok := plugins["nvidia"].(map[string]any)
	if !ok {
		t.Fatal("settings.kubelet-device-plugins.nvidia not found in merged result")
	}
	if nvidia["device-sharing-strategy"] != "time-slicing" {
		t.Errorf("device-sharing-strategy = %v, want time-slicing", nvidia["device-sharing-strategy"])
	}
}

func TestGenerateUserDataDoesNotOverrideKarpenterFields(t *testing.T) {
	// User tries to override api-server — Karpenter should win
	customUserData := `[settings.kubernetes]
api-server = "https://malicious.example.com"
bootstrap-token = "evil-token"

[settings.kubernetes.node-labels]
custom-label = "custom-value"
`

	options := &Options{
		ClusterEndpoint: "https://api.example.com",
		BootstrapToken:  "token123",
		CABundle:        []byte("test-ca-bundle"),
		Labels:          map[string]string{"karpenter-label": "karpenter-value"},
		UserData:        &customUserData,
	}

	encoded, err := Generate(options)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	result := decodeUserData(t, encoded)
	settings := result["settings"].(map[string]any)
	k8s := settings["kubernetes"].(map[string]any)

	if k8s["api-server"] != "https://api.example.com" {
		t.Errorf("api-server = %v, want https://api.example.com (Karpenter should win)", k8s["api-server"])
	}
	if k8s["bootstrap-token"] != "token123" {
		t.Errorf("bootstrap-token = %v, want token123 (Karpenter should win)", k8s["bootstrap-token"])
	}

	labels, ok := k8s["node-labels"].(map[string]any)
	if !ok {
		t.Fatal("node-labels not found")
	}
	if labels["custom-label"] != "custom-value" {
		t.Errorf("custom-label = %v, want custom-value", labels["custom-label"])
	}
	if labels["karpenter-label"] != "karpenter-value" {
		t.Errorf("karpenter-label = %v, want karpenter-value", labels["karpenter-label"])
	}
}

func TestGenerateWithoutUserData(t *testing.T) {
	options := &Options{
		ClusterEndpoint: "https://api.example.com",
		BootstrapToken:  "token123",
		CABundle:        []byte("test-ca-bundle"),
	}

	encoded, err := Generate(options)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	result := decodeUserData(t, encoded)
	settings := result["settings"].(map[string]any)
	k8s := settings["kubernetes"].(map[string]any)

	if k8s["api-server"] != "https://api.example.com" {
		t.Errorf("api-server = %v, want https://api.example.com", k8s["api-server"])
	}

	if _, ok := settings["kubelet-device-plugins"]; ok {
		t.Error("kubelet-device-plugins should not be present without userData")
	}
}

func TestGenerateWithInvalidUserData(t *testing.T) {
	invalidTOML := `this is not valid [[ toml`
	options := &Options{
		ClusterEndpoint: "https://api.example.com",
		BootstrapToken:  "token123",
		CABundle:        []byte("test-ca-bundle"),
		UserData:        &invalidTOML,
	}

	_, err := Generate(options)
	if err == nil {
		t.Fatal("Generate() should fail with invalid TOML user data")
	}
}

func TestGenerateWithEmptyUserData(t *testing.T) {
	empty := ""
	options := &Options{
		ClusterEndpoint: "https://api.example.com",
		BootstrapToken:  "token123",
		CABundle:        []byte("test-ca-bundle"),
		UserData:        &empty,
	}

	// Empty string should behave like no user data
	encoded, err := Generate(options)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	result := decodeUserData(t, encoded)
	settings := result["settings"].(map[string]any)
	k8s := settings["kubernetes"].(map[string]any)
	if k8s["api-server"] != "https://api.example.com" {
		t.Errorf("api-server = %v, want https://api.example.com", k8s["api-server"])
	}
}

func TestBuildConfigCPUManager(t *testing.T) {
	base := &Options{
		ClusterEndpoint: "https://api.example.com",
		BootstrapToken:  "token123",
		CABundle:        []byte("test-ca-bundle"),
	}

	t.Run("empty", func(t *testing.T) {
		cfg := buildConfig(base)
		k := cfg.Settings.Kubernetes
		if k.CPUManagerPolicy != "" || k.CPUManagerPolicyOptions != nil || k.CPUManagerReconcilePeriod != "" {
			t.Errorf("expected no CPU manager fields, got policy=%q options=%v period=%q",
				k.CPUManagerPolicy, k.CPUManagerPolicyOptions, k.CPUManagerReconcilePeriod)
		}
	})

	t.Run("static only", func(t *testing.T) {
		o := *base
		o.CPUManagerPolicy = "static"
		cfg := buildConfig(&o)
		if cfg.Settings.Kubernetes.CPUManagerPolicy != "static" {
			t.Errorf("CPUManagerPolicy = %q, want static", cfg.Settings.Kubernetes.CPUManagerPolicy)
		}
	})

	t.Run("static with options", func(t *testing.T) {
		o := *base
		o.CPUManagerPolicy = "static"
		o.CPUManagerPolicyOptions = []string{"full-pcpus-only", "align-by-socket"}
		cfg := buildConfig(&o)
		if got := cfg.Settings.Kubernetes.CPUManagerPolicyOptions; len(got) != 2 {
			t.Errorf("CPUManagerPolicyOptions = %v, want 2 entries", got)
		}
	})

	t.Run("options dropped when policy is none", func(t *testing.T) {
		o := *base
		o.CPUManagerPolicy = "none"
		o.CPUManagerPolicyOptions = []string{"full-pcpus-only"}
		cfg := buildConfig(&o)
		if cfg.Settings.Kubernetes.CPUManagerPolicyOptions != nil {
			t.Errorf("CPUManagerPolicyOptions should be nil when policy=none, got %v",
				cfg.Settings.Kubernetes.CPUManagerPolicyOptions)
		}
	})

	t.Run("options dropped when policy is empty", func(t *testing.T) {
		o := *base
		o.CPUManagerPolicyOptions = []string{"full-pcpus-only"}
		cfg := buildConfig(&o)
		if cfg.Settings.Kubernetes.CPUManagerPolicyOptions != nil {
			t.Errorf("CPUManagerPolicyOptions should be nil when policy is empty, got %v",
				cfg.Settings.Kubernetes.CPUManagerPolicyOptions)
		}
	})

	t.Run("reconcile period", func(t *testing.T) {
		o := *base
		o.CPUManagerReconcilePeriod = "5s"
		cfg := buildConfig(&o)
		if cfg.Settings.Kubernetes.CPUManagerReconcilePeriod != "5s" {
			t.Errorf("CPUManagerReconcilePeriod = %q, want 5s", cfg.Settings.Kubernetes.CPUManagerReconcilePeriod)
		}
	})

	t.Run("default period (10s) omitted", func(t *testing.T) {
		o := *base
		o.CPUManagerReconcilePeriod = "10s"
		cfg := buildConfig(&o)
		if cfg.Settings.Kubernetes.CPUManagerReconcilePeriod != "" {
			t.Errorf("CPUManagerReconcilePeriod = %q, want empty (kubelet default 10s must not be emitted)", cfg.Settings.Kubernetes.CPUManagerReconcilePeriod)
		}
	})
}

func TestGenerateCPUManagerTOMLEmission(t *testing.T) {
	options := &Options{
		ClusterEndpoint:           "https://api.example.com",
		BootstrapToken:            "token123",
		CABundle:                  []byte("test-ca-bundle"),
		CPUManagerPolicy:          "static",
		CPUManagerPolicyOptions:   []string{"full-pcpus-only"},
		CPUManagerReconcilePeriod: "5s",
	}

	encoded, err := Generate(options)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	result := decodeUserData(t, encoded)
	settings := result["settings"].(map[string]any)
	k8s := settings["kubernetes"].(map[string]any)

	if k8s["cpu-manager-policy"] != "static" {
		t.Errorf("cpu-manager-policy = %v, want static", k8s["cpu-manager-policy"])
	}
	opts, ok := k8s["cpu-manager-policy-options"].([]any)
	if !ok || len(opts) != 1 || opts[0] != "full-pcpus-only" {
		t.Errorf("cpu-manager-policy-options = %v, want [full-pcpus-only]", k8s["cpu-manager-policy-options"])
	}
	if k8s["cpu-manager-reconcile-period"] != "5s" {
		t.Errorf("cpu-manager-reconcile-period = %v, want 5s", k8s["cpu-manager-reconcile-period"])
	}
}

func TestGenerateCPUManagerTOMLDefaultPeriodOmitted(t *testing.T) {
	// Simulates what the apiserver hands us after server-side default
	// injection: period is "10s" even though the user never set it.
	options := &Options{
		ClusterEndpoint:           "https://api.example.com",
		BootstrapToken:            "token123",
		CABundle:                  []byte("test-ca-bundle"),
		CPUManagerPolicy:          "static",
		CPUManagerPolicyOptions:   []string{"full-pcpus-only"},
		CPUManagerReconcilePeriod: "10s",
	}

	encoded, err := Generate(options)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	result := decodeUserData(t, encoded)
	settings := result["settings"].(map[string]any)
	k8s := settings["kubernetes"].(map[string]any)

	if _, ok := k8s["cpu-manager-reconcile-period"]; ok {
		t.Errorf("cpu-manager-reconcile-period must be absent when value equals kubelet default 10s, got %v", k8s["cpu-manager-reconcile-period"])
	}
}

func TestGenerateCPUManagerTOMLAbsentWhenEmpty(t *testing.T) {
	options := &Options{
		ClusterEndpoint: "https://api.example.com",
		BootstrapToken:  "token123",
		CABundle:        []byte("test-ca-bundle"),
	}

	encoded, err := Generate(options)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	result := decodeUserData(t, encoded)
	settings := result["settings"].(map[string]any)
	k8s := settings["kubernetes"].(map[string]any)

	for _, key := range []string{"cpu-manager-policy", "cpu-manager-policy-options", "cpu-manager-reconcile-period"} {
		if v, ok := k8s[key]; ok {
			t.Errorf("%s = %v, want absent", key, v)
		}
	}
}

func TestBuildConfigMaxPods(t *testing.T) {
	base := &Options{
		ClusterEndpoint: "https://api.example.com",
		BootstrapToken:  "token123",
		CABundle:        []byte("test-ca-bundle"),
	}

	t.Run("nil omitted", func(t *testing.T) {
		cfg := buildConfig(base)
		if cfg.Settings.Kubernetes.MaxPods != nil {
			t.Errorf("MaxPods = %v, want nil when unset", *cfg.Settings.Kubernetes.MaxPods)
		}
	})

	t.Run("set value emitted", func(t *testing.T) {
		o := *base
		v := int32(110)
		o.MaxPods = &v
		cfg := buildConfig(&o)
		if cfg.Settings.Kubernetes.MaxPods == nil {
			t.Fatal("MaxPods = nil, want 110")
		}
		if *cfg.Settings.Kubernetes.MaxPods != 110 {
			t.Errorf("MaxPods = %d, want 110", *cfg.Settings.Kubernetes.MaxPods)
		}
	})

	t.Run("zero value is propagated as-is", func(t *testing.T) {
		// The sks-templates treats zero as "use kubelet default", but the
		// Karpenter provider does not strip an explicit zero: doing so would
		// break drift hashing (remove the override, hash stays empty). We
		// always emit what the user asked for and let sks-templates decide.
		o := *base
		v := int32(0)
		o.MaxPods = &v
		cfg := buildConfig(&o)
		if cfg.Settings.Kubernetes.MaxPods == nil || *cfg.Settings.Kubernetes.MaxPods != 0 {
			t.Errorf("MaxPods = %v, want pointer to 0", cfg.Settings.Kubernetes.MaxPods)
		}
	})
}

func TestGenerateMaxPodsTOMLEmission(t *testing.T) {
	t.Run("unset -> absent", func(t *testing.T) {
		options := &Options{
			ClusterEndpoint: "https://api.example.com",
			BootstrapToken:  "token123",
			CABundle:        []byte("test-ca-bundle"),
		}
		encoded, err := Generate(options)
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		result := decodeUserData(t, encoded)
		settings := result["settings"].(map[string]any)
		k8s := settings["kubernetes"].(map[string]any)
		if v, ok := k8s["max-pods"]; ok {
			t.Errorf("max-pods = %v, want absent when unset", v)
		}
	})

	t.Run("set -> emitted", func(t *testing.T) {
		v := int32(250)
		options := &Options{
			ClusterEndpoint: "https://api.example.com",
			BootstrapToken:  "token123",
			CABundle:        []byte("test-ca-bundle"),
			MaxPods:         &v,
		}
		encoded, err := Generate(options)
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		result := decodeUserData(t, encoded)
		settings := result["settings"].(map[string]any)
		k8s := settings["kubernetes"].(map[string]any)

		got, ok := k8s["max-pods"].(int64)
		if !ok {
			t.Fatalf("max-pods = %v (%T), want int64 250", k8s["max-pods"], k8s["max-pods"])
		}
		if got != 250 {
			t.Errorf("max-pods = %d, want 250", got)
		}
	})

	t.Run("merged user data keeps user value", func(t *testing.T) {
		v := int32(64)
		userData := "[settings.kubernetes]\nmax-pods = 42\n"
		options := &Options{
			ClusterEndpoint: "https://api.example.com",
			BootstrapToken:  "token123",
			CABundle:        []byte("test-ca-bundle"),
			MaxPods:         &v,
			UserData:        &userData,
		}
		encoded, err := Generate(options)
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		result := decodeUserData(t, encoded)
		settings := result["settings"].(map[string]any)
		k8s := settings["kubernetes"].(map[string]any)

		got, ok := k8s["max-pods"].(int64)
		if !ok || got != 64 {
			t.Errorf("max-pods = %v, want 64 (Karpenter value must override user value)", k8s["max-pods"])
		}
	})
}
