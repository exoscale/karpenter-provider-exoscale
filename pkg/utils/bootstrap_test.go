package utils

import (
	"testing"
)

func TestBootstrapToken_Token(t *testing.T) {
	tests := []struct {
		name string
		bt   *BootstrapToken
		want string
	}{
		{
			name: "concatenate token ID and secret",
			bt: &BootstrapToken{
				TokenID:     "abc123",
				TokenSecret: "def456ghi789jkl0",
			},
			want: "abc123.def456ghi789jkl0",
		},
		{
			name: "empty values",
			bt: &BootstrapToken{
				TokenID:     "",
				TokenSecret: "",
			},
			want: ".",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.bt.Token()
			if got != tt.want {
				t.Errorf("BootstrapToken.Token() = %v, want %v", got, tt.want)
			}
		})
	}
}
