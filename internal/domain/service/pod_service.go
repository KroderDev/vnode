package service

import (
	"context"

	"github.com/kroderdev/vnode/internal/domain/model"
	"github.com/kroderdev/vnode/internal/domain/ports"
)

// PodService implements ports.PodTranslator.
type PodService struct {
	runtime ports.IsolationRuntime
}

func NewPodService(runtime ports.IsolationRuntime) *PodService {
	return &PodService{runtime: runtime}
}

func (s *PodService) Translate(_ context.Context, pod model.PodSpec, pool model.VNodePool, vnodeName string) (model.PodTranslation, error) {
	return model.TranslatePod(
		pod,
		vnodeName,
		pool.Name,
		pool.Namespace,
		s.runtime.RuntimeClassName(),
	), nil
}

func (s *PodService) SyncStatus(_ context.Context, hostStatus model.PodStatus) (model.PodStatus, error) {
	// Direct passthrough for MVP - host status maps 1:1 to vcluster status
	return hostStatus, nil
}
