// +groupName=karpenter.exoscale.com
package v1

import (
	"github.com/awslabs/operatorpkg/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
)

func init() {
	metav1.AddToGroupVersion(scheme.Scheme, GroupVersion)
	scheme.Scheme.AddKnownTypes(GroupVersion,
		&ExoscaleNodeClass{},
		&ExoscaleNodeClassList{})
	SchemeBuilder.Register(&ExoscaleNodeClass{}, &ExoscaleNodeClassList{})
}

// ExoscaleNodeClass is the Schema for the ExoscaleNodeClass API
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories={karpenter}
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status",description=""
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""
// +kubebuilder:storageversion
type ExoscaleNodeClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec   ExoscaleNodeClassSpec   `json:"spec"`
	Status ExoscaleNodeClassStatus `json:"status,omitempty"`
}

type ImageTemplateSelector struct {
	// +optional
	// +kubebuilder:validation:Pattern="^[0-9]+\\.[0-9]+\\.[0-9]+$"
	Version string `json:"version,omitempty"`

	// +optional
	// +kubebuilder:default="standard"
	// +kubebuilder:validation:Enum=standard;nvidia
	Variant string `json:"variant,omitempty"`
}

// ExoscaleNodeClassSpec defines the desired state of ExoscaleNodeClass
// +kubebuilder:validation:XValidation:rule="!has(self.kubelet) || !has(self.kubelet.imageGCHighThresholdPercent) || !has(self.kubelet.imageGCLowThresholdPercent) || self.kubelet.imageGCLowThresholdPercent < self.kubelet.imageGCHighThresholdPercent",message="imageGCLowThresholdPercent must be less than imageGCHighThresholdPercent"
// +kubebuilder:validation:XValidation:rule="(has(self.templateID) && !has(self.imageTemplateSelector)) || (!has(self.templateID) && has(self.imageTemplateSelector))",message="exactly one of templateID or imageTemplateSelector must be specified"
type ExoscaleNodeClassSpec struct {
	// +optional
	// +kubebuilder:validation:Pattern="^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"
	TemplateID string `json:"templateID,omitempty"`

	// +optional
	ImageTemplateSelector *ImageTemplateSelector `json:"imageTemplateSelector,omitempty"`

	// DiskSize is the size of the root disk in GB
	// +kubebuilder:validation:Minimum=10
	// +kubebuilder:validation:Maximum=8000
	// +kubebuilder:default=50
	// +optional
	DiskSize int64 `json:"diskSize,omitempty"`

	// SecurityGroups is a list of security group IDs to attach to instances
	// +optional
	// +kubebuilder:validation:MaxItems=50
	SecurityGroups []string `json:"securityGroups,omitempty"`

	// AntiAffinityGroups is a list of anti-affinity group IDs
	// +optional
	AntiAffinityGroups []string `json:"antiAffinityGroups,omitempty"`

	// PrivateNetworks is a list of private network IDs to attach to instances
	// +optional
	// +kubebuilder:validation:MaxItems=10
	PrivateNetworks []string `json:"privateNetworks,omitempty"`

	// Kubelet contains configuration for kubelet
	// +optional
	Kubelet KubeletConfiguration `json:"kubelet,omitempty"`
}

type KubeletConfiguration struct {
	// ClusterDNS is a list of IP addresses for the cluster DNS server
	// +kubebuilder:default={"10.96.0.10"}
	// +optional
	ClusterDNS []string `json:"clusterDNS,omitempty"`

	// ImageGCHighThresholdPercent is the disk usage percentage at which image garbage collection is triggered
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=85
	// +optional
	ImageGCHighThresholdPercent *int32 `json:"imageGCHighThresholdPercent,omitempty"`

	// ImageGCLowThresholdPercent is the disk usage percentage below which image garbage collection stops
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=80
	// +optional
	ImageGCLowThresholdPercent *int32 `json:"imageGCLowThresholdPercent,omitempty"`

	// ImageMinimumGCAge is the minimum age for an unused image before it can be garbage collected
	// Example: "2m" for 2 minutes
	// +kubebuilder:default="2m"
	// +kubebuilder:validation:Pattern="^[0-9]+(s|m|h)$"
	// +optional
	ImageMinimumGCAge string `json:"imageMinimumGCAge,omitempty"`

	// KubeReserved is resources reserved for Kubernetes system components
	// +optional
	KubeReserved KubeResourceReservation `json:"kubeReserved,omitempty"`

	// SystemReserved is resources reserved for OS system components
	// +optional
	SystemReserved SystemResourceReservation `json:"systemReserved,omitempty"`
}

type KubeResourceReservation struct {
	// CPU reservation for Kubernetes components
	// +kubebuilder:default="200m"
	// +optional
	// +kubebuilder:validation:Pattern="^(\\d+(m|[.])?(\\d+)?|\\d+m)$"
	CPU string `json:"cpu,omitempty"`

	// Memory reservation for Kubernetes components
	// +kubebuilder:default="300Mi"
	// +optional
	// +kubebuilder:validation:Pattern="^(\\d+(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?)$"
	Memory string `json:"memory,omitempty"`

	// EphemeralStorage reservation for Kubernetes components
	// +kubebuilder:default="1Gi"
	// +optional
	// +kubebuilder:validation:Pattern="^(\\d+(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?)$"
	EphemeralStorage string `json:"ephemeralStorage,omitempty"`
}

type SystemResourceReservation struct {
	// CPU reservation for system components
	// +kubebuilder:default="100m"
	// +optional
	// +kubebuilder:validation:Pattern="^(\\d+(m|[.])?(\\d+)?|\\d+m)$"
	CPU string `json:"cpu,omitempty"`

	// Memory reservation for system components
	// +kubebuilder:default="100Mi"
	// +optional
	// +kubebuilder:validation:Pattern="^(\\d+(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?)$"
	Memory string `json:"memory,omitempty"`

	// EphemeralStorage reservation for system components
	// +kubebuilder:default="3Gi"
	// +optional
	// +kubebuilder:validation:Pattern="^(\\d+(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?)$"
	EphemeralStorage string `json:"ephemeralStorage,omitempty"`
}

func (in *ExoscaleNodeClass) StatusConditions() status.ConditionSet {
	return status.NewReadyConditions().For(in)
}

func (in *ExoscaleNodeClass) GetConditions() []status.Condition {
	return in.Status.Conditions
}

func (in *ExoscaleNodeClass) SetConditions(conditions []status.Condition) {
	in.Status.Conditions = conditions
}

// ExoscaleNodeClassStatus defines the observed state of ExoscaleNodeClass
type ExoscaleNodeClassStatus struct {
	// Conditions contains signals for health and readiness
	Conditions []status.Condition `json:"conditions,omitempty"`
}

// ExoscaleNodeClassList contains a list of ExoscaleNodeClass
// +kubebuilder:object:root=true
type ExoscaleNodeClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExoscaleNodeClass `json:"items"`
}
