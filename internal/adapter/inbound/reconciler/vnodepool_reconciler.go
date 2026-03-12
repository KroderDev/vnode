package reconciler

import (
	"context"
	"fmt"

	"github.com/kroderdev/vnode/api/v1alpha1"
	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/ports"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func mapTaints(in []corev1.Taint) []model.Taint {
	out := make([]model.Taint, 0, len(in))
	for _, t := range in {
		out = append(out, model.Taint{
			Key:    t.Key,
			Value:  t.Value,
			Effect: string(t.Effect),
		})
	}
	return out
}

func mapTolerations(in []corev1.Toleration) []model.Toleration {
	out := make([]model.Toleration, 0, len(in))
	for _, t := range in {
		out = append(out, model.Toleration{
			Key:               t.Key,
			Operator:          string(t.Operator),
			Value:             t.Value,
			Effect:            string(t.Effect),
			TolerationSeconds: t.TolerationSeconds,
		})
	}
	return out
}

const poolFinalizer = "vnode.kroderdev.io/pool-cleanup"

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

	// Handle deletion
	if !cr.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &cr)
	}

	// Ensure finalizer
	if !controllerutil.ContainsFinalizer(&cr, poolFinalizer) {
		controllerutil.AddFinalizer(&cr, poolFinalizer)
		if err := r.Update(ctx, &cr); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
	}

	logger.Info("reconciling VNodePool", "name", cr.Name, "nodeCount", cr.Spec.NodeCount)

	pool := crToPoolModel(&cr)

	// Validate pool spec
	if err := pool.Validate(); err != nil {
		logger.Error(err, "invalid pool spec")
		cr.Status.Phase = string(model.PoolPhaseFailed)
		_ = r.Status().Update(ctx, &cr)
		return ctrl.Result{}, nil // Don't requeue invalid specs
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

func (r *VNodePoolReconciler) handleDeletion(ctx context.Context, cr *v1alpha1.VNodePool) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(cr, poolFinalizer) {
		return ctrl.Result{}, nil
	}

	logger.Info("handling VNodePool deletion", "name", cr.Name)

	// Set phase to Deleting
	cr.Status.Phase = string(model.PoolPhaseDeleting)
	_ = r.Status().Update(ctx, cr)

	// Reconcile with 0 nodes to deprovision all
	pool := crToPoolModel(cr)
	pool.NodeCount = 0
	pool.DeletionPending = true

	if _, err := r.PoolMgr.Reconcile(ctx, pool); err != nil {
		logger.Error(err, "error during pool cleanup")
		return ctrl.Result{}, fmt.Errorf("cleaning up pool %s: %w", cr.Name, err)
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(cr, poolFinalizer)
	if err := r.Update(ctx, cr); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}

	logger.Info("VNodePool cleanup complete", "name", cr.Name)
	return ctrl.Result{}, nil
}

func crToPoolModel(cr *v1alpha1.VNodePool) model.VNodePool {
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
		NodeSelector:    cr.Spec.NodeSelector,
		Taints:          mapTaints(cr.Spec.Taints),
		Tolerations:     mapTolerations(cr.Spec.Tolerations),
		Phase:           model.PoolPhase(cr.Status.Phase),
		ReadyNodes:      cr.Status.ReadyNodes,
		DeletionPending: !cr.DeletionTimestamp.IsZero(),
	}
}

func (r *VNodePoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.VNodePool{}).
		Owns(&v1alpha1.VNode{}).
		Complete(r)
}
