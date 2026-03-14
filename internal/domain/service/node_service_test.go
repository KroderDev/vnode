package service_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/service"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// mockNodeRepo implements ports.NodeRepository with configurable errors.
type mockNodeRepo struct {
	nodes     []model.VNode
	saveErr   error
	deleteErr error
	listErr   error
	saveCalls int
}

func (r *mockNodeRepo) Get(_ context.Context, namespace, name string) (*model.VNode, error) {
	for _, n := range r.nodes {
		if n.Namespace == namespace && n.Name == name {
			return &n, nil
		}
	}
	return nil, nil
}

func (r *mockNodeRepo) Save(_ context.Context, node model.VNode) error {
	r.saveCalls++
	if r.saveErr != nil {
		return r.saveErr
	}
	for i, n := range r.nodes {
		if n.Namespace == node.Namespace && n.Name == node.Name {
			r.nodes[i] = node
			return nil
		}
	}
	r.nodes = append(r.nodes, node)
	return nil
}

func (r *mockNodeRepo) Delete(_ context.Context, namespace, name string) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	for i, n := range r.nodes {
		if n.Namespace == namespace && n.Name == name {
			r.nodes = append(r.nodes[:i], r.nodes[i+1:]...)
			return nil
		}
	}
	return nil
}

func (r *mockNodeRepo) ListByPool(_ context.Context, namespace, poolName string) ([]model.VNode, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	var result []model.VNode
	for _, n := range r.nodes {
		if n.Namespace == namespace && n.PoolName == poolName {
			result = append(result, n)
		}
	}
	return result, nil
}

// mockRegistrar implements ports.NodeRegistrar with configurable errors.
type mockRegistrar struct {
	registerErr   error
	updateErr     error
	deregisterErr error
	registered    map[string]bool
	registerCalls int
	updateCalls   int
}

func newMockRegistrar() *mockRegistrar {
	return &mockRegistrar{registered: make(map[string]bool)}
}

func (r *mockRegistrar) Register(_ context.Context, node model.VNode, _ model.TenantRef) error {
	r.registerCalls++
	if r.registerErr != nil {
		return r.registerErr
	}
	r.registered[node.Name] = true
	return nil
}

func (r *mockRegistrar) Deregister(_ context.Context, node model.VNode, _ model.TenantRef) error {
	if r.deregisterErr != nil {
		return r.deregisterErr
	}
	delete(r.registered, node.Name)
	return nil
}

func (r *mockRegistrar) UpdateNodeStatus(_ context.Context, node model.VNode, _ model.TenantRef) error {
	r.updateCalls++
	if r.updateErr != nil {
		return r.updateErr
	}
	r.registered[node.Name] = true
	return nil
}

// --- Provision tests ---

func TestNodeService_Provision_Success(t *testing.T) {
	repo := &mockNodeRepo{}
	reg := newMockRegistrar()
	svc := service.NewNodeService(discardLogger(), repo, reg)

	pool := model.VNodePool{
		Name:             "pool-a",
		Namespace:        "default",
		PerNodeResources: model.ResourceList{CPU: "4", Memory: "8Gi", Pods: 110},
		TenantRef:        model.TenantRef{VClusterName: "vc-1", VClusterNamespace: "ns"},
	}

	node, err := svc.Provision(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if node.Name != "vnode-pool-a-1" {
		t.Errorf("expected name vnode-pool-a-1, got %s", node.Name)
	}
	if node.Namespace != "default" {
		t.Errorf("expected namespace default, got %s", node.Namespace)
	}
	if node.PoolName != "pool-a" {
		t.Errorf("expected poolName pool-a, got %s", node.PoolName)
	}
	if node.Phase != model.NodePhaseReady {
		t.Errorf("expected phase Ready, got %s", node.Phase)
	}
	if node.Capacity.CPU != "4" {
		t.Errorf("expected CPU 4, got %s", node.Capacity.CPU)
	}
	if !reg.registered[node.Name] {
		t.Error("expected node to be registered")
	}
	// Save called twice: initial + status update
	if repo.saveCalls != 2 {
		t.Errorf("expected 2 save calls, got %d", repo.saveCalls)
	}
}

func TestNodeService_Provision_SecondNode(t *testing.T) {
	repo := &mockNodeRepo{
		nodes: []model.VNode{
			{Name: "pool-a-1", Namespace: "default", PoolName: "pool-a"},
		},
	}
	reg := newMockRegistrar()
	svc := service.NewNodeService(discardLogger(), repo, reg)

	pool := model.VNodePool{
		Name:             "pool-a",
		Namespace:        "default",
		PerNodeResources: model.ResourceList{CPU: "2", Memory: "4Gi", Pods: 50},
		TenantRef:        model.TenantRef{VClusterName: "vc-1", VClusterNamespace: "ns"},
	}

	node, err := svc.Provision(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.Name != "vnode-pool-a-2" {
		t.Errorf("expected name vnode-pool-a-2, got %s", node.Name)
	}
}

func TestNodeService_Provision_Conditions(t *testing.T) {
	repo := &mockNodeRepo{}
	reg := newMockRegistrar()
	svc := service.NewNodeService(discardLogger(), repo, reg)

	pool := model.VNodePool{
		Name: "pool", Namespace: "default",
		PerNodeResources: model.ResourceList{CPU: "1", Memory: "1Gi", Pods: 10},
		TenantRef:        model.TenantRef{VClusterNamespace: "ns"},
	}

	node, err := svc.Provision(context.Background(), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(node.Conditions) != 5 {
		t.Fatalf("expected 5 conditions, got %d", len(node.Conditions))
	}

	var hasRegistered, hasReady, hasKubeconfig, hasLease bool
	for _, c := range node.Conditions {
		if c.Type == model.NodeConditionKubeconfig && c.Status {
			hasKubeconfig = true
		}
		if c.Type == model.NodeConditionRegistered && c.Status {
			hasRegistered = true
		}
		if c.Type == model.NodeConditionLease && c.Status {
			hasLease = true
		}
		if c.Type == model.NodeConditionReady && c.Status {
			hasReady = true
		}
	}
	if !hasKubeconfig {
		t.Error("missing KubeconfigResolved=true condition")
	}
	if !hasRegistered {
		t.Error("missing Registered=true condition")
	}
	if !hasLease {
		t.Error("missing LeaseActive=true condition")
	}
	if !hasReady {
		t.Error("missing Ready=true condition")
	}
}

func TestNodeService_Provision_ListError(t *testing.T) {
	repo := &mockNodeRepo{listErr: errors.New("list failed")}
	reg := newMockRegistrar()
	svc := service.NewNodeService(discardLogger(), repo, reg)

	pool := model.VNodePool{Name: "pool", Namespace: "default", TenantRef: model.TenantRef{VClusterNamespace: "ns"}}
	_, err := svc.Provision(context.Background(), pool)
	if err == nil {
		t.Fatal("expected error from ListByPool")
	}
	if !errors.Is(err, repo.listErr) {
		t.Errorf("expected wrapped list error, got: %v", err)
	}
}

func TestNodeService_Provision_SaveError(t *testing.T) {
	repo := &mockNodeRepo{saveErr: errors.New("save failed")}
	reg := newMockRegistrar()
	svc := service.NewNodeService(discardLogger(), repo, reg)

	pool := model.VNodePool{Name: "pool", Namespace: "default", PerNodeResources: model.ResourceList{}, TenantRef: model.TenantRef{VClusterNamespace: "ns"}}
	_, err := svc.Provision(context.Background(), pool)
	if err == nil {
		t.Fatal("expected error from Save")
	}
}

func TestNodeService_Provision_RegisterError(t *testing.T) {
	repo := &mockNodeRepo{}
	reg := newMockRegistrar()
	reg.registerErr = errors.New("register failed")
	svc := service.NewNodeService(discardLogger(), repo, reg)

	pool := model.VNodePool{Name: "pool", Namespace: "default", PerNodeResources: model.ResourceList{}, TenantRef: model.TenantRef{VClusterNamespace: "ns"}}
	node, err := svc.Provision(context.Background(), pool)
	if err == nil {
		t.Fatal("expected error from Register")
	}
	// Node should still be returned (partially created)
	if node.Name == "" {
		t.Error("expected node to be returned even on register error")
	}
}

// --- Deprovision tests ---

func TestNodeService_Deprovision_Success(t *testing.T) {
	node := model.VNode{Name: "pool-a-1", Namespace: "default", PoolName: "pool-a", Phase: model.NodePhaseReady}
	repo := &mockNodeRepo{nodes: []model.VNode{node}}
	reg := newMockRegistrar()
	reg.registered["pool-a-1"] = true
	svc := service.NewNodeService(discardLogger(), repo, reg)

	err := svc.Deprovision(context.Background(), node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.nodes) != 0 {
		t.Errorf("expected 0 nodes after deprovision, got %d", len(repo.nodes))
	}
	if reg.registered["pool-a-1"] {
		t.Error("expected node to be deregistered")
	}
}

func TestNodeService_Deprovision_SaveError(t *testing.T) {
	node := model.VNode{Name: "node-1", Namespace: "ns"}
	repo := &mockNodeRepo{saveErr: errors.New("save failed")}
	reg := newMockRegistrar()
	svc := service.NewNodeService(discardLogger(), repo, reg)

	err := svc.Deprovision(context.Background(), node)
	if err == nil {
		t.Fatal("expected error from Save")
	}
}

func TestNodeService_Deprovision_DeregisterError_BestEffort(t *testing.T) {
	node := model.VNode{Name: "node-1", Namespace: "ns"}
	repo := &mockNodeRepo{nodes: []model.VNode{node}}
	reg := newMockRegistrar()
	reg.deregisterErr = errors.New("deregister failed")
	svc := service.NewNodeService(discardLogger(), repo, reg)

	// Deregistration failures are best-effort — node CR should still be deleted.
	err := svc.Deprovision(context.Background(), node)
	if err != nil {
		t.Fatalf("expected best-effort deregistration to succeed, got: %v", err)
	}
}

func TestNodeService_Deprovision_DeleteError(t *testing.T) {
	node := model.VNode{Name: "node-1", Namespace: "ns"}
	repo := &mockNodeRepo{nodes: []model.VNode{node}, deleteErr: errors.New("delete failed")}
	reg := newMockRegistrar()
	svc := service.NewNodeService(discardLogger(), repo, reg)

	err := svc.Deprovision(context.Background(), node)
	if err == nil {
		t.Fatal("expected error from Delete")
	}
}

// --- UpdateStatus tests ---

func TestNodeService_UpdateStatus_Success(t *testing.T) {
	node := model.VNode{
		Name:      "node-1",
		Namespace: "ns",
		Phase:     model.NodePhaseReady,
		Conditions: []model.NodeCondition{
			{Type: model.NodeConditionReady, Status: true, Reason: "Ready", Message: "Node is ready"},
		},
	}
	repo := &mockNodeRepo{}
	reg := newMockRegistrar()
	svc := service.NewNodeService(discardLogger(), repo, reg)

	err := svc.UpdateStatus(context.Background(), node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Already-ready node skips redundant save to avoid triggering reconcile loop.
	if repo.saveCalls != 0 {
		t.Errorf("expected 0 save calls for already-ready node, got %d", repo.saveCalls)
	}
}

func TestNodeService_UpdateStatus_TransitionsToReady(t *testing.T) {
	node := model.VNode{
		Name:      "node-1",
		Namespace: "ns",
		Phase:     model.NodePhaseNotReady,
		Conditions: []model.NodeCondition{
			{Type: model.NodeConditionReady, Status: false, Reason: "StatusSyncFailed", Message: "error"},
		},
	}
	repo := &mockNodeRepo{}
	reg := newMockRegistrar()
	svc := service.NewNodeService(discardLogger(), repo, reg)

	err := svc.UpdateStatus(context.Background(), node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.saveCalls != 1 {
		t.Errorf("expected 1 save call for NotReady->Ready transition, got %d", repo.saveCalls)
	}
}

func TestNodeService_UpdateStatus_Error(t *testing.T) {
	// Use a non-ready node so that save is actually called.
	node := model.VNode{
		Name:      "node-1",
		Namespace: "ns",
		Phase:     model.NodePhaseNotReady,
		Conditions: []model.NodeCondition{
			{Type: model.NodeConditionReady, Status: false, Reason: "StatusSyncFailed", Message: "error"},
		},
	}
	repo := &mockNodeRepo{saveErr: errors.New("save failed")}
	reg := newMockRegistrar()
	svc := service.NewNodeService(discardLogger(), repo, reg)

	err := svc.UpdateStatus(context.Background(), node)
	if err == nil {
		t.Fatal("expected error from Save")
	}
}

func TestNodeService_UpdateStatus_IgnoresContextCanceledFromRegistrar(t *testing.T) {
	node := model.VNode{Name: "node-1", Namespace: "ns"}
	repo := &mockNodeRepo{}
	reg := newMockRegistrar()
	reg.updateErr = context.Canceled
	svc := service.NewNodeService(discardLogger(), repo, reg)

	err := svc.UpdateStatus(context.Background(), node)
	if err != nil {
		t.Fatalf("expected context cancellation to be ignored, got %v", err)
	}
	if repo.saveCalls != 0 {
		t.Fatalf("expected no save calls after ignored registrar cancellation, got %d", repo.saveCalls)
	}
}

func TestNodeService_UpdateStatus_RetriesInitialRegistrationAfterTransientFailure(t *testing.T) {
	node := model.VNode{
		Name:      "node-1",
		Namespace: "ns",
		Phase:     model.NodePhaseNotReady,
		TenantRef: model.TenantRef{VClusterNamespace: "tenant-ns", KubeconfigSecret: "tenant-secret"},
		Conditions: []model.NodeCondition{
			{Type: model.NodeConditionRegistered, Status: false, Reason: "RegistrationFailed", Message: "dial tcp timeout"},
			{Type: model.NodeConditionReady, Status: false, Reason: "RegistrationFailed", Message: "dial tcp timeout"},
			{Type: model.NodeConditionDegraded, Status: true, Reason: "RegistrationFailed", Message: "dial tcp timeout"},
		},
	}
	repo := &mockNodeRepo{}
	reg := newMockRegistrar()
	svc := service.NewNodeService(discardLogger(), repo, reg)

	err := svc.UpdateStatus(context.Background(), node)
	if err != nil {
		t.Fatalf("expected registration retry to succeed, got %v", err)
	}
	if reg.registerCalls != 1 {
		t.Fatalf("expected one registration retry, got %d", reg.registerCalls)
	}
	if reg.updateCalls != 0 {
		t.Fatalf("expected no status-only update call during registration retry, got %d", reg.updateCalls)
	}
	if repo.saveCalls != 1 {
		t.Fatalf("expected one save call after successful retry, got %d", repo.saveCalls)
	}
	if len(repo.nodes) != 1 {
		t.Fatalf("expected retried node status to be persisted, got %d nodes", len(repo.nodes))
	}
	if repo.nodes[0].Phase != model.NodePhaseReady {
		t.Fatalf("expected node phase Ready after successful retry, got %s", repo.nodes[0].Phase)
	}
}

func TestNodeService_UpdateStatus_PersistsRetryFailure(t *testing.T) {
	// When the node is already NotReady, a failed retry should NOT save again
	// (to avoid triggering a watch-based re-reconcile loop).
	node := model.VNode{
		Name:      "node-1",
		Namespace: "ns",
		Phase:     model.NodePhaseNotReady,
		TenantRef: model.TenantRef{VClusterNamespace: "tenant-ns", KubeconfigSecret: "tenant-secret"},
		Conditions: []model.NodeCondition{
			{Type: model.NodeConditionRegistered, Status: false, Reason: "RegistrationFailed", Message: "old timeout"},
		},
	}
	repo := &mockNodeRepo{}
	reg := newMockRegistrar()
	reg.registerErr = errors.New("still timing out")
	svc := service.NewNodeService(discardLogger(), repo, reg)

	err := svc.UpdateStatus(context.Background(), node)
	if err == nil {
		t.Fatal("expected registration retry failure to be returned")
	}
	if reg.registerCalls != 1 {
		t.Fatalf("expected one registration retry, got %d", reg.registerCalls)
	}
	if repo.saveCalls != 0 {
		t.Fatalf("expected no save when node is already NotReady, got %d", repo.saveCalls)
	}
}

func TestNodeService_UpdateStatus_PersistsRetryFailure_FromPending(t *testing.T) {
	// When the node transitions from Pending to NotReady, the save IS needed.
	node := model.VNode{
		Name:      "node-1",
		Namespace: "ns",
		Phase:     model.NodePhasePending,
		TenantRef: model.TenantRef{VClusterNamespace: "tenant-ns", KubeconfigSecret: "tenant-secret"},
		Conditions: []model.NodeCondition{
			{Type: model.NodeConditionRegistered, Status: false, Reason: "Registering", Message: "initial"},
		},
	}
	repo := &mockNodeRepo{}
	reg := newMockRegistrar()
	reg.registerErr = errors.New("still timing out")
	svc := service.NewNodeService(discardLogger(), repo, reg)

	err := svc.UpdateStatus(context.Background(), node)
	if err == nil {
		t.Fatal("expected registration retry failure to be returned")
	}
	if len(repo.nodes) != 1 {
		t.Fatalf("expected failed retry status to be persisted, got %d nodes", len(repo.nodes))
	}
	if repo.nodes[0].Phase != model.NodePhaseNotReady {
		t.Fatalf("expected node phase NotReady after failed retry, got %s", repo.nodes[0].Phase)
	}
}

func TestNodeService_UpdateStatus_SkipsUninitializedNode(t *testing.T) {
	node := model.VNode{Name: "node-1", Namespace: "ns"}
	repo := &mockNodeRepo{}
	reg := newMockRegistrar()
	svc := service.NewNodeService(discardLogger(), repo, reg)

	err := svc.UpdateStatus(context.Background(), node)
	if err != nil {
		t.Fatalf("expected uninitialized node status update to be skipped, got %v", err)
	}
	if repo.saveCalls != 0 {
		t.Fatalf("expected no save calls for uninitialized node, got %d", repo.saveCalls)
	}
	if reg.registered[node.Name] {
		t.Fatal("expected registrar not to be invoked for uninitialized node")
	}
}

func TestNodeService_UpdateStatus_IgnoresContextCanceledFromSave(t *testing.T) {
	node := model.VNode{Name: "node-1", Namespace: "ns"}
	repo := &mockNodeRepo{saveErr: context.Canceled}
	reg := newMockRegistrar()
	svc := service.NewNodeService(discardLogger(), repo, reg)

	err := svc.UpdateStatus(context.Background(), node)
	if err != nil {
		t.Fatalf("expected context cancellation from save to be ignored, got %v", err)
	}
}
