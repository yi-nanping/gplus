package gplus

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

// TestQuery_WithScope_Nil 验证 nil fn 写入错误
func TestQuery_WithScope_Nil(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	q.WithScope(nil)
	if err := q.GetError(); err == nil {
		t.Error("nil fn 应产生错误")
	}
}

// TestUpdater_WithScope_Nil 验证 nil fn 写入错误
func TestUpdater_WithScope_Nil(t *testing.T) {
	u, _ := NewUpdater[TestUser](context.Background())
	u.WithScope(nil)
	if err := u.GetError(); err == nil {
		t.Error("nil fn 应产生错误")
	}
}

// TestQuery_WithScope_AppliedInList 验证 WithScope 在查询中生效
func TestQuery_WithScope_AppliedInList(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 20})
	db.Create(&TestUser{Name: "Bob", Age: 30})
	db.Create(&TestUser{Name: "Charlie", Age: 40})

	// 通过 WithScope 注入自定义过滤：只取 age >= 30
	q, _ := NewQuery[TestUser](ctx)
	q.WithScope(func(d *gorm.DB) *gorm.DB {
		return d.Where("age >= ?", 30)
	})

	users, err := repo.List(q)
	assertError(t, err, false, "WithScope 查询不应报错")
	assertEqual(t, 2, len(users), "age>=30 应查出 2 条")
}

// TestQuery_WithScope_MultipleScopes 验证多个 WithScope 按顺序叠加
func TestQuery_WithScope_MultipleScopes(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 20})
	db.Create(&TestUser{Name: "Bob", Age: 30})
	db.Create(&TestUser{Name: "Charlie", Age: 40})

	q, m := NewQuery[TestUser](ctx)
	// 第一个 scope：age >= 20
	q.WithScope(func(d *gorm.DB) *gorm.DB {
		return d.Where("age >= ?", 20)
	})
	// 第二个 scope：age <= 30
	q.WithScope(func(d *gorm.DB) *gorm.DB {
		return d.Where("age <= ?", 30)
	})
	// 正常条件叠加
	q.Order(&m.Age, true)

	users, err := repo.List(q)
	assertError(t, err, false, "多 scope 查询不应报错")
	assertEqual(t, 2, len(users), "20<=age<=30 应查出 2 条")
	assertEqual(t, "Alice", users[0].Name, "第一条应为 Alice")
}

// TestUpdater_WithScope_AppliedInUpdate 验证 WithScope 在更新中生效
func TestUpdater_WithScope_AppliedInUpdate(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 20})
	db.Create(&TestUser{Name: "Bob", Age: 30})

	u, m := NewUpdater[TestUser](ctx)
	// 用 WithScope 注入 WHERE 条件（补充 conditions 使其非空的方式之一）
	u.Eq(&m.Name, "Alice")
	u.WithScope(func(d *gorm.DB) *gorm.DB {
		return d.Where("age < ?", 25)
	})
	u.Set(&m.Age, 99)

	affected, err := repo.UpdateByCond(u)
	assertError(t, err, false, "WithScope 更新不应报错")
	assertEqual(t, int64(1), affected, "应更新 1 条")

	alice, _ := repo.GetById(ctx, int64(1))
	assertEqual(t, 99, alice.Age, "Alice age 应更新为 99")
	bob, _ := repo.GetById(ctx, int64(2))
	assertEqual(t, 30, bob.Age, "Bob 不应被更新")
}

// TestQuery_WithScope_ClearedByReset 验证 Clear 后 scopes 被重置
func TestQuery_WithScope_ClearedByReset(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 20})
	db.Create(&TestUser{Name: "Bob", Age: 30})

	q, _ := NewQuery[TestUser](ctx)
	q.WithScope(func(d *gorm.DB) *gorm.DB {
		return d.Where("age > ?", 100) // 过滤掉所有记录
	})
	q.Clear()

	// Clear 后 scope 应被清除，查出全部记录
	users, err := repo.List(q)
	assertError(t, err, false, "Clear 后查询不应报错")
	assertEqual(t, 2, len(users), "Clear 后应查出全部 2 条")
}
