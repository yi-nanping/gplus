package gplus

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"gorm.io/gorm"
)

// UserWithVersion 用于乐观锁测试的基础模型（int64 version 字段）
type UserWithVersion struct {
	ID      int64  `gorm:"primaryKey;autoIncrement"`
	Name    string `gorm:"column:name"`
	Version int64  `gorm:"column:version" gplus:"version"`
}

// UserWithVersionUint32 测试 uint32 类型的 version 字段
type UserWithVersionUint32 struct {
	ID      int64  `gorm:"primaryKey;autoIncrement"`
	Name    string `gorm:"column:name"`
	Version uint32 `gorm:"column:version" gplus:"version"`
}

// EmbedVersionBase 包含 version 字段的嵌入结构体
type EmbedVersionBase struct {
	Version int64 `gorm:"column:version" gplus:"version"`
}

// UserWithEmbedVersion 通过嵌入字段携带 version
type UserWithEmbedVersion struct {
	ID   int64  `gorm:"primaryKey;autoIncrement"`
	Name string `gorm:"column:name"`
	EmbedVersionBase
}

// setupVersionDB 初始化乐观锁测试用 DB，使用独立 AutoMigrate 避免污染 columnNameCache
func setupVersionDB[T any](t *testing.T) (*Repository[int64, T], *gorm.DB) {
	t.Helper()
	return setupTestDB[T](t)
}

// TestOptimisticLock_NoVersionField 无 version 字段时走原有路径（回归测试）
func TestOptimisticLock_NoVersionField(t *testing.T) {
	unregisterModel[TestUser]()
	defer unregisterModel[TestUser]()
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	user := &TestUser{Name: "Alice", Age: 20, Email: "alice@test.com"}
	if err := repo.Save(ctx, user); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	user.Name = "Alice Updated"
	if err := repo.UpdateById(ctx, user); err != nil {
		t.Errorf("无 version 字段的 UpdateById 不应报错: %v", err)
	}
}

// TestOptimisticLock_BasicUpdate 正常更新：version 自动递增
func TestOptimisticLock_BasicUpdate(t *testing.T) {
	unregisterModel[UserWithVersion]()
	defer unregisterModel[UserWithVersion]()
	versionFieldCache.Delete(reflect.TypeOf((*UserWithVersion)(nil)).Elem().String())

	repo, db := setupTestDB[UserWithVersion](t)
	ctx := context.Background()

	db.Create(&UserWithVersion{Name: "Bob", Version: 0})
	user := &UserWithVersion{}
	db.First(user)

	if user.Version != 0 {
		t.Fatalf("初始 version 应为 0，实际 %d", user.Version)
	}

	user.Name = "Bob Updated"
	if err := repo.UpdateById(ctx, user); err != nil {
		t.Fatalf("UpdateById 失败: %v", err)
	}
	if user.Version != 1 {
		t.Errorf("更新后 entity.Version 应自动递增为 1，实际 %d", user.Version)
	}

	// 验证 DB 中确实更新
	var dbUser UserWithVersion
	db.First(&dbUser, user.ID)
	if dbUser.Name != "Bob Updated" {
		t.Errorf("DB 中 Name 应为 'Bob Updated'，实际 %q", dbUser.Name)
	}
	if dbUser.Version != 1 {
		t.Errorf("DB 中 version 应为 1，实际 %d", dbUser.Version)
	}
}

// TestOptimisticLock_ConflictOnUpdate 版本冲突时返回 ErrOptimisticLock
func TestOptimisticLock_ConflictOnUpdate(t *testing.T) {
	unregisterModel[UserWithVersion]()
	defer unregisterModel[UserWithVersion]()
	versionFieldCache.Delete(reflect.TypeOf((*UserWithVersion)(nil)).Elem().String())

	repo, db := setupTestDB[UserWithVersion](t)
	ctx := context.Background()

	db.Create(&UserWithVersion{Name: "Carol", Version: 0})

	// A、B 同时读到 version=0
	userA := &UserWithVersion{}
	userB := &UserWithVersion{}
	db.First(userA)
	db.First(userB)

	// B 先提交成功，version 变为 1
	userB.Name = "Carol by B"
	if err := repo.UpdateById(ctx, userB); err != nil {
		t.Fatalf("B 更新失败: %v", err)
	}

	// A 再提交，version=0 与 DB 中的 1 不匹配，应返回 ErrOptimisticLock
	userA.Name = "Carol by A"
	err := repo.UpdateById(ctx, userA)
	if !errors.Is(err, ErrOptimisticLock) {
		t.Errorf("期望 ErrOptimisticLock，实际: %v", err)
	}
}

// TestOptimisticLock_RowNotFound 记录不存在时返回 ErrOptimisticLock
func TestOptimisticLock_RowNotFound(t *testing.T) {
	unregisterModel[UserWithVersion]()
	defer unregisterModel[UserWithVersion]()
	versionFieldCache.Delete(reflect.TypeOf((*UserWithVersion)(nil)).Elem().String())

	repo, _ := setupTestDB[UserWithVersion](t)
	ctx := context.Background()

	ghost := &UserWithVersion{ID: 9999, Name: "Ghost", Version: 0}
	err := repo.UpdateById(ctx, ghost)
	if !errors.Is(err, ErrOptimisticLock) {
		t.Errorf("记录不存在时期望 ErrOptimisticLock，实际: %v", err)
	}
}

// TestOptimisticLock_SuccessiveUpdates 连续更新同一 entity，version 持续递增
func TestOptimisticLock_SuccessiveUpdates(t *testing.T) {
	unregisterModel[UserWithVersion]()
	defer unregisterModel[UserWithVersion]()
	versionFieldCache.Delete(reflect.TypeOf((*UserWithVersion)(nil)).Elem().String())

	repo, db := setupTestDB[UserWithVersion](t)
	ctx := context.Background()

	db.Create(&UserWithVersion{Name: "Dave", Version: 0})
	user := &UserWithVersion{}
	db.First(user)

	for i := int64(1); i <= 3; i++ {
		user.Name = "Dave v" + string(rune('0'+i))
		if err := repo.UpdateById(ctx, user); err != nil {
			t.Fatalf("第 %d 次更新失败: %v", i, err)
		}
		if user.Version != i {
			t.Errorf("第 %d 次更新后 entity.Version 应为 %d，实际 %d", i, i, user.Version)
		}
	}
}

// TestOptimisticLock_Uint32Version 测试 uint32 类型的 version 字段
func TestOptimisticLock_Uint32Version(t *testing.T) {
	unregisterModel[UserWithVersionUint32]()
	defer unregisterModel[UserWithVersionUint32]()
	versionFieldCache.Delete(reflect.TypeOf((*UserWithVersionUint32)(nil)).Elem().String())

	repo, db := setupTestDB[UserWithVersionUint32](t)
	ctx := context.Background()

	db.Create(&UserWithVersionUint32{Name: "Eve", Version: 0})
	user := &UserWithVersionUint32{}
	db.First(user)

	user.Name = "Eve Updated"
	if err := repo.UpdateById(ctx, user); err != nil {
		t.Fatalf("uint32 version UpdateById 失败: %v", err)
	}
	if user.Version != 1 {
		t.Errorf("uint32 version 应递增为 1，实际 %d", user.Version)
	}
}

// TestOptimisticLock_EmbedVersion 测试嵌入字段中的 version
func TestOptimisticLock_EmbedVersion(t *testing.T) {
	unregisterModel[UserWithEmbedVersion]()
	defer unregisterModel[UserWithEmbedVersion]()
	versionFieldCache.Delete(reflect.TypeOf((*UserWithEmbedVersion)(nil)).Elem().String())

	repo, db := setupTestDB[UserWithEmbedVersion](t)
	ctx := context.Background()

	db.Create(&UserWithEmbedVersion{Name: "Frank", EmbedVersionBase: EmbedVersionBase{Version: 0}})
	user := &UserWithEmbedVersion{}
	db.First(user)

	user.Name = "Frank Updated"
	if err := repo.UpdateById(ctx, user); err != nil {
		t.Fatalf("嵌入 version UpdateById 失败: %v", err)
	}
	if user.EmbedVersionBase.Version != 1 {
		t.Errorf("嵌入 version 应递增为 1，实际 %d", user.EmbedVersionBase.Version)
	}
}

// TestOptimisticLock_TxVariant 测试 UpdateByIdTx 事务版本
func TestOptimisticLock_TxVariant(t *testing.T) {
	unregisterModel[UserWithVersion]()
	defer unregisterModel[UserWithVersion]()
	versionFieldCache.Delete(reflect.TypeOf((*UserWithVersion)(nil)).Elem().String())

	repo, db := setupTestDB[UserWithVersion](t)
	ctx := context.Background()

	db.Create(&UserWithVersion{Name: "Grace", Version: 0})
	user := &UserWithVersion{}
	db.First(user)

	var updateErr error
	err := db.Transaction(func(tx *gorm.DB) error {
		user.Name = "Grace Tx"
		updateErr = repo.UpdateByIdTx(ctx, user, tx)
		return updateErr
	})
	if err != nil {
		t.Fatalf("事务失败: %v", err)
	}
	if updateErr != nil {
		t.Fatalf("UpdateByIdTx 不应报错: %v", updateErr)
	}
	if user.Version != 1 {
		t.Errorf("Tx 更新后 entity.Version 应为 1，实际 %d", user.Version)
	}
}
