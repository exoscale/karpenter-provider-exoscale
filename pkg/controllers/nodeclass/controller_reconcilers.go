package nodeclass

import (
	"context"
	"fmt"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *ExoscaleNodeClassReconciler) reconcileTemplate(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("nodeclass", nodeClass.Name))

	t, err := r.TemplateResolver.ResolveTemplate(ctx, nodeClass)
	if err != nil {
		return fmt.Errorf("failed to resolve template ID: %w", err)
	}

	log.FromContext(ctx).V(1).Info("verifying template", "templateID", t.ID)
	if _, err := r.ExoscaleClient.GetTemplate(ctx, egov3.UUID(t.ID)); err != nil {
		return fmt.Errorf("template %s not found or not accessible: %w", t.ID, err)
	}

	return nil
}

func (r *ExoscaleNodeClassReconciler) reconcileSecurityGroups(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	sgIDs := []string{}

	// Deprecated field, use selector instead now
	for _, sgID := range nodeClass.Spec.SecurityGroups {
		log.FromContext(ctx).V(1).Info("resolving security group", "securityGroupID", sgID)
		sg, err := r.ExoscaleClient.GetSecurityGroup(ctx, egov3.UUID(sgID))
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get security group", "securityGroupID", sgID)
			return fmt.Errorf("failed to get security group %s: %w", sgID, err)
		}
		sgIDs = append(sgIDs, sg.ID.String())
	}

	for _, selector := range nodeClass.Spec.SecurityGroupSelectorTerms {
		var sg *egov3.SecurityGroup
		var err error
		if selector.ID != "" {
			log.FromContext(ctx).V(1).Info("resolving security group by ID", "securityGroupID", selector.ID)
			sg, err = r.ExoscaleClient.GetSecurityGroup(ctx, egov3.UUID(selector.ID))
		} else if selector.Name != "" {
			log.FromContext(ctx).V(1).Info("resolving security group by Name", "securityGroupName", selector.Name)
			// Discover security group by name
			sg, err = r.getCachedSecurityGroupByName(ctx, selector.Name)
			if err != nil {
				log.FromContext(ctx).Error(err, "failed to get security group by name")
				return fmt.Errorf("failed to get security group by name: %w", err)
			}

			if sg == nil {
				err = fmt.Errorf("security group with name %s not found", selector.Name)
			}
		}
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get security group", "selector", selector)
			return fmt.Errorf("failed to get security group for selector %+v: %w", selector, err)
		}

		sgIDs = append(sgIDs, sg.ID.String())
	}

	nodeClass.Status.SecurityGroups = sgIDs
	return nil
}

func (r *ExoscaleNodeClassReconciler) reconcileAntiAffinityGroups(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	aagIDs := []string{}

	// Deprecated field, use selector instead now
	for _, aagID := range nodeClass.Spec.AntiAffinityGroups {
		log.FromContext(ctx).V(1).Info("resolving anti-affinity group", "antiAffinityGroupID", aagID)
		aag, err := r.ExoscaleClient.GetAntiAffinityGroup(ctx, egov3.UUID(aagID))
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get anti-affinity group", "antiAffinityGroupID", aagID)
			return fmt.Errorf("failed to get anti-affinity group %s: %w", aagID, err)
		}
		aagIDs = append(aagIDs, aag.ID.String())
	}

	for _, selector := range nodeClass.Spec.AntiAffinityGroupSelectorTerms {
		var aag *egov3.AntiAffinityGroup
		var err error
		if selector.ID != "" {
			log.FromContext(ctx).V(1).Info("resolving anti-affinity group by ID", "antiAffinityGroupID", selector.ID)
			aag, err = r.ExoscaleClient.GetAntiAffinityGroup(ctx, egov3.UUID(selector.ID))
		} else if selector.Name != "" {
			log.FromContext(ctx).V(1).Info("resolving anti-affinity group by Name", "antiAffinityGroupName", selector.Name)
			// Discover anti-affinity group by name
			aag, err = r.getCachedAntiAffinityGroupByName(ctx, selector.Name)
			if err != nil {
				log.FromContext(ctx).Error(err, "failed to get anti-affinity group by name")
				return fmt.Errorf("failed to get anti-affinity group by name: %w", err)
			}

			if aag == nil {
				err = fmt.Errorf("anti-affinity group with name %s not found", selector.Name)
			}
		}
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get anti-affinity group", "selector", selector)
			return fmt.Errorf("failed to get anti-affinity group for selector %+v: %w", selector, err)
		}

		aagIDs = append(aagIDs, aag.ID.String())
	}

	nodeClass.Status.AntiAffinityGroups = aagIDs
	return nil
}

func (r *ExoscaleNodeClassReconciler) reconcilePrivateNetworks(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	privNetIDs := []string{}

	// Deprecated field, use selector instead now
	for _, netID := range nodeClass.Spec.PrivateNetworks {
		log.FromContext(ctx).V(1).Info("resolving private network", "privateNetworkID", netID)
		net, err := r.ExoscaleClient.GetPrivateNetwork(ctx, egov3.UUID(netID))
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get private network", "privateNetworkID", netID)
			return fmt.Errorf("failed to get private network %s: %w", netID, err)
		}
		privNetIDs = append(privNetIDs, net.ID.String())
	}

	for _, selector := range nodeClass.Spec.PrivateNetworkSelectorTerms {
		var net *egov3.PrivateNetwork
		var err error
		if selector.ID != "" {
			log.FromContext(ctx).V(1).Info("resolving private network by ID", "privateNetworkID", selector.ID)
			net, err = r.ExoscaleClient.GetPrivateNetwork(ctx, egov3.UUID(selector.ID))
		} else if selector.Name != "" {
			log.FromContext(ctx).V(1).Info("resolving private network by Name", "privateNetworkName", selector.Name)
			// Discover private network by name
			net, err = r.getCachedPrivateNetworkByName(ctx, selector.Name)
			if err != nil {
				log.FromContext(ctx).Error(err, "failed to get private network by name")
				return fmt.Errorf("failed to get private network by name: %w", err)
			}

			if net == nil {
				err = fmt.Errorf("private network with name %s not found", selector.Name)
			}
		}
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to get private network", "selector", selector)
			return fmt.Errorf("failed to get private network for selector %+v: %w", selector, err)
		}

		privNetIDs = append(privNetIDs, net.ID.String())
	}

	nodeClass.Status.PrivateNetworks = privNetIDs
	return nil
}
