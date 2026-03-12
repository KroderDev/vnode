package model

// PoolMode defines how a VNodePool allocates host resources.
type PoolMode string

const (
	PoolModeShared    PoolMode = "shared"
	PoolModeDedicated PoolMode = "dedicated"
	PoolModeBurstable PoolMode = "burstable"
)

// PoolPhase represents the lifecycle phase of a VNodePool.
type PoolPhase string

const (
	PoolPhasePending  PoolPhase = "Pending"
	PoolPhaseReady    PoolPhase = "Ready"
	PoolPhaseScaling  PoolPhase = "Scaling"
	PoolPhaseFailed   PoolPhase = "Failed"
	PoolPhaseDeleting PoolPhase = "Deleting"
)

// TenantRef identifies the target vcluster for a pool.
type TenantRef struct {
	VClusterName     string `json:"vclusterName"`
	VClusterNamespace string `json:"vclusterNamespace"`
	KubeconfigSecret string `json:"kubeconfigSecret"`
}

// VNodePool represents a pool of virtual nodes for a single tenant.
type VNodePool struct {
	ID               string
	Name             string
	Namespace        string
	TenantRef        TenantRef
	Mode             PoolMode
	IsolationBackend string
	NodeCount        int32
	PerNodeResources ResourceList
	Phase            PoolPhase
	ReadyNodes       int32
	Nodes            []string // VNode names
}

// DesiredScaleActions computes the number of nodes to add or remove.
func (p *VNodePool) DesiredScaleActions(currentNodes int32) (toAdd int32, toRemove int32) {
	if p.NodeCount > currentNodes {
		return p.NodeCount - currentNodes, 0
	}
	if p.NodeCount < currentNodes {
		return 0, currentNodes - p.NodeCount
	}
	return 0, 0
}
