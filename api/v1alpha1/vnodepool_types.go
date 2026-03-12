package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VNodePoolSpec defines the desired state of a VNodePool.
type VNodePoolSpec struct {
	// TenantRef references the target vcluster for this pool.
	TenantRef TenantRef `json:"tenantRef"`

	// NodeCount is the desired number of virtual nodes in the pool.
	// +kubebuilder:validation:Minimum=0
	NodeCount int32 `json:"nodeCount"`

	// PerNodeResources defines the capacity advertised by each virtual node.
	PerNodeResources NodeResources `json:"perNodeResources"`

	// Mode determines how pool resources are allocated on the host cluster.
	// +kubebuilder:validation:Enum=shared;dedicated;burstable
	// +kubebuilder:default=shared
	Mode string `json:"mode,omitempty"`

	// IsolationBackend specifies the RuntimeClass to use for pod isolation.
	// +kubebuilder:default=kata
	IsolationBackend string `json:"isolationBackend,omitempty"`

	// RuntimeClassName overrides the runtime class selected by isolationBackend when set.
	// +optional
	RuntimeClassName string `json:"runtimeClassName,omitempty"`

	// NodeSelector specifies host node labels for dedicated/burstable mode.
	// Required when mode is "dedicated".
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Taints applied to virtual nodes for tenant scheduling control.
	// +optional
	Taints []corev1.Taint `json:"taints,omitempty"`

	// Tolerations applied to translated host pods scheduled through this pool.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

// TenantRef identifies the target vcluster.
type TenantRef struct {
	// VClusterName is the name of the target vcluster.
	VClusterName string `json:"vclusterName"`

	// VClusterNamespace is the namespace of the target vcluster.
	VClusterNamespace string `json:"vclusterNamespace"`

	// KubeconfigSecret is the name of the Secret containing the vcluster kubeconfig.
	KubeconfigSecret string `json:"kubeconfigSecret"`
}

// NodeResources defines compute resources for a virtual node.
type NodeResources struct {
	// CPU capacity (e.g. "4", "500m").
	CPU string `json:"cpu"`

	// Memory capacity (e.g. "8Gi", "512Mi").
	Memory string `json:"memory"`

	// Pods is the maximum number of pods per node.
	// +kubebuilder:default=110
	Pods int32 `json:"pods,omitempty"`
}

// VNodePoolStatus defines the observed state of a VNodePool.
type VNodePoolStatus struct {
	// Phase is the current lifecycle phase of the pool.
	// +kubebuilder:validation:Enum=Pending;Ready;Scaling;Failed;Deleting
	Phase string `json:"phase,omitempty"`

	// ReadyNodes is the number of virtual nodes in Ready state.
	ReadyNodes int32 `json:"readyNodes,omitempty"`

	// TotalNodes is the total number of virtual nodes (in any state).
	TotalNodes int32 `json:"totalNodes,omitempty"`

	// Conditions represent the latest observations of the pool's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Nodes",type=integer,JSONPath=`.spec.nodeCount`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyNodes`
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.mode`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// VNodePool is the Schema for the vnodepools API.
type VNodePool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VNodePoolSpec   `json:"spec,omitempty"`
	Status VNodePoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VNodePoolList contains a list of VNodePool.
type VNodePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VNodePool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VNodePool{}, &VNodePoolList{})
}
