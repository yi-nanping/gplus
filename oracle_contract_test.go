//go:build oracle

package gplus

import (
	"testing"
)

// TestOracleDialectorContract 锁定 gorm-oracle Dialector 的关键契约：
//   - db.Name() 必须返回 "oracle"（getQuoteChar 依赖此字符串匹配）
//   - getQuoteChar(db) 必须返回双引号
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

	t.Run("getQuoteChar_返回双引号", func(t *testing.T) {
		qL, qR := getQuoteChar(db)
		if qL != "\"" {
			t.Errorf("Oracle qL 应为双引号，实际 %q", qL)
		}
		if qR != "\"" {
			t.Errorf("Oracle qR 应为双引号，实际 %q", qR)
		}
	})
}
