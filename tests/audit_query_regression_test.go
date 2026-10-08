package gplus_test

import (
	"context"
	"errors"
	"testing"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
)

type auditFixRow struct {
	ID       int `gorm:"primaryKey"`
	TenantID int
	Age      int
	Name     string
}

func setupAuditFixDB(t *testing.T) (*gorm.DB, *Repository[int, auditFixRow]) {
	t.Helper()
	db := openDB(t)
	if err := db.AutoMigrate(&auditFixRow{}); err != nil {
		t.Fatal(err)
	}
	truncateTables(t, db, &auditFixRow{})
	r := NewRepository[int, auditFixRow](db)
	if err := r.SaveBatch(context.Background(), []auditFixRow{
		{ID: 1, TenantID: 1, Age: 20, Name: "A"},
		{ID: 2, TenantID: 1, Age: 20, Name: "A"},
		{ID: 3, TenantID: 2, Age: 30, Name: "B"},
	}); err != nil {
		t.Fatal(err)
	}
	return db, r
}

func TestNestedSubqueryError_StopsReadsAndWrites(t *testing.T) {
	for _, groupedOr := range []bool{false, true} {
		for _, withRule := range []bool{false, true} {
			t.Run(testBoolName(groupedOr)+"/"+testBoolName(withRule), func(t *testing.T) {
				_, r := setupAuditFixDB(t)
				ctx := context.Background()
				if withRule {
					ctx = context.WithValue(ctx, DataRuleKey, []DataRule{{Column: "tenant_id", Condition: "=", Value: "1"}})
				}
				sub, sm := NewQuery[auditFixRow](ctx)
				sub.Select(&sm.ID).Eq(&sm.ID, 1).WhereRaw("")
				q, m := NewQuery[auditFixRow](ctx)
				group := func(g *Query[auditFixRow]) { g.NotInSub(&m.ID, sub) }
				if groupedOr {
					q.Or(group)
				} else {
					q.And(group)
				}
				if rows, err := r.List(q); err == nil || len(rows) != 0 {
					t.Fatalf("invalid nested subquery: rows=%v err=%v", rows, err)
				}
				u, um := NewUpdater[auditFixRow](ctx)
				u.Set(&um.Name, "changed")
				ugroup := func(g *Updater[auditFixRow]) { g.NotInSub(&um.ID, sub) }
				if groupedOr {
					u.Or(ugroup)
				} else {
					u.And(ugroup)
				}
				if n, err := r.UpdateByCond(u); err == nil || n != 0 {
					t.Fatalf("invalid update affected=%d err=%v", n, err)
				}
				rows, err := r.GetByIds(context.Background(), []int{1, 2, 3})
				if err != nil || len(rows) != 3 {
					t.Fatalf("readback=%v err=%v", rows, err)
				}
				for _, row := range rows {
					if row.Name == "changed" {
						t.Fatalf("invalid builder changed stored row: %+v", row)
					}
				}
			})
		}
	}
}

func testBoolName(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func TestRawBindings_QueryAndUpdater(t *testing.T) {
	for _, useOr := range []bool{false, true} {
		for _, tc := range []struct {
			name, sql string
			arg       any
			want      int
		}{
			// 显式类型使 PostgreSQL 能推断 nil 参数，仍验证占位符绑定。
			{"nil", "CAST(? AS CHAR) IS NULL", nil, 3},
			{"slice", "id IN (?)", []any{1, 2}, 2},
		} {
			t.Run(tc.name+"/or="+testBoolName(useOr), func(t *testing.T) {
				_, r := setupAuditFixDB(t)
				q, m := NewQuery[auditFixRow](context.Background())
				if useOr {
					q.Eq(&m.ID, -1).OrWhereRaw(tc.sql, tc.arg)
				} else {
					q.WhereRaw(tc.sql, tc.arg)
				}
				rows, err := r.List(q)
				if err != nil || len(rows) != tc.want {
					t.Fatalf("rows=%v want=%d err=%v", rows, tc.want, err)
				}
				u, um := NewUpdater[auditFixRow](context.Background())
				u.Set(&um.Name, "changed")
				if useOr {
					u.Eq(&um.ID, -1).OrWhereRaw(tc.sql, tc.arg)
				} else {
					u.WhereRaw(tc.sql, tc.arg)
				}
				if n, err := r.UpdateByCond(u); err != nil || n != int64(tc.want) {
					t.Fatalf("affected=%d want=%d err=%v", n, tc.want, err)
				}
				check, cm := NewQuery[auditFixRow](context.Background())
				check.Eq(&cm.Name, "changed")
				changed, err := r.List(check)
				if err != nil || len(changed) != tc.want {
					t.Fatalf("stored changed rows=%v err=%v", changed, err)
				}
			})
		}
	}
}

func TestRawBindings_DBSubquery(t *testing.T) {
	db, r := setupAuditFixDB(t)
	sub, sm := NewQuery[auditFixRow](context.Background())
	sub.Select(&sm.ID).Eq(&sm.ID, 2)
	q, _ := NewQuery[auditFixRow](context.Background())
	q.WhereRaw("id IN (?)", sub.ToDB(db))
	rows, err := r.List(q)
	if err != nil || len(rows) != 1 || rows[0].ID != 2 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}

func TestAliasColumn_PlucksAndAggregates(t *testing.T) {
	_, r := setupAuditFixDB(t)
	q, m := NewQueryAs[auditFixRow](context.Background(), "m")
	values, err := Pluck[auditFixRow, int, int](r, q, &m.Age)
	if err != nil || len(values) != 3 {
		t.Fatalf("pluck=%v err=%v", values, err)
	}
	sum, sumErr := Sum[auditFixRow, int64, int](r, q, &m.Age)
	max, maxErr := Max[auditFixRow, int64, int](r, q, &m.Age)
	min, minErr := Min[auditFixRow, int64, int](r, q, &m.Age)
	avg, avgErr := Avg[auditFixRow, float64, int](r, q, &m.Age)
	if sumErr != nil || maxErr != nil || minErr != nil || avgErr != nil || sum != 70 || max != 30 || min != 20 || avg < 23.33 || avg > 23.34 {
		t.Fatalf("sum=%v/%v max=%v/%v min=%v/%v avg=%v/%v", sum, sumErr, max, maxErr, min, minErr, avg, avgErr)
	}
	revoked, old := NewQueryAs[auditFixRow](context.Background(), "old")
	revoked.Clear()
	if _, err := Pluck[auditFixRow, int, int](r, revoked, &old.Age); !errors.Is(err, ErrAliasRevoked) {
		t.Fatalf("revoked alias err=%v", err)
	}
}
