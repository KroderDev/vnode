package config

import (
	"flag"
	"os"
	"testing"
)

func TestParseRequiresNodeName(t *testing.T) {
	// Reset flags for testing
	os.Args = []string{"cmd", "--host-namespace=test-ns"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

	_, err := Parse()
	if err == nil {
		t.Error("expected error for missing --node-name")
	}
}

func TestParseRequiresHostNamespace(t *testing.T) {
	os.Args = []string{"cmd", "--node-name=vnode-01"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

	_, err := Parse()
	if err == nil {
		t.Error("expected error for missing --host-namespace")
	}
}

func TestParseValidConfig(t *testing.T) {
	os.Args = []string{
		"cmd",
		"--node-name=vnode-01",
		"--cpu=2000m",
		"--memory=4Gi",
		"--host-namespace=vcluster-org1",
		"--runtime-class=kata",
	}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

	cfg, err := Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if cfg.NodeName != "vnode-01" {
		t.Errorf("NodeName = %q, want vnode-01", cfg.NodeName)
	}
	if cfg.CPU.String() != "2" {
		t.Errorf("CPU = %q, want 2", cfg.CPU.String())
	}
	if cfg.Memory.String() != "4Gi" {
		t.Errorf("Memory = %q, want 4Gi", cfg.Memory.String())
	}
	if cfg.HostNamespace != "vcluster-org1" {
		t.Errorf("HostNamespace = %q, want vcluster-org1", cfg.HostNamespace)
	}
	if cfg.RuntimeClass != "kata" {
		t.Errorf("RuntimeClass = %q, want kata", cfg.RuntimeClass)
	}
}

func TestParseInvalidCPU(t *testing.T) {
	os.Args = []string{
		"cmd",
		"--node-name=vnode-01",
		"--cpu=invalid",
		"--host-namespace=ns",
	}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

	_, err := Parse()
	if err == nil {
		t.Error("expected error for invalid --cpu")
	}
}
