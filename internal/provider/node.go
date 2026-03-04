package provider

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuildNode constructs the virtual Node object that will be registered in the host cluster.
func BuildNode(nodeName string, cpu, memory resource.Quantity) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
			Labels: map[string]string{
				"type":                             "virtual",
				"kubernetes.io/role":               "agent",
				"node.kubernetes.io/instance-type":  "vnode",
				"beta.kubernetes.io/os":            "linux",
				"kubernetes.io/os":                 "linux",
				"kubernetes.io/arch":               "amd64",
				"vnode.kroderdev.io/managed":       "true",
			},
		},
		Spec: corev1.NodeSpec{},
		Status: corev1.NodeStatus{
			Phase: corev1.NodeRunning,
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue, Reason: "VNodeReady", Message: "vnode is ready"},
				{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse, Reason: "NoMemoryPressure"},
				{Type: corev1.NodeDiskPressure, Status: corev1.ConditionFalse, Reason: "NoDiskPressure"},
				{Type: corev1.NodePIDPressure, Status: corev1.ConditionFalse, Reason: "NoPIDPressure"},
				{Type: corev1.NodeNetworkUnavailable, Status: corev1.ConditionFalse, Reason: "NetworkReady"},
			},
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    cpu,
				corev1.ResourceMemory: memory,
				corev1.ResourcePods:   resource.MustParse("110"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    cpu,
				corev1.ResourceMemory: memory,
				corev1.ResourcePods:   resource.MustParse("110"),
			},
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
				{Type: corev1.NodeHostName, Address: nodeName},
			},
			NodeInfo: corev1.NodeSystemInfo{
				KubeletVersion:  "v1.31.0",
				OperatingSystem: "linux",
				Architecture:    "amd64",
			},
		},
	}
}

// Ping checks if the node is still active.
func (p *Provider) Ping(_ context.Context) error {
	return nil
}

// NotifyNodeStatus is called to set up a callback for node status updates.
func (p *Provider) NotifyNodeStatus(_ context.Context, _ func(*corev1.Node)) {
	// Static node, no status changes to notify about.
}
