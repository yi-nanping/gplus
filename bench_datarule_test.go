package gplus

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// BenchmarkDataRule_DryRunBuild 测量新建 Query、应用权限规则及构建 SQL 的成本。
// 请求上下文、数据库和 schema 预热不计时；每轮新建 Query，避免累积权限条件。
func BenchmarkDataRule_DryRunBuild(b *testing.B) {
	_, db := setupBenchDB(b)
	dryDB := db.Session(&gorm.Session{DryRun: true})
	baseCtx := context.Background()
	singleRule := []DataRule{{Column: "age", Condition: ">=", Value: "18"}}
	singleCtx := context.WithValue(baseCtx, DataRuleKey, singleRule)
	multiCtx := context.WithValue(baseCtx, DataRuleKey, []DataRule{
		singleRule[0],
		{Column: "is_active", Condition: "=", Value: "1"},
	})
	aliasCtx := context.WithValue(baseCtx, DataRuleKey, []DataRule{
		{Table: "u", Column: "age", Condition: ">=", Value: "18"},
	})
	cases := []struct {
		name      string
		ctx       context.Context
		vars      []any
		sqlParts  []string
		ruleCount int
	}{
		{name: "NoRule", ctx: baseCtx, vars: []any{"Alice"}},
		{name: "SingleRuleAND", ctx: singleCtx, vars: []any{"Alice", "18"}, ruleCount: 1},
		{name: "MultipleRules", ctx: multiCtx, vars: []any{"Alice", "18", "1"}, ruleCount: 1, sqlParts: []string{"\"is_active\" = ?"}},
		{name: "BusinessOR", ctx: singleCtx, vars: []any{"Alice", "Bob", "18"}, ruleCount: 1, sqlParts: []string{" OR ", " AND "}},
		{name: "NestedScope", ctx: singleCtx, vars: []any{"Alice", "Bob", "18"}, ruleCount: 1, sqlParts: []string{" OR ", " AND "}},
		{name: "Alias", ctx: aliasCtx, vars: []any{"Alice", "18"}, ruleCount: 1, sqlParts: []string{"\"u\".\"age\" >= ?"}},
		{name: "ExplicitRuleSubquery", ctx: singleCtx, vars: []any{"Alice", 20, "18", "18"}, ruleCount: 2, sqlParts: []string{"EXISTS"}},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			build := func() *gorm.DB {
				var q *Query[TestUser]
				var m *TestUser
				if tc.name == "Alias" {
					q, m = NewQueryAs[TestUser](tc.ctx, "u")
				} else {
					q, m = NewQuery[TestUser](tc.ctx)
				}
				q.Eq(&m.Name, "Alice")
				switch tc.name {
				case "BusinessOR":
					q.OrEq(&m.Name, "Bob")
				case "NestedScope":
					q.WithScope(func(db *gorm.DB) *gorm.DB {
						return db.Scopes(func(db *gorm.DB) *gorm.DB {
							return db.Scopes(func(db *gorm.DB) *gorm.DB { return db.Or("username = ?", "Bob") })
						})
					})
				case "ExplicitRuleSubquery":
					sub, s := SubQuery[TestUser](q)
					sub.Select(&s.ID).Gt(&s.Age, 20).DataRuleBuilder()
					if err := sub.GetError(); err != nil {
						b.Fatalf("subquery: %v", err)
					}
					q.Exists(sub)
				}
				q.DataRuleBuilder()
				if err := q.GetError(); err != nil {
					b.Fatalf("query: %v", err)
				}
				var rows []TestUser
				result := dryDB.Model(new(TestUser)).Scopes(q.BuildQuery()).Find(&rows)
				if result.Error != nil || result.Statement.SQL.Len() == 0 {
					b.Fatalf("error=%v SQL=%s", result.Error, result.Statement.SQL.String())
				}
				return result
			}
			// 预检 SQL 和绑定参数，避免错误或缺少规则的路径产生虚假性能结果。
			result := build()
			sql := result.Statement.SQL.String()
			if !reflect.DeepEqual(result.Statement.Vars, tc.vars) {
				b.Fatalf("vars=%#v, want=%#v SQL=%s", result.Statement.Vars, tc.vars, sql)
			}
			if got := strings.Count(sql, "age\" >= ?"); got != tc.ruleCount {
				b.Fatalf("age rule count=%d, want=%d SQL=%s", got, tc.ruleCount, sql)
			}
			for _, part := range tc.sqlParts {
				if !strings.Contains(sql, part) {
					b.Fatalf("missing %q in SQL=%s", part, sql)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				build()
			}
		})
	}
}
