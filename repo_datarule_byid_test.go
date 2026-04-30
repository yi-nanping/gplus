package gplus

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// tenantUser 用于 DataRule by-ID 测试的专用模型，不污染 UserWithDelete
type tenantUser struct {
	ID        int64          `gorm:"primaryKey;autoIncrement"`
	Name      string         `gorm:"size:64"`
	TenantID  int            `gorm:"index;column:tenant_id"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

// setupTenantDB 创建独立的 SQLite 内存库，迁移 tenantUser 表
func setupTenantDB(t *testing.T) (*Repository[int64, tenantUser], *gorm.DB) {
	t.Helper()
	db := openDB(t)
	if err := db.AutoMigrate(&tenantUser{}); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	if db.Name() == "mysql" {
		truncateTables(t, db, &tenantUser{})
		t.Cleanup(func() { truncateTables(t, db, &tenantUser{}) })
	}
	return NewRepository[int64, tenantUser](db), db
}

// ctxWithTenantRule 构造携带 tenant_id 数据权限规则的 context
func ctxWithTenantRule(tenantID int) context.Context {
	return context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Column: "tenant_id", Condition: "=", Value: fmt.Sprintf("%d", tenantID)},
	})
}

// insertTenantUsers 插入两个租户的测试数据，返回 (tenant1ID, tenant2ID)
func insertTenantUsers(t *testing.T, db *gorm.DB) (int64, int64) {
	t.Helper()
	u1 := &tenantUser{Name: "Alice", TenantID: 1}
	u2 := &tenantUser{Name: "Bob", TenantID: 2}
	if err := db.Create(u1).Error; err != nil {
		t.Fatalf("insert tenant1 user failed: %v", err)
	}
	if err := db.Create(u2).Error; err != nil {
		t.Fatalf("insert tenant2 user failed: %v", err)
	}
	return u1.ID, u2.ID
}

// TestDataRule_GetById_Blocked 跨租户 GetById 应返回 gorm.ErrRecordNotFound
func TestDataRule_GetById_Blocked(t *testing.T) {
	repo, db := setupTenantDB(t)
	tenant1ID, tenant2ID := insertTenantUsers(t, db)

	// tenant1 用 tenant2 的 ID 查询，应被 DataRule 拦截
	ctx := ctxWithTenantRule(1)
	_, err := repo.GetByIdTx(ctx, tenant2ID, nil)
	if err == nil {
		t.Fatal("期望 ErrRecordNotFound，实际未返回错误")
	}
	if err != gorm.ErrRecordNotFound {
		t.Fatalf("期望 gorm.ErrRecordNotFound，实际: %v", err)
	}

	// 正常路径验证：同租户 ID 能查到
	got, err := repo.GetByIdTx(ctx, tenant1ID, nil)
	if err != nil {
		t.Fatalf("同租户 GetById 失败: %v", err)
	}
	if got.TenantID != 1 {
		t.Fatalf("期望 tenant_id=1，实际: %d", got.TenantID)
	}
}

// TestDataRule_GetByIds_Blocked 混合 ID 列表只读到同租户记录
func TestDataRule_GetByIds_Blocked(t *testing.T) {
	repo, db := setupTenantDB(t)
	tenant1ID, tenant2ID := insertTenantUsers(t, db)

	ctx := ctxWithTenantRule(1)
	results, err := repo.GetByIdsTx(ctx, []int64{tenant1ID, tenant2ID}, nil)
	if err != nil {
		t.Fatalf("GetByIdsTx 返回意外错误: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("期望只返回 1 条记录，实际: %d", len(results))
	}
	if results[0].TenantID != 1 {
		t.Fatalf("期望 tenant_id=1，实际: %d", results[0].TenantID)
	}
}

// TestDataRule_UpdateById_Blocked 跨租户 UpdateById，断言字段未变更
func TestDataRule_UpdateById_Blocked(t *testing.T) {
	repo, db := setupTenantDB(t)
	_, tenant2ID := insertTenantUsers(t, db)

	// tenant1 context，尝试更新 tenant2 的记录
	ctx := ctxWithTenantRule(1)
	entity := &tenantUser{ID: tenant2ID, Name: "Hacked", TenantID: 2}
	// tenantUser 无 version 字段，走普通 Updates 路径；DataRule 限制后 affected=0，返回 nil
	err := repo.UpdateByIdTx(ctx, entity, nil)
	if err != nil {
		t.Fatalf("UpdateByIdTx 返回意外错误: %v", err)
	}

	// 回查确认字段未变更
	var check tenantUser
	if err := db.Unscoped().First(&check, tenant2ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if check.Name != "Bob" {
		t.Fatalf("期望 Name=Bob（未被修改），实际: %s", check.Name)
	}
}

// TestDataRule_UpdateByIds_Blocked 混合 ID 列表，断言只有同租户被改
func TestDataRule_UpdateByIds_Blocked(t *testing.T) {
	repo, db := setupTenantDB(t)
	tenant1ID, tenant2ID := insertTenantUsers(t, db)

	ctx := ctxWithTenantRule(1)
	u, m := NewUpdater[tenantUser](ctx)
	u.Set(&m.Name, "Updated")

	affected, err := repo.UpdateByIdsTx(ctx, []int64{tenant1ID, tenant2ID}, u, nil)
	if err != nil {
		t.Fatalf("UpdateByIdsTx 返回意外错误: %v", err)
	}
	if affected != 1 {
		t.Fatalf("期望 affected=1（只更新同租户），实际: %d", affected)
	}

	// 回查 tenant2 未被修改
	var check tenantUser
	if err := db.First(&check, tenant2ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if check.Name != "Bob" {
		t.Fatalf("期望 tenant2 Name=Bob（未被修改），实际: %s", check.Name)
	}
}

// TestDataRule_DeleteById_Blocked 跨租户 DeleteById，断言记录仍存在
func TestDataRule_DeleteById_Blocked(t *testing.T) {
	repo, db := setupTenantDB(t)
	_, tenant2ID := insertTenantUsers(t, db)

	ctx := ctxWithTenantRule(1)
	affected, err := repo.DeleteByIdTx(ctx, tenant2ID, nil)
	if err != nil {
		t.Fatalf("DeleteByIdTx 返回意外错误: %v", err)
	}
	if affected != 0 {
		t.Fatalf("期望 affected=0（DataRule 拦截），实际: %d", affected)
	}

	// 回查确认记录仍存在（未被软删除）
	var check tenantUser
	if err := db.Unscoped().First(&check, tenant2ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if check.DeletedAt.Valid {
		t.Fatal("期望记录未被删除，实际 deleted_at 被设置")
	}
}

// TestDataRule_DeleteByIds_Blocked 混合 ID 列表，断言只删了同租户
func TestDataRule_DeleteByIds_Blocked(t *testing.T) {
	repo, db := setupTenantDB(t)
	tenant1ID, tenant2ID := insertTenantUsers(t, db)

	ctx := ctxWithTenantRule(1)
	affected, err := repo.DeleteByIdsTx(ctx, []int64{tenant1ID, tenant2ID}, nil)
	if err != nil {
		t.Fatalf("DeleteByIdsTx 返回意外错误: %v", err)
	}
	if affected != 1 {
		t.Fatalf("期望 affected=1（只删除同租户），实际: %d", affected)
	}

	// 回查 tenant2 记录仍存在
	var check tenantUser
	if err := db.Unscoped().First(&check, tenant2ID).Error; err != nil {
		t.Fatalf("回查 tenant2 失败: %v", err)
	}
	if check.DeletedAt.Valid {
		t.Fatal("期望 tenant2 记录未被删除，实际 deleted_at 被设置")
	}
}

// TestDataRule_Restore_Blocked 跨租户 Restore 软删记录，断言 deleted_at 未恢复
func TestDataRule_Restore_Blocked(t *testing.T) {
	repo, db := setupTenantDB(t)
	tenant1ID, tenant2ID := insertTenantUsers(t, db)

	// 先把两个记录都软删除
	if err := db.Delete(&tenantUser{}, tenant1ID).Error; err != nil {
		t.Fatalf("软删除 tenant1 失败: %v", err)
	}
	if err := db.Delete(&tenantUser{}, tenant2ID).Error; err != nil {
		t.Fatalf("软删除 tenant2 失败: %v", err)
	}

	// tenant1 context 尝试恢复 tenant2 的记录
	ctx := ctxWithTenantRule(1)
	affected, err := repo.RestoreTx(ctx, tenant2ID, nil)
	if err != nil {
		t.Fatalf("RestoreTx 返回意外错误: %v", err)
	}
	if affected != 0 {
		t.Fatalf("期望 affected=0（DataRule 拦截），实际: %d", affected)
	}

	// 回查确认 tenant2 的 deleted_at 仍然有值
	var check tenantUser
	if err := db.Unscoped().First(&check, tenant2ID).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if !check.DeletedAt.Valid {
		t.Fatal("期望 tenant2 deleted_at 仍有值（未被恢复），实际已被清空")
	}
}

// TestDataRule_ByID_InvalidColumnError 注入非法 DataRule column，7 个方法每个都应返回含 "data rule" 的错误
func TestDataRule_ByID_InvalidColumnError(t *testing.T) {
	repo, db := setupTenantDB(t)
	tenant1ID, tenant2ID := insertTenantUsers(t, db)

	invalidCtx := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Column: "id; DROP TABLE tenant_users", Condition: "=", Value: "1"},
	})

	assertDataRuleError := func(t *testing.T, err error, method string) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: 期望返回 error，实际为 nil", method)
		}
		if !strings.Contains(err.Error(), "data rule") {
			t.Fatalf("%s: 期望错误含 'data rule'，实际: %v", method, err)
		}
	}

	t.Run("GetByIdTx", func(t *testing.T) {
		_, err := repo.GetByIdTx(invalidCtx, tenant1ID, nil)
		assertDataRuleError(t, err, "GetByIdTx")
	})

	t.Run("GetByIdsTx", func(t *testing.T) {
		_, err := repo.GetByIdsTx(invalidCtx, []int64{tenant1ID}, nil)
		assertDataRuleError(t, err, "GetByIdsTx")
	})

	t.Run("UpdateByIdTx", func(t *testing.T) {
		entity := &tenantUser{ID: tenant1ID, Name: "X", TenantID: 1}
		err := repo.UpdateByIdTx(invalidCtx, entity, nil)
		assertDataRuleError(t, err, "UpdateByIdTx")
	})

	t.Run("UpdateByIdsTx", func(t *testing.T) {
		u, m := NewUpdater[tenantUser](invalidCtx)
		u.Set(&m.Name, "X")
		_, err := repo.UpdateByIdsTx(invalidCtx, []int64{tenant1ID}, u, nil)
		assertDataRuleError(t, err, "UpdateByIdsTx")
	})

	t.Run("DeleteByIdTx", func(t *testing.T) {
		_, err := repo.DeleteByIdTx(invalidCtx, tenant1ID, nil)
		assertDataRuleError(t, err, "DeleteByIdTx")
	})

	t.Run("DeleteByIdsTx", func(t *testing.T) {
		_, err := repo.DeleteByIdsTx(invalidCtx, []int64{tenant2ID}, nil)
		assertDataRuleError(t, err, "DeleteByIdsTx")
	})

	t.Run("RestoreTx", func(t *testing.T) {
		_, err := repo.RestoreTx(invalidCtx, tenant1ID, nil)
		assertDataRuleError(t, err, "RestoreTx")
	})
}

// TestDataRule_ByID_NoRuleNoEffect ctx 无 DataRuleKey 时，7 个方法行为不变（无回归）
func TestDataRule_ByID_NoRuleNoEffect(t *testing.T) {
	repo, db := setupTenantDB(t)
	tenant1ID, tenant2ID := insertTenantUsers(t, db)
	ctx := context.Background()

	t.Run("GetByIdTx_能查到任意记录", func(t *testing.T) {
		got, err := repo.GetByIdTx(ctx, tenant2ID, nil)
		if err != nil {
			t.Fatalf("GetByIdTx 无 DataRule 应能查到任意记录，实际: %v", err)
		}
		if got.TenantID != 2 {
			t.Fatalf("期望 tenant_id=2，实际: %d", got.TenantID)
		}
	})

	t.Run("GetByIdsTx_返回所有记录", func(t *testing.T) {
		results, err := repo.GetByIdsTx(ctx, []int64{tenant1ID, tenant2ID}, nil)
		if err != nil {
			t.Fatalf("GetByIdsTx 无 DataRule 应返回所有记录，实际: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("期望返回 2 条记录，实际: %d", len(results))
		}
	})

	t.Run("UpdateByIdTx_能更新任意记录", func(t *testing.T) {
		entity := &tenantUser{ID: tenant2ID, Name: "UpdatedNoRule", TenantID: 2}
		if err := repo.UpdateByIdTx(ctx, entity, nil); err != nil {
			t.Fatalf("UpdateByIdTx 无 DataRule 应能更新，实际: %v", err)
		}
		var check tenantUser
		db.First(&check, tenant2ID)
		if check.Name != "UpdatedNoRule" {
			t.Fatalf("期望 Name=UpdatedNoRule，实际: %s", check.Name)
		}
	})

	t.Run("UpdateByIdsTx_能更新所有记录", func(t *testing.T) {
		u, m := NewUpdater[tenantUser](ctx)
		u.Set(&m.Name, "BatchUpdated")
		affected, err := repo.UpdateByIdsTx(ctx, []int64{tenant1ID, tenant2ID}, u, nil)
		if err != nil {
			t.Fatalf("UpdateByIdsTx 无 DataRule 失败: %v", err)
		}
		if affected != 2 {
			t.Fatalf("期望 affected=2，实际: %d", affected)
		}
	})

	t.Run("DeleteByIdTx_能删任意记录", func(t *testing.T) {
		// 先插一条额外的记录
		extra := &tenantUser{Name: "Extra", TenantID: 99}
		db.Create(extra)
		affected, err := repo.DeleteByIdTx(ctx, extra.ID, nil)
		if err != nil {
			t.Fatalf("DeleteByIdTx 无 DataRule 失败: %v", err)
		}
		if affected != 1 {
			t.Fatalf("期望 affected=1，实际: %d", affected)
		}
	})

	t.Run("DeleteByIdsTx_能批量删任意记录", func(t *testing.T) {
		// 先插两条额外记录
		e1 := &tenantUser{Name: "Extra1", TenantID: 99}
		e2 := &tenantUser{Name: "Extra2", TenantID: 99}
		db.Create(e1)
		db.Create(e2)
		affected, err := repo.DeleteByIdsTx(ctx, []int64{e1.ID, e2.ID}, nil)
		if err != nil {
			t.Fatalf("DeleteByIdsTx 无 DataRule 失败: %v", err)
		}
		if affected != 2 {
			t.Fatalf("期望 affected=2，实际: %d", affected)
		}
	})

	t.Run("RestoreTx_能恢复任意软删记录", func(t *testing.T) {
		// 先软删 tenant1
		db.Delete(&tenantUser{}, tenant1ID)
		affected, err := repo.RestoreTx(ctx, tenant1ID, nil)
		if err != nil {
			t.Fatalf("RestoreTx 无 DataRule 失败: %v", err)
		}
		if affected != 1 {
			t.Fatalf("期望 affected=1，实际: %d", affected)
		}
		var check tenantUser
		db.Unscoped().First(&check, tenant1ID)
		if check.DeletedAt.Valid {
			t.Fatal("期望 deleted_at 已清空（记录已恢复）")
		}
	})
}
