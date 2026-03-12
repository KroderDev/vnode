package config

import (
	"fmt"
	"os"
)

// Config holds the operator configuration.
type Config struct {
	MetricsAddr          string
	HealthProbeAddr      string
	LeaderElection       bool
	LeaderElectionID     string
	DefaultRuntimeClass  string
	HostNamespace        string
}

// Default returns the default operator configuration.
func Default() Config {
	return Config{
		MetricsAddr:         envOrDefault("METRICS_ADDR", ":8080"),
		HealthProbeAddr:     envOrDefault("HEALTH_PROBE_ADDR", ":8081"),
		LeaderElection:      os.Getenv("LEADER_ELECTION") == "true",
		LeaderElectionID:    "vnode-operator.kroderdev.io",
		DefaultRuntimeClass: envOrDefault("DEFAULT_RUNTIME_CLASS", "kata"),
		HostNamespace:       envOrDefault("HOST_NAMESPACE", "vnode-system"),
	}
}

// Validate checks that required config values are set.
func (c Config) Validate() error {
	if c.HostNamespace == "" {
		return fmt.Errorf("HOST_NAMESPACE is required")
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
