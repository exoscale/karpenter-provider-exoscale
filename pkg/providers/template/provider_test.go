package template

import (
	"testing"
)

func TestExtractSemVer(t *testing.T) {
	tests := []struct {
		name       string
		gitVersion string
		want       string
		wantErr    bool
	}{
		{
			name:       "valid version with v prefix",
			gitVersion: "v1.27.3",
			want:       "1.27.3",
			wantErr:    false,
		},
		{
			name:       "valid version without v prefix",
			gitVersion: "1.28.0",
			want:       "1.28.0",
			wantErr:    false,
		},
		{
			name:       "version with build metadata",
			gitVersion: "v1.26.5+k3s1",
			want:       "1.26.5",
			wantErr:    false,
		},
		{
			name:       "invalid version format",
			gitVersion: "invalid",
			want:       "",
			wantErr:    true,
		},
		{
			name:       "empty string",
			gitVersion: "",
			want:       "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractSemVer(tt.gitVersion)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractSemVer() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("extractSemVer() = %v, want %v", got, tt.want)
			}
		})
	}
}
