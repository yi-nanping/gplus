package gplus

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestUpdater_Logic 测试更新器的逻辑和错误处理
func TestUpdater_Logic(t *testing.T) {
	ctx := context.Background()
	updater, u := NewUpdater[TestUser](ctx)

	t.Run("正常更新流程", func(t *testing.T) {
		updater.Set(&u.Name, "NewName").Set(&u.Age, 30).Eq(&u.ID, 1)

		assertError(t, updater.GetError(), false, "Update builder should be valid")

		m := updater.UpdateMap()
		if m["username"] != "NewName" {
			t.Error("Set failed for username")
		}
		if m["age"] != 30 {
			t.Error("Set failed for age")
		}
	})

	t.Run("Set 无效列指针写入 errs", func(t *testing.T) {
		badUpdater, _ := NewUpdater[TestUser](ctx)
		badUpdater.Set(nil, "Fail")
		if badUpdater.GetError() == nil {
			t.Error("Set nil 应将错误写入 errs")
		}
	})
}

// TestUpdater_Context 测试 Context 返回值
func TestUpdater_Context(t *testing.T) {
	ctx := context.Background()
	u, _ := NewUpdater[TestUser](ctx)
	if u.Context() != ctx {
		t.Error("Context() 应返回构造时传入的 ctx")
	}
}

// TestUpdater_Helpers 测试 Updater 辅助方法
func TestUpdater_Helpers(t *testing.T) {
	ctx := context.Background()

	t.Run("IsEmpty 无 Set 时为 true", func(t *testing.T) {
		u, _ := NewUpdater[TestUser](ctx)
		if !u.IsEmpty() {
			t.Error("新建 Updater 未 Set 时应为 empty")
		}
	})

	t.Run("IsEmpty Set 后为 false", func(t *testing.T) {
		u, model := NewUpdater[TestUser](ctx)
		u.Set(&model.Name, "test")
		if u.IsEmpty() {
			t.Error("Set 后 Updater 不应为 empty")
		}
	})

	t.Run("Table 动态表名", func(t *testing.T) {
		u, _ := NewUpdater[TestUser](ctx)
		u.Table("users_archive")
		if u.tableName != "users_archive" {
			t.Errorf("Table 设置失败，期望 users_archive，实际 %s", u.tableName)
		}
	})

	t.Run("GetError 无错误返回 nil", func(t *testing.T) {
		u, _ := NewUpdater[TestUser](ctx)
		assertError(t, u.GetError(), false, "无错误时 GetError 应返回 nil")
	})

	t.Run("GetError 单个错误用 error 单数", func(t *testing.T) {
		u, _ := NewUpdater[TestUser](ctx)
		u.errs = append(u.errs, fmt.Errorf("单个错误"))
		err := u.GetError()
		if err == nil {
			t.Fatal("期望有错误")
		}
		if !strings.Contains(err.Error(), "1 error") {
			t.Errorf("期望包含 '1 error'，实际: %s", err.Error())
		}
	})

	t.Run("GetError 多个错误用 errors 复数", func(t *testing.T) {
		u, _ := NewUpdater[TestUser](ctx)
		u.errs = append(u.errs, fmt.Errorf("错误1"), fmt.Errorf("错误2"))
		err := u.GetError()
		if err == nil {
			t.Fatal("期望有错误")
		}
		if !strings.Contains(err.Error(), "2 errors") {
			t.Errorf("期望包含 '2 errors'，实际: %s", err.Error())
		}
	})
}

// TestUpdater_SetExpr 测试 SetExpr 原子表达式更新
func TestUpdater_SetExpr(t *testing.T) {
	ctx := context.Background()
	u, model := NewUpdater[TestUser](ctx)
	u.SetExpr(&model.Age, "age + ?", 1)
	m := u.UpdateMap()
	if _, ok := m["age"]; !ok {
		t.Error("SetExpr 应写入 age 字段")
	}
}

// TestUpdater_SetMap 测试 SetMap 批量更新
func TestUpdater_SetMap(t *testing.T) {
	ctx := context.Background()

	t.Run("正常 SetMap", func(t *testing.T) {
		u, _ := NewUpdater[TestUser](ctx)
		u.SetMap(map[string]any{"username": "Alice", "age": 20})
		m := u.UpdateMap()
		if m["username"] != "Alice" {
			t.Error("SetMap username 失败")
		}
		if m["age"] != 20 {
			t.Error("SetMap age 失败")
		}
	})

	t.Run("空 map 写入 errs", func(t *testing.T) {
		u, _ := NewUpdater[TestUser](ctx)
		u.SetMap(map[string]any{})
		if u.GetError() == nil {
			t.Error("SetMap 空 map 应将错误写入 errs")
		}
	})
}

// TestUpdater_UpdateMap_Copy 测试 UpdateMap 返回副本不影响内部状态
func TestUpdater_UpdateMap_Copy(t *testing.T) {
	ctx := context.Background()
	u, model := NewUpdater[TestUser](ctx)
	u.Set(&model.Name, "Bob")
	m := u.UpdateMap()
	m["username"] = "hacked"
	m2 := u.UpdateMap()
	if m2["username"] != "Bob" {
		t.Error("UpdateMap 应返回副本，不允许外部修改内部状态")
	}
}

// TestUpdater_Select_Omit 测试 Select/Omit
func TestUpdater_Select_Omit(t *testing.T) {
	ctx := context.Background()
	_, model := NewUpdater[TestUser](ctx)

	t.Run("Select 追加字段", func(t *testing.T) {
		u, _ := NewUpdater[TestUser](ctx)
		u.Select(&model.Name, &model.Age)
		if len(u.selects) != 2 {
			t.Errorf("Select 应有 2 个字段，实际 %d", len(u.selects))
		}
	})

	t.Run("Omit 追加字段", func(t *testing.T) {
		u, _ := NewUpdater[TestUser](ctx)
		u.Omit(&model.Email)
		if len(u.omits) != 1 {
			t.Errorf("Omit 应有 1 个字段，实际 %d", len(u.omits))
		}
	})
}

// TestUpdater_Unscoped 测试 Unscoped
func TestUpdater_Unscoped(t *testing.T) {
	ctx := context.Background()
	u, _ := NewUpdater[TestUser](ctx)
	if u.unscoped {
		t.Error("默认 unscoped 应为 false")
	}
	u.Unscoped()
	if !u.unscoped {
		t.Error("Unscoped() 后应为 true")
	}
}

// TestUpdater_Clear 测试 Clear 重置状态
func TestUpdater_Clear(t *testing.T) {
	ctx := context.Background()
	u, model := NewUpdater[TestUser](ctx)
	u.Set(&model.Name, "Alice").Eq(&model.Age, 18)
	u.Clear()
	if !u.IsEmpty() {
		t.Error("Clear 后 setMap 应为空")
	}
	if len(u.conditions) != 0 {
		t.Error("Clear 后 conditions 应为空")
	}

	t.Run("Clear 后 errs 应清空", func(t *testing.T) {
		u2, _ := NewUpdater[TestUser](ctx)
		u2.Set(nil, "bad") // 触发错误
		if u2.GetError() == nil {
			t.Fatal("期望有错误")
		}
		u2.Clear()
		if u2.GetError() != nil {
			t.Error("Clear 后 errs 应为空")
		}
	})
}

// TestUpdater_CompareOps 测试 WHERE 条件方法
func TestUpdater_CompareOps(t *testing.T) {
	ctx := context.Background()

	type opCase struct {
		name     string
		fn       func(u *Updater[TestUser], m *TestUser)
		wantIsOr bool
		wantOp   string
	}

	cases := []opCase{
		{"Eq", func(u *Updater[TestUser], m *TestUser) { u.Eq(&m.Age, 18) }, false, OpEq},
		{"Ne", func(u *Updater[TestUser], m *TestUser) { u.Ne(&m.Age, 18) }, false, OpNe},
		{"Gt", func(u *Updater[TestUser], m *TestUser) { u.Gt(&m.Age, 18) }, false, OpGt},
		{"Ge", func(u *Updater[TestUser], m *TestUser) { u.Ge(&m.Age, 18) }, false, OpGe},
		{"Lt", func(u *Updater[TestUser], m *TestUser) { u.Lt(&m.Age, 18) }, false, OpLt},
		{"Le", func(u *Updater[TestUser], m *TestUser) { u.Le(&m.Age, 18) }, false, OpLe},
		{"OrEq", func(u *Updater[TestUser], m *TestUser) { u.OrEq(&m.Age, 18) }, true, OpEq},
		{"OrNe", func(u *Updater[TestUser], m *TestUser) { u.OrNe(&m.Age, 18) }, true, OpNe},
		{"OrGt", func(u *Updater[TestUser], m *TestUser) { u.OrGt(&m.Age, 18) }, true, OpGt},
		{"OrGe", func(u *Updater[TestUser], m *TestUser) { u.OrGe(&m.Age, 18) }, true, OpGe},
		{"OrLt", func(u *Updater[TestUser], m *TestUser) { u.OrLt(&m.Age, 18) }, true, OpLt},
		{"OrLe", func(u *Updater[TestUser], m *TestUser) { u.OrLe(&m.Age, 18) }, true, OpLe},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			u, model := NewUpdater[TestUser](ctx)
			tc.fn(u, model)
			if len(u.conditions) != 1 {
				t.Fatalf("%s: 期望 1 个条件，实际 %d", tc.name, len(u.conditions))
			}
			c := u.conditions[0]
			if c.operator != tc.wantOp {
				t.Errorf("%s: operator=%s，期望=%s", tc.name, c.operator, tc.wantOp)
			}
			if c.isOr != tc.wantIsOr {
				t.Errorf("%s: isOr=%v，期望=%v", tc.name, c.isOr, tc.wantIsOr)
			}
		})
	}
}

// TestUpdater_LikeOps 测试 Like 系列
func TestUpdater_LikeOps(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name      string
		fn        func(u *Updater[TestUser], m *TestUser)
		wantIsOr  bool
		wantValue string
	}{
		{"Like", func(u *Updater[TestUser], m *TestUser) { u.Like(&m.Name, "Alice") }, false, "%Alice%"},
		{"LikeLeft", func(u *Updater[TestUser], m *TestUser) { u.LikeLeft(&m.Name, "Alice") }, false, "%Alice"},
		{"LikeRight", func(u *Updater[TestUser], m *TestUser) { u.LikeRight(&m.Name, "Alice") }, false, "Alice%"},
		{"NotLike", func(u *Updater[TestUser], m *TestUser) { u.NotLike(&m.Name, "Alice") }, false, "%Alice%"},
		{"OrLike", func(u *Updater[TestUser], m *TestUser) { u.OrLike(&m.Name, "Alice") }, true, "%Alice%"},
		{"OrLikeLeft", func(u *Updater[TestUser], m *TestUser) { u.OrLikeLeft(&m.Name, "Alice") }, true, "%Alice"},
		{"OrLikeRight", func(u *Updater[TestUser], m *TestUser) { u.OrLikeRight(&m.Name, "Alice") }, true, "Alice%"},
		{"OrNotLike", func(u *Updater[TestUser], m *TestUser) { u.OrNotLike(&m.Name, "Alice") }, true, "%Alice%"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			u, model := NewUpdater[TestUser](ctx)
			tc.fn(u, model)
			if len(u.conditions) != 1 {
				t.Fatalf("%s: 期望 1 个条件", tc.name)
			}
			c := u.conditions[0]
			if c.value != tc.wantValue {
				t.Errorf("%s: value=%v，期望=%s", tc.name, c.value, tc.wantValue)
			}
			if c.isOr != tc.wantIsOr {
				t.Errorf("%s: isOr=%v，期望=%v", tc.name, c.isOr, tc.wantIsOr)
			}
		})
	}
}

// TestUpdater_InOps 测试 In/NotIn 系列
func TestUpdater_InOps(t *testing.T) {
	ctx := context.Background()

	t.Run("In", func(t *testing.T) {
		u, model := NewUpdater[TestUser](ctx)
		u.In(&model.Age, []int{1, 2})
		if u.conditions[0].operator != OpIn || u.conditions[0].isOr {
			t.Error("In 条件不正确")
		}
	})

	t.Run("OrIn", func(t *testing.T) {
		u, model := NewUpdater[TestUser](ctx)
		u.OrIn(&model.Age, []int{1, 2})
		if u.conditions[0].operator != OpIn || !u.conditions[0].isOr {
			t.Error("OrIn 条件不正确")
		}
	})

	t.Run("NotIn", func(t *testing.T) {
		u, model := NewUpdater[TestUser](ctx)
		u.NotIn(&model.Age, []int{1, 2})
		if u.conditions[0].operator != OpNotIn || u.conditions[0].isOr {
			t.Error("NotIn 条件不正确")
		}
	})

	t.Run("OrNotIn", func(t *testing.T) {
		u, model := NewUpdater[TestUser](ctx)
		u.OrNotIn(&model.Age, []int{1, 2})
		if u.conditions[0].operator != OpNotIn || !u.conditions[0].isOr {
			t.Error("OrNotIn 条件不正确")
		}
	})
}

// TestUpdater_NullOps 测试 IsNull/IsNotNull 系列
func TestUpdater_NullOps(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name     string
		fn       func(u *Updater[TestUser], m *TestUser)
		wantOp   string
		wantIsOr bool
	}{
		{"IsNull", func(u *Updater[TestUser], m *TestUser) { u.IsNull(&m.Name) }, OpIsNull, false},
		{"OrIsNull", func(u *Updater[TestUser], m *TestUser) { u.OrIsNull(&m.Name) }, OpIsNull, true},
		{"IsNotNull", func(u *Updater[TestUser], m *TestUser) { u.IsNotNull(&m.Name) }, OpIsNotNull, false},
		{"OrIsNotNull", func(u *Updater[TestUser], m *TestUser) { u.OrIsNotNull(&m.Name) }, OpIsNotNull, true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			u, model := NewUpdater[TestUser](ctx)
			tc.fn(u, model)
			if len(u.conditions) != 1 {
				t.Fatalf("%s: 期望 1 个条件", tc.name)
			}
			c := u.conditions[0]
			if c.operator != tc.wantOp {
				t.Errorf("%s: operator=%s，期望=%s", tc.name, c.operator, tc.wantOp)
			}
			if c.isOr != tc.wantIsOr {
				t.Errorf("%s: isOr=%v，期望=%v", tc.name, c.isOr, tc.wantIsOr)
			}
		})
	}
}

// TestUpdater_BetweenOps 测试 Between 系列
func TestUpdater_BetweenOps(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name     string
		fn       func(u *Updater[TestUser], m *TestUser)
		wantOp   string
		wantIsOr bool
	}{
		{"Between", func(u *Updater[TestUser], m *TestUser) { u.Between(&m.Age, 18, 30) }, OpBetween, false},
		{"OrBetween", func(u *Updater[TestUser], m *TestUser) { u.OrBetween(&m.Age, 18, 30) }, OpBetween, true},
		{"NotBetween", func(u *Updater[TestUser], m *TestUser) { u.NotBetween(&m.Age, 18, 30) }, OpNotBetween, false},
		{"OrNotBetween", func(u *Updater[TestUser], m *TestUser) { u.OrNotBetween(&m.Age, 18, 30) }, OpNotBetween, true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			u, model := NewUpdater[TestUser](ctx)
			tc.fn(u, model)
			if len(u.conditions) != 1 {
				t.Fatalf("%s: 期望 1 个条件", tc.name)
			}
			c := u.conditions[0]
			if c.operator != tc.wantOp {
				t.Errorf("%s: operator=%s，期望=%s", tc.name, c.operator, tc.wantOp)
			}
			if c.isOr != tc.wantIsOr {
				t.Errorf("%s: isOr=%v，期望=%v", tc.name, c.isOr, tc.wantIsOr)
			}
			val, ok := c.value.([]any)
			if !ok || len(val) != 2 {
				t.Errorf("%s: value 应为 []any{v1,v2}", tc.name)
			}
		})
	}
}

// TestUpdater_And_Or 测试嵌套块
func TestUpdater_And_Or(t *testing.T) {
	ctx := context.Background()

	t.Run("And 正常嵌套", func(t *testing.T) {
		u, model := NewUpdater[TestUser](ctx)
		u.Eq(&model.Name, "Alice").And(func(sub *Updater[TestUser]) {
			sub.Gt(&model.Age, 18).OrEq(&model.IsActive, true)
		})
		if len(u.conditions) != 2 {
			t.Fatalf("And 后 conditions 应为 2，实际 %d", len(u.conditions))
		}
		grp := u.conditions[1]
		if grp.isOr {
			t.Error("And 嵌套块 isOr 应为 false")
		}
		if len(grp.group) != 2 {
			t.Errorf("And 嵌套块内应有 2 个条件，实际 %d", len(grp.group))
		}
	})

	t.Run("And nil fn 写入 errs", func(t *testing.T) {
		u, _ := NewUpdater[TestUser](ctx)
		u.And(nil)
		if u.GetError() == nil {
			t.Error("And(nil) 应写入 errs")
		}
		if len(u.conditions) != 0 {
			t.Errorf("And(nil) 不应追加条件，实际 %d", len(u.conditions))
		}
	})

	t.Run("Or 正常嵌套", func(t *testing.T) {
		u, model := NewUpdater[TestUser](ctx)
		u.Eq(&model.Name, "Alice").Or(func(sub *Updater[TestUser]) {
			sub.Gt(&model.Age, 18)
		})
		if len(u.conditions) != 2 {
			t.Fatalf("Or 后 conditions 应为 2，实际 %d", len(u.conditions))
		}
		grp := u.conditions[1]
		if !grp.isOr {
			t.Error("Or 嵌套块 isOr 应为 true")
		}
	})

	t.Run("Or nil fn 写入 errs", func(t *testing.T) {
		u, _ := NewUpdater[TestUser](ctx)
		u.Or(nil)
		if u.GetError() == nil {
			t.Error("Or(nil) 应写入 errs")
		}
		if len(u.conditions) != 0 {
			t.Errorf("Or(nil) 不应追加条件，实际 %d", len(u.conditions))
		}
	})

	t.Run("And 子块错误传播到外层", func(t *testing.T) {
		u, _ := NewUpdater[TestUser](ctx)
		u.And(func(sub *Updater[TestUser]) {
			sub.errs = append(sub.errs, fmt.Errorf("子块错误"))
		})
		if u.GetError() == nil {
			t.Error("And 子块错误应传播到外层")
		}
	})

	t.Run("Or 子块错误传播到外层", func(t *testing.T) {
		u, _ := NewUpdater[TestUser](ctx)
		u.Or(func(sub *Updater[TestUser]) {
			sub.errs = append(sub.errs, fmt.Errorf("子块错误"))
		})
		if u.GetError() == nil {
			t.Error("Or 子块错误应传播到外层")
		}
	})
}

// TestUpdater_OrWhereRaw_Empty 验证 OrWhereRaw 空 sql 写入 errs
func TestUpdater_OrWhereRaw_Empty(t *testing.T) {
	ctx := context.Background()
	u, _ := NewUpdater[TestUser](ctx)
	u.OrWhereRaw("")
	if u.GetError() == nil {
		t.Error("空 sql 应写入 errs")
	}
	if len(u.conditions) != 0 {
		t.Error("空 sql 不应添加条件")
	}
}
