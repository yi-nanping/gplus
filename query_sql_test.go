package gplus

import (
	"context"
	"gorm.io/gorm"
	"strings"
	"testing"
)

// buildSQL 用 DryRun 方式生成 SQL 字符串和参数，不执行真实查询
func buildSQL(t *testing.T, db *gorm.DB, q *Query[TestUser]) (string, []interface{}) {
	t.Helper()
	stmt := db.Session(&gorm.Session{DryRun: true}).
		Model(&TestUser{}).
		Scopes(q.DataRuleBuilder().BuildQuery()).
		Find(&[]TestUser{}).Statement
	return stmt.SQL.String(), stmt.Vars
}

// stripIdentQuotes 去除标识符引号（反引号/双引号），用于方言无关的 SQL 结构断言。
// SQLite/PG 用双引号、MySQL 用反引号；去引号后可统一断言 FROM/AS 等结构。
func stripIdentQuotes(s string) string {
	return strings.NewReplacer("`", "", `"`, "").Replace(s)
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

// 锁定纯白盒锁状态；SQLite 驱动 SQL 断言位于 tests 模块。
func TestQuery_SQL(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	q.LockWrite()
	if q.lockStrength != "UPDATE" {
		t.Fatalf("LockWrite strength=%q", q.lockStrength)
	}
	q2, _ := NewQuery[TestUser](context.Background())
	q2.LockRead()
	if q2.lockStrength != "SHARE" {
		t.Fatalf("LockRead strength=%q", q2.lockStrength)
	}
}
