package cloudprovider

import (
	"context"
	stderrors "errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/awslabs/operatorpkg/status"
	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/instance"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/instancetype"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/utils"
	"github.com/samber/lo"
	v1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/events"
)

const (
	DriftReasonImageID            cloudprovider.DriftReason = "ImageID"
	DriftReasonAntiAffinityGroups cloudprovider.DriftReason = "AntiAffinityGroups"
	DriftReasonSecurityGroups     cloudprovider.DriftReason = "SecurityGroups"
	DriftReasonPrivateNetworks    cloudprovider.DriftReason = "PrivateNetworks"
	DriftReasonVpcSubnets         cloudprovider.DriftReason = "VpcSubnets"
	DriftReasonElasticIPs         cloudprovider.DriftReason = "ElasticIPs"
	DriftReasonIPv6               cloudprovider.DriftReason = "IPv6ToggleDrift"
	DriftReasonIPForwarding       cloudprovider.DriftReason = "IPForwardingToggleDrift"
	DriftReasonUserDataChanged    cloudprovider.DriftReason = "UserDataChanged"
)

type CloudProvider struct {
	kubeClient           client.Client
	recorder             events.Recorder
	instanceTypeProvider *instancetype.Provider
	instanceProvider     *instance.Provider
}

func NewCloudProvider(
	kubeClient client.Client,
	recorder events.Recorder,
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

	nodeClass, err := c.getNodeClass(ctx, nodeClaim.Spec.NodeClassRef)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, cloudprovider.NewInsufficientCapacityError(err)
		}
		return nil, err
	}
	if nodeClassStatus := nodeClass.StatusConditions().Get(status.ConditionReady); nodeClassStatus == nil {
		return nil, cloudprovider.NewInsufficientCapacityError(fmt.Errorf("unable to determine node class status"))
	} else if nodeClassStatus.IsFalse() {
		return nil, cloudprovider.NewNodeClassNotReadyError(stderrors.New(nodeClassStatus.Message))
	}

	bootstrapToken, err := utils.CreateAndApplyBootstrapTokenSecret(ctx, c.kubeClient, nodeClaim.Name)
	if err != nil {
		return nil, cloudprovider.NewCreateError(err, "BootstrapTokenCreationFailed", fmt.Sprintf("failed to create bootstrap token: %s", err))
	}

	log.FromContext(ctx).V(1).Info("bootstrap token secret created", "secretName", bootstrapToken.SecretName)

	createdInstance, err := c.instanceProvider.Create(ctx, nodeClass, nodeClaim, bootstrapToken.Token())
	if err != nil {
		return nil, cloudprovider.NewCreateError(err, "InstanceCreationFailed", err.Error())
	}

	// The kubelet registers the Node itself; Karpenter matches it by provider ID.
	return createdInstance.ToNodeClaim(), nil
}

// Delete always ends with NodeClaimNotFoundError: Karpenter keeps calling Delete
// until it gets one.
func (c *CloudProvider) Delete(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) error {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"method", "Delete",
		"nodeClaim", nodeClaim.Name,
	))

	// karpenter has its own draining logic when it decides to downscale a nodepool,
	// but we want to be very conservative for people deleting nodeclaims directly
	log.FromContext(ctx).Info("draining node", "nodeName", nodeClaim.Status.NodeName)
	if err := c.drainNode(ctx, nodeClaim.Status.NodeName); err != nil {
		return fmt.Errorf("failed to drain node %s: %w", nodeClaim.Status.NodeName, err)
	}

	log.FromContext(ctx).Info("node drained", "nodeName", nodeClaim.Status.NodeName)

	instanceID, err := utils.ParseProviderID(nodeClaim.Status.ProviderID)
	if err != nil {
		return fmt.Errorf("failed to parse provider ID %s: %w", nodeClaim.Status.ProviderID, err)
	}

	log.FromContext(ctx).Info("deleting instance", "instanceID", instanceID)
	if err := c.instanceProvider.Delete(ctx, instanceID); err != nil && !c.instanceProvider.IsNotFoundError(err) {
		return err
	}

	log.FromContext(ctx).Info("cloud instance deletion completed", "instanceID", instanceID)
	return cloudprovider.NewNodeClaimNotFoundError(nil)
}

// Reboot ignores operationID: the Exoscale API has no per-operation idempotency, but
// rebooting an instance that is already rebooting is a server-side no-op.
func (c *CloudProvider) Reboot(ctx context.Context, nodeClaim *karpenterv1.NodeClaim, operationID string) error {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"method", "Reboot",
		"nodeClaim", nodeClaim.Name,
		"operationID", operationID,
	))

	instanceID, err := utils.ParseProviderID(nodeClaim.Status.ProviderID)
	if err != nil {
		return fmt.Errorf("failed to parse provider ID %s: %w", nodeClaim.Status.ProviderID, err)
	}

	log.FromContext(ctx).Info("rebooting instance", "instanceID", instanceID)
	if err := c.instanceProvider.Reboot(ctx, instanceID); err != nil {
		if c.instanceProvider.IsNotFoundError(err) {
			return cloudprovider.NewNodeClaimNotFoundError(err)
		}
		return err
	}

	log.FromContext(ctx).Info("instance reboot requested", "instanceID", instanceID)
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
		if c.instanceProvider.IsNotFoundError(err) {
			return nil, cloudprovider.NewNodeClaimNotFoundError(err)
		}
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
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"method", "GetInstanceTypes",
		"nodePool", nodePool.Name,
	))

	nodeClass, err := c.getNodeClass(ctx, nodePool.Spec.Template.Spec.NodeClassRef)
	if err != nil {
		return nil, err
	}

	instanceTypes, err := c.instanceTypeProvider.List(nodeClass)
	if err != nil {
		return nil, fmt.Errorf("failed to list instance types: %w", err)
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

	nodeClass, err := c.getNodeClass(ctx, nodeClaim.Spec.NodeClassRef)
	if err != nil {
		return "", err
	}

	instanceID, err := utils.ParseProviderID(nodeClaim.Status.ProviderID)
	if err != nil {
		return "", fmt.Errorf("failed to parse provider ID: %w", err)
	}

	inst, err := c.instanceProvider.Get(ctx, instanceID)
	if err != nil {
		return "", err
	}

	if !inst.IsRunning() {
		log.FromContext(ctx).V(2).Info("instance not running yet, skipping drift detection", "state", inst.State)
		return "", nil
	}

	if nodeClass.Status.ImageID == "" {
		log.FromContext(ctx).V(2).Info("nodeClass has no resolved ImageID yet, skipping template drift check")
	} else if nodeClaim.Status.ImageID != nodeClass.Status.ImageID {
		log.FromContext(ctx).Info("detected template drift", "reason", DriftReasonImageID)
		c.publishDrift(nodeClaim, fmt.Sprintf("Instance template ID drift detected: nodeClaim ImageID %s != nodeClass desired ImageID %s (instance currently reports %s)", nodeClaim.Status.ImageID, nodeClass.Status.ImageID, inst.Template.ID))
		return DriftReasonImageID, nil
	}

	if nodeClass.Spec.EnableIPv6 && !utils.ContainsIPv6Address(inst.Addresses) {
		log.FromContext(ctx).Info("detected IPv6 drift", "reason", DriftReasonIPv6)
		c.publishDrift(nodeClaim, "Instance IPv6 drift detected: nodeClass EnableIPv6 is true but instance doesn't have any IPv6Address")
		return DriftReasonIPv6, nil
	} else if !nodeClass.Spec.EnableIPv6 && utils.ContainsIPv6Address(inst.Addresses) {
		log.FromContext(ctx).Info("detected IPv6 drift", "reason", DriftReasonIPv6)
		c.publishDrift(nodeClaim, "Instance IPv6 drift detected: nodeClass EnableIPv6 is false but instance has IPv6Address")
		return DriftReasonIPv6, nil
	}

	// ip-forwarding is immutable on an instance (not settable via UpdateInstance),
	// so a mismatch can only be resolved by replacing the node.
	if nodeClass.Spec.EnableIPForwarding != inst.IPForwarding {
		log.FromContext(ctx).Info("detected IP forwarding drift", "reason", DriftReasonIPForwarding)
		c.publishDrift(nodeClaim, fmt.Sprintf("Instance IP forwarding drift detected: nodeClass EnableIPForwarding is %t but instance IPForwarding is %t", nodeClass.Spec.EnableIPForwarding, inst.IPForwarding))
		return DriftReasonIPForwarding, nil
	}

	left, right := lo.Difference(nodeClass.Status.AntiAffinityGroups, inst.AntiAffinityGroups)
	if len(left) != 0 || len(right) != 0 {
		log.FromContext(ctx).Info("detected anti-affinity groups drift", "reason", DriftReasonAntiAffinityGroups)
		c.publishDrift(nodeClaim, fmt.Sprintf("Instance anti-affinity groups drift detected: nodeClass AntiAffinityGroups %v != instance AntiAffinityGroups %v", nodeClass.Status.AntiAffinityGroups, inst.AntiAffinityGroups))
		return DriftReasonAntiAffinityGroups, nil
	}

	expectedSecurityGroups, err := c.instanceProvider.ExpectedSecurityGroups(ctx, nodeClass)
	if err != nil {
		return "", err
	}

	left, right = lo.Difference(expectedSecurityGroups, inst.SecurityGroups)
	if len(left) != 0 || len(right) != 0 {
		log.FromContext(ctx).Info("detected security groups drift", "reason", DriftReasonSecurityGroups)
		c.publishDrift(nodeClaim, fmt.Sprintf("Instance security groups drift detected: nodeClass SecurityGroups %v != instance SecurityGroups %v", expectedSecurityGroups, inst.SecurityGroups))
		return DriftReasonSecurityGroups, nil
	}

	left, right = lo.Difference(nodeClass.Status.PrivateNetworks, inst.PrivateNetworks)
	if len(left) != 0 || len(right) != 0 {
		log.FromContext(ctx).Info("detected private networks drift", "reason", DriftReasonPrivateNetworks)
		c.publishDrift(nodeClaim, fmt.Sprintf("Instance private networks drift detected: nodeClass PrivateNetworks %v != instance PrivateNetworks %v", nodeClass.Status.PrivateNetworks, inst.PrivateNetworks))
		return DriftReasonPrivateNetworks, nil
	}

	left, right = lo.Difference(nodeClass.Status.VpcSubnets, inst.VpcSubnets)
	if len(left) != 0 || len(right) != 0 {
		log.FromContext(ctx).Info("detected vpc subnets drift", "reason", DriftReasonVpcSubnets)
		c.publishDrift(nodeClaim, fmt.Sprintf("Instance VPC subnets drift detected: nodeClass VpcSubnets %v (vpc %s) != instance VpcSubnets %v (vpc %s)", nodeClass.Status.VpcSubnets, nodeClass.Status.VpcID, inst.VpcSubnets, inst.VpcID))
		return DriftReasonVpcSubnets, nil
	}

	left, right = lo.Difference(nodeClass.Status.ElasticIPs, inst.ElasticIPs)
	if len(left) != 0 || len(right) != 0 {
		log.FromContext(ctx).Info("detected elastic IPs drift", "reason", DriftReasonElasticIPs)
		c.publishDrift(nodeClaim, fmt.Sprintf("Instance elastic IPs drift detected: nodeClass ElasticIPs %v != instance ElasticIPs %v", nodeClass.Status.ElasticIPs, inst.ElasticIPs))
		return DriftReasonElasticIPs, nil
	}

	expected := nodeClass.Status.ConfigurationHash
	got := nodeClaim.Annotations[constants.AnnotationConfigurationHash]
	if got != expected {
		log.FromContext(ctx).Info("detected user-data drift", "reason", DriftReasonUserDataChanged)
		c.publishDrift(nodeClaim, fmt.Sprintf("User data drift detected: nodeClaim hash %q != nodeClass hash %q", got, expected))
		return DriftReasonUserDataChanged, nil
	}

	expectedInstanceLabels := c.instanceProvider.GenerateInstanceLabels(nodeClaim)
	if !reflect.DeepEqual(expectedInstanceLabels, inst.Labels) {
		log.FromContext(ctx).Info("detected instance labels drift, fixing them", "reason", "Labels")
		if err := c.instanceProvider.UpdateTags(ctx, inst.ID, expectedInstanceLabels); err != nil {
			return "", fmt.Errorf("failed to update instance labels: %w", err)
		}
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

func (c *CloudProvider) getNodeClass(ctx context.Context, ref *karpenterv1.NodeClassReference) (*apiv1.ExoscaleNodeClass, error) {
	if ref == nil {
		return nil, fmt.Errorf("nodeClassRef not specified")
	}
	var nodeClass apiv1.ExoscaleNodeClass
	if err := c.kubeClient.Get(ctx, client.ObjectKey{Name: ref.Name}, &nodeClass); err != nil {
		return nil, fmt.Errorf("failed to get node class %s: %w", ref.Name, err)
	}
	return &nodeClass, nil
}

func (c *CloudProvider) drainNode(ctx context.Context, nodeName string) error {
	// Karpenter sets the node name once the kubelet registered.
	if nodeName == "" {
		return nil
	}
	var node v1.Node
	if err := c.kubeClient.Get(ctx, client.ObjectKey{Name: nodeName}, &node); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if !node.Spec.Unschedulable {
		patch := client.MergeFrom(node.DeepCopy())
		node.Spec.Unschedulable = true
		if err := c.kubeClient.Patch(ctx, &node, patch); err != nil {
			return err
		}
	}

	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 15*time.Minute, true, func(ctx context.Context) (bool, error) {
		var podList v1.PodList
		if err := c.kubeClient.List(ctx, &podList, client.MatchingFields{"spec.nodeName": nodeName}); err != nil {
			return false, nil
		}

		drained := true
		for _, pod := range podList.Items {
			if slices.ContainsFunc(pod.OwnerReferences, func(o metav1.OwnerReference) bool { return o.Kind == "DaemonSet" }) {
				continue
			}

			if pod.DeletionTimestamp != nil {
				drained = false
				continue
			}

			err := c.kubeClient.SubResource("eviction").Create(ctx, &pod, &policyv1.Eviction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				},
			})
			if errors.IsNotFound(err) {
				continue
			}
			drained = false
			if err != nil {
				log.FromContext(ctx).Error(err, "failed to evict pod", "pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name))
			} else {
				log.FromContext(ctx).Info("evicted pod", "pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name))
			}
		}
		return drained, nil
	})
	if err != nil && ctx.Err() == nil {
		log.FromContext(ctx).Error(err, "giving up on drain after 15m", "nodeName", nodeName)
		return nil
	}
	return err
}

func (c *CloudProvider) publishDrift(nodeClaim *karpenterv1.NodeClaim, message string) {
	c.recorder.Publish(events.Event{
		InvolvedObject: nodeClaim,
		Type:           v1.EventTypeNormal,
		Reason:         "DriftDetected",
		Message:        message,
	})
}
