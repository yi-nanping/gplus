package gplus_test

import (
	"context"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
	"strings"
	"testing"
)

// TestUpdater_LeftJoinAs_BasicSQL 验证 LeftJoinAs 正确构建 join 片段。
// 注意：SQLite 不支持 UPDATE ... JOIN 语法，GORM 在 Updates 路径下会忽略 JOIN。
// 因此本测试直接检查 Updater 内部的 joins 字段，确保 JOIN SQL 片段正确生成。
func TestUpdater_LeftJoinAs_BasicSQL(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	u, ut := NewUpdater[TestUser](context.Background())
	o := As[Order](u, "o")
	u.LeftJoinAs(o, &o.UserID, &ut.ID, "")
	u.Set(&ut.Name, "x")
	u.Eq(&ut.Age, 18) // ToSQL 要求至少一个 WHERE 条件

	// 验证 GetError() 无错误（alias 链、字段解析均正确）
	if err := u.GetError(); err != nil {
		t.Fatalf("GetError: %v", err)
	}

	joinSQL := db.ToSQL(func(tx *gorm.DB) *gorm.DB { return tx.Model(new(TestUser)).Scopes(u.BuildQuery()).Find(&[]TestUser{}) })
	if !strings.Contains(joinSQL, "LEFT JOIN") || !strings.Contains(joinSQL, "AS o") {
		t.Fatalf("join SQL=%s", joinSQL)
	}

	// ToSQL 不报错（即使 SQLite 下 UPDATE+JOIN SQL 不含 JOIN）
	_, err := u.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
}

// TestUpdater_InnerJoinAs 验证 InnerJoinAs 正确构建 join 片段。
func TestUpdater_InnerJoinAs(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	u, ut := NewUpdater[TestUser](context.Background())
	o := As[Order](u, "o")
	u.InnerJoinAs(o, &o.UserID, &ut.ID, "")
	u.Set(&ut.Name, "x")
	u.Eq(&ut.Age, 18) // ToSQL 要求至少一个 WHERE 条件

	// 验证 GetError() 无错误
	if err := u.GetError(); err != nil {
		t.Fatalf("GetError: %v", err)
	}

	joinSQL := db.ToSQL(func(tx *gorm.DB) *gorm.DB { return tx.Model(new(TestUser)).Scopes(u.BuildQuery()).Find(&[]TestUser{}) })
	if !strings.Contains(joinSQL, "INNER JOIN") || !strings.Contains(joinSQL, "AS o") {
		t.Fatalf("join SQL=%s", joinSQL)
	}

	// ToSQL 不报错
	_, err := u.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
}

// TestUpdater_Set_AliasFieldResolves 验证 Updater.Set 在 alias 字段下正确解析为 alias.col
func TestUpdater_Set_AliasFieldResolves(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	u, ut := NewUpdater[TestUser](context.Background())
	o := As[Order](u, "o")
	u.LeftJoinAs(o, &o.UserID, &ut.ID, "")
	// 关键：u.Set 用 alias 字段，应解析为 o.<column>
	u.Set(&o.Amount, 100).Eq(&ut.ID, 1)
	if err := u.GetError(); err != nil {
		t.Fatalf("Set with alias field accumulated error: %v", err)
	}
	sql, err := u.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	// SQL 应含 o.amount（可能的形式：o.amount、"o"."amount"、`o`.`amount` 等，取决于 dialect）
	// 只要 set map 中的列名包含 alias 前缀即可验证修复有效
	if !strings.Contains(sql, "o") || !strings.Contains(sql, "amount") {
		t.Errorf("expected alias 'o' and 'amount' in SQL, got %s", sql)
	}
	// 更严格的检查：查看 setMap 中的列名是否包含 alias 前缀
	updateMap := u.UpdateMap()
	if len(updateMap) == 0 {
		t.Fatal("expected setMap to have entries")
	}
	hasAliasAmount := false
	for k := range updateMap {
		if k == "o.amount" {
			hasAliasAmount = true
			break
		}
	}
	if !hasAliasAmount {
		t.Errorf("expected 'o.amount' in setMap, got keys: %v", updateMap)
	}
}
