package gplus

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestQuery_InvalidColumnPanics 验证编程错误（字段指针传错）会触发 panic
func TestQuery_InvalidColumnPanics(t *testing.T) {
	ctx := context.Background()

	t.Run("nil 指针触发 panic", func(t *testing.T) {
		assertPanics(t, func() {
			q, _ := NewQuery[TestUser](ctx)
			q.Eq(nil, "something")
		}, "nil 指针应触发 panic")
	})

	t.Run("外部变量指针触发 panic", func(t *testing.T) {
		assertPanics(t, func() {
			externalVar := 100
			q, _ := NewQuery[TestUser](ctx)
			q.Eq(&externalVar, 100)
		}, "外部变量指针应触发 panic")
	})

	t.Run("合法字段无错误", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "test").Gt(&u.Age, 18)
		assertError(t, q.GetError(), false, "合法字段不应有错误")
	})
}

// TestQuery_NestedLogic 测试 And 嵌套逻辑
func TestQuery_NestedLogic(t *testing.T) {
	ctx := context.Background()
	q, u := NewQuery[TestUser](ctx)

	// 构建复杂的嵌套查询
	// WHERE status = 1 AND (age > 18 OR score > 90)
	q.Eq(&u.IsActive, true).And(func(sub *Query[TestUser]) {
		sub.Gt(&u.Age, 18).OrGt(&u.Score, 90)
	})

	// 检查是否有构建错误
	assertError(t, q.GetError(), false, "Nested query should be valid")

	// 验证内部结构 (白盒测试)
	if len(q.conditions) != 2 {
		t.Fatalf("Expected 2 top-level conditions, got %d", len(q.conditions))
	}

	// 验证第二个条件是一个 Group
	groupCond := q.conditions[1]
	if len(groupCond.group) != 2 {
		t.Errorf("Expected 2 sub-conditions in group, got %d", len(groupCond.group))
	}
}

// TestQuery_Or 测试 Or 开启带括号的 OR 嵌套块
func TestQuery_Or(t *testing.T) {
	ctx := context.Background()

	t.Run("基本 OR 嵌套块", func(t *testing.T) {
		// WHERE name = 'alice' OR (age > 18 AND score > 90)
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").Or(func(sub *Query[TestUser]) {
			sub.Gt(&u.Age, 18).Gt(&u.Score, 90)
		})

		assertError(t, q.GetError(), false, "Or 嵌套块不应有错误")

		if len(q.conditions) != 2 {
			t.Fatalf("期望 2 个顶层条件，实际 %d", len(q.conditions))
		}
		group := q.conditions[1]
		if !group.isOr {
			t.Error("第二个条件应为 OR 类型")
		}
		if len(group.group) != 2 {
			t.Errorf("OR 块内期望 2 个子条件，实际 %d", len(group.group))
		}
	})

	t.Run("OR 块为空时不追加条件", func(t *testing.T) {
		// fn 内不加任何条件，空块应被忽略
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").Or(func(sub *Query[TestUser]) {
			// 故意不添加任何条件
		})

		assertError(t, q.GetError(), false, "空 Or 块不应有错误")
		if len(q.conditions) != 1 {
			t.Errorf("空 Or 块不应追加条件，期望 1 个顶层条件，实际 %d", len(q.conditions))
		}
	})

	t.Run("nil fn 触发 panic", func(t *testing.T) {
		assertPanics(t, func() {
			q, _ := NewQuery[TestUser](ctx)
			q.Or(nil)
		}, "Or(nil) 应触发 panic")
	})

	t.Run("多个 OR 嵌套块", func(t *testing.T) {
		// WHERE name = 'alice' OR (age < 10) OR (score > 99)
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").
			Or(func(sub *Query[TestUser]) {
				sub.Lt(&u.Age, 10)
			}).
			Or(func(sub *Query[TestUser]) {
				sub.Gt(&u.Score, 99)
			})

		assertError(t, q.GetError(), false, "多个 Or 块不应有错误")
		if len(q.conditions) != 3 {
			t.Fatalf("期望 3 个顶层条件，实际 %d", len(q.conditions))
		}
		for i := 1; i <= 2; i++ {
			if !q.conditions[i].isOr {
				t.Errorf("conditions[%d] 应为 OR 类型", i)
			}
			if len(q.conditions[i].group) != 1 {
				t.Errorf("conditions[%d] 期望 1 个子条件，实际 %d", i, len(q.conditions[i].group))
			}
		}
	})

	t.Run("OR 块内支持多种条件类型", func(t *testing.T) {
		// WHERE is_active = true OR (age BETWEEN 18 AND 30 AND email IS NOT NULL)
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.IsActive, true).Or(func(sub *Query[TestUser]) {
			sub.Between(&u.Age, 18, 30).IsNotNull(&u.Email)
		})

		assertError(t, q.GetError(), false, "Or 块内混合条件不应有错误")
		if len(q.conditions) != 2 {
			t.Fatalf("期望 2 个顶层条件，实际 %d", len(q.conditions))
		}
		if len(q.conditions[1].group) != 2 {
			t.Errorf("OR 块内期望 2 个子条件，实际 %d", len(q.conditions[1].group))
		}
	})

	t.Run("OR 块内错误应传播到外层", func(t *testing.T) {
		// 子块内通过 applyDataRule 产生错误（不支持的 condition 类型），应传播到外层 q.errs
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").Or(func(sub *Query[TestUser]) {
			sub.applyDataRule(DataRule{Column: "age", Condition: "INVALID_OP", Value: "1"})
		})

		assertError(t, q.GetError(), true, "Or 块内的错误应传播到外层")
	})
}

// TestQuery_Helpers 测试 Query 辅助方法
func TestQuery_Helpers(t *testing.T) {
	ctx := context.Background()

	t.Run("IsEmpty 空查询", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		if !q.IsEmpty() {
			t.Error("新建 Query 应为空")
		}
	})

	t.Run("IsEmpty 非空查询", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice")
		if q.IsEmpty() {
			t.Error("添加条件后 Query 不应为空")
		}
	})

	t.Run("IsUnscoped 默认 false", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		if q.IsUnscoped() {
			t.Error("默认 Query 不应为 unscoped")
		}
	})

	t.Run("Unscoped 设置后为 true", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Unscoped()
		if !q.IsUnscoped() {
			t.Error("调用 Unscoped() 后应为 true")
		}
	})

	t.Run("Table 动态表名", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Table("test_users_2024")
		if q.tableName != "test_users_2024" {
			t.Errorf("Table 设置失败，期望 test_users_2024，实际 %s", q.tableName)
		}
	})

	t.Run("Page 正常分页", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Page(2, 20)
		if q.limit != 20 {
			t.Errorf("limit 期望 20，实际 %d", q.limit)
		}
		if q.offset != 20 {
			t.Errorf("offset 期望 20，实际 %d", q.offset)
		}
	})

	t.Run("Page 边界值默认为第1页size10", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Page(0, 0)
		if q.limit != 10 {
			t.Errorf("limit 期望 10，实际 %d", q.limit)
		}
		if q.offset != 0 {
			t.Errorf("offset 期望 0，实际 %d", q.offset)
		}
	})

	t.Run("Context 返回传入的 ctx", func(t *testing.T) {
		type ctxKey struct{}
		ctx2 := context.WithValue(ctx, ctxKey{}, "val")
		q, _ := NewQuery[TestUser](ctx2)
		if q.Context() != ctx2 {
			t.Error("Context() 应返回传入的 ctx")
		}
	})

	t.Run("Context nil ctx 返回 Background", func(t *testing.T) {
		q := &Query[TestUser]{}
		if q.Context() == nil {
			t.Error("nil ctx 时 Context() 不应返回 nil")
		}
	})

	t.Run("GetError 单个错误用 error 单数", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.errs = append(q.errs, fmt.Errorf("单个错误"))
		err := q.GetError()
		if err == nil {
			t.Fatal("期望有错误")
		}
		if !strings.Contains(err.Error(), "1 error") {
			t.Errorf("期望包含 '1 error'，实际: %s", err.Error())
		}
	})

	t.Run("GetError 多个错误用 errors 复数", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.errs = append(q.errs, fmt.Errorf("错误1"), fmt.Errorf("错误2"))
		err := q.GetError()
		if err == nil {
			t.Fatal("期望有错误")
		}
		if !strings.Contains(err.Error(), "2 errors") {
			t.Errorf("期望包含 '2 errors'，实际: %s", err.Error())
		}
	})
}

// TestQuery_And 测试 And 嵌套块
func TestQuery_And(t *testing.T) {
	ctx := context.Background()

	t.Run("基本 AND 嵌套块", func(t *testing.T) {
		// WHERE name = 'alice' AND (age > 18 AND score > 90)
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").And(func(sub *Query[TestUser]) {
			sub.Gt(&u.Age, 18).Gt(&u.Score, 90)
		})

		assertError(t, q.GetError(), false, "And 嵌套块不应有错误")
		if len(q.conditions) != 2 {
			t.Fatalf("期望 2 个顶层条件，实际 %d", len(q.conditions))
		}
		grp := q.conditions[1]
		if grp.isOr {
			t.Error("And 块的 isOr 应为 false")
		}
		if len(grp.group) != 2 {
			t.Errorf("And 块内期望 2 个子条件，实际 %d", len(grp.group))
		}
	})

	t.Run("AND 块为空时不追加条件", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").And(func(sub *Query[TestUser]) {})
		if len(q.conditions) != 1 {
			t.Errorf("空 And 块不应追加条件，期望 1，实际 %d", len(q.conditions))
		}
	})

	t.Run("nil fn 触发 panic", func(t *testing.T) {
		assertPanics(t, func() {
			q, _ := NewQuery[TestUser](ctx)
			q.And(nil)
		}, "And(nil) 应触发 panic")
	})

	t.Run("AND 块内错误应传播到外层", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").And(func(sub *Query[TestUser]) {
			sub.applyDataRule(DataRule{Column: "age", Condition: "INVALID_OP", Value: "1"})
		})
		assertError(t, q.GetError(), true, "And 块内的错误应传播到外层")
	})
}

// TestQuery_Having 测试 Having/OrHaving 条件
func TestQuery_Having(t *testing.T) {
	ctx := context.Background()

	t.Run("Having 正常添加", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Having("COUNT(id)", OpGt, 10)
		if len(q.havings) != 1 {
			t.Fatalf("期望 1 个 having 条件，实际 %d", len(q.havings))
		}
		h := q.havings[0]
		if h.expr != "COUNT(id)" || h.operator != OpGt || h.value != 10 || h.isOr {
			t.Errorf("Having 条件字段不符: %+v", h)
		}
	})

	t.Run("OrHaving 正常添加", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.OrHaving("SUM(score)", OpGe, 100)
		if len(q.havings) != 1 {
			t.Fatalf("期望 1 个 having 条件，实际 %d", len(q.havings))
		}
		if !q.havings[0].isOr {
			t.Error("OrHaving 的 isOr 应为 true")
		}
	})

	t.Run("Having 空 col 触发 panic", func(t *testing.T) {
		assertPanics(t, func() {
			q, _ := NewQuery[TestUser](ctx)
			q.Having("", OpGt, 1)
		}, "Having 空 col 应触发 panic")
	})

	t.Run("Having 空 op 触发 panic", func(t *testing.T) {
		assertPanics(t, func() {
			q, _ := NewQuery[TestUser](ctx)
			q.Having("COUNT(id)", "", 1)
		}, "Having 空 op 应触发 panic")
	})

	t.Run("OrHaving 空 col 触发 panic", func(t *testing.T) {
		assertPanics(t, func() {
			q, _ := NewQuery[TestUser](ctx)
			q.OrHaving("", OpGt, 1)
		}, "OrHaving 空 col 应触发 panic")
	})
}

// TestQuery_DataRuleBuilder 测试 DataRuleBuilder 幂等性
func TestQuery_DataRuleBuilder(t *testing.T) {
	t.Run("无 ctx 时不追加条件", func(t *testing.T) {
		q := &Query[TestUser]{
			ScopeBuilder: ScopeBuilder{conditions: make([]condition, 0, 8)},
		}
		q.DataRuleBuilder()
		if len(q.conditions) != 0 {
			t.Error("无 ctx 时不应追加条件")
		}
	})

	t.Run("多次调用只应用一次", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{
			{Column: "dept_id", Condition: "=", Value: "1"},
		})
		q, _ := NewQuery[TestUser](ctx)
		q.DataRuleBuilder()
		q.DataRuleBuilder()
		count := 0
		for _, c := range q.conditions {
			if c.expr == "dept_id" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("DataRule 应只追加一次，实际追加 %d 次", count)
		}
	})
}

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

	t.Run("Set 错误字段触发 panic", func(t *testing.T) {
		assertPanics(t, func() {
			badUpdater, _ := NewUpdater[TestUser](ctx)
			badUpdater.Set(nil, "Fail")
		}, "Set nil 应触发 panic")
	})
}

// TestUpdater_Helpers 测试 Updater 辅助方法
func TestUpdater_Helpers(t *testing.T) {
	ctx := context.Background()

	t.Run("IsEmpty 无 Set 时为 true", func(t *testing.T) {
		u, _ := NewUpdater[TestUser](ctx)
		if !u.IsEmpty() {
			t.Error("未 Set 任何字段时应为 IsEmpty")
		}
	})

	t.Run("IsEmpty Set 后为 false", func(t *testing.T) {
		upd, m := NewUpdater[TestUser](ctx)
		upd.Set(&m.Name, "alice")
		if upd.IsEmpty() {
			t.Error("Set 后不应为 IsEmpty")
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
