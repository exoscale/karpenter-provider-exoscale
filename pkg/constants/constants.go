package constants

import "time"

const (
	LabelManagedBy   = "exoscale.com/managed-by"
	LabelClusterName = "exoscale.com/cluster-name"
	LabelNodeClaim   = "exoscale.com/node-claim"

	ManagedByKarpenter = "karpenter"

	AnnotationBootstrapToken = "exoscale.com/bootstrap-token"
	AnnotationTokenCreated   = "exoscale.com/token-created"
	LabelTokenProvider       = "exoscale.com/token-provider"

	DefaultOperationTimeout  = 10 * time.Minute
	DefaultBootstrapTokenTTL = 10 * time.Minute

	ProviderName = "karpenter-exoscale"

	BootstrapTokenPrefix = "bootstrap-token-"

	BootstrapTokenExtraGroups = "system:bootstrappers:worker,system:bootstrappers:ingress"
)
