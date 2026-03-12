package reconciler

import (
	"context"
	"fmt"
	"time"

	"github.com/kroderdev/vnode/api/v1alpha1"
	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/service"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type PodSyncReconciler struct {
	client.Client
	Executor *service.PodExecutionService
}

func NewPodSyncReconciler(c client.Client, executor *service.PodExecutionService) *PodSyncReconciler {
	return &PodSyncReconciler{Client: c, Executor: executor}
}

func (r *PodSyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var cr v1alpha1.VNodePool
	if err := r.Get(ctx, req.NamespacedName, &cr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	pool := crToPoolModel(&cr)

	if !cr.DeletionTimestamp.IsZero() {
		if err := service.CleanupPoolPods(ctx, r.Executor.HostClient(), pool); err != nil {
			return ctrl.Result{}, fmt.Errorf("cleaning host pods for pool %s: %w", pool.Name, err)
		}
		return ctrl.Result{}, nil
	}

	if pool.Phase == model.PoolPhaseFailed {
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	if err := r.Executor.ReconcilePool(ctx, pool); err != nil {
		logger.Error(err, "failed to reconcile pool pods")
		return ctrl.Result{RequeueAfter: 2 * time.Second}, err
	}

	return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
}

func (r *PodSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.VNodePool{}).
		Complete(r)
}
