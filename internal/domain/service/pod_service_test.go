package service_test

import (
	"context"
	"testing"

	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/service"
)

// fakeRuntime implements ports.IsolationRuntime for testing.
type fakeRuntime struct {
	className string
}

func (r *fakeRuntime) RuntimeClassName() string    { return r.className }
func (r *fakeRuntime) Validate(_ context.Context) error { return nil }

func TestPodService_Translate_Success(t *testing.T) {
	rt := &fakeRuntime{className: "kata"}
	svc := service.NewPodService(rt)

	pod := model.PodSpec{
		Name:      "my-app",
		Namespace: "tenant-ns",
		Containers: []model.Container{
			{Name: "main", Image: "nginx"},
		},
	}
	pool := model.VNodePool{
		Name:      "pool-a",
		Namespace: "host-ns",
	}

	result, err := svc.Translate(context.Background(), pod, pool, "vnode-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TargetPod.RuntimeClassName != "kata" {
		t.Errorf("expected runtimeClass kata, got %s", result.TargetPod.RuntimeClassName)
	}
	if result.TargetPod.Name != "vnode-1-tenant-ns-my-app" {
		t.Errorf("expected translated name, got %s", result.TargetPod.Name)
	}
	if result.TargetPod.Namespace != "host-ns" {
		t.Errorf("expected host namespace, got %s", result.TargetPod.Namespace)
	}
	if result.SourcePod.Name != "my-app" {
		t.Error("source pod not preserved in result")
	}
}

func TestPodService_Translate_UsesRuntimeClassName(t *testing.T) {
	rt := &fakeRuntime{className: "gvisor"}
	svc := service.NewPodService(rt)

	pod := model.PodSpec{Name: "app", Namespace: "ns"}
	pool := model.VNodePool{Name: "pool", Namespace: "host-ns"}

	result, err := svc.Translate(context.Background(), pod, pool, "vn-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TargetPod.RuntimeClassName != "gvisor" {
		t.Errorf("expected runtimeClass gvisor, got %s", result.TargetPod.RuntimeClassName)
	}
}

func TestPodService_Translate_PoolNameAsLabel(t *testing.T) {
	rt := &fakeRuntime{className: "kata"}
	svc := service.NewPodService(rt)

	pod := model.PodSpec{Name: "app", Namespace: "ns"}
	pool := model.VNodePool{Name: "premium-pool", Namespace: "host-ns"}

	result, err := svc.Translate(context.Background(), pod, pool, "vn-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TargetPod.Labels[model.LabelVNodePool] != "premium-pool" {
		t.Errorf("expected pool label premium-pool, got %s", result.TargetPod.Labels[model.LabelVNodePool])
	}
}

func TestPodService_SyncStatus_Passthrough(t *testing.T) {
	rt := &fakeRuntime{className: "kata"}
	svc := service.NewPodService(rt)

	status := model.PodStatus{
		Phase: "Running",
		PodIP: "10.0.0.5",
		ContainerStatuses: []model.ContainerStatus{
			{Name: "main", Ready: true, RestartCount: 2, State: "running"},
		},
		Message: "all good",
		Reason:  "Started",
	}

	result, err := svc.SyncStatus(context.Background(), status)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Phase != "Running" {
		t.Errorf("expected phase Running, got %s", result.Phase)
	}
	if result.PodIP != "10.0.0.5" {
		t.Errorf("expected PodIP 10.0.0.5, got %s", result.PodIP)
	}
	if len(result.ContainerStatuses) != 1 {
		t.Fatalf("expected 1 container status, got %d", len(result.ContainerStatuses))
	}
	if result.ContainerStatuses[0].RestartCount != 2 {
		t.Errorf("expected restartCount 2, got %d", result.ContainerStatuses[0].RestartCount)
	}
	if result.Message != "all good" {
		t.Errorf("expected message preserved, got %s", result.Message)
	}
}

func TestPodService_SyncStatus_EmptyStatus(t *testing.T) {
	rt := &fakeRuntime{className: "kata"}
	svc := service.NewPodService(rt)

	result, err := svc.SyncStatus(context.Background(), model.PodStatus{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Phase != "" {
		t.Errorf("expected empty phase, got %s", result.Phase)
	}
}
