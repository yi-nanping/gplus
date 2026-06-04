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

// AC-5：Clear 后 mainAlias/mainAliasTable 重置，FROM 不含别名
func TestMainAlias_clear_resets_alias_fields(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	q, _ := repo.NewQueryAs(ctx, "ext")

	q.Clear()

	if q.mainAlias != "" || q.mainAliasTable != "" {
		t.Fatalf("Clear 后 mainAlias/mainAliasTable 应为空，实际 mainAlias=%q mainAliasTable=%q", q.mainAlias, q.mainAliasTable)
	}
	sql, _ := q.ToSQL(db)
	if strings.Contains(sql, `AS "ext"`) {
		t.Errorf("Clear 后 FROM 不应含 AS \"ext\"，实际: %s", sql)
	}
}

// AC-4：NewQueryAs + Table("closure_2024") → FROM closure_2024 AS ext（Table 覆盖优先）
func TestMainAlias_table_override_uses_custom_table(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	// 建 closure_2024 表（同 schema）并播种
	if err := db.Exec(`CREATE TABLE closure_2024 (id integer PRIMARY KEY AUTOINCREMENT, ancestor_id integer, descendant_id integer, depth integer)`).Error; err != nil {
		t.Fatalf("create closure_2024: %v", err)
	}
	if err := db.Exec(`INSERT INTO closure_2024 (ancestor_id, descendant_id, depth) VALUES (7,5,0)`).Error; err != nil {
		t.Fatalf("seed closure_2024: %v", err)
	}

	q, m := repo.NewQueryAs(ctx, "ext")
	q.Table("closure_2024").Select(&m.AncestorID).Eq(&m.DescendantID, 5)

	list, err := repo.List(q)
	if err != nil {
		t.Fatalf("List 应成功，实际: %v", err)
	}
	if len(list) != 1 || list[0].AncestorID != 7 {
		t.Fatalf("期望 1 行 AncestorID=7，实际 %+v", list)
	}
	sql, _ := q.ToSQL(db)
	if !strings.Contains(sql, `closure_2024" AS "ext`) {
		t.Errorf("FROM 应含 closure_2024 AS ext，实际: %s", sql)
	}
}

// AC-7：SubQuery 派生的子查询不被主别名物化波及（C-1 回归守卫）
func TestMainAlias_subquery_from_not_polluted(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	outer, ou := repo.NewQuery(ctx)
	sub, su := SubQuery[Closure](outer)
	sub.Select(&su.AncestorID).Eq(&su.DescendantID, 5)
	outer.InSub(&ou.AncestorID, sub)

	sql, _ := outer.ToSQL(db)
	if strings.Contains(sql, "closure AS closure") || strings.Contains(sql, `closure" AS "closure`) {
		t.Errorf("子查询 FROM 不应被主别名物化为 closure AS closure，实际: %s", sql)
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
