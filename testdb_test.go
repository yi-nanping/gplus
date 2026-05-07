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

// defaultMySQLDSN 本地开发默认 DSN，CI 通过 TEST_MYSQL_DSN 覆盖
const defaultMySQLDSN = "root:root@tcp(127.0.0.1:3306)/test?charset=utf8mb4&parseTime=True&loc=Local"

// defaultPGDSN 本地开发默认 PostgreSQL DSN，CI 通过 TEST_PG_DSN 覆盖
const defaultPGDSN = "host=127.0.0.1 port=5432 user=postgres password=postgres dbname=test sslmode=disable"

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
//   - TEST_DB=pg 或 TEST_PG_DSN 非空 → PostgreSQL
//   - TEST_DB=mysql 或 TEST_MYSQL_DSN 非空 → MySQL
//   - 否则 → SQLite (:memory:)
//
// CI 同时设置 TEST_MYSQL_DSN 和 TEST_PG_DSN 时，TEST_DB 决定优先级；未设置 TEST_DB
// 则按 PG > MySQL > SQLite 排查（PG 优先因为方言适配更"严格"，先暴露问题）。
func openDB(t *testing.T) *gorm.DB {
	t.Helper()

	pgDSN := os.Getenv("TEST_PG_DSN")
	mysqlDSN := os.Getenv("TEST_MYSQL_DSN")
	switch os.Getenv("TEST_DB") {
	case "pg", "postgres":
		if pgDSN == "" {
			pgDSN = defaultPGDSN
		}
		return openPG(t, pgDSN)
	case "mysql":
		if mysqlDSN == "" {
			mysqlDSN = defaultMySQLDSN
		}
		return openMySQL(t, mysqlDSN)
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
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Skipf("MySQL 不可用，跳过: %v", err)
	}
	applyDBPoolLimits(t, db)
	return db
}

func openPG(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Skipf("PostgreSQL 不可用，跳过: %v", err)
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
