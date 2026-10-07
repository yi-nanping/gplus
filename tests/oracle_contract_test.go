//go:build oracle

package gplus_test

import (
	"context"
	"strings"
	"testing"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
)

// TestOracleDialectorContract 锁定 gorm-oracle Dialector 的关键契约：
//   - db.Name() 必须返回 "oracle"（getQuoteChar 依赖此字符串匹配）
//   - getQuoteChar(db) 必须返回空 quoter（避免 ORA-00904，详见 builder.go oracle 分支注释）
//
// 上游 Dialector 升级改名时，本测试 fail 第一时间暴露问题。
func TestOracleDialectorContract(t *testing.T) {
	_, db := setupOracleDB(t)

	t.Run("DialectorName_是_oracle", func(t *testing.T) {
		got := db.Name()
		if got != "oracle" {
			t.Fatalf("Dialector Name 契约破坏：期望 \"oracle\"，实际 %q（上游 Dialector 改名？需同步 builder.go: getQuoteChar 分支）", got)
		}
	})

	t.Run("Query_列名方言契约", func(t *testing.T) {
		q, _ := NewQuery[MySQLUser](context.Background())
		q.Select("users.name")
		var rows []MySQLUser
		preview := db.Session(&gorm.Session{DryRun: true}).Model(&MySQLUser{}).Scopes(q.BuildQuery()).Find(&rows)
		projection := strings.TrimPrefix(strings.SplitN(preview.Statement.SQL.String(), " FROM ", 2)[0], "SELECT ")
		if preview.Error != nil || projection != `users.name` {
			t.Fatalf("query projection=%q err=%v", projection, preview.Error)
		}
	})
}
