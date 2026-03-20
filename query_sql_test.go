package gplus

import (
	"context"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newDryRunDB 创建 SQLite 内存 DB，用于 DryRun SQL 验证（无需 AutoMigrate）
func newDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Fatalf("newDryRunDB: %v", err)
	}
	return db
}

// buildSQL 用 DryRun 方式生成 SQL 字符串和参数，不执行真实查询
func buildSQL(t *testing.T, db *gorm.DB, q *Query[TestUser]) (string, []interface{}) {
	t.Helper()
	stmt := db.Session(&gorm.Session{DryRun: true}).
		Model(&TestUser{}).
		Scopes(q.DataRuleBuilder().BuildQuery()).
		Find(&[]TestUser{}).Statement
	return stmt.SQL.String(), stmt.Vars
}

// assertSQL 检查 sql 中是否包含所有期望片段
func assertSQL(t *testing.T, sql string, frags ...string) {
	t.Helper()
	for _, f := range frags {
		if !strings.Contains(sql, f) {
			t.Errorf("SQL 中缺少片段 %q\n实际 SQL: %s", f, sql)
		}
	}
}

func TestQuery_SQL(t *testing.T) {
	db := newDryRunDB(t)
	ctx := context.Background()

	t.Run("AND比较条件_Eq_Ne_Gt_Ge_Lt_Le", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").Ne(&u.Age, 10).Gt(&u.Age, 18).Ge(&u.Score, 60.0).Lt(&u.Age, 100).Le(&u.Score, 99.9)
		sql, _ := buildSQL(t, db, q)
		assertSQL(t, sql,
			`"username" = ?`,
			`"age" <> ?`,
			`"age" > ?`,
			`"score" >= ?`,
			`"age" < ?`,
			`"score" <= ?`,
		)
	})

	t.Run("OR比较条件_OrEq_OrNe_OrGt_OrGe_OrLt_OrLe", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").
			OrEq(&u.Age, 20).OrNe(&u.Age, 10).
			OrGt(&u.Age, 30).OrGe(&u.Score, 50.0).
			OrLt(&u.Age, 5).OrLe(&u.Score, 10.0)
		sql, _ := buildSQL(t, db, q)
		assertSQL(t, sql,
			`"username" = ?`,
			`"age" = ?`,
			`"age" <> ?`,
			`"age" > ?`,
			`"score" >= ?`,
			`"age" < ?`,
			`"score" <= ?`,
		)
	})

	t.Run("LIKE_模糊查询", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Like(&u.Name, "ali").
			LikeLeft(&u.Name, "ce").
			LikeRight(&u.Name, "bo").
			NotLike(&u.Name, "dave").
			OrLike(&u.Email, "test")
		sql, vars := buildSQL(t, db, q)
		assertSQL(t, sql, `"username" LIKE ?`, `"email" LIKE ?`, `"username" NOT LIKE ?`)
		hasPercent := false
		for _, v := range vars {
			if s, ok := v.(string); ok && strings.Contains(s, "%") {
				hasPercent = true
				break
			}
		}
		if !hasPercent {
			t.Errorf("LIKE 参数应含 %%%%，实际 vars: %v", vars)
		}
	})

	t.Run("IN_NotIn", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.In(&u.Age, []int{18, 20}).
			NotIn(&u.Age, []int{99}).
			OrIn(&u.Score, []float64{1.0}).
			OrNotIn(&u.Name, []string{"x"})
		sql, _ := buildSQL(t, db, q)
		// GORM 对切片展开为 IN (?,?)，用 IN ( 匹配
		assertSQL(t, sql, `"age" IN (`, `"age" NOT IN (`)
	})

	t.Run("BETWEEN_NotBetween", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Between(&u.Age, 18, 30).
			NotBetween(&u.Score, 0.0, 10.0).
			OrBetween(&u.Age, 5, 10).
			OrNotBetween(&u.Score, 90.0, 100.0)
		sql, _ := buildSQL(t, db, q)
		assertSQL(t, sql, `"age" BETWEEN ? AND ?`, `"score" NOT BETWEEN ? AND ?`)
	})

	t.Run("NULL_IsNull_IsNotNull", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.IsNull(&u.Email).IsNotNull(&u.Name).OrIsNull(&u.Score).OrIsNotNull(&u.Age)
		sql, _ := buildSQL(t, db, q)
		assertSQL(t, sql, `"email" IS NULL`, `"username" IS NOT NULL`)
	})

	t.Run("And嵌套分组", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.IsActive, true).And(func(sub *Query[TestUser]) {
			sub.Gt(&u.Age, 18).OrGt(&u.Score, 90)
		})
		sql, _ := buildSQL(t, db, q)
		assertSQL(t, sql, `"is_active" = ?`, `"age" > ?`, "(")
	})

	t.Run("Or嵌套分组", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").Or(func(sub *Query[TestUser]) {
			sub.Gt(&u.Age, 18).Gt(&u.Score, 90)
		})
		sql, _ := buildSQL(t, db, q)
		assertSQL(t, sql, `"username" = ?`, `"age" > ?`, "(")
	})

	t.Run("WhereRaw_OrWhereRaw", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").
			WhereRaw("YEAR(created_at) = ?", 2024).
			OrWhereRaw("score > ?", 99)
		sql, _ := buildSQL(t, db, q)
		assertSQL(t, sql, "YEAR(created_at) = ?", "score > ?")
	})

	t.Run("Select", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Select(&u.Name, &u.Age)
		sql, _ := buildSQL(t, db, q)
		assertSQL(t, sql, `"username"`, `"age"`)
		if strings.Contains(sql, `"email"`) {
			t.Errorf("Select 指定列后不应含未选中的 email\nSQL: %s", sql)
		}
	})

	t.Run("Distinct", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Distinct(&u.Age)
		sql, _ := buildSQL(t, db, q)
		assertSQL(t, sql, "DISTINCT", `"age"`)
	})

	t.Run("Order_ASC_DESC_Raw", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Order(&u.Age, false).Order(&u.Name, true).OrderRaw("score DESC NULLS LAST")
		sql, _ := buildSQL(t, db, q)
		assertSQL(t, sql, `"age" DESC`, `"username" ASC`, "score DESC NULLS LAST")
	})

	t.Run("Group_Having_OrHaving", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Group(&u.Age).Having("age", OpGt, 18).OrHaving("score", OpGt, 90)
		sql, _ := buildSQL(t, db, q)
		assertSQL(t, sql, "GROUP BY", `"age"`, "HAVING")
	})

	t.Run("HavingGroup嵌套", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Group(&u.Age).HavingGroup(func(sub *Query[TestUser]) {
			sub.Having("age", OpGt, 18).OrHaving("score", OpGt, 90)
		})
		sql, _ := buildSQL(t, db, q)
		assertSQL(t, sql, "GROUP BY", "HAVING")
	})

	t.Run("Limit_Offset", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Limit(10).Offset(20)
		sql, _ := buildSQL(t, db, q)
		assertSQL(t, sql, "LIMIT 10", "OFFSET 20")
	})

	t.Run("LeftJoin_InnerJoin", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.LeftJoin("orders", "orders.user_id = test_users.id")
		sql, _ := buildSQL(t, db, q)
		assertSQL(t, sql, "LEFT JOIN", "orders")

		q2, _ := NewQuery[TestUser](ctx)
		q2.InnerJoin("orders", "orders.user_id = test_users.id")
		sql2, _ := buildSQL(t, db, q2)
		assertSQL(t, sql2, "INNER JOIN", "orders")
	})

	t.Run("CrossJoin", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.CrossJoin("orders")
		sql, _ := buildSQL(t, db, q)
		assertSQL(t, sql, "CROSS JOIN", "orders")
	})

	t.Run("LockWrite_LockRead", func(t *testing.T) {
		// SQLite DryRun 不生成 FOR UPDATE/FOR SHARE，验证 lockStrength 被正确设置即可
		q, _ := NewQuery[TestUser](ctx)
		q.LockWrite()
		if q.lockStrength != "UPDATE" {
			t.Errorf("LockWrite: lockStrength 期望 UPDATE，实际 %q", q.lockStrength)
		}
		// 确认 DryRun 不报错
		_, _ = buildSQL(t, db, q)

		q2, _ := NewQuery[TestUser](ctx)
		q2.LockRead()
		if q2.lockStrength != "SHARE" {
			t.Errorf("LockRead: lockStrength 期望 SHARE，实际 %q", q2.lockStrength)
		}
		_, _ = buildSQL(t, db, q2)
	})

	t.Run("Table动态表名", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Table("custom_users")
		sql, _ := buildSQL(t, db, q)
		assertSQL(t, sql, "custom_users")
	})

	t.Run("DataRule_ctx注入", func(t *testing.T) {
		rules := []DataRule{
			{Column: "age", Condition: OpGt, Value: "18"},
		}
		ruleCtx := context.WithValue(ctx, DataRuleKey, rules)
		q, _ := NewQuery[TestUser](ruleCtx)
		sql, _ := buildSQL(t, db, q)
		assertSQL(t, sql, `"age"`)
	})
}
