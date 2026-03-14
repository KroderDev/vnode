package runtime_test

import (
	"context"
	"testing"

	"github.com/kroderdev/vnode/internal/adapter/outbound/runtime"
)

// TestSecurity_ValidateIsNoOp explicitly documents that Validate() is a no-op
// stub that returns nil for any input. There is no verification that the
// RuntimeClass actually exists in the cluster.
func TestSecurity_ValidateIsNoOp(t *testing.T) {
	cases := []string{"", "nonexistent-class", "kata", "../escape"}
	for _, rc := range cases {
		adapter := runtime.NewKataAdapter(rc)
		if err := adapter.Validate(context.Background()); err != nil {
			t.Errorf("Validate(%q) returned error: %v", rc, err)
		}
	}
}

// TestSecurity_EmptyRuntimeClassMeansNoIsolation documents that NewKataAdapter
// with an empty string means pods will use the default container runtime
// (typically runc), providing no Kata/gVisor isolation.
func TestSecurity_EmptyRuntimeClassMeansNoIsolation(t *testing.T) {
	adapter := runtime.NewKataAdapter("")
	if adapter.RuntimeClassName() != "" {
		t.Errorf("expected empty RuntimeClassName, got %q", adapter.RuntimeClassName())
	}
}

// TestSecurity_RuntimeClassNameSpecialChars verifies that special characters
// in the runtime class name are returned as-is with no sanitization.
// The K8s API is responsible for validation.
func TestSecurity_RuntimeClassNameSpecialChars(t *testing.T) {
	names := []string{"../escape", "class with spaces", "class\nnewline"}
	for _, name := range names {
		adapter := runtime.NewKataAdapter(name)
		if adapter.RuntimeClassName() != name {
			t.Errorf("expected %q, got %q", name, adapter.RuntimeClassName())
		}
	}
}
