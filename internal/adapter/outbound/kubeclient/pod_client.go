package kubeclient

import (
	"context"

	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/ports"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ ports.ClusterClient = (*PodClusterClient)(nil)

type PodClusterClient struct {
	client    client.Client
	clientset kubernetes.Interface
}

func NewPodClusterClient(c client.Client, cs kubernetes.Interface) *PodClusterClient {
	return &PodClusterClient{client: c, clientset: cs}
}

func (c *PodClusterClient) CreatePod(ctx context.Context, pod model.PodSpec) error {
	return c.client.Create(ctx, podSpecToK8sPod(pod))
}

func (c *PodClusterClient) UpdatePod(ctx context.Context, pod model.PodSpec) error {
	var current corev1.Pod
	if err := c.client.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, &current); err != nil {
		return err
	}
	desired := podSpecToK8sPod(pod)
	current.Labels = desired.Labels
	current.Spec.ServiceAccountName = desired.Spec.ServiceAccountName
	current.Spec.RuntimeClassName = desired.Spec.RuntimeClassName
	current.Spec.NodeSelector = desired.Spec.NodeSelector
	current.Spec.Containers = desired.Spec.Containers
	current.Spec.Volumes = desired.Spec.Volumes
	return c.client.Update(ctx, &current)
}

func (c *PodClusterClient) DeletePod(ctx context.Context, namespace, name string) error {
	err := c.client.Delete(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (c *PodClusterClient) GetPod(ctx context.Context, namespace, name string) (*model.PodSpec, error) {
	var pod corev1.Pod
	if err := c.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &pod); err != nil {
		return nil, err
	}
	spec := podToSpec(&pod)
	return &spec, nil
}

func (c *PodClusterClient) GetPodStatus(ctx context.Context, namespace, name string) (*model.PodStatus, error) {
	var pod corev1.Pod
	if err := c.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &pod); err != nil {
		return nil, err
	}
	status := podToStatus(&pod)
	return &status, nil
}

func (c *PodClusterClient) ListPodsByLabels(ctx context.Context, namespace string, labels map[string]string) ([]model.PodSpec, error) {
	var list corev1.PodList
	opts := []client.ListOption{client.MatchingLabels(labels)}
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}
	if err := c.client.List(ctx, &list, opts...); err != nil {
		return nil, err
	}
	result := make([]model.PodSpec, 0, len(list.Items))
	for i := range list.Items {
		result = append(result, podToSpec(&list.Items[i]))
	}
	return result, nil
}

func (c *PodClusterClient) EnsureConfigMap(ctx context.Context, namespace, name string, data map[string]string, binaryData map[string][]byte, labels map[string]string) error {
	cms := c.clientset.CoreV1().ConfigMaps(namespace)
	existing, err := cms.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		_, err = cms.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    labels,
			},
			Data:       data,
			BinaryData: binaryData,
		}, metav1.CreateOptions{})
		return err
	}
	existing.Data = data
	existing.BinaryData = binaryData
	existing.Labels = labels
	_, err = cms.Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (c *PodClusterClient) EnsureSecret(ctx context.Context, namespace, name string, data map[string][]byte, labels map[string]string) error {
	secrets := c.clientset.CoreV1().Secrets(namespace)
	existing, err := secrets.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		_, err = secrets.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    labels,
			},
			Data: data,
		}, metav1.CreateOptions{})
		return err
	}
	existing.Data = data
	existing.Labels = labels
	_, err = secrets.Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func podSpecToK8sPod(pod model.PodSpec) *corev1.Pod {
	k8sPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Labels:    pod.Labels,
		},
		Spec: corev1.PodSpec{
			NodeName:                     pod.NodeName,
			ServiceAccountName:           pod.ServiceAccountName,
			AutomountServiceAccountToken: pod.AutomountServiceAccountToken,
			RuntimeClassName:             stringPtrOrNil(pod.RuntimeClassName),
			NodeSelector:                 pod.NodeSelector,
			Containers:         make([]corev1.Container, 0, len(pod.Containers)),
			Volumes:            make([]corev1.Volume, 0, len(pod.Volumes)),
		},
	}
	for _, container := range pod.Containers {
		k8sPod.Spec.Containers = append(k8sPod.Spec.Containers, corev1.Container{
			Name:         container.Name,
			Image:        container.Image,
			Command:      container.Command,
			Args:         container.Args,
			Env:          envToK8s(container.Env),
			VolumeMounts: volumeMountsToK8s(container.VolumeMounts),
			Resources: corev1.ResourceRequirements{
				Requests: resourceListToK8s(container.Resources.Requests),
				Limits:   resourceListToK8s(container.Resources.Limits),
			},
		})
	}
	for _, volume := range pod.Volumes {
		k8sPod.Spec.Volumes = append(k8sPod.Spec.Volumes, volumeToK8s(volume))
	}
	return k8sPod
}

func podToSpec(pod *corev1.Pod) model.PodSpec {
	spec := model.PodSpec{
		Name:                         pod.Name,
		Namespace:                    pod.Namespace,
		Labels:                       pod.Labels,
		NodeName:                     pod.Spec.NodeName,
		ServiceAccountName:           pod.Spec.ServiceAccountName,
		AutomountServiceAccountToken: pod.Spec.AutomountServiceAccountToken,
		NodeSelector:                 pod.Spec.NodeSelector,
		Deleting:                     !pod.DeletionTimestamp.IsZero(),
		Containers:         make([]model.Container, 0, len(pod.Spec.Containers)),
		Volumes:            make([]model.Volume, 0, len(pod.Spec.Volumes)),
	}
	if pod.Spec.RuntimeClassName != nil {
		spec.RuntimeClassName = *pod.Spec.RuntimeClassName
	}
	for _, container := range pod.Spec.Containers {
		spec.Containers = append(spec.Containers, model.Container{
			Name:         container.Name,
			Image:        container.Image,
			Command:      container.Command,
			Args:         container.Args,
			Env:          envFromK8s(container.Env),
			VolumeMounts: volumeMountsFromK8s(container.VolumeMounts),
			Resources: model.ContainerResources{
				Requests: resourceListFromK8s(container.Resources.Requests),
				Limits:   resourceListFromK8s(container.Resources.Limits),
			},
		})
	}
	for _, volume := range pod.Spec.Volumes {
		spec.Volumes = append(spec.Volumes, volumeFromK8s(volume))
	}
	return spec
}

func podToStatus(pod *corev1.Pod) model.PodStatus {
	status := model.PodStatus{
		Phase:   string(pod.Status.Phase),
		PodIP:   pod.Status.PodIP,
		Message: pod.Status.Message,
		Reason:  pod.Status.Reason,
	}
	for _, containerStatus := range pod.Status.ContainerStatuses {
		state := ""
		switch {
		case containerStatus.State.Running != nil:
			state = "running"
		case containerStatus.State.Waiting != nil:
			state = "waiting"
		case containerStatus.State.Terminated != nil:
			state = "terminated"
		}
		status.ContainerStatuses = append(status.ContainerStatuses, model.ContainerStatus{
			Name:         containerStatus.Name,
			Ready:        containerStatus.Ready,
			RestartCount: containerStatus.RestartCount,
			State:        state,
		})
	}
	return status
}

func envToK8s(env []model.EnvVar) []corev1.EnvVar {
	out := make([]corev1.EnvVar, 0, len(env))
	for _, item := range env {
		out = append(out, corev1.EnvVar{Name: item.Name, Value: item.Value})
	}
	return out
}

func envFromK8s(env []corev1.EnvVar) []model.EnvVar {
	out := make([]model.EnvVar, 0, len(env))
	for _, item := range env {
		out = append(out, model.EnvVar{Name: item.Name, Value: item.Value})
	}
	return out
}

func volumeMountsToK8s(in []model.VolumeMount) []corev1.VolumeMount {
	out := make([]corev1.VolumeMount, 0, len(in))
	for _, mount := range in {
		out = append(out, corev1.VolumeMount{
			Name:      mount.Name,
			MountPath: mount.MountPath,
			ReadOnly:  mount.ReadOnly,
		})
	}
	return out
}

func volumeMountsFromK8s(in []corev1.VolumeMount) []model.VolumeMount {
	out := make([]model.VolumeMount, 0, len(in))
	for _, mount := range in {
		out = append(out, model.VolumeMount{
			Name:      mount.Name,
			MountPath: mount.MountPath,
			ReadOnly:  mount.ReadOnly,
		})
	}
	return out
}

func stringPtrOrNil(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func resourceListToK8s(in model.ResourceList) corev1.ResourceList {
	out := corev1.ResourceList{}
	if in.CPU != "" {
		out[corev1.ResourceCPU] = resource.MustParse(in.CPU)
	}
	if in.Memory != "" {
		out[corev1.ResourceMemory] = resource.MustParse(in.Memory)
	}
	return out
}

func resourceListFromK8s(in corev1.ResourceList) model.ResourceList {
	out := model.ResourceList{}
	if cpu, ok := in[corev1.ResourceCPU]; ok {
		out.CPU = cpu.String()
	}
	if memory, ok := in[corev1.ResourceMemory]; ok {
		out.Memory = memory.String()
	}
	if pods, ok := in[corev1.ResourcePods]; ok {
		out.Pods = int32(pods.Value())
	}
	return out
}

func volumeToK8s(volume model.Volume) corev1.Volume {
	out := corev1.Volume{Name: volume.Name}
	switch volume.Type {
	case model.VolumeTypeConfigMap:
		out.ConfigMap = &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: volume.Source}}
	case model.VolumeTypeSecret:
		out.Secret = &corev1.SecretVolumeSource{SecretName: volume.Source}
	case model.VolumeTypePVC:
		out.PersistentVolumeClaim = &corev1.PersistentVolumeClaimVolumeSource{ClaimName: volume.Source}
	case model.VolumeTypeEmptyDir:
		out.EmptyDir = &corev1.EmptyDirVolumeSource{}
	case model.VolumeTypeHostPath:
		out.HostPath = &corev1.HostPathVolumeSource{Path: volume.Source}
	case model.VolumeTypeProjected:
		out.Projected = &corev1.ProjectedVolumeSource{}
	}
	return out
}

func volumeFromK8s(volume corev1.Volume) model.Volume {
	out := model.Volume{Name: volume.Name, Type: model.VolumeTypeOther}
	switch {
	case volume.ConfigMap != nil:
		out.Type = model.VolumeTypeConfigMap
		out.Source = volume.ConfigMap.Name
	case volume.Secret != nil:
		out.Type = model.VolumeTypeSecret
		out.Source = volume.Secret.SecretName
	case volume.PersistentVolumeClaim != nil:
		out.Type = model.VolumeTypePVC
		out.Source = volume.PersistentVolumeClaim.ClaimName
	case volume.EmptyDir != nil:
		out.Type = model.VolumeTypeEmptyDir
	case volume.HostPath != nil:
		out.Type = model.VolumeTypeHostPath
		out.Source = volume.HostPath.Path
	case volume.Projected != nil:
		out.Type = model.VolumeTypeProjected
	}
	return out
}
