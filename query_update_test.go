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
