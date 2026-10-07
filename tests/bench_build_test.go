package gplus_test

import (
	"context"
	"testing"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
)

func BenchmarkQuery_DryRunBuild(b *testing.B) {
	_, db := setupBenchDB(b)
	ctx := context.Background()
	dryDB := db.Session(&gorm.Session{DryRun: true})
	for _, kind := range []string{"Plain", "AliasGroup", "CorrelatedSubquery"} {
		b.Run(kind, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var q *Query[TestUser]
				var m *TestUser
				if kind == "AliasGroup" {
					q, m = NewQueryAs[TestUser](ctx, "u")
					q.And(func(s *Query[TestUser]) {
						s.Eq(&m.Name, "Alice").Or(func(n *Query[TestUser]) { n.Gt(&m.Age, 20) })
					})
				} else {
					q, m = NewQuery[TestUser](ctx)
				}
				if kind == "CorrelatedSubquery" {
					sub, o := SubQuery[Order](q)
					sub.Select(&o.ID).WhereRaw("orders.user_id = test_users.id").Gt(&o.ID, 0)
					q.Exists(sub)
				} else if kind == "Plain" {
					q.Eq(&m.Name, "Alice").Gt(&m.Age, 20)
				}
				var rows []TestUser
				tx := dryDB.Model(new(TestUser)).Scopes(q.BuildQuery()).Find(&rows)
				if tx.Error != nil || tx.Statement.SQL.Len() == 0 {
					b.Fatalf("error=%v, SQL=%s", tx.Error, tx.Statement.SQL.String())
				}
			}
		})
	}
}
