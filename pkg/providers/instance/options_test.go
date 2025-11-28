package instance

import (
	"os"
	"testing"
)

func TestGetRequiredEnv(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr bool
	}{
		{
			name:    "env var set",
			key:     "TEST_VAR_EXISTS",
			value:   "test-value",
			wantErr: false,
		},
		{
			name:    "env var not set",
			key:     "TEST_VAR_NOT_EXISTS",
			value:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != "" {
				os.Setenv(tt.key, tt.value)
				defer os.Unsetenv(tt.key)
			}

			got, err := getRequiredEnv(tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("getRequiredEnv() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && got != tt.value {
				t.Errorf("getRequiredEnv() = %v, want %v", got, tt.value)
			}
		})
	}
}

func TestGetEndpoint(t *testing.T) {
	tests := []struct {
		name           string
		apiEndpoint    string
		apiEnvironment string
		want           string
	}{
		{
			name:           "custom endpoint",
			apiEndpoint:    "https://custom.exoscale.com/v2",
			apiEnvironment: "",
			want:           "https://custom.exoscale.com/v2",
		},
		{
			name:           "ppapi environment",
			apiEndpoint:    "",
			apiEnvironment: "ppapi",
			want:           "https://ppapi-ch-gva-2.exoscale.com/v2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := getEndpoint(tt.apiEndpoint, tt.apiEnvironment)
			if string(*endpoint) != tt.want {
				t.Errorf("getEndpoint() = %v, want %v", string(*endpoint), tt.want)
			}
		})
	}
}
