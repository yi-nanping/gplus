//go:build dm

package gplus

import (
	"context"
	"strings"
	"testing"
)

// TestDM_AliasSelfJoin_LeftJoinAs 验证 v0.8.0 alias 体系自连接在 DM 方言下生成正确 SQL。
//
// 期望（DM 双引号 quoter 策略下，详见 builder.go: getQuoteChar dm 分支注释）：
//   - SQL 含 boss alias 标识符（双引号包裹小写匹配 dameng migrator case-sensitive lowercase 列）
//   - SQL 含 LEFT JOIN 关键字
//   - 不含 MySQL 反引号
//   - 不含 SQL Server 方括号
func TestDM_AliasSelfJoin_LeftJoinAs(t *testing.T) {
	_, db := setupDMDB(t)

	q, u := NewQueryAs[MySQLUser](context.Background(), "u")
	boss := As[MySQLUser](q, "boss")
	q.LeftJoinAs(boss, &u.ID, &boss.ID, "")

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("DM 自连接 ToSQL 失败: %v", err)
	}

	if !strings.Contains(sql, "boss") {
		t.Errorf("DM 自连接 SQL 应包含 boss alias，实际: %s", sql)
	}
	if !strings.Contains(strings.ToUpper(sql), "LEFT JOIN") {
		t.Errorf("DM 自连接 SQL 应含 LEFT JOIN，实际: %s", sql)
	}
	if strings.Contains(sql, "`") {
		t.Errorf("DM SQL 不应含反引号（MySQL 方言），实际: %s", sql)
	}
	if strings.Contains(sql, "[") {
		t.Errorf("DM SQL 不应含方括号（SQL Server 方言），实际: %s", sql)
	}
}

// TestDM_AliasField_InQEq 验证 alias 字段在 q.Eq 等类型安全方法中可用，
// 且生成的 SQL 在 DM 方言下解析为 alias 列引用 o.amount。
//
// DM 双引号 quoter 策略下 SQL 中是 "o"."amount"，脱掉双引号后应含 o.amount。
func TestDM_AliasField_InQEq(t *testing.T) {
	_, db := setupDMDB(t)
	if err := db.AutoMigrate(&UserWithDelete{}, &Order{}); err != nil {
		t.Fatalf("DM AutoMigrate 失败: %v", err)
	}
	t.Cleanup(func() {
		// 双引号锁定小写匹配 dameng migrator 引号 lowercase 建表
		_ = db.Exec(`DROP TABLE "orders" PURGE`).Error
		_ = db.Exec(`DROP TABLE "user_with_deletes" PURGE`).Error
	})

	q, u := NewQuery[UserWithDelete](context.Background())
	o := As[Order](q, "o")
	q.LeftJoinAs(o, &o.UserID, &u.ID, "")
	q.Eq(&o.Amount, 100)

	if err := q.GetError(); err != nil {
		t.Fatalf("q.Eq with alias field accumulated error: %v", err)
	}

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("DM ToSQL 失败: %v", err)
	}

	// 方言无关断言：脱掉双引号 + 反引号后应含 o.amount
	clean := strings.NewReplacer(`"`, "", "`", "").Replace(sql)
	if !strings.Contains(clean, "o.amount") {
		t.Errorf("DM SQL 应含 'o.amount' alias 列引用（脱引号后），实际: %s", sql)
	}
	if strings.Contains(sql, "`") {
		t.Errorf("DM SQL 不应含反引号（MySQL 方言），实际: %s", sql)
	}
}

// TestDM_SubQuery_OuterRef_LiteralsRendered 验证 correlated SubQuery 在 DM
// 方言下字面量内联与子查询渲染均正确。
//
// 注：q.ToSQL(db) 内部调用 GORM 的 db.ToSQL → Dialector.Explain，会把 :N 占位符
// 替换为字面量（参数内联），所以本测试不测占位符序号 rebase（拿不到原始 stmt.SQL），
// 改测内联后字面量与子查询关键字均出现在结果 SQL 中。
func TestDM_SubQuery_OuterRef_LiteralsRendered(t *testing.T) {
	_, db := setupDMDB(t)
	if err := db.AutoMigrate(&UserWithDelete{}, &Order{}); err != nil {
		t.Fatalf("DM AutoMigrate 失败: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec(`DROP TABLE "orders" PURGE`).Error
		_ = db.Exec(`DROP TABLE "user_with_deletes" PURGE`).Error
	})

	// 外层 Eq+Gt + correlated EXISTS（子查询用 &u.ID 做相关引用）
	q, u := NewQuery[UserWithDelete](context.Background())
	q.Eq(&u.Name, "alice").Gt(&u.Age, 18)
	sub, o := SubQuery[Order](q)
	sub.Eq(&o.UserID, &u.ID).Gt(&o.Amount, 50)
	q.Exists(sub)

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("DM SubQuery ToSQL 失败: %v", err)
	}

	if !strings.Contains(strings.ToUpper(sql), "EXISTS") {
		t.Errorf("DM SQL 应含 EXISTS，实际: %s", sql)
	}
	if !strings.Contains(sql, "'alice'") {
		t.Errorf("DM SQL 应含外层字面量 'alice'（参数内联），实际: %s", sql)
	}
	if !strings.Contains(sql, "18") {
		t.Errorf("DM SQL 应含外层 age 值 18，实际: %s", sql)
	}
	if !strings.Contains(sql, "50") {
		t.Errorf("DM SQL 应含子查询 amount 值 50，实际: %s", sql)
	}
	if strings.Contains(sql, "`") {
		t.Errorf("DM SQL 不应含反引号（MySQL 方言），实际: %s", sql)
	}
}
