package gplus

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// drUser DataRule.Table 测试专用模型（自引用，用于自连接验证）。
type drUser struct {
	ID     int64  `gorm:"column:id;primaryKey;autoIncrement"`
	DeptID uint   `gorm:"column:dept_id"`
	Age    uint   `gorm:"column:age"`
	Name   string `gorm:"column:name"`
}

func (drUser) TableName() string { return "dr_users" }

// drDataRuleSQL 注入 rules 后用 DryRun 生成 SQL（去引号）并返回 q（供取 GetError）。
// 单表 drUser，不执行；用于操作符穿透与负例断言。
func drDataRuleSQL(t *testing.T, rules []DataRule) (q *Query[drUser], sql string) {
	t.Helper()
	db := newDryRunDB(t)
	ctx := context.WithValue(context.Background(), DataRuleKey, rules)
	qq, _ := NewQuery[drUser](ctx)
	stmt := db.Session(&gorm.Session{DryRun: true}).
		Model(&drUser{}).
		Scopes(qq.DataRuleBuilder().BuildQuery()).
		Find(&[]drUser{}).Statement
	return qq, stripIdentQuotes(stmt.SQL.String())
}

// AC-3: Table 非空 + Column 含点 → fail-fast，GetError 含原始 "dept.id"，DryRun WHERE 不含 dept
func TestDataRuleTable_failfast_when_table_set_and_column_has_dot(t *testing.T) {
	rules := []DataRule{{Table: "ext", Column: "dept.id", Condition: "=", Value: "1"}}
	q, sql := drDataRuleSQL(t, rules)
	err := q.DataRuleBuilder().GetError()
	if err == nil || !strings.Contains(err.Error(), "dept.id") {
		t.Fatalf("期望 fail-fast 错误含原始 dept.id，实际: %v", err)
	}
	if strings.Contains(sql, "dept") {
		t.Fatalf("fail-fast 后 WHERE 不应含 dept，实际 SQL: %s", sql)
	}
}
