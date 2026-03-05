package instance

import (
	"net"
	"testing"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/karpenter-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-exoscale/pkg/utils"
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

	if got == nil {
		t.Fatal("FromExoscaleInstance() returned nil")
	}

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
		ID:       egov3.UUID(instanceID),
		Name:     instanceName,
		State:    egov3.InstanceStateRunning,
		DiskSize: 50,
		PublicIP: net.IP{192, 168, 1, 100},
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

	if got == nil {
		t.Fatal("FromExoscaleInstance() returned nil")
	}

	// Verify we have exactly 3 addresses
	if len(got.Addresses) != 3 {
		t.Fatalf("FromExoscaleInstance() Addresses count = %d, want 3", len(got.Addresses))
	}

	// Verify we have a default Hostname
	if got.Addresses[0].Type != v1.NodeHostName {
		t.Errorf("Addresses[0].Type = %v, want %v", got.Addresses[0].Type, v1.NodeHostName)
	}
	if got.Addresses[0].Address != instanceName {
		t.Errorf("Addresses[0].Address = %v, want %v", got.Addresses[0].Address, instanceName)
	}

	// Verify the external IP address
	expectedIP := "192.168.1.100"
	if got.Addresses[1].Type != v1.NodeExternalIP {
		t.Errorf("Addresses[1].Type = %v, want %v", got.Addresses[1].Type, v1.NodeExternalIP)
	}
	if got.Addresses[1].Address != expectedIP {
		t.Errorf("Addresses[1].Address = %v, want %v", got.Addresses[1].Address, expectedIP)
	}

	// Verify the internal IP address (same as external)
	if got.Addresses[2].Type != v1.NodeInternalIP {
		t.Errorf("Addresses[2].Type = %v, want %v", got.Addresses[2].Type, v1.NodeInternalIP)
	}
	if got.Addresses[2].Address != expectedIP {
		t.Errorf("Addresses[2].Address = %v, want %v", got.Addresses[2].Address, expectedIP)
	}
}

func TestFromExoscaleInstance_NilInputs(t *testing.T) {
	tests := []struct {
		name         string
		instance     *egov3.Instance
		instanceType *cloudprovider.InstanceType
	}{
		{
			name:         "nil instance",
			instance:     nil,
			instanceType: &cloudprovider.InstanceType{},
		},
		{
			name: "nil instanceType",
			instance: &egov3.Instance{
				ID: egov3.UUID("test"),
			},
			instanceType: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromExoscaleInstance(tt.instance, tt.instanceType, "test-zone")
			if got != nil {
				t.Errorf("FromExoscaleInstance() = %v, want nil", got)
			}
		})
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

	if got == nil {
		t.Fatal("ToNodeClaim() returned nil")
	}

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
