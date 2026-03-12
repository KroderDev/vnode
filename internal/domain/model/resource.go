package model

// ResourceList represents a set of compute resources.
type ResourceList struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
	Pods   int32  `json:"pods"`
}
