package gplus

import (
	"strings"
	"testing"

	"gorm.io/gorm"
)

// TestGORMAliasBehaviorProbe 永久锁定 GORM v1.31.x 行为
// 升级 GORM 时此测试 fail，提醒同步检查 alias 体系实现
func TestGORMAliasBehaviorProbe(t *testing.T) {
	_, db := setupTestDB[TestUser](t)

	t.Run("Joins_AliasString_GeneratesExpectedSQL", func(t *testing.T) {
		var users []TestUser
		stmt := db.Session(&gorm.Session{DryRun: true}).
			Joins("LEFT JOIN orders AS o ON o.user_id = test_users.id").
			Find(&users).Statement
		got := stmt.SQL.String()
		want := "LEFT JOIN orders AS o ON o.user_id = test_users.id"
		if !strings.Contains(got, want) {
			t.Errorf("expected SQL contains %q, got %q", want, got)
		}
	})

	t.Run("Where_ExistsSubquery_SubqueryDBSubstitution", func(t *testing.T) {
		subDB := db.Session(&gorm.Session{NewDB: true}).
			Model(&TestUser{}).Select("1").Where("id = ?", 1)
		var users []TestUser
		stmt := db.Session(&gorm.Session{DryRun: true}).
			Where("EXISTS (?)", subDB).
			Find(&users).Statement
		got := stmt.SQL.String()
		if !strings.Contains(got, "EXISTS") {
			t.Errorf("expected SQL contains EXISTS, got %q", got)
		}
	})

	t.Run("SelfJoin_SameTable_DifferentAlias_NoConflict", func(t *testing.T) {
		var users []TestUser
		stmt := db.Session(&gorm.Session{DryRun: true}).
			Joins("LEFT JOIN test_users AS boss ON test_users.id = boss.id").
			Find(&users).Statement
		got := stmt.SQL.String()
		if !strings.Contains(got, "test_users AS boss") {
			t.Errorf("self-join alias not preserved, got %q", got)
		}
	})

	t.Run("Subquery_NoOuterClauseLeak_AssertedExplicitly", func(t *testing.T) {
		// 外层已积累 WHERE
		outer := db.Where("status = ?", "active").Model(&TestUser{}).Select("id, name")
		// 派生子查询
		sub := outer.Session(&gorm.Session{NewDB: true}).
			Model(&TestUser{}).Select("1").Where("age > ?", 18)
		stmt := sub.Session(&gorm.Session{DryRun: true}).Find(&[]TestUser{}).Statement
		got := stmt.SQL.String()
		if strings.Contains(got, "status = ") {
			t.Errorf("subquery leaked outer WHERE: %q", got)
		}
		if strings.Contains(got, "id, name") {
			t.Errorf("subquery leaked outer SELECT: %q", got)
		}
	})

	t.Run("JoinsWithArgs_ArgsParameterized_NotInlined", func(t *testing.T) {
		var users []TestUser
		stmt := db.Session(&gorm.Session{DryRun: true}).
			Joins("LEFT JOIN orders AS o ON o.user_id = test_users.id AND o.status = ?", "paid").
			Find(&users).Statement
		got := stmt.SQL.String()
		if !strings.Contains(got, "?") {
			t.Errorf("expected ? placeholder in DryRun SQL, got %q", got)
		}
		if strings.Contains(got, "'paid'") {
			t.Errorf("paid value should not be inlined: %q", got)
		}
	})
}
