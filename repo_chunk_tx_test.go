package gplus

import (
	"context"
	"errors"
	"testing"
)

// TestChunkTx_NilQuery 验证 nil query 返回 ErrQueryNil
func TestChunkTx_NilQuery(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	db := repo.GetDB()
	tx := db.Begin()
	defer tx.Rollback()
	err := repo.ChunkTx(nil, 10, tx, func(batch []TestUser) error { return nil })
	assertError(t, err, true, "nil query 应返回 ErrQueryNil")
}

// TestChunkTx_BuilderError 验证构建器错误透传
func TestChunkTx_BuilderError(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	q, _ := NewQuery[TestUser](ctx)
	q.Eq(nil, "bad")
	tx := db.Begin()
	defer tx.Rollback()
	err := repo.ChunkTx(q, 10, tx, func(batch []TestUser) error { return nil })
	assertError(t, err, true, "构建器错误应透传")
}

// TestChunkTx_NilTx 验证 tx=nil 时降级为普通连接，行为等同 Chunk
func TestChunkTx_NilTx(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	for i := 1; i <= 5; i++ {
		db.Create(&TestUser{Age: i})
	}

	var collected []TestUser
	q, _ := NewQuery[TestUser](ctx)
	err := repo.ChunkTx(q, 2, nil, func(batch []TestUser) error {
		collected = append(collected, batch...)
		return nil
	})
	assertError(t, err, false, "nil tx 应降级为普通连接")
	assertEqual(t, 5, len(collected), "应收集全部 5 条")
}

// TestChunkTx_WithTx_Commit 验证在事务中分批处理并提交
func TestChunkTx_WithTx_Commit(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	for i := 1; i <= 6; i++ {
		db.Create(&TestUser{Age: i})
	}

	var collected []TestUser
	q, _ := NewQuery[TestUser](ctx)
	tx := db.WithContext(ctx).Begin()
	err := repo.ChunkTx(q, 3, tx, func(batch []TestUser) error {
		collected = append(collected, batch...)
		return nil
	})
	tx.Commit()
	assertError(t, err, false, "事务 Chunk 不应报错")
	assertEqual(t, 6, len(collected), "应收集全部 6 条")
}

// TestChunkTx_FnError 验证 fn 返回错误时立即终止并透传
func TestChunkTx_FnError(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Age: 1})
	db.Create(&TestUser{Age: 2})

	sentinel := errors.New("stop")
	q, _ := NewQuery[TestUser](ctx)
	tx := db.WithContext(ctx).Begin()
	defer tx.Rollback()
	err := repo.ChunkTx(q, 1, tx, func(batch []TestUser) error {
		return sentinel
	})
	assertError(t, err, true, "fn 返回错误应透传")
	if !errors.Is(err, sentinel) {
		t.Errorf("期望 sentinel 错误，got: %v", err)
	}
}

// TestChunk_DelegatesToChunkTx 验证 Chunk 重构为委托 ChunkTx 后行为不变
func TestChunk_DelegatesToChunkTx(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	for i := 1; i <= 4; i++ {
		db.Create(&TestUser{Age: i})
	}

	var collected []TestUser
	q, _ := NewQuery[TestUser](ctx)
	err := repo.Chunk(q, 2, func(batch []TestUser) error {
		collected = append(collected, batch...)
		return nil
	})
	assertError(t, err, false, "Chunk 委托 ChunkTx 后不应报错")
	assertEqual(t, 4, len(collected), "应收集全部 4 条")
}
