package kubeclient

import (
	"context"
	"fmt"

	"github.com/kroderdev/vnode/api/v1alpha1"
	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/ports"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Ensure interface compliance at compile time.
var (
	_ ports.PoolRepository = (*PoolRepository)(nil)
	_ ports.NodeRepository = (*NodeRepository)(nil)
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
		NodeCount:        cr.Spec.NodeCount,
		PerNodeResources: model.ResourceList{
			CPU:    cr.Spec.PerNodeResources.CPU,
			Memory: cr.Spec.PerNodeResources.Memory,
			Pods:   cr.Spec.PerNodeResources.Pods,
		},
		Phase:      model.PoolPhase(cr.Status.Phase),
		ReadyNodes: cr.Status.ReadyNodes,
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
		// Update status after create
		cr.Status.Phase = string(node.Phase)
		return r.client.Status().Update(ctx, &cr)
	}

	// Update existing
	cr.Status.Phase = string(node.Phase)
	return r.client.Status().Update(ctx, &cr)
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
		Phase:     model.NodePhase(cr.Status.Phase),
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
