package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	HostPodCreates = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vnode_host_pod_creates_total",
			Help: "Total number of translated host pods created by vnode.",
		},
		[]string{"pool"},
	)
	HostPodDeletes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vnode_host_pod_deletes_total",
			Help: "Total number of translated host pods deleted by vnode.",
		},
		[]string{"pool"},
	)
	StatusSyncs = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vnode_pod_status_sync_total",
			Help: "Total number of tenant pod status synchronizations from host pods.",
		},
		[]string{"pool"},
	)
	ExecutionFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vnode_pod_execution_failures_total",
			Help: "Total number of pod execution reconciliation failures.",
		},
		[]string{"pool"},
	)
)

func init() {
	ctrlmetrics.Registry.MustRegister(HostPodCreates, HostPodDeletes, StatusSyncs, ExecutionFailures)
}
