package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VNodeSpec defines the desired state of a VNode.
type VNodeSpec struct {
	// PoolRef is the name of the parent VNodePool.
	PoolRef string `json:"poolRef"`

	// Capacity defines the resources this virtual node advertises.
	Capacity NodeResources `json:"capacity"`
}

// VNodeStatus defines the observed state of a VNode.
type VNodeStatus struct {
	// Phase is the current lifecycle phase.
	// +kubebuilder:validation:Enum=Pending;Ready;NotReady;Terminating
	Phase string `json:"phase,omitempty"`

	// Conditions represent the latest observations of the node's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Pool",type=string,JSONPath=`.spec.poolRef`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="CPU",type=string,JSONPath=`.spec.capacity.cpu`
// +kubebuilder:printcolumn:name="Memory",type=string,JSONPath=`.spec.capacity.memory`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// VNode is the Schema for the vnodes API.
type VNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VNodeSpec   `json:"spec,omitempty"`
	Status VNodeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VNodeList contains a list of VNode.
type VNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VNode `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VNode{}, &VNodeList{})
}
