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
