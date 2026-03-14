package kubeclient

import (
	"context"
	"errors"
	"fmt"

	"github.com/kroderdev/vnode/api/v1alpha1"
	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/ports"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Ensure interface compliance at compile time.
var (
	_ ports.PoolRepository = (*PoolRepository)(nil)
	_ ports.NodeRepository = (*NodeRepository)(nil)
)

const (
	annotationVClusterName      = "vnode.kroderdev.io/vcluster-name"
	annotationVClusterNamespace = "vnode.kroderdev.io/vcluster-namespace"
	annotationKubeconfigSecret  = "vnode.kroderdev.io/kubeconfig-secret"
)

// PoolRepository implements ports.PoolRepository using K8s VNodePool CRs.
type PoolRepository struct {
	client client.Client
}

func NewPoolRepository(c client.Client) *PoolRepository {
	return &PoolRepository{client: c}
}

func (r *PoolRepository) Get(ctx context.Context, namespace, name string) (*model.VNodePool, error) {
	var cr v1alpha1.VNodePool
	if err := r.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &cr); err != nil {
		return nil, err
	}
	pool := crToPool(&cr)
	return &pool, nil
}

func (r *PoolRepository) Save(ctx context.Context, pool model.VNodePool) error {
	var cr v1alpha1.VNodePool
	if err := r.client.Get(ctx, client.ObjectKey{Namespace: pool.Namespace, Name: pool.Name}, &cr); err != nil {
		return err
	}

	cr.Status.Phase = string(pool.Phase)
	cr.Status.ReadyNodes = pool.ReadyNodes
	cr.Status.TotalNodes = int32(len(pool.Nodes))

	return r.client.Status().Update(ctx, &cr)
}

func (r *PoolRepository) List(ctx context.Context) ([]model.VNodePool, error) {
	var list v1alpha1.VNodePoolList
	if err := r.client.List(ctx, &list); err != nil {
		return nil, err
	}
	pools := make([]model.VNodePool, 0, len(list.Items))
	for i := range list.Items {
		pools = append(pools, crToPool(&list.Items[i]))
	}
	return pools, nil
}

func crToPool(cr *v1alpha1.VNodePool) model.VNodePool {
	taints := make([]model.Taint, 0, len(cr.Spec.Taints))
	for _, t := range cr.Spec.Taints {
		taints = append(taints, model.Taint{
			Key:    t.Key,
			Value:  t.Value,
			Effect: string(t.Effect),
		})
	}
	tolerations := make([]model.Toleration, 0, len(cr.Spec.Tolerations))
	for _, t := range cr.Spec.Tolerations {
		tolerations = append(tolerations, model.Toleration{
			Key:               t.Key,
			Operator:          string(t.Operator),
			Value:             t.Value,
			Effect:            string(t.Effect),
			TolerationSeconds: t.TolerationSeconds,
		})
	}

	return model.VNodePool{
		ID:        string(cr.UID),
		Name:      cr.Name,
		Namespace: cr.Namespace,
		TenantRef: model.TenantRef{
			VClusterName:      cr.Spec.TenantRef.VClusterName,
			VClusterNamespace: cr.Spec.TenantRef.VClusterNamespace,
			KubeconfigSecret:  cr.Spec.TenantRef.KubeconfigSecret,
		},
		Mode:             model.PoolMode(cr.Spec.Mode),
		IsolationBackend: cr.Spec.IsolationBackend,
		RuntimeClassName: cr.Spec.RuntimeClassName,
		NodeCount:        cr.Spec.NodeCount,
		PerNodeResources: model.ResourceList{
			CPU:    cr.Spec.PerNodeResources.CPU,
			Memory: cr.Spec.PerNodeResources.Memory,
			Pods:   cr.Spec.PerNodeResources.Pods,
		},
		DisplayName:     cr.Spec.DisplayName,
		NodeSelector:    cr.Spec.NodeSelector,
		Taints:          taints,
		Tolerations:     tolerations,
		Phase:           model.PoolPhase(cr.Status.Phase),
		ReadyNodes:      cr.Status.ReadyNodes,
		DeletionPending: !cr.DeletionTimestamp.IsZero(),
	}
}

// NodeRepository implements ports.NodeRepository using K8s VNode CRs.
type NodeRepository struct {
	client client.Client
}

func NewNodeRepository(c client.Client) *NodeRepository {
	return &NodeRepository{client: c}
}

func (r *NodeRepository) Get(ctx context.Context, namespace, name string) (*model.VNode, error) {
	var cr v1alpha1.VNode
	if err := r.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &cr); err != nil {
		return nil, err
	}
	node := crToNode(&cr)
	return &node, nil
}

func (r *NodeRepository) Save(ctx context.Context, node model.VNode) error {
	var cr v1alpha1.VNode
	err := r.client.Get(ctx, client.ObjectKey{Namespace: node.Namespace, Name: node.Name}, &cr)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		// Create new VNode CR
		cr = v1alpha1.VNode{
			ObjectMeta: metav1.ObjectMeta{
				Name:      node.Name,
				Namespace: node.Namespace,
				Labels: map[string]string{
					"vnode.kroderdev.io/pool": node.PoolName,
				},
				Annotations: map[string]string{
					annotationVClusterName:      node.TenantRef.VClusterName,
					annotationVClusterNamespace: node.TenantRef.VClusterNamespace,
					annotationKubeconfigSecret:  node.TenantRef.KubeconfigSecret,
				},
			},
			Spec: v1alpha1.VNodeSpec{
				PoolRef: node.PoolName,
				Capacity: v1alpha1.NodeResources{
					CPU:    node.Capacity.CPU,
					Memory: node.Capacity.Memory,
					Pods:   node.Capacity.Pods,
				},
			},
		}
		if createErr := r.client.Create(ctx, &cr); createErr != nil {
			return fmt.Errorf("creating VNode CR: %w", createErr)
		}
		applyNodeStatus(&cr, node)
		if err := r.client.Status().Update(ctx, &cr); err != nil {
			if apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
				return r.updateStatus(ctx, node)
			}
			return err
		}
		return nil
	}

	if cr.Annotations == nil {
		cr.Annotations = map[string]string{}
	}
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var current v1alpha1.VNode
		if err := r.client.Get(ctx, client.ObjectKey{Namespace: node.Namespace, Name: node.Name}, &current); err != nil {
			return err
		}
		if current.Annotations == nil {
			current.Annotations = map[string]string{}
		}
		current.Annotations[annotationVClusterName] = node.TenantRef.VClusterName
		current.Annotations[annotationVClusterNamespace] = node.TenantRef.VClusterNamespace
		current.Annotations[annotationKubeconfigSecret] = node.TenantRef.KubeconfigSecret
		return r.client.Update(ctx, &current)
	}); err != nil {
		if isIgnorableClientError(err) {
			return nil
		}
		return err
	}

	return r.updateStatus(ctx, node)
}

func (r *NodeRepository) Delete(ctx context.Context, namespace, name string) error {
	cr := &v1alpha1.VNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	return r.client.Delete(ctx, cr)
}

func (r *NodeRepository) ListByPool(ctx context.Context, namespace, poolName string) ([]model.VNode, error) {
	var list v1alpha1.VNodeList
	if err := r.client.List(ctx, &list,
		client.InNamespace(namespace),
		client.MatchingLabels{"vnode.kroderdev.io/pool": poolName},
	); err != nil {
		return nil, err
	}
	nodes := make([]model.VNode, 0, len(list.Items))
	for i := range list.Items {
		nodes = append(nodes, crToNode(&list.Items[i]))
	}
	return nodes, nil
}

func crToNode(cr *v1alpha1.VNode) model.VNode {
	node := model.VNode{
		ID:        string(cr.UID),
		Name:      cr.Name,
		Namespace: cr.Namespace,
		PoolName:  cr.Spec.PoolRef,
		TenantRef: model.TenantRef{
			VClusterName:      cr.Annotations[annotationVClusterName],
			VClusterNamespace: cr.Annotations[annotationVClusterNamespace],
			KubeconfigSecret:  cr.Annotations[annotationKubeconfigSecret],
		},
		Phase: model.NodePhase(cr.Status.Phase),
		Capacity: model.ResourceList{
			CPU:    cr.Spec.Capacity.CPU,
			Memory: cr.Spec.Capacity.Memory,
			Pods:   cr.Spec.Capacity.Pods,
		},
		Allocatable: model.ResourceList{
			CPU:    cr.Spec.Capacity.CPU,
			Memory: cr.Spec.Capacity.Memory,
			Pods:   cr.Spec.Capacity.Pods,
		},
	}

	for _, c := range cr.Status.Conditions {
		node.Conditions = append(node.Conditions, model.NodeCondition{
			Type:    model.NodeConditionType(c.Type),
			Status:  c.Status == metav1.ConditionTrue,
			Reason:  c.Reason,
			Message: c.Message,
		})
	}

	return node
}

func (r *NodeRepository) updateStatus(ctx context.Context, node model.VNode) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var current v1alpha1.VNode
		if err := r.client.Get(ctx, client.ObjectKey{Namespace: node.Namespace, Name: node.Name}, &current); err != nil {
			if isIgnorableClientError(err) {
				return nil
			}
			return err
		}

		applyNodeStatus(&current, node)

		if err := r.client.Status().Update(ctx, &current); err != nil {
			if isIgnorableClientError(err) {
				return nil
			}
			if apierrors.IsConflict(err) {
				return err
			}
			return err
		}
		return nil
	})
}

func isIgnorableClientError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func applyNodeStatus(cr *v1alpha1.VNode, node model.VNode) {
	cr.Status.Phase = string(node.Phase)
	cr.Status.Conditions = make([]metav1.Condition, 0, len(node.Conditions))
	for _, c := range node.Conditions {
		conditionStatus := metav1.ConditionFalse
		if c.Status {
			conditionStatus = metav1.ConditionTrue
		}
		cr.Status.Conditions = append(cr.Status.Conditions, metav1.Condition{
			Type:               string(c.Type),
			Status:             conditionStatus,
			Reason:             c.Reason,
			Message:            c.Message,
			LastTransitionTime: metav1.Now(),
		})
	}
}
