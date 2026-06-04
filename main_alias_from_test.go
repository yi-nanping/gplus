package gplus

import (
	"context"
	"strings"
	"testing"
)

// AC-1：NewQueryAs 主别名查询经 List 真实执行，FROM 物化为 closure AS ext
func TestMainAlias_list_with_alias_emits_from_as(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	q, m := repo.NewQueryAs(ctx, "ext")
	q.Select(&m.AncestorID).Eq(&m.DescendantID, 5)

	list, err := repo.List(q)
	if err != nil {
		t.Fatalf("List 应成功，实际 err: %v", err)
	}
	if len(list) != 1 || list[0].AncestorID != 1 {
		t.Fatalf("期望 1 行 AncestorID=1，实际 %+v", list)
	}

	sql, _ := q.ToSQL(db)
	if !strings.Contains(sql, `closure" AS "ext`) {
		t.Errorf("FROM 应含 closure AS ext，实际 SQL: %s", sql)
	}
}

// AC-2：q.ToDB(db) 物化路径真实执行，FROM 含别名
func TestMainAlias_todb_materializes_alias(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	q, m := repo.NewQueryAs(ctx, "ext")
	q.Select(&m.AncestorID).Eq(&m.DescendantID, 5)

	var rows []Closure
	if err := q.ToDB(db).Find(&rows).Error; err != nil {
		t.Fatalf("ToDB Find 应成功，实际 err: %v", err)
	}
	if len(rows) != 1 || rows[0].AncestorID != 1 {
		t.Fatalf("期望 1 行 AncestorID=1，实际 %+v", rows)
	}
}

// AC-3：无别名查询 FROM 为裸表名，不含 AS（零回归）
func TestMainAlias_no_alias_from_has_no_as(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	q, m := repo.NewQuery(ctx)
	q.Select(&m.AncestorID)

	sql, _ := q.ToSQL(db)
	if !strings.Contains(sql, "FROM `closure`") {
		t.Errorf("无别名 FROM 应为裸表名 `closure`，实际: %s", sql)
	}
	if strings.Contains(sql, " AS ") {
		t.Errorf("无别名查询 FROM 不应含 AS，实际: %s", sql)
	}
	if _, err := repo.List(q); err != nil {
		t.Fatalf("无别名 List 应成功，实际: %v", err)
	}
}
