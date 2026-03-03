package config

import (
	"flag"
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
)

// Config holds all configuration for a vnode instance.
type Config struct {
	NodeName           string
	CPU                resource.Quantity
	Memory             resource.Quantity
	HostNamespace      string
	RuntimeClass       string
	KubeconfigHost     string
	KubeconfigVCluster string
}

// Parse parses command-line flags into a Config.
func Parse() (*Config, error) {
	nodeName := flag.String("node-name", "", "Name of the virtual node to register")
	cpu := flag.String("cpu", "1000m", "CPU capacity to advertise (e.g. 2000m)")
	memory := flag.String("memory", "2Gi", "Memory capacity to advertise (e.g. 4Gi)")
	hostNamespace := flag.String("host-namespace", "", "Namespace in host cluster where real pods are created")
	runtimeClass := flag.String("runtime-class", "kata", "RuntimeClassName for pods on the host cluster")
	kubeconfigHost := flag.String("kubeconfig-host", "", "Path to kubeconfig for the host cluster")
	kubeconfigVCluster := flag.String("kubeconfig-vcluster", "", "Path to kubeconfig for the vcluster")
	flag.Parse()

	if *nodeName == "" {
		return nil, fmt.Errorf("--node-name is required")
	}
	if *hostNamespace == "" {
		return nil, fmt.Errorf("--host-namespace is required")
	}

	cpuQty, err := resource.ParseQuantity(*cpu)
	if err != nil {
		return nil, fmt.Errorf("invalid --cpu value: %w", err)
	}
	memQty, err := resource.ParseQuantity(*memory)
	if err != nil {
		return nil, fmt.Errorf("invalid --memory value: %w", err)
	}

	return &Config{
		NodeName:           *nodeName,
		CPU:                cpuQty,
		Memory:             memQty,
		HostNamespace:      *hostNamespace,
		RuntimeClass:       *runtimeClass,
		KubeconfigHost:     *kubeconfigHost,
		KubeconfigVCluster: *kubeconfigVCluster,
	}, nil
}
