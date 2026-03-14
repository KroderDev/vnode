package model_test

import (
	"math"
	"testing"

	"github.com/kroderdev/vnode/internal/domain/model"
)

// TestSecurity_ExtremeNodeCount documents that there is no upper bound on
// NodeCount. A malicious or misconfigured pool spec could request MaxInt32
// nodes, potentially exhausting cluster resources.
func TestSecurity_ExtremeNodeCount(t *testing.T) {
	pool := model.VNodePool{
		Name:      "extreme",
		NodeCount: math.MaxInt32,
		TenantRef: model.TenantRef{KubeconfigSecret: "secret"},
	}
	// GAP: No upper bound — validation passes
	if err := pool.Validate(); err != nil {
		t.Fatalf("expected no error for extreme NodeCount, got: %v", err)
	}
}

// TestSecurity_SpecialCharsInPoolName documents that pool name validation
// only checks for non-empty. Names with path traversal characters, spaces,
// or other special characters are accepted.
func TestSecurity_SpecialCharsInPoolName(t *testing.T) {
	names := []string{
		"../escape",
		"pool with spaces",
		"pool/slash",
		"pool\nnewline",
		"pool\x00null",
	}
	for _, name := range names {
		pool := model.VNodePool{
			Name:      name,
			TenantRef: model.TenantRef{KubeconfigSecret: "secret"},
		}
		// GAP: No format validation beyond non-empty
		if err := pool.Validate(); err != nil {
			t.Errorf("name %q: unexpected validation error: %v", name, err)
		}
	}
}

// TestSecurity_EmptyModeAccepted documents that an empty string mode passes
// validation, which means a pool with no explicit mode is valid.
func TestSecurity_EmptyModeAccepted(t *testing.T) {
	pool := model.VNodePool{
		Name:      "pool",
		TenantRef: model.TenantRef{KubeconfigSecret: "secret"},
		Mode:      "",
	}
	if err := pool.Validate(); err != nil {
		t.Fatalf("expected empty mode to be accepted, got: %v", err)
	}
}

// TestSecurity_KubeconfigSecretNoNamespaceValidation documents that the
// TenantRef does not enforce any namespace constraints — a pool can reference
// a kubeconfig secret by name alone with no namespace boundary check at the
// domain model level.
func TestSecurity_KubeconfigSecretNoNamespaceValidation(t *testing.T) {
	pool := model.VNodePool{
		Name: "pool",
		TenantRef: model.TenantRef{
			KubeconfigSecret:  "secret-in-other-ns",
			VClusterNamespace: "other-namespace",
		},
	}
	// GAP: No namespace boundary enforcement at model layer
	if err := pool.Validate(); err != nil {
		t.Fatalf("expected validation to pass (no namespace check), got: %v", err)
	}
}
