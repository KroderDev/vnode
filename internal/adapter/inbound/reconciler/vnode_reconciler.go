package reconciler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kroderdev/vnode/api/v1alpha1"
	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/ports"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	vnodeAnnotationVClusterName      = "vnode.kroderdev.io/vcluster-name"
	vnodeAnnotationVClusterNamespace = "vnode.kroderdev.io/vcluster-namespace"
	vnodeAnnotationKubeconfigSecret  = "vnode.kroderdev.io/kubeconfig-secret"
)

// VNodeReconciler reconciles VNode objects.
type VNodeReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	NodeSvc ports.NodeLifecycle
}

func NewVNodeReconciler(c client.Client, scheme *runtime.Scheme, nodeSvc ports.NodeLifecycle) *VNodeReconciler {
	return &VNodeReconciler{
		Client:  c,
		Scheme:  scheme,
		NodeSvc: nodeSvc,
	}
}

func (r *VNodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var cr v1alpha1.VNode
	if err := r.Get(ctx, req.NamespacedName, &cr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("reconciling VNode", "name", cr.Name, "pool", cr.Spec.PoolRef)

	node := model.VNode{
		ID:        string(cr.UID),
		Name:      cr.Name,
		Namespace: cr.Namespace,
		PoolName:  cr.Spec.PoolRef,
		TenantRef: model.TenantRef{
			VClusterName:      cr.Annotations[vnodeAnnotationVClusterName],
			VClusterNamespace: cr.Annotations[vnodeAnnotationVClusterNamespace],
			KubeconfigSecret:  cr.Annotations[vnodeAnnotationKubeconfigSecret],
		},
		Phase: model.NodePhase(cr.Status.Phase),
		Capacity: model.ResourceList{
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

	if err := r.NodeSvc.UpdateStatus(ctx, node); err != nil {
		if apierrors.IsNotFound(err) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to update node status")
		return ctrl.Result{}, fmt.Errorf("updating node %s: %w", node.Name, err)
	}

	// VNode owns convergence for tenant registration and heartbeat. Failed or pending nodes
	// must reschedule themselves so transient vcluster startup timeouts can heal without
	// depending on the parent pool controller to recreate or resync them.
	if shouldRequeueVNode(node) {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

func shouldRequeueVNode(node model.VNode) bool {
	switch node.Phase {
	case model.NodePhasePending, model.NodePhaseNotReady:
		return true
	default:
		return false
	}
}

func (r *VNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.VNode{}).
		Complete(r)
}
