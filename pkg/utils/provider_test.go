package utils

import (
	"testing"
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
