package gplus

import (
	"context"
	"os"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// openPGOrSkip 测试入口：连 PG 服务，本地无 PG 时跳过。CI 必走。
func openPGOrSkip(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		dsn = defaultPGDSN
	}
	return openPG(t, dsn)
}

// TestPG_AliasSelfJoin_LeftJoinAs 验证 v0.8.0 alias 体系自连接在 PG 方言下生成正确 SQL。
//
// 期望：
//   - SQL 含 "boss" alias 标识符
//   - SQL 含 LEFT JOIN 关键字
//   - 列引用使用 PG 双引号转义（与 SQLite 同，与 MySQL 反引号不同）
func TestPG_AliasSelfJoin_LeftJoinAs(t *testing.T) {
	db := openPGOrSkip(t)
	if err := db.AutoMigrate(&MySQLUser{}); err != nil {
		t.Fatalf("PG AutoMigrate 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Migrator().DropTable(&MySQLUser{}) })

	q, u := NewQueryAs[MySQLUser](context.Background(), "u")
	boss := As[MySQLUser](q, "boss")
	q.LeftJoinAs(boss, &u.ID, &boss.ID, "")

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("PG 自连接 ToSQL 失败: %v", err)
	}

	if !strings.Contains(sql, "boss") {
		t.Errorf("PG 自连接 SQL 应包含 boss alias，实际: %s", sql)
	}
	if !strings.Contains(strings.ToUpper(sql), "LEFT JOIN") {
		t.Errorf("PG 自连接 SQL 应含 LEFT JOIN，实际: %s", sql)
	}
	// PG 双引号转义校验（出现至少一次双引号包裹的标识符）
	if !strings.Contains(sql, `"`) {
		t.Errorf("PG 自连接 SQL 应使用双引号转义标识符，实际: %s", sql)
	}
	// 不应出现 MySQL 反引号
	if strings.Contains(sql, "`") {
		t.Errorf("PG SQL 不应含反引号（MySQL 方言），实际: %s", sql)
	}
}

// TestPG_AliasField_InQEq 验证 alias 字段在 q.Eq 等类型安全方法中可用，
// 且生成的 SQL 在 PG 方言下使用双引号转义为 "o"."amount"。
//
// v0.8.0 核心承诺在 PG 方言下的镜像验证（query_exists_test.go: TestAliasField_InQEq_Works
// 仅在 sqlite/mysql 跑过 ToSQL 断言，本测试补 PG）。
func TestPG_AliasField_InQEq(t *testing.T) {
	db := openPGOrSkip(t)
	if err := db.AutoMigrate(&UserWithDelete{}, &Order{}); err != nil {
		t.Fatalf("PG AutoMigrate 失败: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Migrator().DropTable(&Order{}, &UserWithDelete{})
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
		t.Fatalf("PG ToSQL 失败: %v", err)
	}

	// 方言无关断言：脱掉双引号 + 反引号后应含 o.amount
	clean := strings.NewReplacer(`"`, "", "`", "").Replace(sql)
	if !strings.Contains(clean, "o.amount") {
		t.Errorf("PG SQL 应含 'o.amount' alias 列引用（脱引号后），实际: %s", sql)
	}
	// PG 应有双引号
	if !strings.Contains(sql, `"o"."amount"`) {
		t.Errorf("PG SQL 应含双引号转义 \"o\".\"amount\"，实际: %s", sql)
	}
}

// TestPG_SubQuery_OuterRef_LiteralsRendered 验证 correlated SubQuery 在 PG 方言下
// 字面量内联与子查询渲染均正确。
//
// 注：q.ToSQL(db) 内部调用 GORM 的 db.ToSQL → Dialector.Explain，会把 ? 占位符
// 替换为字面量（参数内联），所以本测试不测占位符序号 rebase（拿不到原始 stmt.SQL），
// 改测内联后字面量与子查询关键字均出现在结果 SQL 中。
//
// 占位符 $N 序号 rebase 由 GORM PG 驱动统一处理，alias_probe_test 中
// JoinsWithArgs_ArgsParameterized_NotInlined 已用 DryRun + stmt.SQL.String 验证占位符存在。
func TestPG_SubQuery_OuterRef_LiteralsRendered(t *testing.T) {
	db := openPGOrSkip(t)
	if err := db.AutoMigrate(&UserWithDelete{}, &Order{}); err != nil {
		t.Fatalf("PG AutoMigrate 失败: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Migrator().DropTable(&Order{}, &UserWithDelete{})
	})

	// 外层 Eq+Gt + correlated EXISTS（子查询用 &u.ID 做相关引用）
	q, u := NewQuery[UserWithDelete](context.Background())
	q.Eq(&u.Name, "alice").Gt(&u.Age, 18)
	sub, o := SubQuery[Order](q)
	sub.Eq(&o.UserID, &u.ID).Gt(&o.Amount, 50)
	q.Exists(sub)

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("PG SubQuery ToSQL 失败: %v", err)
	}

	// 子查询关键字必须出现
	if !strings.Contains(strings.ToUpper(sql), "EXISTS") {
		t.Errorf("PG SQL 应含 EXISTS，实际: %s", sql)
	}
	// 外层字面量内联
	if !strings.Contains(sql, "'alice'") {
		t.Errorf("PG SQL 应含外层字面量 'alice'（参数内联），实际: %s", sql)
	}
	if !strings.Contains(sql, "18") {
		t.Errorf("PG SQL 应含外层 age 值 18，实际: %s", sql)
	}
	// 内层字面量内联
	if !strings.Contains(sql, "50") {
		t.Errorf("PG SQL 应含子查询 amount 值 50，实际: %s", sql)
	}
	// 双引号转义生效
	if !strings.Contains(sql, `"`) {
		t.Errorf("PG SQL 应使用双引号转义标识符，实际: %s", sql)
	}
}
