package reconciler

import (
	"context"
	"fmt"
	"time"

	"github.com/kroderdev/vnode/api/v1alpha1"
	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/service"
	"github.com/kroderdev/vnode/internal/observability"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type PodSyncReconciler struct {
	client.Client
	Executor *service.PodExecutionService
	Recorder record.EventRecorder
}

func NewPodSyncReconciler(c client.Client, executor *service.PodExecutionService, recorder record.EventRecorder) *PodSyncReconciler {
	return &PodSyncReconciler{Client: c, Executor: executor, Recorder: recorder}
}

func (r *PodSyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var cr v1alpha1.VNodePool
	if err := r.Get(ctx, req.NamespacedName, &cr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	pool := crToPoolModel(&cr)
	if err := pool.Validate(); err != nil {
		return ctrl.Result{}, nil
	}

	if !cr.DeletionTimestamp.IsZero() {
		deleted, err := service.CleanupPoolPods(ctx, r.Executor.HostClient(), pool)
		if err != nil {
			observability.ExecutionFailures.WithLabelValues(pool.Name).Inc()
			r.recordWarning(&cr, "PodCleanupFailed", err)
			return ctrl.Result{}, fmt.Errorf("cleaning host pods for pool %s: %w", pool.Name, err)
		}
		if deleted > 0 {
			observability.HostPodDeletes.WithLabelValues(pool.Name).Add(float64(deleted))
			r.Recorder.Eventf(&cr, "Normal", "PodCleanupComplete", "Deleted %d host pods during pool cleanup", deleted)
		}
		return ctrl.Result{}, nil
	}

	if pool.Phase == model.PoolPhaseFailed {
		return ctrl.Result{RequeueAfter: 3 * time.Second}, nil
	}

	result, err := r.Executor.ReconcilePool(ctx, pool)
	if err != nil {
		logger.Error(err, "failed to reconcile pool pods")
		observability.ExecutionFailures.WithLabelValues(pool.Name).Inc()
		r.recordWarning(&cr, "PodExecutionFailed", err)
		_ = r.updateExecutionConditions(ctx, cr.Namespace, cr.Name, metav1.ConditionFalse, metav1.ConditionTrue, "PodExecutionFailed", err.Error())
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	observability.HostPodCreates.WithLabelValues(pool.Name).Add(float64(result.CreatedHostPods))
	observability.HostPodDeletes.WithLabelValues(pool.Name).Add(float64(result.DeletedHostPods))
	observability.StatusSyncs.WithLabelValues(pool.Name).Add(float64(result.SyncedStatuses))
	if result.CreatedHostPods > 0 || result.DeletedHostPods > 0 || result.SyncedStatuses > 0 {
		r.Recorder.Eventf(&cr, "Normal", "PodExecutionSynced", "Processed %d tenant pods, created %d host pods, deleted %d host pods, synced %d statuses", result.SourcePods, result.CreatedHostPods, result.DeletedHostPods, result.SyncedStatuses)
	}
	_ = r.updateExecutionConditions(ctx, cr.Namespace, cr.Name, metav1.ConditionTrue, metav1.ConditionFalse, "PodExecutionReady", fmt.Sprintf("Processed %d tenant pods", result.SourcePods))

	return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
}

func (r *PodSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("vnodepool-podsync").
		For(&v1alpha1.VNodePool{}).
		Complete(r)
}

func (r *PodSyncReconciler) updateExecutionConditions(ctx context.Context, namespace, name string, podExecutionReady, degraded metav1.ConditionStatus, reason, message string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var current v1alpha1.VNodePool
		if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &current); err != nil {
			return err
		}
		setPodSyncCondition(&current.Status.Conditions, "PodExecutionReady", podExecutionReady, reason, message)
		setPodSyncCondition(&current.Status.Conditions, "Degraded", degraded, reason, message)
		return r.Status().Update(ctx, &current)
	})
}

func setPodSyncCondition(conditions *[]metav1.Condition, conditionType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	current := *conditions
	for i := range current {
		if current[i].Type != conditionType {
			continue
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

func (r *PodSyncReconciler) recordWarning(pool *v1alpha1.VNodePool, reason string, err error) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(pool, "Warning", reason, "%v", err)
}
