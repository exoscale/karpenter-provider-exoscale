package utils

import (
	"fmt"
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

func TestGenerateBootstrapTokenID(t *testing.T) {
	tokenID, err := GenerateBootstrapTokenID()
	require.NoError(t, err)
	assert.Len(t, tokenID, 6)

	for _, char := range tokenID {
		b := byte(char)
		assert.True(t, (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f'),
			"character %c is not valid for token ID", char)
	}
}

func TestGenerateBootstrapTokenSecret(t *testing.T) {
	tokenSecret, err := GenerateBootstrapTokenSecret()
	require.NoError(t, err)
	assert.Len(t, tokenSecret, 16)

	for _, char := range tokenSecret {
		b := byte(char)
		assert.True(t, (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f'), "character %c is not valid for token secret", char)
	}
}

func TestTokenUniqueness(t *testing.T) {
	tokens := make(map[string]bool)

	for i := 0; i < 100; i++ {
		id, err := GenerateBootstrapTokenID()
		require.NoError(t, err)

		secret, err := GenerateBootstrapTokenSecret()
		require.NoError(t, err)

		token := fmt.Sprintf("%s.%s", id, secret)
		assert.False(t, tokens[token], "duplicate token generated: %s", token)
		tokens[token] = true
	}
}
