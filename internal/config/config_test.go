package config_test

import (
	"os"
	"testing"

	"github.com/kroderdev/vnode/internal/config"
)

func TestDefault_Values(t *testing.T) {
	// Clear env vars that might be set
	for _, key := range []string{"METRICS_ADDR", "HEALTH_PROBE_ADDR", "LEADER_ELECTION", "DEFAULT_RUNTIME_CLASS", "HOST_NAMESPACE"} {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("failed to unset %s: %v", key, err)
		}
	}

	cfg := config.Default()

	if cfg.MetricsAddr != ":8080" {
		t.Errorf("expected MetricsAddr :8080, got %s", cfg.MetricsAddr)
	}
	if cfg.HealthProbeAddr != ":8081" {
		t.Errorf("expected HealthProbeAddr :8081, got %s", cfg.HealthProbeAddr)
	}
	if cfg.LeaderElection {
		t.Error("expected LeaderElection false by default")
	}
	if cfg.LeaderElectionID != "vnode-operator.kroderdev.io" {
		t.Errorf("expected LeaderElectionID vnode-operator.kroderdev.io, got %s", cfg.LeaderElectionID)
	}
	if cfg.DefaultRuntimeClass != "kata" {
		t.Errorf("expected DefaultRuntimeClass kata, got %s", cfg.DefaultRuntimeClass)
	}
	if cfg.HostNamespace != "vnode-system" {
		t.Errorf("expected HostNamespace vnode-system, got %s", cfg.HostNamespace)
	}
}

func TestDefault_EnvOverrides(t *testing.T) {
	t.Setenv("METRICS_ADDR", ":9090")
	t.Setenv("HEALTH_PROBE_ADDR", ":9091")
	t.Setenv("LEADER_ELECTION", "true")
	t.Setenv("DEFAULT_RUNTIME_CLASS", "gvisor")
	t.Setenv("HOST_NAMESPACE", "custom-ns")

	cfg := config.Default()

	if cfg.MetricsAddr != ":9090" {
		t.Errorf("expected MetricsAddr :9090, got %s", cfg.MetricsAddr)
	}
	if cfg.HealthProbeAddr != ":9091" {
		t.Errorf("expected HealthProbeAddr :9091, got %s", cfg.HealthProbeAddr)
	}
	if !cfg.LeaderElection {
		t.Error("expected LeaderElection true")
	}
	if cfg.DefaultRuntimeClass != "gvisor" {
		t.Errorf("expected DefaultRuntimeClass gvisor, got %s", cfg.DefaultRuntimeClass)
	}
	if cfg.HostNamespace != "custom-ns" {
		t.Errorf("expected HostNamespace custom-ns, got %s", cfg.HostNamespace)
	}
}

func TestDefault_LeaderElection_NonTrue(t *testing.T) {
	t.Setenv("LEADER_ELECTION", "false")
	cfg := config.Default()
	if cfg.LeaderElection {
		t.Error("expected LeaderElection false for value 'false'")
	}

	t.Setenv("LEADER_ELECTION", "yes")
	cfg = config.Default()
	if cfg.LeaderElection {
		t.Error("expected LeaderElection false for value 'yes' (only 'true' is truthy)")
	}
}

func TestValidate_Success(t *testing.T) {
	cfg := config.Config{HostNamespace: "vnode-system"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_EmptyHostNamespace(t *testing.T) {
	cfg := config.Config{HostNamespace: ""}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty HostNamespace")
	}
}

func TestValidate_DefaultPassesValidation(t *testing.T) {
	for _, key := range []string{"METRICS_ADDR", "HEALTH_PROBE_ADDR", "LEADER_ELECTION", "DEFAULT_RUNTIME_CLASS", "HOST_NAMESPACE"} {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("failed to unset %s: %v", key, err)
		}
	}

	cfg := config.Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should pass validation: %v", err)
	}
}
