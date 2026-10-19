package gplus

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type incrDryRunDialector struct {
	unitDialector
	name  string
	quote byte
}

func (d incrDryRunDialector) Name() string { return d.name }

func (d incrDryRunDialector) QuoteTo(writer clause.Writer, name string) {
	writer.WriteByte(d.quote)
	for _, char := range name {
		switch byte(char) {
		case '.':
			writer.WriteByte(d.quote)
			writer.WriteByte('.')
			writer.WriteByte(d.quote)
		case d.quote:
			writer.WriteByte(d.quote)
			writer.WriteByte(d.quote)
		default:
			writer.WriteByte(byte(char))
		}
	}
	writer.WriteByte(d.quote)
}

func newIncrDryRunDB(t testing.TB, dialect string, quote byte) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(incrDryRunDialector{name: dialect, quote: quote}, &gorm.Config{
		DryRun:                 true,
		SkipDefaultTransaction: true,
		DisableAutomaticPing:   true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func buildIncrUpdate[T any](db *gorm.DB, u *Updater[T]) *gorm.DB {
	return db.Session(&gorm.Session{DryRun: true, SkipDefaultTransaction: true}).
		WithContext(u.Context()).Model(new(T)).Scopes(u.BuildUpdate()).Updates(u.setMap)
}

func containsIncrVar(vars []any, want int64) bool {
	for _, value := range vars {
		if value == want {
			return true
		}
	}
	return false
}

func TestUpdater_Incr_BuildsSetAndCASInOneUpdate(t *testing.T) {
	u, m := NewUpdater[TestUser](context.Background())
	u.Set(&m.Name, "alice").
		Incr(&m.Age, 12345).
		Incr(&m.Score, -6789).
		Eq(&m.ID, int64(7)).
		Eq(&m.Age, 30)

	tx := buildIncrUpdate(newIncrDryRunDB(t, "sqlite", '"'), u)
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	sql := tx.Statement.SQL.String()
	for _, fragment := range []string{"UPDATE", "SET", `"username"`, `"age"="age" + ?`, `"score"="score" + ?`, `"id" = ?`, `"age" = ?`} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("SQL 缺少 %q: %s", fragment, sql)
		}
	}
	if strings.Contains(sql, "12345") || strings.Contains(sql, "-6789") {
		t.Fatalf("delta 应参数化，不应直接出现在 SQL 中: %s", sql)
	}
	if !containsIncrVar(tx.Statement.Vars, 12345) || !containsIncrVar(tx.Statement.Vars, -6789) {
		t.Fatalf("delta 未进入绑定参数: %#v", tx.Statement.Vars)
	}
}

func TestUpdater_Incr_ColumnTagAndDialectQuotes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dialect string
		quote   byte
	}{
		{name: "SQLite", dialect: "sqlite", quote: '"'},
		{name: "MySQL", dialect: "mysql", quote: '`'},
		{name: "PostgreSQL", dialect: "postgres", quote: '"'},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, m := NewUpdater[TestUser](context.Background())
			u.Incr(&m.Name, 24680).Eq(&m.ID, int64(1))
			tx := buildIncrUpdate(newIncrDryRunDB(t, tc.dialect, tc.quote), u)
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			sql := tx.Statement.SQL.String()
			quotedColumn := fmt.Sprintf("%cusername%c", tc.quote, tc.quote)
			if strings.Count(sql, quotedColumn) < 2 {
				t.Fatalf("SET 键和表达式列应按 %s 方言引用 column tag: %s", tc.name, sql)
			}
			if strings.Contains(sql, "test_users.username") {
				t.Fatalf("SET 表达式不应写入带表名前缀的列: %s", sql)
			}
			if !containsIncrVar(tx.Statement.Vars, 24680) {
				t.Fatalf("delta 未参数化: SQL=%s vars=%#v", sql, tx.Statement.Vars)
			}
		})
	}
}

func TestUpdater_Incr_RejectsNonMainModelFieldsAndFailsClosed(t *testing.T) {
	tests := []struct {
		name  string
		field func(*Updater[TestUser], *TestUser) any
	}{
		{name: "字符串", field: func(_ *Updater[TestUser], _ *TestUser) any { return "age" }},
		{name: "nil", field: func(_ *Updater[TestUser], _ *TestUser) any { return nil }},
		{name: "typed nil", field: func(_ *Updater[TestUser], _ *TestUser) any {
			var field *int
			return field
		}},
		{name: "独立实例", field: func(_ *Updater[TestUser], _ *TestUser) any {
			orphan := new(TestUser)
			return &orphan.Age
		}},
		{name: "其他模型", field: func(_ *Updater[TestUser], _ *TestUser) any { return &Model[Order]().Amount }},
		{name: "alias", field: func(u *Updater[TestUser], _ *TestUser) any { return &As[Order](u, "o").Amount }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u, m := NewUpdater[TestUser](context.Background())
			u.Set(&m.Name, "kept").Incr(tc.field(u, m), 1).Eq(&m.ID, int64(1))
			if u.GetError() == nil {
				t.Fatal("非法字段必须累积错误")
			}
			if _, ok := u.UpdateMap()["age"]; ok {
				t.Fatal("非法字段不应写入 setMap")
			}
			tx := buildIncrUpdate(newIncrDryRunDB(t, "sqlite", '"'), u)
			if tx.Error == nil {
				t.Fatalf("BuildUpdate 必须在字段错误时 fail closed，SQL=%s", tx.Statement.SQL.String())
			}
			if tx.Statement.SQL.Len() != 0 {
				t.Fatalf("fail closed 不应构建 UPDATE SQL: %s", tx.Statement.SQL.String())
			}
		})
	}
}

func TestUpdater_Incr_SelectOmitAndLastAssignmentWins(t *testing.T) {
	t.Run("Select", func(t *testing.T) {
		u, m := NewUpdater[TestUser](context.Background())
		u.Incr(&m.Age, 1).Incr(&m.Score, 2).Select(&m.Age).Eq(&m.ID, int64(1))
		tx := buildIncrUpdate(newIncrDryRunDB(t, "sqlite", '"'), u)
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		sql := tx.Statement.SQL.String()
		if !strings.Contains(sql, `"age"="age" + ?`) || strings.Contains(sql, `"score"=`) {
			t.Fatalf("Select 未限制 Incr 更新列: %s", sql)
		}
	})

	t.Run("Omit", func(t *testing.T) {
		u, m := NewUpdater[TestUser](context.Background())
		u.Incr(&m.Age, 1).Incr(&m.Score, 2).Omit(&m.Score).Eq(&m.ID, int64(1))
		tx := buildIncrUpdate(newIncrDryRunDB(t, "sqlite", '"'), u)
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		sql := tx.Statement.SQL.String()
		if !strings.Contains(sql, `"age"="age" + ?`) || strings.Contains(sql, `"score"=`) {
			t.Fatalf("Omit 未排除 Incr 更新列: %s", sql)
		}
	})

	t.Run("Incr覆盖Set", func(t *testing.T) {
		u, m := NewUpdater[TestUser](context.Background())
		u.Set(&m.Age, 10).Incr(&m.Age, 3)
		expr, ok := u.UpdateMap()["age"].(clause.Expr)
		if !ok || expr.SQL != "? + ?" || len(expr.Vars) != 2 || expr.Vars[1] != int64(3) {
			t.Fatalf("最后一次 Incr 应覆盖 Set: %#v", u.UpdateMap()["age"])
		}
	})

	t.Run("Set覆盖Incr", func(t *testing.T) {
		u, m := NewUpdater[TestUser](context.Background())
		u.Incr(&m.Age, 3).Set(&m.Age, 10)
		if got := u.UpdateMap()["age"]; got != 10 {
			t.Fatalf("最后一次 Set 应覆盖 Incr: %#v", got)
		}
	})
}

func TestUpdater_Incr_PointerEmbeddedPrefixField(t *testing.T) {
	u, m := NewUpdater[prefixModel](context.Background())
	u.Incr(&m.Shipping.City, 1)
	if err := u.GetError(); err != nil {
		t.Fatalf("命名指针嵌入字段应属于主模型: %v", err)
	}
	if _, ok := u.UpdateMap()["ship_city"]; !ok {
		t.Fatalf("应使用带 embeddedPrefix 的物理列名: %#v", u.UpdateMap())
	}
}
