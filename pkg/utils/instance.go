package utils

import "fmt"

func GenerateInstanceName(prefix string, nodeClaimName string) string {
	return fmt.Sprintf("%s-%s", prefix, nodeClaimName)
}
