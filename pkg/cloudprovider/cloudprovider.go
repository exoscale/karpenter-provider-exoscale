package cloudprovider

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	"github.com/awslabs/operatorpkg/status"
	"github.com/samber/lo"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instance"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instancetype"
	"github.com/exoscale/karpenter-exoscale/pkg/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	karpenterevents "sigs.k8s.io/karpenter/pkg/events"
)

const (
	DriftReasonImageID            cloudprovider.DriftReason = "ImageID"
	DriftReasonAntiAffinityGroups cloudprovider.DriftReason = "AntiAffinityGroups"
	DriftReasonSecurityGroups     cloudprovider.DriftReason = "SecurityGroups"
	DriftReasonPrivateNetworks    cloudprovider.DriftReason = "PrivateNetworks"
)

type CloudProvider struct {
	kubeClient           client.Client
	recorder             karpenterevents.Recorder
	instanceTypeProvider *instancetype.Provider
	instanceProvider     *instance.Provider
}

func NewCloudProvider(
	kubeClient client.Client,
	recorder karpenterevents.Recorder,
	instanceTypeProvider *instancetype.Provider,
	instanceProvider *instance.Provider,
) *CloudProvider {
	return &CloudProvider{
		kubeClient:           kubeClient,
		recorder:             recorder,
		instanceTypeProvider: instanceTypeProvider,
		instanceProvider:     instanceProvider,
	}
}

func (c *CloudProvider) Create(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) (*karpenterv1.NodeClaim, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"method", "Create",
		"nodeClaim", nodeClaim.Name,
	))
	log.FromContext(ctx).Info("creating instance for node claim")

	nodeClass, err := c.resolveNodeClassFromNodeClaim(ctx, nodeClaim)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, cloudprovider.NewInsufficientCapacityError(fmt.Errorf("resolving node class from nodeclaim, %w", err))
		}
		return nil, fmt.Errorf("resolving node class from nodeclaim, %w", err)
	}
	if nodeClassStatus := nodeClass.StatusConditions().Get(status.ConditionReady); nodeClassStatus.IsFalse() {
		if nodeClassStatus == nil {
			return nil, cloudprovider.NewInsufficientCapacityError(fmt.Errorf("unable to determine node class status, %w", err))
		}
		return nil, cloudprovider.NewNodeClassNotReadyError(stderrors.New(nodeClassStatus.Message))
	}

	log.FromContext(ctx).V(1).Info("retrieved node class", "nodeClass", nodeClass.Name)

	bootstrapToken, err := utils.CreateAndApplyBootstrapTokenSecret(ctx, c.kubeClient, nodeClaim.Name)
	if err != nil {
		return nil, cloudprovider.NewCreateError(err, "BootstrapTokenCreationFailed", fmt.Sprintf("failed to create bootstrap token: %s", err))
	}

	log.FromContext(ctx).V(1).Info("bootstrap token secret created", "secretName", bootstrapToken.SecretName)

	createdInstance, err := c.instanceProvider.Create(ctx, nodeClass, nodeClaim, bootstrapToken.Token())
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to create instance")
		return nil, cloudprovider.NewCreateError(err, "InstanceCreationFailed", err.Error())
	}

	nc := createdInstance.ToNodeClaim()
	nc.Name = nodeClaim.Name
	nc.Status.Allocatable = applyOverhead(nc.Status.Allocatable, nodeClass)

	return nc, nil
}

func (c *CloudProvider) Delete(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) error {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"method", "Delete",
		"nodeClaim", nodeClaim.Name,
	))

	instanceID, err := utils.ParseProviderID(nodeClaim.Status.ProviderID)
	if err != nil {
		return fmt.Errorf("failed to parse provider ID %s: %w", nodeClaim.Status.ProviderID, err)
	}

	log.FromContext(ctx).Info("deleting instance", "instanceID", instanceID)
	if err := c.instanceProvider.Delete(ctx, instanceID); err != nil {
		log.FromContext(ctx).Error(err, "failed to delete instance", "instanceID", instanceID)
		return err
	}

	log.FromContext(ctx).Info("cloud instance deletion completed", "instanceID", instanceID)
	return nil
}

func (c *CloudProvider) Get(ctx context.Context, providerID string) (*karpenterv1.NodeClaim, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"method", "Get",
		"providerID", providerID,
	))

	instanceID, err := utils.ParseProviderID(providerID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse provider ID: %w", err)
	}

	inst, err := c.instanceProvider.Get(ctx, instanceID)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get instance", "instanceID", instanceID)
		return nil, err
	}

	nc := inst.ToNodeClaim()
	log.FromContext(ctx).V(1).Info("retrieved instance", "instanceID", instanceID, "nodeClaim", nc.Name)
	return nc, nil
}

func (c *CloudProvider) List(ctx context.Context) ([]*karpenterv1.NodeClaim, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"method", "List",
	))

	instances, err := c.instanceProvider.List(ctx)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to list instances")
		return nil, fmt.Errorf("failed to list instances: %w", err)
	}

	var nodeClaims []*karpenterv1.NodeClaim
	for _, inst := range instances {
		nodeClaims = append(nodeClaims, inst.ToNodeClaim())
	}

	log.FromContext(ctx).V(1).Info("listed instances", "count", len(nodeClaims))
	return nodeClaims, nil
}

func (c *CloudProvider) GetInstanceTypes(ctx context.Context, nodePool *karpenterv1.NodePool) ([]*cloudprovider.InstanceType, error) {
	nodePoolName := "<none>"
	if nodePool != nil {
		nodePoolName = nodePool.Name
	}
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"method", "GetInstanceTypes",
		"nodePool", nodePoolName,
	))

	instanceTypes, err := c.instanceTypeProvider.List()
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to list instance types")
		return nil, fmt.Errorf("failed to list instance types: %w", err)
	}

	if nodePool != nil && nodePool.Spec.Template.Spec.NodeClassRef != nil {
		var nodeClass apiv1.ExoscaleNodeClass
		if err := c.kubeClient.Get(ctx, client.ObjectKey{Name: nodePool.Spec.Template.Spec.NodeClassRef.Name}, &nodeClass); err == nil {
			nodeClass = applyNodeClassDefaults(nodeClass)
			overhead := c.buildOverhead(&nodeClass)
			for _, it := range instanceTypes {
				it.Overhead = overhead
			}
		}
	}

	log.FromContext(ctx).V(1).Info("retrieved instance types", "count", len(instanceTypes))
	return instanceTypes, nil
}

func (c *CloudProvider) IsDrifted(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) (cloudprovider.DriftReason, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"method", "IsDrifted",
		"nodeClaim", nodeClaim.Name,
		"providerID", nodeClaim.Status.ProviderID,
	))
	log.FromContext(ctx).V(2).Info("checking for drift")

	nodeClass, err := c.resolveNodeClassFromNodeClaim(ctx, nodeClaim)
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
		log.FromContext(ctx).Error(err, "failed to get instance", "instanceID", instanceID)
		return "", err
	}

	if nodeClaim.Status.ImageID != inst.Template.ID {
		log.FromContext(ctx).Info("detected template drift", "reason", DriftReasonImageID)
		return DriftReasonImageID, nil
	}

	left, right := lo.Difference(nodeClass.Spec.AntiAffinityGroups, inst.AntiAffinityGroups)
	if len(left) != 0 || len(right) != 0 {
		log.FromContext(ctx).Info("detected anti-affinity groups drift", "reason", DriftReasonAntiAffinityGroups)
		return DriftReasonAntiAffinityGroups, nil
	}

	left, right = lo.Difference(nodeClass.Spec.SecurityGroups, inst.SecurityGroups)
	if len(left) != 0 || len(right) != 0 {
		log.FromContext(ctx).Info("detected security groups drift", "reason", DriftReasonSecurityGroups)
		return DriftReasonSecurityGroups, nil
	}

	left, right = lo.Difference(nodeClass.Spec.PrivateNetworks, inst.PrivateNetworks)
	if len(left) != 0 || len(right) != 0 {
		log.FromContext(ctx).Info("detected private networks drift", "reason", DriftReasonPrivateNetworks)
		return DriftReasonPrivateNetworks, nil
	}

	log.FromContext(ctx).V(2).Info("no drift detected")
	return "", nil
}

func (c *CloudProvider) GetSupportedNodeClasses() []status.Object {
	return []status.Object{&apiv1.ExoscaleNodeClass{}}
}

func (c *CloudProvider) RepairPolicies() []cloudprovider.RepairPolicy {
	return []cloudprovider.RepairPolicy{
		{
			ConditionType:      v1.NodeReady,
			ConditionStatus:    v1.ConditionFalse,
			TolerationDuration: 15 * time.Minute,
		},
		{
			ConditionType:      v1.NodeDiskPressure,
			ConditionStatus:    v1.ConditionTrue,
			TolerationDuration: 10 * time.Minute,
		},
		{
			ConditionType:      v1.NodeMemoryPressure,
			ConditionStatus:    v1.ConditionTrue,
			TolerationDuration: 10 * time.Minute,
		},
		{
			ConditionType:      v1.NodePIDPressure,
			ConditionStatus:    v1.ConditionTrue,
			TolerationDuration: 10 * time.Minute,
		},
		{
			ConditionType:      v1.NodeNetworkUnavailable,
			ConditionStatus:    v1.ConditionTrue,
			TolerationDuration: 5 * time.Minute,
		},
	}
}

func (c *CloudProvider) Name() string {
	return "exoscale"
}

func (c *CloudProvider) resolveNodeClassFromNodeClaim(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) (*apiv1.ExoscaleNodeClass, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"method", "getNodeClass",
		"nodeClaim", nodeClaim.Name,
	))

	if nodeClaim.Spec.NodeClassRef == nil {
		return nil, fmt.Errorf("nodeClassRef not specified in NodeClaim")
	}

	var nodeClass apiv1.ExoscaleNodeClass
	if err := c.kubeClient.Get(ctx, client.ObjectKey{
		Name: nodeClaim.Spec.NodeClassRef.Name,
	}, &nodeClass); err != nil {
		return nil, fmt.Errorf("failed to get node class %s: %w", nodeClaim.Spec.NodeClassRef.Name, err)
	}

	nodeClass = applyNodeClassDefaults(nodeClass)

	log.FromContext(ctx).V(1).Info("retrieved node class", "nodeClass", nodeClass.Name)
	return &nodeClass, nil
}

func applyOverhead(allocatable v1.ResourceList, nodeClass *apiv1.ExoscaleNodeClass) v1.ResourceList {
	allocatable = subtractReservation(allocatable, nodeClass.Spec.KubeReserved)
	return subtractReservation(allocatable, nodeClass.Spec.SystemReserved)
}

func subtractReservation(allocatable v1.ResourceList, reservation apiv1.ResourceReservation) v1.ResourceList {
	result := allocatable.DeepCopy()

	subtractResource := func(value string, resourceName v1.ResourceName) {
		if value == "" {
			return
		}

		quantity, err := resource.ParseQuantity(value)
		if err != nil {
			return
		}

		if current, exists := result[resourceName]; exists {
			current.Sub(quantity)
			result[resourceName] = current
		}
	}

	subtractResource(reservation.CPU, v1.ResourceCPU)
	subtractResource(reservation.Memory, v1.ResourceMemory)
	subtractResource(reservation.EphemeralStorage, v1.ResourceEphemeralStorage)

	return result
}

func applyNodeClassDefaults(nodeClass apiv1.ExoscaleNodeClass) apiv1.ExoscaleNodeClass {
	if nodeClass.Spec.KubeReserved.CPU == "" {
		nodeClass.Spec.KubeReserved.CPU = "200m"
	}
	if nodeClass.Spec.KubeReserved.Memory == "" {
		nodeClass.Spec.KubeReserved.Memory = "300Mi"
	}
	if nodeClass.Spec.KubeReserved.EphemeralStorage == "" {
		nodeClass.Spec.KubeReserved.EphemeralStorage = "1Gi"
	}

	if nodeClass.Spec.SystemReserved.CPU == "" {
		nodeClass.Spec.SystemReserved.CPU = "100m"
	}
	if nodeClass.Spec.SystemReserved.Memory == "" {
		nodeClass.Spec.SystemReserved.Memory = "100Mi"
	}
	if nodeClass.Spec.SystemReserved.EphemeralStorage == "" {
		nodeClass.Spec.SystemReserved.EphemeralStorage = "3Gi"
	}

	return nodeClass
}

func (c *CloudProvider) buildOverhead(nodeClass *apiv1.ExoscaleNodeClass) *cloudprovider.InstanceTypeOverhead {
	parseResource := func(value string) resource.Quantity {
		if qty, err := resource.ParseQuantity(value); err == nil {
			return qty
		}
		return resource.Quantity{}
	}

	return &cloudprovider.InstanceTypeOverhead{
		KubeReserved: v1.ResourceList{
			v1.ResourceCPU:              parseResource(nodeClass.Spec.KubeReserved.CPU),
			v1.ResourceMemory:           parseResource(nodeClass.Spec.KubeReserved.Memory),
			v1.ResourceEphemeralStorage: parseResource(nodeClass.Spec.KubeReserved.EphemeralStorage),
		},
		SystemReserved: v1.ResourceList{
			v1.ResourceCPU:              parseResource(nodeClass.Spec.SystemReserved.CPU),
			v1.ResourceMemory:           parseResource(nodeClass.Spec.SystemReserved.Memory),
			v1.ResourceEphemeralStorage: parseResource(nodeClass.Spec.SystemReserved.EphemeralStorage),
		},
	}
}
