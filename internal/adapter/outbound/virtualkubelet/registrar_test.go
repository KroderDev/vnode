package virtualkubelet

import (
	"context"
	"testing"

	"github.com/kroderdev/vnode/internal/domain/model"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

type fakeResolver struct {
	data []byte
	err  error
}

func (r *fakeResolver) Resolve(context.Context, string, string) ([]byte, error) {
	return r.data, r.err
}

func TestTenantClientManager_CachesClientsets(t *testing.T) {
	manager := NewTenantClientManager(&fakeResolver{data: validKubeconfigBytes()})
	calls := 0
	clientset := kubernetesfake.NewClientset()
	manager.factory = func(*rest.Config) (kubernetes.Interface, error) {
		calls++
		return clientset, nil
	}

	tenant := model.TenantRef{VClusterNamespace: "tenant-a", KubeconfigSecret: "cfg"}
	first, err := manager.Get(context.Background(), tenant)
	if err != nil {
		t.Fatalf("first get: %v", err)
	}
	second, err := manager.Get(context.Background(), tenant)
	if err != nil {
		t.Fatalf("second get: %v", err)
	}

	if first != second {
		t.Fatal("expected cached clientset instance")
	}
	if calls != 1 {
		t.Fatalf("expected single client factory call, got %d", calls)
	}
}

func TestRegistrar_Register_CreatesTenantNodeAndLease(t *testing.T) {
	clientset := kubernetesfake.NewClientset()
	manager := NewTenantClientManager(&fakeResolver{data: validKubeconfigBytes()})
	manager.factory = func(*rest.Config) (kubernetes.Interface, error) {
		return clientset, nil
	}
	reg := NewRegistrar(manager)

	node := model.VNode{
		Name:        "pool-a-1",
		PoolName:    "pool-a",
		TenantRef:   model.TenantRef{VClusterName: "tenant-a", VClusterNamespace: "tenant-a", KubeconfigSecret: "cfg"},
		Taints:      []model.Taint{{Key: "dedicated", Value: "true", Effect: string(corev1.TaintEffectNoSchedule)}},
		Capacity:    model.ResourceList{CPU: "2", Memory: "4Gi", Pods: 110},
		Allocatable: model.ResourceList{CPU: "2", Memory: "4Gi", Pods: 110},
		Conditions: []model.NodeCondition{
			{Type: model.NodeConditionReady, Status: true, Reason: "Ready", Message: "Node is ready"},
			{Type: model.NodeConditionLease, Status: true, Reason: "LeaseActive", Message: "Lease active"},
		},
	}

	if err := reg.Register(context.Background(), node, node.TenantRef); err != nil {
		t.Fatalf("register: %v", err)
	}

	tenantNode, err := clientset.CoreV1().Nodes().Get(context.Background(), "pool-a-1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if tenantNode.Labels["vnode.kroderdev.io/pool"] != "pool-a" {
		t.Fatalf("unexpected pool label: %#v", tenantNode.Labels)
	}
	if len(tenantNode.Spec.Taints) != 1 {
		t.Fatalf("expected tenant taint to be applied")
	}
	if got := tenantNode.Status.Capacity.Cpu().String(); got != "2" {
		t.Fatalf("unexpected cpu capacity: %s", got)
	}

	lease, err := clientset.CoordinationV1().Leases(leaseNamespace).Get(context.Background(), "pool-a-1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get lease: %v", err)
	}
	if lease.Name != "pool-a-1" {
		t.Fatalf("unexpected lease name: %s", lease.Name)
	}
}

func TestRegistrar_Deregister_RemovesTenantArtifacts(t *testing.T) {
	clientset := kubernetesfake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: leaseNamespace}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "pool-a-1"}},
	)
	manager := NewTenantClientManager(&fakeResolver{data: validKubeconfigBytes()})
	manager.factory = func(*rest.Config) (kubernetes.Interface, error) {
		return clientset, nil
	}
	reg := NewRegistrar(manager)

	node := model.VNode{Name: "pool-a-1"}
	tenant := model.TenantRef{VClusterNamespace: "tenant-a", KubeconfigSecret: "cfg"}
	if _, err := clientset.CoordinationV1().Leases(leaseNamespace).Create(context.Background(), leaseForTest("pool-a-1"), metav1.CreateOptions{}); err != nil {
		t.Fatalf("create lease: %v", err)
	}

	if err := reg.Deregister(context.Background(), node, tenant); err != nil {
		t.Fatalf("deregister: %v", err)
	}
	if _, err := clientset.CoreV1().Nodes().Get(context.Background(), "pool-a-1", metav1.GetOptions{}); err == nil {
		t.Fatal("expected node to be deleted")
	}
	if _, err := clientset.CoordinationV1().Leases(leaseNamespace).Get(context.Background(), "pool-a-1", metav1.GetOptions{}); err == nil {
		t.Fatal("expected lease to be deleted")
	}
}

func leaseForTest(name string) *coordinationv1.Lease {
	return &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: leaseNamespace},
	}
}

func validKubeconfigBytes() []byte {
	return []byte(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://example.invalid
  name: test
contexts:
- context:
    cluster: test
    user: test
  name: test
current-context: test
users:
- name: test
  user:
    token: fake`)
}
