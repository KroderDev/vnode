package service

import (
	"context"
	"fmt"

	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/ports"
)

// PoolService implements ports.PoolManager.
type PoolService struct {
	nodeRepo ports.NodeRepository
	nodeSvc  ports.NodeLifecycle
}

func NewPoolService(nodeRepo ports.NodeRepository, nodeSvc ports.NodeLifecycle) *PoolService {
	return &PoolService{
		nodeRepo: nodeRepo,
		nodeSvc:  nodeSvc,
	}
}

func (s *PoolService) Reconcile(ctx context.Context, pool model.VNodePool) (model.VNodePool, error) {
	existing, err := s.nodeRepo.ListByPool(ctx, pool.Namespace, pool.Name)
	if err != nil {
		return pool, fmt.Errorf("listing nodes for pool %s: %w", pool.Name, err)
	}

	currentCount := int32(len(existing))
	toAdd, toRemove := pool.DesiredScaleActions(currentCount)

	// Scale up: provision new nodes
	for i := int32(0); i < toAdd; i++ {
		node, err := s.nodeSvc.Provision(ctx, pool)
		if err != nil {
			pool.Phase = model.PoolPhaseFailed
			return pool, fmt.Errorf("provisioning node %d for pool %s: %w", i, pool.Name, err)
		}
		pool.Nodes = append(pool.Nodes, node.Name)
	}

	// Scale down: deprovision excess nodes (remove from the end)
	for i := int32(0); i < toRemove; i++ {
		idx := len(existing) - 1 - int(i)
		if idx < 0 {
			break
		}
		if err := s.nodeSvc.Deprovision(ctx, existing[idx]); err != nil {
			pool.Phase = model.PoolPhaseFailed
			return pool, fmt.Errorf("deprovisioning node %s: %w", existing[idx].Name, err)
		}
	}

	// Recount ready nodes
	readyCount := int32(0)
	nodes, _ := s.nodeRepo.ListByPool(ctx, pool.Namespace, pool.Name)
	for _, n := range nodes {
		if n.IsReady() {
			readyCount++
		}
	}
	pool.ReadyNodes = readyCount

	if readyCount == pool.NodeCount {
		pool.Phase = model.PoolPhaseReady
	} else if toAdd > 0 || toRemove > 0 {
		pool.Phase = model.PoolPhaseScaling
	}

	return pool, nil
}
