package model

import "fmt"

// PodSpec is a domain representation of a pod, decoupled from K8s types.
type PodSpec struct {
	Name      string
	Namespace string
	Labels    map[string]string
	NodeName  string
	Containers []Container
	Volumes    []Volume

	// Fields to strip/override during translation
	ServiceAccountName string
	RuntimeClassName   string
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
	Name     string
	Type     VolumeType
	Source   string
}

// VolumeType identifies the kind of volume.
type VolumeType string

const (
	VolumeTypeProjected   VolumeType = "projected"
	VolumeTypeConfigMap   VolumeType = "configMap"
	VolumeTypeSecret      VolumeType = "secret"
	VolumeTypeEmptyDir    VolumeType = "emptyDir"
	VolumeTypePVC         VolumeType = "pvc"
	VolumeTypeHostPath    VolumeType = "hostPath"
	VolumeTypeOther       VolumeType = "other"
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
	LabelManagedBy        = "app.kubernetes.io/managed-by"
	LabelManagedByValue   = "kroderdev-vnode"
	LabelVNodePool        = "vnode.kroderdev.io/pool"
	LabelVNodeName        = "vnode.kroderdev.io/node-name"
	LabelSourcePodName    = "vnode.kroderdev.io/source-pod-name"
	LabelSourcePodNS      = "vnode.kroderdev.io/source-pod-namespace"
)

// TranslatePod converts a vcluster pod spec into a host cluster pod spec.
// It strips vcluster-injected fields and applies the isolation RuntimeClass.
func TranslatePod(source PodSpec, vnodeName, poolName, targetNamespace, runtimeClass string) PodTranslation {
	target := PodSpec{
		Name:             fmt.Sprintf("%s-%s-%s", vnodeName, source.Namespace, source.Name),
		Namespace:        targetNamespace,
		RuntimeClassName: runtimeClass,
		Labels: map[string]string{
			LabelManagedBy:     LabelManagedByValue,
			LabelVNodePool:     poolName,
			LabelVNodeName:     vnodeName,
			LabelSourcePodName: source.Name,
			LabelSourcePodNS:   source.Namespace,
		},
		Containers: translateContainers(source.Containers),
		Volumes:    filterVolumes(source.Volumes),
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
func translateContainers(containers []Container) []Container {
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
			translated.VolumeMounts = append(translated.VolumeMounts, vm)
		}
		result = append(result, translated)
	}
	return result
}

// filterVolumes removes projected SA token volumes injected by vcluster.
func filterVolumes(volumes []Volume) []Volume {
	result := make([]Volume, 0, len(volumes))
	for _, v := range volumes {
		if v.Type == VolumeTypeProjected {
			continue
		}
		result = append(result, v)
	}
	return result
}

func isServiceAccountMount(vm VolumeMount) bool {
	return vm.MountPath == "/var/run/secrets/kubernetes.io/serviceaccount"
}
