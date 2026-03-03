package translator

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// LabelVNodeName identifies which vnode owns this pod.
	LabelVNodeName = "vnode.kroderdev.io/node-name"
	// LabelVClusterPodName stores the original pod name from the vcluster.
	LabelVClusterPodName = "vnode.kroderdev.io/vcluster-pod-name"
	// LabelVClusterPodNamespace stores the original namespace from the vcluster.
	LabelVClusterPodNamespace = "vnode.kroderdev.io/vcluster-pod-namespace"
	// LabelManagedBy marks pods managed by vnode.
	LabelManagedBy = "app.kubernetes.io/managed-by"
	// ManagedByValue is the manager identifier.
	ManagedByValue = "kroderdev-vnode"
)

// Translator converts vcluster pods into host cluster pods.
type Translator struct {
	hostNamespace string
	runtimeClass  string
	nodeName      string
}

// New creates a Translator.
func New(hostNamespace, runtimeClass, nodeName string) *Translator {
	return &Translator{
		hostNamespace: hostNamespace,
		runtimeClass:  runtimeClass,
		nodeName:      nodeName,
	}
}

// Translate converts a vcluster pod spec into a host cluster pod.
// The host pod runs in the configured namespace with the specified RuntimeClass.
func (t *Translator) Translate(pod *corev1.Pod) *corev1.Pod {
	hostPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      t.hostPodName(pod),
			Namespace: t.hostNamespace,
			Labels: map[string]string{
				LabelManagedBy:            ManagedByValue,
				LabelVNodeName:            t.nodeName,
				LabelVClusterPodName:      pod.Name,
				LabelVClusterPodNamespace: pod.Namespace,
			},
			Annotations: make(map[string]string),
		},
		Spec: *pod.Spec.DeepCopy(),
	}

	hostPod.Spec.RuntimeClassName = &t.runtimeClass
	hostPod.Spec.NodeName = ""
	hostPod.Spec.NodeSelector = nil
	hostPod.Spec.Affinity = nil
	hostPod.Spec.Tolerations = nil
	hostPod.Spec.ServiceAccountName = ""
	hostPod.Spec.AutomountServiceAccountToken = boolPtr(false)

	// Clear volume mounts that reference vcluster service accounts / secrets
	for i := range hostPod.Spec.Containers {
		hostPod.Spec.Containers[i].VolumeMounts = filterVolumeMounts(hostPod.Spec.Containers[i].VolumeMounts)
	}
	for i := range hostPod.Spec.InitContainers {
		hostPod.Spec.InitContainers[i].VolumeMounts = filterVolumeMounts(hostPod.Spec.InitContainers[i].VolumeMounts)
	}
	hostPod.Spec.Volumes = filterVolumes(hostPod.Spec.Volumes)

	return hostPod
}

func (t *Translator) hostPodName(pod *corev1.Pod) string {
	return fmt.Sprintf("%s-%s-%s", t.nodeName, pod.Namespace, pod.Name)
}

// MatchLabels returns label selector to find pods owned by this vnode.
func (t *Translator) MatchLabels() map[string]string {
	return map[string]string{
		LabelManagedBy: ManagedByValue,
		LabelVNodeName: t.nodeName,
	}
}

// HostPodName returns the expected host pod name for a vcluster pod.
func (t *Translator) HostPodName(namespace, name string) string {
	return fmt.Sprintf("%s-%s-%s", t.nodeName, namespace, name)
}

func boolPtr(b bool) *bool { return &b }

func filterVolumeMounts(mounts []corev1.VolumeMount) []corev1.VolumeMount {
	var filtered []corev1.VolumeMount
	for _, m := range mounts {
		if m.MountPath == "/var/run/secrets/kubernetes.io/serviceaccount" {
			continue
		}
		filtered = append(filtered, m)
	}
	return filtered
}

func filterVolumes(volumes []corev1.Volume) []corev1.Volume {
	var filtered []corev1.Volume
	for _, v := range volumes {
		if v.Projected != nil || (v.Secret != nil && v.Name == "kube-api-access") {
			continue
		}
		filtered = append(filtered, v)
	}
	return filtered
}
