package gplus

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

// 本文件对应 spec docs/superpowers/specs/2026-06-10-firstorupdate-datarule-fix-design.md
// 的 AC-1 ~ AC-4（测试 1:1 对应 AC）。
// 复用 repo_datarule_byid_test.go 的 tenantUser / setupTenantDB / ctxWithTenantRule / insertTenantUsers。

// AC-1：u 侧 DataRule 与目标行租户不匹配时，UPDATE 被拦截（affected=0），行保持原值
func TestFirstOrUpdate_DataRule_update_blocked_when_updater_rule_mismatch(t *testing.T) {
	repo, db := setupTenantDB(t)
	_, bobID := insertTenantUsers(t, db) // Bob: TenantID=2

	q, m := NewQuery[tenantUser](context.Background()) // q 无规则，能查到 Bob
	q.Eq(&m.ID, bobID)
	u, um := NewUpdater[tenantUser](ctxWithTenantRule(1)) // u 规则 tenant_id=1，与 Bob 不匹配
	u.Set(&um.Name, "HACKED")

	data, created, err := repo.FirstOrUpdate(q, u, &tenantUser{Name: "fallback", TenantID: 2})
	if err != nil {
		t.Fatalf("期望 err=nil，实际: %v", err)
	}
	if created {
		t.Fatal("期望 created=false（记录已存在）")
	}
	var row tenantUser
	if e := db.First(&row, bobID).Error; e != nil {
		t.Fatalf("回读失败: %v", e)
	}
	if row.Name != "Bob" {
		t.Fatalf("期望 UPDATE 被 u 侧 DataRule 拦截、行名保持 Bob，实际: %q", row.Name)
	}
	if data.Name != "Bob" {
		t.Fatalf("期望返回未变更行（Name=Bob），实际: %q", data.Name)
	}
}

// AC-2：q/u 同租户规则、行在权限内 → 正常更新（回归保护）
func TestFirstOrUpdate_DataRule_same_tenant_updates_normally(t *testing.T) {
	repo, db := setupTenantDB(t)
	aliceID, _ := insertTenantUsers(t, db) // Alice: TenantID=1

	ctx := ctxWithTenantRule(1)
	q, m := NewQuery[tenantUser](ctx)
	q.Eq(&m.ID, aliceID)
	u, um := NewUpdater[tenantUser](ctx)
	u.Set(&um.Name, "Alice2")

	data, created, err := repo.FirstOrUpdate(q, u, &tenantUser{Name: "fallback", TenantID: 1})
	if err != nil {
		t.Fatalf("期望 err=nil，实际: %v", err)
	}
	if created {
		t.Fatal("期望 created=false（记录已存在）")
	}
	var row tenantUser
	if e := db.First(&row, aliceID).Error; e != nil {
		t.Fatalf("回读失败: %v", e)
	}
	if row.Name != "Alice2" {
		t.Fatalf("期望行被更新为 Alice2，实际: %q", row.Name)
	}
	if data.Name != "Alice2" {
		t.Fatalf("期望返回更新后行（Name=Alice2），实际: %q", data.Name)
	}
}

// AC-3：更新把行改出 DataRule 可见范围 → 重读查不到，整个事务回滚（D-1 语义）
func TestFirstOrUpdate_DataRule_rolls_back_when_update_moves_row_out_of_scope(t *testing.T) {
	repo, db := setupTenantDB(t)
	aliceID, _ := insertTenantUsers(t, db) // Alice: TenantID=1

	ctx := ctxWithTenantRule(1)
	q, m := NewQuery[tenantUser](ctx)
	q.Eq(&m.ID, aliceID)
	u, um := NewUpdater[tenantUser](ctx)
	u.Set(&um.TenantID, 2) // 把行改出 tenant_id=1 的权限范围

	_, created, err := repo.FirstOrUpdate(q, u, &tenantUser{Name: "fallback", TenantID: 1})
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("期望 gorm.ErrRecordNotFound（D-1 回滚语义），实际: %v", err)
	}
	if created {
		t.Fatal("期望 created=false")
	}
	var row tenantUser
	if e := db.First(&row, aliceID).Error; e != nil {
		t.Fatalf("回读失败: %v", e)
	}
	if row.TenantID != 1 {
		t.Fatalf("期望事务回滚后 TenantID 仍为 1，实际: %d", row.TenantID)
	}
}

// AC-4：u 侧 ctx 携带非法 DataRule（含括号列名）→ fail-fast，不发 UPDATE，DB 无变更
func TestFirstOrUpdate_DataRule_invalid_updater_rule_fails_fast(t *testing.T) {
	repo, db := setupTenantDB(t)
	aliceID, _ := insertTenantUsers(t, db) // Alice: TenantID=1

	q, m := NewQuery[tenantUser](context.Background()) // q 干净 ctx
	q.Eq(&m.ID, aliceID)
	badCtx := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Column: "dept(id)", Condition: "=", Value: "1"}, // 含括号，白名单拒绝
	})
	u, um := NewUpdater[tenantUser](badCtx)
	u.Set(&um.Name, "X")

	_, _, err := repo.FirstOrUpdate(q, u, &tenantUser{Name: "fallback", TenantID: 1})
	if err == nil {
		t.Fatal("期望非法 u 侧 DataRule 返回错误，实际 err=nil")
	}
	var row tenantUser
	if e := db.First(&row, aliceID).Error; e != nil {
		t.Fatalf("回读失败: %v", e)
	}
	if row.Name != "Alice" {
		t.Fatalf("期望 DB 无变更（Name=Alice），实际: %q", row.Name)
	}
}
