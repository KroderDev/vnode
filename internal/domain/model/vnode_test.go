package model_test

import (
	"testing"

	"github.com/kroderdev/vnode/internal/domain/model"
)

func TestVNode_IsReady_True(t *testing.T) {
	node := model.VNode{
		Conditions: []model.NodeCondition{
			{Type: model.NodeConditionReady, Status: true},
		},
	}
	if !node.IsReady() {
		t.Error("expected node to be ready")
	}
}

func TestVNode_IsReady_False(t *testing.T) {
	node := model.VNode{
		Conditions: []model.NodeCondition{
			{Type: model.NodeConditionReady, Status: false, Reason: "NotReady"},
		},
	}
	if node.IsReady() {
		t.Error("expected node to not be ready")
	}
}

func TestVNode_IsReady_NoConditions(t *testing.T) {
	node := model.VNode{}
	if node.IsReady() {
		t.Error("expected node with no conditions to not be ready")
	}
}

func TestVNode_IsReady_EmptyConditions(t *testing.T) {
	node := model.VNode{Conditions: []model.NodeCondition{}}
	if node.IsReady() {
		t.Error("expected node with empty conditions to not be ready")
	}
}

func TestVNode_IsReady_OnlyRegisteredCondition(t *testing.T) {
	node := model.VNode{
		Conditions: []model.NodeCondition{
			{Type: model.NodeConditionRegistered, Status: true},
		},
	}
	if node.IsReady() {
		t.Error("expected node with only Registered condition to not be ready")
	}
}

func TestVNode_IsReady_MultipleConditions(t *testing.T) {
	node := model.VNode{
		Conditions: []model.NodeCondition{
			{Type: model.NodeConditionRegistered, Status: true},
			{Type: model.NodeConditionReady, Status: true},
		},
	}
	if !node.IsReady() {
		t.Error("expected node with Ready=true among multiple conditions to be ready")
	}
}

func TestVNode_IsReady_MultipleConditions_ReadyFalse(t *testing.T) {
	node := model.VNode{
		Conditions: []model.NodeCondition{
			{Type: model.NodeConditionRegistered, Status: true},
			{Type: model.NodeConditionReady, Status: false},
		},
	}
	if node.IsReady() {
		t.Error("expected node with Ready=false to not be ready")
	}
}
