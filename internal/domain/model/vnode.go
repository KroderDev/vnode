package model

// NodePhase represents the lifecycle phase of a VNode.
type NodePhase string

const (
	NodePhasePending     NodePhase = "Pending"
	NodePhaseReady       NodePhase = "Ready"
	NodePhaseNotReady    NodePhase = "NotReady"
	NodePhaseTerminating NodePhase = "Terminating"
)

// NodeConditionType identifies a condition on a VNode.
type NodeConditionType string

const (
	NodeConditionRegistered NodeConditionType = "Registered"
	NodeConditionReady      NodeConditionType = "Ready"
	NodeConditionKubeconfig NodeConditionType = "KubeconfigResolved"
	NodeConditionLease      NodeConditionType = "LeaseActive"
	NodeConditionDegraded   NodeConditionType = "Degraded"
)

// NodeCondition describes the state of a VNode at a point in time.
type NodeCondition struct {
	Type    NodeConditionType
	Status  bool
	Reason  string
	Message string
}

// VNode represents a single virtual node within a pool.
type VNode struct {
	ID          string
	Name        string
	Namespace   string
	PoolName    string
	TenantRef   TenantRef
	Taints      []Taint
	Phase       NodePhase
	Capacity    ResourceList
	Allocatable ResourceList
	Conditions  []NodeCondition
}

// IsReady returns true if the node has a Ready condition with status true.
func (n *VNode) IsReady() bool {
	if n.Phase == NodePhaseReady {
		return true
	}
	for _, c := range n.Conditions {
		if c.Type == NodeConditionReady {
			return c.Status
		}
	}
	return false
}
