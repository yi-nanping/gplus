package gplus

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

func BenchmarkAliasLookup_FiveAliases(b *testing.B) {
	q, _ := NewQuery[TestUser](context.Background())
	_ = As[Order](q, "o1")
	_ = As[Order](q, "o2")
	_ = As[Order](q, "o3")
	_ = As[Order](q, "o4")
	o := As[Order](q, "o5")
	addr := uintptrOf(&o.UserID)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = q.core.lookupAddr(addr)
	}
}

func BenchmarkQuery_DryRunBuild(b *testing.B) {
	_, db := setupBenchDB(b)
	sqlDB, err := db.DB()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = sqlDB.Close() })
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
