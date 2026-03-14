package reconciler

import (
	"testing"

	"github.com/kroderdev/vnode/api/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestVNodePoolPredicatesIgnoreStatusOnlyUpdates(t *testing.T) {
	predicate := vnodePoolPredicates()

	oldObj := &v1alpha1.VNodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pool-a",
			Namespace:  "default",
			Generation: 1,
		},
		Status: v1alpha1.VNodePoolStatus{
			Phase:      "Scaling",
			ReadyNodes: 0,
			TotalNodes: 3,
		},
	}
	newObj := oldObj.DeepCopy()
	newObj.Status.ReadyNodes = 1
	newObj.Status.Phase = "Scaling"

	if predicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatal("expected status-only VNodePool update to be ignored")
	}
}

func TestVNodePhaseChangedPredicateIgnoresSamePhase(t *testing.T) {
	pred := vnodePhaseChangedPredicate()

	old := &v1alpha1.VNode{
		ObjectMeta: metav1.ObjectMeta{Name: "n1", Namespace: "ns"},
		Status:     v1alpha1.VNodeStatus{Phase: "NotReady"},
	}
	new := old.DeepCopy()
	new.Status.Conditions = []metav1.Condition{{Type: "Registered", Status: metav1.ConditionTrue}}

	if pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: new}) {
		t.Fatal("expected same-phase VNode update to be ignored")
	}
}

func TestVNodePhaseChangedPredicateTriggersOnPhaseChange(t *testing.T) {
	pred := vnodePhaseChangedPredicate()

	old := &v1alpha1.VNode{
		ObjectMeta: metav1.ObjectMeta{Name: "n1", Namespace: "ns"},
		Status:     v1alpha1.VNodeStatus{Phase: "NotReady"},
	}
	new := old.DeepCopy()
	new.Status.Phase = "Ready"

	if !pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: new}) {
		t.Fatal("expected phase-changing VNode update to trigger reconcile")
	}
}

func TestVNodePoolPredicatesAllowGenerationChanges(t *testing.T) {
	predicate := vnodePoolPredicates()

	oldObj := &v1alpha1.VNodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "pool-a",
			Namespace:  "default",
			Generation: 1,
		},
	}
	newObj := oldObj.DeepCopy()
	newObj.Generation = 2
	newObj.Spec.NodeCount = 3

	if !predicate.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
		t.Fatal("expected generation-changing VNodePool update to be processed")
	}
}
