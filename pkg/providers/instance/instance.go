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
	"github.com/exoscale/karpenter-exoscale/pkg/errors"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instancetype"
	"github.com/patrickmn/go-cache"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/scheduling"
)

const (
	cacheTTL     = 30 * time.Second
	cacheCleanup = 60 * time.Second
)

type DefaultProvider struct {
	exoClient            EgoscaleClient
	zone                 string
	clusterName          string
	cache                *cache.Cache
	instanceTypeProvider instancetype.Provider
}

func NewProvider(exoClient EgoscaleClient, zone, clusterName string, instanceTypeProvider instancetype.Provider) Provider {
	return &DefaultProvider{
		exoClient:            exoClient,
		zone:                 zone,
		clusterName:          clusterName,
		cache:                cache.New(cacheTTL, cacheCleanup),
		instanceTypeProvider: instanceTypeProvider,
	}
}

func (p *DefaultProvider) Create(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass,
	nodeClaim *karpenterv1.NodeClaim, userData string, tags map[string]string) (*Instance, error) {

	start := time.Now()
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("nodeClaim", nodeClaim.Name))

	requirements := scheduling.NewNodeSelectorRequirementsWithMinValues(nodeClaim.Spec.Requirements...)
	instanceTypeRequirement := requirements.Get(corev1.LabelInstanceTypeStable)

	if instanceTypeRequirement == nil || len(instanceTypeRequirement.Values()) == 0 {
		return nil, fmt.Errorf("no instance type requirement specified")
	}

	instanceTypes := instanceTypeRequirement.Values()
	instanceTypeName := p.findCheapestInstanceType(instanceTypes)

	instanceTypeID, ok := p.instanceTypeProvider.GetInstanceTypeID(instanceTypeName)
	if !ok {
		return nil, fmt.Errorf("failed to find instance type ID for %s", instanceTypeName)
	}

	instanceName := fmt.Sprintf("%s-%s", p.clusterName, nodeClaim.Name)

	if err := p.checkAntiAffinityGroups(ctx, nodeClass.Spec.AntiAffinityGroups); err != nil {
		return nil, fmt.Errorf("anti-affinity group capacity check failed: %w", err)
	}

	createRequest := egov3.CreateInstanceRequest{
		Name:         instanceName,
		InstanceType: &egov3.InstanceType{ID: egov3.UUID(instanceTypeID)},
		Template:     &egov3.Template{ID: egov3.UUID(nodeClass.Spec.TemplateID)},
		DiskSize:     nodeClass.Spec.DiskSize,
		UserData:     userData,
		Labels: map[string]string{
			constants.LabelManagedBy:   constants.ManagedByKarpenter,
			constants.LabelClusterName: p.clusterName,
			constants.LabelNodeClaim:   nodeClaim.Name,
		},
		SecurityGroups:     p.convertSecurityGroups(nodeClass.Spec.SecurityGroups),
		AntiAffinityGroups: p.convertAntiAffinityGroups(nodeClass.Spec.AntiAffinityGroups),
	}

	for k, v := range tags {
		createRequest.Labels[k] = v
	}

	createCtx, cancel := context.WithTimeout(ctx, constants.DefaultOperationTimeout)
	defer cancel()

	operation, err := p.exoClient.CreateInstance(createCtx, createRequest)
	if err != nil {
		// Check if it's a capacity error (e.g., no available resources)
		if p.isCapacityError(err) {
			return nil, errors.NewInsufficientCapacityError(err)
		}
		return nil, fmt.Errorf("failed to create instance: %w", err)
	}

	log.FromContext(ctx).Info("instance creation started", "operationID", operation.ID)

	finalOp, err := p.exoClient.Wait(createCtx, operation, egov3.OperationStateSuccess)
	if err != nil {
		// Check the operation reason for specific error types
		if finalOp != nil {
			switch finalOp.Reason {
			case egov3.OperationReasonUnavailable:
				return nil, errors.NewInsufficientCapacityError(fmt.Errorf("instance creation failed: resources unavailable - %s", finalOp.Message))
			case egov3.OperationReasonForbidden:
				return nil, errors.NewInsufficientCapacityError(fmt.Errorf("instance creation failed: quota exceeded - %s", finalOp.Message))
			case egov3.OperationReasonBusy:
				return nil, errors.NewInsufficientCapacityError(fmt.Errorf("instance creation failed: resources busy - %s", finalOp.Message))
			}
		}
		return nil, fmt.Errorf("failed to wait for instance creation: %w", err)
	}

	createdInstance, err := p.exoClient.GetInstance(ctx, operation.Reference.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get created instance: %w", err)
	}

	// The API might not return full instance type details, so we populate them from what we know
	if createdInstance.InstanceType == nil {
		createdInstance.InstanceType = &egov3.InstanceType{}
	}
	// Get the full instance type details from the provider
	instanceType, err := p.instanceTypeProvider.Get(ctx, instanceTypeName)
	if err == nil && instanceType != nil {
		// Use the instance type name to set family and size
		parts := strings.SplitN(instanceTypeName, ".", 2)
		if len(parts) == 2 {
			createdInstance.InstanceType.Family = egov3.InstanceTypeFamily(parts[0])
			createdInstance.InstanceType.Size = egov3.InstanceTypeSize(parts[1])
		}
		// IMPORTANT: Set the CPU and Memory values from the instance type
		// These are needed for Karpenter to know the node's capacity
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

	if len(nodeClass.Spec.PrivateNetworks) > 0 {
		if err := p.attachPrivateNetworks(ctx, createdInstance.ID, nodeClass.Spec.PrivateNetworks); err != nil {
			log.FromContext(ctx).Error(err, "failed to attach private networks, cleaning up instance")

			deleteErr := p.Delete(ctx, createdInstance.ID.String())
			if deleteErr != nil {
				log.FromContext(ctx).Error(deleteErr, "failed to delete instance after network attachment failure")
			}

			return nil, fmt.Errorf("failed to attach private networks to instance %s: %w", createdInstance.ID.String(), err)
		}
	}

	instance := FromExoscaleInstance(createdInstance, p.zone)
	p.cache.Set(instance.ID, instance, cacheTTL)

	log.FromContext(ctx).Info("instance created successfully", "instanceID", instance.ID, "duration", time.Since(start))

	return instance, nil
}

func (p *DefaultProvider) Get(ctx context.Context, id string) (*Instance, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("instanceID", id))

	if cached, found := p.cache.Get(id); found {
		log.FromContext(ctx).V(2).Info("cache hit for instance")
		return cached.(*Instance), nil
	}
	log.FromContext(ctx).V(2).Info("cache miss for instance")

	instance, err := p.exoClient.GetInstance(ctx, egov3.UUID(id))
	if err != nil {
		if p.isNotFoundError(err) {
			return nil, errors.NewInstanceNotFoundError(id)
		}
		return nil, fmt.Errorf("failed to get instance %s: %w", id, err)
	}

	result := FromExoscaleInstance(instance, p.zone)
	p.populateInstanceTypeDetails(ctx, result)
	p.cache.Set(id, result, cacheTTL)

	return result, nil
}

func (p *DefaultProvider) List(ctx context.Context) ([]*Instance, error) {
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
		managedBy, hasManagedBy := instance.Labels[constants.LabelManagedBy]
		clusterName, hasClusterName := instance.Labels[constants.LabelClusterName]

		if !hasManagedBy || managedBy != constants.ManagedByKarpenter {
			continue
		}
		if !hasClusterName || clusterName != p.clusterName {
			continue
		}

		// List API doesn't return AntiAffinityGroups, so we need to fetch full instance details
		// to ensure drift detection works correctly
		fullInstance, err := p.exoClient.GetInstance(ctx, instance.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get full instance details for %s: %w", instance.ID, err)
		}

		inst := FromExoscaleInstance(fullInstance, p.zone)
		p.populateInstanceTypeDetails(ctx, inst)
		result = append(result, inst)
		p.cache.Set(inst.ID, inst, cacheTTL)
	}

	return result, nil
}

func (p *DefaultProvider) Delete(ctx context.Context, id string) error {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("instanceID", id))

	deleteCtx, cancel := context.WithTimeout(ctx, constants.DefaultOperationTimeout)
	defer cancel()

	log.FromContext(ctx).Info("deleting instance")

	operation, err := p.exoClient.DeleteInstance(deleteCtx, egov3.UUID(id))
	if err != nil {
		if p.isNotFoundError(err) {
			log.FromContext(ctx).Info("instance not found, nothing to delete")
			p.cache.Delete(id)
			return errors.NewInstanceNotFoundError(id)
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

	p.cache.Delete(id)
	return nil
}

func (p *DefaultProvider) UpdateTags(ctx context.Context, id string, tags map[string]string) error {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("instanceID", id))

	instance, err := p.exoClient.GetInstance(ctx, egov3.UUID(id))
	if err != nil {
		if p.isNotFoundError(err) {
			return errors.NewInstanceNotFoundError(id)
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

	p.cache.Delete(id)

	log.FromContext(ctx).Info("instance labels updated successfully", "tagsCount", len(tags))
	return nil
}

func (p *DefaultProvider) attachPrivateNetworks(ctx context.Context, instanceID egov3.UUID, networkIDs []string) error {
	for _, networkID := range networkIDs {
		if err := p.attachPrivateNetwork(ctx, instanceID, networkID); err != nil {
			return err
		}
	}
	return nil
}

func (p *DefaultProvider) attachPrivateNetwork(ctx context.Context, instanceID egov3.UUID, networkID string) error {
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

// findCheapestInstanceType returns the cheapest instance type from the given list
func (p *DefaultProvider) findCheapestInstanceType(instanceTypes []string) string {
	if len(instanceTypes) == 0 {
		return ""
	}
	if len(instanceTypes) == 1 {
		return instanceTypes[0]
	}

	cheapest := instanceTypes[0]
	var cheapestPrice float64
	foundValidPrice := false

	for _, typeName := range instanceTypes {
		instanceType, err := p.instanceTypeProvider.Get(context.Background(), typeName)
		if err == nil && instanceType != nil && len(instanceType.Offerings) > 0 {
			price := instanceType.Offerings[0].Price
			if !foundValidPrice || price < cheapestPrice {
				cheapestPrice = price
				cheapest = typeName
				foundValidPrice = true
			}
		}
	}

	return cheapest
}

func (p *DefaultProvider) isNotFoundError(err error) bool {
	return stderrors.Is(err, egov3.ErrNotFound)
}

func (p *DefaultProvider) isCapacityError(err error) bool {
	if err == nil {
		return false
	}
	// Check for common capacity-related error messages
	errStr := err.Error()
	return strings.Contains(errStr, "quota") ||
		strings.Contains(errStr, "capacity") ||
		strings.Contains(errStr, "limit") ||
		strings.Contains(errStr, "unavailable") ||
		stderrors.Is(err, egov3.ErrServiceUnavailable)
}

// populateInstanceTypeDetails ensures the instance has CPU/Memory values populated
// The Exoscale API doesn't always return these, so we need to fetch them from our cache
func (p *DefaultProvider) populateInstanceTypeDetails(ctx context.Context, inst *Instance) {
	if inst == nil || inst.InstanceType == nil || inst.InstanceTypeName == "" {
		return
	}

	instanceType, err := p.instanceTypeProvider.Get(ctx, inst.InstanceTypeName)
	if err != nil || instanceType == nil || instanceType.Capacity == nil {
		return
	}

	// Populate CPU, Memory, and GPU from the instance type provider
	if cpuQuantity, ok := instanceType.Capacity[corev1.ResourceCPU]; ok {
		inst.InstanceType.Cpus = cpuQuantity.Value()
	}
	if memQuantity, ok := instanceType.Capacity[corev1.ResourceMemory]; ok {
		inst.InstanceType.Memory = memQuantity.Value()
	}
	if gpuQuantity, ok := instanceType.Capacity[instancetype.ResourceNvidiaGPU]; ok {
		inst.InstanceType.Gpus = gpuQuantity.Value()
	}
}

func (p *DefaultProvider) convertSecurityGroups(ids []string) []egov3.SecurityGroup {
	result := make([]egov3.SecurityGroup, len(ids))
	for i, id := range ids {
		result[i] = egov3.SecurityGroup{ID: egov3.UUID(id)}
	}
	return result
}

func (p *DefaultProvider) convertAntiAffinityGroups(ids []string) []egov3.AntiAffinityGroup {
	result := make([]egov3.AntiAffinityGroup, len(ids))
	for i, id := range ids {
		result[i] = egov3.AntiAffinityGroup{ID: egov3.UUID(id)}
	}
	return result
}

// checkAntiAffinityGroups checks if the anti-affinity groups have capacity for new instances
// It returns an error if any group is at or over the MaxInstancesPerAntiAffinityGroup limit
func (p *DefaultProvider) checkAntiAffinityGroups(ctx context.Context, requestedGroups []string) error {
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
