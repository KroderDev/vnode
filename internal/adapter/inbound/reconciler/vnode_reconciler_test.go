package reconciler

import (
	"context"
	"testing"
	"time"

	"github.com/kroderdev/vnode/api/v1alpha1"
	"github.com/kroderdev/vnode/internal/domain/model"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type fakeNodeLifecycle struct {
	updateStatusFn func(context.Context, model.VNode) error
}

func (f *fakeNodeLifecycle) Provision(context.Context, model.VNodePool) (model.VNode, error) {
	return model.VNode{}, nil
}

func (f *fakeNodeLifecycle) Deprovision(context.Context, model.VNode) error {
	return nil
}

func (f *fakeNodeLifecycle) UpdateStatus(ctx context.Context, node model.VNode) error {
	if f.updateStatusFn != nil {
		return f.updateStatusFn(ctx, node)
	}
	return nil
}

func TestVNodeReconciler_RequeuesNotReadyNodes(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding scheme: %v", err)
	}

	nodeCR := &v1alpha1.VNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "node-1",
			Namespace: "default",
		},
		Spec: v1alpha1.VNodeSpec{
			PoolRef: "pool-a",
			Capacity: v1alpha1.NodeResources{
				CPU:    "2000m",
				Memory: "4Gi",
				Pods:   110,
			},
		},
		Status: v1alpha1.VNodeStatus{
			Phase: string(model.NodePhaseNotReady),
			Conditions: []metav1.Condition{{
				Type:    string(model.NodeConditionRegistered),
				Status:  metav1.ConditionFalse,
				Reason:  "RegistrationFailed",
				Message: "dial tcp timeout",
			}},
		},
	}

	reconciler := &VNodeReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(nodeCR).WithObjects(nodeCR).Build(),
		Scheme: scheme,
		NodeSvc: &fakeNodeLifecycle{
			updateStatusFn: func(_ context.Context, node model.VNode) error {
				if node.Phase != model.NodePhaseNotReady {
					t.Fatalf("expected NotReady node phase, got %s", node.Phase)
				}
				return nil
			},
		},
	}

	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(nodeCR)})
	if err != nil {
		t.Fatalf("unexpected reconcile error: %v", err)
	}
	if result.RequeueAfter != 2*time.Second {
		t.Fatalf("expected RequeueAfter 2s, got %s", result.RequeueAfter)
	}
}

func TestVNodeReconciler_DoesNotRequeueReadyNodes(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding scheme: %v", err)
	}

	nodeCR := &v1alpha1.VNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "node-1",
			Namespace: "default",
		},
		Spec: v1alpha1.VNodeSpec{
			PoolRef: "pool-a",
			Capacity: v1alpha1.NodeResources{
				CPU:    "2000m",
				Memory: "4Gi",
				Pods:   110,
			},
		},
		Status: v1alpha1.VNodeStatus{
			Phase: string(model.NodePhaseReady),
			Conditions: []metav1.Condition{{
				Type:    string(model.NodeConditionReady),
				Status:  metav1.ConditionTrue,
				Reason:  "Ready",
				Message: "Node is ready",
			}},
		},
	}

	reconciler := &VNodeReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(nodeCR).WithObjects(nodeCR).Build(),
		Scheme: scheme,
		NodeSvc: &fakeNodeLifecycle{
			updateStatusFn: func(_ context.Context, node model.VNode) error {
				if node.Phase != model.NodePhaseReady {
					t.Fatalf("expected Ready node phase, got %s", node.Phase)
				}
				return nil
			},
		},
	}

	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(nodeCR)})
	if err != nil {
		t.Fatalf("unexpected reconcile error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("expected no requeue for ready node, got %s", result.RequeueAfter)
	}
}
