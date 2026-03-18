package gplus

import (
	"context"
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

// TestQuery_NestedLogic 测试 And/Or 嵌套逻辑
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
