package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	vknode "github.com/virtual-kubelet/virtual-kubelet/node"

	"github.com/kroderdev/vnode/internal/config"
	"github.com/kroderdev/vnode/internal/provider"
	"github.com/kroderdev/vnode/internal/translator"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Parse()
	if err != nil {
		log.Error("failed to parse config", "err", err)
		os.Exit(1)
	}

	log.Info("starting vnode",
		"node", cfg.NodeName,
		"cpu", cfg.CPU.String(),
		"memory", cfg.Memory.String(),
		"hostNamespace", cfg.HostNamespace,
		"runtimeClass", cfg.RuntimeClass,
	)

	hostClient, err := buildClient(cfg.KubeconfigHost)
	if err != nil {
		log.Error("failed to create host kubernetes client", "err", err)
		os.Exit(1)
	}

	vclusterConfig, err := buildRESTConfig(cfg.KubeconfigVCluster)
	if err != nil {
		log.Error("failed to create vcluster REST config", "err", err)
		os.Exit(1)
	}
	vclusterClient, err := kubernetes.NewForConfig(vclusterConfig)
	if err != nil {
		log.Error("failed to create vcluster kubernetes client", "err", err)
		os.Exit(1)
	}

	trans := translator.New(cfg.HostNamespace, cfg.RuntimeClass, cfg.NodeName)
	prov := provider.New(log, hostClient, trans, cfg.HostNamespace)

	node := provider.BuildNode(cfg.NodeName, cfg.CPU, cfg.Memory)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	nodeController, err := vknode.NewNodeController(
		prov,
		node,
		hostClient.CoreV1().Nodes(),
		vknode.WithNodeEnableLeaseV1(
			hostClient.CoordinationV1().Leases("kube-node-lease"),
			40,
		),
		vknode.WithNodePingInterval(10*time.Second),
		vknode.WithNodeStatusUpdateInterval(30*time.Second),
	)
	if err != nil {
		log.Error("failed to create node controller", "err", err)
		os.Exit(1)
	}

	podControllerCfg, err := provider.NewPodControllerConfig(ctx, prov, vclusterClient, cfg.NodeName)
	if err != nil {
		log.Error("failed to create pod controller config", "err", err)
		os.Exit(1)
	}

	podController, err := vknode.NewPodController(*podControllerCfg)
	if err != nil {
		log.Error("failed to create pod controller", "err", err)
		os.Exit(1)
	}

	// Start status sync loop
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := prov.SyncPodStatus(ctx); err != nil {
					log.Warn("pod status sync failed", "err", err)
				}
			}
		}
	}()

	errCh := make(chan error, 2)

	go func() {
		if err := nodeController.Run(ctx); err != nil {
			errCh <- fmt.Errorf("node controller: %w", err)
		}
	}()

	// Wait for node to be ready before starting pod controller
	<-nodeController.Ready()
	log.Info("node registered, starting pod controller")

	go func() {
		if err := podController.Run(ctx, 1); err != nil {
			errCh <- fmt.Errorf("pod controller: %w", err)
		}
	}()

	select {
	case err := <-errCh:
		log.Error("controller error", "err", err)
		cancel()
		os.Exit(1)
	case <-ctx.Done():
		log.Info("shutting down vnode")
	}
}

func buildClient(kubeconfigPath string) (kubernetes.Interface, error) {
	cfg, err := buildRESTConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

func buildRESTConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath == "" {
		return rest.InClusterConfig()
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfigPath)
}
