package gplus

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

func TestDeleteByIds_EmptyIds(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	affected, err := repo.DeleteByIds(ctx, []int64{})
	assertError(t, err, false, "空 ids 不应报错")
	assertEqual(t, int64(0), affected, "空 ids 受影响行数应为 0")
}

func TestDeleteByIds_NilIds(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	affected, err := repo.DeleteByIds(ctx, nil)
	assertError(t, err, false, "nil ids 不应报错")
	assertEqual(t, int64(0), affected, "nil ids 受影响行数应为 0")
}

func TestDeleteByIds_Normal(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "Alice", Age: 20})
	db.Create(&TestUser{Name: "Bob", Age: 25})
	db.Create(&TestUser{Name: "Charlie", Age: 30})

	affected, err := repo.DeleteByIds(ctx, []int64{1, 2})
	assertError(t, err, false, "正常删除不应报错")
	assertEqual(t, int64(2), affected, "应删除 2 条记录")

	// 验证剩余记录
	users, _ := repo.GetByIds(ctx, []int64{1, 2, 3})
	assertEqual(t, 1, len(users), "只剩 Charlie")
}

func TestDeleteByIds_PartialMatch(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "Alice", Age: 20})

	// id=1 存在，id=999 不存在
	affected, err := repo.DeleteByIds(ctx, []int64{1, 999})
	assertError(t, err, false, "部分匹配不应报错")
	assertEqual(t, int64(1), affected, "只删除存在的记录")
}

func TestDeleteByIds_AllNotFound(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	affected, err := repo.DeleteByIds(ctx, []int64{100, 200})
	assertError(t, err, false, "全不存在不应报错")
	assertEqual(t, int64(0), affected, "全不存在受影响行数为 0")
}

func TestDeleteByIdsTx_Commit(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "Dave", Age: 35})

	_ = db.Transaction(func(tx *gorm.DB) error {
		affected, err := repo.DeleteByIdsTx(ctx, []int64{1}, tx)
		assertError(t, err, false, "事务内删除不应报错")
		assertEqual(t, int64(1), affected, "事务内应删除 1 条")
		return nil
	})

	// 提交后确认已删除
	users, _ := repo.GetByIds(ctx, []int64{1})
	assertEqual(t, 0, len(users), "提交后记录应已删除")
}

func TestDeleteByIdsTx_Rollback(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "Eve", Age: 28})

	_ = db.Transaction(func(tx *gorm.DB) error {
		repo.DeleteByIdsTx(ctx, []int64{1}, tx)
		return errors.New("rollback")
	})

	// 回滚后记录应仍存在
	users, _ := repo.GetByIds(ctx, []int64{1})
	assertEqual(t, 1, len(users), "回滚后记录应仍存在")
}
