package gplus

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

// TestIncrBy_NilUpdater 验证 nil updater 返回错误
func TestIncrBy_NilUpdater(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	_, err := repo.IncrBy(nil, nil, 1)
	assertError(t, err, true, "nil updater 应返回错误")
}

// TestIncrBy_NoCondition 验证无 WHERE 条件时拒绝全表更新
func TestIncrBy_NoCondition(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	u, _ := NewUpdater[TestUser](context.Background())
	_, err := repo.IncrBy(u, nil, 1)
	if !errors.Is(err, ErrUpdateNoCondition) {
		t.Errorf("无条件应返回 ErrUpdateNoCondition，实际: %v", err)
	}
}

// TestIncrBy_BuilderError 验证构建器错误透传
func TestIncrBy_BuilderError(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	u, _ := NewUpdater[TestUser](context.Background())
	u.Eq(nil, "bad") // 累积错误
	_, err := repo.IncrBy(u, nil, 1)
	assertError(t, err, true, "构建器错误应透传")
}

// TestIncrBy_Normal 验证正常自增
func TestIncrBy_Normal(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 20})
	db.Create(&TestUser{Name: "Bob", Age: 25})

	u, m := NewUpdater[TestUser](ctx)
	u.Eq(&m.Name, "Alice")

	affected, err := repo.IncrBy(u, &m.Age, 5)
	assertError(t, err, false, "正常自增不应报错")
	assertEqual(t, int64(1), affected, "应影响 1 行")

	// 验证 Alice age = 25，Bob 不变
	alice, _ := repo.GetById(ctx, int64(1))
	assertEqual(t, 25, alice.Age, "Alice age 应自增为 25")
	bob, _ := repo.GetById(ctx, int64(2))
	assertEqual(t, 25, bob.Age, "Bob age 不应改变")
}

// TestIncrBy_MultiRow 验证多行自增
func TestIncrBy_MultiRow(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 10})
	db.Create(&TestUser{Name: "Alice", Age: 20})

	u, m := NewUpdater[TestUser](ctx)
	u.Eq(&m.Name, "Alice")

	affected, err := repo.IncrBy(u, &m.Age, 3)
	assertError(t, err, false, "多行自增不应报错")
	assertEqual(t, int64(2), affected, "应影响 2 行")

	users, _ := repo.List(func() *Query[TestUser] {
		q, qm := NewQuery[TestUser](ctx)
		q.Eq(&qm.Name, "Alice")
		return q
	}())
	for _, usr := range users {
		if usr.Age != 13 && usr.Age != 23 {
			t.Errorf("age 应自增 3，实际: %d", usr.Age)
		}
	}
}

// TestIncrByTx_Commit 验证事务提交后自增生效
func TestIncrByTx_Commit(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Dave", Age: 30})

	_ = db.Transaction(func(tx *gorm.DB) error {
		u, m := NewUpdater[TestUser](ctx)
		u.Eq(&m.Name, "Dave")
		affected, err := repo.IncrByTx(u, &m.Age, 10, tx)
		assertError(t, err, false, "事务内自增不应报错")
		assertEqual(t, int64(1), affected, "事务内应影响 1 行")
		return nil
	})

	dave, _ := repo.GetById(ctx, int64(1))
	assertEqual(t, 40, dave.Age, "提交后 age 应为 40")
}

// TestIncrByTx_Rollback 验证事务回滚后自增不生效
func TestIncrByTx_Rollback(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Eve", Age: 50})

	_ = db.Transaction(func(tx *gorm.DB) error {
		u, m := NewUpdater[TestUser](ctx)
		u.Eq(&m.Name, "Eve")
		_, _ = repo.IncrByTx(u, &m.Age, 10, tx)
		return errors.New("rollback")
	})

	eve, _ := repo.GetById(ctx, int64(1))
	assertEqual(t, 50, eve.Age, "回滚后 age 应保持 50")
}

// TestDecrBy_Normal 验证正常自减
func TestDecrBy_Normal(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Frank", Age: 30})

	u, m := NewUpdater[TestUser](ctx)
	u.Eq(&m.Name, "Frank")

	affected, err := repo.DecrBy(u, &m.Age, 5)
	assertError(t, err, false, "正常自减不应报错")
	assertEqual(t, int64(1), affected, "应影响 1 行")

	frank, _ := repo.GetById(ctx, int64(1))
	assertEqual(t, 25, frank.Age, "Frank age 应自减为 25")
}

// TestDecrByTx_Commit 验证事务自减提交
func TestDecrByTx_Commit(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Grace", Age: 100})

	_ = db.Transaction(func(tx *gorm.DB) error {
		u, m := NewUpdater[TestUser](ctx)
		u.Eq(&m.Name, "Grace")
		affected, err := repo.DecrByTx(u, &m.Age, 20, tx)
		assertError(t, err, false, "事务自减不应报错")
		assertEqual(t, int64(1), affected, "事务内应影响 1 行")
		return nil
	})

	grace, _ := repo.GetById(ctx, int64(1))
	assertEqual(t, 80, grace.Age, "提交后 age 应为 80")
}
