package gplus

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"
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

// AC-6：自连接主+副别名真实执行（scenario 2 源 query 形态），FindAs 投影
func TestMainAlias_selfjoin_executes_and_projects(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	for _, c := range []Closure{{AncestorID: 1, DescendantID: 5, Depth: 0}, {AncestorID: 5, DescendantID: 7, Depth: 0}} {
		if err := repo.Save(ctx, &c); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	q, ext := repo.NewQueryAs(ctx, "ext")
	sub := As[Closure](q, "sub")
	q.CrossJoinAs(sub).
		WhereRaw("sub.ancestor_id = ?", 5).
		Eq(&ext.DescendantID, 5)
	q.SelectRaw("ext.ancestor_id").
		SelectRaw("sub.descendant_id").
		SelectRaw("ext.depth + sub.depth + 1 AS depth")

	// 投影 DTO：字段经 GORM snake_case 映射 ancestor_id/descendant_id/depth
	type projRow struct {
		AncestorID   uint
		DescendantID uint
		Depth        uint
	}
	var rows []projRow
	if err := FindAs[Closure, projRow](repo, q, &rows); err != nil {
		t.Fatalf("FindAs 应成功，实际: %v", err)
	}
	if len(rows) != 1 || rows[0] != (projRow{AncestorID: 1, DescendantID: 7, Depth: 1}) {
		t.Fatalf("期望 1 行 {1,7,1}，实际 %+v", rows)
	}

	sql, _ := q.ToSQL(db)
	// FROM 主表带引号；CROSS JOIN 副表不带引号（appendJoinAsNoOn 生成）
	if !strings.Contains(sql, `closure" AS "ext`) {
		t.Errorf("FROM 应含带引号主别名 closure AS ext，实际: %s", sql)
	}
	if !strings.Contains(sql, "CROSS JOIN closure AS sub") {
		t.Errorf("JOIN 应含不带引号副别名 closure AS sub，实际: %s", sql)
	}
}

// AC-8：主别名 Query 传 DeleteByCondTx 走 BuildDelete，不物化别名（结构性禁止）
// BuildDelete 只调用 applyWhere，不调用 applyMainAlias，因此 FROM 不含 AS ext。
// 通过 db.ToSQL（DryRun 等价）验证 SQL 形态——不真实执行，
// 因为 WHERE 字段仍带别名前缀（ext.descendant_id），属已知限制。
func TestMainAlias_delete_path_no_materialize(t *testing.T) {
	_, db := setupTestDB[Closure](t)
	ctx := context.Background()

	q, m := NewQueryAs[Closure](ctx, "ext")
	q.Eq(&m.DescendantID, 5)

	var model Closure
	deleteSQL := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return tx.WithContext(ctx).Model(&model).Scopes(q.BuildDelete()).Delete(&model)
	})
	// FROM 不含 AS ext（BuildDelete 不调用 applyMainAlias）
	if strings.Contains(deleteSQL, `AS "ext"`) || strings.Contains(deleteSQL, "AS ext") {
		t.Errorf("DELETE SQL FROM 不应含 AS ext，实际: %s", deleteSQL)
	}
	if !strings.Contains(deleteSQL, "DELETE FROM") {
		t.Errorf("应生成 DELETE FROM 语句，实际: %s", deleteSQL)
	}
}

// AC-9：主别名 + DataRule 注入裸列 depth + 自连接 → ambiguous（已知限制）
func TestMainAlias_datarule_selfjoin_ambiguous(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	_ = db
	rules := []DataRule{{Column: "depth", Condition: ">=", Value: "0"}}
	ctx := context.WithValue(context.Background(), DataRuleKey, rules)
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	q, ext := repo.NewQueryAs(ctx, "ext")
	sub := As[Closure](q, "sub")
	q.CrossJoinAs(sub).WhereRaw("sub.ancestor_id = ?", 1).Eq(&ext.DescendantID, 5)
	q.SelectRaw("ext.ancestor_id")

	_, err := repo.List(q)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "ambiguous") {
		t.Fatalf("期望 ambiguous 错误（DataRule 裸列 depth 在自连接下歧义），实际 err: %v", err)
	}
}

// AC-11：主别名查询走 First 路径（GetOne）因 GORM 自动 ORDER BY closure.id 裸表名被别名遮蔽而失败（已知限制）
func TestMainAlias_first_path_not_supported(t *testing.T) {
	repo, _ := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	q, m := repo.NewQueryAs(ctx, "ext")
	q.Eq(&m.DescendantID, 5)

	_, err := repo.GetOne(q)
	if err == nil || !strings.Contains(err.Error(), "no such column") || !strings.Contains(err.Error(), "closure.id") {
		t.Fatalf("期望 First 路径报 no such column: closure.id（已知限制），实际 err: %v", err)
	}
}

// M-1：validTableName 注入守卫表驱动单测
func TestMainAlias_validTableName_guard(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"裸表名", "closure", true},
		{"带数字下划线", "closure_2024", true},
		{"单点 schema.table", "main.closure", true},
		{"引号闭合注入", `x"; DROP TABLE closure; --`, false},
		{"AS 注入", "closure AS evil", false},
		{"空格", "clo sure", false},
		{"空串", "", false},
		{"数字开头", "2closure", false},
		{"多段 a.b.c 不支持", "a.b.c", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validTableName.MatchString(tc.input); got != tc.want {
				t.Errorf("validTableName(%q) = %v，期望 %v", tc.input, got, tc.want)
			}
		})
	}
}

// AC-10：Count/Page 路径（BuildCount）主别名物化，total 与 list 表名一致
func TestMainAlias_count_and_page_materialize_alias(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	for _, c := range []Closure{{AncestorID: 1, DescendantID: 5, Depth: 0}, {AncestorID: 2, DescendantID: 5, Depth: 0}} {
		if err := repo.Save(ctx, &c); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Count 路径
	q1, m1 := repo.NewQueryAs(ctx, "ext")
	q1.Eq(&m1.DescendantID, 5)
	total, err := repo.Count(q1)
	if err != nil {
		t.Fatalf("Count 应成功，实际: %v", err)
	}
	if total != 2 {
		t.Fatalf("Count 期望 2，实际 %d", total)
	}
	countSQL, _ := q1.ToCountSQL(db)
	if !strings.Contains(countSQL, `closure" AS "ext`) {
		t.Errorf("ToCountSQL FROM 应含 closure AS ext，实际: %s", countSQL)
	}

	// Page 路径（COUNT 段 + 数据段一致）
	q2, m2 := repo.NewQueryAs(ctx, "ext")
	q2.Eq(&m2.DescendantID, 5)
	list, pageTotal, err := repo.Page(q2, false)
	if err != nil {
		t.Fatalf("Page 应成功，实际: %v", err)
	}
	if pageTotal != 2 || len(list) != 2 {
		t.Fatalf("Page 期望 total=2 len=2，实际 total=%d len=%d", pageTotal, len(list))
	}
}
