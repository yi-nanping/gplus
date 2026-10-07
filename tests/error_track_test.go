package gplus_test

import (
	"context"
	"errors"
	"testing"

	. "github.com/yi-nanping/gplus"
)

// AC-1：Query 本体错误（坏字段指针）→ BuildCount 路径短路，不执行 COUNT
func TestErrTrack_AC1_query_buildcount_shortcircuits_on_builder_error(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	db.Create(&TestUser{Name: "A", Age: 10})

	q, m := NewQuery[TestUser](context.Background())
	var orphan TestUser  // 非规范实例，字段地址未注册
	q.Eq(&orphan.Age, 1) // 错误累积，条件被丢弃
	q.Eq(&m.Age, 10)     // 有效条件

	var n int64
	result := db.Model(&TestUser{}).Scopes(q.BuildCount()).Count(&n)
	if result.Error == nil {
		t.Fatal("期望 BuildCount 在 builder 带错时短路返回错误，实际 Error=nil（条件被静默丢弃后照常计数）")
	}
}

// AC-2：Updater 本体错误 → BuildUpdate 路径短路（fail-open 实证：
// 调用方意图 WHERE age=10 AND name='B'（命中 0 行），坏指针条件被丢后实际 WHERE age=10 会误改 A 行）
func TestErrTrack_AC2_updater_buildupdate_shortcircuits_on_builder_error(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	db.Create(&TestUser{Name: "A", Age: 10, Score: 1.0})
	db.Create(&TestUser{Name: "B", Age: 20, Score: 2.0})

	u, m := NewUpdater[TestUser](context.Background())
	var orphan TestUser
	u.Set(&m.Score, 99.0)
	u.Eq(&m.Age, 10)        // 有效条件
	u.Eq(&orphan.Name, "B") // 坏指针，错误累积、条件被丢弃

	result := db.Model(&TestUser{}).Scopes(u.BuildUpdate()).Updates(u.UpdateMap())
	if result.Error == nil {
		t.Fatal("期望 BuildUpdate 在 builder 带错时短路返回错误，实际 Error=nil")
	}
	if result.RowsAffected != 0 {
		t.Fatalf("期望 affected=0，实际: %d", result.RowsAffected)
	}
	var rows []TestUser
	if e := db.Order("age").Find(&rows).Error; e != nil || len(rows) != 2 {
		t.Fatalf("回读失败: %v, rows=%d", e, len(rows))
	}
	if rows[0].Score != 1.0 || rows[1].Score != 2.0 {
		t.Fatalf("期望两行 Score 均不变（1.0/2.0），实际: %v/%v——改了调用方意图之外的行", rows[0].Score, rows[1].Score)
	}
}

// AC-3：Updater 链级错误（core.errs，白盒注入隔离桶）→ BuildUpdate 短路
func TestErrTrack_AC3_updater_buildupdate_shortcircuits_on_core_error(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	db.Create(&TestUser{Name: "A", Age: 10, Score: 1.0})

	u, m := NewUpdater[TestUser](context.Background())
	u.Set(&m.Score, 99.0)
	u.Eq(&m.Age, 10)
	o := As[Order](u, "o")
	u.Clear()
	u.Set(&m.Score, 99.0).Eq(&m.Age, 10)
	u.Eq(&o.UserID, 1) // 使用已撤销 alias，通过公开接口触发链级错误

	result := db.Model(&TestUser{}).Scopes(u.BuildUpdate()).Updates(u.UpdateMap())
	if !errors.Is(result.Error, ErrAliasRevoked) {
		t.Fatalf("期望 Error 含 ErrAliasRevoked，实际: %v", result.Error)
	}
	var row TestUser
	if e := db.Where("age = ?", 10).First(&row).Error; e != nil {
		t.Fatalf("回读失败: %v", e)
	}
	if row.Score != 1.0 {
		t.Fatalf("期望 Score 不变（1.0），实际: %v", row.Score)
	}
}

// AC-5：无错路径 SQL 字节级不变（重构回归锁）
func TestErrTrack_AC5_clean_query_sql_unchanged(t *testing.T) {
	db := newDryRunDB(t)
	q, m := NewQuery[TestUser](context.Background())
	q.Eq(&m.Age, 10).Order(&m.Age, true)

	sql, vars := buildSQL(t, db, q)
	// 表名反引号 + 列名双引号是 SQLite 驱动 DryRun 的实际输出格式（混合属正常），字节锁定防重构漂移
	want := "SELECT * FROM `test_users` WHERE \"age\" = ? ORDER BY \"age\" ASC"
	if sql != want {
		t.Fatalf("期望 SQL 字节不变:\n want: %s\n got:  %s", want, sql)
	}
	if len(vars) != 1 || vars[0] != 10 {
		t.Fatalf("期望 Vars=[10]，实际: %v", vars)
	}
}

// AC-7：Clear() 解除短路状态，复用后正常执行
func TestErrTrack_AC7_clear_resets_shortcircuit(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	db.Create(&TestUser{Name: "A", Age: 10})

	q, m := NewQuery[TestUser](context.Background())
	var orphan TestUser
	q.Eq(&orphan.Age, 1) // 带错
	q.Clear()
	q.Eq(&m.Age, 10) // 清空后重建有效条件

	var rows []TestUser
	result := db.Model(&TestUser{}).Scopes(q.BuildQuery()).Find(&rows)
	if result.Error != nil {
		t.Fatalf("期望 Clear 后正常执行，实际 Error: %v", result.Error)
	}
	if len(rows) != 1 || rows[0].Name != "A" {
		t.Fatalf("期望查到 1 行 A，实际: %d 行", len(rows))
	}
}
