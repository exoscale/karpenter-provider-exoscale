package instance

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instancetype"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/template"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/userdata"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/scheduling"
)

type Provider struct {
	exoClient            *egov3.Client
	instanceTypeProvider *instancetype.Provider
	templateProvider     *template.Provider
	userdataProvider     *userdata.Provider
	options              *Options
}

func NewProvider(
	exoClient *egov3.Client,
	instanceTypeProvider *instancetype.Provider,
	templateProvider *template.Provider,
	userdataProvider *userdata.Provider,
	options *Options,
) *Provider {
	return &Provider{
		exoClient:            exoClient,
		instanceTypeProvider: instanceTypeProvider,
		templateProvider:     templateProvider,
		userdataProvider:     userdataProvider,
		options:              options,
	}
}

func (p *Provider) Create(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass, nodeClaim *karpenterv1.NodeClaim, bootstrapToken string) (*Instance, error) {
	userData, err := p.buildUserdata(ctx, nodeClass, nodeClaim, bootstrapToken)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("nodeClaim", nodeClaim.Name))

	requirements := scheduling.NewNodeSelectorRequirementsWithMinValues(nodeClaim.Spec.Requirements...)

	osRequirement := requirements.Get(corev1.LabelOSStable)
	if osRequirement != nil && !osRequirement.Has(string(corev1.Linux)) {
		return nil, fmt.Errorf("unsupported os requirement specified: %s", osRequirement)
	}

	archRequirement := requirements.Get(corev1.LabelArchStable)
	if archRequirement != nil && !archRequirement.Has("amd64") {
		return nil, fmt.Errorf("unsupported architecture requirement: %s", archRequirement)
	}

	instanceName := nodeClaim.Name
	if p.GetInstancePrefix() != "" {
		instanceName = p.GetInstancePrefix() + instanceName
	}

	instanceTypeRequirement := requirements.Get(corev1.LabelInstanceTypeStable)

	if instanceTypeRequirement == nil || len(instanceTypeRequirement.Values()) == 0 {
		return nil, fmt.Errorf("no instance type requirement specified")
	}

	instanceTypes := instanceTypeRequirement.Values()
	prices := make(map[string]float64)
	for _, typeName := range instanceTypes {
		instanceType, err := p.instanceTypeProvider.GetByName(typeName)
		if err == nil && instanceType != nil && len(instanceType.Offerings) > 0 {
			prices[typeName] = instanceType.Offerings[0].Price
		}
	}

	instanceTypeName := findCheapestInstanceType(instanceTypes, prices)
	instanceTypeID, found := p.instanceTypeProvider.GetIDForName(instanceTypeName)
	if !found {
		return nil, fmt.Errorf("failed to find instance type ID for %s", instanceTypeName)
	}

	if err := p.checkAntiAffinityGroups(ctx, nodeClass.Status.AntiAffinityGroups); err != nil {
		return nil, fmt.Errorf("anti-affinity group capacity check failed: %w", err)
	}

	t, err := p.templateProvider.ResolveTemplate(ctx, nodeClass)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve template ID: %w", err)
	}

	createRequest := egov3.CreateInstanceRequest{
		Name:               instanceName,
		InstanceType:       &egov3.InstanceType{ID: egov3.UUID(instanceTypeID)},
		Template:           &egov3.Template{ID: egov3.UUID(t.ID)},
		DiskSize:           nodeClass.Spec.DiskSize,
		UserData:           userData,
		Labels:             p.GenerateInstanceLabels(nodeClaim),
		SecurityGroups:     p.convertSecurityGroups(nodeClass.Status.SecurityGroups),
		AntiAffinityGroups: p.convertAntiAffinityGroups(nodeClass.Status.AntiAffinityGroups),
	}

	createCtx, cancel := context.WithTimeout(ctx, constants.DefaultOperationTimeout)
	defer cancel()

	operation, err := p.exoClient.CreateInstance(createCtx, createRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to create instance: %w", err)
	}

	log.FromContext(ctx).Info("instance creation started", "operationID", operation.ID)
	createdInstance, err := p.exoClient.GetInstance(ctx, operation.Reference.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get created instance: %w", err)
	}

	// Hydrate the instance type
	createdInstance.InstanceType = &egov3.InstanceType{}
	instanceType, err := p.instanceTypeProvider.GetByName(instanceTypeName)
	if err == nil && instanceType != nil {
		parts := strings.SplitN(instanceTypeName, ".", 2)
		if len(parts) == 2 {
			createdInstance.InstanceType.Family = egov3.InstanceTypeFamily(parts[0])
			createdInstance.InstanceType.Size = egov3.InstanceTypeSize(parts[1])
		}

		if instanceType.Capacity != nil {
			if cpuQuantity, ok := instanceType.Capacity[corev1.ResourceCPU]; ok {
				createdInstance.InstanceType.Cpus = cpuQuantity.Value()
			}
			if memQuantity, ok := instanceType.Capacity[corev1.ResourceMemory]; ok {
				createdInstance.InstanceType.Memory = memQuantity.Value()
			}
			if gpuQuantity, ok := instanceType.Capacity[instancetype.ResourceNvidiaGPU]; ok {
				createdInstance.InstanceType.Gpus = gpuQuantity.Value()
			}
		}
	}

	if len(nodeClass.Status.PrivateNetworks) > 0 {
		if err := p.attachPrivateNetworks(ctx, createdInstance.ID, nodeClass.Status.PrivateNetworks); err != nil {
			log.FromContext(ctx).Error(err, "failed to attach private networks, cleaning up instance")

			deleteErr := p.Delete(ctx, createdInstance.ID.String())
			if deleteErr != nil {
				log.FromContext(ctx).Error(deleteErr, "failed to delete instance after network attachment failure")
			}

			return nil, fmt.Errorf("failed to attach private networks to instance %s: %w", createdInstance.ID.String(), err)
		}
	}

	instanceType, err = p.instanceTypeProvider.GetByName(instanceTypeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance type %s: %w", createdInstance.InstanceType.ID, err)
	}

	instance := FromExoscaleInstance(createdInstance, instanceType, p.options.Zone)
	log.FromContext(ctx).Info("instance created successfully", "instanceID", instance.ID, "duration", time.Since(start))

	return instance, nil
}

func (p *Provider) Get(ctx context.Context, id string) (*Instance, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("instanceID", id))

	instance, err := p.exoClient.GetInstance(ctx, egov3.UUID(id))
	if err != nil {
		if p.IsNotFoundError(err) {
			return nil, err // return raw typed error, callers will handle this type
		}
		return nil, fmt.Errorf("failed to get instance %s: %w", id, err)
	}

	instanceType, err := p.instanceTypeProvider.GetByID(string(instance.InstanceType.ID))
	if err != nil {
		return nil, fmt.Errorf("failed to get instance type %s: %w", instance.InstanceType.ID, err)
	}

	result := FromExoscaleInstance(instance, instanceType, p.options.Zone)
	return result, nil
}

func (p *Provider) List(ctx context.Context) ([]*Instance, error) {
	instances, err := p.exoClient.ListInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list instances: %w", err)
	}

	var result []*Instance
	for _, instance := range instances.Instances {
		// Filter only instances managed by this Karpenter cluster
		if instance.Labels == nil {
			continue
		}
		managedBy, hasManagedBy := instance.Labels[constants.InstanceLabelManagedBy]
		clusterID, hasClusterName := instance.Labels[constants.InstanceLabelClusterID]

		if !hasManagedBy || managedBy != constants.ManagedByKarpenter {
			continue
		}
		if !hasClusterName || clusterID != p.options.ClusterID {
			continue
		}

		// List API doesn't return AntiAffinityGroups, so we need to fetch full instance details
		// to ensure drift detection works correctly
		fullInstance, err := p.exoClient.GetInstance(ctx, instance.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get full instance details for %s: %w", instance.ID, err)
		}

		instanceType, err := p.instanceTypeProvider.GetByID(string(instance.InstanceType.ID))
		if err != nil {
			return nil, fmt.Errorf("failed to get instance type %s: %w", instance.InstanceType.ID, err)
		}

		inst := FromExoscaleInstance(fullInstance, instanceType, p.options.Zone)
		result = append(result, inst)
	}

	return result, nil
}

func (p *Provider) Delete(ctx context.Context, id string) error {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("instanceID", id))

	deleteCtx, cancel := context.WithTimeout(ctx, constants.DefaultOperationTimeout)
	defer cancel()

	log.FromContext(ctx).Info("deleting instance")

	operation, err := p.exoClient.DeleteInstance(deleteCtx, egov3.UUID(id))
	if err != nil {
		if p.IsNotFoundError(err) {
			log.FromContext(ctx).Info("instance not found, nothing to delete")
			return err
		}
		log.FromContext(ctx).Error(err, "failed to delete instance")
		return fmt.Errorf("failed to delete instance: %w", err)
	}

	// Wait for deletion to complete
	if operation != nil {
		log.FromContext(ctx).Info("waiting for delete operation to complete", "operationID", operation.ID)
		_, err = p.exoClient.Wait(deleteCtx, operation, egov3.OperationStateSuccess)
		if err != nil {
			log.FromContext(ctx).Error(err, "failed waiting for instance deletion", "operationID", operation.ID)
			return fmt.Errorf("failed waiting for instance deletion: %w", err)
		}
		log.FromContext(ctx).Info("instance deleted successfully", "operationID", operation.ID)
	}

	return nil
}

func (p *Provider) UpdateTags(ctx context.Context, id string, tags map[string]string) error {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("instanceID", id))

	instance, err := p.exoClient.GetInstance(ctx, egov3.UUID(id))
	if err != nil {
		if p.IsNotFoundError(err) {
			return fmt.Errorf("instance %s not found", id)
		}
		return fmt.Errorf("failed to get instance %s for tag update: %w", id, err)
	}

	updatedLabels := make(map[string]string, len(instance.Labels)+len(tags))
	for k, v := range instance.Labels {
		updatedLabels[k] = v
	}
	for k, v := range tags {
		updatedLabels[k] = v
	}

	updateRequest := egov3.UpdateInstanceRequest{
		Labels: updatedLabels,
	}

	operation, err := p.exoClient.UpdateInstance(ctx, egov3.UUID(id), updateRequest)
	if err != nil {
		return fmt.Errorf("failed to update instance labels: %w", err)
	}

	if operation != nil {
		_, err = p.exoClient.Wait(ctx, operation, egov3.OperationStateSuccess)
		if err != nil {
			return fmt.Errorf("failed waiting for instance label update: %w", err)
		}
	}

	log.FromContext(ctx).Info("instance labels updated successfully", "tagsCount", len(tags))
	return nil
}

func (p *Provider) GenerateInstanceLabels(nodeClaim *karpenterv1.NodeClaim) map[string]string {
	labels := map[string]string{
		constants.InstanceLabelManagedBy: constants.ManagedByKarpenter,
		constants.InstanceLabelClusterID: p.options.ClusterID,
		constants.InstanceLabelNodeClaim: nodeClaim.Name,
	}

	// used in kubectl output, see
	// https://github.com/kubernetes-sigs/karpenter/blob/v1.8.0/pkg/apis/crds/karpenter.sh_nodeclaims.yaml#L46
	if np, ok := nodeClaim.ObjectMeta.Labels[karpenterv1.NodePoolLabelKey]; ok {
		labels[constants.InstanceLabelNodepoolName] = np
	}

	return labels
}

func (p *Provider) GetInstancePrefix() string {
	return p.options.InstancePrefix
}

func (p *Provider) buildUserdata(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass, nodeClaim *karpenterv1.NodeClaim, bootstrapToken string) (string, error) {
	userdataOptions := userdata.NewOptions(nodeClass, nodeClaim)
	userdataOptions.ClusterEndpoint = p.options.ClusterEndpoint
	userdataOptions.ClusterDomain = p.options.ClusterDomain
	userdataOptions.BootstrapToken = bootstrapToken

	userData, err := p.userdataProvider.Generate(ctx, nodeClass, nodeClaim, userdataOptions)
	if err != nil {
		return "", cloudprovider.NewCreateError(err, "UserDataGenerationFailed", fmt.Sprintf("failed to generate user data: %s", err))
	}

	return userData, nil
}

func (p *Provider) attachPrivateNetworks(ctx context.Context, instanceID egov3.UUID, networkIDs []string) error {
	for _, networkID := range networkIDs {
		if err := p.attachPrivateNetwork(ctx, instanceID, networkID); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) attachPrivateNetwork(ctx context.Context, instanceID egov3.UUID, networkID string) error {
	attachCtx, cancel := context.WithTimeout(ctx, constants.DefaultOperationTimeout)
	defer cancel()

	operation, err := p.exoClient.AttachInstanceToPrivateNetwork(attachCtx, egov3.UUID(networkID), egov3.AttachInstanceToPrivateNetworkRequest{
		Instance: &egov3.AttachInstanceToPrivateNetworkRequestInstance{
			ID: instanceID,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to attach private network %s: %w", networkID, err)
	}

	if operation != nil {
		_, err = p.exoClient.Wait(attachCtx, operation, egov3.OperationStateSuccess)
		if err != nil {
			return fmt.Errorf("failed waiting for network attachment %s: %w", networkID, err)
		}
	}

	return nil
}

func findCheapestInstanceType(instanceTypes []string, prices map[string]float64) string {
	if len(instanceTypes) == 0 {
		return ""
	}
	if len(instanceTypes) == 1 {
		return instanceTypes[0]
	}

	cheapest := instanceTypes[0]
	cheapestPrice := prices[instanceTypes[0]]
	foundValidPrice := false

	for _, typeName := range instanceTypes {
		if price, exists := prices[typeName]; exists {
			if !foundValidPrice || price < cheapestPrice {
				cheapestPrice = price
				cheapest = typeName
				foundValidPrice = true
			}
		}
	}

	return cheapest
}

func (p *Provider) IsNotFoundError(err error) bool {
	return stderrors.Is(err, egov3.ErrNotFound)
}

func (p *Provider) convertSecurityGroups(ids []string) []egov3.SecurityGroup {
	result := make([]egov3.SecurityGroup, len(ids))
	for i, id := range ids {
		result[i] = egov3.SecurityGroup{ID: egov3.UUID(id)}
	}
	return result
}

func (p *Provider) convertAntiAffinityGroups(ids []string) []egov3.AntiAffinityGroup {
	result := make([]egov3.AntiAffinityGroup, len(ids))
	for i, id := range ids {
		result[i] = egov3.AntiAffinityGroup{ID: egov3.UUID(id)}
	}
	return result
}

// checkAntiAffinityGroups checks if the anti-affinity groups have capacity for new instances
// It returns an error if any group is at or over the MaxInstancesPerAntiAffinityGroup limit
func (p *Provider) checkAntiAffinityGroups(ctx context.Context, requestedGroups []string) error {
	if len(requestedGroups) == 0 {
		return nil
	}

	for _, groupID := range requestedGroups {
		group, err := p.exoClient.GetAntiAffinityGroup(ctx, egov3.UUID(groupID))
		if err != nil {
			return fmt.Errorf("failed to get anti-affinity group %s: %w", groupID, err)
		}

		currentCount := len(group.Instances)
		if currentCount >= constants.MaxInstancesPerAntiAffinityGroup {
			return fmt.Errorf("anti-affinity group %s (%s) is at capacity: %d/%d instances",
				groupID, group.Name, currentCount, constants.MaxInstancesPerAntiAffinityGroup)
		}

		log.FromContext(ctx).V(1).Info("anti-affinity group has capacity",
			"groupID", groupID,
			"groupName", group.Name,
			"currentCount", currentCount,
			"maxCount", constants.MaxInstancesPerAntiAffinityGroup)
	}

	return nil
}
