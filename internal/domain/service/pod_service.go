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
	opts := model.TranslateOpts{
		VNodeName:       vnodeName,
		PoolName:        pool.Name,
		TargetNamespace: pool.Namespace,
		RuntimeClass:    s.runtime.RuntimeClassName(),
	}
	// For dedicated/burstable modes, apply node selector to pin pods to labeled host nodes
	if pool.Mode == model.PoolModeDedicated || pool.Mode == model.PoolModeBurstable {
		opts.NodeSelector = pool.NodeSelector
	}
	return model.TranslatePod(pod, opts), nil
}

func (s *PodService) SyncStatus(_ context.Context, hostStatus model.PodStatus) (model.PodStatus, error) {
	// Direct passthrough for MVP - host status maps 1:1 to vcluster status
	return hostStatus, nil
}
