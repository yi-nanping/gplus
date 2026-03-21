package gplus

import (
	"context"
	"errors"
	"testing"
)

func TestChunk_NilQuery(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	err := repo.Chunk(nil, 10, func(batch []TestUser) error { return nil })
	assertError(t, err, true, "nil query 应返回 ErrQueryNil")
}

func TestChunk_BuilderError(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()
	q, _ := NewQuery[TestUser](ctx)
	q.Eq(nil, "bad") // 累积错误

	err := repo.Chunk(q, 10, func(batch []TestUser) error { return nil })
	assertError(t, err, true, "构建器错误应透传")
}

func TestChunk_Normal(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	for i := 1; i <= 10; i++ {
		db.Create(&TestUser{Age: i})
	}

	var collected []TestUser
	q, _ := NewQuery[TestUser](ctx)
	err := repo.Chunk(q, 3, func(batch []TestUser) error {
		collected = append(collected, batch...)
		return nil
	})
	assertError(t, err, false, "正常 Chunk 不应报错")
	assertEqual(t, 10, len(collected), "应收集全部 10 条")
}

func TestChunk_WithCondition(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	for i := 1; i <= 10; i++ {
		db.Create(&TestUser{Age: i})
	}

	var collected []TestUser
	q, u := NewQuery[TestUser](ctx)
	q.Gt(&u.Age, 5)
	err := repo.Chunk(q, 2, func(batch []TestUser) error {
		collected = append(collected, batch...)
		return nil
	})
	assertError(t, err, false, "条件 Chunk 不应报错")
	assertEqual(t, 5, len(collected), "age>5 应收集 5 条")
}

func TestChunk_EmptyTable(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	called := false
	q, _ := NewQuery[TestUser](ctx)
	err := repo.Chunk(q, 10, func(batch []TestUser) error {
		called = true
		return nil
	})
	assertError(t, err, false, "空表不应报错")
	assertEqual(t, false, called, "空表时 fn 不应被调用")
}

func TestChunk_FnError(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Age: 1})
	db.Create(&TestUser{Age: 2})

	sentinel := errors.New("stop")
	q, _ := NewQuery[TestUser](ctx)
	err := repo.Chunk(q, 1, func(batch []TestUser) error {
		return sentinel
	})
	assertError(t, err, true, "fn 返回错误应终止并透传")
	if !errors.Is(err, sentinel) {
		t.Errorf("期望 sentinel 错误，got: %v", err)
	}
}

func TestChunk_BatchSizeExact(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	for i := 1; i <= 6; i++ {
		db.Create(&TestUser{Age: i})
	}

	batches := 0
	q, _ := NewQuery[TestUser](ctx)
	err := repo.Chunk(q, 3, func(batch []TestUser) error {
		batches++
		return nil
	})
	assertError(t, err, false, "整除批次不应报错")
	assertEqual(t, 2, batches, "6条/批大小3 应分 2 批")
}
