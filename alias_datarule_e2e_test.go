// alias_datarule_e2e_test.go — v0.8.0 DataRule×alias e2e 验收（spec H9）
package gplus

import (
	"context"
	"strings"
	"testing"
)

// tenantOrder 专用于 DataRule×alias e2e 测试的订单模型（含 tenant_id）
type tenantOrder struct {
	ID       int64 `gorm:"primaryKey;autoIncrement"`
	UserID   int64 `gorm:"column:user_id"`
	TenantID int   `gorm:"column:tenant_id"`
	Amount   int   `gorm:"column:amount"`
}

// TestDataRuleAliasContract_NoAutoInjectionToSideTable 段 A：锁敞开契约 v0.8.0
//
// DataRule 仅注入到主表（tenant_id 出现 1 次），不自动跨 JOIN 到副表。
// 此测试 PASS 表示 v0.8.0 "副表敞开" 契约成立；
// 未来 v0.9 若加 cross-table 自动注入，此段会 FAIL（提醒同步更新）。
func TestDataRuleAliasContract_NoAutoInjectionToSideTable(t *testing.T) {
	_, db := setupTestDB[TestUser](t)

	// 注入 DataRule 到 ctx：限制主表 tenant_id = 1
	rules := []DataRule{
		{Column: "tenant_id", Condition: "=", Value: "1"},
	}
	ctx := context.WithValue(context.Background(), DataRuleKey, rules)

	q, u := NewQuery[TestUser](ctx)
	// 使用 As 注册副表 alias（tenantOrder），故意不在 extraSQL 加 tenant 过滤
	o := As[tenantOrder](q, "o")
	q.LeftJoinAs(o, &o.UserID, &u.ID, "") // 未加 AND o.tenant_id = ?

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL 失败: %v", err)
	}

	// 主表 DataRule 注入：SQL 必须含 tenant_id
	if !strings.Contains(sql, "tenant_id") {
		t.Errorf("DataRule 应注入主表 tenant_id 条件，实际 SQL: %s", sql)
	}

	// 副表敞开契约：tenant_id 应只出现 1 次（仅主表 WHERE，副表 ON 无 tenant 过滤）
	count := strings.Count(sql, "tenant_id")
	if count != 1 {
		t.Errorf("段 A 锁敞开契约：期望 tenant_id 仅出现 1 次（仅主表），实际 %d 次，SQL: %s", count, sql)
	}
}

// TestDataRuleAliasContract_ExplicitExtraBlocksLeak 段 B：验合规模式
//
// 在 JoinAs extraSQL 中显式加副表 tenant 过滤时，SQL 含 2 处 tenant_id（主表 WHERE + JOIN ON）。
// 两段并列 PASS = v0.8.0 H9 验收通过。
func TestDataRuleAliasContract_ExplicitExtraBlocksLeak(t *testing.T) {
	_, db := setupTestDB[TestUser](t)

	rules := []DataRule{
		{Column: "tenant_id", Condition: "=", Value: "1"},
	}
	ctx := context.WithValue(context.Background(), DataRuleKey, rules)

	q, u := NewQuery[TestUser](ctx)
	o := As[tenantOrder](q, "o")
	// 合规模式：显式在 ON 条件加副表 tenant 过滤
	q.LeftJoinAs(o, &o.UserID, &u.ID, "AND o.tenant_id = ?", 1)

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL 失败: %v", err)
	}

	// 主表 WHERE + JOIN ON 各一处 tenant_id，共 ≥ 2 次
	count := strings.Count(sql, "tenant_id")
	if count < 2 {
		t.Errorf("段 B 合规模式：期望 tenant_id ≥ 2 次（主表 WHERE + 副表 ON），实际 %d 次，SQL: %s", count, sql)
	}
}

// TestDataRuleAlias_SelfJoin_BothDialects 双方言自连接：users JOIN users boss
//
// 使用 NewQueryAs 给主表起 alias "u"，再用 As 注册 boss alias 做自连接。
// 验证生成 SQL 包含 "boss" alias（sqlite 方言）。
// mysql 通过环境变量 TEST_DB=mysql 激活（CI 可选）。
func TestDataRuleAlias_SelfJoin_BothDialects(t *testing.T) {
	_, db := setupTestDB[TestUser](t)

	q, u := NewQueryAs[TestUser](context.Background(), "u")
	boss := As[TestUser](q, "boss")
	q.LeftJoinAs(boss, &u.BossID, &boss.ID, "")

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("自连接 ToSQL 失败: %v", err)
	}

	// 期望 SQL 含 boss alias（无论方言）
	if !strings.Contains(sql, "boss") {
		t.Errorf("自连接 SQL 应包含 boss alias，实际: %s", sql)
	}
}
