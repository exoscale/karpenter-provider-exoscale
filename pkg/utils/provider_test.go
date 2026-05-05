package utils

import (
	"testing"

	v1 "k8s.io/api/core/v1"
)

func TestParseProviderID(t *testing.T) {
	tests := []struct {
		name       string
		providerID string
		want       string
		wantErr    bool
	}{
		{
			name:       "valid provider ID",
			providerID: "exoscale://a1b2c3d4-1234-5678-9abc-123456789012",
			want:       "a1b2c3d4-1234-5678-9abc-123456789012",
			wantErr:    false,
		},
		{
			name:       "invalid prefix",
			providerID: "aws://instance-id",
			want:       "",
			wantErr:    true,
		},
		{
			name:       "missing instance ID",
			providerID: "exoscale://",
			want:       "",
			wantErr:    true,
		},
		{
			name:       "invalid UUID format",
			providerID: "exoscale://not-a-uuid",
			want:       "",
			wantErr:    true,
		},
		{
			name:       "empty string",
			providerID: "",
			want:       "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseProviderID(tt.providerID)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseProviderID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseProviderID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainsIPv6Address(t *testing.T) {
	tests := []struct {
		name      string
		addresses []v1.NodeAddress
		want      bool
	}{
		{
			name: "contains valid IPv6",
			addresses: []v1.NodeAddress{
				{Type: v1.NodeInternalIP, Address: "192.168.1.1"},
				{Type: v1.NodeInternalIP, Address: "2001:db8::1"},
			},
			want: true,
		},
		{
			name: "only IPv4",
			addresses: []v1.NodeAddress{
				{Type: v1.NodeInternalIP, Address: "192.168.1.1"},
				{Type: v1.NodeExternalIP, Address: "1.2.3.4"},
			},
			want: false,
		},
		{
			name: "only IPv6",
			addresses: []v1.NodeAddress{
				{Type: v1.NodeInternalIP, Address: "fe80::1"},
			},
			want: true,
		},
		{
			name:      "empty list",
			addresses: []v1.NodeAddress{},
			want:      false,
		},
		{
			name: "invalid address is not counted as IPv6",
			addresses: []v1.NodeAddress{
				{Type: v1.NodeHostName, Address: "not:an:ip"},
			},
			want: false,
		},
		{
			name: "IPv4-mapped IPv6 address",
			addresses: []v1.NodeAddress{
				{Type: v1.NodeInternalIP, Address: "::ffff:192.0.2.1"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContainsIPv6Address(tt.addresses)
			if got != tt.want {
				t.Errorf("ContainsIPv6Address() = %v, want %v", got, tt.want)
			}
		})
	}
}
