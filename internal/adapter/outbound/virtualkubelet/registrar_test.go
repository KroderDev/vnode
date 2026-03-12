package virtualkubelet_test

import (
	"context"
	"testing"

	"github.com/kroderdev/vnode/internal/adapter/outbound/virtualkubelet"
	"github.com/kroderdev/vnode/internal/domain/model"
)

func TestRegistrar_Register_Success(t *testing.T) {
	reg := virtualkubelet.NewRegistrar()
	node := model.VNode{Name: "pool-1"}
	tenant := model.TenantRef{VClusterNamespace: "tenant-ns"}

	err := reg.Register(context.Background(), node, tenant)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reg.IsRegistered("pool-1") {
		t.Error("expected node to be registered")
	}
}

func TestRegistrar_Register_MultipleNodes(t *testing.T) {
	reg := virtualkubelet.NewRegistrar()
	tenant := model.TenantRef{VClusterNamespace: "ns"}

	for _, name := range []string{"node-1", "node-2", "node-3"} {
		if err := reg.Register(context.Background(), model.VNode{Name: name}, tenant); err != nil {
			t.Fatalf("unexpected error registering %s: %v", name, err)
		}
	}

	if reg.RegisteredCount() != 3 {
		t.Errorf("expected 3 registered, got %d", reg.RegisteredCount())
	}
	for _, name := range []string{"node-1", "node-2", "node-3"} {
		if !reg.IsRegistered(name) {
			t.Errorf("expected %s to be registered", name)
		}
	}
}

func TestRegistrar_Register_Idempotent(t *testing.T) {
	reg := virtualkubelet.NewRegistrar()
	node := model.VNode{Name: "node-1"}
	tenant := model.TenantRef{VClusterNamespace: "ns"}

	_ = reg.Register(context.Background(), node, tenant)
	_ = reg.Register(context.Background(), node, tenant)

	if reg.RegisteredCount() != 1 {
		t.Errorf("expected 1 registered after double register, got %d", reg.RegisteredCount())
	}
}

func TestRegistrar_Deregister_Success(t *testing.T) {
	reg := virtualkubelet.NewRegistrar()
	node := model.VNode{Name: "node-1"}
	tenant := model.TenantRef{VClusterNamespace: "ns"}

	_ = reg.Register(context.Background(), node, tenant)
	err := reg.Deregister(context.Background(), node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg.IsRegistered("node-1") {
		t.Error("expected node to be deregistered")
	}
	if reg.RegisteredCount() != 0 {
		t.Errorf("expected 0 registered, got %d", reg.RegisteredCount())
	}
}

func TestRegistrar_Deregister_NonExistent(t *testing.T) {
	reg := virtualkubelet.NewRegistrar()
	err := reg.Deregister(context.Background(), model.VNode{Name: "ghost"})
	if err != nil {
		t.Fatalf("expected no error for non-existent node, got: %v", err)
	}
}

func TestRegistrar_Deregister_OnlyRemovesCorrectNode(t *testing.T) {
	reg := virtualkubelet.NewRegistrar()
	tenant := model.TenantRef{VClusterNamespace: "ns"}

	_ = reg.Register(context.Background(), model.VNode{Name: "node-1"}, tenant)
	_ = reg.Register(context.Background(), model.VNode{Name: "node-2"}, tenant)
	_ = reg.Register(context.Background(), model.VNode{Name: "node-10"}, tenant)

	_ = reg.Deregister(context.Background(), model.VNode{Name: "node-1"})

	if reg.IsRegistered("node-1") {
		t.Error("node-1 should be deregistered")
	}
	if !reg.IsRegistered("node-2") {
		t.Error("node-2 should still be registered")
	}
	if !reg.IsRegistered("node-10") {
		t.Error("node-10 should still be registered (not a suffix match victim)")
	}
	if reg.RegisteredCount() != 2 {
		t.Errorf("expected 2 registered, got %d", reg.RegisteredCount())
	}
}

func TestRegistrar_Deregister_DoesNotFalseMatchSuffix(t *testing.T) {
	// Regression test: "node-1" should NOT match "other-node-1"
	reg := virtualkubelet.NewRegistrar()
	tenant := model.TenantRef{VClusterNamespace: "ns"}

	_ = reg.Register(context.Background(), model.VNode{Name: "other-node-1"}, tenant)

	// Deregister "node-1" should NOT remove "other-node-1"
	// because the key is "ns/other-node-1" and we look for "/node-1" suffix
	// Actually "ns/other-node-1" does end with "/node-1"... wait, no.
	// The key is "ns/other-node-1", suffix is "/node-1"
	// "ns/other-node-1" ends with "-node-1", not "/node-1" ✓
	_ = reg.Deregister(context.Background(), model.VNode{Name: "node-1"})

	// other-node-1 should NOT have been removed
	if !reg.IsRegistered("other-node-1") {
		t.Error("other-node-1 should not have been deregistered by node-1")
	}
}

func TestRegistrar_UpdateNodeStatus_Noop(t *testing.T) {
	reg := virtualkubelet.NewRegistrar()
	err := reg.UpdateNodeStatus(context.Background(), model.VNode{Name: "any"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegistrar_IsRegistered_NotFound(t *testing.T) {
	reg := virtualkubelet.NewRegistrar()
	if reg.IsRegistered("nonexistent") {
		t.Error("expected false for non-registered node")
	}
}

func TestRegistrar_RegisteredCount_Empty(t *testing.T) {
	reg := virtualkubelet.NewRegistrar()
	if reg.RegisteredCount() != 0 {
		t.Errorf("expected 0, got %d", reg.RegisteredCount())
	}
}
