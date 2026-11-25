package instance

import (
	"net"
	"testing"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/utils"
	"github.com/samber/lo"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
)

func TestFromExoscaleInstance(t *testing.T) {
	instanceID := "instance-123"
	instanceName := "test-instance"
	zone := "ch-gva-2"

	exoInstance := &egov3.Instance{
		ID:       egov3.UUID(instanceID),
		Name:     instanceName,
		State:    egov3.InstanceStateRunning,
		DiskSize: 50,
		Labels: map[string]string{
			constants.InstanceLabelNodeClaim: "test-claim",
		},
		CreatedAT: time.Now(),
		InstanceType: &egov3.InstanceType{
			Family: "standard",
			Size:   "medium",
		},
		Template: &egov3.Template{
			ID: egov3.UUID("template-123"),
		},
	}

	instanceType := &cloudprovider.InstanceType{
		Capacity: v1.ResourceList{
			v1.ResourceCPU:    *resource.NewQuantity(2, resource.DecimalSI),
			v1.ResourceMemory: *resource.NewQuantity(4*1024*1024*1024, resource.BinarySI),
		},
	}

	got := FromExoscaleInstance(exoInstance, instanceType, zone)

	if got.ID != instanceID {
		t.Errorf("FromExoscaleInstance() ID = %v, want %v", got.ID, instanceID)
	}

	if got.Name != instanceName {
		t.Errorf("FromExoscaleInstance() Name = %v, want %v", got.Name, instanceName)
	}

	if got.Zone != zone {
		t.Errorf("FromExoscaleInstance() Zone = %v, want %v", got.Zone, zone)
	}

	if got.InstanceTypeName != "standard.medium" {
		t.Errorf("FromExoscaleInstance() InstanceTypeName = %v, want standard.medium", got.InstanceTypeName)
	}
}

func TestFromExoscaleInstance_WithAddresses(t *testing.T) {
	instanceID := "instance-456"
	instanceName := "test-instance-with-ip"
	zone := "ch-gva-2"

	exoInstance := &egov3.Instance{
		ID:          egov3.UUID(instanceID),
		Name:        instanceName,
		State:       egov3.InstanceStateRunning,
		DiskSize:    50,
		PublicIP:    net.IP{192, 168, 1, 100},
		Ipv6Address: "fe80::1:2:3",
		Labels: map[string]string{
			constants.InstanceLabelNodeClaim: "test-claim-with-ip",
		},
		CreatedAT: time.Now(),
		InstanceType: &egov3.InstanceType{
			Family: "standard",
			Size:   "medium",
		},
		Template: &egov3.Template{
			ID: egov3.UUID("template-456"),
		},
	}

	instanceType := &cloudprovider.InstanceType{
		Capacity: v1.ResourceList{
			v1.ResourceCPU:    *resource.NewQuantity(2, resource.DecimalSI),
			v1.ResourceMemory: *resource.NewQuantity(4*1024*1024*1024, resource.BinarySI),
		},
	}

	got := FromExoscaleInstance(exoInstance, instanceType, zone)

	if len(got.Addresses) != 5 {
		t.Fatalf("FromExoscaleInstance() Addresses count = %d, want 5", len(got.Addresses))
	}

	if got.Addresses[0].Type != v1.NodeHostName {
		t.Errorf("Addresses[0].Type = %v, want %v", got.Addresses[0].Type, v1.NodeHostName)
	}
	if got.Addresses[0].Address != instanceName {
		t.Errorf("Addresses[0].Address = %v, want %v", got.Addresses[0].Address, instanceName)
	}

	expectedIP := "192.168.1.100"
	if got.Addresses[1].Type != v1.NodeExternalIP {
		t.Errorf("Addresses[1].Type = %v, want %v", got.Addresses[1].Type, v1.NodeExternalIP)
	}
	if got.Addresses[1].Address != expectedIP {
		t.Errorf("Addresses[1].Address = %v, want %v", got.Addresses[1].Address, expectedIP)
	}

	if got.Addresses[2].Type != v1.NodeInternalIP {
		t.Errorf("Addresses[2].Type = %v, want %v", got.Addresses[2].Type, v1.NodeInternalIP)
	}
	if got.Addresses[2].Address != expectedIP {
		t.Errorf("Addresses[2].Address = %v, want %v", got.Addresses[2].Address, expectedIP)
	}

	expectedIPv6 := "fe80::1:2:3"
	if got.Addresses[3].Type != v1.NodeExternalIP {
		t.Errorf("Addresses[3].Type = %v, want %v", got.Addresses[3].Type, v1.NodeExternalIP)
	}
	if got.Addresses[3].Address != expectedIPv6 {
		t.Errorf("Addresses[3].Address = %v, want %v", got.Addresses[3].Address, expectedIPv6)
	}

	if got.Addresses[4].Type != v1.NodeInternalIP {
		t.Errorf("Addresses[4].Type = %v, want %v", got.Addresses[4].Type, v1.NodeInternalIP)
	}
	if got.Addresses[4].Address != expectedIPv6 {
		t.Errorf("Addresses[4].Address = %v, want %v", got.Addresses[4].Address, expectedIPv6)
	}
}

func TestInstance_ToNodeClaim(t *testing.T) {
	instance := &Instance{
		ID:               "instance-123",
		Name:             "test-instance",
		InstanceTypeName: "standard.medium",
		Zone:             "ch-gva-2",
		Capacity: map[v1.ResourceName]resource.Quantity{
			v1.ResourceCPU:    *resource.NewQuantity(2, resource.DecimalSI),
			v1.ResourceMemory: *resource.NewQuantity(4*1024*1024*1024, resource.BinarySI),
		},
		Labels: map[string]string{
			constants.InstanceLabelNodeClaim: "test-claim",
		},
		CreatedAt: time.Now(),
	}

	got := instance.ToNodeClaim()

	if got.Name != "test-claim" {
		t.Errorf("ToNodeClaim() Name = %v, want test-claim", got.Name)
	}

	if got.Status.ProviderID != utils.ExoscaleProviderIDPrefix+"instance-123" {
		t.Errorf("ToNodeClaim() ProviderID = %v, want %v", got.Status.ProviderID, utils.ExoscaleProviderIDPrefix+"instance-123")
	}

	if got.Labels[v1.LabelInstanceTypeStable] != "standard.medium" {
		t.Errorf("ToNodeClaim() instance type label = %v, want standard.medium", got.Labels[v1.LabelInstanceTypeStable])
	}

	if got.Labels[karpenterv1.CapacityTypeLabelKey] != karpenterv1.CapacityTypeOnDemand {
		t.Errorf("ToNodeClaim() capacity type = %v, want %v", got.Labels[karpenterv1.CapacityTypeLabelKey], karpenterv1.CapacityTypeOnDemand)
	}
}

func TestInstance_IsRunning(t *testing.T) {
	tests := []struct {
		name  string
		state egov3.InstanceState
		want  bool
	}{
		{name: "running", state: egov3.InstanceStateRunning, want: true},
		{name: "starting", state: egov3.InstanceStateStarting, want: false},
		{name: "stopping", state: egov3.InstanceStateStopping, want: false},
		{name: "stopped", state: egov3.InstanceStateStopped, want: false},
		{name: "destroying", state: egov3.InstanceStateDestroying, want: false},
		{name: "error", state: egov3.InstanceStateError, want: false},
		{name: "empty", state: egov3.InstanceState(""), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := &Instance{State: tt.state}
			if got := i.IsRunning(); got != tt.want {
				t.Errorf("IsRunning() with state %q = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}

func TestFromExoscaleInstance_WithVpc(t *testing.T) {
	zone := "ch-gva-2"

	exoInstance := &egov3.Instance{
		ID:    egov3.UUID("instance-vpc"),
		Name:  "test-instance-vpc",
		State: egov3.InstanceStateRunning,
		InstanceType: &egov3.InstanceType{
			Family: "standard",
			Size:   "medium",
		},
		Template: &egov3.Template{ID: egov3.UUID("template-123")},
		Vpc: &egov3.InstanceVpc{
			ID:   egov3.UUID("11111111-0000-0000-0000-000000000001"),
			Name: "my-vpc",
			Subnets: []egov3.InstanceVpcSubnets{
				{ID: egov3.UUID("440e8400-e29b-41d4-a716-446655440030"), Name: "subnet-1"},
				{ID: egov3.UUID("550e8400-e29b-41d4-a716-446655440031"), Name: "subnet-2"},
			},
		},
	}

	instanceType := &cloudprovider.InstanceType{
		Capacity: v1.ResourceList{
			v1.ResourceCPU:    *resource.NewQuantity(2, resource.DecimalSI),
			v1.ResourceMemory: *resource.NewQuantity(4*1024*1024*1024, resource.BinarySI),
		},
	}

	got := FromExoscaleInstance(exoInstance, instanceType, zone)

	if got.VpcID != "11111111-0000-0000-0000-000000000001" {
		t.Errorf("FromExoscaleInstance() VpcID = %v, want %v", got.VpcID, "11111111-0000-0000-0000-000000000001")
	}

	wantSubnets := []string{"440e8400-e29b-41d4-a716-446655440030", "550e8400-e29b-41d4-a716-446655440031"}
	if len(got.VpcSubnets) != len(wantSubnets) {
		t.Fatalf("FromExoscaleInstance() VpcSubnets = %v, want %v", got.VpcSubnets, wantSubnets)
	}
	for i, want := range wantSubnets {
		if got.VpcSubnets[i] != want {
			t.Errorf("FromExoscaleInstance() VpcSubnets[%d] = %v, want %v", i, got.VpcSubnets[i], want)
		}
	}
}

func TestFromExoscaleInstance_WithoutVpc(t *testing.T) {
	exoInstance := &egov3.Instance{
		ID:    egov3.UUID("instance-novpc"),
		Name:  "test-instance-novpc",
		State: egov3.InstanceStateRunning,
		InstanceType: &egov3.InstanceType{
			Family: "standard",
			Size:   "medium",
		},
		Template: &egov3.Template{ID: egov3.UUID("template-123")},
	}

	instanceType := &cloudprovider.InstanceType{
		Capacity: v1.ResourceList{
			v1.ResourceCPU: *resource.NewQuantity(2, resource.DecimalSI),
		},
	}

	got := FromExoscaleInstance(exoInstance, instanceType, "ch-gva-2")
	if got.VpcID != "" {
		t.Errorf("FromExoscaleInstance() VpcID = %v, want empty", got.VpcID)
	}
	if len(got.VpcSubnets) != 0 {
		t.Errorf("FromExoscaleInstance() VpcSubnets = %v, want empty", got.VpcSubnets)
	}
}

func TestFromExoscaleInstance_IPForwarding(t *testing.T) {
	instanceType := &cloudprovider.InstanceType{
		Capacity: v1.ResourceList{
			v1.ResourceCPU: *resource.NewQuantity(2, resource.DecimalSI),
		},
	}

	newInstance := func(ipForwarding *bool) *egov3.Instance {
		return &egov3.Instance{
			ID:           egov3.UUID("instance-ipfwd"),
			Name:         "test-instance-ipfwd",
			State:        egov3.InstanceStateRunning,
			InstanceType: &egov3.InstanceType{Family: "standard", Size: "medium"},
			Template:     &egov3.Template{ID: egov3.UUID("template-123")},
			IPForwarding: ipForwarding,
		}
	}

	got := FromExoscaleInstance(newInstance(lo.ToPtr(true)), instanceType, "ch-gva-2")
	if !got.IPForwarding {
		t.Error("FromExoscaleInstance() IPForwarding = false, want true")
	}

	got = FromExoscaleInstance(newInstance(lo.ToPtr(false)), instanceType, "ch-gva-2")
	if got.IPForwarding {
		t.Error("FromExoscaleInstance() IPForwarding = true, want false")
	}

	got = FromExoscaleInstance(newInstance(nil), instanceType, "ch-gva-2")
	if got.IPForwarding {
		t.Error("FromExoscaleInstance() IPForwarding = true for nil, want false")
	}
}
