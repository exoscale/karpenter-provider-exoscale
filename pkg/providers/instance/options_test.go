package instance

import (
	"context"
	"os"
	"testing"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/egoscale/v3/credentials"
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
	ctx := context.Background()
	client, _ := egov3.NewClient(credentials.NewStaticCredentials("test", "test"))

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
			endpoint, err := getEndpoint(ctx, client, "ch-gva-2", tt.apiEndpoint, tt.apiEnvironment)
			if err != nil {
				t.Errorf("getEndpoint() error = %v", err)
				return
			}
			if string(*endpoint) != tt.want {
				t.Errorf("getEndpoint() = %v, want %v", string(*endpoint), tt.want)
			}
		})
	}
}
