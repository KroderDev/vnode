package reconciler

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetStatusConditionNoOpPreservesTransitionTime(t *testing.T) {
	ts := metav1.NewTime(time.Unix(123, 0))
	conditions := []metav1.Condition{{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "PoolScaling",
		Message:            "0/3 virtual nodes ready",
		LastTransitionTime: ts,
	}}

	setStatusCondition(&conditions, "Ready", metav1.ConditionFalse, "PoolScaling", "0/3 virtual nodes ready")

	if got := conditions[0].LastTransitionTime; !got.Equal(&ts) {
		t.Fatalf("expected transition time to remain unchanged, got %v want %v", got, ts)
	}
}

func TestSetStatusConditionUpdatesTransitionTimeOnChange(t *testing.T) {
	ts := metav1.NewTime(time.Unix(123, 0))
	conditions := []metav1.Condition{{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "PoolScaling",
		Message:            "0/3 virtual nodes ready",
		LastTransitionTime: ts,
	}}

	setStatusCondition(&conditions, "Ready", metav1.ConditionTrue, "PoolReady", "3/3 virtual nodes ready")

	if conditions[0].Status != metav1.ConditionTrue {
		t.Fatalf("expected status to update to true, got %s", conditions[0].Status)
	}
	if !conditions[0].LastTransitionTime.After(ts.Time) {
		t.Fatalf("expected transition time to move forward, got %v want after %v", conditions[0].LastTransitionTime, ts)
	}
}

func TestSetPodSyncConditionNoOpPreservesTransitionTime(t *testing.T) {
	ts := metav1.NewTime(time.Unix(123, 0))
	conditions := []metav1.Condition{{
		Type:               "PodExecutionReady",
		Status:             metav1.ConditionTrue,
		Reason:             "PodExecutionReady",
		Message:            "Processed 0 tenant pods",
		LastTransitionTime: ts,
	}}

	setPodSyncCondition(&conditions, "PodExecutionReady", metav1.ConditionTrue, "PodExecutionReady", "Processed 0 tenant pods")

	if got := conditions[0].LastTransitionTime; !got.Equal(&ts) {
		t.Fatalf("expected transition time to remain unchanged, got %v want %v", got, ts)
	}
}

func TestSetPodSyncConditionUpdatesTransitionTimeOnChange(t *testing.T) {
	ts := metav1.NewTime(time.Unix(123, 0))
	conditions := []metav1.Condition{{
		Type:               "PodExecutionReady",
		Status:             metav1.ConditionTrue,
		Reason:             "PodExecutionReady",
		Message:            "Processed 0 tenant pods",
		LastTransitionTime: ts,
	}}

	setPodSyncCondition(&conditions, "PodExecutionReady", metav1.ConditionFalse, "PodExecutionFailed", "tenant sync failed")

	if conditions[0].Status != metav1.ConditionFalse {
		t.Fatalf("expected status to update to false, got %s", conditions[0].Status)
	}
	if !conditions[0].LastTransitionTime.After(ts.Time) {
		t.Fatalf("expected transition time to move forward, got %v want after %v", conditions[0].LastTransitionTime, ts)
	}
}
