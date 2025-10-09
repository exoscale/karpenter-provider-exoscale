package cloudprovider

import (
	"context"
	"fmt"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/pkg/errors"
	"github.com/exoscale/karpenter-exoscale/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
)

func (c *CloudProvider) IsDrifted(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) (cloudprovider.DriftReason, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"method", "IsDrifted",
		"nodeClaim", nodeClaim.Name,
		"providerID", nodeClaim.Status.ProviderID,
	))
	log.FromContext(ctx).V(2).Info("checking for drift")

	nodeClass, err := c.GetNodeClass(ctx, nodeClaim)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get node class")
		return "", fmt.Errorf("failed to get node class: %w", err)
	}

	instanceID, err := utils.ParseProviderID(nodeClaim.Status.ProviderID)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to parse provider ID")
		return "", fmt.Errorf("failed to parse provider ID: %w", err)
	}

	inst, err := c.instanceProvider.Get(ctx, instanceID)
	if err != nil {
		if errors.IsInstanceNotFoundError(err) {
			log.FromContext(ctx).V(1).Info("instance not found during drift check", "instanceID", instanceID)
			return "", cloudprovider.NewNodeClaimNotFoundError(fmt.Errorf("instance %s not found", instanceID))
		}
		log.FromContext(ctx).Error(err, "failed to get instance", "instanceID", instanceID)
		return "", fmt.Errorf("failed to get instance %s: %w", instanceID, err)
	}

	// Convert instance to InstanceData for drift detection
	instanceData := &apiv1.InstanceData{
		SecurityGroups:     inst.SecurityGroups,
		PrivateNetworks:    inst.PrivateNetworks,
		AntiAffinityGroups: inst.AntiAffinityGroups,
	}
	if inst.Template != nil {
		instanceData.TemplateID = string(inst.Template.ID)
	}

	expectedTemplateID, err := c.templateResolver.ResolveTemplateID(ctx, nodeClass)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to resolve template ID")
		return "", fmt.Errorf("failed to resolve template ID: %w", err)
	}

	if instanceData.TemplateID != expectedTemplateID {
		log.FromContext(ctx).Info("detected template drift",
			"expected", expectedTemplateID,
			"actual", instanceData.TemplateID)
		return cloudprovider.DriftReason("TemplateID"), nil
	}

	if drifted, reason := nodeClass.HasSecurityGroupsDrifted(instanceData); drifted {
		log.FromContext(ctx).Info("detected security groups drift", "reason", reason)
		return cloudprovider.DriftReason(reason), nil
	}

	if drifted, reason := nodeClass.HasPrivateNetworksDrifted(instanceData); drifted {
		log.FromContext(ctx).Info("detected private networks drift", "reason", reason)
		return cloudprovider.DriftReason(reason), nil
	}

	if drifted, reason := nodeClass.HasAntiAffinityGroupsDrifted(instanceData); drifted {
		log.FromContext(ctx).Info("detected anti-affinity groups drift", "reason", reason)
		return cloudprovider.DriftReason(reason), nil
	}

	log.FromContext(ctx).V(2).Info("no drift detected")
	return "", nil
}
