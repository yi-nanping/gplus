package gplus

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSubQuery_NilOuter_AccumulatesError_H4(t *testing.T) {
	sub, _ := SubQuery[Order](nil)
	if !errors.Is(sub.GetError(), ErrSubqueryOuterNil) {
		t.Errorf("expected ErrSubqueryOuterNil, got %v", sub.GetError())
	}
}

func TestSubQuery_HappyPath(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	sub, o := SubQuery[Order](q)
	if sub.gplusCore().outerQueryRef != AnyQuery(q) {
		t.Errorf("outerQueryRef not set correctly")
	}
	if o == nil {
		t.Errorf("nil instance returned")
	}
}

func TestSubQuery_CorrelatedAliasResolves(t *testing.T) {
	q, u := NewQuery[TestUser](context.Background())
	sub, o := SubQuery[Order](q)
	col, err := sub.resolveColumnName(uintptrOf(&u.ID))
	if err != nil {
		t.Fatalf("expected resolve to succeed, got %v", err)
	}
	if !strings.Contains(col, "id") {
		t.Errorf("expected u.ID resolved, got %s", col)
	}
	col2, err := sub.resolveColumnName(uintptrOf(&o.UserID))
	if err != nil {
		t.Fatalf("sub alias resolve failed: %v", err)
	}
	if col2 != "orders.user_id" {
		t.Errorf("expected orders.user_id, got %s", col2)
	}
}

func TestSubQueryAs_MainTableAlias(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	sub, o := SubQueryAs[Order](q, "o")
	col, err := sub.resolveColumnName(uintptrOf(&o.UserID))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if col != "o.user_id" {
		t.Errorf("expected o.user_id, got %s", col)
	}
}
