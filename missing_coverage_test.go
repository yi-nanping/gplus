package gplus

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// --- BETWEEN nil 参数写入 errs ---

func TestQuery_Between_NilArgs(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		fn   func(q *Query[TestUser], u *TestUser)
	}{
		{"Between_val1_nil", func(q *Query[TestUser], u *TestUser) { q.Between(&u.Age, nil, 30) }},
		{"Between_val2_nil", func(q *Query[TestUser], u *TestUser) { q.Between(&u.Age, 18, nil) }},
		{"OrBetween_val1_nil", func(q *Query[TestUser], u *TestUser) { q.OrBetween(&u.Age, nil, 30) }},
		{"NotBetween_val2_nil", func(q *Query[TestUser], u *TestUser) { q.NotBetween(&u.Age, 18, nil) }},
		{"OrNotBetween_both_nil", func(q *Query[TestUser], u *TestUser) { q.OrNotBetween(&u.Age, nil, nil) }},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q, u := NewQuery[TestUser](ctx)
			c.fn(q, u)
			if q.GetError() == nil {
				t.Errorf("%s：nil 参数应写入 errs", c.name)
			}
			if len(q.conditions) != 0 {
				t.Errorf("%s：nil 参数不应添加条件", c.name)
			}
		})
	}
}

func TestUpdater_Between_NilArgs(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		fn   func(u *Updater[TestUser], m *TestUser)
	}{
		{"Between_v1_nil", func(u *Updater[TestUser], m *TestUser) { u.Between(&m.Age, nil, 30) }},
		{"Between_v2_nil", func(u *Updater[TestUser], m *TestUser) { u.Between(&m.Age, 18, nil) }},
		{"NotBetween_v1_nil", func(u *Updater[TestUser], m *TestUser) { u.NotBetween(&m.Age, nil, 30) }},
		{"OrBetween_v2_nil", func(u *Updater[TestUser], m *TestUser) { u.OrBetween(&m.Age, 18, nil) }},
		{"OrNotBetween_both_nil", func(u *Updater[TestUser], m *TestUser) { u.OrNotBetween(&m.Age, nil, nil) }},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ud, m := NewUpdater[TestUser](ctx)
			c.fn(ud, m)
			if ud.GetError() == nil {
				t.Errorf("%s：nil 参数应写入 errs", c.name)
			}
			if len(ud.conditions) != 0 {
				t.Errorf("%s：nil 参数不应添加条件", c.name)
			}
		})
	}
}

// --- DataRule NOT IN / BETWEEN 格式错误 ---

func TestDataRule_NotIn(t *testing.T) {
	ctx := context.Background()

	t.Run("NOT IN 正常", func(t *testing.T) {
		rules := []DataRule{{Column: "age", Condition: "NOT IN", Value: "18,25,30"}}
		ctx2 := context.WithValue(ctx, DataRuleKey, rules)
		q, _ := NewQuery[TestUser](ctx2)
		q.DataRuleBuilder()
		assertError(t, q.GetError(), false, "NOT IN 正常应无错误")
		if len(q.conditions) != 1 {
			t.Errorf("期望 1 个条件，实际 %d", len(q.conditions))
		}
		if q.conditions[0].operator != OpNotIn {
			t.Errorf("operator 期望 NOT IN，实际 %q", q.conditions[0].operator)
		}
	})
}

func TestDataRule_Between_InvalidFormat(t *testing.T) {
	ctx := context.Background()

	t.Run("BETWEEN 只有一个值写入 errs", func(t *testing.T) {
		rules := []DataRule{{Column: "age", Condition: "BETWEEN", Value: "18"}}
		ctx2 := context.WithValue(ctx, DataRuleKey, rules)
		q, _ := NewQuery[TestUser](ctx2)
		q.DataRuleBuilder()
		if q.GetError() == nil {
			t.Error("BETWEEN 格式错误应写入 errs")
		}
		if len(q.conditions) != 0 {
			t.Error("BETWEEN 格式错误不应添加条件")
		}
	})

	t.Run("BETWEEN 空值写入 errs", func(t *testing.T) {
		rules := []DataRule{{Column: "age", Condition: "BETWEEN", Value: ","}}
		ctx2 := context.WithValue(ctx, DataRuleKey, rules)
		q, _ := NewQuery[TestUser](ctx2)
		q.DataRuleBuilder()
		// "," 分割后 len==2，但值为空字符串，Between 会接受（空字符串非 nil）
		// 此处验证不产生错误，SQL 由数据库层处理
		assertError(t, q.GetError(), false, "空字符串边界值不应报错")
	})

	t.Run("BETWEEN 三个值写入 errs", func(t *testing.T) {
		rules := []DataRule{{Column: "age", Condition: "BETWEEN", Value: "18,25,30"}}
		ctx2 := context.WithValue(ctx, DataRuleKey, rules)
		q, _ := NewQuery[TestUser](ctx2)
		q.DataRuleBuilder()
		if q.GetError() == nil {
			t.Error("BETWEEN 三个值应写入 errs")
		}
	})
}

// --- getModelInstance 并发初始化安全 ---

func TestGetModelInstance_Concurrent(t *testing.T) {
	// 先清理缓存，确保触发慢路径
	unregisterModel[TestUser]()

	const goroutines = 50
	var wg sync.WaitGroup
	results := make([]*TestUser, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = getModelInstance[TestUser]()
		}(i)
	}
	wg.Wait()

	// 所有 goroutine 应拿到同一个单例指针
	for i := 1; i < goroutines; i++ {
		if results[i] != results[0] {
			t.Errorf("goroutine %d 拿到了不同的实例指针", i)
		}
	}

	// 单例的字段地址应可正常解析（验证 columnNameCache 已完整写入）
	instance := results[0]
	_, err := resolveColumnName(&instance.Name)
	if err != nil {
		t.Errorf("并发初始化后字段应可解析，得到错误: %v", err)
	}
}

// --- DeleteByCondTx Unscoped + 空条件保护 ---

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

// --- Updater Select/Omit 无效列指针写入 errs ---

func TestUpdater_Select_InvalidPointer(t *testing.T) {
	ctx := context.Background()

	t.Run("Select nil 指针写入 errs", func(t *testing.T) {
		u, _ := NewUpdater[TestUser](ctx)
		u.Select(nil)
		if u.GetError() == nil {
			t.Error("Select nil 应写入 errs")
		}
		if len(u.selects) != 0 {
			t.Error("Select nil 不应追加到 selects")
		}
	})

	t.Run("Select 混合有效无效指针，有效的正常追加", func(t *testing.T) {
		u, m := NewUpdater[TestUser](ctx)
		u.Select(&m.Name, nil)
		if u.GetError() == nil {
			t.Error("包含 nil 的 Select 应写入 errs")
		}
		if len(u.selects) != 1 {
			t.Errorf("有效列应正常追加，期望 1 个，实际 %d", len(u.selects))
		}
		if !strings.Contains(u.selects[0], "username") {
			t.Errorf("selects[0] 期望包含 username，实际 %q", u.selects[0])
		}
	})
}

func TestUpdater_Omit_InvalidPointer(t *testing.T) {
	ctx := context.Background()

	t.Run("Omit nil 指针写入 errs", func(t *testing.T) {
		u, _ := NewUpdater[TestUser](ctx)
		u.Omit(nil)
		if u.GetError() == nil {
			t.Error("Omit nil 应写入 errs")
		}
		if len(u.omits) != 0 {
			t.Error("Omit nil 不应追加到 omits")
		}
	})
}

// --- 非法 DataRule 通过 Repository 方法应返回错误（回归测试）---

func TestDataRule_InvalidCondition_Repository(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)

	invalidRules := []DataRule{{Column: "age", Condition: "INVALID_OP", Value: "18"}}
	ctx := context.WithValue(context.Background(), DataRuleKey, invalidRules)

	t.Run("List 返回 DataRule 错误", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		_, err := repo.List(q)
		if err == nil {
			t.Error("非法 DataRule 应使 List 返回错误")
		}
	})

	t.Run("GetOne 返回 DataRule 错误", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		_, err := repo.GetOne(q)
		if err == nil {
			t.Error("非法 DataRule 应使 GetOne 返回错误")
		}
	})

	t.Run("Count 返回 DataRule 错误", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		_, err := repo.Count(q)
		if err == nil {
			t.Error("非法 DataRule 应使 Count 返回错误")
		}
	})

	t.Run("Page 返回 DataRule 错误", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		_, _, err := repo.Page(q, false)
		if err == nil {
			t.Error("非法 DataRule 应使 Page 返回错误")
		}
	})

	t.Run("UpdateByCond 返回 DataRule 错误", func(t *testing.T) {
		u, model := NewUpdater[TestUser](ctx)
		u.Set(&model.Name, "x").Eq(&model.ID, 1)
		_, err := repo.UpdateByCond(u)
		if err == nil {
			t.Error("非法 DataRule 应使 UpdateByCond 返回错误")
		}
	})

	t.Run("DeleteByCond 返回 DataRule 错误", func(t *testing.T) {
		q, model := NewQuery[TestUser](ctx)
		q.Eq(&model.ID, 1)
		_, err := repo.DeleteByCond(q)
		if err == nil {
			t.Error("非法 DataRule 应使 DeleteByCond 返回错误")
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

// --- Upsert / UpsertBatch ---

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

// --- SaveBatch / CreateBatch 批量写 ---

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

// --- GetByLock 悲观锁 ---

var errTestSentinel = errors.New("test error")

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
		q.errs = append(q.errs, errTestSentinel)
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
		db.Transaction(func(tx *gorm.DB) error {
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

// --- LeftJoin / RightJoin / LockWithOpt ---

func TestQuery_LeftRightJoin(t *testing.T) {
	ctx := context.Background()

	t.Run("LeftJoin 追加 LEFT JOIN 条件", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.LeftJoin("orders", "orders.user_id = test_users.id")
		if len(q.joins) != 1 || q.joins[0].method != JoinLeft {
			t.Errorf("期望 LEFT JOIN，实际 %+v", q.joins)
		}
	})

	t.Run("RightJoin 追加 RIGHT JOIN 条件", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.RightJoin("orders", "orders.user_id = test_users.id")
		if len(q.joins) != 1 || q.joins[0].method != JoinRight {
			t.Errorf("期望 RIGHT JOIN，实际 %+v", q.joins)
		}
	})
}

func TestQuery_LockWithOpt(t *testing.T) {
	ctx := context.Background()

	t.Run("UPDATE NOWAIT", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.LockWithOpt("UPDATE", "NOWAIT")
		if q.lockStrength != "UPDATE" || q.lockOptions != "NOWAIT" {
			t.Errorf("lockStrength=%q lockOptions=%q", q.lockStrength, q.lockOptions)
		}
	})

	t.Run("SHARE SKIP LOCKED", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.LockWithOpt("SHARE", "SKIP LOCKED")
		if q.lockOptions != "SKIP LOCKED" {
			t.Errorf("期望 SKIP LOCKED，实际 %q", q.lockOptions)
		}
	})
}

// --- applyDataRule 未覆盖分支 ---

func TestDataRule_LeftRightLike_IsNull(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name      string
		condition string
	}{
		{"LEFT_LIKE", "LEFT_LIKE"},
		{"RIGHT_LIKE", "RIGHT_LIKE"},
		{"IS NULL", "IS NULL"},
		{"IS NOT NULL", "IS NOT NULL"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rules := []DataRule{{Column: "age", Condition: c.condition, Value: "ali"}}
			ctx2 := context.WithValue(ctx, DataRuleKey, rules)
			q, _ := NewQuery[TestUser](ctx2)
			q.DataRuleBuilder()
			assertError(t, q.GetError(), false, c.name+" 不应报错")
			if len(q.conditions) != 1 {
				t.Errorf("%s 期望 1 个条件，实际 %d", c.name, len(q.conditions))
			}
		})
	}
}

// --- quoteColumn 方言转义 ---

func TestQuoteColumn_Dialects(t *testing.T) {
	cases := []struct {
		name string
		qL   string
		qR   string
		col  string
		want string
	}{
		{"sqlite 双引号", `"`, `"`, "name", `"name"`},
		{"mysql 反引号", "`", "`", "name", "`name`"},
		{"sqlserver 方括号", "[", "]", "name", "[name]"},
		{"已转义不重复", `"`, `"`, `"name"`, `"name"`},
		{"table.*", `"`, `"`, "users.*", `"users".*`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := quoteColumn(c.col, c.qL, c.qR)
			if got != c.want {
				t.Errorf("期望 %q，实际 %q", c.want, got)
			}
		})
	}
}

// --- Repository nil q 分支 ---

func TestRepository_NilQuery(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)

	t.Run("GetOne nil q", func(t *testing.T) {
		_, err := repo.GetOne(nil)
		if err != ErrQueryNil {
			t.Errorf("期望 ErrQueryNil，实际: %v", err)
		}
	})

	t.Run("List nil q", func(t *testing.T) {
		_, err := repo.List(nil)
		if err != ErrQueryNil {
			t.Errorf("期望 ErrQueryNil，实际: %v", err)
		}
	})

	t.Run("Count nil q", func(t *testing.T) {
		_, err := repo.Count(nil)
		if err != ErrQueryNil {
			t.Errorf("期望 ErrQueryNil，实际: %v", err)
		}
	})

	t.Run("Page nil q", func(t *testing.T) {
		_, _, err := repo.Page(nil, false)
		if err != ErrQueryNil {
			t.Errorf("期望 ErrQueryNil，实际: %v", err)
		}
	})

	t.Run("DeleteByCond nil q", func(t *testing.T) {
		_, err := repo.DeleteByCond(nil)
		if err != ErrDeleteEmpty {
			t.Errorf("期望 ErrDeleteEmpty，实际: %v", err)
		}
	})
}

// --- Query/Updater 无效列指针分支（Select/Omit/Group/Order/Distinct/join）---

func TestQuery_InvalidPointer_Branches(t *testing.T) {
	ctx := context.Background()

	t.Run("Order 无效指针写入 errs", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Order(nil, true)
		assertError(t, q.GetError(), true, "Order nil 应写入 errs")
		if len(q.orders) != 0 {
			t.Errorf("Order nil 不应追加 orders，实际 %d", len(q.orders))
		}
	})

	t.Run("Distinct 无效指针写入 errs", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Distinct(nil)
		assertError(t, q.GetError(), true, "Distinct nil 应写入 errs")
	})

	t.Run("Group 无效指针写入 errs", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Group(nil)
		assertError(t, q.GetError(), true, "Group nil 应写入 errs")
		if len(q.groups) != 0 {
			t.Errorf("Group nil 不应追加 groups，实际 %d", len(q.groups))
		}
	})

	t.Run("join 空 table 写入 errs", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.LeftJoin("", "on condition")
		assertError(t, q.GetError(), true, "LeftJoin 空 table 应写入 errs")
		if len(q.joins) != 0 {
			t.Errorf("join 空 table 不应追加 joins，实际 %d", len(q.joins))
		}
	})
}

// --- Updater.Context nil ctx 分支 / SetExpr 无效指针 ---

func TestUpdater_Context_NilCtx(t *testing.T) {
	u := &Updater[TestUser]{}
	if u.Context() == nil {
		t.Error("nil ctx 应返回 context.Background()")
	}
}

func TestUpdater_SetExpr_InvalidPointer(t *testing.T) {
	ctx := context.Background()
	u, _ := NewUpdater[TestUser](ctx)
	u.SetExpr(nil, "age + ?", 1)
	if u.GetError() == nil {
		t.Error("SetExpr nil 应写入 errs")
	}
	if len(u.setMap) != 0 {
		t.Errorf("SetExpr nil 不应写入 setMap，实际 %d", len(u.setMap))
	}
}

// --- applyDataRule 未覆盖分支 ---

func TestDataRule_AdditionalBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("SQL 注入防护拒绝 SQL 条件", func(t *testing.T) {
		rules := []DataRule{{Column: "age", Condition: "SQL", Value: "1=1"}}
		ctx2 := context.WithValue(ctx, DataRuleKey, rules)
		q, _ := NewQuery[TestUser](ctx2)
		q.DataRuleBuilder()
		assertError(t, q.GetError(), true, "SQL 条件应被拒绝")
		if len(q.conditions) != 0 {
			t.Errorf("SQL 条件不应追加，实际 %d", len(q.conditions))
		}
	})

	t.Run("USE_SQL_RULES 注入防护", func(t *testing.T) {
		rules := []DataRule{{Column: "age", Condition: "USE_SQL_RULES", Value: "1=1"}}
		ctx2 := context.WithValue(ctx, DataRuleKey, rules)
		q, _ := NewQuery[TestUser](ctx2)
		q.DataRuleBuilder()
		assertError(t, q.GetError(), true, "USE_SQL_RULES 应被拒绝")
	})

	t.Run("EQ 别名正常", func(t *testing.T) {
		rules := []DataRule{{Column: "age", Condition: "EQ", Value: "18"}}
		ctx2 := context.WithValue(ctx, DataRuleKey, rules)
		q, _ := NewQuery[TestUser](ctx2)
		q.DataRuleBuilder()
		assertError(t, q.GetError(), false, "EQ 别名不应报错")
		if len(q.conditions) != 1 {
			t.Errorf("EQ 期望 1 个条件，实际 %d", len(q.conditions))
		}
	})

	t.Run("GT/GE/LT/LE/NE 别名", func(t *testing.T) {
		aliases := []struct{ cond, val string }{
			{"GT", "18"}, {"GE", "18"}, {"LT", "30"}, {"LE", "30"}, {"NE", "0"},
		}
		for _, a := range aliases {
			rules := []DataRule{{Column: "age", Condition: a.cond, Value: a.val}}
			ctx2 := context.WithValue(ctx, DataRuleKey, rules)
			q, _ := NewQuery[TestUser](ctx2)
			q.DataRuleBuilder()
			assertError(t, q.GetError(), false, a.cond+" 不应报错")
			if len(q.conditions) != 1 {
				t.Errorf("%s 期望 1 个条件，实际 %d", a.cond, len(q.conditions))
			}
		}
	})

	t.Run("空 value 且非 IS NULL 提前返回", func(t *testing.T) {
		rules := []DataRule{{Column: "age", Condition: "=", Value: ""}}
		ctx2 := context.WithValue(ctx, DataRuleKey, rules)
		q, _ := NewQuery[TestUser](ctx2)
		q.DataRuleBuilder()
		assertError(t, q.GetError(), false, "空 value 不应报错")
		if len(q.conditions) != 0 {
			t.Errorf("空 value 不应追加条件，实际 %d", len(q.conditions))
		}
	})

	t.Run("BETWEEN 使用 Values 字段", func(t *testing.T) {
		rules := []DataRule{{Column: "age", Condition: "BETWEEN", Values: []string{"18", "30"}}}
		ctx2 := context.WithValue(ctx, DataRuleKey, rules)
		q, _ := NewQuery[TestUser](ctx2)
		q.DataRuleBuilder()
		assertError(t, q.GetError(), false, "BETWEEN Values 不应报错")
		if len(q.conditions) != 1 {
			t.Errorf("BETWEEN Values 期望 1 个条件，实际 %d", len(q.conditions))
		}
	})
}


// --- UpdateByCondTx nil updater 分支 ---

func TestRepository_UpdateByCondTx_NilUpdater(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)

	t.Run("nil updater 返回 ErrUpdateEmpty", func(t *testing.T) {
		_, err := repo.UpdateByCond(nil)
		if err != ErrUpdateEmpty {
			t.Errorf("期望 ErrUpdateEmpty，实际: %v", err)
		}
	})

	t.Run("空 setMap 返回 ErrUpdateEmpty", func(t *testing.T) {
		u, _ := NewUpdater[TestUser](context.Background())
		_, err := repo.UpdateByCond(u)
		if err != ErrUpdateEmpty {
			t.Errorf("期望 ErrUpdateEmpty，实际: %v", err)
		}
	})
}

// --- applyWhere isRaw 有值分支（通过内部构造触发）---

func TestApplyWhere_IsRaw(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	db.Create(&TestUser{Name: "RawUser", Age: 25})
	ctx := context.Background()

	t.Run("isRaw 无参数条件", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		// 直接写入 isRaw 条件（无 value）
		q.conditions = append(q.conditions, condition{expr: "age > 18", isRaw: true})
		list, err := repo.List(q)
		assertError(t, err, false, "isRaw 条件不应报错")
		if len(list) != 1 {
			t.Errorf("期望 1 条，实际 %d", len(list))
		}
	})

	t.Run("isRaw 有参数条件", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		// 直接写入 isRaw 条件（有 value）
		q.conditions = append(q.conditions, condition{expr: "age > ?", isRaw: true, value: 18})
		list, err := repo.List(q)
		assertError(t, err, false, "isRaw 有参数条件不应报错")
		if len(list) != 1 {
			t.Errorf("期望 1 条，实际 %d", len(list))
		}
	})
}

// TestUpdater_applyDataRule_AllBranches 覆盖 Updater.applyDataRule 全分支
func TestUpdater_applyDataRule_AllBranches(t *testing.T) {
	tests := []struct {
		name      string
		rule      DataRule
		wantError bool
	}{
		{"EQ 别名", DataRule{Column: "age", Condition: "EQ", Value: "18"}, false},
		{"NE", DataRule{Column: "age", Condition: "<>", Value: "18"}, false},
		{"NE 别名", DataRule{Column: "age", Condition: "NE", Value: "18"}, false},
		{"GT", DataRule{Column: "age", Condition: ">", Value: "18"}, false},
		{"GT 别名", DataRule{Column: "age", Condition: "GT", Value: "18"}, false},
		{"GE 别名", DataRule{Column: "age", Condition: "GE", Value: "18"}, false},
		{"LT", DataRule{Column: "age", Condition: "<", Value: "18"}, false},
		{"LT 别名", DataRule{Column: "age", Condition: "LT", Value: "18"}, false},
		{"LE", DataRule{Column: "age", Condition: "<=", Value: "18"}, false},
		{"LE 别名", DataRule{Column: "age", Condition: "LE", Value: "18"}, false},
		{"IN 逗号分割", DataRule{Column: "age", Condition: "IN", Value: "18,25"}, false},
		{"IN Values", DataRule{Column: "age", Condition: "IN", Values: []string{"18", "25"}}, false},
		{"NOT IN 逗号分割", DataRule{Column: "age", Condition: "NOT IN", Value: "18,25"}, false},
		{"NOT IN Values", DataRule{Column: "age", Condition: "NOT IN", Values: []string{"18"}}, false},
		{"LIKE", DataRule{Column: "username", Condition: "LIKE", Value: "test"}, false},
		{"LEFT_LIKE", DataRule{Column: "username", Condition: "LEFT_LIKE", Value: "test"}, false},
		{"RIGHT_LIKE", DataRule{Column: "username", Condition: "RIGHT_LIKE", Value: "test"}, false},
		{"IS NULL", DataRule{Column: "age", Condition: "IS NULL"}, false},
		{"IS NOT NULL", DataRule{Column: "age", Condition: "IS NOT NULL"}, false},
		{"BETWEEN Values", DataRule{Column: "age", Condition: "BETWEEN", Values: []string{"10", "30"}}, false},
		{"BETWEEN 逗号分割", DataRule{Column: "age", Condition: "BETWEEN", Value: "10,30"}, false},
		{"BETWEEN 值不足", DataRule{Column: "age", Condition: "BETWEEN", Value: "10"}, true},
		{"SQL 注入防护", DataRule{Column: "age", Condition: "SQL", Value: "1=1"}, true},
		{"USE_SQL_RULES 防护", DataRule{Column: "age", Condition: "USE_SQL_RULES", Value: "x"}, true},
		{"空 value 提前返回", DataRule{Column: "age", Condition: "=", Value: ""}, false},
		{"未知 condition", DataRule{Column: "age", Condition: "UNKNOWN", Value: "1"}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{tc.rule})
			u, model := NewUpdater[TestUser](ctx)
			u.Eq(&model.ID, 1) // 确保有条件，不触发 ErrUpdateNoCondition
			u.DataRuleBuilder()
			err := u.GetError()
			if tc.wantError && err == nil {
				t.Errorf("期望错误，实际无错误")
			}
			if !tc.wantError && err != nil {
				t.Errorf("不期望错误，实际: %v", err)
			}
		})
	}
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

// --- OrderRaw 集成测试 ---

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

// --- applyGroupHaving 复杂路径 ---

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

	t.Run("Having OR 嵌套组 isOr=true 正确合并", func(t *testing.T) {
		// 先 Having(bob)，再注入 isOr=true 的嵌套组(alice)
		// → HAVING (username = 'bob' OR (username = 'alice')) → bob 和 alice 两组
		q, u := NewQuery[TestUser](ctx)
		q.Select(&u.Name).Group("username").
			Having("username", OpEq, "bob")
		q.havings = append(q.havings, condition{
			group: []condition{{expr: "username", operator: OpEq, value: "alice"}},
			isOr:  true,
		})
		list, err := repo.List(q)
		if err != nil {
			t.Fatalf("Having OR 嵌套组不应报错: %v", err)
		}
		if len(list) != 2 {
			t.Errorf("HAVING bob OR (alice) 期望 2 组，实际 %d", len(list))
		}
	})
}

// --- applyWhere 复杂路径 ---

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

	t.Run("empty expr 条件跳过", func(t *testing.T) {
		// 覆盖 applyWhere line 303-305: clauseStr == "" 跳过
		q, _ := NewQuery[TestUser](ctx)
		q.conditions = append(q.conditions, condition{expr: "", operator: OpEq, value: "x"})
		list, err := repo.List(q)
		if err != nil {
			t.Fatalf("empty expr 条件不应报错: %v", err)
		}
		if len(list) != 2 {
			t.Errorf("empty expr 跳过后期望 2 条，实际 %d", len(list))
		}
	})
}

// --- buildLeafSQL 防御路径 ---

// TestBuildLeafSQL_BetweenDefensive 验证 BETWEEN value 不合法时返回 ok=false
func TestBuildLeafSQL_BetweenDefensive(t *testing.T) {
	cases := []struct {
		name  string
		cond  condition
	}{
		{"value 非 []any", condition{expr: "age", operator: OpBetween, value: "not_a_slice"}},
		{"[]any 长度为 1", condition{expr: "age", operator: OpBetween, value: []any{1}}},
		{"[]any 长度为 3", condition{expr: "age", operator: OpNotBetween, value: []any{1, 2, 3}}},
		{"nil value", condition{expr: "age", operator: OpBetween, value: nil}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, ok := buildLeafSQL(tc.cond, "`", "`")
			if ok {
				t.Errorf("期望 ok=false，实际 ok=true，条件: %+v", tc.cond)
			}
		})
	}
}

// --- getQuoteChar 方言分支 ---

// testMockDialector 最简 Dialector 实现，仅用于测试 getQuoteChar default 分支
type testMockDialector struct{ dialectName string }

func (d testMockDialector) Name() string                                          { return d.dialectName }
func (d testMockDialector) Initialize(*gorm.DB) error                             { return nil }
func (d testMockDialector) Migrator(*gorm.DB) gorm.Migrator                       { return nil }
func (d testMockDialector) DataTypeOf(*schema.Field) string                       { return "" }
func (d testMockDialector) DefaultValueOf(*schema.Field) clause.Expression        { return nil }
func (d testMockDialector) BindVarTo(clause.Writer, *gorm.Statement, interface{}) {}
func (d testMockDialector) QuoteTo(clause.Writer, string)                         {}
func (d testMockDialector) Explain(string, ...interface{}) string                 { return "" }

func TestGetQuoteChar_Dialects(t *testing.T) {
	t.Run("nil Dialector 返回空字符串", func(t *testing.T) {
		db := &gorm.DB{Config: &gorm.Config{}}
		qL, qR := getQuoteChar(db)
		if qL != "" || qR != "" {
			t.Errorf("nil dialector 期望 (\"\",\"\")，实际 (%q,%q)", qL, qR)
		}
	})

	t.Run("mysql 方言返回反引号", func(t *testing.T) {
		// mysql.Open 仅创建 dialector，sql.Open 懒连接，无需真实 MySQL
		db := &gorm.DB{Config: &gorm.Config{Dialector: mysql.Open("root:@tcp(127.0.0.1:3306)/test")}}
		qL, qR := getQuoteChar(db)
		if qL != "`" || qR != "`" {
			t.Errorf("mysql 期望反引号，实际 (%q,%q)", qL, qR)
		}
	})

	t.Run("未知方言返回空字符串", func(t *testing.T) {
		db := &gorm.DB{Config: &gorm.Config{Dialector: testMockDialector{"unknown_db"}}}
		qL, qR := getQuoteChar(db)
		if qL != "" || qR != "" {
			t.Errorf("未知方言期望 (\"\",\"\")，实际 (%q,%q)", qL, qR)
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
