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

// TestPG_SubQuery_OuterRef_PlaceholderRebase 验证跨层 SubQuery 在 PG 占位符 $1, $2
// 序号正确。PG 驱动把 ? 占位符重写为 $N，嵌套子查询时序号必须按出现顺序连续递增。
//
// MySQL/SQLite 用 ? 不存在序号问题；PG 是测试占位符 rebase 的关键方言。
func TestPG_SubQuery_OuterRef_PlaceholderRebase(t *testing.T) {
	db := openPGOrSkip(t)
	if err := db.AutoMigrate(&UserWithDelete{}, &Order{}); err != nil {
		t.Fatalf("PG AutoMigrate 失败: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Migrator().DropTable(&Order{}, &UserWithDelete{})
	})

	// 外层条件 + EXISTS 子查询 + 子查询条件 → 至少 3 个占位符（$1, $2, $3）
	q, u := NewQuery[UserWithDelete](context.Background())
	q.Eq(&u.Name, "alice").Gt(&u.Age, 18)
	sub, o := SubQuery[Order](q)
	sub.Eq(&o.UserID, u.ID).Gt(&o.Amount, 50)
	q.Exists(sub)

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("PG SubQuery ToSQL 失败: %v", err)
	}

	// PG 占位符必须是 $N 形式（不是 ?）
	if strings.Contains(sql, "?") {
		t.Errorf("PG SQL 不应含 ? 占位符（应被驱动重写为 $N），实际: %s", sql)
	}
	if !strings.Contains(sql, "$1") {
		t.Errorf("PG SQL 应含 $1 占位符，实际: %s", sql)
	}
	// 子查询 EXISTS 形态
	if !strings.Contains(strings.ToUpper(sql), "EXISTS") {
		t.Errorf("PG SQL 应含 EXISTS，实际: %s", sql)
	}
	// 不应有占位符序号断裂（$1 到 $N 连续，至少 $1 $2 $3 都存在）
	for _, ph := range []string{"$1", "$2", "$3"} {
		if !strings.Contains(sql, ph) {
			t.Errorf("PG SQL 应含 %s 占位符（占位符 rebase 序号检查），实际: %s", ph, sql)
		}
	}
}
