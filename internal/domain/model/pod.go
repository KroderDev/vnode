package model

import (
	"fmt"
	"strings"
)

// PodSpec is a domain representation of a pod, decoupled from K8s types.
type PodSpec struct {
	Name       string
	Namespace  string
	Labels     map[string]string
	NodeName   string
	Containers []Container
	Volumes    []Volume

	// Fields to strip/override during translation
	ServiceAccountName           string
	AutomountServiceAccountToken *bool
	RuntimeClassName             string
	NodeSelector                 map[string]string

	// Deleting is true when the pod has a non-zero DeletionTimestamp.
	Deleting bool
}

// Container is a minimal container spec.
type Container struct {
	Name         string
	Image        string
	Command      []string
	Args         []string
	Env          []EnvVar
	VolumeMounts []VolumeMount
	Resources    ContainerResources
}

// ContainerResources holds resource requests and limits for a container.
type ContainerResources struct {
	Requests ResourceList
	Limits   ResourceList
}

// EnvVar is a key-value environment variable.
type EnvVar struct {
	Name  string
	Value string
}

// Volume represents a volume in a pod.
type Volume struct {
	Name   string
	Type   VolumeType
	Source string
}

// VolumeType identifies the kind of volume.
type VolumeType string

const (
	VolumeTypeProjected VolumeType = "projected"
	VolumeTypeConfigMap VolumeType = "configMap"
	VolumeTypeSecret    VolumeType = "secret"
	VolumeTypeEmptyDir  VolumeType = "emptyDir"
	VolumeTypePVC       VolumeType = "pvc"
	VolumeTypeHostPath  VolumeType = "hostPath"
	VolumeTypeOther     VolumeType = "other"
)

// VolumeMount links a container to a volume.
type VolumeMount struct {
	Name      string
	MountPath string
	ReadOnly  bool
}

// PodStatus represents the status of a pod.
type PodStatus struct {
	Phase             string
	PodIP             string
	ContainerStatuses []ContainerStatus
	Message           string
	Reason            string
}

// ContainerStatus is the runtime status of a container.
type ContainerStatus struct {
	Name         string
	Ready        bool
	RestartCount int32
	State        string
}

// PodTranslation holds the result of translating a vcluster pod to a host pod.
type PodTranslation struct {
	SourcePod PodSpec
	TargetPod PodSpec
}

const (
	LabelManagedBy      = "app.kubernetes.io/managed-by"
	LabelManagedByValue = "kroderdev-vnode"
	LabelVNodePool      = "vnode.kroderdev.io/pool"
	LabelVNodeName      = "vnode.kroderdev.io/node-name"
	LabelSourcePodName  = "vnode.kroderdev.io/source-pod-name"
	LabelSourcePodNS    = "vnode.kroderdev.io/source-pod-namespace"
)

// TranslateOpts holds options for pod translation.
type TranslateOpts struct {
	VNodeName       string
	PoolName        string
	TargetNamespace string
	RuntimeClass    string
	NodeSelector    map[string]string // Applied for dedicated/burstable pool modes
}

// TranslatePod converts a vcluster pod spec into a host cluster pod spec.
// It strips vcluster-injected fields and applies the isolation RuntimeClass.
func TranslatePod(source PodSpec, opts TranslateOpts) PodTranslation {
	disableAutomount := false
	translatedVolumes, strippedVolumes := filterVolumes(source.Volumes, source.Namespace)
	target := PodSpec{
		Name:                         fmt.Sprintf("%s-%s-%s", opts.VNodeName, source.Namespace, source.Name),
		Namespace:                    opts.TargetNamespace,
		AutomountServiceAccountToken: &disableAutomount,
		RuntimeClassName:             opts.RuntimeClass,
		NodeSelector:                 opts.NodeSelector,
		Labels: map[string]string{
			LabelManagedBy:     LabelManagedByValue,
			LabelVNodePool:     opts.PoolName,
			LabelVNodeName:     opts.VNodeName,
			LabelSourcePodName: source.Name,
			LabelSourcePodNS:   source.Namespace,
		},
		Containers: translateContainers(source.Containers, strippedVolumes),
		Volumes:    translatedVolumes,
	}

	// Copy non-vnode labels from source
	for k, v := range source.Labels {
		if _, exists := target.Labels[k]; !exists {
			target.Labels[k] = v
		}
	}

	return PodTranslation{
		SourcePod: source,
		TargetPod: target,
	}
}

// translateContainers copies containers and strips SA token volume mounts.
func translateContainers(containers []Container, strippedVolumes map[string]struct{}) []Container {
	result := make([]Container, 0, len(containers))
	for _, c := range containers {
		translated := Container{
			Name:      c.Name,
			Image:     c.Image,
			Command:   c.Command,
			Args:      c.Args,
			Env:       c.Env,
			Resources: c.Resources,
		}
		// Strip service account token mounts
		for _, vm := range c.VolumeMounts {
			if isServiceAccountMount(vm) {
				continue
			}
			if _, stripped := strippedVolumes[vm.Name]; stripped {
				continue
			}
			translated.VolumeMounts = append(translated.VolumeMounts, vm)
		}
		result = append(result, translated)
	}
	return result
}

// filterVolumes removes projected SA token volumes and namespaces ConfigMap/Secret
// sources to avoid collisions between tenant namespaces in the host cluster.
func filterVolumes(volumes []Volume, sourceNamespace string) ([]Volume, map[string]struct{}) {
	result := make([]Volume, 0, len(volumes))
	stripped := make(map[string]struct{})
	for _, v := range volumes {
		if v.Type == VolumeTypeProjected || v.Type == VolumeTypeHostPath {
			stripped[v.Name] = struct{}{}
			continue
		}
		if v.Type == VolumeTypeConfigMap || v.Type == VolumeTypeSecret {
			v.Source = sourceNamespace + "-" + v.Source
		}
		result = append(result, v)
	}
	return result, stripped
}

// VolumeResourceRef identifies a resource (ConfigMap or Secret) that needs to
// be synced from the tenant cluster to the host namespace.
type VolumeResourceRef struct {
	Type            VolumeType // VolumeTypeConfigMap or VolumeTypeSecret
	SourceName      string     // Original name in the tenant cluster
	SourceNamespace string     // Namespace in the tenant cluster
	TargetName      string     // Namespaced name in the host cluster
}

// ResourceRefs returns the ConfigMap/Secret references that need to be synced.
func (t PodTranslation) ResourceRefs() []VolumeResourceRef {
	var refs []VolumeResourceRef
	sourceNS := t.SourcePod.Namespace
	for _, sv := range t.SourcePod.Volumes {
		if sv.Type != VolumeTypeConfigMap && sv.Type != VolumeTypeSecret {
			continue
		}
		refs = append(refs, VolumeResourceRef{
			Type:            sv.Type,
			SourceName:      sv.Source,
			SourceNamespace: sourceNS,
			TargetName:      sourceNS + "-" + sv.Source,
		})
	}
	return refs
}

func isServiceAccountMount(vm VolumeMount) bool {
	const serviceAccountPath = "/var/run/secrets/kubernetes.io/serviceaccount"

	trimmed := strings.TrimRight(vm.MountPath, "/")
	return trimmed == serviceAccountPath || strings.HasPrefix(trimmed, serviceAccountPath+"/")
}
