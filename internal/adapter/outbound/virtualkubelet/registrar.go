package virtualkubelet

import (
	"context"
	"fmt"

	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/ports"
)

// Ensure interface compliance at compile time.
var _ ports.NodeRegistrar = (*Registrar)(nil)

// Registrar implements ports.NodeRegistrar.
// In the MVP, this is a stub that will be backed by Virtual Kubelet in Phase 2.
type Registrar struct {
	// registered tracks which nodes have been registered (in-memory for MVP).
	registered map[string]bool
}

func NewRegistrar() *Registrar {
	return &Registrar{
		registered: make(map[string]bool),
	}
}

func (r *Registrar) Register(_ context.Context, node model.VNode, tenant model.TenantRef) error {
	key := fmt.Sprintf("%s/%s", tenant.VClusterNamespace, node.Name)
	r.registered[key] = true
	return nil
}

func (r *Registrar) Deregister(_ context.Context, node model.VNode) error {
	// Try all possible keys since we may not know the tenant namespace at deregister time.
	for k := range r.registered {
		if k == node.Name || hasSuffix(k, "/"+node.Name) {
			delete(r.registered, k)
			return nil
		}
	}
	return nil
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// IsRegistered returns true if the given node name is currently registered.
func (r *Registrar) IsRegistered(name string) bool {
	for k := range r.registered {
		if k == name || hasSuffix(k, "/"+name) {
			return true
		}
	}
	return false
}

// RegisteredCount returns the number of currently registered nodes.
func (r *Registrar) RegisteredCount() int {
	return len(r.registered)
}

func (r *Registrar) UpdateNodeStatus(_ context.Context, _ model.VNode) error {
	return nil
}
