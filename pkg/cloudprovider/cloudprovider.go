package cloudprovider

import (
	"context"

	"github.com/awslabs/operatorpkg/status"
	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
)

type CloudProvider struct {
	kubeClient    client.Client
	dynClient     *dynamic.DynamicClient
	exoClient     *egov3.Client
	instanceTypes []*cloudprovider.InstanceType
}

func NewCloudProvider(ctx context.Context, kubeClient client.Client, dynClient *dynamic.DynamicClient, exoClient *egov3.Client) *CloudProvider {
	return &CloudProvider{
		kubeClient: kubeClient,
		dynClient:  dynClient,
		exoClient:  exoClient,
		instanceTypes: ConstructInstanceTypes(cloudprovider.InstanceTypeOverhead{
			// TOOD discover this from somewhere
			KubeReserved: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("100m"),
				v1.ResourceMemory: resource.MustParse("100Mi"),
			},
			SystemReserved: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("100m"),
				v1.ResourceMemory: resource.MustParse("100Mi"),
			},
		}),
	}
}

func (c *CloudProvider) Create(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) (*karpenterv1.NodeClaim, error) {

	// if err := c.kubeClient.Create(ctx, node); err != nil {
	// 	return created, fmt.Errorf("failed to create node %s: %w", node.Name, err)
	// }

	// return created, nil
	return nil, nil
}

func (c *CloudProvider) Delete(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) error {
	//return c.delete(ctx, nodeClaim.Status.ProviderID)
	return nil
}

func (c *CloudProvider) Get(_ context.Context, providerID string) (*karpenterv1.NodeClaim, error) {
	//return claims, nil
	return nil, nil
}

func (c *CloudProvider) List(_ context.Context) ([]*karpenterv1.NodeClaim, error) {
	// TODO
	claims := []*karpenterv1.NodeClaim{}
	return claims, nil
}

func (c *CloudProvider) GetInstanceTypes(ctx context.Context, nodePool *karpenterv1.NodePool) ([]*cloudprovider.InstanceType, error) {
	return c.instanceTypes, nil
}

func (c *CloudProvider) IsDrifted(_ context.Context, nodeClaim *karpenterv1.NodeClaim) (cloudprovider.DriftReason, error) {
	//return cloudprovider.DriftReason("DriftReasonNetworkID"), nil
	return "", nil
}

func (c *CloudProvider) GetSupportedNodeClasses() []status.Object {
	return []status.Object{&apiv1.ExoscaleNodeClass{}}
}

func (c *CloudProvider) Name() string {
	return "Exoscale"
}
