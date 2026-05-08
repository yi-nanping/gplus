//go:build oracle

package gplus

import (
	"os"
	"testing"

	oracle "github.com/godoes/gorm-oracle"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// defaultOracleDSN 本地开发默认 DSN，CI 通过 TEST_ORACLE_DSN 覆盖。
//
// 警告：仅限本地 Docker 开发。`system` 是 Oracle DBA 权限账户（非 SYS 超级账户但
// 仍可执行所有数据库管理操作），密码 oracle 是 Oracle 23c Free Docker 镜像的默认
// 密码——绝不能用于生产。CI/生产请用 TEST_ORACLE_DSN 提供独立测试账户，且仅授予
// 最小测试权限（CONNECT + RESOURCE 即可）。
const defaultOracleDSN = "oracle://system:oracle@127.0.0.1:1521/FREEPDB1"

// setupOracleDB 与 PG/MySQL 同模式：非泛型，绑定 MySQLUser 复用既有测试 struct。
// 镜像 setupPGDB(t) 风格保持代码组织一致。
//
// 标识符长度自检：MySQLUser → my_sql_users (12 chars)；id/username/age/email
// 字段全部 ≤8 chars——满足 Oracle 12c R1 的 30 字符上限要求。
func setupOracleDB(t *testing.T) (*Repository[int64, MySQLUser], *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("TEST_ORACLE_DSN")
	if dsn == "" {
		dsn = defaultOracleDSN
	}

	db, err := gorm.Open(oracle.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Skipf("Oracle 不可用，跳过集成测试: %v", err)
	}
	applyDBPoolLimits(t, db)

	// 直接走 truncateOracleTables 的 DROP+AutoMigrate 路径建表
	// 原因：godoes/gorm-oracle migrator 对已存在的表 ALTER ADD 时报 ORA-01430，
	// 必须先 DROP 再 CREATE 才能保证从干净状态开始
	repo := NewRepository[int64, MySQLUser](db)
	truncateOracleTables(t, db, &MySQLUser{})
	t.Cleanup(func() { truncateOracleTables(t, db, &MySQLUser{}) })

	return repo, db
}

// truncateOracleTables：DROP TABLE PURGE + AutoMigrate 策略
//
// 决策原因（数据库审计建议）：
//   - Oracle TRUNCATE 不重置 IDENTITY 序列
//   - ALTER TABLE MODIFY IDENTITY 流程复杂
//   - DROP + AutoMigrate 是最可靠的 IDENTITY 重置方式
//
// PURGE 必要性：Oracle DROP TABLE 默认进入回收站（USER_RECYCLEBIN），测试循环
// 反复 DROP 同名表会触发 ORA-38301 或空间耗尽。GORM Migrator().DropTable 不会
// 自动加 PURGE，必须用原生 SQL 显式 PURGE。
func truncateOracleTables(t *testing.T, db *gorm.DB, models ...any) {
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
		t.Logf("truncateOracle: drop+migrate OK: %s", stmt.Table)
	}
}
