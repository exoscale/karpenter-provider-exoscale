package constants

import "time"

const (
	InstanceLabelManagedBy    = "exoscale.com/managed-by"
	InstanceLabelClusterID    = "exoscale.com/cluster-id"
	InstanceLabelNodeClaim    = "exoscale.com/node-claim"
	InstanceLabelNodepoolName = "exoscale.com/nodepool-name"

	ManagedByKarpenter = "karpenter"

	AnnotationBootstrapToken = "exoscale.com/bootstrap-token"
	AnnotationTokenCreated   = "exoscale.com/token-created"
	LabelTokenProvider       = "exoscale.com/token-provider"

	LabelInstanceFamily = "exoscale.com/instance-family"
	LabelInstanceSize   = "exoscale.com/instance-size"

	DefaultOperationTimeout  = 10 * time.Minute
	DefaultBootstrapTokenTTL = 10 * time.Minute

	// MaxInstancesPerAntiAffinityGroup is the Exoscale platform limit
	// for the maximum number of instances that can be in a single anti-affinity group
	MaxInstancesPerAntiAffinityGroup = 8

	ProviderName = "karpenter-exoscale"
)
