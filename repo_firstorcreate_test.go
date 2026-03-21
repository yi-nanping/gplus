package gplus

import (
	"context"
	"testing"
)

func TestFirstOrCreate_NilQuery(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	_, _, err := repo.FirstOrCreate(nil, nil)
	assertError(t, err, true, "nil query 应返回 ErrQueryNil")
}

func TestFirstOrCreate_BuilderError(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()
	q, _ := NewQuery[TestUser](ctx)
	q.Eq(nil, "bad") // 累积错误

	_, _, err := repo.FirstOrCreate(q, nil)
	assertError(t, err, true, "构建器错误应透传")
}

func TestFirstOrCreate_Found(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 20})

	q, u := NewQuery[TestUser](ctx)
	q.Eq(&u.Name, "Alice")

	user, created, err := repo.FirstOrCreate(q, &TestUser{Name: "Alice", Age: 99})
	assertError(t, err, false, "查到记录不应报错")
	assertEqual(t, false, created, "已存在时 created 应为 false")
	assertEqual(t, 20, user.Age, "应返回已有记录，age=20 而非 defaults 的 99")
}

func TestFirstOrCreate_NotFound_NilDefaults(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	q, u := NewQuery[TestUser](ctx)
	q.Eq(&u.Name, "Bob")

	user, created, err := repo.FirstOrCreate(q, nil)
	assertError(t, err, false, "nil defaults 创建不应报错")
	assertEqual(t, true, created, "未找到时 created 应为 true")
	assertEqual(t, "", user.Name, "nil defaults 应创建零值记录")
}

func TestFirstOrCreate_NotFound_WithDefaults(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	q, u := NewQuery[TestUser](ctx)
	q.Eq(&u.Name, "Charlie")

	defaults := &TestUser{Name: "Charlie", Age: 30}
	user, created, err := repo.FirstOrCreate(q, defaults)
	assertError(t, err, false, "创建不应报错")
	assertEqual(t, true, created, "未找到时 created 应为 true")
	assertEqual(t, "Charlie", user.Name, "应使用 defaults 创建")
	assertEqual(t, 30, user.Age, "age 应为 defaults 的 30")
}

func TestFirstOrCreate_Idempotent(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	// 第一次：创建
	q1, u1 := NewQuery[TestUser](ctx)
	q1.Eq(&u1.Name, "Dave")
	_, created1, err := repo.FirstOrCreate(q1, &TestUser{Name: "Dave", Age: 25})
	assertError(t, err, false, "第一次不应报错")
	assertEqual(t, true, created1, "第一次应创建")

	// 第二次：找到
	q2, u2 := NewQuery[TestUser](ctx)
	q2.Eq(&u2.Name, "Dave")
	user, created2, err := repo.FirstOrCreate(q2, &TestUser{Name: "Dave", Age: 99})
	assertError(t, err, false, "第二次不应报错")
	assertEqual(t, false, created2, "第二次应找到已有记录")
	assertEqual(t, 25, user.Age, "第二次应返回原记录 age=25")
}
