package gplus

import (
	"os"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// applyDBPoolLimits 限制连接池规模并在测试结束时关闭底层 *sql.DB。
// 解决多测试 case 反复 gorm.Open 导致连接数耗尽（如 MySQL 8.0 默认 max_connections=151
// 或 PostgreSQL 默认 100）的问题。
func applyDBPoolLimits(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取底层 *sql.DB 失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(time.Minute)
	t.Cleanup(func() { _ = sqlDB.Close() })
}

// openDB 根据环境变量选择驱动：
//   - TEST_DB=sqlite → SQLite (:memory:)，忽略其他驱动 DSN
//   - TEST_DB=pg/postgres → PostgreSQL；TEST_DB=mysql → MySQL
//   - 未设置 TEST_DB 时，按 TEST_PG_DSN > TEST_MYSQL_DSN > SQLite 选择
//
// 显式选择 MySQL/PG 但未配置对应 DSN，或已配置 DSN 的连接失败，均终止测试。
func openDB(t *testing.T) *gorm.DB {
	t.Helper()

	pgDSN := os.Getenv("TEST_PG_DSN")
	mysqlDSN := os.Getenv("TEST_MYSQL_DSN")
	switch os.Getenv("TEST_DB") {
	case "sqlite":
		return openSQLite(t)
	case "pg", "postgres":
		return openPG(t, pgDSN)
	case "mysql":
		return openMySQL(t, mysqlDSN)
	case "":
	default:
		t.Fatalf("不支持的 TEST_DB: %q", os.Getenv("TEST_DB"))
	}

	if pgDSN != "" {
		return openPG(t, pgDSN)
	}
	if mysqlDSN != "" {
		return openMySQL(t, mysqlDSN)
	}
	return openSQLite(t)
}

func openMySQL(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	if dsn == "" {
		if os.Getenv("TEST_DB") == "mysql" {
			t.Fatal("TEST_DB=mysql 必须配置 TEST_MYSQL_DSN")
		}
		t.Skip("未设置 TEST_MYSQL_DSN，跳过 MySQL 集成测试")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Fatalf("连接已配置的 MySQL 测试数据库失败: %v", err)
	}
	applyDBPoolLimits(t, db)
	return db
}

func openPG(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	if dsn == "" {
		if selected := os.Getenv("TEST_DB"); selected == "pg" || selected == "postgres" {
			t.Fatal("TEST_DB=pg/postgres 必须配置 TEST_PG_DSN")
		}
		t.Skip("未设置 TEST_PG_DSN，跳过 PostgreSQL 集成测试")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Fatalf("连接已配置的 PostgreSQL 测试数据库失败: %v", err)
	}
	applyDBPoolLimits(t, db)
	return db
}

func openSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Fatalf("failed to open SQLite: %v", err)
	}
	return db
}

// truncateTables 清空并重置多张表，调用方须按依赖顺序传入（子表在前，父表在后）。
// 用 DELETE + 自增重置代替 TRUNCATE，在 FK 检查开启的情况下正确工作。
func truncateTables(t *testing.T, db *gorm.DB, models ...any) {
	t.Helper()
	for _, model := range models {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			t.Logf("无法解析表名: %v", err)
			continue
		}
		if err := db.Exec("DELETE FROM " + stmt.Table).Error; err != nil {
			t.Logf("清空表 %s 失败: %v", stmt.Table, err)
			continue
		}
		switch db.Name() {
		case "mysql":
			if err := db.Exec("ALTER TABLE " + stmt.Table + " AUTO_INCREMENT = 1").Error; err != nil {
				t.Logf("重置自增 %s 失败: %v", stmt.Table, err)
			}
		case "postgres":
			// GORM 默认序列命名为 <table>_id_seq；非 id 主键或非 serial 类型时序列名不同，忽略错误
			if err := db.Exec("ALTER SEQUENCE IF EXISTS " + stmt.Table + "_id_seq RESTART WITH 1").Error; err != nil {
				t.Logf("重置序列 %s_id_seq 失败: %v", stmt.Table, err)
			}
		}
	}
}
