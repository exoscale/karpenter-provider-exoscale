package cloudprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/awslabs/operatorpkg/status"
	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instance"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instancetype"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/pricing"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/userdata"
	"github.com/exoscale/karpenter-exoscale/pkg/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	karpenterevents "sigs.k8s.io/karpenter/pkg/events"
)

const (
	DefaultClusterDNS    = "10.96.0.10"
	DefaultClusterDomain = "cluster.local"

	AnnotationBootstrapToken = "exoscale.com/bootstrap-token"
	AnnotationTokenCreated   = "exoscale.com/token-created"
	LabelTokenProvider       = "exoscale.com/token-provider"

	InstanceDeleteTimeout = 5 * time.Minute
)

type ExoscaleClient interface {
	CreateInstance(ctx context.Context, req egov3.CreateInstanceRequest) (*egov3.Operation, error)
	GetInstance(ctx context.Context, id egov3.UUID) (*egov3.Instance, error)
	DeleteInstance(ctx context.Context, id egov3.UUID) (*egov3.Operation, error)
	ListInstances(ctx context.Context, opts ...egov3.ListInstancesOpt) (*egov3.ListInstancesResponse, error)
	AttachInstanceToPrivateNetwork(ctx context.Context, id egov3.UUID, req egov3.AttachInstanceToPrivateNetworkRequest) (*egov3.Operation, error)
	ListInstanceTypes(ctx context.Context) (*egov3.ListInstanceTypesResponse, error)
	Wait(ctx context.Context, op *egov3.Operation, states ...egov3.OperationState) (*egov3.Operation, error)
}

type CloudProvider struct {
	kubeClient           client.Client
	exoClient            ExoscaleClient
	clusterEndpoint      string
	recorder             karpenterevents.Recorder
	instanceTypeProvider instancetype.Provider
	pricingProvider      pricing.Provider
	instanceProvider     instance.Provider
	userDataProvider     userdata.Provider
	clusterName          string
	zone                 string
	clusterDNS           string
	clusterDomain        string
}

func NewCloudProvider(
	kubeClient client.Client,
	exoClient ExoscaleClient,
	clusterEndpoint string,
	recorder karpenterevents.Recorder,
	instanceTypeProvider instancetype.Provider,
	pricingProvider pricing.Provider,
	instanceProvider instance.Provider,
	userDataProvider userdata.Provider,
	zone string,
	clusterName string,
	clusterDNS string,
	clusterDomain string,
) *CloudProvider {
	if clusterDNS == "" {
		clusterDNS = DefaultClusterDNS
	}
	if clusterDomain == "" {
		clusterDomain = DefaultClusterDomain
	}

	return &CloudProvider{
		kubeClient:           kubeClient,
		exoClient:            exoClient,
		clusterEndpoint:      clusterEndpoint,
		recorder:             recorder,
		instanceTypeProvider: instanceTypeProvider,
		pricingProvider:      pricingProvider,
		instanceProvider:     instanceProvider,
		userDataProvider:     userDataProvider,
		clusterName:          clusterName,
		zone:                 zone,
		clusterDNS:           clusterDNS,
		clusterDomain:        clusterDomain,
	}
}

func (c *CloudProvider) Create(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) (*karpenterv1.NodeClaim, error) {
	logger := log.FromContext(ctx).WithName("cloudprovider").WithValues(
		"method", "Create",
		"nodeClaim", nodeClaim.Name,
	)
	logger.Info("creating instance for node claim")

	nodeClass, err := c.GetNodeClass(ctx, nodeClaim)
	if err != nil {
		logger.Error(err, "failed to get node class")
		return nil, fmt.Errorf("failed to get node class: %w", err)
	}
	logger.V(1).Info("retrieved node class", "nodeClass", nodeClass.Name)

	secret, token, err := c.applyBootstrapTokenSecret(ctx, nodeClaim.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to create bootstrap token: %w", err)
	}

	userDataOptions := &userdata.Options{
		ClusterName:     c.clusterName,
		ClusterEndpoint: c.clusterEndpoint,
		ClusterDNS:      c.clusterDNS,
		ClusterDomain:   c.clusterDomain,
		BootstrapToken:  token,
		Taints:          nodeClaim.Spec.Taints,
		Labels:          nodeClaim.Labels,
	}

	userData, err := c.userDataProvider.Generate(ctx, nodeClass, nodeClaim, userDataOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to generate user data: %w", err)
	}

	if nodeClaim.Annotations == nil {
		nodeClaim.Annotations = make(map[string]string)
	}
	nodeClaim.Annotations[AnnotationBootstrapToken] = secret.Name

	tags := map[string]string{}
	createdInstance, err := c.instanceProvider.Create(ctx, nodeClass, nodeClaim, userData, tags)
	if err != nil {
		logger.Error(err, "failed to create instance")
		return nil, err
	}

	nodeClaim.Status.ProviderID = utils.FormatProviderID(createdInstance.ID)
	nodeClaim.Status.Capacity = v1.ResourceList{}

	if createdInstance.InstanceType != nil {
		setInstanceCapacity(nodeClaim.Status.Capacity, createdInstance.InstanceType)
	}

	if nodeClaim.Labels == nil {
		nodeClaim.Labels = make(map[string]string)
	}

	if createdInstance.InstanceType != nil {
		instanceTypeName := fmt.Sprintf("%s.%s", createdInstance.InstanceType.Family, createdInstance.InstanceType.Size)
		nodeClaim.Labels[v1.LabelInstanceTypeStable] = instanceTypeName
	}

	nodeClaim.Labels[v1.LabelTopologyZone] = c.zone
	nodeClaim.Labels[constants.LabelClusterName] = c.clusterName

	return nodeClaim, nil
}

func (c *CloudProvider) Delete(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) error {
	logger := log.FromContext(ctx).WithName("cloudprovider").WithValues(
		"method", "Delete",
		"nodeClaim", nodeClaim.Name,
	)

	instanceID, err := utils.ParseProviderID(nodeClaim.Status.ProviderID)
	if err != nil {
		return fmt.Errorf("failed to parse provider ID %s: %w", nodeClaim.Status.ProviderID, err)
	}

	logger.Info("deleting instance", "instanceID", instanceID)
	return c.instanceProvider.Delete(ctx, instanceID)
}

func (c *CloudProvider) Get(ctx context.Context, providerID string) (*karpenterv1.NodeClaim, error) {
	logger := log.FromContext(ctx).WithName("cloudprovider").WithValues(
		"method", "Get",
		"providerID", providerID,
	)

	instanceID, err := utils.ParseProviderID(providerID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse provider ID: %w", err)
	}

	inst, err := c.instanceProvider.Get(ctx, instanceID)
	if err != nil {
		logger.Error(err, "failed to get instance", "instanceID", instanceID)
		return nil, fmt.Errorf("failed to get instance %s: %w", instanceID, err)
	}

	nodeClaim := c.createNodeClaimFromInstanceProvider(inst)
	logger.V(1).Info("retrieved instance", "instanceID", instanceID, "nodeClaim", nodeClaim.Name)
	return nodeClaim, nil
}

func (c *CloudProvider) List(ctx context.Context) ([]*karpenterv1.NodeClaim, error) {
	logger := log.FromContext(ctx).WithName("cloudprovider").WithValues(
		"method", "List",
	)

	instances, err := c.instanceProvider.List(ctx)
	if err != nil {
		logger.Error(err, "failed to list instances")
		return nil, fmt.Errorf("failed to list instances: %w", err)
	}

	var nodeClaims []*karpenterv1.NodeClaim
	for _, inst := range instances {
		nodeClaims = append(nodeClaims, c.createNodeClaimFromInstanceProvider(inst))
	}

	logger.V(1).Info("listed instances", "count", len(nodeClaims))
	return nodeClaims, nil
}

func (c *CloudProvider) GetInstanceTypes(ctx context.Context, nodePool *karpenterv1.NodePool) ([]*cloudprovider.InstanceType, error) {
	nodePoolName := "<none>"
	if nodePool != nil {
		nodePoolName = nodePool.Name
	}
	logger := log.FromContext(ctx).WithName("cloudprovider").WithValues(
		"method", "GetInstanceTypes",
		"nodePool", nodePoolName,
	)

	instanceTypes, err := c.instanceTypeProvider.List(ctx, nil)
	if err != nil {
		logger.Error(err, "failed to list instance types")
		return nil, fmt.Errorf("failed to list instance types: %w", err)
	}

	instanceTypes = c.ApplyNodePoolOverhead(ctx, nodePool, instanceTypes)

	logger.V(1).Info("retrieved instance types", "count", len(instanceTypes))
	return instanceTypes, nil
}

func (c *CloudProvider) ApplyNodePoolOverhead(ctx context.Context, nodePool *karpenterv1.NodePool, instanceTypes []*cloudprovider.InstanceType) []*cloudprovider.InstanceType {
	if nodePool == nil || nodePool.Spec.Template.Spec.NodeClassRef == nil {
		return instanceTypes
	}

	var nodeClass apiv1.ExoscaleNodeClass
	nodeClassName := nodePool.Spec.Template.Spec.NodeClassRef.Name
	if err := c.kubeClient.Get(ctx, client.ObjectKey{Name: nodeClassName}, &nodeClass); err != nil {
		return instanceTypes
	}

	overhead := c.CalculateInstanceOverhead(ctx, &nodeClass)
	return c.applyOverheadToInstanceTypes(instanceTypes, overhead)
}

func (c *CloudProvider) CalculateInstanceOverhead(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) cloudprovider.InstanceTypeOverhead {
	logger := log.FromContext(ctx).WithName("cloudprovider").WithValues(
		"method", "calculateInstanceOverhead",
		"nodeClass", nodeClass.Name,
	)

	overhead := cloudprovider.InstanceTypeOverhead{
		KubeReserved:   v1.ResourceList{},
		SystemReserved: v1.ResourceList{},
	}

	c.applyResourceReservation(ctx, &overhead.KubeReserved, nodeClass.Spec.KubeReserved, "kube-reserved")
	c.applyResourceReservation(ctx, &overhead.SystemReserved, nodeClass.Spec.SystemReserved, "system-reserved")

	logger.V(2).Info("calculated instance overhead", "overhead", overhead)
	return overhead
}

func (c *CloudProvider) applyResourceReservation(ctx context.Context, target *v1.ResourceList, source apiv1.ResourceReservation, reservationType string) {
	logger := log.FromContext(ctx).WithName("cloudprovider").WithValues(
		"method", "applyResourceReservation",
		"type", reservationType,
	)

	if source.CPU != "" {
		if quantity, err := resource.ParseQuantity(source.CPU); err == nil {
			(*target)[v1.ResourceCPU] = quantity
		} else {
			logger.V(1).Info("failed to parse CPU reservation", "value", source.CPU, "error", err)
		}
	}

	if source.Memory != "" {
		if quantity, err := resource.ParseQuantity(source.Memory); err == nil {
			(*target)[v1.ResourceMemory] = quantity
		} else {
			logger.V(1).Info("failed to parse memory reservation", "value", source.Memory, "error", err)
		}
	}

	if source.EphemeralStorage != "" {
		if quantity, err := resource.ParseQuantity(source.EphemeralStorage); err == nil {
			(*target)[v1.ResourceEphemeralStorage] = quantity
		} else {
			logger.V(1).Info("failed to parse ephemeral storage reservation", "value", source.EphemeralStorage, "error", err)
		}
	}
}

func (c *CloudProvider) applyOverheadToInstanceTypes(instanceTypes []*cloudprovider.InstanceType, overhead cloudprovider.InstanceTypeOverhead) []*cloudprovider.InstanceType {
	for _, instanceType := range instanceTypes {
		instanceType.Overhead = &overhead
	}
	return instanceTypes
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

func (c *CloudProvider) GetNodeClass(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) (*apiv1.ExoscaleNodeClass, error) {
	logger := log.FromContext(ctx).WithName("cloudprovider").WithValues(
		"method", "getNodeClass",
		"nodeClaim", nodeClaim.Name,
	)

	if nodeClaim.Spec.NodeClassRef == nil {
		return nil, fmt.Errorf("nodeClassRef not specified in NodeClaim")
	}

	var nodeClass apiv1.ExoscaleNodeClass
	if err := c.kubeClient.Get(ctx, client.ObjectKey{
		Name: nodeClaim.Spec.NodeClassRef.Name,
	}, &nodeClass); err != nil {
		return nil, fmt.Errorf("failed to get node class %s: %w", nodeClaim.Spec.NodeClassRef.Name, err)
	}

	logger.V(1).Info("retrieved node class", "nodeClass", nodeClass.Name)
	return &nodeClass, nil
}

func (c *CloudProvider) deleteInstance(ctx context.Context, instanceID string) error {
	deleteCtx, cancel := context.WithTimeout(ctx, InstanceDeleteTimeout)
	defer cancel()

	operation, err := c.exoClient.DeleteInstance(deleteCtx, egov3.UUID(instanceID))
	if err != nil {
		return fmt.Errorf("failed to delete instance: %w", err)
	}

	if _, err := c.exoClient.Wait(deleteCtx, operation, egov3.OperationStateSuccess); err != nil {
		return fmt.Errorf("failed waiting for instance deletion: %w", err)
	}

	return nil
}

func setInstanceCapacity(capacity v1.ResourceList, instanceType *egov3.InstanceType) {
	if instanceType == nil {
		return
	}

	capacity[v1.ResourceCPU] = *resource.NewQuantity(instanceType.Cpus, resource.DecimalSI)
	capacity[v1.ResourceMemory] = *resource.NewQuantity(int64(instanceType.Memory)*1024*1024, resource.BinarySI)

	if instanceType.Gpus > 0 {
		capacity["nvidia.com/gpu"] = *resource.NewQuantity(instanceType.Gpus, resource.DecimalSI)
	}
}

func (c *CloudProvider) updateNodeClaimWithInstance(nodeClaim *karpenterv1.NodeClaim, instance *egov3.Instance) *karpenterv1.NodeClaim {
	updatedNodeClaim := nodeClaim.DeepCopy()
	updatedNodeClaim.Status.ProviderID = utils.FormatProviderID(instance.ID.String())
	updatedNodeClaim.Status.Capacity = make(v1.ResourceList)
	setInstanceCapacity(updatedNodeClaim.Status.Capacity, instance.InstanceType)
	return updatedNodeClaim
}

func (c *CloudProvider) createNodeClaimFromInstance(instance *egov3.Instance) *karpenterv1.NodeClaim {
	nodeClaim := &karpenterv1.NodeClaim{
		Status: karpenterv1.NodeClaimStatus{
			ProviderID: utils.FormatProviderID(instance.ID.String()),
			Capacity:   make(v1.ResourceList),
		},
	}

	setInstanceCapacity(nodeClaim.Status.Capacity, instance.InstanceType)

	if nodeClaimName, ok := instance.Labels[constants.LabelNodeClaim]; ok {
		nodeClaim.Name = nodeClaimName
	}

	return nodeClaim
}

func (c *CloudProvider) createNodeClaimFromListInstance(instance *egov3.ListInstancesResponseInstances) *karpenterv1.NodeClaim {
	nodeClaim := &karpenterv1.NodeClaim{
		Status: karpenterv1.NodeClaimStatus{
			ProviderID: utils.FormatProviderID(instance.ID.String()),
			Capacity:   make(v1.ResourceList),
		},
	}

	setInstanceCapacity(nodeClaim.Status.Capacity, instance.InstanceType)

	if nodeClaimName, ok := instance.Labels[constants.LabelNodeClaim]; ok {
		nodeClaim.Name = nodeClaimName
	}

	return nodeClaim
}

func (c *CloudProvider) resolveInstanceType(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) (string, error) {
	logger := log.FromContext(ctx).WithName("cloudprovider").WithValues(
		"method", "resolveInstanceType",
		"nodeClaim", nodeClaim.Name,
	)

	if len(nodeClaim.Spec.Requirements) == 0 {
		return "", fmt.Errorf("no instance type requirements specified")
	}

	for _, req := range nodeClaim.Spec.Requirements {
		if req.Key == "node.kubernetes.io/instance-type" {
			if len(req.Values) > 0 {
				instanceType := req.Values[0]
				logger.V(1).Info("selected instance type from requirements", "type", instanceType)
				return instanceType, nil
			}
		}
	}

	return "", fmt.Errorf("no instance type found in requirements")
}

func (c *CloudProvider) createNodeClaimFromInstanceProvider(inst *instance.Instance) *karpenterv1.NodeClaim {
	if inst == nil {
		return nil
	}

	nodeClaim := &karpenterv1.NodeClaim{}
	nodeClaim.Status.ProviderID = utils.FormatProviderID(inst.ID)

	if inst.Labels != nil {
		if nodeClaimName, ok := inst.Labels[constants.LabelNodeClaim]; ok {
			nodeClaim.Name = nodeClaimName
		}
	}

	nodeClaim.Status.Capacity = v1.ResourceList{}
	if inst.InstanceType != nil {
		setInstanceCapacity(nodeClaim.Status.Capacity, inst.InstanceType)
	}

	nodeClaim.Status.Allocatable = nodeClaim.Status.Capacity.DeepCopy()
	return nodeClaim
}
