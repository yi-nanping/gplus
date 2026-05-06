package gplus

import (
	"context"
	"errors"
	"testing"
)

func TestNewQueryAs_HappyPath(t *testing.T) {
	q, u := NewQueryAs[TestUser](context.Background(), "u")
	if q == nil || u == nil {
		t.Fatalf("nil returned")
	}
	col, err := q.resolveColumnName(uintptrOf(&u.Name))
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	// TestUser.Name 字段 gorm tag 为 column:username
	if col != "u.username" {
		t.Errorf("expected u.username, got %s", col)
	}
}

func TestNewQueryAs_InvalidNameAccumulates(t *testing.T) {
	q, _ := NewQueryAs[TestUser](context.Background(), "1bad")
	if !errors.Is(q.GetError(), ErrAliasInvalidName) {
		t.Fatalf("expected ErrAliasInvalidName, got %v", q.GetError())
	}
}

func TestNewQueryAs_ConflictsWithSideAlias(t *testing.T) {
	q, _ := NewQueryAs[TestUser](context.Background(), "u")
	_ = As[TestUser](q, "u") // 副表与主表同名
	if !errors.Is(q.GetError(), ErrAliasDuplicate) {
		t.Fatalf("expected ErrAliasDuplicate, got %v", q.GetError())
	}
}
