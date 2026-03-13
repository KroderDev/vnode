package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/service"
)

// fakeNodeRepo implements ports.NodeRepository for testing.
type fakeNodeRepo struct {
	nodes []model.VNode
}

func (r *fakeNodeRepo) Get(_ context.Context, namespace, name string) (*model.VNode, error) {
	for _, n := range r.nodes {
		if n.Namespace == namespace && n.Name == name {
			return &n, nil
		}
	}
	return nil, nil
}

func (r *fakeNodeRepo) Save(_ context.Context, node model.VNode) error {
	for i, n := range r.nodes {
		if n.Namespace == node.Namespace && n.Name == node.Name {
			r.nodes[i] = node
			return nil
		}
	}
	r.nodes = append(r.nodes, node)
	return nil
}

func (r *fakeNodeRepo) Delete(_ context.Context, namespace, name string) error {
	for i, n := range r.nodes {
		if n.Namespace == namespace && n.Name == name {
			r.nodes = append(r.nodes[:i], r.nodes[i+1:]...)
			return nil
		}
	}
	return nil
}

func (r *fakeNodeRepo) ListByPool(_ context.Context, namespace, poolName string) ([]model.VNode, error) {
	var result []model.VNode
	for _, n := range r.nodes {
		if n.Namespace == namespace && n.PoolName == poolName {
			result = append(result, n)
		}
	}
	return result, nil
}

// fakeNodeService implements ports.NodeLifecycle for testing.
type fakeNodeService struct {
	repo            *fakeNodeRepo
	provisionErr    error
	deprovisionErr  error
	provisionCalls  int
	deprovisionCalls int
}

func (s *fakeNodeService) Provision(_ context.Context, pool model.VNodePool) (model.VNode, error) {
	s.provisionCalls++
	if s.provisionErr != nil {
		return model.VNode{}, s.provisionErr
	}
	existing, _ := s.repo.ListByPool(context.Background(), pool.Namespace, pool.Name)
	node := model.VNode{
		Name:      pool.Name + "-" + string(rune('1'+len(existing))),
		Namespace: pool.Namespace,
		PoolName:  pool.Name,
		Phase:     model.NodePhaseReady,
		Capacity:  pool.PerNodeResources,
		Conditions: []model.NodeCondition{
			{Type: model.NodeConditionReady, Status: true},
		},
	}
	s.repo.nodes = append(s.repo.nodes, node)
	return node, nil
}

func (s *fakeNodeService) Deprovision(_ context.Context, node model.VNode) error {
	s.deprovisionCalls++
	if s.deprovisionErr != nil {
		return s.deprovisionErr
	}
	for i, n := range s.repo.nodes {
		if n.Name == node.Name {
			s.repo.nodes = append(s.repo.nodes[:i], s.repo.nodes[i+1:]...)
			return nil
		}
	}
	return nil
}

func (s *fakeNodeService) UpdateStatus(_ context.Context, _ model.VNode) error {
	return nil
}

func TestPoolService_Reconcile_ScaleUp(t *testing.T) {
	repo := &fakeNodeRepo{}
	nodeSvc := &fakeNodeService{repo: repo}
	svc := service.NewPoolService(repo, nodeSvc)

	pool := model.VNodePool{
		Name:      "test-pool",
		Namespace: "default",
		NodeCount: 3,
		PerNodeResources: model.ResourceList{CPU: "4", Memory: "8Gi", Pods: 110},
		Phase:     model.PoolPhasePending,
	}

	result, err := svc.Reconcile(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Phase != model.PoolPhaseReady {
		t.Errorf("expected phase Ready, got %s", result.Phase)
	}
	if result.ReadyNodes != 3 {
		t.Errorf("expected 3 ready nodes, got %d", result.ReadyNodes)
	}
	if len(repo.nodes) != 3 {
		t.Errorf("expected 3 nodes in repo, got %d", len(repo.nodes))
	}
}

func TestPoolService_Reconcile_ScaleDown(t *testing.T) {
	repo := &fakeNodeRepo{
		nodes: []model.VNode{
			{Name: "pool-1", Namespace: "default", PoolName: "test-pool", Phase: model.NodePhaseReady, Conditions: []model.NodeCondition{{Type: model.NodeConditionReady, Status: true}}},
			{Name: "pool-2", Namespace: "default", PoolName: "test-pool", Phase: model.NodePhaseReady, Conditions: []model.NodeCondition{{Type: model.NodeConditionReady, Status: true}}},
			{Name: "pool-3", Namespace: "default", PoolName: "test-pool", Phase: model.NodePhaseReady, Conditions: []model.NodeCondition{{Type: model.NodeConditionReady, Status: true}}},
		},
	}
	nodeSvc := &fakeNodeService{repo: repo}
	svc := service.NewPoolService(repo, nodeSvc)

	pool := model.VNodePool{
		Name:      "test-pool",
		Namespace: "default",
		NodeCount: 1,
		Phase:     model.PoolPhaseReady,
	}

	result, err := svc.Reconcile(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.nodes) != 1 {
		t.Errorf("expected 1 node in repo, got %d", len(repo.nodes))
	}
	if result.ReadyNodes != 1 {
		t.Errorf("expected 1 ready node, got %d", result.ReadyNodes)
	}
}

func TestPoolService_Reconcile_NoChange(t *testing.T) {
	repo := &fakeNodeRepo{
		nodes: []model.VNode{
			{Name: "pool-1", Namespace: "default", PoolName: "test-pool", Phase: model.NodePhaseReady, Conditions: []model.NodeCondition{{Type: model.NodeConditionReady, Status: true}}},
			{Name: "pool-2", Namespace: "default", PoolName: "test-pool", Phase: model.NodePhaseReady, Conditions: []model.NodeCondition{{Type: model.NodeConditionReady, Status: true}}},
		},
	}
	nodeSvc := &fakeNodeService{repo: repo}
	svc := service.NewPoolService(repo, nodeSvc)

	pool := model.VNodePool{
		Name:      "test-pool",
		Namespace: "default",
		NodeCount: 2,
		Phase:     model.PoolPhaseReady,
	}

	result, err := svc.Reconcile(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Phase != model.PoolPhaseReady {
		t.Errorf("expected phase Ready, got %s", result.Phase)
	}
	if len(repo.nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(repo.nodes))
	}
}

func TestPoolService_Reconcile_ProvisionError_SetsFailed(t *testing.T) {
	repo := &fakeNodeRepo{}
	nodeSvc := &fakeNodeService{
		repo:         repo,
		provisionErr: errors.New("provision boom"),
	}
	svc := service.NewPoolService(repo, nodeSvc)

	pool := model.VNodePool{
		Name: "pool", Namespace: "default", NodeCount: 2,
	}

	result, err := svc.Reconcile(context.Background(), pool)
	if err == nil {
		t.Fatal("expected error from provision failure")
	}
	if result.Phase != model.PoolPhaseFailed {
		t.Errorf("expected phase Failed, got %s", result.Phase)
	}
}

func TestPoolService_Reconcile_DeprovisionError_SetsFailed(t *testing.T) {
	repo := &fakeNodeRepo{
		nodes: []model.VNode{
			{Name: "n-1", Namespace: "default", PoolName: "pool", Conditions: []model.NodeCondition{{Type: model.NodeConditionReady, Status: true}}},
			{Name: "n-2", Namespace: "default", PoolName: "pool", Conditions: []model.NodeCondition{{Type: model.NodeConditionReady, Status: true}}},
		},
	}
	nodeSvc := &fakeNodeService{
		repo:           repo,
		deprovisionErr: errors.New("deprovision boom"),
	}
	svc := service.NewPoolService(repo, nodeSvc)

	pool := model.VNodePool{
		Name: "pool", Namespace: "default", NodeCount: 0,
	}

	result, err := svc.Reconcile(context.Background(), pool)
	if err == nil {
		t.Fatal("expected error from deprovision failure")
	}
	if result.Phase != model.PoolPhaseFailed {
		t.Errorf("expected phase Failed, got %s", result.Phase)
	}
}

func TestPoolService_Reconcile_ScaleFromZero(t *testing.T) {
	repo := &fakeNodeRepo{}
	nodeSvc := &fakeNodeService{repo: repo}
	svc := service.NewPoolService(repo, nodeSvc)

	pool := model.VNodePool{
		Name: "pool", Namespace: "default", NodeCount: 5,
		PerNodeResources: model.ResourceList{CPU: "1", Memory: "1Gi", Pods: 10},
	}

	result, err := svc.Reconcile(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nodeSvc.provisionCalls != 5 {
		t.Errorf("expected 5 provision calls, got %d", nodeSvc.provisionCalls)
	}
	if result.ReadyNodes != 5 {
		t.Errorf("expected 5 ready nodes, got %d", result.ReadyNodes)
	}
}

func TestPoolService_Reconcile_ScaleToZero(t *testing.T) {
	repo := &fakeNodeRepo{
		nodes: []model.VNode{
			{Name: "n-1", Namespace: "default", PoolName: "pool"},
			{Name: "n-2", Namespace: "default", PoolName: "pool"},
		},
	}
	nodeSvc := &fakeNodeService{repo: repo}
	svc := service.NewPoolService(repo, nodeSvc)

	pool := model.VNodePool{
		Name: "pool", Namespace: "default", NodeCount: 0,
	}

	_, err := svc.Reconcile(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nodeSvc.deprovisionCalls != 2 {
		t.Errorf("expected 2 deprovision calls, got %d", nodeSvc.deprovisionCalls)
	}
	if len(repo.nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(repo.nodes))
	}
}

func TestPoolService_Reconcile_MixedReadyNotReady(t *testing.T) {
	repo := &fakeNodeRepo{
		nodes: []model.VNode{
			{Name: "n-1", Namespace: "default", PoolName: "pool", Phase: model.NodePhaseReady, Conditions: []model.NodeCondition{{Type: model.NodeConditionReady, Status: true}}},
			{Name: "n-2", Namespace: "default", PoolName: "pool", Phase: model.NodePhaseNotReady, Conditions: []model.NodeCondition{{Type: model.NodeConditionReady, Status: false}}},
			{Name: "n-3", Namespace: "default", PoolName: "pool", Phase: model.NodePhaseTerminating},
		},
	}
	nodeSvc := &fakeNodeService{repo: repo}
	svc := service.NewPoolService(repo, nodeSvc)

	pool := model.VNodePool{
		Name: "pool", Namespace: "default", NodeCount: 3,
	}

	result, err := svc.Reconcile(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ReadyNodes != 1 {
		t.Errorf("expected 1 ready node, got %d", result.ReadyNodes)
	}
	// Not all ready, so should still be Scaling (since 1 != 3)
	if result.Phase == model.PoolPhaseReady {
		t.Error("expected pool NOT to be Ready when only 1/3 nodes are ready")
	}
}

func TestPoolService_Reconcile_NodesPopulatedOnScaleUp(t *testing.T) {
	repo := &fakeNodeRepo{}
	nodeSvc := &fakeNodeService{repo: repo}
	svc := service.NewPoolService(repo, nodeSvc)

	pool := model.VNodePool{
		Name: "pool", Namespace: "default", NodeCount: 2,
		PerNodeResources: model.ResourceList{CPU: "1", Memory: "1Gi", Pods: 10},
	}

	result, err := svc.Reconcile(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Nodes) != 2 {
		t.Errorf("expected 2 node names in pool.Nodes, got %d", len(result.Nodes))
	}
}

func TestPoolService_Reconcile_PartialProvisionFailure(t *testing.T) {
	callCount := 0
	repo := &fakeNodeRepo{}
	nodeSvc := &fakeNodeService{repo: repo}
	svc := service.NewPoolService(repo, nodeSvc)

	// Hack: make provision fail on 2nd call
	origProvision := nodeSvc.Provision
	_ = origProvision // can't easily do this with struct, so use provisionErr after 1 call

	// Instead: provision 1 node first, then set error
	pool := model.VNodePool{
		Name: "pool", Namespace: "default", NodeCount: 1,
		PerNodeResources: model.ResourceList{CPU: "1", Memory: "1Gi", Pods: 10},
	}
	_, _ = svc.Reconcile(context.Background(), pool)
	_ = callCount

	// Now try to scale to 3, but provision fails
	nodeSvc.provisionErr = errors.New("out of capacity")
	pool.NodeCount = 3
	result, err := svc.Reconcile(context.Background(), pool)
	if err == nil {
		t.Fatal("expected error on partial provision failure")
	}
	if result.Phase != model.PoolPhaseFailed {
		t.Errorf("expected Failed phase, got %s", result.Phase)
	}
	// Should still have the 1 node from before
	if len(repo.nodes) != 1 {
		t.Errorf("expected 1 node (pre-existing), got %d", len(repo.nodes))
	}
}
