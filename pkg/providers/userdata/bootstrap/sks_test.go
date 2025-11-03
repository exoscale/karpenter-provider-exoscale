package bootstrap

import (
	"testing"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
)

func TestIsEmptyResourceReservation(t *testing.T) {
	tests := []struct {
		name string
		rr   apiv1.ResourceReservation
		want bool
	}{
		{
			name: "empty reservation",
			rr:   apiv1.ResourceReservation{},
			want: true,
		},
		{
			name: "only CPU set",
			rr: apiv1.ResourceReservation{
				CPU: "100m",
			},
			want: false,
		},
		{
			name: "only Memory set",
			rr: apiv1.ResourceReservation{
				Memory: "1Gi",
			},
			want: false,
		},
		{
			name: "only EphemeralStorage set",
			rr: apiv1.ResourceReservation{
				EphemeralStorage: "10Gi",
			},
			want: false,
		},
		{
			name: "all fields set",
			rr: apiv1.ResourceReservation{
				CPU:              "100m",
				Memory:           "1Gi",
				EphemeralStorage: "10Gi",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isEmptyResourceReservation(tt.rr)
			if got != tt.want {
				t.Errorf("isEmptyResourceReservation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertResourceReservation(t *testing.T) {
	tests := []struct {
		name string
		rr   apiv1.ResourceReservation
		want *ResourceReservation
	}{
		{
			name: "empty reservation",
			rr:   apiv1.ResourceReservation{},
			want: &ResourceReservation{},
		},
		{
			name: "all fields set",
			rr: apiv1.ResourceReservation{
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
			rr: apiv1.ResourceReservation{
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
			got := convertResourceReservation(tt.rr)
			if got.CPU != tt.want.CPU || got.Memory != tt.want.Memory || got.EphemeralStorage != tt.want.EphemeralStorage {
				t.Errorf("convertResourceReservation() = %+v, want %+v", got, tt.want)
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
