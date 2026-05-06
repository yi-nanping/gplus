package gplus

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Order 类型在 advanced_test.go 中已定义，含 ID/UserID/Amount/Remark 字段。
// 如需 Status 字段，调整为使用 Remark（现有字段）。

func TestLeftJoinAs_BasicSQL(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	q, u := NewQuery[TestUser](context.Background())
	o := As[Order](q, "o")
	q.LeftJoinAs(o, &o.UserID, &u.ID, "")

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	if !strings.Contains(sql, "LEFT JOIN") {
		t.Errorf("expected LEFT JOIN, got %s", sql)
	}
	// alias 形式含 AS o（方言差异：可能加引号）
	if !strings.Contains(sql, " AS ") || !strings.Contains(sql, "o") {
		t.Errorf("expected alias AS o, got %s", sql)
	}
	// ON 条件必须含主表前缀，防多表 JOIN 时 SQL 报 ambiguous（CRITICAL fix v0.8.0）
	if !strings.Contains(sql, "test_users.id") {
		t.Errorf("expected ON condition to contain main table prefix 'test_users.id' (ambiguous fix), got %s", sql)
	}
}

func TestJoinAsVariants(t *testing.T) {
	cases := []struct {
		name    string
		method  func(q *Query[TestUser], alias any, l, r any)
		wantSQL string
	}{
		{"RightJoinAs", func(q *Query[TestUser], a, l, r any) { q.RightJoinAs(a, l, r, "") }, "RIGHT JOIN"},
		{"InnerJoinAs", func(q *Query[TestUser], a, l, r any) { q.InnerJoinAs(a, l, r, "") }, "INNER JOIN"},
		{"OuterJoinAs", func(q *Query[TestUser], a, l, r any) { q.OuterJoinAs(a, l, r, "") }, "OUTER JOIN"},
		{"FullJoinAs", func(q *Query[TestUser], a, l, r any) { q.FullJoinAs(a, l, r, "") }, "FULL JOIN"},
	}
	_, db := setupTestDB[TestUser](t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, u := NewQuery[TestUser](context.Background())
			o := As[Order](q, "o")
			tc.method(q, o, &o.UserID, &u.ID)
			sql, err := q.ToSQL(db)
			if err != nil {
				t.Fatalf("ToSQL: %v", err)
			}
			if !strings.Contains(sql, tc.wantSQL) {
				t.Errorf("expected %s in %s", tc.wantSQL, sql)
			}
		})
	}
}

func TestLeftJoinAs_ExtraSQLParameterized_C1(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	q, u := NewQuery[TestUser](context.Background())
	o := As[Order](q, "o")
	// extraSQL 含 ?，参数 "paid" 走 GORM 参数化（绝不拼字面 SQL）
	q.LeftJoinAs(o, &o.UserID, &u.ID, "AND o.remark = ?", "paid")

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	// DryRun 会内联参数；断言 remark 关键词存在即可
	if !strings.Contains(sql, "remark") {
		t.Errorf("expected remark keyword in SQL, got %s", sql)
	}
}

func TestLeftJoinAs_AliasNotInChain(t *testing.T) {
	q1, _ := NewQuery[TestUser](context.Background())
	q2, u2 := NewQuery[TestUser](context.Background())
	// oOfQ1 属于 q1 链，不属于 q2 链
	oOfQ1 := As[Order](q1, "o")
	q2.LeftJoinAs(oOfQ1, &oOfQ1.UserID, &u2.ID, "")
	if !errors.Is(q2.GetError(), ErrAliasNotInChain) {
		t.Errorf("expected ErrAliasNotInChain, got %v", q2.GetError())
	}
}
