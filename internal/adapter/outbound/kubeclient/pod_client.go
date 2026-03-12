package kubeclient

import (
	"context"

	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/ports"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ ports.ClusterClient = (*PodClusterClient)(nil)

type PodClusterClient struct {
	client client.Client
}

func NewPodClusterClient(c client.Client) *PodClusterClient {
	return &PodClusterClient{client: c}
}

func (c *PodClusterClient) CreatePod(ctx context.Context, pod model.PodSpec) error {
	return c.client.Create(ctx, podSpecToK8sPod(pod))
}

func (c *PodClusterClient) UpdatePod(ctx context.Context, pod model.PodSpec) error {
	var current corev1.Pod
	if err := c.client.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, &current); err != nil {
		return err
	}
	current.Labels = pod.Labels
	current.Spec.RuntimeClassName = stringPtrOrNil(pod.RuntimeClassName)
	current.Spec.NodeSelector = pod.NodeSelector
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

func podSpecToK8sPod(pod model.PodSpec) *corev1.Pod {
	k8sPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Labels:    pod.Labels,
		},
		Spec: corev1.PodSpec{
			NodeName:           pod.NodeName,
			ServiceAccountName: pod.ServiceAccountName,
			RuntimeClassName:   stringPtrOrNil(pod.RuntimeClassName),
			NodeSelector:       pod.NodeSelector,
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
		})
	}
	for _, volume := range pod.Volumes {
		k8sPod.Spec.Volumes = append(k8sPod.Spec.Volumes, corev1.Volume{Name: volume.Name})
	}
	return k8sPod
}

func podToSpec(pod *corev1.Pod) model.PodSpec {
	spec := model.PodSpec{
		Name:               pod.Name,
		Namespace:          pod.Namespace,
		Labels:             pod.Labels,
		NodeName:           pod.Spec.NodeName,
		ServiceAccountName: pod.Spec.ServiceAccountName,
		NodeSelector:       pod.Spec.NodeSelector,
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
		})
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
