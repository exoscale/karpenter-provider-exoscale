package cloudprovider

import (
	"context"
	"fmt"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/pkg/metrics"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instance"
	"github.com/exoscale/karpenter-exoscale/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	karpentermetrics "sigs.k8s.io/karpenter/pkg/metrics"
)

func (c *CloudProvider) IsDrifted(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) (cloudprovider.DriftReason, error) {
	logger := log.FromContext(ctx).WithName("cloudprovider").WithValues(
		"method", "IsDrifted",
		"nodeClaim", nodeClaim.Name,
		"providerID", nodeClaim.Status.ProviderID,
	)
	logger.V(2).Info("checking for drift")

	nodeClass, err := c.GetNodeClass(ctx, nodeClaim)
	if err != nil {
		logger.Error(err, "failed to get node class")
		return "", fmt.Errorf("failed to get node class: %w", err)
	}

	instanceID, err := utils.ParseProviderID(nodeClaim.Status.ProviderID)
	if err != nil {
		logger.Error(err, "failed to parse provider ID")
		return "", fmt.Errorf("failed to parse provider ID: %w", err)
	}

	inst, err := c.instanceProvider.Get(ctx, instanceID)
	if err != nil {
		logger.Error(err, "failed to get instance", "instanceID", instanceID)
		return "", fmt.Errorf("failed to get instance %s: %w", instanceID, err)
	}

	if reason := c.checkTemplateDrift(inst, nodeClass); reason != "" {
		logger.Info("detected template drift", "reason", reason)
		metrics.DriftDetectedTotal.Inc(map[string]string{karpentermetrics.ReasonLabel: string(reason)})
		return reason, nil
	}

	if reason := c.checkSecurityGroupsDrift(ctx, inst, nodeClass); reason != "" {
		logger.Info("detected security groups drift", "reason", reason)
		metrics.DriftDetectedTotal.Inc(map[string]string{karpentermetrics.ReasonLabel: string(reason)})
		return reason, nil
	}

	if reason := c.checkPrivateNetworksDrift(ctx, inst, nodeClass); reason != "" {
		logger.Info("detected private networks drift", "reason", reason)
		metrics.DriftDetectedTotal.Inc(map[string]string{karpentermetrics.ReasonLabel: string(reason)})
		return reason, nil
	}

	if reason := c.checkAntiAffinityGroupsDrift(ctx, inst, nodeClass); reason != "" {
		logger.Info("detected anti-affinity groups drift", "reason", reason)
		metrics.DriftDetectedTotal.Inc(map[string]string{karpentermetrics.ReasonLabel: string(reason)})
		return reason, nil
	}

	logger.V(2).Info("no drift detected")
	return "", nil
}

func (c *CloudProvider) checkTemplateDrift(instance *instance.Instance, nodeClass *apiv1.ExoscaleNodeClass) cloudprovider.DriftReason {
	if instance.Template == nil {
		return ""
	}

	if string(instance.Template.ID) != nodeClass.Spec.TemplateID {
		return "TemplateID"
	}

	return ""
}

func (c *CloudProvider) checkSecurityGroupsDrift(ctx context.Context, instance *instance.Instance, nodeClass *apiv1.ExoscaleNodeClass) cloudprovider.DriftReason {
	logger := log.FromContext(ctx).WithName("cloudprovider").WithValues(
		"method", "checkSecurityGroupsDrift",
	)

	expectedSGs := utils.ToStringSet(nodeClass.Spec.SecurityGroups)
	actualSGs := utils.ToStringSet(instance.SecurityGroups)

	if !utils.CompareSets(expectedSGs, actualSGs) {
		logger.V(1).Info("security groups mismatch",
			"expected", len(expectedSGs),
			"actual", len(actualSGs))
		return "SecurityGroups"
	}

	return ""
}

func (c *CloudProvider) checkPrivateNetworksDrift(ctx context.Context, instance *instance.Instance, nodeClass *apiv1.ExoscaleNodeClass) cloudprovider.DriftReason {
	logger := log.FromContext(ctx).WithName("cloudprovider").WithValues(
		"method", "checkPrivateNetworksDrift",
	)

	expectedNetworks := utils.ToStringSet(nodeClass.Spec.PrivateNetworks)
	actualNetworks := utils.ToStringSetFiltered(instance.PrivateNetworks)

	if !utils.CompareSets(expectedNetworks, actualNetworks) {
		logger.V(1).Info("private networks mismatch",
			"expected", len(expectedNetworks),
			"actual", len(actualNetworks))
		return "PrivateNetworks"
	}

	return ""
}

func (c *CloudProvider) checkAntiAffinityGroupsDrift(ctx context.Context, instance *instance.Instance, nodeClass *apiv1.ExoscaleNodeClass) cloudprovider.DriftReason {
	logger := log.FromContext(ctx).WithName("cloudprovider").WithValues(
		"method", "checkAntiAffinityGroupsDrift",
	)

	expectedAAGs := utils.ToStringSet(nodeClass.Spec.AntiAffinityGroups)
	actualAAGs := utils.ToStringSet(instance.AntiAffinityGroups)

	if !utils.CompareSets(expectedAAGs, actualAAGs) {
		logger.V(1).Info("anti-affinity groups mismatch",
			"expected", len(expectedAAGs),
			"actual", len(actualAAGs))
		return "AntiAffinityGroups"
	}

	return ""
}
