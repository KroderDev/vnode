package provider

import (
	"context"
	"log/slog"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kroderdev/vnode/internal/translator"
)

func newTestProvider(objects ...runtime.Object) *Provider {
	client := fake.NewSimpleClientset(objects...)
	trans := translator.New("host-ns", "kata", "vnode-01")
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return New(log, client, trans, "host-ns")
}

func testPod(ns, name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", Image: "nginx:latest"},
			},
		},
	}
}

func TestCreatePod(t *testing.T) {
	p := newTestProvider()
	ctx := context.Background()

	pod := testPod("default", "nginx")
	if err := p.CreatePod(ctx, pod); err != nil {
		t.Fatalf("CreatePod: %v", err)
	}

	// Verify host pod was created
	hostPod, err := p.hostClient.CoreV1().Pods("host-ns").Get(ctx, "vnode-01-default-nginx", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("host pod not found: %v", err)
	}
	if hostPod.Spec.RuntimeClassName == nil || *hostPod.Spec.RuntimeClassName != "kata" {
		t.Error("expected RuntimeClassName kata")
	}

	// Verify pod is tracked
	got, err := p.GetPod(ctx, "default", "nginx")
	if err != nil {
		t.Fatalf("GetPod: %v", err)
	}
	if got == nil {
		t.Fatal("expected pod to be tracked")
	}
}

func TestCreatePodIdempotent(t *testing.T) {
	p := newTestProvider()
	ctx := context.Background()

	pod := testPod("default", "nginx")
	if err := p.CreatePod(ctx, pod); err != nil {
		t.Fatalf("first CreatePod: %v", err)
	}
	if err := p.CreatePod(ctx, pod); err != nil {
		t.Fatalf("second CreatePod should not error: %v", err)
	}
}

func TestDeletePod(t *testing.T) {
	p := newTestProvider()
	ctx := context.Background()

	pod := testPod("default", "nginx")
	if err := p.CreatePod(ctx, pod); err != nil {
		t.Fatalf("CreatePod: %v", err)
	}

	if err := p.DeletePod(ctx, pod); err != nil {
		t.Fatalf("DeletePod: %v", err)
	}

	// Verify pod is removed from tracking
	got, err := p.GetPod(ctx, "default", "nginx")
	if err != nil {
		t.Fatalf("GetPod: %v", err)
	}
	if got != nil {
		t.Error("expected pod to be removed from tracking")
	}
}

func TestDeletePodNotFound(t *testing.T) {
	p := newTestProvider()
	ctx := context.Background()

	pod := testPod("default", "nonexistent")
	if err := p.DeletePod(ctx, pod); err != nil {
		t.Fatalf("DeletePod of nonexistent should not error: %v", err)
	}
}

func TestGetPods(t *testing.T) {
	p := newTestProvider()
	ctx := context.Background()

	pod1 := testPod("default", "nginx-1")
	pod2 := testPod("default", "nginx-2")
	_ = p.CreatePod(ctx, pod1)
	_ = p.CreatePod(ctx, pod2)

	pods, err := p.GetPods(ctx)
	if err != nil {
		t.Fatalf("GetPods: %v", err)
	}
	if len(pods) != 2 {
		t.Errorf("expected 2 pods, got %d", len(pods))
	}
}

func TestGetPodNotFound(t *testing.T) {
	p := newTestProvider()
	ctx := context.Background()

	got, err := p.GetPod(ctx, "default", "nonexistent")
	if err != nil {
		t.Fatalf("GetPod: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent pod")
	}
}
