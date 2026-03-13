package service

import (
	"context"
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
		return result, err
	}

	nodes, err := s.nodeRepo.ListByPool(ctx, pool.Namespace, pool.Name)
	if err != nil {
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

		if !isTerminalPod(sourcePod) {
			created, err := ensureHostPod(ctx, s.hostPods, translation.TargetPod)
			if err != nil {
				return result, fmt.Errorf("ensuring host pod for %s/%s: %w", sourcePod.Namespace, sourcePod.Name, err)
			}
			if created {
				result.CreatedHostPods++
			}
		}

		hostStatus, err := s.hostPods.GetPodStatus(ctx, translation.TargetPod.Namespace, translation.TargetPod.Name)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return result, fmt.Errorf("getting host pod status for %s/%s: %w", translation.TargetPod.Namespace, translation.TargetPod.Name, err)
			}
		} else {
			synced, err := s.translator.SyncStatus(ctx, *hostStatus)
			if err != nil {
				return result, fmt.Errorf("syncing host status for %s/%s: %w", sourcePod.Namespace, sourcePod.Name, err)
			}
			if err := updateTenantPodStatus(ctx, clientset, sourcePod.Namespace, sourcePod.Name, synced); err != nil {
				return result, fmt.Errorf("updating tenant pod status for %s/%s: %w", sourcePod.Namespace, sourcePod.Name, err)
			}
			result.SyncedStatuses++
		}

		if isTerminalPod(sourcePod) {
			if err := s.hostPods.DeletePod(ctx, translation.TargetPod.Namespace, translation.TargetPod.Name); err != nil {
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
		return result, fmt.Errorf("listing host pods for cleanup: %w", err)
	}
	for _, hostPod := range hostPods {
		if _, ok := desired[keyForPod(hostPod.Namespace, hostPod.Name)]; ok {
			continue
		}
		if err := s.hostPods.DeletePod(ctx, hostPod.Namespace, hostPod.Name); err != nil {
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
		return 0, err
	}
	deleted := 0
	for _, pod := range pods {
		if err := host.DeletePod(ctx, pod.Namespace, pod.Name); err != nil {
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

func ensureHostPod(ctx context.Context, host ports.ClusterClient, pod model.PodSpec) (bool, error) {
	if _, err := host.GetPod(ctx, pod.Namespace, pod.Name); err != nil {
		if apierrors.IsNotFound(err) {
			return true, host.CreatePod(ctx, pod)
		}
		return false, err
	}
	return false, nil
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
			Name:  container.Name,
			Image: container.Image,
		})
	}
	return model.PodSpec{
		Name:       pod.Name,
		Namespace:  pod.Namespace,
		Labels:     pod.Labels,
		NodeName:   pod.Spec.NodeName,
		Containers: containers,
	}
}

func keyForPod(namespace, name string) string {
	return namespace + "/" + name
}

func isTerminalPod(pod corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
}
