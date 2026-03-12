package model_test

import (
	"testing"

	"github.com/kroderdev/vnode/internal/domain/model"
)

func TestDesiredScaleActions_ScaleUp(t *testing.T) {
	pool := model.VNodePool{NodeCount: 5}
	toAdd, toRemove := pool.DesiredScaleActions(2)
	if toAdd != 3 {
		t.Errorf("expected toAdd=3, got %d", toAdd)
	}
	if toRemove != 0 {
		t.Errorf("expected toRemove=0, got %d", toRemove)
	}
}

func TestDesiredScaleActions_ScaleDown(t *testing.T) {
	pool := model.VNodePool{NodeCount: 1}
	toAdd, toRemove := pool.DesiredScaleActions(4)
	if toAdd != 0 {
		t.Errorf("expected toAdd=0, got %d", toAdd)
	}
	if toRemove != 3 {
		t.Errorf("expected toRemove=3, got %d", toRemove)
	}
}

func TestDesiredScaleActions_NoChange(t *testing.T) {
	pool := model.VNodePool{NodeCount: 3}
	toAdd, toRemove := pool.DesiredScaleActions(3)
	if toAdd != 0 || toRemove != 0 {
		t.Errorf("expected no changes, got toAdd=%d toRemove=%d", toAdd, toRemove)
	}
}

func TestDesiredScaleActions_FromZero(t *testing.T) {
	pool := model.VNodePool{NodeCount: 5}
	toAdd, toRemove := pool.DesiredScaleActions(0)
	if toAdd != 5 {
		t.Errorf("expected toAdd=5, got %d", toAdd)
	}
	if toRemove != 0 {
		t.Errorf("expected toRemove=0, got %d", toRemove)
	}
}

func TestDesiredScaleActions_ToZero(t *testing.T) {
	pool := model.VNodePool{NodeCount: 0}
	toAdd, toRemove := pool.DesiredScaleActions(3)
	if toAdd != 0 {
		t.Errorf("expected toAdd=0, got %d", toAdd)
	}
	if toRemove != 3 {
		t.Errorf("expected toRemove=3, got %d", toRemove)
	}
}

func TestDesiredScaleActions_BothZero(t *testing.T) {
	pool := model.VNodePool{NodeCount: 0}
	toAdd, toRemove := pool.DesiredScaleActions(0)
	if toAdd != 0 || toRemove != 0 {
		t.Errorf("expected no changes, got toAdd=%d toRemove=%d", toAdd, toRemove)
	}
}
