package gplus_test

import (
	"context"
	"errors"
	"testing"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
)

func TestDebug_ScopeErrorPropagates(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	want := errors.New("scope rejected query")
	scope := func(tx *gorm.DB) *gorm.DB {
		_ = tx.AddError(want)
		return tx
	}
	for _, kind := range []string{"select", "count", "update"} {
		t.Run(kind, func(t *testing.T) {
			var sql string
			var err error
			if kind == "update" {
				u, m := NewUpdater[TestUser](context.Background())
				sql, err = u.Set(&m.Age, 30).Eq(&m.ID, 1).WithScope(scope).ToSQL(db)
			} else {
				q, m := NewQuery[TestUser](context.Background())
				q.Eq(&m.ID, 1).WithScope(scope)
				if kind == "count" {
					sql, err = q.ToCountSQL(db)
				} else {
					sql, err = q.ToSQL(db)
				}
			}
			if sql != "" || !errors.Is(err, want) {
				t.Fatalf("SQL=%q, error=%v; expected scope error", sql, err)
			}
		})
	}
}
