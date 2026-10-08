package gplus_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	. "github.com/yi-nanping/gplus"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestDistinctCount_RealDriverSQL(t *testing.T) {
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
			ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{{Column: "tenant_id", Condition: "=", Value: "1"}})
			q, m := NewQuery[auditFixRow](ctx)
			q.SelectRaw("age + ? AS shifted", 5).Select(&m.Name).Distinct().WhereRaw("CAST(? AS CHAR) IS NULL", nil).Page(2, 1)
			var count int64
			preview := db.WithContext(ctx).Model(new(auditFixRow)).Scopes(q.DataRuleBuilder().BuildCount()).Count(&count)
			sql := preview.Statement.SQL.String()
			if preview.Error != nil || !strings.HasPrefix(sql, "SELECT count(*) FROM (SELECT DISTINCT ") || strings.Contains(sql, "LIMIT") || strings.Contains(sql, "OFFSET") {
				t.Fatalf("SQL=%s err=%v", sql, preview.Error)
			}
			if !reflect.DeepEqual(preview.Statement.Vars, []any{5, nil, "1"}) {
				t.Fatalf("SQL=%s vars=%#v", sql, preview.Statement.Vars)
			}
			if dialect.Name() == "postgres" && (!strings.Contains(sql, "$1") || !strings.Contains(sql, "$3")) {
				t.Fatalf("PostgreSQL binding: %s", sql)
			}
		})
	}
}
