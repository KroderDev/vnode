package main

import (
	"fmt"
	"os"

	"github.com/kroderdev/vnode/api/v1alpha1"
	"github.com/kroderdev/vnode/internal/adapter/inbound/reconciler"
	"github.com/kroderdev/vnode/internal/adapter/outbound/kubeclient"
	"github.com/kroderdev/vnode/internal/adapter/outbound/runtime"
	vkregistrar "github.com/kroderdev/vnode/internal/adapter/outbound/virtualkubelet"
	"github.com/kroderdev/vnode/internal/config"
	"github.com/kroderdev/vnode/internal/domain/service"

	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	kruntime "k8s.io/apimachinery/pkg/runtime"
)

var (
	scheme = kruntime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))

	// Required for the scheme to have a codec factory (used internally by controller-runtime).
	_ = serializer.NewCodecFactory(scheme)
}

func main() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	logger := ctrl.Log.WithName("setup")

	cfg := config.Default()
	if err := cfg.Validate(); err != nil {
		logger.Error(err, "invalid configuration")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: cfg.MetricsAddr,
		},
		HealthProbeBindAddress: cfg.HealthProbeAddr,
		LeaderElection:         cfg.LeaderElection,
		LeaderElectionID:       cfg.LeaderElectionID,
	})
	if err != nil {
		logger.Error(err, "unable to create manager")
		os.Exit(1)
	}

	// Build outbound adapters
	nodeRepo := kubeclient.NewNodeRepository(mgr.GetClient())
	hostPods := kubeclient.NewPodClusterClient(mgr.GetClient())
	kubeconfigResolver := kubeclient.NewSecretKubeconfigResolver(mgr.GetClient())
	tenantClients := vkregistrar.NewTenantClientManager(kubeconfigResolver)
	registrar := vkregistrar.NewRegistrar(tenantClients)
	kataRuntime := runtime.NewKataAdapter(cfg.DefaultRuntimeClass)

	// Build domain services
	nodeSvc := service.NewNodeService(nodeRepo, registrar)
	poolSvc := service.NewPoolService(nodeRepo, nodeSvc)
	podSvc := service.NewPodService(kataRuntime)
	podExecSvc := service.NewPodExecutionService(nodeRepo, hostPods, podSvc, tenantClients)

	// Build inbound adapters (reconcilers)
	poolReconciler := reconciler.NewVNodePoolReconciler(mgr.GetClient(), mgr.GetScheme(), poolSvc)
	vnodeReconciler := reconciler.NewVNodeReconciler(mgr.GetClient(), mgr.GetScheme(), nodeSvc)
	podSyncReconciler := reconciler.NewPodSyncReconciler(mgr.GetClient(), podExecSvc)

	if err := poolReconciler.SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "VNodePool")
		os.Exit(1)
	}
	if err := vnodeReconciler.SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "VNode")
		os.Exit(1)
	}
	if err := podSyncReconciler.SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "PodSync")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	logger.Info("starting vnode operator")
	fmt.Println("vnode operator starting...")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "problem running manager")
		os.Exit(1)
	}
}
