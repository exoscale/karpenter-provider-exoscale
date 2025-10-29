package utils

import "fmt"

func GenerateInstanceName(prefix string, nodeClaimName string) string {
	if prefix == "" {
		return nodeClaimName
	}
	return fmt.Sprintf("%s-%s", prefix, nodeClaimName)
}
