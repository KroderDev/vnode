package service

import (
	"context"
	"fmt"

	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/ports"
)

// NodeService implements ports.NodeLifecycle.
type NodeService struct {
	nodeRepo  ports.NodeRepository
	registrar ports.NodeRegistrar
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
		Name:        fmt.Sprintf("%s-%d", pool.Name, len(existing)+1),
		Namespace:   pool.Namespace,
		PoolName:    pool.Name,
		TenantRef:   pool.TenantRef,
		Taints:      pool.Taints,
		Phase:       model.NodePhasePending,
		Capacity:    pool.PerNodeResources,
		Allocatable: pool.PerNodeResources,
		Conditions: []model.NodeCondition{
			{Type: model.NodeConditionReady, Status: false, Reason: "Registering", Message: "Waiting for tenant cluster node registration"},
		},
	}

	if err := s.nodeRepo.Save(ctx, node); err != nil {
		return model.VNode{}, fmt.Errorf("saving node %s: %w", node.Name, err)
	}

	if err := s.registrar.Register(ctx, node, pool.TenantRef); err != nil {
		node.Phase = model.NodePhaseNotReady
		node.Conditions = []model.NodeCondition{
			{Type: model.NodeConditionKubeconfig, Status: false, Reason: "RegistrationFailed", Message: err.Error()},
			{Type: model.NodeConditionRegistered, Status: false, Reason: "RegistrationFailed", Message: err.Error()},
			{Type: model.NodeConditionReady, Status: false, Reason: "RegistrationFailed", Message: err.Error()},
			{Type: model.NodeConditionDegraded, Status: true, Reason: "RegistrationFailed", Message: err.Error()},
		}
		_ = s.nodeRepo.Save(ctx, node)
		return node, fmt.Errorf("registering node %s in vcluster: %w", node.Name, err)
	}

	node.Phase = model.NodePhaseReady
	node.Conditions = []model.NodeCondition{
		{Type: model.NodeConditionKubeconfig, Status: true, Reason: "KubeconfigResolved", Message: "Tenant kubeconfig resolved"},
		{Type: model.NodeConditionRegistered, Status: true, Reason: "RegistrationSucceeded", Message: "Node registered in tenant cluster"},
		{Type: model.NodeConditionLease, Status: true, Reason: "LeaseActive", Message: "Node lease is active"},
		{Type: model.NodeConditionReady, Status: true, Reason: "Ready", Message: "Node is ready"},
		{Type: model.NodeConditionDegraded, Status: false, Reason: "Healthy", Message: "Node registration is healthy"},
	}

	if err := s.nodeRepo.Save(ctx, node); err != nil {
		return node, fmt.Errorf("updating node %s status: %w", node.Name, err)
	}

	return node, nil
}

func (s *NodeService) Deprovision(ctx context.Context, node model.VNode) error {
	node.Phase = model.NodePhaseTerminating
	node.Conditions = []model.NodeCondition{
		{Type: model.NodeConditionReady, Status: false, Reason: "Terminating", Message: "Node is being removed"},
	}
	if err := s.nodeRepo.Save(ctx, node); err != nil {
		return fmt.Errorf("marking node %s as terminating: %w", node.Name, err)
	}

	if err := s.registrar.Deregister(ctx, node, node.TenantRef); err != nil {
		return fmt.Errorf("deregistering node %s: %w", node.Name, err)
	}

	if err := s.nodeRepo.Delete(ctx, node.Namespace, node.Name); err != nil {
		return fmt.Errorf("deleting node %s: %w", node.Name, err)
	}

	return nil
}

func (s *NodeService) UpdateStatus(ctx context.Context, node model.VNode) error {
	if err := s.registrar.UpdateNodeStatus(ctx, node, node.TenantRef); err != nil {
		node.Phase = model.NodePhaseNotReady
		node.Conditions = []model.NodeCondition{
			{Type: model.NodeConditionReady, Status: false, Reason: "StatusSyncFailed", Message: err.Error()},
			{Type: model.NodeConditionDegraded, Status: true, Reason: "StatusSyncFailed", Message: err.Error()},
		}
		if saveErr := s.nodeRepo.Save(ctx, node); saveErr != nil {
			return fmt.Errorf("updating node %s status after sync failure: %w", node.Name, saveErr)
		}
		return fmt.Errorf("syncing tenant node status for %s: %w", node.Name, err)
	}
	return s.nodeRepo.Save(ctx, node)
}
