package gplus_test

import (
	"context"
	"strings"
	"testing"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
)

func TestJoinOn_LeftPreservesRowsAndCount(t *testing.T) {
	repo, _, users := setupJoinContractDB(t)
	q, u := NewQuery[joinContractUser](context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Table: "join_contract_users", Column: "tenant_id", Condition: "=", Value: "1"},
	}))
	org := As[joinContractOrg](q, "org")
	q.LeftJoinAsOn(org, &u.OrgID, &org.ID, func(on *OnBuilder) {
		on.Eq(&org.TenantID, 1).IsNull(&org.DeletedAt).And(func(group *OnBuilder) {
			group.In(&org.Name, []string{"active"}).Or(func(other *OnBuilder) { other.Eq(&org.Name, "outside") })
		})
	}).Select(&u.ID, "org.name AS org_name").Order(&u.ID, true)
	var rows []joinContractProjection
	total, err := PageAs(repo, q, &rows, false)
	if err != nil || total != 4 {
		t.Fatalf("total=%d err=%v", total, err)
	}
	assertJoinContractRows(t, rows, users, "active", "", "", "")
}

func TestJoinOn_InnerAndEmptyIn(t *testing.T) {
	repo, _, _ := setupJoinContractDB(t)
	q, u := NewQuery[joinContractUser](context.Background())
	org := As[joinContractOrg](q, "org")
	q.InnerJoinAsOn(org, &u.OrgID, &org.ID, func(on *OnBuilder) {
		on.Eq(&org.TenantID, 1).IsNull(&org.DeletedAt).IsNotNull(&org.Name)
	})
	rows, err := repo.List(q)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	q, u = NewQuery[joinContractUser](context.Background())
	org = As[joinContractOrg](q, "org")
	q.LeftJoinAsOn(org, &u.OrgID, &org.ID, func(on *OnBuilder) { on.In(&org.Name, []string{}) })
	count, err := repo.Count(q)
	if err != nil || count != 4 {
		t.Fatalf("empty IN must preserve left rows: count=%d err=%v", count, err)
	}
}

func TestJoinOn_ValuesRemainBound(t *testing.T) {
	_, db, _ := setupJoinContractDB(t)
	q, u := NewQuery[joinContractUser](context.Background())
	org := As[joinContractOrg](q, "org")
	value := "x' OR 1=1 --"
	q.LeftJoinAsOn(org, &u.OrgID, &org.ID, func(on *OnBuilder) { on.Eq(&org.Name, value) })
	var rows []joinContractUser
	tx := db.Session(&gorm.Session{DryRun: true}).Model(new(joinContractUser)).Scopes(q.BuildQuery()).Find(&rows)
	if tx.Error != nil || strings.Contains(tx.Statement.SQL.String(), value) || len(tx.Statement.Vars) != 1 || tx.Statement.Vars[0] != value {
		t.Fatalf("SQL=%s vars=%v err=%v", tx.Statement.SQL.String(), tx.Statement.Vars, tx.Error)
	}
	if strings.Contains(tx.Statement.SQL.String(), "WHERE") {
		t.Fatalf("ON filter leaked into WHERE: %s", tx.Statement.SQL.String())
	}
}

func TestJoinOn_InvalidInputRejectsQueryAndCount(t *testing.T) {
	_, db, _ := setupJoinContractDB(t)
	for _, kind := range []string{"nil_callback", "nil_field", "string_field", "string_join_column", "invalid_in", "nil_group", "foreign_alias", "foreign_model", "revoked_field"} {
		t.Run(kind, func(t *testing.T) {
			q, u := NewQuery[joinContractUser](context.Background())
			org := As[joinContractOrg](q, "org")
			var left any = &u.OrgID
			fn := func(on *OnBuilder) {}
			switch kind {
			case "nil_callback":
				fn = nil
			case "nil_field":
				fn = func(on *OnBuilder) { on.Eq(nil, 1) }
			case "string_field":
				fn = func(on *OnBuilder) { on.Eq("org.tenant_id", 1) }
			case "string_join_column":
				left = "join_contract_users.org_id"
			case "invalid_in":
				fn = func(on *OnBuilder) { on.In(&org.Name, 1) }
			case "nil_group":
				fn = func(on *OnBuilder) { on.And(nil) }
			case "foreign_alias":
				other, _ := NewQuery[joinContractUser](context.Background())
				foreign := As[joinContractOrg](other, "foreign")
				fn = func(on *OnBuilder) { on.Eq(&foreign.Name, "active") }
			case "foreign_model":
				foreign := Model[joinContractOrg]()
				fn = func(on *OnBuilder) { on.Eq(&foreign.TenantID, 123) }
			case "revoked_field":
				old := org
				q.Clear()
				org = As[joinContractOrg](q, "fresh")
				fn = func(on *OnBuilder) { on.Eq(&old.Name, "active") }
			}
			q.LeftJoinAsOn(org, left, &org.ID, fn)
			if q.GetError() == nil {
				t.Fatal("invalid ON input must accumulate a build error")
			}
			var rows []joinContractUser
			tx := db.Session(&gorm.Session{DryRun: true}).Model(new(joinContractUser)).Scopes(q.BuildQuery()).Find(&rows)
			if tx.Error == nil || tx.Statement.SQL.Len() != 0 {
				t.Fatalf("SELECT error=%v SQL=%s", tx.Error, tx.Statement.SQL.String())
			}
			var count int64
			tx = db.Session(&gorm.Session{DryRun: true}).Model(new(joinContractUser)).Scopes(q.BuildCount()).Count(&count)
			if tx.Error == nil || tx.Statement.SQL.Len() != 0 {
				t.Fatalf("COUNT error=%v SQL=%s", tx.Error, tx.Statement.SQL.String())
			}
		})
	}
}

func TestJoinOn_OuterCanonicalField(t *testing.T) {
	repo, db, users := setupJoinContractDB(t)
	if err := db.Create(&joinContractUser{TenantID: 2, OrgID: users[0].OrgID}).Error; err != nil {
		t.Fatal(err)
	}
	q, u := NewQuery[joinContractUser](context.Background())
	sub, org := SubQuery[joinContractOrg](q)
	linked := As[joinContractUser](sub, "linked")
	sub.Select(&org.ID).InnerJoinAsOn(linked, &org.ID, &linked.OrgID, func(on *OnBuilder) {
		on.Eq(&u.TenantID, 1)
	})
	count, err := repo.Count(q.Exists(sub))
	if err != nil || count != 4 {
		t.Fatalf("outer tenant filter: count=%d err=%v", count, err)
	}
}

type JoinOnPointerBase struct {
	TenantID int
}

type joinOnPointerUser struct {
	*JoinOnPointerBase
	ID    int64 `gorm:"primaryKey"`
	OrgID int64
}

func TestJoinOn_PointerEmbeddedAndDynamicTable(t *testing.T) {
	db := openSQLite(t)
	q, u := NewQuery[joinOnPointerUser](context.Background())
	org := As[joinContractOrg](q, "org")
	q.Table("custom_users").LeftJoinAsOn(org, &u.OrgID, &org.ID, func(on *OnBuilder) {
		on.Eq(&u.TenantID, 1)
	})
	var rows []joinOnPointerUser
	tx := db.Session(&gorm.Session{DryRun: true}).Model(new(joinOnPointerUser)).Scopes(q.BuildQuery()).Find(&rows)
	if tx.Error != nil || !strings.Contains(tx.Statement.SQL.String(), "custom_users") || len(tx.Statement.Vars) != 1 {
		t.Fatalf("SQL=%s vars=%v err=%v", tx.Statement.SQL.String(), tx.Statement.Vars, tx.Error)
	}
}
