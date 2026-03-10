package bootstrap

import (
	"testing"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
)

func TestConvertKubeResourceReservation(t *testing.T) {
	tests := []struct {
		name string
		rr   apiv1.KubeResourceReservation
		want *ResourceReservation
	}{
		{
			name: "empty reservation",
			rr:   apiv1.KubeResourceReservation{},
			want: &ResourceReservation{},
		},
		{
			name: "all fields set",
			rr: apiv1.KubeResourceReservation{
				CPU:              "100m",
				Memory:           "1Gi",
				EphemeralStorage: "10Gi",
			},
			want: &ResourceReservation{
				CPU:              "100m",
				Memory:           "1Gi",
				EphemeralStorage: "10Gi",
			},
		},
		{
			name: "partial fields",
			rr: apiv1.KubeResourceReservation{
				CPU:    "200m",
				Memory: "2Gi",
			},
			want: &ResourceReservation{
				CPU:    "200m",
				Memory: "2Gi",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertKubeResourceReservation(tt.rr)
			if got.CPU != tt.want.CPU || got.Memory != tt.want.Memory || got.EphemeralStorage != tt.want.EphemeralStorage {
				t.Errorf("convertKubeResourceReservation() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMarshalTOML(t *testing.T) {
	s := &SKSBootstrap{}
	config := &Config{
		Settings: Settings{
			Kubernetes: KubernetesSettings{
				APIServer:          "https://api.example.com",
				BootstrapToken:     "token123",
				CloudProvider:      "external",
				ClusterCertificate: "base64cert",
			},
		},
	}

	got, err := s.marshalTOML(config)
	if err != nil {
		t.Fatalf("marshalTOML() error = %v", err)
	}

	if len(got) == 0 {
		t.Error("marshalTOML() returned empty bytes")
	}
}

func TestCompressAndEncode(t *testing.T) {
	s := &SKSBootstrap{}
	input := []byte("test user data content")

	got, err := s.compressAndEncode(input)
	if err != nil {
		t.Fatalf("compressAndEncode() error = %v", err)
	}

	if len(got) == 0 {
		t.Error("compressAndEncode() returned empty string")
	}
}

func TestBuildConfigWithFeatureGates(t *testing.T) {
	s := &SKSBootstrap{}
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

	config := s.buildConfig(options)

	if config.Settings.Kubernetes.FeatureGates == nil {
		t.Fatal("FeatureGates should not be nil")
	}

	if len(config.Settings.Kubernetes.FeatureGates) != 3 {
		t.Errorf("Expected 3 feature gates, got %d", len(config.Settings.Kubernetes.FeatureGates))
	}

	if config.Settings.Kubernetes.FeatureGates["ImageVolume"] != true {
		t.Error("ImageVolume should be true")
	}

	if config.Settings.Kubernetes.FeatureGates["MemoryQoS"] != true {
		t.Error("MemoryQoS should be true")
	}

	if config.Settings.Kubernetes.FeatureGates["TestFeature"] != false {
		t.Error("TestFeature should be false")
	}
}

func TestBuildConfigWithoutFeatureGates(t *testing.T) {
	s := &SKSBootstrap{}
	options := &Options{
		ClusterEndpoint: "https://api.exoscale-cloud.com",
		BootstrapToken:  "token123",
		CABundle:        []byte("test-ca-bundle"),
	}

	config := s.buildConfig(options)

	if config.Settings.Kubernetes.FeatureGates != nil {
		t.Error("FeatureGates should be nil when not provided")
	}
}
