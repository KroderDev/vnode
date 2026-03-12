package service

import (
	"context"
	"fmt"

	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/ports"
)

// NodeService implements ports.NodeLifecycle.
type NodeService struct {
	nodeRepo   ports.NodeRepository
	registrar  ports.NodeRegistrar
}

func NewNodeService(nodeRepo ports.NodeRepository, registrar ports.NodeRegistrar) *NodeService {
	return &NodeService{
		nodeRepo:  nodeRepo,
		registrar: registrar,
	}
}

func (s *NodeService) Provision(ctx context.Context, pool model.VNodePool) (model.VNode, error) {
	// Determine next node index
	existing, err := s.nodeRepo.ListByPool(ctx, pool.Namespace, pool.Name)
	if err != nil {
		return model.VNode{}, fmt.Errorf("listing existing nodes: %w", err)
	}

	node := model.VNode{
		Name:      fmt.Sprintf("%s-%d", pool.Name, len(existing)+1),
		Namespace: pool.Namespace,
		PoolName:  pool.Name,
		Phase:     model.NodePhasePending,
		Capacity:  pool.PerNodeResources,
		Allocatable: pool.PerNodeResources,
	}

	if err := s.nodeRepo.Save(ctx, node); err != nil {
		return model.VNode{}, fmt.Errorf("saving node %s: %w", node.Name, err)
	}

	if err := s.registrar.Register(ctx, node, pool.TenantRef); err != nil {
		return node, fmt.Errorf("registering node %s in vcluster: %w", node.Name, err)
	}

	node.Phase = model.NodePhaseReady
	node.Conditions = []model.NodeCondition{
		{Type: model.NodeConditionRegistered, Status: true, Reason: "Registered", Message: "Node registered in vcluster"},
		{Type: model.NodeConditionReady, Status: true, Reason: "Ready", Message: "Node is ready"},
	}

	if err := s.nodeRepo.Save(ctx, node); err != nil {
		return node, fmt.Errorf("updating node %s status: %w", node.Name, err)
	}

	return node, nil
}

func (s *NodeService) Deprovision(ctx context.Context, node model.VNode) error {
	node.Phase = model.NodePhaseTerminating
	if err := s.nodeRepo.Save(ctx, node); err != nil {
		return fmt.Errorf("marking node %s as terminating: %w", node.Name, err)
	}

	if err := s.registrar.Deregister(ctx, node); err != nil {
		return fmt.Errorf("deregistering node %s: %w", node.Name, err)
	}

	if err := s.nodeRepo.Delete(ctx, node.Namespace, node.Name); err != nil {
		return fmt.Errorf("deleting node %s: %w", node.Name, err)
	}

	return nil
}

func (s *NodeService) UpdateStatus(ctx context.Context, node model.VNode) error {
	return s.nodeRepo.Save(ctx, node)
}
