package gplus_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	. "github.com/yi-nanping/gplus"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// 使用内存 SQLite 连接池，仅检查方言构建；不会连接 MySQL/PostgreSQL。
func TestJoinOn_DialectSQL(t *testing.T) {
	base := openSQLite(t)
	for _, dialect := range []gorm.Dialector{
		mysql.New(mysql.Config{Conn: base.ConnPool, SkipInitializeWithVersion: true}),
		postgres.New(postgres.Config{Conn: base.ConnPool}),
	} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, SkipDefaultTransaction: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			q, u := NewQuery[joinContractUser](context.Background())
			org := As[joinContractOrg](q, "org")
			value := "x' OR 1=1 --"
			q.LeftJoinAsOn(org, &u.OrgID, &org.ID, func(on *OnBuilder) {
				on.Eq(&org.Name, value).In(&org.TenantID, []int{1, 2})
			}).Eq(&u.TenantID, 99)
			for _, countPath := range []bool{false, true} {
				var tx *gorm.DB
				if countPath {
					var count int64
					tx = db.Model(new(joinContractUser)).Scopes(q.BuildCount()).Count(&count)
				} else {
					var rows []joinContractUser
					tx = db.Model(new(joinContractUser)).Scopes(q.BuildQuery()).Find(&rows)
				}
				sql := tx.Statement.SQL.String()
				wantColumn := "`org`.`name`"
				if dialect.Name() == "postgres" {
					wantColumn = `"org"."name"`
				}
				if tx.Error != nil || !strings.Contains(sql, wantColumn) || strings.Contains(sql, value) {
					t.Fatalf("SQL=%s err=%v", sql, tx.Error)
				}
				if !reflect.DeepEqual(tx.Statement.Vars, []any{value, 1, 2, 99}) {
					t.Fatalf("parameter order: %v", tx.Statement.Vars)
				}
				if dialect.Name() == "postgres" && !strings.Contains(sql, "$4") {
					t.Fatalf("PostgreSQL placeholders: %s", sql)
				}
			}
		})
	}
}

// TestDriverProjection_MySQLPostgres 使用真实驱动生成投影 SQL，无需数据库服务。
func TestDriverProjection_MySQLPostgres(t *testing.T) {
	base := openSQLite(t)
	for _, dialect := range []gorm.Dialector{
		mysql.New(mysql.Config{Conn: base.ConnPool, SkipInitializeWithVersion: true}),
		postgres.New(postgres.Config{Conn: base.ConnPool}),
	} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, SkipDefaultTransaction: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct{ input, mysql, postgres string }{
				{"name", "`name`", `"name"`},
				{"users.name", "`users`.`name`", `"users"."name"`},
				{"users.name AS u_name", "`users`.`name` AS `u_name`", `"users"."name" AS "u_name"`},
				{"count(id)", "count(id)", "count(id)"},
				{"users.*", "`users`.*", `"users".*`},
				{"", "*", "*"},
			} {
				q, _ := NewQuery[MySQLUser](context.Background())
				q.Select(tc.input)
				var rows []MySQLUser
				preview := db.Model(&MySQLUser{}).Scopes(q.BuildQuery()).Find(&rows)
				if tc.input == "" {
					if !errors.Is(preview.Error, ErrColumnEmpty) {
						t.Fatalf("Select(empty) must reject before SQL: %v", preview.Error)
					}
					continue
				}
				projection := strings.TrimPrefix(strings.SplitN(preview.Statement.SQL.String(), " FROM ", 2)[0], "SELECT ")
				want := tc.mysql
				if dialect.Name() == "postgres" {
					want = tc.postgres
				}
				if preview.Error != nil || projection != want {
					t.Fatalf("Select(%q): projection=%q want=%q err=%v", tc.input, projection, want, preview.Error)
				}
			}
		})
	}
}
