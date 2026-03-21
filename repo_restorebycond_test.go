package gplus

import (
	"context"
	"errors"
	"testing"
)

// TestRestoreByCond_NilQuery 验证 nil query 返回 ErrQueryNil
func TestRestoreByCond_NilQuery(t *testing.T) {
	repo, _ := setupAdvancedDB(t)
	_, err := repo.RestoreByCond(nil)
	if !errors.Is(err, ErrQueryNil) {
		t.Errorf("期望 ErrQueryNil，得到 %v", err)
	}
}

// TestRestoreByCond_EmptyQuery 验证空条件返回 ErrRestoreEmpty
func TestRestoreByCond_EmptyQuery(t *testing.T) {
	repo, _ := setupAdvancedDB(t)
	q, _ := NewQuery[UserWithDelete](context.Background())
	_, err := repo.RestoreByCond(q)
	if !errors.Is(err, ErrRestoreEmpty) {
		t.Errorf("期望 ErrRestoreEmpty，得到 %v", err)
	}
}

// TestRestoreByCond_QueryBuilderError 验证构建器错误被提前返回
func TestRestoreByCond_QueryBuilderError(t *testing.T) {
	repo, _ := setupAdvancedDB(t)
	q, _ := NewQuery[UserWithDelete](context.Background())
	q.Eq(nil, "bad") // 累积错误
	_, err := repo.RestoreByCond(q)
	assertError(t, err, true, "构建器错误应被返回")
}

// TestRestoreByCond_Normal 验证按条件批量恢复软删除记录
func TestRestoreByCond_Normal(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	// 创建并软删除两条记录
	u1 := UserWithDelete{Name: "Alice", Age: 20}
	u2 := UserWithDelete{Name: "Bob", Age: 25}
	u3 := UserWithDelete{Name: "Charlie", Age: 30}
	db.Create(&u1)
	db.Create(&u2)
	db.Create(&u3)
	db.Delete(&u1)
	db.Delete(&u2)
	// u3 不删除

	// 按 age <= 25 恢复（Alice 和 Bob）
	q, u := NewQuery[UserWithDelete](ctx)
	q.Le(&u.Age, 25)
	affected, err := repo.RestoreByCond(q)
	assertError(t, err, false, "RestoreByCond 应成功")
	assertEqual(t, int64(2), affected, "应恢复 2 条")

	// 验证 Alice 和 Bob 已恢复
	var count int64
	db.Model(&UserWithDelete{}).Where("id IN ?", []int64{u1.ID, u2.ID}).Count(&count)
	assertEqual(t, int64(2), count, "Alice 和 Bob 应已恢复")
}

// TestRestoreByCond_OnlySoftDeleted 验证只影响已软删除的行
func TestRestoreByCond_OnlySoftDeleted(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	u1 := UserWithDelete{Name: "Alice", Age: 20}
	u2 := UserWithDelete{Name: "Bob", Age: 25}
	db.Create(&u1)
	db.Create(&u2)
	db.Delete(&u1) // 只删除 Alice
	// Bob 未删除

	q, u := NewQuery[UserWithDelete](ctx)
	q.Le(&u.Age, 30)
	affected, err := repo.RestoreByCond(q)
	assertError(t, err, false, "RestoreByCond 应成功")
	// 只有 Alice 被软删除，Bob 没有，所以只有 1 条被恢复
	assertEqual(t, int64(1), affected, "只有已软删除的记录应被恢复")
}

// TestRestoreByCond_NoMatch 验证条件无匹配时 affected=0
func TestRestoreByCond_NoMatch(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	u1 := UserWithDelete{Name: "Alice", Age: 20}
	db.Create(&u1)
	db.Delete(&u1)

	q, u := NewQuery[UserWithDelete](ctx)
	q.Gt(&u.Age, 100) // 不可能匹配
	affected, err := repo.RestoreByCond(q)
	assertError(t, err, false, "无匹配不应报错")
	assertEqual(t, int64(0), affected, "无匹配时 affected 应为 0")
}

// TestRestoreByCondTx_Commit 验证事务提交后记录恢复
func TestRestoreByCondTx_Commit(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	u1 := UserWithDelete{Name: "Alice", Age: 20}
	db.Create(&u1)
	db.Delete(&u1)

	tx := db.WithContext(ctx).Begin()
	q, u := NewQuery[UserWithDelete](ctx)
	q.Eq(&u.Name, "Alice")
	affected, err := repo.RestoreByCondTx(q, tx)
	tx.Commit()

	assertError(t, err, false, "事务恢复不应报错")
	assertEqual(t, int64(1), affected, "事务提交后应恢复 1 条")

	var count int64
	db.Model(&UserWithDelete{}).Where("id = ?", u1.ID).Count(&count)
	assertEqual(t, int64(1), count, "提交后应能查到")
}

// TestRestoreByCondTx_Rollback 验证事务回滚后记录仍为软删除
func TestRestoreByCondTx_Rollback(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	u1 := UserWithDelete{Name: "Alice", Age: 20}
	db.Create(&u1)
	db.Delete(&u1)

	tx := db.WithContext(ctx).Begin()
	q, u := NewQuery[UserWithDelete](ctx)
	q.Eq(&u.Name, "Alice")
	_, _ = repo.RestoreByCondTx(q, tx)
	tx.Rollback()

	var count int64
	db.Model(&UserWithDelete{}).Where("id = ?", u1.ID).Count(&count)
	assertEqual(t, int64(0), count, "回滚后记录应仍为软删除状态")
}
