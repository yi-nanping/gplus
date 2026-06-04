package gplus

import (
	"context"
	"sort"
	"strings"
	"testing"
)

// AC-6：无参 SelectRaw 字节级不变
func TestSelectRaw_noargs_sql_unchanged(t *testing.T) {
	db := newDryRunDB(t)
	q, _ := NewQuery[TestUser](context.Background())
	q.SelectRaw("COUNT(*) AS cnt")
	sql, _ := buildSQL(t, db, q)
	want := "SELECT COUNT(*) AS \"cnt\" FROM `test_users`"
	if sql != want {
		t.Errorf("SQL 回归:\n want=%s\n got =%s", want, sql)
	}
}

// AC-7：Select 字段指针字节级不变（逗号无空格）
func TestSelect_pointers_sql_unchanged(t *testing.T) {
	db := newDryRunDB(t)
	q, u := NewQuery[TestUser](context.Background())
	q.Select(&u.Name, &u.Age)
	sql, _ := buildSQL(t, db, q)
	want := "SELECT \"username\",\"age\" FROM `test_users`"
	if sql != want {
		t.Errorf("SQL 回归:\n want=%s\n got =%s", want, sql)
	}
}

// AC-10：Distinct 无 args 字节级不变
func TestDistinct_noargs_sql_unchanged(t *testing.T) {
	db := newDryRunDB(t)
	q, u := NewQuery[TestUser](context.Background())
	q.Distinct(&u.Age)
	sql, _ := buildSQL(t, db, q)
	want := "SELECT DISTINCT \"age\" FROM `test_users`"
	if sql != want {
		t.Errorf("SQL 回归:\n want=%s\n got =%s", want, sql)
	}
}

// AC-1：单参绑定，值正确（age=18,19,20 → age+1=19,20,21）
func TestSelectRaw_binds_single_arg_value(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	for _, age := range []int{18, 19, 20} {
		if err := repo.Save(ctx, &TestUser{Age: age}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	q, _ := repo.NewQuery(ctx)
	q.SelectRaw("age + ?", 1)
	var got []int
	if err := db.Model(&TestUser{}).Scopes(q.DataRuleBuilder().BuildQuery()).Scan(&got).Error; err != nil {
		t.Fatalf("scan: %v", err)
	}
	sort.Ints(got)
	want := []int{19, 20, 21}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("期望 %v，实际 %v", want, got)
	}
}

// AC-2：单参走 Vars 而非字面量
func TestSelectRaw_single_arg_into_vars(t *testing.T) {
	db := newDryRunDB(t)
	q, _ := NewQuery[TestUser](context.Background())
	q.SelectRaw("age + ?", 1)
	sql, vars := buildSQL(t, db, q)
	assertSQL(t, sql, "age + ?")
	if len(vars) != 1 || vars[0] != 1 {
		t.Errorf("Vars 期望 [1]，实际 %v", vars)
	}
}

// AC-3：多 SelectRaw args 顺序 = 调用顺序
func TestSelectRaw_multi_args_order(t *testing.T) {
	db := newDryRunDB(t)
	q, _ := NewQuery[TestUser](context.Background())
	q.SelectRaw("?", 7).SelectRaw("age + ?", 100)
	_, vars := buildSQL(t, db, q)
	if len(vars) != 2 || vars[0] != 7 || vars[1] != 100 {
		t.Errorf("Vars 期望 [7 100]，实际 %v", vars)
	}
}

// AC-4：混用 Select 指针 + SelectRaw(args)，交错顺序 + SELECT 子句字面（单串路径，逗号带空格）
func TestSelectRaw_mixed_with_select_pointers(t *testing.T) {
	db := newDryRunDB(t)
	q, u := NewQuery[TestUser](context.Background())
	q.Select(&u.ID).SelectRaw("?", 9).Select(&u.Age)
	sql, vars := buildSQL(t, db, q)
	want := "SELECT \"id\", ?, \"age\" FROM `test_users`"
	if sql != want {
		t.Errorf("SQL:\n want=%s\n got =%s", want, sql)
	}
	if len(vars) != 1 || vars[0] != 9 {
		t.Errorf("Vars 期望 [9]，实际 %v", vars)
	}
}

// AC-5：裸 ? 不被 quoteColumn 误转义成 "?"
func TestSelectRaw_bare_placeholder_not_quoted(t *testing.T) {
	db := newDryRunDB(t)
	q, _ := NewQuery[TestUser](context.Background())
	q.SelectRaw("?", 5)
	sql, _ := buildSQL(t, db, q)
	if strings.Contains(sql, "\"?\"") {
		t.Errorf("裸 ? 被错误转义为 \"?\":\n %s", sql)
	}
	assertSQL(t, sql, "SELECT ?")
}

// AC-13：单 expr 多占位符展开顺序（age + 5 - 2 = age+3）
func TestSelectRaw_single_expr_multi_placeholder(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	for _, age := range []int{18, 19, 20} {
		if err := repo.Save(ctx, &TestUser{Age: age}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	q, _ := repo.NewQuery(ctx)
	q.SelectRaw("age + ? - ?", 5, 2)
	var got []int
	if err := db.Model(&TestUser{}).Scopes(q.DataRuleBuilder().BuildQuery()).Scan(&got).Error; err != nil {
		t.Fatalf("scan: %v", err)
	}
	sort.Ints(got)
	want := []int{21, 22, 23}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("期望 %v（age+3），实际 %v", want, got)
	}
}

// AC-8：空 expr 报错且不污染 selects
func TestSelectRaw_empty_expr_errors(t *testing.T) {
	db := newDryRunDB(t)
	q, _ := NewQuery[TestUser](context.Background())
	q.SelectRaw("", 1)
	if q.GetError() == nil {
		t.Fatal("空 expr 应累积错误")
	}
	_, vars := buildSQL(t, db, q)
	if len(vars) != 0 {
		t.Errorf("空 expr 不应留下 Vars，实际 %v", vars)
	}
}

// AC-9：Clear() 重置 args，后续 BuildQuery 无残留投影/Vars
func TestSelectRaw_clear_resets_args(t *testing.T) {
	db := newDryRunDB(t)
	q, _ := NewQuery[TestUser](context.Background())
	q.SelectRaw("age + ?", 1)
	q.Clear()
	sql, vars := buildSQL(t, db, q)
	if strings.Contains(sql, "age + ?") {
		t.Errorf("Clear 后不应残留投影:\n %s", sql)
	}
	if len(vars) != 0 {
		t.Errorf("Clear 后 Vars 应为空，实际 %v", vars)
	}
}

// AC-12：Distinct + SelectRaw(args) 必须保留 DISTINCT 且 Vars 正确
func TestSelectRaw_with_distinct_keeps_distinct(t *testing.T) {
	db := newDryRunDB(t)
	q, u := NewQuery[TestUser](context.Background())
	q.Distinct(&u.Age).SelectRaw("age + ?", 1)
	sql, vars := buildSQL(t, db, q)
	assertSQL(t, sql, "DISTINCT", "age + ?")
	if len(vars) != 1 || vars[0] != 1 {
		t.Errorf("Vars 期望 [1]，实际 %v", vars)
	}
}
