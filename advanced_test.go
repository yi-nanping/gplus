package gplus

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite" // 纯 Go SQLite
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// UserWithDelete 用于测试软删除
type UserWithDelete struct {
	ID        int64          `gorm:"primaryKey"`
	Name      string         `gorm:"size:64"`
	Age       int            `gorm:"index"`
	DeletedAt gorm.DeletedAt `gorm:"index"`             // 软删除标记
	Orders    []Order        `gorm:"foreignKey:UserID"` // 关联测试
}

// Order 用于测试 Join 和 Preload
type Order struct {
	ID     int64 `gorm:"primaryKey"`
	UserID int64 `gorm:"index"`
	Amount int
	Remark string
}

// setupAdvancedDB 初始化包含关联表的数据库
func setupAdvancedDB(t *testing.T) (*Repository[int64, UserWithDelete], *gorm.DB) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}

	if err := db.AutoMigrate(&UserWithDelete{}, &Order{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	return NewRepository[int64, UserWithDelete](db), db
}

func TestAdvanced_Features(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	// 准备基础数据
	users := []UserWithDelete{
		{Name: "UserA", Age: 20},
		{Name: "UserB", Age: 30},
		{Name: "UserC", Age: 20}, // 用于 Group 测试 (两个 20 岁)
	}
	db.Create(&users)

	// 准备关联数据
	db.Create(&Order{UserID: 1, Amount: 100, Remark: "Order1"}) // UserA
	db.Create(&Order{UserID: 1, Amount: 200, Remark: "Order2"}) // UserA
	db.Create(&Order{UserID: 2, Amount: 300, Remark: "Order3"}) // UserB

	// -------------------------------------------------------------------
	// 1. ScopeBuilder 测试: Preload, Group, Distinct
	// -------------------------------------------------------------------
	t.Run("ScopeBuilder_Preload", func(t *testing.T) {
		q, u := NewQuery[UserWithDelete](ctx)
		// 预加载 Orders，按 ID 升序固定返回顺序：UserA, UserB, UserC
		q.LikeRight(&u.Name, "User").Preload("Orders").Order(&u.ID, true)

		result, err := repo.List(q)
		assertError(t, err, false, "Preload query failed")

		if len(result) != 3 {
			t.Fatalf("Expected 3 users, got %d", len(result))
		}
		// UserA 有 2 个订单，UserB 有 1 个，UserC 有 0 个
		if result[0].Name != "UserA" || len(result[0].Orders) != 2 {
			t.Errorf("UserA: expected 2 orders, got %d", len(result[0].Orders))
		}
		if result[1].Name != "UserB" || len(result[1].Orders) != 1 {
			t.Errorf("UserB: expected 1 order, got %d", len(result[1].Orders))
		}
		if result[2].Name != "UserC" || len(result[2].Orders) != 0 {
			t.Errorf("UserC: expected 0 orders, got %d", len(result[2].Orders))
		}
	})

	t.Run("ScopeBuilder_Group_Distinct", func(t *testing.T) {
		// 测试 Distinct: 获取不重复的 Age
		// 注意: Repository.List 返回的是 []T，Select 指定字段后其他字段为零值
		q, u := NewQuery[UserWithDelete](ctx)
		q.Distinct(&u.Age) // DISTINCT age → age=20, age=30 两种年龄

		result, err := repo.List(q)
		assertError(t, err, false, "Distinct query failed")

		if len(result) != 2 {
			t.Errorf("Expected 2 distinct ages, got %d", len(result))
		}
	})

	// -------------------------------------------------------------------
	// 2. 事务测试: 验证回滚
	// -------------------------------------------------------------------
	t.Run("Transaction_Rollback", func(t *testing.T) {
		// 1. 开启事务
		tx := db.Begin()

		updater, u := NewUpdater[UserWithDelete](ctx)
		// 尝试将 UserA 的名字改为 "Changed"
		updater.Set(&u.Name, "Changed").Eq(&u.ID, 1)

		// 2. 在事务中执行更新
		affected, err := repo.UpdateByCondTx(updater, tx)
		assertError(t, err, false, "Update in tx should succeed initially")
		assertEqual(t, int64(1), affected, "Should affect 1 row")

		// 3. 模拟后续业务逻辑报错 -> 回滚事务
		tx.Rollback()

		// 4. 验证数据是否恢复
		var user UserWithDelete
		db.First(&user, 1)
		if user.Name == "Changed" {
			t.Error("Transaction failed to rollback, name is changed")
		}
		if user.Name != "UserA" {
			t.Errorf("Expected name 'UserA', got '%s'", user.Name)
		}
	})

	// -------------------------------------------------------------------
	// 3. DataRule (数据权限) 测试
	// -------------------------------------------------------------------
	t.Run("DataRule_Injection", func(t *testing.T) {
		// 模拟从中间件注入数据权限规则
		// 规则：只能查看 Age > 25 的用户 (即 UserB)
		rule := DataRule{
			Column:    "age",
			Condition: ">",
			Value:     "25",
		}

		// 注入 Context
		// 注意：这里的 Key 必须与源码中定义的一致。
		// 假设源码 consts.go 或 query.go 中导出了 DataRuleKey
		ctxWithRule := context.WithValue(ctx, DataRuleKey, []DataRule{rule})

		q, _ := NewQuery[UserWithDelete](ctxWithRule)
		// 不需要手动加条件，DataRuleBuilder 会自动处理

		list, err := repo.List(q)
		assertError(t, err, false, "DataRule query failed")

		// 预期只查出 UserB (Age 30)，过滤掉 UserA/C (Age 20)
		if len(list) != 1 {
			t.Errorf("Expected 1 user due to data rule, got %d", len(list))
		}
		if len(list) > 0 && list[0].Name != "UserB" {
			t.Errorf("Expected UserB, got %s", list[0].Name)
		}
	})

	// -------------------------------------------------------------------
	// 4. 软删除测试
	// -------------------------------------------------------------------
	t.Run("SoftDelete_And_Unscoped", func(t *testing.T) {
		// 删除 UserA
		qDel, u := NewQuery[UserWithDelete](ctx)
		qDel.Eq(&u.ID, 1)
		count, err := repo.DeleteByCond(qDel)
		if err != nil {
			t.Errorf("DeleteByCond failed: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1 deleted row, got %d", count)
		}

		// 1. 正常查询 (应该查不到)
		qNormal, _ := NewQuery[UserWithDelete](ctx)
		qNormal.Eq(&u.ID, 1)
		list1, _ := repo.List(qNormal)
		if len(list1) != 0 {
			t.Error("Soft deleted record should not be found in normal query")
		}

		// 2. Unscoped 查询 (应该能查到)
		qUnscoped, _ := NewQuery[UserWithDelete](ctx)
		qUnscoped.Eq(&u.ID, 1).Unscoped() // 关键调用
		list2, _ := repo.List(qUnscoped)

		if len(list2) != 1 {
			t.Error("Soft deleted record SHOULD be found in unscoped query")
		}
		if len(list2) > 0 && list2[0].DeletedAt.Valid == false {
			t.Error("Loaded record should have valid DeletedAt")
		}
	})
}
