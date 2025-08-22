package instancetype

import (
	"context"
	"testing"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
)

func TestBuildInstanceTypes(t *testing.T) {
	ctx := context.Background()

	provider := &exoscaleProvider{
		zone:            "ch-gva-2",
		instanceTypeMap: make(map[string]*cloudprovider.InstanceType),
		instanceIDMap:   make(map[string]string),
		prices:          make(map[string]float64),
	}

	provider.prices["standard.medium"] = 0.05
	provider.prices["standard.large"] = 0.10
	provider.prices["standard.extra-large"] = 0.20

	trueVal := true
	exoTypes := &egov3.ListInstanceTypesResponse{
		InstanceTypes: []egov3.InstanceType{
			{
				ID:         "xl-id",
				Family:     "standard",
				Size:       "extra-large",
				Cpus:       4,
				Memory:     16 * 1024 * 1024 * 1024, // 16GB
				Authorized: &trueVal,
			},
			{
				ID:         "large-id",
				Family:     "standard",
				Size:       "large",
				Cpus:       2,
				Memory:     8 * 1024 * 1024 * 1024, // 8GB
				Authorized: &trueVal,
			},
			{
				ID:         "medium-id",
				Family:     "standard",
				Size:       "medium",
				Cpus:       2,
				Memory:     4 * 1024 * 1024 * 1024, // 4GB
				Authorized: &trueVal,
			},
		},
	}

	instanceTypes, instanceTypeMap, instanceIDMap := provider.buildInstanceTypes(ctx, exoTypes)

	assert.Equal(t, 3, len(instanceTypes))
	assert.Equal(t, 3, len(instanceTypeMap))
	assert.Equal(t, 3, len(instanceIDMap))

	assert.Contains(t, instanceTypeMap, "standard.medium")
	assert.Contains(t, instanceTypeMap, "standard.large")
	assert.Contains(t, instanceTypeMap, "standard.extra-large")

	assert.Equal(t, "medium-id", instanceIDMap["standard.medium"])
	assert.Equal(t, "large-id", instanceIDMap["standard.large"])
	assert.Equal(t, "xl-id", instanceIDMap["standard.extra-large"])
}

func TestBuildInstanceTypes_WithCapacity(t *testing.T) {
	ctx := context.Background()

	provider := &exoscaleProvider{
		zone:            "ch-gva-2",
		instanceTypeMap: make(map[string]*cloudprovider.InstanceType),
		instanceIDMap:   make(map[string]string),
		prices:          make(map[string]float64),
	}
	// Set test prices
	provider.prices["standard.small"] = 0.10
	provider.prices["standard.medium"] = 0.10
	provider.prices["standard.large"] = 0.10

	trueVal := true
	exoTypes := &egov3.ListInstanceTypesResponse{
		InstanceTypes: []egov3.InstanceType{
			{
				ID:         "large-id",
				Family:     "standard",
				Size:       "large",
				Cpus:       4,
				Memory:     8 * 1024 * 1024 * 1024,
				Authorized: &trueVal,
			},
			{
				ID:         "small-id",
				Family:     "standard",
				Size:       "small",
				Cpus:       1,
				Memory:     2 * 1024 * 1024 * 1024,
				Authorized: &trueVal,
			},
			{
				ID:         "medium-id",
				Family:     "standard",
				Size:       "medium",
				Cpus:       2,
				Memory:     4 * 1024 * 1024 * 1024,
				Authorized: &trueVal,
			},
		},
	}

	instanceTypes, instanceTypeMap, _ := provider.buildInstanceTypes(ctx, exoTypes)

	assert.Equal(t, 3, len(instanceTypes))

	smallType := instanceTypeMap["standard.small"]
	assert.NotNil(t, smallType)
	cpuSmall := smallType.Capacity[corev1.ResourceCPU]
	assert.Equal(t, int64(1), cpuSmall.Value())

	mediumType := instanceTypeMap["standard.medium"]
	assert.NotNil(t, mediumType)
	cpuMedium := mediumType.Capacity[corev1.ResourceCPU]
	assert.Equal(t, int64(2), cpuMedium.Value())

	largeType := instanceTypeMap["standard.large"]
	assert.NotNil(t, largeType)
	cpuLarge := largeType.Capacity[corev1.ResourceCPU]
	assert.Equal(t, int64(4), cpuLarge.Value())
}
