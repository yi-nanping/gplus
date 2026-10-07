package gplus

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
	"strings"
	"sync"
	"testing"
)

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
		if !strings.Contains(u.selects[0].expr, "username") {
			t.Errorf("selects[0] 期望包含 username，实际 %q", u.selects[0].expr)
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

func TestDataRule_InvalidCondition_Repository(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))

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

var errTestSentinel = errors.New("test error")

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

func TestRepository_NilQuery(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))

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

func TestRepository_UpdateByCondTx_NilUpdater(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))

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

// TestBuildLeafSQL_BetweenDefensive 验证 BETWEEN value 不合法时返回 ok=false
func TestBuildLeafSQL_BetweenDefensive(t *testing.T) {
	cases := []struct {
		name string
		cond condition
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

// testMockDialector 最简 Dialector 实现，仅用于测试 getQuoteChar default 分支
type testMockDialector struct{ dialectName string }

func (d testMockDialector) Name() string { return d.dialectName }

func (d testMockDialector) Initialize(*gorm.DB) error { return nil }

func (d testMockDialector) Migrator(*gorm.DB) gorm.Migrator { return nil }

func (d testMockDialector) DataTypeOf(*schema.Field) string { return "" }

func (d testMockDialector) DefaultValueOf(*schema.Field) clause.Expression { return nil }

func (d testMockDialector) BindVarTo(clause.Writer, *gorm.Statement, interface{}) {}

func (d testMockDialector) QuoteTo(clause.Writer, string) {}

func (d testMockDialector) Explain(string, ...interface{}) string { return "" }

func TestGetQuoteChar_Dialects(t *testing.T) {
	t.Run("nil Dialector 返回空字符串", func(t *testing.T) {
		db := &gorm.DB{Config: &gorm.Config{}}
		qL, qR := getQuoteChar(db)
		if qL != "" || qR != "" {
			t.Errorf("nil dialector 期望 (\"\",\"\")，实际 (%q,%q)", qL, qR)
		}
	})

	t.Run("mysql 方言返回反引号", func(t *testing.T) {
		// 仅验证方言名称分支，不加载数据库驱动。
		db := &gorm.DB{Config: &gorm.Config{Dialector: testMockDialector{"mysql"}}}
		qL, qR := getQuoteChar(db)
		if qL != "`" || qR != "`" {
			t.Errorf("mysql 期望反引号，实际 (%q,%q)", qL, qR)
		}
	})

	t.Run("oracle 方言返回空 quoter 避免 case 冲突", func(t *testing.T) {
		// 用 testMockDialector 模拟 Oracle，避免在默认 build 引入 gorm-oracle 依赖。
		// godoes/gorm-oracle migrator 用 UPPERCASE 不带引号建表，加双引号转义会
		// 触发 ORA-00904 invalid identifier，因此 oracle 分支返回空 quoter。
		db := &gorm.DB{Config: &gorm.Config{Dialector: testMockDialector{"oracle"}}}
		qL, qR := getQuoteChar(db)
		if qL != "" || qR != "" {
			t.Errorf("oracle 期望空字符串，实际 (%q,%q)", qL, qR)
		}
	})

	t.Run("dm 方言返回双引号", func(t *testing.T) {
		// 用 testMockDialector 模拟 DM，避免在默认 build 引入 gorm-dameng 依赖。
		// 实测决策（推翻 spec 早期假设）：godoes/gorm-dameng migrator 用带引号
		// lowercase 建表，列名存为 case-sensitive 小写。DM CASE_SENSITIVE=Y +
		// Oracle 兼容模式下裸标识符会被 UPPERCASE 解析导致无效列名错误，必须
		// 用双引号锁定小写。dm 与 postgres/sqlite 共用双引号 quoter，不与
		// oracle 共用空 quoter（详见 builder.go 注释）。
		db := &gorm.DB{Config: &gorm.Config{Dialector: testMockDialector{"dm"}}}
		qL, qR := getQuoteChar(db)
		if qL != "\"" || qR != "\"" {
			t.Errorf("dm 期望双引号，实际 (%q,%q)", qL, qR)
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

// M-2: BuildQueryDB 把当前 Query 条件应用到 db 并返回带条件的 *gorm.DB（公开便捷 API，原零调用零覆盖）。
func TestBuildQueryDB_AppliesConditions(t *testing.T) {
	db := newDryRunDB(t)
	q, u := NewQuery[TestUser](context.Background())
	q.Eq(&u.Name, "alice")
	got := q.BuildQueryDB(db.Session(&gorm.Session{DryRun: true}).Model(&TestUser{}))
	sql := got.Find(&[]TestUser{}).Statement.SQL.String()
	if !strings.Contains(sql, "username") {
		t.Errorf("BuildQueryDB 应应用 Eq 条件（含 username 列），实际: %s", sql)
	}
}

// 私有错误桶注入保留在核心模块；真实带锁查询位于 tests 模块。
func TestRepository_GetByLock(t *testing.T) {
	db := newDryRunDB(t)
	repo := NewRepository[int64, TestUser](db)
	q, _ := NewQuery[TestUser](context.Background())
	q.errs = append(q.errs, errTestSentinel)
	_, err := repo.GetByLock(q, db)
	if !errors.Is(err, errTestSentinel) {
		t.Fatalf("builder error=%v", err)
	}
}

// 直接注入私有 raw 条件，验证有值/无值两个白盒分支；真实查询位于 tests 模块。
func TestApplyWhere_IsRaw(t *testing.T) {
	db := newDryRunDB(t)
	for _, tc := range []struct {
		name string
		cond condition
		vars int
	}{
		{"isRaw 无参数条件", condition{expr: "age > 18", isRaw: true}, 0},
		{"isRaw 有参数条件", condition{expr: "age > ?", isRaw: true, value: 18}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, _ := NewQuery[TestUser](context.Background())
			q.conditions = append(q.conditions, tc.cond)
			result := db.Model(new(TestUser)).Scopes(q.BuildQuery()).Find(&[]TestUser{})
			if result.Error != nil {
				t.Fatal(result.Error)
			}
			if !strings.Contains(result.Statement.SQL.String(), tc.cond.expr) || len(result.Statement.Vars) != tc.vars {
				t.Fatalf("SQL=%s vars=%v", result.Statement.SQL.String(), result.Statement.Vars)
			}
		})
	}
}

// 公共接口无法直接构造 OR 嵌套 HAVING 私有节点，保留其 SQL 构建白盒测试。
func TestApplyGroupHaving_ComplexPaths(t *testing.T) {
	db := newDryRunDB(t)
	q, u := NewQuery[TestUser](context.Background())
	q.Select(&u.Name).Group("username").Having("username", OpEq, "bob")
	q.havings = append(q.havings, condition{group: []condition{{expr: "username", operator: OpEq, value: "alice"}}, isOr: true})
	result := db.Model(new(TestUser)).Scopes(q.BuildQuery()).Find(&[]TestUser{})
	sql := result.Statement.SQL.String()
	if result.Error != nil || !strings.Contains(sql, "HAVING") || !strings.Contains(sql, " OR ") || !strings.Contains(sql, "(") {
		t.Fatalf("error=%v SQL=%s", result.Error, sql)
	}
	if len(result.Statement.Vars) != 2 || result.Statement.Vars[0] != "bob" || result.Statement.Vars[1] != "alice" {
		t.Fatalf("vars=%v", result.Statement.Vars)
	}
}

// 公共接口拒绝空列，直接注入空叶节点以保留防御分支验证。
func TestApplyWhere_ComplexPaths(t *testing.T) {
	db := newDryRunDB(t)
	q, _ := NewQuery[TestUser](context.Background())
	q.conditions = append(q.conditions, condition{expr: "", operator: OpEq, value: "x"})
	result := db.Model(new(TestUser)).Scopes(q.BuildQuery()).Find(&[]TestUser{})
	if result.Error != nil || strings.Contains(result.Statement.SQL.String(), "WHERE") || len(result.Statement.Vars) != 0 {
		t.Fatalf("error=%v SQL=%s vars=%v", result.Error, result.Statement.SQL.String(), result.Statement.Vars)
	}
}
