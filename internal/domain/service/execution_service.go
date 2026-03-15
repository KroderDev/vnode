package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/kroderdev/vnode/internal/adapter/outbound/virtualkubelet"
	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/ports"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type PodExecutionService struct {
	nodeRepo   ports.NodeRepository
	hostPods   ports.ClusterClient
	translator ports.PodTranslator
	tenants    *virtualkubelet.TenantClientManager
}

type PodExecutionResult struct {
	SourcePods      int
	CreatedHostPods int
	DeletedHostPods int
	SyncedStatuses  int
}

func NewPodExecutionService(nodeRepo ports.NodeRepository, hostPods ports.ClusterClient, translator ports.PodTranslator, tenants *virtualkubelet.TenantClientManager) *PodExecutionService {
	return &PodExecutionService{
		nodeRepo:   nodeRepo,
		hostPods:   hostPods,
		translator: translator,
		tenants:    tenants,
	}
}

func (s *PodExecutionService) HostClient() ports.ClusterClient {
	return s.hostPods
}

func (s *PodExecutionService) ReconcilePool(ctx context.Context, pool model.VNodePool) (PodExecutionResult, error) {
	result := PodExecutionResult{}

	clientset, err := s.tenants.Get(ctx, pool.TenantRef)
	if err != nil {
		if isIgnorableShutdownError(err) {
			return result, nil
		}
		return result, err
	}

	nodes, err := s.nodeRepo.ListByPool(ctx, pool.Namespace, pool.Name)
	if err != nil {
		if isIgnorableShutdownError(err) {
			return result, nil
		}
		return result, fmt.Errorf("listing pool nodes: %w", err)
	}

	ready := map[string]model.VNode{}
	for _, node := range nodes {
		if node.IsReady() {
			ready[node.Name] = node
		}
	}

	sourcePods, err := listTenantPodsForNodes(ctx, clientset, ready)
	if err != nil {
		if isIgnorableShutdownError(err) {
			return result, nil
		}
		return result, err
	}
	result.SourcePods = len(sourcePods)

	desired := make(map[string]struct{}, len(sourcePods))
	for _, sourcePod := range sourcePods {
		translation, err := s.translator.Translate(ctx, podFromCore(sourcePod), pool, sourcePod.Spec.NodeName)
		if err != nil {
			return result, fmt.Errorf("translating pod %s/%s: %w", sourcePod.Namespace, sourcePod.Name, err)
		}

		desired[keyForPod(translation.TargetPod.Namespace, translation.TargetPod.Name)] = struct{}{}

		// Sync ConfigMaps and Secrets referenced by the pod from tenant to host
		if err := syncPodResources(ctx, clientset, s.hostPods, translation, pool); err != nil {
			if isIgnorableShutdownError(err) {
				return result, nil
			}
			return result, fmt.Errorf("syncing resources for pod %s/%s: %w", sourcePod.Namespace, sourcePod.Name, err)
		}

		if !isTerminalPod(sourcePod) {
			created, deleted, err := ensureHostPod(ctx, s.hostPods, translation.TargetPod)
			if err != nil {
				if errors.Is(err, errPodTerminating) {
					// Host pod is being deleted; skip this pod and let the
					// next reconcile handle it once deletion completes.
					if deleted {
						result.DeletedHostPods++
					}
					continue
				}
				if isIgnorableShutdownError(err) {
					return result, nil
				}
				return result, fmt.Errorf("ensuring host pod for %s/%s: %w", sourcePod.Namespace, sourcePod.Name, err)
			}
			if created {
				result.CreatedHostPods++
			}
			if deleted {
				result.DeletedHostPods++
			}
		}

		hostStatus, err := s.hostPods.GetPodStatus(ctx, translation.TargetPod.Namespace, translation.TargetPod.Name)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				if isIgnorableShutdownError(err) {
					return result, nil
				}
				return result, fmt.Errorf("getting host pod status for %s/%s: %w", translation.TargetPod.Namespace, translation.TargetPod.Name, err)
			}
		} else {
			synced, err := s.translator.SyncStatus(ctx, *hostStatus)
			if err != nil {
				if isIgnorableShutdownError(err) {
					return result, nil
				}
				return result, fmt.Errorf("syncing host status for %s/%s: %w", sourcePod.Namespace, sourcePod.Name, err)
			}
			if err := updateTenantPodStatus(ctx, clientset, sourcePod.Namespace, sourcePod.Name, synced); err != nil {
				if isIgnorableShutdownError(err) {
					return result, nil
				}
				return result, fmt.Errorf("updating tenant pod status for %s/%s: %w", sourcePod.Namespace, sourcePod.Name, err)
			}
			result.SyncedStatuses++
		}

		if isTerminalPod(sourcePod) {
			if err := s.hostPods.DeletePod(ctx, translation.TargetPod.Namespace, translation.TargetPod.Name); err != nil {
				if isIgnorableShutdownError(err) {
					return result, nil
				}
				return result, fmt.Errorf("deleting host pod for terminal source pod %s/%s: %w", sourcePod.Namespace, sourcePod.Name, err)
			}
			result.DeletedHostPods++
		}
	}

	hostPods, err := s.hostPods.ListPodsByLabels(ctx, pool.Namespace, map[string]string{
		model.LabelManagedBy: model.LabelManagedByValue,
		model.LabelVNodePool: pool.Name,
	})
	if err != nil {
		if isIgnorableShutdownError(err) {
			return result, nil
		}
		return result, fmt.Errorf("listing host pods for cleanup: %w", err)
	}
	for _, hostPod := range hostPods {
		if _, ok := desired[keyForPod(hostPod.Namespace, hostPod.Name)]; ok {
			continue
		}
		if err := s.hostPods.DeletePod(ctx, hostPod.Namespace, hostPod.Name); err != nil {
			if isIgnorableShutdownError(err) {
				return result, nil
			}
			return result, fmt.Errorf("deleting orphaned host pod %s/%s: %w", hostPod.Namespace, hostPod.Name, err)
		}
		result.DeletedHostPods++
	}

	return result, nil
}

func CleanupPoolPods(ctx context.Context, host ports.ClusterClient, pool model.VNodePool) (int, error) {
	pods, err := host.ListPodsByLabels(ctx, pool.Namespace, map[string]string{
		model.LabelManagedBy: model.LabelManagedByValue,
		model.LabelVNodePool: pool.Name,
	})
	if err != nil {
		if isIgnorableShutdownError(err) {
			return 0, nil
		}
		return 0, err
	}
	deleted := 0
	for _, pod := range pods {
		if err := host.DeletePod(ctx, pod.Namespace, pod.Name); err != nil {
			if isIgnorableShutdownError(err) {
				return deleted, nil
			}
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func listTenantPodsForNodes(ctx context.Context, clientset kubernetes.Interface, ready map[string]model.VNode) ([]corev1.Pod, error) {
	if len(ready) == 0 {
		return nil, nil
	}
	list, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing tenant pods: %w", err)
	}
	result := make([]corev1.Pod, 0)
	for i := range list.Items {
		pod := list.Items[i]
		if pod.Labels[model.LabelManagedBy] == model.LabelManagedByValue {
			continue
		}
		if !pod.DeletionTimestamp.IsZero() {
			continue
		}
		if _, ok := ready[pod.Spec.NodeName]; !ok {
			continue
		}
		result = append(result, pod)
	}
	return result, nil
}

// errPodTerminating is returned when the host pod exists but is being deleted.
// The reconciler should requeue and wait for deletion to complete.
var errPodTerminating = fmt.Errorf("host pod is terminating, waiting for deletion to complete")

func ensureHostPod(ctx context.Context, host ports.ClusterClient, pod model.PodSpec) (bool, bool, error) {
	current, err := host.GetPod(ctx, pod.Namespace, pod.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return true, false, host.CreatePod(ctx, pod)
		}
		return false, false, err
	}
	// If the existing pod is being deleted, wait for it to be fully removed
	// before attempting to create a replacement.
	if current.Deleting {
		return false, false, errPodTerminating
	}
	if podSpecsEqual(*current, pod) {
		return false, false, nil
	}
	if err := host.DeletePod(ctx, pod.Namespace, pod.Name); err != nil {
		return false, false, err
	}
	// After requesting deletion, the pod enters Terminating state.
	// Return errPodTerminating so the reconciler requeues and waits
	// for the pod to be fully removed before creating the replacement.
	return false, true, errPodTerminating
}

func updateTenantPodStatus(ctx context.Context, clientset kubernetes.Interface, namespace, name string, status model.PodStatus) error {
	current, err := clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	current.Status.Phase = corev1.PodPhase(status.Phase)
	current.Status.PodIP = status.PodIP
	current.Status.Message = status.Message
	current.Status.Reason = status.Reason
	current.Status.ContainerStatuses = make([]corev1.ContainerStatus, 0, len(status.ContainerStatuses))
	for _, cs := range status.ContainerStatuses {
		containerStatus := corev1.ContainerStatus{
			Name:         cs.Name,
			Ready:        cs.Ready,
			RestartCount: cs.RestartCount,
		}
		switch cs.State {
		case "running":
			containerStatus.State.Running = &corev1.ContainerStateRunning{}
		case "waiting":
			containerStatus.State.Waiting = &corev1.ContainerStateWaiting{}
		case "terminated":
			containerStatus.State.Terminated = &corev1.ContainerStateTerminated{}
		}
		current.Status.ContainerStatuses = append(current.Status.ContainerStatuses, containerStatus)
	}
	_, err = clientset.CoreV1().Pods(namespace).UpdateStatus(ctx, current, metav1.UpdateOptions{})
	return err
}

func podFromCore(pod corev1.Pod) model.PodSpec {
	containers := make([]model.Container, 0, len(pod.Spec.Containers))
	for _, container := range pod.Spec.Containers {
		containers = append(containers, model.Container{
			Name:         container.Name,
			Image:        container.Image,
			Command:      container.Command,
			Args:         container.Args,
			Env:          envVarsFromCore(container.Env),
			VolumeMounts: volumeMountsFromCore(container.VolumeMounts),
			Resources: model.ContainerResources{
				Requests: resourceListFromCore(container.Resources.Requests),
				Limits:   resourceListFromCore(container.Resources.Limits),
			},
		})
	}
	volumes := make([]model.Volume, 0, len(pod.Spec.Volumes))
	for _, volume := range pod.Spec.Volumes {
		volumes = append(volumes, volumeFromCore(volume))
	}
	return model.PodSpec{
		Name:               pod.Name,
		Namespace:          pod.Namespace,
		Labels:             pod.Labels,
		NodeName:           pod.Spec.NodeName,
		ServiceAccountName: pod.Spec.ServiceAccountName,
		Containers:         containers,
		Volumes:            volumes,
	}
}

func keyForPod(namespace, name string) string {
	return namespace + "/" + name
}

func isTerminalPod(pod corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
}

func podSpecsEqual(a, b model.PodSpec) bool {
	return a.Name == b.Name &&
		a.Namespace == b.Namespace &&
		normalizeServiceAccount(a.ServiceAccountName) == normalizeServiceAccount(b.ServiceAccountName) &&
		boolPtrEqual(a.AutomountServiceAccountToken, b.AutomountServiceAccountToken) &&
		a.RuntimeClassName == b.RuntimeClassName &&
		stringMapEqual(a.Labels, b.Labels) &&
		stringMapEqual(a.NodeSelector, b.NodeSelector) &&
		containersEqual(a.Containers, b.Containers) &&
		volumesEqual(a.Volumes, b.Volumes)
}

func boolPtrEqual(a, b *bool) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func normalizeServiceAccount(name string) string {
	if name == "" {
		return "default"
	}
	return name
}

func stringMapEqual(a, b map[string]string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func containersEqual(a, b []model.Container) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Image != b[i].Image {
			return false
		}
		if !stringSliceEqual(a[i].Command, b[i].Command) || !stringSliceEqual(a[i].Args, b[i].Args) {
			return false
		}
		if !envVarsEqual(a[i].Env, b[i].Env) || !volumeMountsEqual(a[i].VolumeMounts, b[i].VolumeMounts) {
			return false
		}
		if a[i].Resources != b[i].Resources {
			return false
		}
	}
	return true
}

func volumesEqual(a, b []model.Volume) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func envVarsEqual(a, b []model.EnvVar) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func volumeMountsEqual(a, b []model.VolumeMount) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func envVarsFromCore(in []corev1.EnvVar) []model.EnvVar {
	out := make([]model.EnvVar, 0, len(in))
	for _, env := range in {
		out = append(out, model.EnvVar{Name: env.Name, Value: env.Value})
	}
	return out
}

func volumeMountsFromCore(in []corev1.VolumeMount) []model.VolumeMount {
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

func resourceListFromCore(in corev1.ResourceList) model.ResourceList {
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

func volumeFromCore(in corev1.Volume) model.Volume {
	out := model.Volume{Name: in.Name, Type: model.VolumeTypeOther}
	switch {
	case in.ConfigMap != nil:
		out.Type = model.VolumeTypeConfigMap
		out.Source = in.ConfigMap.Name
	case in.Secret != nil:
		out.Type = model.VolumeTypeSecret
		out.Source = in.Secret.SecretName
	case in.PersistentVolumeClaim != nil:
		out.Type = model.VolumeTypePVC
		out.Source = in.PersistentVolumeClaim.ClaimName
	case in.EmptyDir != nil:
		out.Type = model.VolumeTypeEmptyDir
	case in.HostPath != nil:
		out.Type = model.VolumeTypeHostPath
		out.Source = in.HostPath.Path
	case in.Projected != nil:
		out.Type = model.VolumeTypeProjected
	}
	return out
}

func syncPodResources(ctx context.Context, clientset kubernetes.Interface, host ports.ClusterClient, translation model.PodTranslation, pool model.VNodePool) error {
	refs := translation.ResourceRefs()
	if len(refs) == 0 {
		return nil
	}

	labels := map[string]string{
		model.LabelManagedBy: model.LabelManagedByValue,
		model.LabelVNodePool: pool.Name,
	}

	for _, ref := range refs {
		switch ref.Type {
		case model.VolumeTypeConfigMap:
			cm, err := clientset.CoreV1().ConfigMaps(ref.SourceNamespace).Get(ctx, ref.SourceName, metav1.GetOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					continue // ConfigMap doesn't exist in tenant; skip
				}
				return fmt.Errorf("getting tenant ConfigMap %s/%s: %w", ref.SourceNamespace, ref.SourceName, err)
			}
			if err := host.EnsureConfigMap(ctx, pool.Namespace, ref.TargetName, cm.Data, cm.BinaryData, labels); err != nil {
				return fmt.Errorf("ensuring host ConfigMap %s/%s: %w", pool.Namespace, ref.TargetName, err)
			}
		case model.VolumeTypeSecret:
			secret, err := clientset.CoreV1().Secrets(ref.SourceNamespace).Get(ctx, ref.SourceName, metav1.GetOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}
				return fmt.Errorf("getting tenant Secret %s/%s: %w", ref.SourceNamespace, ref.SourceName, err)
			}
			if err := host.EnsureSecret(ctx, pool.Namespace, ref.TargetName, secret.Data, labels); err != nil {
				return fmt.Errorf("ensuring host Secret %s/%s: %w", pool.Namespace, ref.TargetName, err)
			}
		}
	}
	return nil
}

func isIgnorableShutdownError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
