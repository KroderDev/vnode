package e2e

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/kroderdev/vnode/api/v1alpha1"
	"github.com/kroderdev/vnode/internal/adapter/inbound/reconciler"
	"github.com/kroderdev/vnode/internal/adapter/outbound/kubeclient"
	"github.com/kroderdev/vnode/internal/adapter/outbound/virtualkubelet"
	"github.com/kroderdev/vnode/internal/domain/service"
)

var (
	suiteOnce     sync.Once
	suiteEnv      *envtest.Environment
	suiteClient   client.Client
	suiteCancel   context.CancelFunc
	suiteSetupErr error
	suiteSkip     bool
)

func TestVNodePoolLifecycleE2E(t *testing.T) {
	k8sClient := startOperatorEnv(t)

	ctx := context.Background()
	ns := createNamespace(t, ctx, k8sClient, "tenant-a")
	createKubeconfigSecret(t, ctx, k8sClient, ns, "tenant-a-kubeconfig")

	pool := &v1alpha1.VNodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pool-a",
			Namespace: ns,
		},
		Spec: v1alpha1.VNodePoolSpec{
			TenantRef: v1alpha1.TenantRef{
				VClusterName:      "tenant-a",
				VClusterNamespace: ns,
				KubeconfigSecret:  "tenant-a-kubeconfig",
			},
			NodeCount: 2,
			PerNodeResources: v1alpha1.NodeResources{
				CPU:    "2000m",
				Memory: "4Gi",
				Pods:   110,
			},
			Mode:             "shared",
			IsolationBackend: "kata",
		},
	}
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("create pool: %v", err)
	}

	waitFor(t, 10*time.Second, 200*time.Millisecond, func() error {
		current := &v1alpha1.VNodePool{}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), current); err != nil {
			return err
		}
		if current.Status.Phase != "Ready" || current.Status.ReadyNodes != 2 || current.Status.TotalNodes != 2 {
			return errors.New("pool not ready yet")
		}
		return nil
	})

	waitFor(t, 10*time.Second, 200*time.Millisecond, func() error {
		var nodes v1alpha1.VNodeList
		if err := k8sClient.List(ctx, &nodes, client.InNamespace(ns), client.MatchingLabels{"vnode.kroderdev.io/pool": "pool-a"}); err != nil {
			return err
		}
		if len(nodes.Items) != 2 {
			return errors.New("expected 2 vnodes")
		}
		return nil
	})

	current := &v1alpha1.VNodePool{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), current); err != nil {
		t.Fatalf("get pool for update: %v", err)
	}
	current.Spec.NodeCount = 1
	if err := k8sClient.Update(ctx, current); err != nil {
		t.Fatalf("scale pool down: %v", err)
	}

	waitFor(t, 10*time.Second, 200*time.Millisecond, func() error {
		refreshed := &v1alpha1.VNodePool{}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), refreshed); err != nil {
			return err
		}
		if refreshed.Status.Phase != "Ready" || refreshed.Status.ReadyNodes != 1 || refreshed.Status.TotalNodes != 1 {
			return errors.New("scaled pool not ready yet")
		}
		var nodes v1alpha1.VNodeList
		if err := k8sClient.List(ctx, &nodes, client.InNamespace(ns), client.MatchingLabels{"vnode.kroderdev.io/pool": "pool-a"}); err != nil {
			return err
		}
		if len(nodes.Items) != 1 {
			return errors.New("expected 1 vnode after scale down")
		}
		return nil
	})

	if err := k8sClient.Delete(ctx, current); err != nil {
		t.Fatalf("delete pool: %v", err)
	}

	waitFor(t, 10*time.Second, 200*time.Millisecond, func() error {
		err := k8sClient.Get(ctx, client.ObjectKey{Name: "pool-a", Namespace: ns}, &v1alpha1.VNodePool{})
		if err == nil {
			return errors.New("pool still exists")
		}
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		var nodes v1alpha1.VNodeList
		if err := k8sClient.List(ctx, &nodes, client.InNamespace(ns), client.MatchingLabels{"vnode.kroderdev.io/pool": "pool-a"}); err != nil {
			return err
		}
		if len(nodes.Items) != 0 {
			return errors.New("vnodes still exist after pool deletion")
		}
		return nil
	})

}

func TestDedicatedPoolValidationE2E(t *testing.T) {
	k8sClient := startOperatorEnv(t)

	ctx := context.Background()
	ns := createNamespace(t, ctx, k8sClient, "tenant-b")
	createKubeconfigSecret(t, ctx, k8sClient, ns, "tenant-b-kubeconfig")

	pool := &v1alpha1.VNodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalid-dedicated",
			Namespace: ns,
		},
		Spec: v1alpha1.VNodePoolSpec{
			TenantRef: v1alpha1.TenantRef{
				VClusterName:      "tenant-b",
				VClusterNamespace: ns,
				KubeconfigSecret:  "tenant-b-kubeconfig",
			},
			NodeCount: 1,
			PerNodeResources: v1alpha1.NodeResources{
				CPU:    "1000m",
				Memory: "2Gi",
				Pods:   110,
			},
			Mode:             "dedicated",
			IsolationBackend: "kata",
		},
	}
	if err := k8sClient.Create(ctx, pool); err != nil {
		t.Fatalf("create invalid pool: %v", err)
	}

	waitFor(t, 10*time.Second, 200*time.Millisecond, func() error {
		current := &v1alpha1.VNodePool{}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), current); err != nil {
			return err
		}
		if current.Status.Phase != "Failed" {
			return errors.New("expected Failed phase")
		}
		var nodes v1alpha1.VNodeList
		if err := k8sClient.List(ctx, &nodes, client.InNamespace(ns), client.MatchingLabels{"vnode.kroderdev.io/pool": "invalid-dedicated"}); err != nil {
			return err
		}
		if len(nodes.Items) != 0 {
			return errors.New("invalid pool should not create vnodes")
		}
		return nil
	})
}

func TestMain(m *testing.M) {
	code := func() int {
		if _, ok := os.LookupEnv("KUBEBUILDER_ASSETS"); !ok {
			suiteSkip = true
			return m.Run()
		}

		setupSuite()
		if suiteSetupErr != nil {
			return 1
		}

		code := m.Run()
		teardownSuite()
		return code
	}()
	os.Exit(code)
}

func startOperatorEnv(t *testing.T) client.Client {
	t.Helper()
	if suiteSkip {
		t.Skip("KUBEBUILDER_ASSETS is not set; skipping e2e envtest suite")
	}
	setupSuite()
	if suiteSetupErr != nil {
		t.Fatalf("setup envtest suite: %v", suiteSetupErr)
	}
	return suiteClient
}

func setupSuite() {
	suiteOnce.Do(func() {
		logf.SetLogger(zap.New(zap.UseDevMode(true)))

		scheme := runtime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(scheme))
		utilruntime.Must(v1alpha1.AddToScheme(scheme))

		suiteEnv = &envtest.Environment{
			CRDs: []*apiextensionsv1.CustomResourceDefinition{
				mustVNodePoolCRD(),
				mustVNodeCRD(),
			},
			ErrorIfCRDPathMissing: true,
		}

		cfg, err := suiteEnv.Start()
		if err != nil {
			suiteSetupErr = err
			return
		}

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme: scheme,
			Metrics: metricsserver.Options{
				BindAddress: "0",
			},
			HealthProbeBindAddress: "0",
			LeaderElection:         false,
		})
		if err != nil {
			suiteSetupErr = err
			return
		}

		nodeRepo := kubeclient.NewNodeRepository(mgr.GetClient())
		registrar := virtualkubelet.NewRegistrar()
		nodeSvc := service.NewNodeService(nodeRepo, registrar)
		poolSvc := service.NewPoolService(nodeRepo, nodeSvc)

		if err := reconciler.NewVNodePoolReconciler(mgr.GetClient(), mgr.GetScheme(), poolSvc).SetupWithManager(mgr); err != nil {
			suiteSetupErr = err
			return
		}
		if err := reconciler.NewVNodeReconciler(mgr.GetClient(), mgr.GetScheme(), nodeSvc).SetupWithManager(mgr); err != nil {
			suiteSetupErr = err
			return
		}

		ctx, cancel := context.WithCancel(context.Background())
		suiteCancel = cancel
		go func() {
			_ = mgr.Start(ctx)
		}()

		suiteClient, err = client.New(cfg, client.Options{Scheme: scheme})
		if err != nil {
			suiteSetupErr = err
			return
		}
	})
}

func teardownSuite() {
	if suiteCancel != nil {
		suiteCancel()
	}
	if suiteEnv != nil {
		if err := suiteEnv.Stop(); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not supported by windows") {
			panic(err)
		}
	}
}

func createNamespace(t *testing.T, ctx context.Context, c client.Client, name string) string {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := c.Create(ctx, ns); err != nil {
		t.Fatalf("create namespace %s: %v", name, err)
	}
	return name
}

func createKubeconfigSecret(t *testing.T, ctx context.Context, c client.Client, namespace, name string) {
	t.Helper()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"config": []byte("apiVersion: v1\nkind: Config\n"),
		},
	}
	if err := c.Create(ctx, secret); err != nil {
		t.Fatalf("create kubeconfig secret: %v", err)
	}
}

func waitFor(t *testing.T, timeout, interval time.Duration, fn func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := fn(); err == nil {
			return
		} else {
			lastErr = err
		}
		time.Sleep(interval)
	}
	t.Fatalf("condition not met before timeout: %v", lastErr)
}

func mustVNodePoolCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "vnodepools.vnode.kroderdev.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "vnode.kroderdev.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:     "vnodepools",
				Singular:   "vnodepool",
				Kind:       "VNodePool",
				ShortNames: []string{"vnp"},
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: baseSchema(),
					},
					Subresources: &apiextensionsv1.CustomResourceSubresources{
						Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
					},
				},
			},
		},
	}
}

func mustVNodeCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "vnodes.vnode.kroderdev.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "vnode.kroderdev.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:     "vnodes",
				Singular:   "vnode",
				Kind:       "VNode",
				ShortNames: []string{"vn"},
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: baseSchema(),
					},
					Subresources: &apiextensionsv1.CustomResourceSubresources{
						Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
					},
				},
			},
		},
	}
}

func baseSchema() *apiextensionsv1.JSONSchemaProps {
	return &apiextensionsv1.JSONSchemaProps{
		Type: "object",
		Properties: map[string]apiextensionsv1.JSONSchemaProps{
			"apiVersion": {Type: "string"},
			"kind":       {Type: "string"},
			"metadata":   {Type: "object"},
			"spec": {
				Type:                   "object",
				XPreserveUnknownFields: boolPtr(true),
			},
			"status": {
				Type:                   "object",
				XPreserveUnknownFields: boolPtr(true),
			},
		},
	}
}

func boolPtr(v bool) *bool {
	return &v
}
