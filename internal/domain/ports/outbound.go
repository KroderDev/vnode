package ports

import (
	"context"

	"github.com/kroderdev/vnode/internal/domain/model"
)

// ClusterClient abstracts Kubernetes API operations on a cluster.
type ClusterClient interface {
	CreatePod(ctx context.Context, pod model.PodSpec) error
	UpdatePod(ctx context.Context, pod model.PodSpec) error
	DeletePod(ctx context.Context, namespace, name string) error
	GetPod(ctx context.Context, namespace, name string) (*model.PodSpec, error)
	GetPodStatus(ctx context.Context, namespace, name string) (*model.PodStatus, error)
	ListPodsByLabels(ctx context.Context, namespace string, labels map[string]string) ([]model.PodSpec, error)
}

// NodeRegistrar registers and manages virtual nodes in a target cluster.
type NodeRegistrar interface {
	Register(ctx context.Context, node model.VNode, tenant model.TenantRef) error
	Deregister(ctx context.Context, node model.VNode) error
	UpdateNodeStatus(ctx context.Context, node model.VNode) error
}

// IsolationRuntime provides the runtime class name for pod isolation.
type IsolationRuntime interface {
	RuntimeClassName() string
	Validate(ctx context.Context) error
}

// KubeconfigResolver resolves a tenant's kubeconfig from a Secret reference.
type KubeconfigResolver interface {
	Resolve(ctx context.Context, secretNamespace, secretName string) ([]byte, error)
}

// PoolRepository persists and retrieves VNodePool state.
type PoolRepository interface {
	Get(ctx context.Context, namespace, name string) (*model.VNodePool, error)
	Save(ctx context.Context, pool model.VNodePool) error
	List(ctx context.Context) ([]model.VNodePool, error)
}

// NodeRepository persists and retrieves VNode state.
type NodeRepository interface {
	Get(ctx context.Context, namespace, name string) (*model.VNode, error)
	Save(ctx context.Context, node model.VNode) error
	Delete(ctx context.Context, namespace, name string) error
	ListByPool(ctx context.Context, namespace, poolName string) ([]model.VNode, error)
}
