package gplus

import (
	"context"
	"testing"
)

// TestRestore_NotDeleted 验证未软删除的记录恢复后 affected=0
func TestRestore_NotDeleted(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()
	user := UserWithDelete{Name: "Alice", Age: 18}
	db.Create(&user)

	affected, err := repo.Restore(ctx, user.ID)
	assertError(t, err, false, "未软删除的记录恢复不应报错")
	assertEqual(t, int64(0), affected, "未软删除时 affected 应为 0")
}

// TestRestore_Normal 验证软删除后恢复，记录重新可查
func TestRestore_Normal(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()
	user := UserWithDelete{Name: "Bob", Age: 20}
	db.Create(&user)
	db.Delete(&user)

	// 确认已软删除
	var count int64
	db.Model(&UserWithDelete{}).Where("id = ?", user.ID).Count(&count)
	assertEqual(t, int64(0), count, "软删除后查不到")

	affected, err := repo.Restore(ctx, user.ID)
	assertError(t, err, false, "恢复不应报错")
	assertEqual(t, int64(1), affected, "恢复后 affected 应为 1")

	// 确认已恢复
	db.Model(&UserWithDelete{}).Where("id = ?", user.ID).Count(&count)
	assertEqual(t, int64(1), count, "恢复后应能查到")
}

// TestRestore_NonExistent 验证不存在的 id 恢复后 affected=0
func TestRestore_NonExistent(t *testing.T) {
	repo, _ := setupAdvancedDB(t)
	ctx := context.Background()

	affected, err := repo.Restore(ctx, 9999)
	assertError(t, err, false, "不存在的记录恢复不应报错")
	assertEqual(t, int64(0), affected, "不存在时 affected 应为 0")
}

// TestRestoreTx_Commit 验证在事务中恢复并提交
func TestRestoreTx_Commit(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()
	user := UserWithDelete{Name: "Charlie", Age: 22}
	db.Create(&user)
	db.Delete(&user)

	tx := db.WithContext(ctx).Begin()
	affected, err := repo.RestoreTx(ctx, user.ID, tx)
	tx.Commit()

	assertError(t, err, false, "事务恢复不应报错")
	assertEqual(t, int64(1), affected, "事务提交后 affected 应为 1")

	var count int64
	db.Model(&UserWithDelete{}).Where("id = ?", user.ID).Count(&count)
	assertEqual(t, int64(1), count, "提交后应能查到")
}

// TestRestoreTx_Rollback 验证在事务中恢复后回滚，记录仍为软删除状态
func TestRestoreTx_Rollback(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()
	user := UserWithDelete{Name: "Dave", Age: 24}
	db.Create(&user)
	db.Delete(&user)

	tx := db.WithContext(ctx).Begin()
	_, err := repo.RestoreTx(ctx, user.ID, tx)
	assertError(t, err, false, "事务恢复不应报错")
	tx.Rollback()

	// 回滚后记录应仍为软删除
	var count int64
	db.Model(&UserWithDelete{}).Where("id = ?", user.ID).Count(&count)
	assertEqual(t, int64(0), count, "回滚后记录应仍为软删除状态")
}

// TestRestore_Idempotent 验证多次恢复是幂等的
func TestRestore_Idempotent(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()
	user := UserWithDelete{Name: "Eve", Age: 26}
	db.Create(&user)
	db.Delete(&user)

	// 第一次恢复
	affected1, err := repo.Restore(ctx, user.ID)
	assertError(t, err, false, "第一次恢复不应报错")
	assertEqual(t, int64(1), affected1, "第一次 affected 应为 1")

	// 第二次恢复（已恢复，不应报错）
	affected2, err := repo.Restore(ctx, user.ID)
	assertError(t, err, false, "第二次恢复不应报错")
	assertEqual(t, int64(0), affected2, "第二次 affected 应为 0")
}
