package gplus

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

func TestGetByIds_EmptyIds(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	users, err := repo.GetByIds(ctx, []int64{})
	assertError(t, err, false, "空 ids 不应报错")
	assertEqual(t, 0, len(users), "空 ids 应返回空切片")
}

func TestGetByIds_NilIds(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	users, err := repo.GetByIds(ctx, nil)
	assertError(t, err, false, "nil ids 不应报错")
	assertEqual(t, 0, len(users), "nil ids 应返回空切片")
}

func TestGetByIds_Normal(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "Alice", Age: 20})
	db.Create(&TestUser{Name: "Bob", Age: 25})
	db.Create(&TestUser{Name: "Charlie", Age: 30})

	// 查 Alice 和 Charlie
	users, err := repo.GetByIds(ctx, []int64{1, 3})
	assertError(t, err, false, "正常查询不应报错")
	assertEqual(t, 2, len(users), "应返回 2 条记录")
}

func TestGetByIds_PartialMatch(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "Alice", Age: 20})

	// id=1 存在，id=999 不存在
	users, err := repo.GetByIds(ctx, []int64{1, 999})
	assertError(t, err, false, "部分匹配不应报错")
	assertEqual(t, 1, len(users), "只返回存在的记录")
}

func TestGetByIds_AllNotFound(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	users, err := repo.GetByIds(ctx, []int64{100, 200})
	assertError(t, err, false, "全不存在不应报错（与 GetById 不同，不返回 RecordNotFound）")
	assertEqual(t, 0, len(users), "全不存在应返回空切片")
}

func TestGetByIdsTx_Commit(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	_ = db.Transaction(func(tx *gorm.DB) error {
		tx.Create(&TestUser{Name: "Dave", Age: 35})
		users, err := repo.GetByIdsTx(ctx, []int64{1}, tx)
		assertError(t, err, false, "事务内查询不应报错")
		assertEqual(t, 1, len(users), "事务内应找到刚插入的记录")
		return nil
	})

	// 事务提交后外部也可查到
	users, err := repo.GetByIds(ctx, []int64{1})
	assertError(t, err, false, "提交后不应报错")
	assertEqual(t, 1, len(users), "提交后应能查到")
}

func TestGetByIdsTx_Rollback(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	_ = db.Transaction(func(tx *gorm.DB) error {
		tx.Create(&TestUser{Name: "Eve", Age: 28})
		return errors.New("rollback")
	})

	users, err := repo.GetByIds(ctx, []int64{1})
	assertError(t, err, false, "回滚后查询不应报错")
	assertEqual(t, 0, len(users), "回滚后不应查到数据")
}
