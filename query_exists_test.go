package gplus

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestExists_BasicSQL(t *testing.T) {
	_, db := setupAdvancedDB(t)
	// 使用 WhereRaw 避免 alias 字段指针与全局 columnNameCache 的解析冲突
	// SubQuery[Order](q) 创建相关子查询，EXISTS (SELECT * FROM orders WHERE user_id = ...)
	q, u := NewQuery[UserWithDelete](context.Background())
	sub, _ := SubQuery[Order](q)
	sub.WhereRaw("user_id = ?", u.ID)
	q.Exists(sub)
	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	if !strings.Contains(sql, "EXISTS") {
		t.Errorf("expected EXISTS in SQL, got %s", sql)
	}
}

func TestNotExists_BasicSQL(t *testing.T) {
	_, db := setupAdvancedDB(t)
	q, u := NewQuery[UserWithDelete](context.Background())
	sub, _ := SubQuery[Order](q)
	sub.WhereRaw("user_id = ?", u.ID)
	q.NotExists(sub)
	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	if !strings.Contains(sql, "NOT EXISTS") {
		t.Errorf("expected NOT EXISTS, got %s", sql)
	}
}

func TestExists_NilSub_AccumulatesErrSubqueryNil(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	q.Exists(nil)
	if !errors.Is(q.GetError(), ErrSubqueryNil) {
		t.Errorf("expected ErrSubqueryNil, got %v", q.GetError())
	}
}

func TestExists_SubErrorsPropagate(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	sub, _ := SubQuery[Order](nil) // sub 自身有 ErrSubqueryOuterNil
	q.Exists(sub)
	if !errors.Is(q.GetError(), ErrSubqueryOuterNil) {
		t.Errorf("expected sub error to propagate to q, got %v", q.GetError())
	}
}
