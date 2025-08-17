package utils

import (
	"testing"
)

func TestParseProviderID(t *testing.T) {
	tests := []struct {
		name       string
		providerID string
		wantID     string
		wantErr    bool
	}{
		{
			name:       "valid provider ID",
			providerID: "exoscale://123e4567-e89b-12d3-a456-426614174000",
			wantID:     "123e4567-e89b-12d3-a456-426614174000",
			wantErr:    false,
		},
		{
			name:       "missing prefix",
			providerID: "123e4567-e89b-12d3-a456-426614174000",
			wantErr:    true,
		},
		{
			name:       "wrong prefix",
			providerID: "plouf://123e4567-e89b-12d3-a456-426614174000",
			wantErr:    true,
		},
		{
			name:       "empty provider ID",
			providerID: "",
			wantErr:    true,
		},
		{
			name:       "prefix only",
			providerID: "exoscale://",
			wantErr:    true,
		},
		{
			name:       "invalid UUID format",
			providerID: "exoscale://not-a-uuid",
			wantErr:    true,
		},
		{
			name:       "UUID with uppercase letters",
			providerID: "exoscale://123E4567-E89B-12D3-A456-426614174000",
			wantErr:    true,
		},
		{
			name:       "UUID missing segments",
			providerID: "exoscale://123e4567-e89b-12d3-a456",
			wantErr:    true,
		},
		{
			name:       "UUID with extra characters",
			providerID: "exoscale://123e4567-e89b-12d3-a456-426614174000-extra",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, err := ParseProviderID(tt.providerID)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseProviderID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotID != tt.wantID {
				t.Errorf("ParseProviderID() = %v, want %v", gotID, tt.wantID)
			}
		})
	}
}

func TestFormatProviderID(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		want       string
	}{
		{
			name:       "valid UUID",
			instanceID: "123e4567-e89b-12d3-a456-426614174000",
			want:       "exoscale://123e4567-e89b-12d3-a456-426614174000",
		},
		{
			name:       "empty string",
			instanceID: "",
			want:       "exoscale://",
		},
		{
			name:       "any string gets prefixed",
			instanceID: "any-string",
			want:       "exoscale://any-string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatProviderID(tt.instanceID); got != tt.want {
				t.Errorf("FormatProviderID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProviderIDRoundTrip(t *testing.T) {
	instanceID := "123e4567-e89b-12d3-a456-426614174000"

	formatted := FormatProviderID(instanceID)
	parsed, err := ParseProviderID(formatted)

	if err != nil {
		t.Errorf("Round trip failed: %v", err)
	}

	if parsed != instanceID {
		t.Errorf("Round trip mismatch: got %v, want %v", parsed, instanceID)
	}
}
