package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
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
	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/service"
)

var (
	suiteOnce      sync.Once
	suiteEnv       *envtest.Environment
	suiteCfg       *rest.Config
	suiteClient    client.Client
	suiteClientset kubernetes.Interface
	suiteCancel    context.CancelFunc
	suiteSetupErr  error
	suiteSkip      bool
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
		podExecutionReady := hasCondition(current.Status.Conditions, "PodExecutionReady", metav1.ConditionTrue)
		if current.Status.Phase != "Ready" || current.Status.ReadyNodes != 2 || current.Status.TotalNodes != 2 || !podExecutionReady {
			return fmt.Errorf("pool not ready yet: phase=%s ready=%d total=%d podExecutionReady=%t conditions=%v", current.Status.Phase, current.Status.ReadyNodes, current.Status.TotalNodes, podExecutionReady, current.Status.Conditions)
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

	waitFor(t, 10*time.Second, 200*time.Millisecond, func() error {
		node, err := suiteClientset.CoreV1().Nodes().Get(ctx, "pool-a-1", metav1.GetOptions{})
		if err != nil {
			return err
		}
		if node.Labels["vnode.kroderdev.io/pool"] != "pool-a" {
			return errors.New("tenant node labels not applied")
		}
		lease, err := suiteClientset.CoordinationV1().Leases("kube-node-lease").Get(ctx, "pool-a-1", metav1.GetOptions{})
		if err != nil {
			return err
		}
		if lease.Name != "pool-a-1" {
			return errors.New("expected tenant lease for pool-a-1")
		}
		return nil
	})

	sourcePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "workload-a",
			Namespace: ns,
			Labels: map[string]string{
				"app": "workload-a",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "pool-a-1",
			Containers: []corev1.Container{
				{Name: "main", Image: "nginx:stable"},
			},
		},
	}
	if err := k8sClient.Create(ctx, sourcePod); err != nil {
		t.Fatalf("create source pod: %v", err)
	}

	hostPodName := "pool-a-1-" + ns + "-workload-a"
	waitFor(t, 10*time.Second, 200*time.Millisecond, func() error {
		hostPod := &corev1.Pod{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: hostPodName, Namespace: ns}, hostPod); err != nil {
			return err
		}
		if hostPod.Labels[model.LabelSourcePodName] != "workload-a" {
			return errors.New("expected translated host pod labels")
		}
		return nil
	})

	waitFor(t, 10*time.Second, 200*time.Millisecond, func() error {
		hostPod := &corev1.Pod{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: hostPodName, Namespace: ns}, hostPod); err != nil {
			return err
		}
		hostPod.Status.Phase = corev1.PodRunning
		hostPod.Status.PodIP = "10.42.0.10"
		hostPod.Status.ContainerStatuses = []corev1.ContainerStatus{
			{
				Name:  "main",
				Ready: true,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			},
		}
		return k8sClient.Status().Update(ctx, hostPod)
	})

	waitFor(t, 10*time.Second, 200*time.Millisecond, func() error {
		tenantPod, err := suiteClientset.CoreV1().Pods(ns).Get(ctx, "workload-a", metav1.GetOptions{})
		if err != nil {
			return err
		}
		if tenantPod.Status.Phase != corev1.PodRunning || tenantPod.Status.PodIP != "10.42.0.10" {
			return errors.New("tenant pod status not synced from host pod")
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
			return fmt.Errorf("scaled pool not ready yet: phase=%s ready=%d total=%d conditions=%v", refreshed.Status.Phase, refreshed.Status.ReadyNodes, refreshed.Status.TotalNodes, refreshed.Status.Conditions)
		}
		var nodes v1alpha1.VNodeList
		if err := k8sClient.List(ctx, &nodes, client.InNamespace(ns), client.MatchingLabels{"vnode.kroderdev.io/pool": "pool-a"}); err != nil {
			return err
		}
		if len(nodes.Items) != 1 {
			return fmt.Errorf("expected 1 vnode after scale down, got %d", len(nodes.Items))
		}
		if _, err := suiteClientset.CoreV1().Nodes().Get(ctx, "pool-a-2", metav1.GetOptions{}); client.IgnoreNotFound(err) != nil {
			return err
		} else if err == nil {
			return errors.New("pool-a-2 tenant node still exists after scale down")
		}
		if _, err := suiteClientset.CoordinationV1().Leases("kube-node-lease").Get(ctx, "pool-a-2", metav1.GetOptions{}); client.IgnoreNotFound(err) != nil {
			return err
		} else if err == nil {
			return errors.New("pool-a-2 lease still exists after scale down")
		}
		hostPod := &corev1.Pod{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: hostPodName, Namespace: ns}, hostPod); err != nil {
			return err
		}
		return nil
	})

	if err := k8sClient.Delete(ctx, sourcePod); err != nil {
		t.Fatalf("delete source pod: %v", err)
	}

	waitFor(t, 10*time.Second, 200*time.Millisecond, func() error {
		err := k8sClient.Get(ctx, client.ObjectKey{Name: hostPodName, Namespace: ns}, &corev1.Pod{})
		if err == nil {
			return errors.New("host pod still exists after source pod deletion")
		}
		return client.IgnoreNotFound(err)
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
		if _, err := suiteClientset.CoreV1().Nodes().Get(ctx, "pool-a-1", metav1.GetOptions{}); client.IgnoreNotFound(err) != nil {
			return err
		} else if err == nil {
			return errors.New("tenant node still exists after pool deletion")
		}
		if _, err := suiteClientset.CoordinationV1().Leases("kube-node-lease").Get(ctx, "pool-a-1", metav1.GetOptions{}); client.IgnoreNotFound(err) != nil {
			return err
		} else if err == nil {
			return errors.New("tenant lease still exists after pool deletion")
		}
		return nil
	})

}

func TestDedicatedPoolValidationE2E(t *testing.T) {
	k8sClient := startOperatorEnv(t)

	ctx := context.Background()
	ns := createNamespace(t, ctx, k8sClient, "tenant-b")
	createRawKubeconfigSecret(t, ctx, k8sClient, ns, "tenant-b-kubeconfig", []byte("not-a-kubeconfig"))

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
		if !hasCondition(current.Status.Conditions, "Degraded", metav1.ConditionTrue) {
			return errors.New("expected degraded pool condition")
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
		suiteCfg = cfg

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
		hostPods := kubeclient.NewPodClusterClient(mgr.GetClient())
		resolver := kubeclient.NewSecretKubeconfigResolver(mgr.GetClient())
		tenantClients := virtualkubelet.NewTenantClientManager(resolver)
		registrar := virtualkubelet.NewRegistrar(tenantClients)
		nodeSvc := service.NewNodeService(nodeRepo, registrar)
		poolSvc := service.NewPoolService(nodeRepo, nodeSvc)
		podSvc := service.NewPodService(nilRuntime{})
		podExecSvc := service.NewPodExecutionService(nodeRepo, hostPods, podSvc, tenantClients)

		if err := reconciler.NewVNodePoolReconciler(mgr.GetClient(), mgr.GetScheme(), poolSvc).SetupWithManager(mgr); err != nil {
			suiteSetupErr = err
			return
		}
		if err := reconciler.NewVNodeReconciler(mgr.GetClient(), mgr.GetScheme(), nodeSvc).SetupWithManager(mgr); err != nil {
			suiteSetupErr = err
			return
		}
		if err := reconciler.NewPodSyncReconciler(mgr.GetClient(), podExecSvc, mgr.GetEventRecorder("vnode-pod-sync")).SetupWithManager(mgr); err != nil {
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
		suiteClientset, err = kubernetes.NewForConfig(cfg)
		if err != nil {
			suiteSetupErr = err
			return
		}
		if _, err := suiteClientset.NodeV1().RuntimeClasses().Create(context.Background(), &nodev1.RuntimeClass{
			ObjectMeta: metav1.ObjectMeta{Name: "kata"},
			Handler:    "kata",
		}, metav1.CreateOptions{}); err != nil {
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
	data, err := buildKubeconfigBytes(suiteCfg)
	if err != nil {
		t.Fatalf("build kubeconfig: %v", err)
	}
	createRawKubeconfigSecret(t, ctx, c, namespace, name, data)
}

func createRawKubeconfigSecret(t *testing.T, ctx context.Context, c client.Client, namespace, name string, data []byte) {
	t.Helper()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"config": data,
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

type nilRuntime struct{}

func (nilRuntime) RuntimeClassName() string       { return "kata" }
func (nilRuntime) Validate(context.Context) error { return nil }

func buildKubeconfigBytes(cfg *rest.Config) ([]byte, error) {
	kubeconfig := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"envtest": {
				Server:                   cfg.Host,
				CertificateAuthorityData: cfg.CAData,
			},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"envtest": {
				ClientCertificateData: cfg.CertData,
				ClientKeyData:         cfg.KeyData,
				Token:                 cfg.BearerToken,
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"envtest": {
				Cluster:  "envtest",
				AuthInfo: "envtest",
			},
		},
		CurrentContext: "envtest",
	}
	return clientcmd.Write(kubeconfig)
}

func hasCondition(conditions []metav1.Condition, conditionType string, status metav1.ConditionStatus) bool {
	for _, condition := range conditions {
		if condition.Type == conditionType && condition.Status == status {
			return true
		}
	}
	return false
}
