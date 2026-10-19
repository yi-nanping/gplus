package gplus_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
)

func TestTypedQuerySelectAs_JoinProjectionContextAndCount(t *testing.T) {
	repo, db := setupTypedBuilderDB(t)
	teams := []typedBuilderTeam{{Label: "engineering"}, {Label: "sales"}}
	if err := db.Create(&teams).Error; err != nil {
		t.Fatalf("写入团队失败: %v", err)
	}
	accounts := []typedBuilderAccount{
		{TenantID: 1, TeamID: teams[0].ID, Name: "alice", Score: 10, Version: 1},
		{TenantID: 1, TeamID: teams[1].ID, Name: "bob", Score: 20, Version: 1},
		{TenantID: 2, TeamID: teams[1].ID, Name: "mallory", Score: 30, Version: 1},
	}
	if err := db.Create(&accounts).Error; err != nil {
		t.Fatalf("写入投影账号失败: %v", err)
	}

	type contextKey string
	const markerKey contextKey = "typed-builder-marker"
	ctx := context.WithValue(context.Background(), markerKey, "request-42")
	ctx = context.WithValue(ctx, DataRuleKey, []DataRule{
		{Table: "acct", Column: "tenant_key", Condition: "=", Value: "1"},
	})
	var callbackCalls atomic.Int32
	var wrongContext atomic.Bool
	callbackName := fmt.Sprintf("typed_builder:query:%p", &callbackCalls)
	if err := db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != (typedBuilderAccount{}).TableName() {
			return
		}
		callbackCalls.Add(1)
		if tx.Statement.Context.Value(markerKey) != "request-42" {
			wrongContext.Store(true)
		}
	}); err != nil {
		t.Fatalf("注册 Query callback 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	q, account := NewQueryAs[typedBuilderAccount](ctx, "acct")
	team := As[typedBuilderTeam](q, "team")
	q.LeftJoinAs(team, &account.TeamID, &team.ID, "").
		Select(&account.ID, &account.TenantID).
		SelectAs(&account.Name, "order").
		SelectAs(&team.Label, "custom_team_label").
		SelectExpr(Add(Col(&account.Score), Lit(1))).
		Order(&account.ID, true)
	type projection struct {
		ID        int64  `gorm:"column:account_key"`
		TenantID  int    `gorm:"column:tenant_key"`
		Order     string `gorm:"column:order"`
		TeamLabel string `gorm:"column:custom_team_label"`
	}
	var rows []projection
	if err := FindAs(repo, q, &rows); err != nil {
		t.Fatalf("FindAs SelectAs 投影失败: %v", err)
	}
	if len(rows) != 2 || rows[0] != (projection{ID: accounts[0].ID, TenantID: 1, Order: "alice", TeamLabel: "engineering"}) ||
		rows[1] != (projection{ID: accounts[1].ID, TenantID: 1, Order: "bob", TeamLabel: "sales"}) {
		t.Fatalf("Select/SelectAs/SelectExpr 混合投影结果=%+v", rows)
	}
	total, err := repo.Count(q)
	if err != nil || total != 2 {
		t.Fatalf("带 SelectAs 的 Count=%d err=%v，期望 2/nil", total, err)
	}
	if wrongContext.Load() {
		t.Fatal("FindAs/Count 的 Query callback 未收到 Query context")
	}
	if calls := callbackCalls.Load(); calls != 2 {
		t.Fatalf("Query callback 次数=%d，期望 FindAs 与 Count 各一次", calls)
	}
}

func TestTypedQueryEqColumn_CorrelatedNotExistsAliasesAndSoftDelete(t *testing.T) {
	repo, db := setupTypedBuilderDB(t)
	accounts := []typedBuilderAccount{
		{TenantID: 1, Name: "active-child", Version: 1},
		{TenantID: 1, Name: "soft-child", Version: 1},
		{TenantID: 1, Name: "no-child", Version: 1},
	}
	if err := db.Create(&accounts).Error; err != nil {
		t.Fatalf("写入相关子查询账号失败: %v", err)
	}
	events := []typedBuilderEvent{
		{AccountID: accounts[0].ID},
		{AccountID: accounts[1].ID, DeletedAt: gorm.DeletedAt{Time: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), Valid: true}},
	}
	if err := db.Create(&events).Error; err != nil {
		t.Fatalf("写入相关事件失败: %v", err)
	}

	assertOrphans := func(t *testing.T, q *Query[typedBuilderAccount]) {
		t.Helper()
		rows, err := repo.List(q)
		if err != nil {
			t.Fatalf("执行相关 NOT EXISTS 失败: %v", err)
		}
		if len(rows) != 2 || rows[0].Name != "soft-child" || rows[1].Name != "no-child" {
			t.Fatalf("相关 NOT EXISTS 结果=%+v，期望软删除关系与无关系账号", rows)
		}
	}

	t.Run("Unnamed", func(t *testing.T) {
		q, account := NewQuery[typedBuilderAccount](context.Background())
		sub, event := SubQuery[typedBuilderEvent](q)
		sub.EqColumn(&event.AccountID, &account.ID)
		q.NotExists(sub).Order(&account.ID, true)
		assertOrphans(t, q)
	})

	t.Run("Named", func(t *testing.T) {
		q, account := NewQueryAs[typedBuilderAccount](context.Background(), "acct")
		sub, event := SubQueryAs[typedBuilderEvent](q, "evt")
		sub.EqColumn(&event.AccountID, &account.ID)
		q.NotExists(sub).Order(&account.ID, true)
		assertOrphans(t, q)
	})

	t.Run("SelfCorrelatedWithDistinctAliases", func(t *testing.T) {
		if err := db.Model(&typedBuilderAccount{}).
			Where("account_key = ?", accounts[1].ID).
			Update("team_ref", accounts[0].ID).Error; err != nil {
			t.Fatalf("建立同模型自关联失败: %v", err)
		}

		q, parent := NewQueryAs[typedBuilderAccount](context.Background(), "parent")
		sub, child := SubQueryAs[typedBuilderAccount](q, "child")
		sub.EqColumn(&child.TeamID, &parent.ID)
		q.NotExists(sub).Order(&parent.ID, true)
		rows, err := repo.List(q)
		if err != nil {
			t.Fatalf("执行同模型相关 NOT EXISTS 失败: %v", err)
		}
		if len(rows) != 2 || rows[0].Name != "soft-child" || rows[1].Name != "no-child" {
			t.Fatalf("同模型相关 NOT EXISTS 结果=%+v，期望排除存在 child 的 active-child", rows)
		}
	})
}

func TestTypedBuilder_InvalidFieldsFailBeforeReadOrWrite(t *testing.T) {
	repo, db := setupTypedBuilderDB(t)
	inside, _ := seedTypedBuilderAccounts(t, db)
	var queryCalls atomic.Int32
	var updateCalls atomic.Int32
	queryCallback := fmt.Sprintf("typed_builder:invalid-query:%p", &queryCalls)
	updateCallback := fmt.Sprintf("typed_builder:invalid-update:%p", &updateCalls)
	if err := db.Callback().Query().Before("gorm:query").Register(queryCallback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == inside.TableName() {
			queryCalls.Add(1)
		}
	}); err != nil {
		t.Fatalf("注册非法读取 callback 失败: %v", err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register(updateCallback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == inside.TableName() {
			updateCalls.Add(1)
		}
	}); err != nil {
		t.Fatalf("注册非法更新 callback 失败: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(queryCallback)
		_ = db.Callback().Update().Remove(updateCallback)
	})

	u, updateModel := NewUpdater[typedBuilderAccount](context.Background())
	u.Incr("score_points", 99).Eq(&updateModel.ID, inside.ID)
	if affected, err := repo.UpdateByCond(u); err == nil || affected != 0 {
		t.Fatalf("Incr 字符串字段 affected=%d err=%v，期望构建错误且不执行", affected, err)
	}

	qSelect, _ := NewQuery[typedBuilderAccount](context.Background())
	qSelect.SelectAs("display_name", "name")
	var selected []struct{ Name string }
	if err := FindAs(repo, qSelect, &selected); err == nil {
		t.Fatal("SelectAs 字符串字段应在 FindAs 入口返回错误")
	}

	qColumn, model := NewQuery[typedBuilderAccount](context.Background())
	foreign := typedBuilderAccount{}
	qColumn.EqColumn(&model.ID, &foreign.ID)
	if _, err := repo.List(qColumn); err == nil {
		t.Fatal("EqColumn 未注册字段应在 List 入口返回错误")
	}
	if queryCalls.Load() != 0 || updateCalls.Load() != 0 {
		t.Fatalf("非法字段不得发出 SQL，query callbacks=%d update callbacks=%d", queryCalls.Load(), updateCalls.Load())
	}
	if got := getTypedBuilderAccount(t, repo, inside.ID); got.Score != 10 {
		t.Fatalf("非法 Incr 后 score=%d，期望保持 10", got.Score)
	}
}
