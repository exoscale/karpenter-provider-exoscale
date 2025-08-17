package instance

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-exoscale/pkg/errors"
	"github.com/exoscale/karpenter-exoscale/pkg/metrics"
	"github.com/patrickmn/go-cache"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

const (
	cacheTTL     = 30 * time.Second
	cacheCleanup = 60 * time.Second

	networkAttachTimeout = 10 * time.Minute
	creationTimeout      = 5 * time.Minute
	deletionTimeout      = 5 * time.Minute
)

type DefaultProvider struct {
	exoClient   EgoscaleClient
	zone        string
	clusterName string
	cache       *cache.Cache
}

func NewProvider(exoClient EgoscaleClient, zone, clusterName string) Provider {
	return &DefaultProvider{
		exoClient:   exoClient,
		zone:        zone,
		clusterName: clusterName,
		cache:       cache.New(cacheTTL, cacheCleanup),
	}
}

func (p *DefaultProvider) Create(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass,
	nodeClaim *karpenterv1.NodeClaim, userData string, tags map[string]string) (*Instance, error) {

	start := time.Now()
	logger := log.FromContext(ctx).WithValues("nodeClaim", nodeClaim.Name)

	instanceType, err := p.resolveInstanceType(nodeClaim)
	if err != nil {
		return nil, err
	}

	instanceName := fmt.Sprintf("%s-%s", p.clusterName, nodeClaim.Name)

	createRequest := egov3.CreateInstanceRequest{
		Name:         instanceName,
		InstanceType: &egov3.InstanceType{ID: egov3.UUID(instanceType)},
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

	createCtx, cancel := context.WithTimeout(ctx, creationTimeout)
	defer cancel()

	metrics.APICallsTotal.Inc(map[string]string{metrics.OperationLabel: "CreateInstance"})
	apiStart := time.Now()
	operation, err := p.exoClient.CreateInstance(createCtx, createRequest)
	metrics.APICallDurationSeconds.Observe(time.Since(apiStart).Seconds(), map[string]string{metrics.OperationLabel: "CreateInstance"})
	if err != nil {
		metrics.APICallErrorsTotal.Inc(map[string]string{
			metrics.OperationLabel: "CreateInstance",
			metrics.ErrorTypeLabel: "create_failed",
		})
		return nil, fmt.Errorf("failed to create instance: %w", err)
	}

	logger.Info("instance creation started", "operationID", operation.ID)

	_, err = p.exoClient.Wait(createCtx, operation, egov3.OperationStateSuccess)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for instance creation: %w", err)
	}

	createdInstance, err := p.exoClient.GetInstance(ctx, operation.Reference.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get created instance: %w", err)
	}

	if len(nodeClass.Spec.PrivateNetworks) > 0 {
		metrics.NetworkAttachmentsTotal.Inc(map[string]string{})
		if err := p.attachPrivateNetworks(ctx, createdInstance.ID, nodeClass.Spec.PrivateNetworks); err != nil {
			metrics.NetworkAttachmentErrorsTotal.Inc(map[string]string{})
			logger.Error(err, "failed to attach private networks, cleaning up instance")

			deleteErr := p.Delete(ctx, createdInstance.ID.String())
			if deleteErr != nil {
				logger.Error(deleteErr, "failed to delete instance after network attachment failure")
			}

			return nil, errors.NewNetworkAttachmentError(createdInstance.ID.String(), "", err)
		}
	}

	instance := FromExoscaleInstance(createdInstance, p.zone)
	p.cache.Set(instance.ID, instance, cacheTTL)

	logger.Info("instance created successfully", "instanceID", instance.ID)

	metrics.InstancesLaunchedTotal.Inc(map[string]string{
		metrics.InstanceTypeLabel: instanceType,
		metrics.ZoneLabel:         p.zone,
		metrics.NodeClassLabel:    nodeClass.Name,
	})
	metrics.InstanceLaunchDurationSeconds.Observe(
		time.Since(start).Seconds(),
		map[string]string{
			metrics.InstanceTypeLabel: instanceType,
			metrics.ZoneLabel:         p.zone,
		},
	)

	return instance, nil
}

func (p *DefaultProvider) Get(ctx context.Context, id string) (*Instance, error) {
	if cached, found := p.cache.Get(id); found {
		metrics.CacheHitsTotal.Inc(map[string]string{metrics.CacheTypeLabel: "instance"})
		return cached.(*Instance), nil
	}
	metrics.CacheMissesTotal.Inc(map[string]string{metrics.CacheTypeLabel: "instance"})

	metrics.APICallsTotal.Inc(map[string]string{metrics.OperationLabel: "GetInstance"})
	apiStart := time.Now()
	instance, err := p.exoClient.GetInstance(ctx, egov3.UUID(id))
	metrics.APICallDurationSeconds.Observe(time.Since(apiStart).Seconds(), map[string]string{metrics.OperationLabel: "GetInstance"})
	if err != nil {
		metrics.APICallErrorsTotal.Inc(map[string]string{
			metrics.OperationLabel: "GetInstance",
			metrics.ErrorTypeLabel: "get_failed",
		})
		if p.isNotFoundError(err) {
			return nil, errors.NewInstanceNotFoundError(id)
		}
		return nil, fmt.Errorf("failed to get instance %s: %w", id, err)
	}

	result := FromExoscaleInstance(instance, p.zone)
	p.cache.Set(id, result, cacheTTL)

	return result, nil
}

func (p *DefaultProvider) List(ctx context.Context) ([]*Instance, error) {
	metrics.APICallsTotal.Inc(map[string]string{metrics.OperationLabel: "ListInstances"})
	apiStart := time.Now()
	instances, err := p.exoClient.ListInstances(ctx)
	metrics.APICallDurationSeconds.Observe(time.Since(apiStart).Seconds(), map[string]string{metrics.OperationLabel: "ListInstances"})
	if err != nil {
		metrics.APICallErrorsTotal.Inc(map[string]string{
			metrics.OperationLabel: "ListInstances",
			metrics.ErrorTypeLabel: "list_failed",
		})
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

		inst := &Instance{
			ID:           instance.ID.String(),
			Name:         instance.Name,
			State:        instance.State,
			InstanceType: instance.InstanceType,
			Template:     instance.Template,
			Zone:         p.zone,
			Labels:       instance.Labels,
			CreatedAt:    instance.CreatedAT,
		}

		for _, sg := range instance.SecurityGroups {
			inst.SecurityGroups = append(inst.SecurityGroups, string(sg.ID))
		}

		for _, pn := range instance.PrivateNetworks {
			inst.PrivateNetworks = append(inst.PrivateNetworks, string(pn.ID))
		}

		result = append(result, inst)
		p.cache.Set(inst.ID, inst, cacheTTL)
	}

	return result, nil
}

func (p *DefaultProvider) Delete(ctx context.Context, id string) error {
	deleteCtx, cancel := context.WithTimeout(ctx, deletionTimeout)
	defer cancel()

	metrics.APICallsTotal.Inc(map[string]string{metrics.OperationLabel: "DeleteInstance"})
	apiStart := time.Now()
	operation, err := p.exoClient.DeleteInstance(deleteCtx, egov3.UUID(id))
	metrics.APICallDurationSeconds.Observe(time.Since(apiStart).Seconds(), map[string]string{metrics.OperationLabel: "DeleteInstance"})
	if err != nil {
		metrics.APICallErrorsTotal.Inc(map[string]string{
			metrics.OperationLabel: "DeleteInstance",
			metrics.ErrorTypeLabel: "delete_failed",
		})
		if p.isNotFoundError(err) {
			p.cache.Delete(id)
			return nil
		}
		return fmt.Errorf("failed to delete instance: %w", err)
	}

	if operation != nil {
		_, err = p.exoClient.Wait(deleteCtx, operation, egov3.OperationStateSuccess)
		if err != nil {
			return fmt.Errorf("failed waiting for instance deletion: %w", err)
		}
	}

	p.cache.Delete(id)
	return nil
}

func (p *DefaultProvider) UpdateTags(ctx context.Context, id string, tags map[string]string) error {
	logger := log.FromContext(ctx).WithValues("instanceID", id)

	instance, err := p.exoClient.GetInstance(ctx, egov3.UUID(id))
	if err != nil {
		if p.isNotFoundError(err) {
			return errors.NewInstanceNotFoundError(id)
		}
		return fmt.Errorf("failed to get instance %s for tag update: %w", id, err)
	}

	updatedLabels := make(map[string]string)
	for k, v := range instance.Labels {
		updatedLabels[k] = v
	}

	for k, v := range tags {
		updatedLabels[k] = v
	}

	updateRequest := egov3.UpdateInstanceRequest{
		Labels: updatedLabels,
	}

	metrics.APICallsTotal.Inc(map[string]string{metrics.OperationLabel: "UpdateInstance"})
	apiStart := time.Now()
	operation, err := p.exoClient.UpdateInstance(ctx, egov3.UUID(id), updateRequest)
	metrics.APICallDurationSeconds.Observe(time.Since(apiStart).Seconds(), map[string]string{metrics.OperationLabel: "UpdateInstance"})
	if err != nil {
		metrics.APICallErrorsTotal.Inc(map[string]string{
			metrics.OperationLabel: "UpdateInstance",
			metrics.ErrorTypeLabel: "update_failed",
		})
		return fmt.Errorf("failed to update instance labels: %w", err)
	}

	if operation != nil {
		_, err = p.exoClient.Wait(ctx, operation, egov3.OperationStateSuccess)
		if err != nil {
			return fmt.Errorf("failed waiting for instance label update: %w", err)
		}
	}

	p.cache.Delete(id)

	logger.Info("instance labels updated successfully", "tagsCount", len(tags))
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
	attachCtx, cancel := context.WithTimeout(ctx, networkAttachTimeout)
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

func (p *DefaultProvider) resolveInstanceType(nodeClaim *karpenterv1.NodeClaim) (string, error) {
	if len(nodeClaim.Spec.Requirements) == 0 {
		return "", fmt.Errorf("no instance type requirements specified")
	}

	for _, req := range nodeClaim.Spec.Requirements {
		if req.Key == "node.kubernetes.io/instance-type" {
			if len(req.Values) > 0 {
				return req.Values[0], nil
			}
		}
	}

	return "", fmt.Errorf("no instance type found in requirements")
}

func (p *DefaultProvider) isNotFoundError(err error) bool {
	return stderrors.Is(err, egov3.ErrNotFound)
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
