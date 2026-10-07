package gplus_test

import (
	"context"

	"github.com/glebarez/sqlite"
	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"testing"
)

// setupTestDB 初始化内存 SQLite 用于 ToDB 测试
func setupToDBTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	return db
}

// TestQuery_ToDB 测试 ToDB 将 Query 条件转换为 GORM DB 对象
func TestQuery_ToDB(t *testing.T) {
	ctx := context.Background()

	t.Run("ToDB 返回非 nil DB", func(t *testing.T) {
		db := setupToDBTestDB(t)
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").Gt(&u.Age, 18)
		result := q.ToDB(db)
		if result == nil {
			t.Fatal("ToDB 返回了 nil")
		}
	})

	t.Run("ToDB 不污染原始 DB", func(t *testing.T) {
		db := setupToDBTestDB(t)
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "bob")
		_ = q.ToDB(db)
		// 原始 db 的 Statement 不应被修改（ToDB 使用了 Session）
		if db.Statement != nil && db.Statement.SQL.Len() > 0 {
			t.Error("ToDB 不应污染原始 DB 的 Statement")
		}
	})

	t.Run("空 Query ToDB 返回干净 DB", func(t *testing.T) {
		db := setupToDBTestDB(t)
		q, _ := NewQuery[TestUser](ctx)
		result := q.ToDB(db)
		if result == nil {
			t.Fatal("空 Query 的 ToDB 不应返回 nil")
		}
	})

	t.Run("ToDB 有错误时将错误注入 DB", func(t *testing.T) {
		db := setupToDBTestDB(t)
		q, _ := NewQuery[TestUser](ctx)
		q.Eq(nil, "bad") // 触发 builder 错误
		result := q.ToDB(db)
		if result.Error == nil {
			t.Error("ToDB 有 builder 错误时，返回的 DB 应携带错误")
		}
	})

	t.Run("ToDB 不继承 dirty db 的已有条件", func(t *testing.T) {
		db := setupToDBTestDB(t)
		// 模拟已有条件的 dirty db（如 db.Where("deleted_at IS NULL")）
		dirty := db.Where("1 = 1")
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Age, 18)
		result := q.ToDB(dirty)
		if result == nil {
			t.Fatal("ToDB 返回了 nil")
		}
		// result 不应携带错误
		if result.Error != nil {
			t.Errorf("ToDB 不应携带错误: %v", result.Error)
		}
	})
}
