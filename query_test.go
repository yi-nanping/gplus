package gplus

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestQuery_InvalidColumnErrors 验证编程错误（字段指针传错）会累积到 errs
func TestQuery_InvalidColumnErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("nil 指针累积错误", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Eq(nil, "something")
		assertError(t, q.GetError(), true, "nil 指针应累积错误")
	})

	t.Run("外部变量指针累积错误", func(t *testing.T) {
		externalVar := 100
		q, _ := NewQuery[TestUser](ctx)
		q.Eq(&externalVar, 100)
		assertError(t, q.GetError(), true, "外部变量指针应累积错误")
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

	assertError(t, q.GetError(), false, "Nested query should be valid")

	if len(q.conditions) != 2 {
		t.Fatalf("Expected 2 top-level conditions, got %d", len(q.conditions))
	}

	groupCond := q.conditions[1]
	if len(groupCond.group) != 2 {
		t.Errorf("Expected 2 sub-conditions in group, got %d", len(groupCond.group))
	}
}

// TestQuery_Or 测试 Or 开启带括号的 OR 嵌套块
func TestQuery_Or(t *testing.T) {
	ctx := context.Background()

	t.Run("基本 OR 嵌套块", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").Or(func(sub *Query[TestUser]) {
			sub.Gt(&u.Age, 18).Gt(&u.Score, 90)
		})

		assertError(t, q.GetError(), false, "Or 嵌套块不应有错误")
		if len(q.conditions) != 2 {
			t.Fatalf("期望 2 个顶层条件，实际 %d", len(q.conditions))
		}
		if !q.conditions[1].isOr {
			t.Error("Or 嵌套块的 isOr 应为 true")
		}
		if len(q.conditions[1].group) != 2 {
			t.Errorf("Or 块内期望 2 个子条件，实际 %d", len(q.conditions[1].group))
		}
	})

	t.Run("OR 块为空时不追加条件", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").Or(func(sub *Query[TestUser]) {})

		assertError(t, q.GetError(), false, "空 Or 块不应有错误")
		if len(q.conditions) != 1 {
			t.Errorf("空 Or 块不应追加条件，期望 1 个顶层条件，实际 %d", len(q.conditions))
		}
	})

	t.Run("nil fn 写入 errs", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Or(nil)
		assertError(t, q.GetError(), true, "Or(nil) 应写入 errs")
		if len(q.conditions) != 0 {
			t.Errorf("Or(nil) 不应追加条件，实际 %d", len(q.conditions))
		}
	})

	t.Run("多个 OR 嵌套块", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").
			Or(func(sub *Query[TestUser]) { sub.Lt(&u.Age, 10) }).
			Or(func(sub *Query[TestUser]) { sub.Gt(&u.Score, 99) })

		assertError(t, q.GetError(), false, "多个 Or 块不应有错误")
		if len(q.conditions) != 3 {
			t.Fatalf("期望 3 个顶层条件，实际 %d", len(q.conditions))
		}
		for i := 1; i < 3; i++ {
			if !q.conditions[i].isOr {
				t.Errorf("条件 %d 的 isOr 应为 true", i)
			}
		}
	})

	t.Run("OR 块内支持多种条件类型", func(t *testing.T) {
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
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").Or(func(sub *Query[TestUser]) {
			sub.applyDataRule(DataRule{Column: "age", Condition: "INVALID_OP", Value: "1"})
		})

		assertError(t, q.GetError(), true, "Or 块内的错误应传播到外层")
	})
}

// TestQuery_And 测试 And 嵌套块
func TestQuery_And(t *testing.T) {
	ctx := context.Background()

	t.Run("基本 AND 嵌套块", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").And(func(sub *Query[TestUser]) {
			sub.Gt(&u.Age, 18).Gt(&u.Score, 90)
		})

		assertError(t, q.GetError(), false, "And 嵌套块不应有错误")
		if len(q.conditions) != 2 {
			t.Fatalf("期望 2 个顶层条件，实际 %d", len(q.conditions))
		}
		if q.conditions[1].isOr {
			t.Error("And 嵌套块的 isOr 应为 false")
		}
		if len(q.conditions[1].group) != 2 {
			t.Errorf("And 块内期望 2 个子条件，实际 %d", len(q.conditions[1].group))
		}
	})

	t.Run("AND 块为空时不追加条件", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").And(func(sub *Query[TestUser]) {})

		if len(q.conditions) != 1 {
			t.Errorf("空 And 块不应追加条件，期望 1，实际 %d", len(q.conditions))
		}
	})

	t.Run("nil fn 写入 errs", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.And(nil)
		assertError(t, q.GetError(), true, "And(nil) 应写入 errs")
		if len(q.conditions) != 0 {
			t.Errorf("And(nil) 不应追加条件，实际 %d", len(q.conditions))
		}
	})

	t.Run("AND 块内错误应传播到外层", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").And(func(sub *Query[TestUser]) {
			sub.applyDataRule(DataRule{Column: "age", Condition: "INVALID_OP", Value: "1"})
		})

		assertError(t, q.GetError(), true, "And 块内的错误应传播到外层")
	})
}

// TestQuery_Having 测试 Having 条件
func TestQuery_Having(t *testing.T) {
	ctx := context.Background()

	t.Run("Having 正常添加", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Having("age", OpGt, 18)

		if len(q.havings) != 1 {
			t.Fatalf("期望 1 个 having 条件，实际 %d", len(q.havings))
		}
		if q.havings[0].isOr {
			t.Error("Having 的 isOr 应为 false")
		}
	})

	t.Run("OrHaving 正常添加", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.OrHaving("age", OpGt, 18)

		if len(q.havings) != 1 {
			t.Fatalf("期望 1 个 having 条件，实际 %d", len(q.havings))
		}
		if !q.havings[0].isOr {
			t.Error("OrHaving 的 isOr 应为 true")
		}
	})

	t.Run("Having 空 col 写入 errs", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Having("", OpGt, 18)
		assertError(t, q.GetError(), true, "Having 空 col 应写入 errs")
		if len(q.havings) != 0 {
			t.Errorf("Having 空 col 不应追加条件，实际 %d", len(q.havings))
		}
	})

	t.Run("Having 空 op 写入 errs", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Having("age", "", 18)
		assertError(t, q.GetError(), true, "Having 空 op 应写入 errs")
	})

	t.Run("OrHaving 空 col 写入 errs", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.OrHaving("", OpGt, 18)
		assertError(t, q.GetError(), true, "OrHaving 空 col 应写入 errs")
		if len(q.havings) != 0 {
			t.Errorf("OrHaving 空 col 不应追加条件，实际 %d", len(q.havings))
		}
	})
}

// TestQuery_DataRuleBuilder 测试 DataRuleBuilder 应用与幂等性
func TestQuery_DataRuleBuilder(t *testing.T) {
	t.Run("无 ctx 时不追加条件", func(t *testing.T) {
		q := &Query[TestUser]{
			ScopeBuilder: ScopeBuilder{conditions: make([]condition, 0)},
		}
		q.DataRuleBuilder()
		if len(q.conditions) != 0 {
			t.Errorf("无 ctx 时不应追加任何条件，实际 %d", len(q.conditions))
		}
	})

	t.Run("DataRuleBuilder 幂等：多次调用只应用一次", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{
			{Column: "dept_id", Condition: "=", Value: "42"},
		})
		q, _ := NewQuery[TestUser](ctx)
		q.DataRuleBuilder()
		q.DataRuleBuilder()
		q.DataRuleBuilder()
		if len(q.conditions) != 1 {
			t.Errorf("DataRuleBuilder 应幂等，期望 1 个条件，实际 %d", len(q.conditions))
		}
	})
}

// TestQuery_ApplyDataRule_InvalidColumn 验证非法列名写入 errs
func TestQuery_ApplyDataRule_InvalidColumn(t *testing.T) {
	ctx := context.Background()

	t.Run("含括号的列名应写入 errs", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.applyDataRule(DataRule{Column: "age+(SELECT 1)", Condition: "=", Value: "1"})
		assertError(t, q.GetError(), true, "非法列名应写入 errs")
		if len(q.conditions) != 0 {
			t.Errorf("非法列名不应追加条件，实际 %d", len(q.conditions))
		}
	})

	t.Run("含空格的列名应写入 errs", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.applyDataRule(DataRule{Column: "dept id", Condition: "=", Value: "1"})
		assertError(t, q.GetError(), true, "含空格列名应写入 errs")
	})

	t.Run("合法列名 table.col 应正常追加条件", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.applyDataRule(DataRule{Column: "u.dept_id", Condition: "=", Value: "42"})
		assertError(t, q.GetError(), false, "合法 table.col 不应有错误")
		if len(q.conditions) != 1 {
			t.Errorf("期望 1 个条件，实际 %d", len(q.conditions))
		}
	})
}

// TestQuery_Select_InvalidPointer 验证 Select 传入非法指针写入 errs
func TestQuery_Select_InvalidPointer(t *testing.T) {
	ctx := context.Background()

	t.Run("外部变量指针写入 errs", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		external := 0
		q.Select(&external)
		assertError(t, q.GetError(), true, "外部变量指针应写入 errs")
		if len(q.selects) != 0 {
			t.Errorf("非法列不应追加到 selects，实际 %d", len(q.selects))
		}
	})

	t.Run("合法字段正常追加", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Select(&u.Name, &u.Age)
		assertError(t, q.GetError(), false, "合法字段不应有错误")
		if len(q.selects) != 2 {
			t.Errorf("期望 2 个 select 字段，实际 %d", len(q.selects))
		}
	})
}

// TestQuery_Omit_InvalidPointer 验证 Omit 非法指针写入 errs
func TestQuery_Omit_InvalidPointer(t *testing.T) {
	ctx := context.Background()

	t.Run("外部变量指针写入 errs", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		external := 0
		q.Omit(&external)
		assertError(t, q.GetError(), true, "外部变量指针应写入 errs")
		if len(q.omits) != 0 {
			t.Errorf("非法指针不应追加 omits，实际 %d", len(q.omits))
		}
	})

	t.Run("合法字段正常追加", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Omit(&u.Name)
		assertError(t, q.GetError(), false, "合法字段不应有错误")
		if len(q.omits) != 1 {
			t.Errorf("期望 1 个 omit，实际 %d", len(q.omits))
		}
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

// TestQuery_Clear 测试 Clear 重置状态
func TestQuery_Clear(t *testing.T) {
	ctx := context.Background()

	t.Run("Clear 后 conditions 和 orders 应清空", func(t *testing.T) {
		q, model := NewQuery[TestUser](ctx)
		q.Eq(&model.Age, 18).Order(&model.Name, true)
		q.Clear()
		if len(q.conditions) != 0 {
			t.Error("Clear 后 conditions 应为空")
		}
		if len(q.orders) != 0 {
			t.Error("Clear 后 orders 应为空")
		}
	})

	t.Run("Clear 后 errs 应清空", func(t *testing.T) {
		q2, _ := NewQuery[TestUser](ctx)
		q2.Eq(nil, "bad") // 触发错误
		if q2.GetError() == nil {
			t.Fatal("期望有错误")
		}
		q2.Clear()
		if q2.GetError() != nil {
			t.Error("Clear 后 errs 应为空")
		}
	})

	t.Run("Clear 后 dataRuleApplied 应重置", func(t *testing.T) {
		q3, _ := NewQuery[TestUser](ctx)
		q3.DataRuleBuilder() // 标记为已应用
		if !q3.dataRuleApplied {
			t.Fatal("期望 dataRuleApplied=true")
		}
		q3.Clear()
		if q3.dataRuleApplied {
			t.Error("Clear 后 dataRuleApplied 应为 false")
		}
	})
}
