package constants

import "time"

const (
	LabelManagedBy = "exoscale.com/managed-by"
	LabelClusterID = "exoscale.com/cluster-id"
	LabelNodeClaim = "exoscale.com/node-claim"

	ManagedByKarpenter = "karpenter"

	AnnotationBootstrapToken = "exoscale.com/bootstrap-token"
	AnnotationTokenCreated   = "exoscale.com/token-created"
	LabelTokenProvider       = "exoscale.com/token-provider"

	DefaultOperationTimeout  = 10 * time.Minute
	DefaultBootstrapTokenTTL = 10 * time.Minute

	// MaxInstancesPerAntiAffinityGroup is the Exoscale platform limit
	// for the maximum number of instances that can be in a single anti-affinity group
	MaxInstancesPerAntiAffinityGroup = 8

	ProviderName = "karpenter-exoscale"

	BootstrapTokenPrefix = "bootstrap-token-"

	BootstrapTokenExtraGroups = "system:bootstrappers:worker,system:bootstrappers:ingress"
)
