package virtualkubelet

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/ports"
	"github.com/kroderdev/vnode/internal/version"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
)

const leaseNamespace = "kube-node-lease"

// ClientFactory builds a Kubernetes clientset from a REST config.
type ClientFactory func(cfg *rest.Config) (kubernetes.Interface, error)

// TenantClientManager resolves tenant kubeconfigs and caches clientsets per tenant secret.
type TenantClientManager struct {
	resolver ports.KubeconfigResolver
	factory  ClientFactory

	mu      sync.RWMutex
	clients map[string]kubernetes.Interface
}

func NewTenantClientManager(resolver ports.KubeconfigResolver) *TenantClientManager {
	return &TenantClientManager{
		resolver: resolver,
		factory: func(cfg *rest.Config) (kubernetes.Interface, error) {
			return kubernetes.NewForConfig(cfg)
		},
		clients: map[string]kubernetes.Interface{},
	}
}

func (m *TenantClientManager) Get(ctx context.Context, tenant model.TenantRef) (kubernetes.Interface, error) {
	key := cacheKey(tenant)

	m.mu.RLock()
	if clientset, ok := m.clients[key]; ok {
		m.mu.RUnlock()
		return clientset, nil
	}
	m.mu.RUnlock()

	kubeconfig, err := m.resolver.Resolve(ctx, tenant.VClusterNamespace, tenant.KubeconfigSecret)
	if err != nil {
		m.Invalidate(tenant)
		return nil, fmt.Errorf("resolving tenant kubeconfig: %w", err)
	}

	cfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		m.Invalidate(tenant)
		return nil, fmt.Errorf("building tenant rest config: %w", err)
	}

	clientset, err := m.factory(cfg)
	if err != nil {
		m.Invalidate(tenant)
		return nil, fmt.Errorf("building tenant clientset: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[key] = clientset
	return clientset, nil
}

func (m *TenantClientManager) Invalidate(tenant model.TenantRef) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.clients, cacheKey(tenant))
}

func cacheKey(tenant model.TenantRef) string {
	return fmt.Sprintf("%s/%s", tenant.VClusterNamespace, tenant.KubeconfigSecret)
}

// Registrar implements ports.NodeRegistrar using the tenant cluster API.
type Registrar struct {
	clients *TenantClientManager
}

var _ ports.NodeRegistrar = (*Registrar)(nil)

func NewRegistrar(clients *TenantClientManager) *Registrar {
	return &Registrar{clients: clients}
}

func (r *Registrar) Register(ctx context.Context, node model.VNode, tenant model.TenantRef) error {
	clientset, err := r.clients.Get(ctx, tenant)
	if err != nil {
		return err
	}

	if err := ensureLeaseNamespace(ctx, clientset); err != nil {
		return err
	}

	if err := r.upsertNode(ctx, clientset, node); err != nil {
		return err
	}
	if err := r.UpdateNodeStatus(ctx, node, tenant); err != nil {
		return err
	}

	return nil
}

func (r *Registrar) Deregister(ctx context.Context, node model.VNode, tenant model.TenantRef) error {
	if tenant.KubeconfigSecret == "" {
		return nil
	}
	clientset, err := r.clients.Get(ctx, tenant)
	if err != nil {
		return err
	}

	if err := clientset.CoordinationV1().Leases(leaseNamespace).Delete(ctx, node.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting lease %s: %w", node.Name, err)
	}
	if err := clientset.CoreV1().Nodes().Delete(ctx, node.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("deleting tenant node %s: %w", node.Name, err)
	}

	return nil
}

func (r *Registrar) UpdateNodeStatus(ctx context.Context, node model.VNode, tenant model.TenantRef) error {
	if tenant.KubeconfigSecret == "" {
		return nil
	}
	clientset, err := r.clients.Get(ctx, tenant)
	if err != nil {
		return err
	}

	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := clientset.CoreV1().Nodes().Get(ctx, node.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("getting tenant node %s: %w", node.Name, err)
		}

		current.Status.Capacity = buildResources(node.Capacity)
		current.Status.Allocatable = buildResources(node.Allocatable)
		current.Status.Conditions = buildNodeConditions(node)
		current.Status.NodeInfo = corev1.NodeSystemInfo{
			KubeletVersion:          "vnode/" + version.Version,
			OperatingSystem:         "linux",
			Architecture:            "amd64",
			ContainerRuntimeVersion: "vnode/" + version.Version,
		}

		if _, err := clientset.CoreV1().Nodes().UpdateStatus(ctx, current, metav1.UpdateOptions{}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return fmt.Errorf("updating tenant node %s status: %w", node.Name, err)
	}

	if err := upsertLease(ctx, clientset, node.Name); err != nil {
		return err
	}

	return nil
}

func (r *Registrar) upsertNode(ctx context.Context, clientset kubernetes.Interface, node model.VNode) error {
	current, err := clientset.CoreV1().Nodes().Get(ctx, node.Name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("getting tenant node %s: %w", node.Name, err)
		}
		created := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:        node.Name,
				Labels:      nodeLabels(node),
				Annotations: nodeAnnotations(node),
			},
			Spec: corev1.NodeSpec{
				Taints: mapNodeTaints(node.Taints),
			},
		}
		if _, err := clientset.CoreV1().Nodes().Create(ctx, created, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("creating tenant node %s: %w", node.Name, err)
		}
		return nil
	}

	current.Labels = nodeLabels(node)
	current.Annotations = nodeAnnotations(node)
	current.Spec.Taints = mapNodeTaints(node.Taints)

	if _, err := clientset.CoreV1().Nodes().Update(ctx, current, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating tenant node %s: %w", node.Name, err)
	}

	return nil
}

func ensureLeaseNamespace(ctx context.Context, clientset kubernetes.Interface) error {
	if _, err := clientset.CoreV1().Namespaces().Get(ctx, leaseNamespace, metav1.GetOptions{}); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("getting lease namespace: %w", err)
	}

	_, err := clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: leaseNamespace},
	}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("creating lease namespace: %w", err)
	}
	return nil
}

func upsertLease(ctx context.Context, clientset kubernetes.Interface, nodeName string) error {
	now := metav1.NewMicroTime(time.Now().UTC())
	duration := int32(40)

	current, err := clientset.CoordinationV1().Leases(leaseNamespace).Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("getting node lease %s: %w", nodeName, err)
		}
		_, err := clientset.CoordinationV1().Leases(leaseNamespace).Create(ctx, &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nodeName,
				Namespace: leaseNamespace,
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity:       &nodeName,
				LeaseDurationSeconds: &duration,
				RenewTime:            &now,
			},
		}, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("creating node lease %s: %w", nodeName, err)
		}
		return nil
	}

	current.Spec.HolderIdentity = &nodeName
	current.Spec.LeaseDurationSeconds = &duration
	current.Spec.RenewTime = &now
	if _, err := clientset.CoordinationV1().Leases(leaseNamespace).Update(ctx, current, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating node lease %s: %w", nodeName, err)
	}
	return nil
}

func buildResources(in model.ResourceList) corev1.ResourceList {
	out := corev1.ResourceList{}
	if in.CPU != "" {
		out[corev1.ResourceCPU] = resource.MustParse(in.CPU)
	}
	if in.Memory != "" {
		out[corev1.ResourceMemory] = resource.MustParse(in.Memory)
	}
	if in.Pods > 0 {
		out[corev1.ResourcePods] = *resource.NewQuantity(int64(in.Pods), resource.DecimalSI)
	}
	return out
}

func buildNodeConditions(node model.VNode) []corev1.NodeCondition {
	now := metav1.NewTime(time.Now().UTC())
	conditions := make([]corev1.NodeCondition, 0, len(node.Conditions))
	for _, c := range node.Conditions {
		status := corev1.ConditionFalse
		if c.Status {
			status = corev1.ConditionTrue
		}
		conditionType := corev1.NodeConditionType(c.Type)
		if c.Type == model.NodeConditionReady {
			conditionType = corev1.NodeReady
		}
		conditions = append(conditions, corev1.NodeCondition{
			Type:               conditionType,
			Status:             status,
			Reason:             c.Reason,
			Message:            c.Message,
			LastHeartbeatTime:  now,
			LastTransitionTime: now,
		})
	}
	return conditions
}

func nodeLabels(node model.VNode) map[string]string {
	return map[string]string{
		"kubernetes.io/os":          "linux",
		"kubernetes.io/arch":        "amd64",
		"node.kubernetes.io/exclude-from-external-load-balancers": "true",
		"node-role.kubernetes.io/vnode": "",
		"vnode.kroderdev.io/managed":    "true",
		"vnode.kroderdev.io/pool":       node.PoolName,
		"vnode.kroderdev.io/vnode":      node.Name,
	}
}

func nodeAnnotations(node model.VNode) map[string]string {
	return map[string]string{
		"vnode.kroderdev.io/vcluster-name":      node.TenantRef.VClusterName,
		"vnode.kroderdev.io/vcluster-namespace": node.TenantRef.VClusterNamespace,
	}
}

func mapNodeTaints(in []model.Taint) []corev1.Taint {
	out := make([]corev1.Taint, 0, len(in))
	for _, t := range in {
		out = append(out, corev1.Taint{
			Key:    t.Key,
			Value:  t.Value,
			Effect: corev1.TaintEffect(t.Effect),
		})
	}
	return out
}
