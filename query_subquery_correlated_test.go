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

// TestSubQuery_NestedThreeLayers 验证嵌套子查询 3 层时 outerQueryRef 链能正确解析
func TestSubQuery_NestedThreeLayers(t *testing.T) {
	q, u := NewQuery[TestUser](context.Background())
	sub1, o := SubQuery[Order](q)
	sub2, p := SubQuery[Product](sub1)

	// sub2 应能向上解析到 q.u（祖父）和 sub1.o（父）和自身 p
	colU, err := sub2.resolveColumnName(uintptrOf(&u.ID))
	if err != nil {
		t.Errorf("nested resolve to grandparent failed: %v", err)
	}
	if !strings.Contains(colU, "id") {
		t.Errorf("expected ID in colU, got %s", colU)
	}

	colO, err := sub2.resolveColumnName(uintptrOf(&o.UserID))
	if err != nil {
		t.Errorf("nested resolve to parent failed: %v", err)
	}
	if colO != "orders.user_id" {
		t.Errorf("expected orders.user_id, got %s", colO)
	}

	colP, err := sub2.resolveColumnName(uintptrOf(&p.Name))
	if err != nil {
		t.Errorf("self alias resolve failed: %v", err)
	}
	// Product 的 Name 字段实际 column 名取决于 gorm tag；如无 tag 默认 snake_case
	// 这里只断言含 name 关键字（products.name 或 products.{column}）
	if !strings.Contains(colP, "name") {
		t.Errorf("expected name in colP, got %s", colP)
	}
}

// TestSubQuery_OuterCanonicalSingletonReferenced 验证 sub 能引用外层 q 的规范单例字段
// 这是 correlated subquery 的核心场景：外层 q 用 NewQuery（非 NewQueryAs）时主表是规范单例
func TestSubQuery_OuterCanonicalSingletonReferenced(t *testing.T) {
	q, u := NewQuery[TestUser](context.Background())
	sub, o := SubQuery[Order](q)

	// &u.Name 走全局 columnNameCache（&u.Name 是规范单例字段）
	colU, err := sub.resolveColumnName(uintptrOf(&u.Name))
	if err != nil {
		t.Fatalf("expected sub to resolve outer canonical singleton, got %v", err)
	}
	// 因 sub.aliases 非空（有 orders 主表 alias），resolveColumnName 顶层 fallback 会加主表前缀
	// 但这是 sub 视角——sub 的"主表"是 orders，u 的字段不应该被 sub 视为主表
	// 实际行为：u 走 sub 沿链查找，到 q（顶层 q.aliases 为空），最终 fallback 全局
	// 全局命中 username（u.Name 的 column tag），返回什么取决于 resolveColumnName 实现细节
	if !strings.Contains(colU, "username") {
		t.Errorf("expected username (from gorm column tag) in colU, got %s", colU)
	}

	// sub 自己的 alias 字段
	colO, err := sub.resolveColumnName(uintptrOf(&o.UserID))
	if err != nil {
		t.Fatalf("sub alias resolve: %v", err)
	}
	if colO != "orders.user_id" {
		t.Errorf("expected orders.user_id, got %s", colO)
	}
}
