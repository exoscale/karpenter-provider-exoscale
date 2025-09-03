package cloudprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/awslabs/operatorpkg/status"
	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-exoscale/pkg/errors"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instance"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instancetype"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/userdata"
	"github.com/exoscale/karpenter-exoscale/pkg/utils"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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
		instanceProvider:     instanceProvider,
		userDataProvider:     userDataProvider,
		clusterName:          clusterName,
		zone:                 zone,
		clusterDNS:           clusterDNS,
		clusterDomain:        clusterDomain,
	}
}

func (c *CloudProvider) Create(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) (*karpenterv1.NodeClaim, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"method", "Create",
		"nodeClaim", nodeClaim.Name,
	))
	log.FromContext(ctx).Info("creating instance for node claim")

	nodeClass, err := c.GetNodeClass(ctx, nodeClaim)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get node class")
		if k8serrors.IsNotFound(err) {
			return nil, cloudprovider.NewNodeClassNotReadyError(fmt.Errorf("node class not found: %w", err))
		}
		return nil, cloudprovider.NewNodeClassNotReadyError(fmt.Errorf("failed to get node class: %w", err))
	}

	if !c.isNodeClassReady(nodeClass) {
		return nil, cloudprovider.NewNodeClassNotReadyError(fmt.Errorf("node class %s is not ready", nodeClass.Name))
	}
	log.FromContext(ctx).V(1).Info("retrieved node class", "nodeClass", nodeClass.Name)

	bootstrapToken, err := utils.CreateAndApplyBootstrapTokenSecret(ctx, c.kubeClient, nodeClaim.Name)
	if err != nil {
		return nil, cloudprovider.NewCreateError(err, "BootstrapTokenCreationFailed", fmt.Sprintf("failed to create bootstrap token: %s", err))
	}

	log.FromContext(ctx).V(1).Info("bootstrap token secret created", "secretName", bootstrapToken.SecretName)

	nodeTaints := append([]v1.Taint{}, nodeClaim.Spec.Taints...)
	nodeTaints = append(nodeTaints, nodeClaim.Spec.StartupTaints...)

	userDataOptions := &userdata.Options{
		ClusterEndpoint: c.clusterEndpoint,
		ClusterDNS:      c.clusterDNS,
		ClusterDomain:   c.clusterDomain,
		BootstrapToken:  bootstrapToken.Token(),
		Taints:          nodeTaints,
		Labels:          nodeClaim.Labels,
	}

	userData, err := c.userDataProvider.Generate(ctx, nodeClass, nodeClaim, userDataOptions)
	if err != nil {
		return nil, cloudprovider.NewCreateError(err, "UserDataGenerationFailed", fmt.Sprintf("failed to generate user data: %s", err))
	}

	if nodeClaim.Annotations == nil {
		nodeClaim.Annotations = make(map[string]string)
	}
	nodeClaim.Annotations[constants.AnnotationBootstrapToken] = bootstrapToken.SecretName

	tags := map[string]string{}
	createdInstance, err := c.instanceProvider.Create(ctx, nodeClass, nodeClaim, userData, tags)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to create instance")

		if errors.IsInsufficientCapacityError(err) {
			return nil, cloudprovider.NewInsufficientCapacityError(err)
		}

		return nil, cloudprovider.NewCreateError(err, "InstanceCreationFailed", err.Error())
	}

	nodeClaim.Status.ProviderID = utils.FormatProviderID(createdInstance.ID)
	nodeClaim.Status.Capacity = v1.ResourceList{}

	if createdInstance.InstanceType != nil {
		setInstanceCapacity(nodeClaim.Status.Capacity, createdInstance.InstanceType, nodeClass.Spec.DiskSize)
	}

	nodeClaim.Status.Allocatable = nodeClaim.Status.Capacity.DeepCopy()
	nodeClaim.Status.NodeName = fmt.Sprintf("%s-%s", c.clusterName, nodeClaim.Name)

	if nodeClaim.Labels == nil {
		nodeClaim.Labels = make(map[string]string)
	}

	if createdInstance.InstanceTypeName != "" {
		nodeClaim.Labels[v1.LabelInstanceTypeStable] = createdInstance.InstanceTypeName
	}

	nodeClaim.Labels[v1.LabelTopologyZone] = c.zone
	nodeClaim.Labels[constants.LabelClusterName] = c.clusterName
	nodeClaim.Labels[karpenterv1.CapacityTypeLabelKey] = karpenterv1.CapacityTypeOnDemand

	return nodeClaim, nil
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
	err = c.instanceProvider.Delete(ctx, instanceID)
	if err != nil {
		if errors.IsInstanceNotFoundError(err) {
			return cloudprovider.NewNodeClaimNotFoundError(fmt.Errorf("instance %s not found", instanceID))
		}
		return err
	}
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
		if errors.IsInstanceNotFoundError(err) {
			log.FromContext(ctx).V(1).Info("instance not found", "instanceID", instanceID)
			return nil, cloudprovider.NewNodeClaimNotFoundError(fmt.Errorf("instance %s not found", instanceID))
		}
		log.FromContext(ctx).Error(err, "failed to get instance", "instanceID", instanceID)
		return nil, fmt.Errorf("failed to get instance %s: %w", instanceID, err)
	}

	nodeClaim := c.createNodeClaimFromInstanceProvider(inst)
	log.FromContext(ctx).V(1).Info("retrieved instance", "instanceID", instanceID, "nodeClaim", nodeClaim.Name)
	return nodeClaim, nil
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
		nodeClaims = append(nodeClaims, c.createNodeClaimFromInstanceProvider(inst))
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

	instanceTypes, err := c.instanceTypeProvider.List(ctx, nil)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to list instance types")
		return nil, fmt.Errorf("failed to list instance types: %w", err)
	}

	instanceTypes = c.ApplyNodePoolOverhead(ctx, nodePool, instanceTypes)

	log.FromContext(ctx).V(1).Info("retrieved instance types", "count", len(instanceTypes))
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
	instanceTypes = c.addEphemeralStorageToInstanceTypes(instanceTypes, nodeClass.Spec.DiskSize)
	return c.applyOverheadToInstanceTypes(instanceTypes, overhead)
}

func (c *CloudProvider) CalculateInstanceOverhead(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) cloudprovider.InstanceTypeOverhead {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"method", "calculateInstanceOverhead",
		"nodeClass", nodeClass.Name,
	))

	overhead := cloudprovider.InstanceTypeOverhead{
		KubeReserved:   v1.ResourceList{},
		SystemReserved: v1.ResourceList{},
	}

	c.applyResourceReservation(ctx, &overhead.KubeReserved, nodeClass.Spec.KubeReserved, "kube-reserved")
	c.applyResourceReservation(ctx, &overhead.SystemReserved, nodeClass.Spec.SystemReserved, "system-reserved")

	log.FromContext(ctx).V(2).Info("calculated instance overhead", "overhead", overhead)
	return overhead
}

func (c *CloudProvider) applyResourceReservation(ctx context.Context, target *v1.ResourceList, source apiv1.ResourceReservation, reservationType string) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"type", reservationType,
	))

	applyResource := func(value string, resourceName v1.ResourceName, resourceType string) {
		if value != "" {
			if quantity, err := resource.ParseQuantity(value); err == nil {
				(*target)[resourceName] = quantity
			} else {
				log.FromContext(ctx).V(1).Info("failed to parse reservation", "resource", resourceType, "value", value, "error", err)
			}
		}
	}

	applyResource(source.CPU, v1.ResourceCPU, "CPU")
	applyResource(source.Memory, v1.ResourceMemory, "memory")
	applyResource(source.EphemeralStorage, v1.ResourceEphemeralStorage, "ephemeral storage")
}

func (c *CloudProvider) applyOverheadToInstanceTypes(instanceTypes []*cloudprovider.InstanceType, overhead cloudprovider.InstanceTypeOverhead) []*cloudprovider.InstanceType {
	for _, instanceType := range instanceTypes {
		instanceType.Overhead = &overhead
	}
	return instanceTypes
}

func (c *CloudProvider) addEphemeralStorageToInstanceTypes(instanceTypes []*cloudprovider.InstanceType, diskSizeGB int64) []*cloudprovider.InstanceType {
	ephemeralStorageBytes := calculateEphemeralStorageBytes(diskSizeGB)
	for _, instanceType := range instanceTypes {
		if instanceType.Capacity == nil {
			instanceType.Capacity = v1.ResourceList{}
		}
		instanceType.Capacity[v1.ResourceEphemeralStorage] = *resource.NewQuantity(ephemeralStorageBytes, resource.BinarySI)
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

	log.FromContext(ctx).V(1).Info("retrieved node class", "nodeClass", nodeClass.Name)
	return &nodeClass, nil
}

func calculateEphemeralStorageBytes(diskSizeGB int64) int64 {
	if diskSizeGB == 0 {
		diskSizeGB = 50 // Default
	}
	ephemeralStorageGiB := diskSizeGB - 5
	if ephemeralStorageGiB < 0 {
		ephemeralStorageGiB = 0
	}
	return ephemeralStorageGiB * 1024 * 1024 * 1024
}

func setInstanceCapacity(capacity v1.ResourceList, instanceType *egov3.InstanceType, diskSizeGB int64) {
	if instanceType == nil {
		return
	}

	capacity[v1.ResourceCPU] = *resource.NewQuantity(instanceType.Cpus, resource.DecimalSI)
	capacity[v1.ResourceMemory] = *resource.NewQuantity(instanceType.Memory, resource.BinarySI)
	capacity[v1.ResourceEphemeralStorage] = *resource.NewQuantity(calculateEphemeralStorageBytes(diskSizeGB), resource.BinarySI)
	capacity[v1.ResourcePods] = *resource.NewQuantity(110, resource.DecimalSI)

	if instanceType.Gpus > 0 {
		capacity[instancetype.ResourceNvidiaGPU] = *resource.NewQuantity(instanceType.Gpus, resource.DecimalSI)
	}
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
		setInstanceCapacity(nodeClaim.Status.Capacity, inst.InstanceType, 0)
	}

	nodeClaim.Status.Allocatable = nodeClaim.Status.Capacity.DeepCopy()
	return nodeClaim
}

func (c *CloudProvider) isNodeClassReady(nodeClass *apiv1.ExoscaleNodeClass) bool {
	if nodeClass == nil {
		return false
	}

	return nodeClass.StatusConditions().IsTrue(status.ConditionReady)
}
