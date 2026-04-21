package gplus

import (
	"os"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// defaultMySQLDSN 本地开发默认 DSN，CI 通过 TEST_MYSQL_DSN 覆盖
const defaultMySQLDSN = "root:root@tcp(127.0.0.1:3306)/test?charset=utf8mb4&parseTime=True&loc=Local"

// openDB 根据环境变量选择 SQLite（默认）或 MySQL（TEST_DB=mysql）
func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	useMySQL := dsn != "" || os.Getenv("TEST_DB") == "mysql"

	if useMySQL {
		if dsn == "" {
			dsn = defaultMySQLDSN
		}
		db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Info),
		})
		if err != nil {
			t.Skipf("MySQL 不可用，跳过: %v", err)
		}
		return db
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Fatalf("failed to open SQLite: %v", err)
	}
	return db
}

// truncateTables 清空并重置多张表，调用方须按依赖顺序传入（子表在前，父表在后）。
// 用 DELETE + ALTER AUTO_INCREMENT 代替 TRUNCATE，在 FK 检查开启的情况下正确工作。
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
		if db.Name() == "mysql" {
			if err := db.Exec("ALTER TABLE " + stmt.Table + " AUTO_INCREMENT = 1").Error; err != nil {
				t.Logf("重置自增 %s 失败: %v", stmt.Table, err)
			}
		}
	}
}
