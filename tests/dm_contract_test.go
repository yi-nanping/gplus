//go:build dm

package gplus_test

import (
	"context"
	"strings"
	"testing"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
)

// TestDMDialectorContract 锁定 gorm-dameng Dialector 的关键契约：
//   - db.Name() 必须返回 "dm"（getQuoteChar 依赖此字符串匹配）
//   - getQuoteChar(db) 必须返回双引号 quoter（实测决策：dameng migrator 用引号
//     lowercase 建表，需双引号锁定小写匹配；详见 builder.go dm 分支注释）
//
// 守卫入口：必须保持 setupDMDB(t) 调用作为 TEST_DM_REQUIRED 守卫覆盖入口
// （spec §3.5）。后续重构若把契约测试改成不调 setup 的 mock dialector 形式，
// 守卫会失效——届时需在 README 显式说明并加补偿守卫。
//
// 上游 Dialector 升级改名 / 改 migrator 输出大小写策略时，本测试 fail 第一时间暴露问题。
func TestDMDialectorContract(t *testing.T) {
	_, db := setupDMDB(t) // 守卫入口

	t.Run("DialectorName_是_dm", func(t *testing.T) {
		got := db.Name()
		if got != "dm" {
			t.Fatalf("Dialector Name 契约破坏：期望 \"dm\"，实际 %q（上游 Dialector 改名？需同步 builder.go: getQuoteChar 分支字符串 + missing_coverage_test.go dm 子测试 + spec §6 风险表第 1 行）", got)
		}
	})

	t.Run("Query_列名方言契约", func(t *testing.T) {
		q, _ := NewQuery[MySQLUser](context.Background())
		q.Select("users.name")
		var rows []MySQLUser
		preview := db.Session(&gorm.Session{DryRun: true}).Model(&MySQLUser{}).Scopes(q.BuildQuery()).Find(&rows)
		projection := strings.TrimPrefix(strings.SplitN(preview.Statement.SQL.String(), " FROM ", 2)[0], "SELECT ")
		if preview.Error != nil || projection != `"users"."name"` {
			t.Fatalf("query projection=%q err=%v", projection, preview.Error)
		}
	})
}
