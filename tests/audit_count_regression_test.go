package gplus_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
)

func TestDistinctCount_PageAndProjection(t *testing.T) {
	for _, kind := range []string{"one", "multi", "all", "join_all", "group", "bound_select"} {
		t.Run(kind, func(t *testing.T) {
			db, r := setupAuditFixDB(t)
			q, m := NewQuery[auditFixRow](context.Background())
			want := int64(2)
			switch kind {
			case "one":
				q.Eq(&m.Age, 20).Distinct(&m.Age)
				want = 1
			case "multi":
				q.Distinct(&m.Age, &m.Name)
			case "all":
				q.Distinct()
				want = 3
			case "join_all":
				q.LeftJoin("audit_fix_rows AS dup", "audit_fix_rows.tenant_id = dup.tenant_id").Distinct()
				want = 3
			case "group":
				q.Distinct(&m.Age).Group(&m.Age).Having("age", OpGe, 20)
			case "bound_select":
				q.SelectRaw("age + ? AS age", 1).Distinct()
			}
			q.Page(1, 1)
			if n, err := r.Count(q); err != nil || n != want {
				t.Fatalf("Count=%d want=%d err=%v", n, want, err)
			}
			rows, total, err := r.Page(q, false)
			if err != nil || total != want || len(rows) != 1 {
				t.Fatalf("Page rows=%v total=%d want=%d err=%v", rows, total, want, err)
			}
			var projected []auditFixRow
			if n, err := PageAs(r, q, &projected, false); err != nil || n != want || len(projected) != 1 {
				t.Fatalf("PageAs rows=%v total=%d want=%d err=%v", projected, n, want, err)
			}
			sql, err := q.ToCountSQL(db)
			if err != nil || !strings.Contains(strings.ToLower(sql), "count(") || strings.Contains(sql, "LIMIT 1") {
				t.Fatalf("count SQL=%s err=%v", sql, err)
			}
		})
	}
}

func TestDistinctCount_SoftDelete(t *testing.T) {
	r, db := setupTenantDB(t)
	aliceID, _ := insertTenantUsers(t, db)
	if n, err := r.DeleteById(context.Background(), aliceID); err != nil || n != 1 {
		t.Fatalf("delete affected=%d err=%v", n, err)
	}
	q, m := NewQuery[tenantUser](context.Background())
	q.Distinct(&m.TenantID)
	if n, err := r.Count(q); err != nil || n != 1 {
		t.Fatalf("soft-deleted row counted: count=%d err=%v", n, err)
	}
	q.Unscoped()
	if n, err := r.Count(q); err != nil || n != 2 {
		t.Fatalf("Unscoped count=%d err=%v", n, err)
	}
}

func TestDistinctCount_CallbackContextRulesAndTx(t *testing.T) {
	db, r := setupAuditFixDB(t)
	ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{{Column: "tenant_id", Condition: "=", Value: "1"}})
	calls, rowCalls := 0, 0
	if err := db.Callback().Query().Before("gorm:query").Register("audit:distinct_count", func(tx *gorm.DB) {
		calls++
		if tx.Statement.Context != ctx || reflect.TypeOf(tx.Statement.Model) != reflect.TypeOf(new(auditFixRow)) {
			t.Errorf("callback model/context mismatch: %T", tx.Statement.Model)
		}
		tx.Where("name = ?", "A")
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().Before("gorm:row").Register("audit:distinct_row", func(tx *gorm.DB) { rowCalls++ }); err != nil {
		t.Fatal(err)
	}
	q, m := NewQuery[auditFixRow](ctx)
	q.Eq(&m.Name, "A").Distinct(&m.Age).Page(1, 1).WithScope(func(tx *gorm.DB) *gorm.DB { return tx.Or("tenant_id = ?", 2) })
	err := db.Transaction(func(tx *gorm.DB) error {
		var rows []auditFixRow
		n, err := PageAsTx(r, q, &rows, false, tx)
		if err != nil || n != 1 || len(rows) != 1 || rows[0].Age != 20 {
			t.Fatalf("rows=%v total=%d err=%v", rows, n, err)
		}
		return nil
	})
	if err != nil || calls != 2 || rowCalls != 0 {
		t.Fatalf("err=%v query callbacks=%d row callbacks=%d", err, calls, rowCalls)
	}
}

func TestDistinctAggregate_KeepsAggregateProjection(t *testing.T) {
	_, r := setupAuditFixDB(t)
	q, m := NewQuery[auditFixRow](context.Background())
	q.Distinct(&m.Age)
	n, err := Sum[auditFixRow, int64, int](r, q, &m.Age)
	if err != nil || n != 70 {
		t.Fatalf("Sum=%d want=70 err=%v", n, err)
	}
}

func TestDistinctCount_CanceledContext(t *testing.T) {
	_, r := setupAuditFixDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	q, m := NewQuery[auditFixRow](ctx)
	q.Distinct(&m.Age)
	if _, err := r.Count(q); !errors.Is(err, context.Canceled) {
		t.Fatalf("Count error=%v want canceled", err)
	}
}
