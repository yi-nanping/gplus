//go:build dm

package gplus

import (
	"testing"
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

	t.Run("getQuoteChar_返回双引号", func(t *testing.T) {
		qL, qR := getQuoteChar(db)
		if qL != "\"" {
			t.Errorf("DM qL 应为双引号（锁定小写匹配 dameng migrator 引号 lowercase 建表），实际 %q", qL)
		}
		if qR != "\"" {
			t.Errorf("DM qR 应为双引号，实际 %q", qR)
		}
	})
}
