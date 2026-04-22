package gplus

import (
	"context"
	"testing"
)

// TestFirstOrUpdate_NilQuery 验证 nil query 返回 ErrQueryNil
func TestFirstOrUpdate_NilQuery(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()
	u, _ := NewUpdater[TestUser](ctx)
	_, _, err := repo.FirstOrUpdate(nil, u, &TestUser{})
	assertError(t, err, true, "nil query 应返回 ErrQueryNil")
}

// TestFirstOrUpdate_NilDefaults 验证 nil defaults 返回 ErrDefaultsNil
func TestFirstOrUpdate_NilDefaults(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()
	q, _ := NewQuery[TestUser](ctx)
	u, um := NewUpdater[TestUser](ctx)
	u.Set(&um.Age, 99)
	_, _, err := repo.FirstOrUpdate(q, u, nil)
	assertError(t, err, true, "nil defaults 应返回 ErrDefaultsNil")
}

// TestFirstOrUpdate_NilUpdater 验证 nil updater 返回 ErrUpdateEmpty
func TestFirstOrUpdate_NilUpdater(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()
	q, _ := NewQuery[TestUser](ctx)
	_, _, err := repo.FirstOrUpdate(q, nil, &TestUser{})
	assertError(t, err, true, "nil updater 应返回 ErrUpdateEmpty")
}

// TestFirstOrUpdate_EmptyUpdater 验证空 updater（无 Set 字段）返回 ErrUpdateEmpty
func TestFirstOrUpdate_EmptyUpdater(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()
	q, _ := NewQuery[TestUser](ctx)
	u, _ := NewUpdater[TestUser](ctx)
	_, _, err := repo.FirstOrUpdate(q, u, &TestUser{})
	assertError(t, err, true, "空 updater 应返回 ErrUpdateEmpty")
}

// TestFirstOrUpdate_QueryBuilderError 验证 query 构建器错误透传
func TestFirstOrUpdate_QueryBuilderError(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()
	q, _ := NewQuery[TestUser](ctx)
	q.Eq(nil, "bad")
	u, um := NewUpdater[TestUser](ctx)
	u.Set(&um.Age, 99)
	_, _, err := repo.FirstOrUpdate(q, u, &TestUser{})
	assertError(t, err, true, "query 构建器错误应透传")
}

// TestFirstOrUpdate_UpdaterBuilderError 验证 updater 构建器错误透传
func TestFirstOrUpdate_UpdaterBuilderError(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()
	q, qm := NewQuery[TestUser](ctx)
	q.Eq(&qm.Name, "Alice")
	u, um := NewUpdater[TestUser](ctx)
	u.Set(&um.Age, 99)
	u.Eq(nil, "bad") // 累积错误
	_, _, err := repo.FirstOrUpdate(q, u, &TestUser{})
	assertError(t, err, true, "updater 构建器错误应透传")
}

// TestFirstOrUpdate_Found_Updated 验证找到记录时执行更新，created=false
func TestFirstOrUpdate_Found_Updated(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 18})

	q, qm := NewQuery[TestUser](ctx)
	q.Eq(&qm.Name, "Alice")
	u, um := NewUpdater[TestUser](ctx)
	u.Set(&um.Age, 30)

	data, created, err := repo.FirstOrUpdate(q, u, &TestUser{Name: "Alice", Age: 18})
	assertError(t, err, false, "找到记录时不应报错")
	assertEqual(t, false, created, "找到记录时 created 应为 false")
	assertEqual(t, "Alice", data.Name, "返回的记录 Name 应为 Alice")
	assertEqual(t, 30, data.Age, "返回的记录 Age 应已更新为 30")

	// 验证数据库实际更新
	var check TestUser
	db.Where("username = ?", "Alice").First(&check)
	assertEqual(t, 30, check.Age, "数据库中 Age 应已更新为 30")
}

// TestFirstOrUpdate_UpdateQueryField 验证更新的字段与查询条件字段相同时返回值仍正确
// 回归：旧实现用 data（含旧字段值）重读，更新查询字段后会找不到记录
func TestFirstOrUpdate_UpdateQueryField(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Old", Age: 20})

	q, qm := NewQuery[TestUser](ctx)
	q.Eq(&qm.Name, "Old")
	u, um := NewUpdater[TestUser](ctx)
	u.Set(&um.Name, "New") // 更新的字段正是查询条件字段

	data, created, err := repo.FirstOrUpdate(q, u, &TestUser{Name: "Old", Age: 20})
	assertError(t, err, false, "更新查询条件字段时不应报错")
	assertEqual(t, false, created, "记录已存在，created 应为 false")
	assertEqual(t, "New", data.Name, "返回的 Name 应已更新为 New")
}

// TestFirstOrUpdate_NotFound_Created 验证未找到记录时用 defaults 创建，created=true
func TestFirstOrUpdate_NotFound_Created(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	q, qm := NewQuery[TestUser](ctx)
	q.Eq(&qm.Name, "Bob")
	u, um := NewUpdater[TestUser](ctx)
	u.Set(&um.Age, 25)
	defaults := &TestUser{Name: "Bob", Age: 20}

	data, created, err := repo.FirstOrUpdate(q, u, defaults)
	assertError(t, err, false, "未找到时创建不应报错")
	assertEqual(t, true, created, "未找到时 created 应为 true")
	assertEqual(t, "Bob", data.Name, "创建的记录 Name 应为 Bob")
	assertEqual(t, 20, data.Age, "创建时使用 defaults，Age 应为 20")

	// 验证数据库中已创建
	var count int64
	db.Model(&TestUser{}).Where("username = ?", "Bob").Count(&count)
	assertEqual(t, int64(1), count, "数据库中应有 1 条 Bob 记录")
}
