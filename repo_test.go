package gplus

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestDB 初始化内存数据库并返回 Repository
func setupTestDB[T any](t *testing.T) (*Repository[int64, T], *gorm.DB) {
	// 使用 sqlite 内存模式
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}

	// 自动迁移表结构
	err = db.AutoMigrate(new(T))
	if err != nil {
		t.Fatalf("failed to migrate table: %v", err)
	}

	return NewRepository[int64, T](db), db
}

// TestRepository_CRUD_And_Errors 测试 CRUD 和错误处理
func TestRepository_CRUD_And_Errors(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	// 插入模拟数据
	db.Create(&TestUser{Name: "Alice", Age: 20, Email: "alice@example.com"})
	db.Create(&TestUser{Name: "Bob", Age: 25, Email: "bob@example.com"})

	t.Run("正常 List 查询", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "Alice")

		users, err := repo.List(q)
		assertError(t, err, false, "List should succeed")
		assertEqual(t, 1, len(users), "Should find 1 user")
	})

	t.Run("List 错误列名触发 panic", func(t *testing.T) {
		assertPanics(t, func() {
			q, _ := NewQuery[TestUser](ctx)
			q.Eq(nil, "fail")
		}, "Eq(nil) 应触发 panic")
	})

	t.Run("Update 错误列名触发 panic", func(t *testing.T) {
		_, u := NewUpdater[TestUser](ctx)
		assertPanics(t, func() {
			updater, _ := NewUpdater[TestUser](ctx)
			updater.Set(&u.Name, "NewName").Eq(new(int), 1)
		}, "Eq(new(int)) 应触发 panic")
	})
}

// TestRepository_AdvancedFeatures 分页与原生 SQL 测试
func TestRepository_AdvancedFeatures(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	// 准备 10 条数据
	for i := 1; i <= 10; i++ {
		db.Create(&TestUser{Age: i + 10})
	}

	t.Run("分页查询测试", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Gt(&u.Age, 10).Order(&u.Age, true)
		q.Limit(3).Offset(0)

		// 查询第一页，每页 3 条
		res, total, err := repo.Page(q, false)
		assertError(t, err, false, "Page query should succeed")
		assertEqual(t, int64(10), total, "Total count should be 10")
		assertEqual(t, 3, len(res), "Record count should be 3")
		assertEqual(t, 11, res[0].Age, "First record age should be 11")
	})

	t.Run("原生 SQL 映射测试", func(t *testing.T) {
		// 测试 RawQuery
		users, err := repo.RawQuery(ctx, "SELECT * FROM test_users WHERE age > ?", 18)
		assertError(t, err, false, "RawQuery should succeed")
		if len(users) == 0 {
			t.Error("RawQuery should return results")
		}
	})
}
