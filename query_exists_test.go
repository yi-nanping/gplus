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

// TestAliasField_InQEq_Works 验证 alias 字段在 q.Eq 等类型安全方法中可用
// 这是 v0.8.0 alias 体系的核心承诺：&o.Field 应被 Eq/Where 等方法正确解析为 alias.col
func TestAliasField_InQEq_Works(t *testing.T) {
	_, db := setupAdvancedDB(t)
	q, u := NewQuery[UserWithDelete](context.Background())
	o := As[Order](q, "o")
	q.LeftJoinAs(o, &o.UserID, &u.ID, "")

	// 关键测试：q.Eq 必须能解析 alias 字段地址
	q.Eq(&o.Amount, 100)

	if err := q.GetError(); err != nil {
		t.Fatalf("q.Eq with alias field accumulated error: %v", err)
	}

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	// SQL 应含 o.amount（alias 列引用），SQLite 方言会加引号为 "o"."amount"
	if !strings.Contains(sql, "o") || !strings.Contains(sql, "amount") {
		t.Errorf("expected alias 'o.amount' reference in SQL, got %s", sql)
	}
	// 确认 o.amount 以某种方式出现（裸列名或带引号均可）
	if !strings.Contains(sql, `"o"."amount"`) && !strings.Contains(sql, "o.amount") {
		t.Errorf("expected 'o.amount' or '\"o\".\"amount\"' in SQL, got %s", sql)
	}
}
