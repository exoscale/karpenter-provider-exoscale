package instance

import (
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/instancetype"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/template"
	"github.com/samber/lo"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
)

const ExoscaleProviderIDPrefix = "exoscale://"

type Instance struct {
	ID                 string
	Name               string
	State              egov3.InstanceState
	InstanceType       *egov3.InstanceType
	InstanceTypeName   string
	Template           template.Template
	Zone               string
	DiskSize           int64
	Labels             map[string]string
	SecurityGroups     []string
	PrivateNetworks    []string
	AntiAffinityGroups []string
	Capacity           map[v1.ResourceName]resource.Quantity
	Addresses          []v1.NodeAddress
	CreatedAt          time.Time
}

func FromExoscaleInstance(instance *egov3.Instance, instanceType *cloudprovider.InstanceType, zone string) *Instance {
	if instance == nil || instanceType == nil {
		return nil
	}

	i := &Instance{
		ID:           instance.ID.String(),
		Name:         instance.Name,
		State:        instance.State,
		InstanceType: instance.InstanceType,
		Template:     template.FromExoscaleTemplate(instance.Template),
		Zone:         zone,
		DiskSize:     instance.DiskSize,
		Labels:       instance.Labels,
		CreatedAt:    instance.CreatedAT,
		Capacity:     make(map[v1.ResourceName]resource.Quantity),
		Addresses: []v1.NodeAddress{
			{
				Type:    v1.NodeHostName,
				Address: instance.Name,
			},
		},
	}

	// Preprovision IPv4 addresses in order to ensure konnectivity can connect very early to the nodes
	if instance.PublicIP != nil {
		i.Addresses = append(i.Addresses, v1.NodeAddress{
			Type:    v1.NodeExternalIP,
			Address: instance.PublicIP.String(),
		})
		i.Addresses = append(i.Addresses, v1.NodeAddress{
			Type:    v1.NodeInternalIP,
			Address: instance.PublicIP.String(),
		})
	}

	family := string(instance.InstanceType.Family)
	size := string(instance.InstanceType.Size)
	i.InstanceTypeName = family + "." + size

	for _, sg := range instance.SecurityGroups {
		i.SecurityGroups = append(i.SecurityGroups, string(sg.ID))
	}

	for _, pn := range instance.PrivateNetworks {
		i.PrivateNetworks = append(i.PrivateNetworks, string(pn.ID))
	}

	for _, aag := range instance.AntiAffinityGroups {
		i.AntiAffinityGroups = append(i.AntiAffinityGroups, string(aag.ID))
	}

	// Capacities

	if cpuQuantity, ok := instanceType.Capacity[v1.ResourceCPU]; ok {
		i.InstanceType.Cpus = cpuQuantity.Value()
		i.Capacity[v1.ResourceCPU] = cpuQuantity
	}
	if memQuantity, ok := instanceType.Capacity[v1.ResourceMemory]; ok {
		i.InstanceType.Memory = memQuantity.Value()
		i.Capacity[v1.ResourceMemory] = memQuantity
	}
	if gpuQuantity, ok := instanceType.Capacity[instancetype.ResourceNvidiaGPU]; ok {
		i.InstanceType.Gpus = gpuQuantity.Value()
		i.Capacity[instancetype.ResourceNvidiaGPU] = gpuQuantity
	}

	i.Capacity[v1.ResourceEphemeralStorage] = *resource.NewQuantity(instance.DiskSize*1024*1024*1024, resource.BinarySI)
	i.Capacity[v1.ResourcePods] = *resource.NewQuantity(110, resource.DecimalSI)

	return i
}

// ToNodeClaim creates a NodeClaim from the instance
func (i *Instance) ToNodeClaim() *karpenterv1.NodeClaim {
	capacity := v1.ResourceList{}
	allocatable := v1.ResourceList{}
	for k, v := range i.Capacity {
		capacity[k] = v.DeepCopy()
		allocatable[k] = v.DeepCopy()
	}

	instanceLabels := map[string]string{
		v1.LabelTopologyZone:             i.Zone,
		v1.LabelInstanceTypeStable:       i.InstanceTypeName,
		v1.LabelArchStable:               "amd64",
		karpenterv1.CapacityTypeLabelKey: karpenterv1.CapacityTypeOnDemand,
	}

	nodeClaim := &karpenterv1.NodeClaim{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.Time{Time: i.CreatedAt},
			Labels:            lo.Assign(instanceLabels, i.Template.Labels),
		},
		Status: karpenterv1.NodeClaimStatus{
			ProviderID:  ExoscaleProviderIDPrefix + i.ID,
			ImageID:     i.Template.ID,
			Capacity:    capacity,
			Allocatable: allocatable,
		},
	}

	if nodeClaimName, ok := i.Labels[constants.InstanceLabelNodeClaim]; ok {
		nodeClaim.Name = nodeClaimName
	}

	return nodeClaim
}

func (i *Instance) GetCapacityAndAllocatable() (v1.ResourceList, v1.ResourceList) {
	capacity := v1.ResourceList{}
	allocatable := v1.ResourceList{}
	for k, v := range i.Capacity {
		capacity[k] = v.DeepCopy()
		allocatable[k] = v.DeepCopy()
	}
	return capacity, allocatable
}
