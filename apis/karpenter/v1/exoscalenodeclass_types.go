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

// ExoscaleNodeClassSpec defines the desired state of ExoscaleNodeClass
// +kubebuilder:validation:XValidation:rule="!has(self.imageGCHighThresholdPercent) || !has(self.imageGCLowThresholdPercent) || self.imageGCLowThresholdPercent < self.imageGCHighThresholdPercent",message="imageGCLowThresholdPercent must be less than imageGCHighThresholdPercent"
type ExoscaleNodeClassSpec struct {
	// TemplateID is the ID of the template to use for instances
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern="^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"
	TemplateID string `json:"templateID"`

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
	// Example: "5m" for 5 minutes
	// +kubebuilder:default="5m"
	// +kubebuilder:validation:Pattern="^[0-9]+(s|m|h)$"
	// +optional
	ImageMinimumGCAge string `json:"imageMinimumGCAge,omitempty"`

	// KubeReserved is resources reserved for Kubernetes system components
	// +optional
	KubeReserved ResourceReservation `json:"kubeReserved,omitempty"`

	// SystemReserved is resources reserved for OS system components
	// +optional
	SystemReserved ResourceReservation `json:"systemReserved,omitempty"`

	// NodeLabels are labels to be applied to the node upon registration
	// +optional
	NodeLabels map[string]string `json:"nodeLabels,omitempty"`

	// NodeTaints are taints to be applied to the node upon registration
	// +optional
	// +kubebuilder:validation:MaxItems=50
	// +kubebuilder:validation:XValidation:rule="self.all(taint, has(taint.key) && taint.key != '')",message="taint key is required"
	NodeTaints []NodeTaint `json:"nodeTaints,omitempty"`
}

type ResourceReservation struct {
	// CPU reservation (e.g., "200m")
	// +optional
	// +kubebuilder:validation:Pattern="^(\\d+(m|[.])?(\\d+)?|\\d+m)$"
	CPU string `json:"cpu,omitempty"`

	// Memory reservation (e.g., "300Mi")
	// +optional
	// +kubebuilder:validation:Pattern="^(\\d+(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?)$"
	Memory string `json:"memory,omitempty"`

	// EphemeralStorage reservation (e.g., "1Gi")
	// +optional
	// +kubebuilder:validation:Pattern="^(\\d+(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?)$"
	EphemeralStorage string `json:"ephemeralStorage,omitempty"`
}

// NodeTaint represents a Kubernetes taint to be applied to a node
type NodeTaint struct {
	// Key is the taint key
	Key string `json:"key"`
	// Value is the taint value
	// +optional
	Value string `json:"value,omitempty"`
	// Effect is the taint effect (NoSchedule, PreferNoSchedule, NoExecute)
	// +kubebuilder:validation:Enum=NoSchedule;PreferNoSchedule;NoExecute
	Effect string `json:"effect"`
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
