package runtime_test

import (
	"context"
	"testing"

	"github.com/kroderdev/vnode/internal/adapter/outbound/runtime"
)

func TestKataAdapter_DefaultClassName(t *testing.T) {
	adapter := runtime.NewKataAdapter("")
	if adapter.RuntimeClassName() != "kata" {
		t.Errorf("expected default 'kata', got %s", adapter.RuntimeClassName())
	}
}

func TestKataAdapter_CustomClassName(t *testing.T) {
	adapter := runtime.NewKataAdapter("kata-qemu")
	if adapter.RuntimeClassName() != "kata-qemu" {
		t.Errorf("expected 'kata-qemu', got %s", adapter.RuntimeClassName())
	}
}

func TestKataAdapter_Validate_ReturnsNil(t *testing.T) {
	adapter := runtime.NewKataAdapter("kata")
	if err := adapter.Validate(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestKataAdapter_Validate_WithCancelledContext(t *testing.T) {
	adapter := runtime.NewKataAdapter("kata")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// MVP stub ignores context, should still return nil
	if err := adapter.Validate(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
