package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/ports"
)

// NodeService implements ports.NodeLifecycle.
type NodeService struct {
	log       *slog.Logger
	nodeRepo  ports.NodeRepository
	registrar ports.NodeRegistrar
}

func NewNodeService(log *slog.Logger, nodeRepo ports.NodeRepository, registrar ports.NodeRegistrar) *NodeService {
	return &NodeService{
		log:       log,
		nodeRepo:  nodeRepo,
		registrar: registrar,
	}
}

func (s *NodeService) logDeregisterWarning(node model.VNode, err error) {
	s.log.Warn("best-effort deregistration failed (vCluster may be gone)", "node", node.Name, "error", err)
}

func (s *NodeService) Provision(ctx context.Context, pool model.VNodePool) (model.VNode, error) {
	// Determine next node index
	existing, err := s.nodeRepo.ListByPool(ctx, pool.Namespace, pool.Name)
	if err != nil {
		return model.VNode{}, fmt.Errorf("listing existing nodes: %w", err)
	}

	displayName := pool.DisplayName
	if displayName == "" {
		displayName = pool.Name
	}

	node := model.VNode{
		Name:        fmt.Sprintf("vnode-%s-%d", displayName, len(existing)+1),
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
	node.Conditions = readyNodeConditions()

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

	// Best-effort deregistration: if the vCluster is already gone or the
	// kubeconfig secret was deleted, the tenant-side node no longer exists
	// anyway. Log the error but proceed with VNode CR deletion so the
	// finalizer can be removed and the pool can be cleaned up.
	if err := s.registrar.Deregister(ctx, node, node.TenantRef); err != nil {
		s.logDeregisterWarning(node, err)
	}

	if err := s.nodeRepo.Delete(ctx, node.Namespace, node.Name); err != nil {
		return fmt.Errorf("deleting node %s: %w", node.Name, err)
	}

	return nil
}

func (s *NodeService) UpdateStatus(ctx context.Context, node model.VNode) error {
	if node.Phase == "" && len(node.Conditions) == 0 {
		return nil
	}
	// A transient vcluster startup timeout can leave the VNode CR in NotReady before the
	// tenant-side Node object ever exists. In that case, UpdateNodeStatus can never heal
	// the node because there is nothing to update yet, so we must retry full registration.
	if shouldRetryRegistration(node) {
		if err := s.registrar.Register(ctx, node, node.TenantRef); err != nil {
			if isIgnorableStatusError(err) {
				return nil
			}
			node.Phase = model.NodePhaseNotReady
			node.Conditions = registrationFailedConditions(err)
			if saveErr := s.nodeRepo.Save(ctx, node); saveErr != nil {
				if isIgnorableStatusError(saveErr) {
					return nil
				}
				return fmt.Errorf("updating node %s status after registration retry failure: %w", node.Name, saveErr)
			}
			return fmt.Errorf("retrying tenant node registration for %s: %w", node.Name, err)
		}
		node.Phase = model.NodePhaseReady
		node.Conditions = readyNodeConditions()
		if err := s.nodeRepo.Save(ctx, node); err != nil {
			if isIgnorableStatusError(err) {
				return nil
			}
			return err
		}
		return nil
	}
	if err := s.registrar.UpdateNodeStatus(ctx, node, node.TenantRef); err != nil {
		if isIgnorableStatusError(err) {
			return nil
		}
		node.Phase = model.NodePhaseNotReady
		node.Conditions = []model.NodeCondition{
			{Type: model.NodeConditionReady, Status: false, Reason: "StatusSyncFailed", Message: err.Error()},
			{Type: model.NodeConditionDegraded, Status: true, Reason: "StatusSyncFailed", Message: err.Error()},
		}
		if saveErr := s.nodeRepo.Save(ctx, node); saveErr != nil {
			if isIgnorableStatusError(saveErr) {
				return nil
			}
			return fmt.Errorf("updating node %s status after sync failure: %w", node.Name, saveErr)
		}
		return fmt.Errorf("syncing tenant node status for %s: %w", node.Name, err)
	}
	if node.Phase == model.NodePhaseReady && node.IsReady() {
		// Nothing changed — skip save to avoid triggering a redundant reconcile.
		return nil
	}
	node.Phase = model.NodePhaseReady
	node.Conditions = readyNodeConditions()
	if err := s.nodeRepo.Save(ctx, node); err != nil {
		if isIgnorableStatusError(err) {
			return nil
		}
		return err
	}
	return nil
}

func readyNodeConditions() []model.NodeCondition {
	return []model.NodeCondition{
		{Type: model.NodeConditionKubeconfig, Status: true, Reason: "KubeconfigResolved", Message: "Tenant kubeconfig resolved"},
		{Type: model.NodeConditionRegistered, Status: true, Reason: "RegistrationSucceeded", Message: "Node registered in tenant cluster"},
		{Type: model.NodeConditionLease, Status: true, Reason: "LeaseActive", Message: "Node lease is active"},
		{Type: model.NodeConditionReady, Status: true, Reason: "Ready", Message: "Node is ready"},
		{Type: model.NodeConditionDegraded, Status: false, Reason: "Healthy", Message: "Node registration is healthy"},
	}
}

func registrationFailedConditions(err error) []model.NodeCondition {
	return []model.NodeCondition{
		{Type: model.NodeConditionKubeconfig, Status: false, Reason: "RegistrationFailed", Message: err.Error()},
		{Type: model.NodeConditionRegistered, Status: false, Reason: "RegistrationFailed", Message: err.Error()},
		{Type: model.NodeConditionReady, Status: false, Reason: "RegistrationFailed", Message: err.Error()},
		{Type: model.NodeConditionDegraded, Status: true, Reason: "RegistrationFailed", Message: err.Error()},
	}
}

func shouldRetryRegistration(node model.VNode) bool {
	if node.Phase != model.NodePhasePending && node.Phase != model.NodePhaseNotReady {
		return false
	}
	for _, condition := range node.Conditions {
		if condition.Type != model.NodeConditionRegistered {
			continue
		}
		return !condition.Status
	}
	return false
}

func isIgnorableStatusError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
