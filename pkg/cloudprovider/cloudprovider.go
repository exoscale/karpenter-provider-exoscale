package cloudprovider

import (
	"context"
	stderrors "errors"
	"fmt"
	"reflect"
	"time"

	"github.com/awslabs/operatorpkg/status"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instance"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instancetype"
	"github.com/exoscale/karpenter-exoscale/pkg/utils"
	"github.com/samber/lo"
	v1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	karpentertypes "sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/events"
)

const (
	DriftReasonImageID            cloudprovider.DriftReason = "ImageID"
	DriftReasonAntiAffinityGroups cloudprovider.DriftReason = "AntiAffinityGroups"
	DriftReasonSecurityGroups     cloudprovider.DriftReason = "SecurityGroups"
	DriftReasonPrivateNetworks    cloudprovider.DriftReason = "PrivateNetworks"
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

	return nc, nil
}

// Karpenter 1.8 doc says:
// Delete removes a NodeClaim from the cloudprovider by its provider id. Delete should return
// NodeClaimNotFoundError if the cloudProvider instance is already terminated and nil if deletion was triggered.
// Karpenter will keep retrying until Delete returns a NodeClaimNotFound error.
func (c *CloudProvider) Delete(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) error {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"method", "Delete",
		"nodeClaim", nodeClaim.Name,
	))

	// karpenter has its own draining logic when it decides to downscale a nodepool,
	// but we want to be very conservative for people deleting nodeclaims directly
	log.FromContext(ctx).Info("draining node", "nodeName", nodeClaim.Status.NodeName)
	if err := c.drainNode(ctx, nodeClaim.Status.NodeName); err != nil {
		log.FromContext(ctx).Error(err, "failed to drain node", "nodeName", nodeClaim.Status.NodeName)
		return fmt.Errorf("failed to drain node %s: %w", nodeClaim.Status.NodeName, err)
	}

	log.FromContext(ctx).Info("node drained", "nodeName", nodeClaim.Status.NodeName)

	instanceID, err := utils.ParseProviderID(nodeClaim.Status.ProviderID)
	if err != nil {
		return fmt.Errorf("failed to parse provider ID %s: %w", nodeClaim.Status.ProviderID, err)
	}

	log.FromContext(ctx).Info("deleting instance", "instanceID", instanceID)
	if err := c.instanceProvider.Delete(ctx, instanceID); err != nil && !c.instanceProvider.IsNotFoundError(err) {
		log.FromContext(ctx).Error(err, "failed to delete instance", "instanceID", instanceID)
		return err
	}

	log.FromContext(ctx).Info("cloud instance deletion completed", "instanceID", instanceID)
	// Implementation require to return NodeClaimNotFoundError if the instance is removed
	return karpentertypes.NewNodeClaimNotFoundError(nil)
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
			return nil, karpentertypes.NewNodeClaimNotFoundError(err)
		}
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
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"method", "GetInstanceTypes",
		"nodePool", nodePool.Name,
	))

	nodeClass, err := c.resolveNodeClassFromNodePool(ctx, nodePool)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to resolve node class")
		return nil, fmt.Errorf("failed to resolve node class: %w", err)
	}

	instanceTypes, err := c.instanceTypeProvider.List(nodeClass)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to list instance types")
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
		c.publishEvent(nodeClaim, v1.EventTypeNormal, "DriftDetected",
			fmt.Sprintf("Instance template ID drift detected: nodeClaim ImageID %s != instance Template ID %s", nodeClaim.Status.ImageID, inst.Template.ID))
		return DriftReasonImageID, nil
	}

	left, right := lo.Difference(nodeClass.Spec.AntiAffinityGroups, inst.AntiAffinityGroups)
	if len(left) != 0 || len(right) != 0 {
		log.FromContext(ctx).Info("detected anti-affinity groups drift", "reason", DriftReasonAntiAffinityGroups)
		c.publishEvent(nodeClaim, v1.EventTypeNormal, "DriftDetected",
			fmt.Sprintf("Instance anti-affinity groups drift detected: nodeClass AntiAffinityGroups %v != instance AntiAffinityGroups %v", nodeClass.Spec.AntiAffinityGroups, inst.AntiAffinityGroups))
		return DriftReasonAntiAffinityGroups, nil
	}

	left, right = lo.Difference(nodeClass.Spec.SecurityGroups, inst.SecurityGroups)
	if len(left) != 0 || len(right) != 0 {
		log.FromContext(ctx).Info("detected security groups drift", "reason", DriftReasonSecurityGroups)
		c.publishEvent(nodeClaim, v1.EventTypeNormal, "DriftDetected",
			fmt.Sprintf("Instance security groups drift detected: nodeClass SecurityGroups %v != instance SecurityGroups %v", nodeClass.Spec.SecurityGroups, inst.SecurityGroups))
		return DriftReasonSecurityGroups, nil
	}

	left, right = lo.Difference(nodeClass.Spec.PrivateNetworks, inst.PrivateNetworks)
	if len(left) != 0 || len(right) != 0 {
		log.FromContext(ctx).Info("detected private networks drift", "reason", DriftReasonPrivateNetworks)
		c.publishEvent(nodeClaim, v1.EventTypeNormal, "DriftDetected",
			fmt.Sprintf("Instance private networks drift detected: nodeClass PrivateNetworks %v != instance PrivateNetworks %v", nodeClass.Spec.PrivateNetworks, inst.PrivateNetworks))
		return DriftReasonPrivateNetworks, nil
	}

	// fix instance labels drift
	expectedInstanceLabels := c.instanceProvider.GenerateInstanceLabels(nodeClaim)
	if !reflect.DeepEqual(expectedInstanceLabels, inst.Labels) {
		log.FromContext(ctx).Info("detected instance labels drift, fixing them", "reason", "Labels")
		if err := c.instanceProvider.UpdateTags(ctx, inst.ID, expectedInstanceLabels); err != nil {
			log.FromContext(ctx).Error(err, "failed to update instance labels", "instanceID", instanceID)
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

func (c *CloudProvider) resolveNodeClassFromNodeClaim(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) (*apiv1.ExoscaleNodeClass, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"method", "resolveNodeClassFromNodeClaim",
		"nodeClaim", nodeClaim.Name,
	))

	nodeClassRef := nodeClaim.Spec.NodeClassRef
	if nodeClassRef == nil {
		return nil, fmt.Errorf("nodeClassRef not specified in NodeClaim")
	}

	var nodeClass apiv1.ExoscaleNodeClass
	if err := c.kubeClient.Get(ctx, client.ObjectKey{
		Name: nodeClassRef.Name,
	}, &nodeClass); err != nil {
		return nil, fmt.Errorf("failed to get node class %s: %w", nodeClassRef.Name, err)
	}

	log.FromContext(ctx).V(1).Info("retrieved node class", "nodeClass", nodeClass.Name)
	return &nodeClass, nil
}

func (c *CloudProvider) resolveNodeClassFromNodePool(ctx context.Context, nodePool *karpenterv1.NodePool) (*apiv1.ExoscaleNodeClass, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"method", "resolveNodeClassFromNodePool",
		"nodePool", nodePool.Name,
	))

	nodeClassRef := nodePool.Spec.Template.Spec.NodeClassRef
	if nodeClassRef == nil {
		return nil, fmt.Errorf("nodeClassRef not specified in NodePool")
	}

	var nodeClass apiv1.ExoscaleNodeClass
	if err := c.kubeClient.Get(ctx, client.ObjectKey{
		Name: nodeClassRef.Name,
	}, &nodeClass); err != nil {
		return nil, fmt.Errorf("failed to get node class %s: %w", nodeClassRef.Name, err)
	}

	log.FromContext(ctx).V(1).Info("retrieved node class from node pool", "nodeClass", nodeClass.Name)
	return &nodeClass, nil
}

func (c *CloudProvider) drainNode(ctx context.Context, nodeName string) error {
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

	evictStartTime := time.Now()
	for {
		if time.Since(evictStartTime) > 15*time.Minute {
			log.FromContext(ctx).Error(fmt.Errorf("timed out waiting for pods to be evicted"), "nodeName", nodeName, "timeout", 5*time.Minute)
			return nil
		}

		evictionFailed := false
		foundInDeletionPods := false
		justEvictedPod := false

		// List pods on the node
		var podList v1.PodList
		if err := c.kubeClient.List(ctx, &podList, client.MatchingFields{"spec.nodeName": nodeName}); err != nil {
			goto nextAttempt
		}

		for _, pod := range podList.Items {
			// Ignore DaemonSet pods
			isDaemonSet := false
			for _, owner := range pod.OwnerReferences {
				if owner.Kind == "DaemonSet" {
					isDaemonSet = true
					break
				}
			}
			if isDaemonSet {
				continue
			}

			// Ignore in deletion pods
			if pod.DeletionTimestamp != nil {
				foundInDeletionPods = true
				continue
			}

			// Evict pod
			if err := c.kubeClient.SubResource("eviction").Create(ctx, &pod, &policyv1.Eviction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				},
			}); err != nil {
				if !errors.IsNotFound(err) {
					log.FromContext(ctx).Error(err, "failed to evict pod", "pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name))
					evictionFailed = true
				}
			} else {
				log.FromContext(ctx).Info("evicted pod", "pod", fmt.Sprintf("%s/%s", pod.Namespace, pod.Name))
				justEvictedPod = true
			}
		}

		// No more pods to evict and we didn't evicted one now, we can delete node
		if !evictionFailed && !foundInDeletionPods && !justEvictedPod || len(podList.Items) == 0 {
			break
		}

		// Wait before next eviction attempt
	nextAttempt:
		time.Sleep(time.Second * 5)
	}

	return nil
}

func (c *CloudProvider) publishEvent(nodeClaim *karpenterv1.NodeClaim, eventType, reason, message string) {
	c.recorder.Publish(events.Event{
		InvolvedObject: nodeClaim,
		Type:           eventType,
		Reason:         reason,
		Message:        message,
	})
}
