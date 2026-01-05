package gplus

import (
	"context"
	"strings"
	"testing"
)

// TestQuery_ErrorAccumulation 测试错误累积机制
func TestQuery_ErrorAccumulation(t *testing.T) {
	ctx := context.Background()
	q, u := NewQuery[TestUser](ctx)

	// 模拟场景：
	// 1. 传入一个合法的字段指针
	// 2. 传入一个 nil (错误1)
	// 3. 传入一个外部变量指针 (错误2)
	externalVar := 100

	q.Eq(&u.Name, "test"). // 正常
				Eq(nil, "something"). // 错误 1
				Gt(&u.Age, 18).       // 正常
				Eq(&externalVar, 100) // 错误 2

	err := q.GetError()

	// 断言必须有错误
	assertError(t, err, true, "Should have accumulated errors")

	// 验证错误信息的完整性
	errMsg := err.Error()
	// 1. 验证是否包含 nil 指针引发的错误
	if !strings.Contains(errMsg, "gplus: argument must be a struct field pointer") {
		t.Errorf("Expected nil pointer error, got: %s", errMsg)
	}

	// 2. 验证是否包含地址解析失败引发的错误
	if !strings.Contains(errMsg, "gplus: column name not found for pointer") {
		t.Errorf("Expected column not found error, got: %s", errMsg)
	}

	// 3. 验证是否收集到了两个错误
	// errors.Join 生成的错误可以通过 errors.Unwrap 拆解，或者直接数换行符
	errCount := len(q.errs)
	if errCount != 2 {
		t.Errorf("Expected 2 errors in slice, got %d", errCount)
	}
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

	t.Run("异常更新流程-错误累积", func(t *testing.T) {
		badUpdater, bu := NewUpdater[TestUser](ctx)

		// 1. Set 错误字段
		// 2. Where 错误字段
		badUpdater.Set(nil, "Fail").Eq(&bu.Name, "Valid").Set(new(int), 1)

		err := badUpdater.GetError()
		assertError(t, err, true, "Should detect errors in Updater")

		if !strings.Contains(err.Error(), "set error") {
			t.Error("Should contain 'set error'")
		}
	})
}
