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
	working := append([]model.VNode(nil), existing...)

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
		working = append(working, node)
	}

	// Scale down: deprovision excess nodes (remove from the end)
	for i := int32(0); i < toRemove; i++ {
		idx := len(working) - 1
		if idx < 0 {
			break
		}
		if err := s.nodeSvc.Deprovision(ctx, working[idx]); err != nil {
			pool.Phase = model.PoolPhaseFailed
			return pool, fmt.Errorf("deprovisioning node %s: %w", working[idx].Name, err)
		}
		working = working[:idx]
	}

	readyCount := int32(0)
	for _, n := range working {
		if n.Phase != model.NodePhaseNotReady && n.Phase != model.NodePhaseTerminating {
			readyCount++
		}
	}
	pool.ReadyNodes = readyCount

	if readyCount == pool.NodeCount {
		pool.Phase = model.PoolPhaseReady
	} else {
		pool.Phase = model.PoolPhaseScaling
	}

	return pool, nil
}
