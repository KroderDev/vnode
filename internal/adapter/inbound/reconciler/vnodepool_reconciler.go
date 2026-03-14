package reconciler

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/kroderdev/vnode/api/v1alpha1"
	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/ports"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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
		_ = r.updatePoolStatus(ctx, cr.Namespace, cr.Name, func(status *v1alpha1.VNodePoolStatus) {
			status.Phase = string(model.PoolPhaseFailed)
			setStatusCondition(&status.Conditions, "Ready", metav1.ConditionFalse, "ValidationFailed", err.Error())
			setStatusCondition(&status.Conditions, "Degraded", metav1.ConditionTrue, "ValidationFailed", err.Error())
		})
		return ctrl.Result{}, nil // Don't requeue invalid specs
	}

	result, err := r.PoolMgr.Reconcile(ctx, pool)
	if err != nil {
		logger.Error(err, "failed to reconcile pool")
		_ = r.updatePoolStatus(ctx, cr.Namespace, cr.Name, func(status *v1alpha1.VNodePoolStatus) {
			status.Phase = string(model.PoolPhaseFailed)
			setStatusCondition(&status.Conditions, "Ready", metav1.ConditionFalse, "ReconcileFailed", err.Error())
			setStatusCondition(&status.Conditions, "Degraded", metav1.ConditionTrue, "ReconcileFailed", err.Error())
		})
		return ctrl.Result{}, fmt.Errorf("reconciling pool %s: %w", pool.Name, err)
	}

	if err := r.updatePoolStatus(ctx, cr.Namespace, cr.Name, func(status *v1alpha1.VNodePoolStatus) {
		status.Phase = string(result.Phase)
		status.ReadyNodes = result.ReadyNodes
		status.TotalNodes = result.NodeCount
		setStatusCondition(&status.Conditions, "Ready", conditionStatus(result.Phase == model.PoolPhaseReady), phaseReason(result.Phase), fmt.Sprintf("%d/%d virtual nodes ready", result.ReadyNodes, result.NodeCount))
		setStatusCondition(&status.Conditions, "Degraded", conditionStatus(result.Phase == model.PoolPhaseFailed), phaseReason(result.Phase), fmt.Sprintf("Pool phase is %s", result.Phase))
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating pool status: %w", err)
	}

	if !cr.DeletionTimestamp.IsZero() && result.Phase != model.PoolPhaseReady {
		return ctrl.Result{RequeueAfter: time.Second}, nil
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
	_ = r.updatePoolStatus(ctx, cr.Namespace, cr.Name, func(status *v1alpha1.VNodePoolStatus) {
		status.Phase = string(model.PoolPhaseDeleting)
		setStatusCondition(&status.Conditions, "Ready", metav1.ConditionFalse, "Deleting", "Pool is being deleted")
		setStatusCondition(&status.Conditions, "Degraded", metav1.ConditionFalse, "Deleting", "Pool cleanup in progress")
	})

	// Reconcile with 0 nodes to deprovision all
	pool := crToPoolModel(cr)
	pool.NodeCount = 0
	pool.DeletionPending = true

	if _, err := r.PoolMgr.Reconcile(ctx, pool); err != nil {
		logger.Error(err, "error during pool cleanup")
		return ctrl.Result{}, fmt.Errorf("cleaning up pool %s: %w", cr.Name, err)
	}

	var remaining v1alpha1.VNodeList
	if err := r.List(ctx, &remaining,
		client.InNamespace(cr.Namespace),
		client.MatchingLabels{"vnode.kroderdev.io/pool": cr.Name},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing remaining VNodes for pool %s: %w", cr.Name, err)
	}
	if len(remaining.Items) > 0 {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	// Remove finalizer on the latest resource version to avoid status-update races.
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var current v1alpha1.VNodePool
		if err := r.Get(ctx, client.ObjectKeyFromObject(cr), &current); err != nil {
			return client.IgnoreNotFound(err)
		}
		controllerutil.RemoveFinalizer(&current, poolFinalizer)
		return r.Update(ctx, &current)
	}); err != nil {
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
		For(&v1alpha1.VNodePool{}, builder.WithPredicates(vnodePoolPredicates())).
		Watches(&v1alpha1.VNode{},
			handler.EnqueueRequestsFromMapFunc(vnodeToPool),
			builder.WithPredicates(vnodePhaseChangedPredicate()),
		).
		Complete(r)
}

func vnodePoolPredicates() predicate.Predicate {
	return predicate.GenerationChangedPredicate{}
}

// vnodeToPool maps a VNode event to its parent VNodePool reconcile request.
func vnodeToPool(_ context.Context, obj client.Object) []reconcile.Request {
	vnode, ok := obj.(*v1alpha1.VNode)
	if !ok {
		return nil
	}
	poolName := vnode.Labels["vnode.kroderdev.io/pool"]
	if poolName == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: client.ObjectKey{
			Namespace: vnode.Namespace,
			Name:      poolName,
		},
	}}
}

// vnodePhaseChangedPredicate triggers only when a VNode's status phase changes.
func vnodePhaseChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool { return false },
		DeleteFunc: func(e event.DeleteEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldVNode, ok1 := e.ObjectOld.(*v1alpha1.VNode)
			newVNode, ok2 := e.ObjectNew.(*v1alpha1.VNode)
			if !ok1 || !ok2 {
				return false
			}
			return oldVNode.Status.Phase != newVNode.Status.Phase
		},
		GenericFunc: func(e event.GenericEvent) bool { return false },
	}
}

func (r *VNodePoolReconciler) updatePoolStatus(ctx context.Context, namespace, name string, mutate func(*v1alpha1.VNodePoolStatus)) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var current v1alpha1.VNodePool
		if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &current); err != nil {
			return err
		}
		base := current.DeepCopy()
		mutate(&current.Status)
		if reflect.DeepEqual(base.Status, current.Status) {
			return nil
		}
		return r.Status().Patch(ctx, &current, client.MergeFrom(base))
	})
}

func setStatusCondition(conditions *[]metav1.Condition, conditionType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	current := *conditions
	for i := range current {
		if current[i].Type != conditionType {
			continue
		}
		if current[i].Status == status && current[i].Reason == reason && current[i].Message == message {
			*conditions = current
			return
		}
		current[i].Status = status
		current[i].Reason = reason
		current[i].Message = message
		current[i].LastTransitionTime = now
		*conditions = current
		return
	}
	*conditions = append(current, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	})
}

func conditionStatus(ok bool) metav1.ConditionStatus {
	if ok {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}

func phaseReason(phase model.PoolPhase) string {
	switch phase {
	case model.PoolPhaseReady:
		return "PoolReady"
	case model.PoolPhaseScaling:
		return "PoolScaling"
	case model.PoolPhaseDeleting:
		return "PoolDeleting"
	case model.PoolPhaseFailed:
		return "PoolFailed"
	default:
		return "PoolPending"
	}
}
