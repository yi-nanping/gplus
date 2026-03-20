package gplus

import (
	"context"
	"errors"
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

	t.Run("List 错误列名累积错误", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Eq(nil, "fail")
		assertError(t, q.GetError(), true, "Eq(nil) 应累积错误")
	})

	t.Run("Update 错误列名累积错误", func(t *testing.T) {
		updater, u := NewUpdater[TestUser](ctx)
		updater.Set(&u.Name, "NewName").Eq(new(int), 1)
		assertError(t, updater.GetError(), true, "Eq(new(int)) 应累积错误")
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
		// 10 条数据 age=11~20，WHERE age>18 → age=19 和 age=20 共 2 条
		users, err := repo.RawQuery(ctx, "SELECT * FROM test_users WHERE age > ? ORDER BY age", 18)
		assertError(t, err, false, "RawQuery should succeed")
		if len(users) != 2 {
			t.Errorf("expected 2 users with age>18, got %d", len(users))
		}
		if len(users) == 2 && (users[0].Age != 19 || users[1].Age != 20) {
			t.Errorf("expected age=[19,20], got [%d,%d]", users[0].Age, users[1].Age)
		}
	})
}

// TestPluck 测试 Pluck 泛型函数
func TestPluck(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	// 预置数据
	seeds := []*TestUser{
		{Name: "Alice", Age: 25, Email: "alice@example.com"},
		{Name: "Bob", Age: 30, Email: "bob@example.com"},
		{Name: "Charlie", Age: 25, Email: "charlie@example.com"},
	}
	for _, u := range seeds {
		if err := repo.Save(ctx, u); err != nil {
			t.Fatalf("预置数据失败: %v", err)
		}
	}

	t.Run("提取所有用户名", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Order(&u.Name, true)
		names, err := Pluck[TestUser, string, int64](repo, q, &u.Name)
		assertError(t, err, false, "Pluck 应成功")
		if len(names) != 3 {
			t.Errorf("应返回 3 条记录，实际: %d", len(names))
		}
		assertEqual(t, "Alice", names[0], "首条应为 Alice")
	})

	t.Run("带条件提取年龄", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "Bob")
		ages, err := Pluck[TestUser, int, int64](repo, q, &u.Age)
		assertError(t, err, false, "Pluck 应成功")
		if len(ages) != 1 {
			t.Errorf("应返回 1 条记录，实际: %d", len(ages))
		}
		assertEqual(t, 30, ages[0], "Bob 年龄应为 30")
	})

	t.Run("字符串列名提取邮箱", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		emails, err := Pluck[TestUser, string, int64](repo, q, "email")
		assertError(t, err, false, "Pluck 应成功")
		if len(emails) != 3 {
			t.Errorf("应返回 3 条邮箱，实际: %d", len(emails))
		}
	})

	t.Run("条件不匹配返回空切片", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "NotExist")
		names, err := Pluck[TestUser, string, int64](repo, q, &u.Name)
		assertError(t, err, false, "Pluck 应成功")
		if len(names) != 0 {
			t.Errorf("应返回空切片，实际: %v", names)
		}
	})

	t.Run("nil Query 返回 ErrQueryNil", func(t *testing.T) {
		_, err := Pluck[TestUser, string, int64](repo, nil, "username")
		if !errors.Is(err, ErrQueryNil) {
			t.Errorf("nil Query 应返回 ErrQueryNil，实际: %v", err)
		}
	})
}

// TestRepository_BasicCRUD 覆盖 Save/UpdateById/UpdateByCond/DeleteById/DeleteByCond/RecordNotFound
func TestRepository_BasicCRUD(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	t.Run("Create_Save", func(t *testing.T) {
		user := &TestUser{Name: "Alice", Age: 25, Email: "alice@example.com"}
		err := repo.Save(ctx, user)
		assertError(t, err, false, "Save 应成功")
		if user.ID == 0 {
			t.Error("Save 后 ID 应被赋值")
		}
	})

	t.Run("Update_ById", func(t *testing.T) {
		user := &TestUser{Name: "Bob", Age: 20}
		_ = repo.Save(ctx, user)
		user.Age = 30
		err := repo.UpdateById(ctx, user)
		assertError(t, err, false, "UpdateById 应成功")
		got, err := repo.GetById(ctx, user.ID)
		assertError(t, err, false, "GetById 应成功")
		assertEqual(t, 30, got.Age, "Age 应已更新为 30")
	})

	t.Run("Update_ByCond", func(t *testing.T) {
		user := &TestUser{Name: "Charlie", Age: 18}
		_ = repo.Save(ctx, user)
		upd, uu := NewUpdater[TestUser](ctx)
		upd.Set(&uu.Age, 99).Eq(&uu.Name, "Charlie")
		affected, err := repo.UpdateByCond(upd)
		assertError(t, err, false, "UpdateByCond 应成功")
		assertEqual(t, int64(1), affected, "UpdateByCond 应影响 1 行")
	})

	t.Run("Delete_ById", func(t *testing.T) {
		user := &TestUser{Name: "Dave", Age: 22}
		_ = repo.Save(ctx, user)
		affected, err := repo.DeleteById(ctx, user.ID)
		assertError(t, err, false, "DeleteById 应成功")
		assertEqual(t, int64(1), affected, "DeleteById 应影响 1 行")
		_, err = repo.GetById(ctx, user.ID)
		if !IsNotFound(err) {
			t.Errorf("删除后 GetById 应返回 RecordNotFound，实际: %v", err)
		}
	})

	t.Run("Delete_ByCond", func(t *testing.T) {
		user := &TestUser{Name: "Eve", Age: 28}
		_ = repo.Save(ctx, user)
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "Eve")
		affected, err := repo.DeleteByCond(q)
		assertError(t, err, false, "DeleteByCond 应成功")
		assertEqual(t, int64(1), affected, "DeleteByCond 应影响 1 行")
	})

	t.Run("RecordNotFound", func(t *testing.T) {
		_, err := repo.GetById(ctx, 99999)
		if !IsNotFound(err) {
			t.Errorf("查询不存在的 ID 应返回 RecordNotFound，实际: %v", err)
		}
	})
}
