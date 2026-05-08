//go:build dm

package gplus

import (
	"os"
	"testing"

	dameng "github.com/godoes/gorm-dameng"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// defaultDMDSN 故意留空字符串——强制下游必须显式设置 TEST_DM_DSN。
//
// 防自相矛盾策略（spec §3.3）：dameng 镜像默认密码版本差异较大（SYSDBA / SYSDBA001 / 部分
// 版本首登强制改密），spec 写死任一具体密码都会与某些镜像版本不一致导致 connect fail。
// 故 plan 阶段 Task 0 Step 7 实测密码后写入 README 的 TEST_DM_DSN 样例供下游用户参考，
// 而 setup helper 自身不预设默认。
const defaultDMDSN = ""

// setupDMDB 与 setupOracleDB 同模式：非泛型，绑定 MySQLUser 复用既有测试 struct。
//
// 标识符长度自检：MySQLUser → my_sql_users (12 chars)；id/username/age/email
// 字段全部 ≤8 chars——沿用 Oracle 12c R1 的 30 字符上限规范（DM 8 实际 128，
// 但保留与既有 Oracle 测试 struct 一致便于跨方言通用）。
//
// 保留字回避（Task 0 Step 10 已预查）：MySQLUser 字段 name/age/email 不与 DM 8
// Oracle 兼容模式保留字冲突。新增测试字段需主动避开 comment / type / group / role /
// order / size / level / number / date 等 DM/Oracle 共用保留字（空 quoter 策略下
// gplus 不会自动加引号，TD-14）。
//
// 不前置 AutoMigrate（spec §3.3）：直接走 truncateDMTables 的 DROP+AutoMigrate 路径
// 建表。沿用 v0.8.2 Oracle commit 7627ea6 的修订决策——godoes/gorm-dameng migrator
// 也假定走 Oracle 兼容路径，已存在表 ALTER ADD 极可能报 ORA-01430 column already
// exists 等价错误，必须先 DROP 再 CREATE 才能保证从干净状态开始。
//
// Skip 误报防护（spec §3.2）：TEST_DM_REQUIRED=1 时，DSN 未设或连接失败均改 t.Fatalf
// 避免 exit 0 误报。作者本地实施 / 未来 CI 引用 setup helper 时设此 env。
func setupDMDB(t *testing.T) (*Repository[int64, MySQLUser], *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("TEST_DM_DSN")
	if dsn == "" {
		dsn = defaultDMDSN
	}
	if dsn == "" {
		if os.Getenv("TEST_DM_REQUIRED") == "1" {
			t.Fatalf("TEST_DM_DSN 未设置但 TEST_DM_REQUIRED=1，DM 实测被强制要求")
		}
		t.Skip("TEST_DM_DSN 未设置，跳过 DM 测试（参见 README DM 数据库支持章节）")
	}

	db, err := gorm.Open(dameng.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		if os.Getenv("TEST_DM_REQUIRED") == "1" {
			t.Fatalf("DM 强制要求但不可用: %v", err)
		}
		t.Skipf("DM 不可用，跳过集成测试: %v", err)
	}
	applyDBPoolLimits(t, db)

	repo := NewRepository[int64, MySQLUser](db)
	truncateDMTables(t, db, &MySQLUser{})
	t.Cleanup(func() { truncateDMTables(t, db, &MySQLUser{}) })

	return repo, db
}

// truncateDMTables：DROP TABLE PURGE + AutoMigrate 策略
//
// 决策原因（沿用 Oracle 路径）：
//   - DM Oracle 兼容模式 TRUNCATE 不重置 IDENTITY 序列
//   - ALTER TABLE MODIFY IDENTITY 流程复杂
//   - DROP + AutoMigrate 是最可靠的 IDENTITY 重置方式
//
// PURGE 子句：DM 8 已确认支持 `DROP TABLE X PURGE` 语法（与 Oracle 兼容），
// 且有回收站机制（SF_RECYCLE_BIN_* 系列函数）。直接沿用 Oracle 路径无需修改。
func truncateDMTables(t *testing.T, db *gorm.DB, models ...any) {
	t.Helper()
	for _, m := range models {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(m); err != nil {
			t.Logf("parse table name failed: %v", err)
			continue
		}
		if err := db.Exec("DROP TABLE \"" + stmt.Table + "\" PURGE").Error; err != nil {
			t.Logf("drop table %s warn (可能不存在): %v", stmt.Table, err)
		}
		if err := db.AutoMigrate(m); err != nil {
			t.Fatalf("re-migrate %s failed: %v", stmt.Table, err)
		}
		t.Logf("truncateDM: drop+migrate OK: %s", stmt.Table)
	}
}
