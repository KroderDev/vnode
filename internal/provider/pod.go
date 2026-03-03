package provider

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"github.com/kroderdev/vnode/internal/translator"
)

// Provider implements the Virtual Kubelet PodLifecycleHandler and NodeProvider interfaces.
type Provider struct {
	log           *slog.Logger
	hostClient    kubernetes.Interface
	translator    *translator.Translator
	hostNamespace string

	// Track pods for GetPod/GetPods
	mu   sync.RWMutex
	pods map[string]*corev1.Pod // key: "namespace/name" from vcluster perspective

	notifyPods func(*corev1.Pod)
}

// New creates a new Provider.
func New(log *slog.Logger, hostClient kubernetes.Interface, trans *translator.Translator, hostNamespace string) *Provider {
	return &Provider{
		log:           log,
		hostClient:    hostClient,
		translator:    trans,
		hostNamespace: hostNamespace,
		pods:          make(map[string]*corev1.Pod),
	}
}

func podKey(namespace, name string) string {
	return namespace + "/" + name
}

// CreatePod takes a pod from the vcluster and creates it in the host cluster.
func (p *Provider) CreatePod(ctx context.Context, pod *corev1.Pod) error {
	p.log.Info("creating pod", "namespace", pod.Namespace, "name", pod.Name)

	hostPod := p.translator.Translate(pod)

	created, err := p.hostClient.CoreV1().Pods(p.hostNamespace).Create(ctx, hostPod, metav1.CreateOptions{})
	if err != nil {
		if k8serr.IsAlreadyExists(err) {
			p.log.Warn("pod already exists on host", "name", hostPod.Name)
			return nil
		}
		return fmt.Errorf("create host pod: %w", err)
	}

	p.mu.Lock()
	pod.Status = p.translateStatus(created)
	p.pods[podKey(pod.Namespace, pod.Name)] = pod.DeepCopy()
	p.mu.Unlock()

	return nil
}

// UpdatePod updates a pod in the host cluster.
func (p *Provider) UpdatePod(ctx context.Context, pod *corev1.Pod) error {
	p.log.Info("updating pod", "namespace", pod.Namespace, "name", pod.Name)

	hostPodName := p.translator.HostPodName(pod.Namespace, pod.Name)

	existing, err := p.hostClient.CoreV1().Pods(p.hostNamespace).Get(ctx, hostPodName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get host pod for update: %w", err)
	}

	// Update containers (image changes, etc.)
	hostPod := p.translator.Translate(pod)
	existing.Spec.Containers = hostPod.Spec.Containers
	existing.Spec.InitContainers = hostPod.Spec.InitContainers

	_, err = p.hostClient.CoreV1().Pods(p.hostNamespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("update host pod: %w", err)
	}

	p.mu.Lock()
	p.pods[podKey(pod.Namespace, pod.Name)] = pod.DeepCopy()
	p.mu.Unlock()

	return nil
}

// DeletePod removes a pod from the host cluster.
func (p *Provider) DeletePod(ctx context.Context, pod *corev1.Pod) error {
	p.log.Info("deleting pod", "namespace", pod.Namespace, "name", pod.Name)

	hostPodName := p.translator.HostPodName(pod.Namespace, pod.Name)

	err := p.hostClient.CoreV1().Pods(p.hostNamespace).Delete(ctx, hostPodName, metav1.DeleteOptions{})
	if err != nil && !k8serr.IsNotFound(err) {
		return fmt.Errorf("delete host pod: %w", err)
	}

	p.mu.Lock()
	key := podKey(pod.Namespace, pod.Name)
	if cachedPod, ok := p.pods[key]; ok {
		now := metav1.Now()
		cachedPod.Status.Phase = corev1.PodSucceeded
		cachedPod.Status.Reason = "ProviderTerminated"
		for i := range cachedPod.Status.ContainerStatuses {
			cachedPod.Status.ContainerStatuses[i].State = corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{
					ExitCode:   0,
					Reason:     "Completed",
					FinishedAt: now,
				},
			}
		}
		if p.notifyPods != nil {
			p.notifyPods(cachedPod)
		}
		delete(p.pods, key)
	}
	p.mu.Unlock()

	return nil
}

// GetPod returns a pod by name from the vcluster perspective.
func (p *Provider) GetPod(_ context.Context, namespace, name string) (*corev1.Pod, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	pod, ok := p.pods[podKey(namespace, name)]
	if !ok {
		return nil, nil
	}
	return pod.DeepCopy(), nil
}

// GetPodStatus returns the status of a pod by querying the host cluster.
func (p *Provider) GetPodStatus(ctx context.Context, namespace, name string) (*corev1.PodStatus, error) {
	hostPodName := p.translator.HostPodName(namespace, name)

	hostPod, err := p.hostClient.CoreV1().Pods(p.hostNamespace).Get(ctx, hostPodName, metav1.GetOptions{})
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get host pod status: %w", err)
	}

	status := p.translateStatus(hostPod)
	return &status, nil
}

// GetPods returns all pods tracked by this provider.
func (p *Provider) GetPods(_ context.Context) ([]*corev1.Pod, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	pods := make([]*corev1.Pod, 0, len(p.pods))
	for _, pod := range p.pods {
		pods = append(pods, pod.DeepCopy())
	}
	return pods, nil
}

// NotifyPods registers a callback for pod status changes.
func (p *Provider) NotifyPods(_ context.Context, cb func(*corev1.Pod)) {
	p.notifyPods = cb
}

// SyncPodStatus fetches real pod statuses from the host and updates the cache.
// This should be called periodically.
func (p *Provider) SyncPodStatus(ctx context.Context) error {
	matchLabels := p.translator.MatchLabels()
	selector := labels.Set(matchLabels).AsSelector()

	hostPods, err := p.hostClient.CoreV1().Pods(p.hostNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return fmt.Errorf("list host pods: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range hostPods.Items {
		hostPod := &hostPods.Items[i]
		vPodName := hostPod.Labels[translator.LabelVClusterPodName]
		vPodNS := hostPod.Labels[translator.LabelVClusterPodNamespace]
		key := podKey(vPodNS, vPodName)

		if cached, ok := p.pods[key]; ok {
			cached.Status = p.translateStatus(hostPod)
			if p.notifyPods != nil {
				p.notifyPods(cached.DeepCopy())
			}
		}
	}

	return nil
}

func (p *Provider) translateStatus(hostPod *corev1.Pod) corev1.PodStatus {
	return corev1.PodStatus{
		Phase:             hostPod.Status.Phase,
		Conditions:        hostPod.Status.Conditions,
		Message:           hostPod.Status.Message,
		Reason:            hostPod.Status.Reason,
		HostIP:            hostPod.Status.HostIP,
		HostIPs:           hostPod.Status.HostIPs,
		PodIP:             hostPod.Status.PodIP,
		PodIPs:            hostPod.Status.PodIPs,
		StartTime:         hostPod.Status.StartTime,
		ContainerStatuses: hostPod.Status.ContainerStatuses,
	}
}
