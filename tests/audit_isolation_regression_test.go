package gplus_test

import (
	"context"
	"errors"
	"testing"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
)

func TestDataRule_EmptyValueInvalidConditionRejectsReadAndUpdate(t *testing.T) {
	for _, condition := range []string{"unsupported", "SQL", "USE_SQL_RULES", "  sql  ", ""} {
		t.Run(condition, func(t *testing.T) {
			repo, db := setupTenantDB(t)
			aliceID, bobID := insertTenantUsers(t, db)
			queryCalls, updateCalls := 0, 0
			if err := db.Callback().Query().Before("gorm:query").Register("audit:invalid-rule-query", func(*gorm.DB) { queryCalls++ }); err != nil {
				t.Fatal(err)
			}
			if err := db.Callback().Update().Before("gorm:update").Register("audit:invalid-rule-update", func(*gorm.DB) { updateCalls++ }); err != nil {
				t.Fatal(err)
			}
			ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{{Column: "tenant_id", Condition: condition}})
			q, m := NewQuery[tenantUser](ctx)
			q.Eq(&m.Name, "Alice").OrEq(&m.Name, "Bob")
			rows, err := repo.List(q)
			if err == nil || len(rows) != 0 {
				t.Errorf("非法规则应拒绝读取: rows=%d err=%v", len(rows), err)
			}
			u, um := NewUpdater[tenantUser](ctx)
			u.Set(&um.Name, "changed").Eq(&um.Name, "Alice").OrEq(&um.Name, "Bob")
			affected, err := repo.UpdateByCond(u)
			if err == nil || affected != 0 {
				t.Errorf("非法规则应拒绝更新: affected=%d err=%v", affected, err)
			}
			if queryCalls != 0 || updateCalls != 0 {
				t.Errorf("非法规则应在执行前短路: queryCalls=%d updateCalls=%d", queryCalls, updateCalls)
			}
			for id, name := range map[int64]string{aliceID: "Alice", bobID: "Bob"} {
				var stored tenantUser
				if err := db.First(&stored, id).Error; err != nil || stored.Name != name {
					t.Errorf("非法规则改变数据库: row=%+v err=%v", stored, err)
				}
			}
		})
	}
}

func TestDataRule_EmptyValueValidConditionRemainsIgnored(t *testing.T) {
	for _, condition := range []string{"=", "EQ", "<>", "!=", "NE", ">", "GT", ">=", "GE", "<", "LT", "<=", "LE", "IN", "NOT IN", "LIKE", "LEFT_LIKE", "RIGHT_LIKE", "BETWEEN"} {
		t.Run(condition, func(t *testing.T) {
			repo, db := setupTenantDB(t)
			insertTenantUsers(t, db)
			ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{{Column: "tenant_id", Condition: condition}})
			q, _ := NewQuery[tenantUser](ctx)
			rows, err := repo.List(q)
			if err != nil || len(rows) != 2 {
				t.Errorf("合法空值规则应沿用忽略契约: rows=%d err=%v", len(rows), err)
			}
			u, um := NewUpdater[tenantUser](ctx)
			u.Set(&um.Name, "changed").In(&um.Name, []string{"Alice", "Bob"})
			affected, err := repo.UpdateByCond(u)
			if err != nil || affected != 2 {
				t.Errorf("合法空值规则应沿用忽略契约: affected=%d err=%v", affected, err)
			}
		})
	}
}

func TestFirstOrUpdate_JoinedDataRuleReloadAndRollback(t *testing.T) {
	for _, structured := range []bool{false, true} {
		for _, moveOut := range []bool{false, true} {
			name := "raw"
			if structured {
				name = "structured"
			}
			if moveOut {
				name += "/rollback"
			} else {
				name += "/reload"
			}
			t.Run(name, func(t *testing.T) {
				repo, db := setupTenantDB(t)
				aliceID, _ := insertTenantUsers(t, db)
				ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{{Table: "ext", Column: "tenant_id", Condition: "=", Value: "1"}})
				q, m := NewQuery[tenantUser](ctx)
				if structured {
					ext := As[tenantUser](q, "ext")
					q.LeftJoinAsOn(ext, &m.ID, &ext.ID, func(on *OnBuilder) {
						on.Eq(&ext.TenantID, 1)
					})
				} else {
					q.LeftJoin("tenant_users AS ext", "tenant_users.id = ext.id")
				}
				q.Eq("tenant_users.id", aliceID).Eq("tenant_users.name", "Alice")
				u, um := NewUpdater[tenantUser](context.Background())
				if moveOut {
					u.Set(&um.TenantID, 2)
				} else {
					u.Set(&um.Name, "changed")
				}
				row, created, err := repo.FirstOrUpdate(q, u, &tenantUser{})
				var stored tenantUser
				if getErr := db.First(&stored, aliceID).Error; getErr != nil {
					t.Fatal(getErr)
				}
				if moveOut {
					if !errors.Is(err, gorm.ErrRecordNotFound) || created || stored.TenantID != 1 {
						t.Errorf("权限外更新应回滚: stored=%+v created=%v err=%v", stored, created, err)
					}
				} else if err != nil || created || row.Name != "changed" || stored.Name != "changed" {
					t.Errorf("重读应保留JOIN权限且不恢复旧业务WHERE: row=%+v stored=%+v created=%v err=%v", row, stored, created, err)
				}
			})
		}
	}
}

func TestFirstOrUpdate_WithoutJoinedDataRuleReloadsByPrimaryKey(t *testing.T) {
	for _, scenario := range []string{"no-rule", "ignored-empty-rule", "main-rule", "qualified-main-rule"} {
		t.Run(scenario, func(t *testing.T) {
			repo, db := setupTenantDB(t)
			aliceID, _ := insertTenantUsers(t, db)
			ctx := context.Background()
			switch scenario {
			case "ignored-empty-rule":
				ctx = context.WithValue(ctx, DataRuleKey, []DataRule{{Table: "ext", Column: "tenant_id", Condition: "="}})
			case "main-rule":
				// id/name 在 JOIN 中二义，tenant_id 通过没有该列的投影 JOIN 保持单义。
				ctx = ctxWithTenantRule(1)
			case "qualified-main-rule":
				ctx = context.WithValue(ctx, DataRuleKey, []DataRule{{Table: "tenant_users", Column: "tenant_id", Condition: "=", Value: "1"}})
			}
			q, _ := NewQuery[tenantUser](ctx)
			q.InnerJoin("(SELECT id, name FROM tenant_users) AS ext", "tenant_users.id = ext.id AND ext.name = ?", "Alice").Eq("tenant_users.id", aliceID)
			u, um := NewUpdater[tenantUser](context.Background())
			u.Set(&um.Name, "changed")
			row, created, err := repo.FirstOrUpdate(q, u, &tenantUser{})
			stored, getErr := repo.GetById(context.Background(), aliceID)
			if err != nil || getErr != nil || created || row.Name != "changed" || stored.Name != "changed" {
				t.Errorf("无副表权限条件时应沿用按主键重读契约: row=%+v stored=%+v created=%v err=%v getErr=%v", row, stored, created, err, getErr)
			}
		})
	}
}

func TestFirstOrUpdate_ScopeJoinedDataRuleReloadAndRollback(t *testing.T) {
	for _, scenario := range []string{"reload", "rollback"} {
		t.Run(scenario, func(t *testing.T) {
			repo, db := setupTenantDB(t)
			aliceID, _ := insertTenantUsers(t, db)
			ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{{Table: "ext", Column: "tenant_id", Condition: "=", Value: "1"}})
			scopeCalls := 0
			q, _ := NewQuery[tenantUser](ctx)
			q.Eq("tenant_users.id", aliceID).WithScope(func(db *gorm.DB) *gorm.DB {
				scopeCalls++
				return db.Joins("LEFT JOIN tenant_users AS ext ON tenant_users.id = ext.id AND ext.tenant_id = ?", 1).
					Where("tenant_users.name = ?", "Alice")
			})
			u, um := NewUpdater[tenantUser](context.Background())
			if scenario == "rollback" {
				u.Set(&um.TenantID, 2)
			} else {
				u.Set(&um.Name, "changed")
			}
			row, created, err := repo.FirstOrUpdate(q, u, &tenantUser{})
			stored, getErr := repo.GetById(context.Background(), aliceID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			if scopeCalls != 1 {
				t.Errorf("重读不得重新执行业务scope: calls=%d", scopeCalls)
			}
			if scenario == "rollback" {
				if !errors.Is(err, gorm.ErrRecordNotFound) || created || stored.TenantID != 1 {
					t.Errorf("scope JOIN权限外更新应回滚: stored=%+v created=%v err=%v", stored, created, err)
				}
			} else if err != nil || created || row.Name != "changed" || stored.Name != "changed" {
				t.Errorf("重读应保留scope JOIN权限且不恢复业务WHERE: row=%+v stored=%+v created=%v err=%v", row, stored, created, err)
			}
		})
	}
}
