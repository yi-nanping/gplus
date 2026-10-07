package gplus_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	. "github.com/yi-nanping/gplus"
)

func TestAliasGroup_QueryAndOr(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	if err := db.Create(&[]TestUser{{Name: "Alice", Age: 30}, {Name: "Bob", Age: 20}}).Error; err != nil {
		t.Fatal(err)
	}
	for _, useOr := range []bool{false, true} {
		t.Run(map[bool]string{false: "And", true: "Or"}[useOr], func(t *testing.T) {
			q, m := NewQueryAs[TestUser](context.Background(), "u")
			group := func(s *Query[TestUser]) {
				s.Eq(&m.Name, "Alice").And(func(nested *Query[TestUser]) { nested.Gt(&m.Age, 25) })
			}
			if useOr {
				q.Eq(&m.Name, "absent").Or(group)
			} else {
				q.And(group)
			}
			rows, err := repo.List(q)
			if err != nil || len(rows) != 1 || rows[0].Name != "Alice" {
				t.Fatalf("rows=%v, err=%v", rows, err)
			}
		})
	}
}

func TestAliasGroup_UpdaterAndOr(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	for _, useOr := range []bool{false, true} {
		t.Run(map[bool]string{false: "And", true: "Or"}[useOr], func(t *testing.T) {
			u, m := NewUpdater[TestUser](context.Background())
			a := As[TestUser](u, "a")
			u.Set(&m.Age, 30)
			group := func(s *Updater[TestUser]) {
				s.Eq(&a.Name, "Alice").And(func(nested *Updater[TestUser]) { nested.Gt(&a.Age, 25) })
			}
			if useOr {
				u.Eq(&m.Name, "absent").Or(group)
			} else {
				u.And(group)
			}
			sql, err := u.ToSQL(db)
			if err != nil || !strings.Contains(sql, "a") || !strings.Contains(sql, "Alice") {
				t.Fatalf("SQL=%q, err=%v", sql, err)
			}
		})
	}
}

func TestAliasGroup_RevokedAliasRejected(t *testing.T) {
	q, m := NewQueryAs[TestUser](context.Background(), "u")
	q.Clear()
	q.And(func(s *Query[TestUser]) { s.Eq(&m.Name, "Alice") })
	if !errors.Is(q.GetError(), ErrAliasRevoked) {
		t.Fatalf("expected revoked alias error, got %v", q.GetError())
	}
}
