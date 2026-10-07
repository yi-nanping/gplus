package gplus_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
)

type joinContractUser struct {
	ID       int64 `gorm:"primaryKey;autoIncrement"`
	TenantID int
	OrgID    int64
}

type joinContractOrg struct {
	ID        int64 `gorm:"primaryKey;autoIncrement"`
	TenantID  int
	Name      string
	DeletedAt gorm.DeletedAt
}

type joinContractProjection struct {
	ID      int64
	OrgName sql.NullString
}

func setupJoinContractDB(t *testing.T) (*Repository[int64, joinContractUser], *gorm.DB, []joinContractUser) {
	t.Helper()
	db := openDB(t)
	if err := db.AutoMigrate(&joinContractUser{}, &joinContractOrg{}); err != nil {
		t.Fatal(err)
	}
	if db.Name() == "mysql" || db.Name() == "postgres" {
		truncateTables(t, db, &joinContractUser{}, &joinContractOrg{})
		t.Cleanup(func() { truncateTables(t, db, &joinContractUser{}, &joinContractOrg{}) })
	}
	orgs := []joinContractOrg{
		{TenantID: 1, Name: "active"},
		{TenantID: 1, Name: "deleted", DeletedAt: gorm.DeletedAt{Time: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Valid: true}},
		{TenantID: 2, Name: "outside"},
	}
	if err := db.Create(&orgs).Error; err != nil {
		t.Fatal(err)
	}
	users := []joinContractUser{
		{TenantID: 1, OrgID: orgs[0].ID},
		{TenantID: 1, OrgID: orgs[1].ID},
		{TenantID: 1, OrgID: orgs[2].ID},
		{TenantID: 1, OrgID: orgs[2].ID + 1000},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	return NewRepository[int64, joinContractUser](db), db, users
}

func newJoinContractQuery(extraSQL string, args ...any) (*Query[joinContractUser], *joinContractOrg) {
	// 主表 DataRule 显式限定表名；它不代表选择了副表规则。
	ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Table: "join_contract_users", Column: "tenant_id", Condition: "=", Value: "1"},
	})
	q, u := NewQuery[joinContractUser](ctx)
	org := As[joinContractOrg](q, "org")
	q.LeftJoinAs(org, &u.OrgID, &org.ID, extraSQL, args...).
		Select(&u.ID, "org.name AS org_name").Order(&u.ID, true)
	return q, org
}

func assertJoinContractRows(t *testing.T, rows []joinContractProjection, users []joinContractUser, names ...string) {
	t.Helper()
	if len(rows) != len(names) || len(users) != len(names) {
		t.Fatalf("rows=%+v，期望 %d 个主对象", rows, len(names))
	}
	for i, name := range names {
		want := sql.NullString{String: name, Valid: name != ""}
		if rows[i].ID != users[i].ID || rows[i].OrgName != want {
			t.Fatalf("rows[%d]=%+v，期望 ID=%d OrgName=%+v", i, rows[i], users[i].ID, want)
		}
	}
}

func TestLeftJoinAs_ExplicitSideRulesPreserveMainRows(t *testing.T) {
	for _, tc := range []struct {
		name     string
		extraSQL string
		args     []any
		names    []string
	}{
		{name: "NoSideRules", names: []string{"active", "deleted", "outside", ""}},
		{name: "SoftDeleteOnly", extraSQL: "AND org.deleted_at IS NULL", names: []string{"active", "", "outside", ""}},
		{name: "TenantOnly", extraSQL: "AND org.tenant_id = ?", args: []any{1}, names: []string{"active", "deleted", "", ""}},
		{name: "Both", extraSQL: "AND org.deleted_at IS NULL AND org.tenant_id = ?", args: []any{1}, names: []string{"active", "", "", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, _, users := setupJoinContractDB(t)
			q, _ := newJoinContractQuery(tc.extraSQL, tc.args...)
			var rows []joinContractProjection
			if err := FindAs(repo, q, &rows); err != nil {
				t.Fatal(err)
			}
			assertJoinContractRows(t, rows, users, tc.names...)
		})
	}
}

func TestLeftJoinAs_WhereChangesPreservation(t *testing.T) {
	repo, _, users := setupJoinContractDB(t)
	q, org := newJoinContractQuery("")
	// WHERE 的副表条件会排除无匹配、软删除及跨租户副表对应的主对象。
	q.IsNull(&org.DeletedAt).Eq(&org.TenantID, 1)
	var rows []joinContractProjection
	if err := FindAs(repo, q, &rows); err != nil {
		t.Fatal(err)
	}
	assertJoinContractRows(t, rows, users[:1], "active")
}

func TestPageAs_JoinOnRulesPreserveTotalAndRows(t *testing.T) {
	repo, _, users := setupJoinContractDB(t)
	q, _ := newJoinContractQuery("AND org.deleted_at IS NULL AND org.tenant_id = ?", 1)
	var rows []joinContractProjection
	total, err := PageAs(repo, q.Page(1, 2), &rows, false)
	if err != nil || total != 4 {
		t.Fatalf("total=%d err=%v，期望保留 4 个主对象", total, err)
	}
	assertJoinContractRows(t, rows, users[:2], "active", "")
}

func TestLeftJoinAs_ExtraArgsRemainBoundValues(t *testing.T) {
	repo, db, users := setupJoinContractDB(t)
	literal := "active' OR 1=1 --"
	result := db.Model(&joinContractOrg{}).Where("name = ?", "active").Update("name", literal)
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("更新测试字面量 affected=%d err=%v", result.RowsAffected, result.Error)
	}
	for _, tc := range []struct {
		name  string
		input string
		match string
	}{
		{name: "ExistingLiteral", input: literal, match: literal},
		{name: "AbsentLiteral", input: "missing' OR 1=1 --"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, _ := newJoinContractQuery("AND org.name = ? AND org.tenant_id = ?", tc.input, 1)
			var rows []joinContractProjection
			if err := FindAs(repo, q, &rows); err != nil {
				t.Fatal(err)
			}
			assertJoinContractRows(t, rows, users, tc.match, "", "", "")
		})
	}
}

func TestLeftJoinAs_SideDataRuleUsesWhere(t *testing.T) {
	repo, _, users := setupJoinContractDB(t)
	ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Table: "join_contract_users", Column: "tenant_id", Condition: "=", Value: "1"},
		{Table: "org", Column: "tenant_id", Condition: "=", Value: "1"},
	})
	q, u := NewQuery[joinContractUser](ctx)
	org := As[joinContractOrg](q, "org")
	q.LeftJoinAs(org, &u.OrgID, &org.ID, "").
		Select(&u.ID, "org.name AS org_name").Order(&u.ID, true)
	var rows []joinContractProjection
	if err := FindAs(repo, q, &rows); err != nil {
		t.Fatal(err)
	}
	// 显式副表 DataRule 进入 WHERE，移除无匹配主对象，也不自动附加软删除条件。
	assertJoinContractRows(t, rows, users[:2], "active", "deleted")
}
