package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

func GenerateSecureRandomString(length int) (string, error) {
	b := make([]byte, length)
	for i := 0; i < 3; i++ {
		if _, err := rand.Read(b); err == nil {
			return hex.EncodeToString(b)[:length], nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return "", fmt.Errorf("failed to generate secure random string: crypto/rand unavailable")
}
