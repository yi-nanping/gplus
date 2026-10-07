package gplus_test

import (
	"context"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
	"strings"
	"testing"
)

func TestDeleteByCondTx_UnscopedEmptyReturnsError(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	t.Run("Unscoped + 空条件返回 ErrDeleteEmpty", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Unscoped()
		_, err := repo.DeleteByCondTx(q, nil)
		if err != ErrDeleteEmpty {
			t.Errorf("期望 ErrDeleteEmpty，实际: %v", err)
		}
	})

	t.Run("Unscoped + 有条件正常执行", func(t *testing.T) {
		repo2, db := setupTestDB[TestUser](t)
		db.Create(&TestUser{Name: "PhysDelete", Age: 99})
		q, m := NewQuery[TestUser](ctx)
		q.Eq(&m.Name, "PhysDelete").Unscoped()
		affected, err := repo2.DeleteByCondTx(q, nil)
		if err != nil {
			t.Errorf("Unscoped + 有条件不应报错: %v", err)
		}
		if affected != 1 {
			t.Errorf("期望删除 1 条，实际 %d", affected)
		}
	})
}

// TestDataRule_UpdateByCond_Applied 验证 DataRule 条件正确追加到 UPDATE WHERE 子句
func TestDataRule_UpdateByCond_Applied(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	// 插入两条数据
	_ = repo.SaveBatch(ctx, []TestUser{{Name: "Alice", Age: 20}, {Name: "Bob", Age: 30}})

	// 注入 DataRule：只允许操作 age >= 25 的记录
	rules := []DataRule{{Column: "age", Condition: ">=", Value: "25"}}
	ctxWithRule := context.WithValue(ctx, DataRuleKey, rules)

	u, model := NewUpdater[TestUser](ctxWithRule)
	u.Set(&model.Name, "Updated").
		Ge(&model.Age, 1) // 宽泛条件，DataRule 会追加 age >= 25

	affected, err := repo.UpdateByCond(u)
	if err != nil {
		t.Fatalf("UpdateByCond 不应报错: %v", err)
	}
	// DataRule age >= 25 只命中 Bob(30)，Alice(20) 不受影响
	if affected != 1 {
		t.Errorf("期望影响 1 行，实际 %d", affected)
	}

	// 验证 Alice 未被更新
	q, qModel := NewQuery[TestUser](ctx)
	q.Eq(&qModel.Name, "Alice")
	list, _ := repo.List(q)
	if len(list) != 1 {
		t.Errorf("Alice 应仍存在，实际找到 %d 条", len(list))
	}
}

func TestRepository_Upsert(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	t.Run("Upsert 无主键执行 INSERT", func(t *testing.T) {
		u := &TestUser{Name: "UpsertNew", Age: 10}
		if err := repo.Upsert(ctx, u); err != nil {
			t.Fatalf("Upsert 不应报错: %v", err)
		}
		if u.ID == 0 {
			t.Error("Upsert 后应分配主键")
		}
	})

	t.Run("Upsert 有主键执行 UPDATE", func(t *testing.T) {
		// 先插入
		u := &TestUser{Name: "Before", Age: 1}
		_ = repo.Save(ctx, u)
		// 再 upsert 更新
		u.Name = "After"
		if err := repo.Upsert(ctx, u); err != nil {
			t.Fatalf("Upsert 更新不应报错: %v", err)
		}
		got, err := repo.GetById(ctx, u.ID)
		if err != nil {
			t.Fatalf("GetById 失败: %v", err)
		}
		if got.Name != "After" {
			t.Errorf("期望 Name=After，实际 %s", got.Name)
		}
	})

	t.Run("UpsertTx 事务", func(t *testing.T) {
		repo2, db := setupTestDB[TestUser](t)
		u := &TestUser{Name: "UpsertTx", Age: 5}
		if err := repo2.UpsertTx(ctx, u, db); err != nil {
			t.Fatalf("UpsertTx 不应报错: %v", err)
		}
	})

	t.Run("UpsertBatch 批量", func(t *testing.T) {
		repo3, _ := setupTestDB[TestUser](t)
		users := []TestUser{{Name: "UB1", Age: 1}, {Name: "UB2", Age: 2}}
		if err := repo3.UpsertBatch(ctx, users); err != nil {
			t.Fatalf("UpsertBatch 不应报错: %v", err)
		}
		q, _ := NewQuery[TestUser](ctx)
		list, _ := repo3.List(q)
		if len(list) != 2 {
			t.Errorf("期望 2 条，实际 %d", len(list))
		}
	})

	t.Run("UpsertBatchTx 事务批量", func(t *testing.T) {
		repo4, db := setupTestDB[TestUser](t)
		users := []TestUser{{Name: "UBTx", Age: 99}}
		if err := repo4.UpsertBatchTx(ctx, users, db); err != nil {
			t.Fatalf("UpsertBatchTx 不应报错: %v", err)
		}
	})
}

func TestRepository_SaveBatch(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	t.Run("SaveBatch 正常插入", func(t *testing.T) {
		users := []TestUser{{Name: "Batch1", Age: 10}, {Name: "Batch2", Age: 20}}
		if err := repo.SaveBatch(ctx, users); err != nil {
			t.Fatalf("SaveBatch 不应报错: %v", err)
		}
		q, _ := NewQuery[TestUser](ctx)
		list, _ := repo.List(q)
		if len(list) != 2 {
			t.Errorf("期望 2 条，实际 %d", len(list))
		}
	})

	t.Run("SaveBatchTx 事务插入", func(t *testing.T) {
		repo2, db := setupTestDB[TestUser](t)
		more := []TestUser{{Name: "TxBatch", Age: 99}}
		if err := repo2.SaveBatchTx(ctx, more, db); err != nil {
			t.Fatalf("SaveBatchTx 不应报错: %v", err)
		}
	})
}

func TestRepository_CreateBatch(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	t.Run("CreateBatch 分批插入", func(t *testing.T) {
		users := []*TestUser{{Name: "CB1", Age: 11}, {Name: "CB2", Age: 22}, {Name: "CB3", Age: 33}}
		if err := repo.CreateBatch(ctx, users, 2); err != nil {
			t.Fatalf("CreateBatch 不应报错: %v", err)
		}
		q, _ := NewQuery[TestUser](ctx)
		list, _ := repo.List(q)
		if len(list) != 3 {
			t.Errorf("期望 3 条，实际 %d", len(list))
		}
	})

	t.Run("CreateBatchTx 事务分批插入", func(t *testing.T) {
		repo2, db := setupTestDB[TestUser](t)
		more := []*TestUser{{Name: "CBTx", Age: 55}}
		if err := repo2.CreateBatchTx(ctx, more, 1, db); err != nil {
			t.Fatalf("CreateBatchTx 不应报错: %v", err)
		}
	})

	t.Run("CreateBatch batchSize<=0 应返回错误", func(t *testing.T) {
		users := []*TestUser{{Name: "X", Age: 1}}
		if err := repo.CreateBatch(ctx, users, 0); err == nil {
			t.Error("batchSize=0 应返回错误")
		}
		if err := repo.CreateBatch(ctx, users, -1); err == nil {
			t.Error("batchSize=-1 应返回错误")
		}
		if err := repo.CreateBatchTx(ctx, users, 0, nil); err == nil {
			t.Error("CreateBatchTx batchSize=0 应返回错误")
		}
	})
}

func TestRepository_GetByLock(t *testing.T) {
	ctx := context.Background()

	t.Run("tx 为 nil 返回 ErrTransactionReq", func(t *testing.T) {
		repo, _ := setupTestDB[TestUser](t)
		q, _ := NewQuery[TestUser](ctx)
		_, err := repo.GetByLock(q, nil)
		if err != ErrTransactionReq {
			t.Errorf("期望 ErrTransactionReq，实际: %v", err)
		}
	})

	t.Run("q 为 nil 返回 ErrQueryNil", func(t *testing.T) {
		repo, db := setupTestDB[TestUser](t)
		_, err := repo.GetByLock(nil, db)
		if err != ErrQueryNil {
			t.Errorf("期望 ErrQueryNil，实际: %v", err)
		}
	})

	t.Run("q 有错误返回 builder 错误", func(t *testing.T) {
		repo, db := setupTestDB[TestUser](t)
		q, _ := NewQuery[TestUser](ctx)
		q.Eq(nil, "invalid") // 公开接口触发 builder 错误
		_, err := repo.GetByLock(q, db)
		if err == nil {
			t.Error("builder 有错误时应返回错误")
		}
	})

	t.Run("正常带锁查询（自动补 LockWrite）", func(t *testing.T) {
		repo, db := setupTestDB[TestUser](t)
		db.Create(&TestUser{Name: "LockUser", Age: 30})
		var found *TestUser
		var lockErr error
		_ = db.Transaction(func(tx *gorm.DB) error {
			q, m := NewQuery[TestUser](ctx)
			q.Eq(&m.Name, "LockUser")
			found, lockErr = repo.GetByLock(q, tx)
			return lockErr
		})
		if lockErr != nil {
			t.Fatalf("GetByLock 不应报错: %v", lockErr)
		}
		if found == nil || found.Name != "LockUser" {
			t.Error("GetByLock 应返回正确记录")
		}
	})
}

func TestApplyWhere_IsRaw(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	db.Create(&TestUser{Name: "RawUser", Age: 25})
	ctx := context.Background()

	t.Run("isRaw 无参数条件", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		// 直接写入 isRaw 条件（无 value）
		q.WhereRaw("age > 18")
		list, err := repo.List(q)
		assertError(t, err, false, "isRaw 条件不应报错")
		if len(list) != 1 {
			t.Errorf("期望 1 条，实际 %d", len(list))
		}
	})

	t.Run("isRaw 有参数条件", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		// 直接写入 isRaw 条件（有 value）
		q.WhereRaw("age > ?", 18)
		list, err := repo.List(q)
		assertError(t, err, false, "isRaw 有参数条件不应报错")
		if len(list) != 1 {
			t.Errorf("期望 1 条，实际 %d", len(list))
		}
	})
}

// TestWhereRaw_Query 验证 Query.WhereRaw / OrWhereRaw
func TestWhereRaw_Query(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 20})
	db.Create(&TestUser{Name: "Bob", Age: 30})

	t.Run("WhereRaw 无参数", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.WhereRaw("age > 25")
		list, err := repo.List(q)
		assertError(t, err, false, "WhereRaw 无参不应报错")
		if len(list) != 1 || list[0].Name != "Bob" {
			t.Errorf("期望 Bob，实际 %v", list)
		}
	})

	t.Run("WhereRaw 单参数", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.WhereRaw("age > ?", 25)
		list, err := repo.List(q)
		assertError(t, err, false, "WhereRaw 单参不应报错")
		if len(list) != 1 || list[0].Name != "Bob" {
			t.Errorf("期望 Bob，实际 %v", list)
		}
	})

	t.Run("WhereRaw 多参数", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.WhereRaw("age > ? AND age < ?", 18, 25)
		list, err := repo.List(q)
		assertError(t, err, false, "WhereRaw 多参不应报错")
		if len(list) != 1 || list[0].Name != "Alice" {
			t.Errorf("期望 Alice，实际 %v", list)
		}
	})

	t.Run("OrWhereRaw", func(t *testing.T) {
		q, model := NewQuery[TestUser](ctx)
		q.Eq(&model.Name, "Alice").OrWhereRaw("age = ?", 30)
		list, err := repo.List(q)
		assertError(t, err, false, "OrWhereRaw 不应报错")
		if len(list) != 2 {
			t.Errorf("期望 2 条，实际 %d", len(list))
		}
	})

	t.Run("WhereRaw 空 sql 写入 errs", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.WhereRaw("")
		if q.GetError() == nil {
			t.Error("空 sql 应写入 errs")
		}
	})

	t.Run("OrWhereRaw 空 sql 写入 errs", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.OrWhereRaw("")
		if q.GetError() == nil {
			t.Error("空 sql 应写入 errs")
		}
	})

	t.Run("OrWhereRaw 多参数", func(t *testing.T) {
		q, model := NewQuery[TestUser](ctx)
		q.Eq(&model.Name, "Alice").OrWhereRaw("age > ? AND age < ?", 25, 35)
		list, err := repo.List(q)
		assertError(t, err, false, "OrWhereRaw 多参不应报错")
		if len(list) != 2 {
			t.Errorf("期望 2 条，实际 %d", len(list))
		}
	})
}

// TestWhereRaw_Updater 验证 Updater.WhereRaw / OrWhereRaw
func TestWhereRaw_Updater(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 20})
	db.Create(&TestUser{Name: "Bob", Age: 30})

	t.Run("WhereRaw 单参数更新", func(t *testing.T) {
		u, model := NewUpdater[TestUser](ctx)
		u.Set(&model.Name, "AliceNew").WhereRaw("age = ?", 20)
		affected, err := repo.UpdateByCond(u)
		assertError(t, err, false, "WhereRaw 更新不应报错")
		if affected != 1 {
			t.Errorf("期望影响 1 行，实际 %d", affected)
		}
	})

	t.Run("WhereRaw 多参数更新", func(t *testing.T) {
		u, model := NewUpdater[TestUser](ctx)
		u.Set(&model.Name, "BobNew").WhereRaw("age > ? AND age < ?", 25, 35)
		affected, err := repo.UpdateByCond(u)
		assertError(t, err, false, "WhereRaw 多参更新不应报错")
		if affected != 1 {
			t.Errorf("期望影响 1 行，实际 %d", affected)
		}
	})

	t.Run("Updater WhereRaw 空 sql 写入 errs", func(t *testing.T) {
		u, model := NewUpdater[TestUser](ctx)
		u.Set(&model.Name, "x").WhereRaw("")
		if u.GetError() == nil {
			t.Error("空 sql 应写入 errs")
		}
	})

	t.Run("Updater OrWhereRaw 空 sql 写入 errs", func(t *testing.T) {
		u, model := NewUpdater[TestUser](ctx)
		u.Set(&model.Name, "x").OrWhereRaw("")
		if u.GetError() == nil {
			t.Error("空 sql 应写入 errs")
		}
	})

	t.Run("Updater OrWhereRaw 多参数", func(t *testing.T) {
		u, model := NewUpdater[TestUser](ctx)
		u.Set(&model.Name, "OrRawNew").WhereRaw("age = ?", 999).OrWhereRaw("age > ? AND age < ?", 25, 35)
		affected, err := repo.UpdateByCond(u)
		assertError(t, err, false, "OrWhereRaw 多参更新不应报错")
		if affected != 1 {
			t.Errorf("期望影响 1 行，实际 %d", affected)
		}
	})
}

func TestRepository_OrderRaw(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	// 插入三条记录，age 分别为 30, 18, 25
	db.Create(&TestUser{Name: "C", Age: 30})
	db.Create(&TestUser{Name: "A", Age: 18})
	db.Create(&TestUser{Name: "B", Age: 25})

	t.Run("OrderRaw 按指定顺序排序", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		// SQLite 支持 CASE WHEN 排序
		q.OrderRaw("CASE age WHEN 18 THEN 0 WHEN 25 THEN 1 ELSE 2 END")
		list, err := repo.List(q)
		if err != nil {
			t.Fatalf("OrderRaw 不应报错: %v", err)
		}
		if len(list) != 3 {
			t.Fatalf("期望 3 条，实际 %d", len(list))
		}
		// 第一条应为 age=18
		if list[0].Age != 18 {
			t.Errorf("期望第一条 age=18，实际 %d", list[0].Age)
		}
	})

	t.Run("OrderRaw 与 Order 共存按预期生效", func(t *testing.T) {
		q, m := NewQuery[TestUser](ctx)
		q.OrderRaw("CASE age WHEN 18 THEN 0 ELSE 1 END").Order(&m.Age, true)
		list, err := repo.List(q)
		if err != nil {
			t.Fatalf("OrderRaw+Order 不应报错: %v", err)
		}
		if len(list) != 3 {
			t.Fatalf("期望 3 条，实际 %d", len(list))
		}
	})
}

// TestApplyGroupHaving_ComplexPaths 覆盖 applyGroupHaving 的 OrHaving、HavingGroup、OR嵌套组执行路径
func TestApplyGroupHaving_ComplexPaths(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "alice", Age: 25})
	db.Create(&TestUser{Name: "alice", Age: 30})
	db.Create(&TestUser{Name: "bob", Age: 20})

	t.Run("OrHaving 叶子 OR 正确追加到 HAVING", func(t *testing.T) {
		// HAVING (username = 'nobody' OR username = 'alice') → 只有 alice 组匹配
		q, u := NewQuery[TestUser](ctx)
		q.Select(&u.Name).Group("username").
			Having("username", OpEq, "nobody").
			OrHaving("username", OpEq, "alice")
		list, err := repo.List(q)
		if err != nil {
			t.Fatalf("OrHaving 不应报错: %v", err)
		}
		if len(list) != 1 {
			t.Errorf("HAVING nobody OR alice 期望 1 组，实际 %d", len(list))
		}
	})

	t.Run("HavingGroup 嵌套 AND 正确追加到 HAVING", func(t *testing.T) {
		// HAVING (username = 'alice') → 只有 alice 组匹配
		q, u := NewQuery[TestUser](ctx)
		q.Select(&u.Name).Group("username").
			HavingGroup(func(sub *Query[TestUser]) {
				sub.Having("username", OpEq, "alice")
			})
		list, err := repo.List(q)
		if err != nil {
			t.Fatalf("HavingGroup 不应报错: %v", err)
		}
		if len(list) != 1 {
			t.Errorf("HAVING (username = alice) 期望 1 组，实际 %d", len(list))
		}
	})

	t.Run("OrHaving 公开路径正确合并", func(t *testing.T) {
		// 先 Having(bob)，再通过 OrHaving 添加 alice 条件
		// → HAVING (username = 'bob' OR username = 'alice') → bob 和 alice 两组
		q, u := NewQuery[TestUser](ctx)
		q.Select(&u.Name).Group("username").
			Having("username", OpEq, "bob")
		q.OrHaving("username", OpEq, "alice")
		list, err := repo.List(q)
		if err != nil {
			t.Fatalf("Having OR 嵌套组不应报错: %v", err)
		}
		if len(list) != 2 {
			t.Errorf("HAVING bob OR (alice) 期望 2 组，实际 %d", len(list))
		}
	})
}

// TestApplyWhere_ComplexPaths 覆盖 applyWhere 的子查询、OR嵌套组、empty expr 路径
func TestApplyWhere_ComplexPaths(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "alice", Age: 25})
	db.Create(&TestUser{Name: "bob", Age: 30})

	t.Run("OR 嵌套组执行路径", func(t *testing.T) {
		// 覆盖 applyWhere line 295: d = d.Or(subDb)
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "nobody").Or(func(sub *Query[TestUser]) {
			sub.Eq(&u.Name, "alice")
		})
		list, err := repo.List(q)
		if err != nil {
			t.Fatalf("OR 嵌套组不应报错: %v", err)
		}
		if len(list) != 1 {
			t.Errorf("期望 1 条，实际 %d", len(list))
		}
	})

	t.Run("子查询 AND 路径", func(t *testing.T) {
		// 覆盖 applyWhere line 317: d = d.Where(sqlStr, subQuery)
		subQ, su := NewQuery[TestUser](ctx)
		subQ.Eq(&su.Name, "alice")
		subQ.Select(&su.Age)
		subQ.Table("test_users")
		q, u := NewQuery[TestUser](ctx)
		q.In(&u.Age, subQ.ToDB(db))
		list, err := repo.List(q)
		if err != nil {
			t.Fatalf("子查询 AND 不应报错: %v", err)
		}
		if len(list) != 1 {
			t.Errorf("期望 1 条，实际 %d", len(list))
		}
	})

	t.Run("子查询 OR 路径", func(t *testing.T) {
		// 覆盖 applyWhere line 315: d = d.Or(sqlStr, subQuery)
		subQ, su := NewQuery[TestUser](ctx)
		subQ.Eq(&su.Name, "alice")
		subQ.Select(&su.Age)
		subQ.Table("test_users")
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Age, 999).OrIn(&u.Age, subQ.ToDB(db))
		list, err := repo.List(q)
		if err != nil {
			t.Fatalf("子查询 OR 不应报错: %v", err)
		}
		if len(list) != 1 {
			t.Errorf("期望 1 条，实际 %d", len(list))
		}
	})

}

// TestApplyJoins_CrossJoin 验证无 ON 条件的 CrossJoin 走 applyJoins 执行路径
func TestApplyJoins_CrossJoin(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 25})

	q, _ := NewQuery[TestUser](ctx)
	q.CrossJoin("test_users AS t2")
	// 验证 CrossJoin（无 ON 条件）能正确构建并执行 SQL，覆盖 applyJoins 无条件分支
	list, err := repo.List(q)
	if err != nil {
		t.Fatalf("CrossJoin 不应报错: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("CROSS JOIN 1×1 期望 1 条，实际 %d", len(list))
	}
}

// TestApplyJoins_WithArgs 验证 LeftJoin 带绑定参数走 applyJoins 有参数分支
func TestApplyJoins_WithArgs(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Bob", Age: 20})

	q, _ := NewQuery[TestUser](ctx)
	// 带参数的 JOIN：覆盖 len(j.args) > 0 路径
	q.LeftJoin("test_users t2", "t2.id = test_users.id AND t2.age > ?", 0)
	q.WhereRaw("test_users.username = ?", "Bob")
	list, err := repo.List(q)
	if err != nil {
		t.Fatalf("LeftJoin 带参数不应报错: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("期望 1 条，实际 %d", len(list))
	}
}

// TestHavingGroup_EmptyFn 验证 HavingGroup 空函数体走 buildHavingExprs 空嵌套路径
func TestHavingGroup_EmptyFn(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Charlie", Age: 30})

	q, u := NewQuery[TestUser](ctx)
	q.Select(&u.Age).Group(&u.Age)
	// 空函数体：subExprs 长度为 0，触发 continue 分支
	q.HavingGroup(func(sub *Query[TestUser]) {})
	list, err := repo.List(q)
	if err != nil {
		t.Fatalf("空 HavingGroup 不应报错: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("期望 1 条，实际 %d", len(list))
	}
}

// M-1: Updater.OrNotExists OR 分支。共享的 appendExists 已被其他 Exists 测试覆盖，
// 本测试补 OrNotExists wrapper（"NOT EXISTS", true）的直接调用覆盖，与 Query 侧 TestOrNotExists_OrBranchSQL 对称。
func TestUpdater_OrNotExists_OrBranchSQL(t *testing.T) {
	_, db := setupAdvancedDB(t)
	u, m := NewUpdater[UserWithDelete](context.Background())
	u.Set(&m.Name, "x").Eq(&m.Name, "alice")
	sub, o := SubQuery[Order](u)
	sub.Eq(&o.UserID, m.ID)
	u.OrNotExists(sub)
	sql, err := u.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	if !strings.Contains(sql, "OR NOT EXISTS") {
		t.Errorf("期望 SQL 含 'OR NOT EXISTS'，实际: %s", sql)
	}
}

// M-3: FindAsTx / FindOneAsTx / PageAsTx 透传 DataRuleBuilder 错误。
// 原测试均走无 tx 包装函数，Tx 变体的 DataRuleBuilder().GetError() 分支未被直接覆盖。
func TestFindAsTx_Variants_PropagateDataRuleError(t *testing.T) {
	_, repo := setupPageDB(t)
	// SQL 条件类型被 applyDataRule 拒绝 → DataRuleBuilder().GetError() 非 nil
	badRules := []DataRule{{Column: "age", Condition: "SQL", Value: "1=1"}}
	badCtx := context.WithValue(context.Background(), DataRuleKey, badRules)

	t.Run("FindAsTx", func(t *testing.T) {
		q, _ := NewQuery[pageUser](badCtx)
		var rows []pageVO
		err := FindAsTx[pageUser, pageVO, uint](repo, q, &rows, nil)
		assertError(t, err, true, "FindAsTx 应透传 DataRule 错误")
	})
	t.Run("FindOneAsTx", func(t *testing.T) {
		q, _ := NewQuery[pageUser](badCtx)
		var one pageVO
		err := FindOneAsTx[pageUser, pageVO, uint](repo, q, &one, nil)
		assertError(t, err, true, "FindOneAsTx 应透传 DataRule 错误")
	})
	t.Run("PageAsTx", func(t *testing.T) {
		q, _ := NewQuery[pageUser](badCtx)
		var rows []pageVO
		_, err := PageAsTx[pageUser, pageVO, uint](repo, q, &rows, false, nil)
		assertError(t, err, true, "PageAsTx 应透传 DataRule 错误")
	})
}
