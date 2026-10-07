package gplus

import (
	"context"
	"errors"
	"testing"
)

func TestErrTrack_DataRulePreservesExistingDBError(t *testing.T) {
	db := newDryRunDB(t)
	want := errors.New("existing database error")
	db.AddError(want)
	ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Column: "age", Condition: "IN", Values: []string{"18", "20"}},
	})
	q, m := NewQuery[TestUser](ctx)
	q.Eq(&m.Name, "Alice").DataRuleBuilder()
	var rows []TestUser
	result := db.Model(new(TestUser)).Scopes(q.BuildQuery()).Find(&rows)
	if !errors.Is(result.Error, want) || result.RowsAffected != 0 || result.Statement.SQL.Len() != 0 {
		t.Fatalf("原有数据库错误应阻止构建与执行 SQL: err=%v, affected=%d, SQL=%s", result.Error, result.RowsAffected, result.Statement.SQL.String())
	}
}

// AC-3：Updater 链级错误（core.errs，白盒注入隔离桶）→ BuildUpdate 短路
func TestErrTrack_AC3_updater_buildupdate_shortcircuits_on_core_error(t *testing.T) {
	db := newDryRunDB(t)

	u, m := NewUpdater[TestUser](context.Background())
	u.Set(&m.Score, 99.0)
	u.Eq(&m.Age, 10)
	u.core.appendErr(ErrAliasRevoked) // 白盒注入：仅链级桶有错，本体 errs 保持空

	result := db.Model(&TestUser{}).Scopes(u.BuildUpdate()).Updates(u.setMap)
	if !errors.Is(result.Error, ErrAliasRevoked) {
		t.Fatalf("期望 Error 含 ErrAliasRevoked，实际: %v", result.Error)
	}
	if result.RowsAffected != 0 || result.Statement.SQL.Len() != 0 {
		t.Fatalf("short-circuit: affected=%d SQL=%s", result.RowsAffected, result.Statement.SQL.String())
	}
}

// AC-6：Updater.Exists(nil) 错误写本体 errs 桶（对齐 Query.appendExists 与双侧 InSub）
func TestErrTrack_AC6_updater_exists_nil_writes_builder_errs(t *testing.T) {
	u, _ := NewUpdater[TestUser](context.Background())
	u.Exists(nil)

	if len(u.errs) != 1 || !errors.Is(u.errs[0], ErrSubqueryNil) {
		t.Fatalf("期望错误写入本体 u.errs（1 条 ErrSubqueryNil），实际 u.errs=%v", u.errs)
	}
	if len(u.core.errs) != 0 {
		t.Fatalf("期望 core.errs 为空（链级桶不收本体错误），实际: %v", u.core.errs)
	}
	if u.GetError() == nil {
		t.Fatal("期望 GetError() 非 nil（对外可见性不变）")
	}
}
