package reconciler

import (
	"context"
	"fmt"

	"github.com/kroderdev/vnode/api/v1alpha1"
	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/ports"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// VNodePoolReconciler reconciles VNodePool objects.
type VNodePoolReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	PoolMgr ports.PoolManager
}

func NewVNodePoolReconciler(c client.Client, scheme *runtime.Scheme, poolMgr ports.PoolManager) *VNodePoolReconciler {
	return &VNodePoolReconciler{
		Client:  c,
		Scheme:  scheme,
		PoolMgr: poolMgr,
	}
}

func (r *VNodePoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var cr v1alpha1.VNodePool
	if err := r.Get(ctx, req.NamespacedName, &cr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("reconciling VNodePool", "name", cr.Name, "nodeCount", cr.Spec.NodeCount)

	pool := model.VNodePool{
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

	result, err := r.PoolMgr.Reconcile(ctx, pool)
	if err != nil {
		logger.Error(err, "failed to reconcile pool")
		cr.Status.Phase = string(model.PoolPhaseFailed)
		_ = r.Status().Update(ctx, &cr)
		return ctrl.Result{}, fmt.Errorf("reconciling pool %s: %w", pool.Name, err)
	}

	cr.Status.Phase = string(result.Phase)
	cr.Status.ReadyNodes = result.ReadyNodes
	cr.Status.TotalNodes = result.NodeCount

	if err := r.Status().Update(ctx, &cr); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating pool status: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *VNodePoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.VNodePool{}).
		Owns(&v1alpha1.VNode{}).
		Complete(r)
}
