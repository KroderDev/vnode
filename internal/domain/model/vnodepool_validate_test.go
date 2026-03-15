package model_test

import (
	"testing"

	"github.com/kroderdev/vnode/internal/domain/model"
)

func TestVNodePool_Validate_Success_Shared(t *testing.T) {
	pool := model.VNodePool{
		Name:      "pool-a",
		Namespace: "tenant-a",
		Mode:      model.PoolModeShared,
		TenantRef: model.TenantRef{KubeconfigSecret: "secret", VClusterNamespace: "tenant-a"},
	}
	if err := pool.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVNodePool_Validate_Success_Dedicated(t *testing.T) {
	pool := model.VNodePool{
		Name:         "pool-b",
		Namespace:    "tenant-a",
		Mode:         model.PoolModeDedicated,
		TenantRef:    model.TenantRef{KubeconfigSecret: "secret", VClusterNamespace: "tenant-a"},
		NodeSelector: map[string]string{"tenant": "a"},
	}
	if err := pool.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVNodePool_Validate_Success_Burstable(t *testing.T) {
	pool := model.VNodePool{
		Name:      "pool-c",
		Namespace: "tenant-a",
		Mode:      model.PoolModeBurstable,
		TenantRef: model.TenantRef{KubeconfigSecret: "secret", VClusterNamespace: "tenant-a"},
	}
	if err := pool.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVNodePool_Validate_Success_EmptyMode(t *testing.T) {
	pool := model.VNodePool{
		Name:      "pool",
		TenantRef: model.TenantRef{KubeconfigSecret: "secret"},
	}
	if err := pool.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVNodePool_Validate_EmptyName(t *testing.T) {
	pool := model.VNodePool{
		TenantRef: model.TenantRef{KubeconfigSecret: "secret"},
	}
	if err := pool.Validate(); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestVNodePool_Validate_MissingKubeconfigSecret(t *testing.T) {
	pool := model.VNodePool{
		Name: "pool",
	}
	if err := pool.Validate(); err == nil {
		t.Fatal("expected error for missing kubeconfigSecret")
	}
}

func TestVNodePool_Validate_NegativeNodeCount(t *testing.T) {
	pool := model.VNodePool{
		Name:      "pool",
		Namespace: "tenant-a",
		NodeCount: -1,
		TenantRef: model.TenantRef{KubeconfigSecret: "secret", VClusterNamespace: "tenant-a"},
	}
	if err := pool.Validate(); err == nil {
		t.Fatal("expected error for negative nodeCount")
	}
}

func TestVNodePool_Validate_DedicatedWithoutNodeSelector(t *testing.T) {
	pool := model.VNodePool{
		Name:      "pool",
		Namespace: "tenant-a",
		Mode:      model.PoolModeDedicated,
		TenantRef: model.TenantRef{KubeconfigSecret: "secret", VClusterNamespace: "tenant-a"},
	}
	if err := pool.Validate(); err == nil {
		t.Fatal("expected error for dedicated mode without nodeSelector")
	}
}

func TestVNodePool_Validate_InvalidMode(t *testing.T) {
	pool := model.VNodePool{
		Name:      "pool",
		Namespace: "tenant-a",
		Mode:      "invalid",
		TenantRef: model.TenantRef{KubeconfigSecret: "secret", VClusterNamespace: "tenant-a"},
	}
	if err := pool.Validate(); err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestVNodePool_Validate_RejectsCrossNamespaceTenantRef(t *testing.T) {
	pool := model.VNodePool{
		Name:      "pool",
		Namespace: "tenant-a",
		TenantRef: model.TenantRef{
			KubeconfigSecret:  "secret",
			VClusterNamespace: "tenant-b",
		},
	}
	if err := pool.Validate(); err == nil {
		t.Fatal("expected error for cross-namespace tenantRef")
	}
}
