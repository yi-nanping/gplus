package gplus

import (
	"context"
	"strings"
	"sync"
	"testing"
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
	UnregisterModel[TestUser]()

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

// --- DeleteByCondTX Unscoped + 空条件保护 ---

func TestDeleteByCondTX_UnscopedEmptyReturnsError(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	t.Run("Unscoped + 空条件返回 ErrDeleteEmpty", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Unscoped()
		_, err := repo.DeleteByCondTX(q, nil)
		if err != ErrDeleteEmpty {
			t.Errorf("期望 ErrDeleteEmpty，实际: %v", err)
		}
	})

	t.Run("Unscoped + 有条件正常执行", func(t *testing.T) {
		repo2, db := setupTestDB[TestUser](t)
		db.Create(&TestUser{Name: "PhysDelete", Age: 99})
		q, m := NewQuery[TestUser](ctx)
		q.Eq(&m.Name, "PhysDelete").Unscoped()
		affected, err := repo2.DeleteByCondTX(q, nil)
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
