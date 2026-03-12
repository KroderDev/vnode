package ports

import (
	"context"

	"github.com/kroderdev/vnode/internal/domain/model"
)

// PoolManager handles VNodePool reconciliation logic.
type PoolManager interface {
	Reconcile(ctx context.Context, pool model.VNodePool) (model.VNodePool, error)
}

// NodeLifecycle manages individual VNode provisioning and deprovisioning.
type NodeLifecycle interface {
	Provision(ctx context.Context, pool model.VNodePool) (model.VNode, error)
	Deprovision(ctx context.Context, node model.VNode) error
	UpdateStatus(ctx context.Context, node model.VNode) error
}

// PodTranslator translates vcluster pods into host cluster pods.
type PodTranslator interface {
	Translate(ctx context.Context, pod model.PodSpec, pool model.VNodePool, vnodeName string) (model.PodTranslation, error)
	SyncStatus(ctx context.Context, hostStatus model.PodStatus) (model.PodStatus, error)
}
