package gplus

import (
	"context"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestDataRule_OR_ReadIsolation(t *testing.T) {
	for _, scenario := range []string{"direct", "group", "reuse", "scope", "nestedScope", "rulesOnly", "clear"} {
		t.Run(scenario, func(t *testing.T) {
			repo, db := setupTenantDB(t)
			aliceID, _ := insertTenantUsers(t, db)
			q, m := NewQuery[tenantUser](ctxWithTenantRule(1))
			switch scenario {
			case "direct":
				q.Eq(&m.Name, "Bob").OrEq(&m.Name, "Alice")
			case "group":
				q.And(func(s *Query[tenantUser]) { s.Eq(&m.Name, "Bob").OrEq(&m.Name, "Alice") }).OrEq(&m.Name, "Bob")
			case "reuse":
				q.Eq(&m.Name, "Alice").DataRuleBuilder().DataRuleBuilder().OrEq(&m.Name, "Bob")
			case "scope":
				q.Eq(&m.Name, "Alice").WithScope(func(db *gorm.DB) *gorm.DB { return db.Or("name = ?", "Bob") })
			case "nestedScope":
				q.Eq(&m.Name, "Alice").WithScope(nestedTenantORScope)
			case "clear":
				q.Eq(&m.Name, "Bob").DataRuleBuilder()
				q.Clear()
				q.Eq(&m.Name, "Alice").OrEq(&m.Name, "Bob")
			}
			rows, err := repo.List(q)
			if err != nil || len(rows) != 1 || rows[0].ID != aliceID {
				t.Fatalf("List rows=%+v err=%v, expected tenant 1 only", rows, err)
			}
			count, err := repo.Count(q)
			if err != nil || count != 1 {
				t.Fatalf("Count=%d err=%v, expected 1", count, err)
			}
			rows, total, err := repo.Page(q.Page(1, 10), false)
			if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != aliceID {
				t.Fatalf("Page rows=%+v total=%d err=%v", rows, total, err)
			}
		})
	}
}

func TestDataRule_OR_UpdateIsolation(t *testing.T) {
	for _, scenario := range []string{"direct", "group", "reuse", "scope", "nestedScope", "rulesOnly", "clear", "increment", "decrement"} {
		t.Run(scenario, func(t *testing.T) {
			repo, db := setupTenantDB(t)
			aliceID, bobID := insertTenantUsers(t, db)
			u, m := NewUpdater[tenantUser](ctxWithTenantRule(1))
			u.Set(&m.Name, "changed")
			switch scenario {
			case "group":
				u.And(func(s *Updater[tenantUser]) { s.Eq(&m.Name, "Bob").OrEq(&m.Name, "Alice") }).OrEq(&m.Name, "Bob")
			case "reuse":
				u.Eq(&m.Name, "Alice").DataRuleBuilder().DataRuleBuilder().OrEq(&m.Name, "Bob")
			case "scope":
				u.Eq(&m.Name, "Alice").WithScope(func(db *gorm.DB) *gorm.DB { return db.Or("name = ?", "Bob") })
			case "nestedScope":
				u.Eq(&m.Name, "Alice").WithScope(nestedTenantORScope)
			case "rulesOnly":
			case "clear":
				u.Eq(&m.Name, "Bob").DataRuleBuilder()
				u.Clear()
				u.Set(&m.Name, "changed").Eq(&m.Name, "Alice").OrEq(&m.Name, "Bob")
			default:
				u.Eq(&m.Name, "Bob").OrEq(&m.Name, "Alice")
			}
			var affected int64
			var err error
			wantName := "changed"
			switch scenario {
			case "increment":
				affected, err = repo.IncrBy(u, &m.TenantID, 10)
				wantName = "Alice"
			case "decrement":
				affected, err = repo.DecrBy(u, &m.TenantID, 1)
				wantName = "Alice"
			default:
				affected, err = repo.UpdateByCond(u)
			}
			if err != nil || affected != 1 {
				t.Fatalf("affected=%d err=%v, expected 1", affected, err)
			}
			var alice, bob tenantUser
			if err := db.First(&alice, aliceID).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.First(&bob, bobID).Error; err != nil {
				t.Fatal(err)
			}
			if alice.Name != wantName || bob.Name != "Bob" || bob.TenantID != 2 {
				t.Fatalf("unexpected stored rows: alice=%+v bob=%+v", alice, bob)
			}
			if scenario == "increment" && alice.TenantID != 11 || scenario == "decrement" && alice.TenantID != 0 {
				t.Fatalf("atomic update not applied: alice=%+v", alice)
			}
		})
	}
}

func nestedTenantORScope(db *gorm.DB) *gorm.DB {
	return db.Scopes(func(db *gorm.DB) *gorm.DB {
		return db.Scopes(func(db *gorm.DB) *gorm.DB { return db.Or("name = ?", "Bob") })
	})
}

func TestDataRule_OR_CustomWhereBuilder(t *testing.T) {
	_, db := setupTenantDB(t)
	aliceID, _ := insertTenantUsers(t, db)
	customCalls := 0
	db.ClauseBuilders["WHERE"] = func(c clause.Clause, builder clause.Builder) {
		customCalls++
		c.Build(builder)
	}
	q, m := NewQuery[tenantUser](ctxWithTenantRule(1))
	q.Eq(&m.Name, "Alice").WithScope(nestedTenantORScope)
	rows, err := NewRepository[int64, tenantUser](db).List(q)
	if err != nil || len(rows) != 1 || rows[0].ID != aliceID || customCalls != 1 {
		t.Fatalf("rows=%+v err=%v customCalls=%d", rows, err, customCalls)
	}
	// 原始连接上的配置不可被带权限规则的查询污染。
	var unfiltered []tenantUser
	if err := db.Find(&unfiltered).Error; err != nil {
		t.Fatal(err)
	}
	if len(unfiltered) != 2 || customCalls != 2 {
		t.Fatalf("shared WHERE builder polluted: rows=%+v customCalls=%d", unfiltered, customCalls)
	}
	// NewDB 子查询继承 Config，但不能继承父语句的权限规则。
	sub, sm := NewQuery[tenantUser](context.Background())
	sub.Eq(&sm.Name, "Bob")
	outer, _ := NewQuery[tenantUser](ctxWithTenantRule(1))
	outer.Exists(sub)
	rows, err = NewRepository[int64, tenantUser](db).List(outer)
	if err != nil || len(rows) != 1 || rows[0].ID != aliceID {
		t.Fatalf("outer rules leaked into scope-only subquery: rows=%+v err=%v", rows, err)
	}
}

func TestDataRule_OR_BuildOnlyAndExplicitSubquery(t *testing.T) {
	repo, db := setupTenantDB(t)
	aliceID, _ := insertTenantUsers(t, db)
	q, m := NewQuery[tenantUser](ctxWithTenantRule(1))
	q.Eq(&m.Name, "Bob").OrEq(&m.Name, "Alice")
	var rows []tenantUser
	// 构建入口仍由调用方明确选择是否应用 Context 中的规则。
	if err := db.Model(&tenantUser{}).Scopes(q.BuildQuery()).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("BuildQuery applied implicit rules: %+v", rows)
	}
	q.DataRuleBuilder().Select(&m.ID).WithScope(nestedTenantORScope)
	outer, om := NewQuery[tenantUser](context.Background())
	outer.InSub(&om.ID, q)
	rows, err := repo.List(outer)
	if err != nil || len(rows) != 1 || rows[0].ID != aliceID {
		t.Fatalf("explicitly protected subquery rows=%+v err=%v", rows, err)
	}
}

func TestDataRule_OR_NoRulesPreservesPrecedence(t *testing.T) {
	repo, db := setupTenantDB(t)
	insertTenantUsers(t, db)
	q, m := NewQuery[tenantUser](context.Background())
	q.Eq(&m.Name, "Bob").OrEq(&m.Name, "Alice").Eq(&m.TenantID, 1)
	rows, err := repo.List(q)
	if err != nil || len(rows) != 2 {
		t.Fatalf("ordinary AND/OR semantics changed: rows=%+v err=%v", rows, err)
	}
	u, um := NewUpdater[tenantUser](context.Background())
	u.Set(&um.Name, "changed").Eq(&um.Name, "Bob").OrEq(&um.Name, "Alice").Eq(&um.TenantID, 1)
	affected, err := repo.UpdateByCond(u)
	if err != nil || affected != 2 {
		t.Fatalf("ordinary update AND/OR semantics changed: affected=%d err=%v", affected, err)
	}
}

type dataRuleORParent struct {
	ID       int64 `gorm:"primaryKey"`
	TenantID int
	ChildID  int64
	Child    tenantUser `gorm:"foreignKey:ChildID;references:ID"`
}

func TestDataRule_OR_CustomWherePreloadIsolation(t *testing.T) {
	_, db := setupTenantDB(t)
	_, bobID := insertTenantUsers(t, db)
	if err := db.AutoMigrate(&dataRuleORParent{}); err != nil {
		t.Fatal(err)
	}
	parent := dataRuleORParent{TenantID: 1, ChildID: bobID}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	db.ClauseBuilders["WHERE"] = func(c clause.Clause, builder clause.Builder) { c.Build(builder) }
	q, _ := NewQuery[dataRuleORParent](ctxWithTenantRule(1))
	q.Preload("Child")
	rows, err := NewRepository[int64, dataRuleORParent](db).List(q)
	// 权限规则限制父表；现有 Preload 约定不自动为关联记录追加父表规则。
	if err != nil || len(rows) != 1 || rows[0].ID != parent.ID || rows[0].Child.ID != bobID || rows[0].Child.TenantID != 2 {
		t.Fatalf("parent rules leaked into preload: rows=%+v err=%v", rows, err)
	}
}

func TestDataRule_OR_DeleteRestoreIsolation(t *testing.T) {
	for _, restore := range []bool{false, true} {
		t.Run(map[bool]string{false: "delete", true: "restore"}[restore], func(t *testing.T) {
			repo, db := setupTenantDB(t)
			aliceID, bobID := insertTenantUsers(t, db)
			if restore {
				if err := db.Delete(&tenantUser{}, []int64{aliceID, bobID}).Error; err != nil {
					t.Fatal(err)
				}
			}
			q, m := NewQuery[tenantUser](ctxWithTenantRule(1))
			q.Eq(&m.Name, "Bob").OrEq(&m.Name, "Alice")
			var affected int64
			var err error
			if restore {
				affected, err = repo.RestoreByCond(q)
			} else {
				affected, err = repo.DeleteByCond(q)
			}
			if err != nil || affected != 1 {
				t.Fatalf("affected=%d err=%v", affected, err)
			}
			var alice, bob tenantUser
			if err := db.Unscoped().First(&alice, aliceID).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Unscoped().First(&bob, bobID).Error; err != nil {
				t.Fatal(err)
			}
			if alice.DeletedAt.Valid == restore || bob.DeletedAt.Valid != restore {
				t.Fatalf("unexpected deletion state: alice=%+v bob=%+v", alice, bob)
			}
		})
	}
}

func TestDataRule_OR_InvalidRuleBlocksExecution(t *testing.T) {
	repo, db := setupTenantDB(t)
	aliceID, bobID := insertTenantUsers(t, db)
	ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{{Column: "tenant_id", Condition: "unsupported", Value: "1"}})
	q, m := NewQuery[tenantUser](ctx)
	q.Eq(&m.Name, "Alice").OrEq(&m.Name, "Bob")
	if _, err := repo.List(q); err == nil {
		t.Fatal("invalid rule must reject reads")
	}
	u, um := NewUpdater[tenantUser](ctx)
	u.Set(&um.Name, "changed").Eq(&um.Name, "Alice").OrEq(&um.Name, "Bob")
	if _, err := repo.UpdateByCond(u); err == nil {
		t.Fatal("invalid rule must reject updates")
	}
	var rows []tenantUser
	if err := db.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != aliceID || rows[0].Name != "Alice" || rows[1].ID != bobID || rows[1].Name != "Bob" {
		t.Fatalf("invalid rule changed records: %+v", rows)
	}
}
