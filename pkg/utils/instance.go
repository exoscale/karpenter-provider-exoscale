package utils

import "fmt"

func GenerateInstanceName(clusterID string, nodeClaimName string) string {
	return fmt.Sprintf("%s-%s", clusterID, nodeClaimName)
}
