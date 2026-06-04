package gplus

import (
	"context"
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
