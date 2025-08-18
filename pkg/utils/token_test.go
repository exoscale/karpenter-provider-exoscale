package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateSecureRandomString(t *testing.T) {
	tests := []struct {
		name   string
		length int
	}{
		{"6 chars", 6},
		{"16 chars", 16},
		{"32 chars", 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GenerateSecureRandomString(tt.length)
			require.NoError(t, err)
			assert.Len(t, result, tt.length)

			for _, char := range result {
				b := byte(char)
				assert.True(t, (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f'),
					"character %c is not a valid hex character", char)
			}
		})
	}
}

func TestTokenUniqueness(t *testing.T) {
	tokens := make(map[string]bool)

	for i := 0; i < 100; i++ {
		id, err := GenerateSecureRandomString(6)
		require.NoError(t, err)

		assert.False(t, tokens[id], "duplicate id generated: %s", id)
		tokens[id] = true
	}
}
