package runtime

import "context"

// KataAdapter implements ports.IsolationRuntime for Kata Containers.
type KataAdapter struct {
	runtimeClassName string
}

func NewKataAdapter(runtimeClassName string) *KataAdapter {
	return &KataAdapter{runtimeClassName: runtimeClassName}
}

func (a *KataAdapter) RuntimeClassName() string {
	return a.runtimeClassName
}

func (a *KataAdapter) Validate(_ context.Context) error {
	// In a real implementation, this would check that the RuntimeClass exists
	// in the host cluster. For MVP, we trust the user configured it.
	return nil
}
