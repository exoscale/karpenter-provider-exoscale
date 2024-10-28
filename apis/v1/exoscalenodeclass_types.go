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

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
type ExoscaleNodeClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Status            ExoscaleNodeClassStatus `json:"status,omitempty"`
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

type ExoscaleNodeClassStatus struct {
	// Conditions contains signals for health and readiness
	Conditions []status.Condition `json:"conditions,omitempty"`
}

// idk why we should implement DeepCopyInto ourselves and framework don't succeed to generate it
// let's go
func (in *ExoscaleNodeClassStatus) DeepCopyInto(out *ExoscaleNodeClassStatus) {
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]status.Condition, len(*in))
		copy(*out, *in)
	}
}

// ExoscaleNodeClassList contains a list of ExoscaleNodeClass

// +kubebuilder:object:root=true
type ExoscaleNodeClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExoscaleNodeClass `json:"items"`
}
