package gplus

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

func TestUpdateByIds_EmptyIds(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()
	u, m := NewUpdater[TestUser](ctx)
	u.Set(&m.Name, "X")

	affected, err := repo.UpdateByIds(ctx, []int64{}, u)
	assertError(t, err, false, "空 ids 不应报错")
	assertEqual(t, int64(0), affected, "空 ids 受影响行数应为 0")
}

func TestUpdateByIds_NilUpdater(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	_, err := repo.UpdateByIds(ctx, []int64{1}, nil)
	assertError(t, err, true, "nil updater 应返回错误")
}

func TestUpdateByIds_EmptySetMap(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()
	u, _ := NewUpdater[TestUser](ctx)
	// 没有调用 Set，setMap 为空

	_, err := repo.UpdateByIds(ctx, []int64{1}, u)
	assertError(t, err, true, "空 setMap 应返回 ErrUpdateEmpty")
}

func TestUpdateByIds_BuilderError(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()
	u, m := NewUpdater[TestUser](ctx)
	u.Set(&m.Name, "X")
	u.Eq(nil, "bad") // 累积错误

	_, err := repo.UpdateByIds(ctx, []int64{1}, u)
	assertError(t, err, true, "构建器错误应透传")
}

func TestUpdateByIds_Normal(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 20})
	db.Create(&TestUser{Name: "Bob", Age: 25})
	db.Create(&TestUser{Name: "Charlie", Age: 30})

	u, m := NewUpdater[TestUser](ctx)
	u.Set(&m.Age, 99)

	affected, err := repo.UpdateByIds(ctx, []int64{1, 2}, u)
	assertError(t, err, false, "正常更新不应报错")
	assertEqual(t, int64(2), affected, "应更新 2 条")

	// 验证 Alice 和 Bob 已更新，Charlie 未变
	users, _ := repo.GetByIds(ctx, []int64{1, 2, 3})
	for _, usr := range users {
		if usr.Name == "Charlie" {
			assertEqual(t, 30, usr.Age, "Charlie 不应被更新")
		} else {
			assertEqual(t, 99, usr.Age, usr.Name+" 应被更新为 99")
		}
	}
}

func TestUpdateByIds_PartialMatch(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 20})

	u, m := NewUpdater[TestUser](ctx)
	u.Set(&m.Age, 88)

	affected, err := repo.UpdateByIds(ctx, []int64{1, 999}, u)
	assertError(t, err, false, "部分匹配不应报错")
	assertEqual(t, int64(1), affected, "只更新存在的记录")
}

func TestUpdateByIdsTx_Commit(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Dave", Age: 35})

	_ = db.Transaction(func(tx *gorm.DB) error {
		u, m := NewUpdater[TestUser](ctx)
		u.Set(&m.Age, 77)
		affected, err := repo.UpdateByIdsTx(ctx, []int64{1}, u, tx)
		assertError(t, err, false, "事务内更新不应报错")
		assertEqual(t, int64(1), affected, "事务内应更新 1 条")
		return nil
	})

	users, _ := repo.GetByIds(ctx, []int64{1})
	assertEqual(t, 1, len(users), "提交后记录应存在")
	assertEqual(t, 77, users[0].Age, "提交后 age 应为 77")
}
