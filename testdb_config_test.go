package gplus

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestDBConfig_Subprocess 隔离 testing.Fatal/Skip 的退出行为；只使用本地 SQLite 或无效配置。
func TestDBConfig_Subprocess(t *testing.T) {
	if os.Getenv("GPLUS_DB_CONFIG_CHILD") != "1" {
		return
	}
	switch os.Getenv("GPLUS_DB_CONFIG_ENTRY") {
	case "mysql":
		openMySQL(t, os.Getenv("TEST_MYSQL_DSN"))
	case "pg":
		openPG(t, os.Getenv("TEST_PG_DSN"))
	default:
		db := openDB(t)
		if db.Name() != "sqlite" {
			t.Fatalf("预期 SQLite，实际 %q", db.Name())
		}
	}
}

func TestDBConfig_Selection(t *testing.T) {
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, selected, mysqlDSN, pgDSN, entry string
		wantFailure                            bool
		wantOutput                             string
	}{
		{name: "invalid_driver", selected: "invalid", wantFailure: true, wantOutput: "不支持的 TEST_DB"},
		{name: "mysql_missing_dsn", selected: "mysql", wantFailure: true, wantOutput: "必须配置 TEST_MYSQL_DSN"},
		{name: "pg_missing_dsn", selected: "pg", wantFailure: true, wantOutput: "必须配置 TEST_PG_DSN"},
		{name: "postgres_missing_dsn", selected: "postgres", wantFailure: true, wantOutput: "必须配置 TEST_PG_DSN"},
		{name: "sqlite_overrides_dsn", selected: "sqlite", mysqlDSN: "invalid-dsn", pgDSN: "invalid-dsn", wantOutput: "PASS"},
		{name: "mysql_invalid_dsn", selected: "mysql", mysqlDSN: "invalid-dsn", wantFailure: true, wantOutput: "连接已配置的 MySQL 测试数据库失败"},
		{name: "pg_invalid_dsn", selected: "pg", pgDSN: "invalid-dsn", wantFailure: true, wantOutput: "连接已配置的 PostgreSQL 测试数据库失败"},
		{name: "optional_mysql", selected: "sqlite", entry: "mysql", wantOutput: "--- SKIP: TestDBConfig_Subprocess"},
		{name: "optional_pg", selected: "sqlite", entry: "pg", wantOutput: "--- SKIP: TestDBConfig_Subprocess"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(program, "-test.run=^TestDBConfig_Subprocess$", "-test.v")
			// 丢弃继承的数据库配置，子进程只能使用本用例提供的无效 DSN。
			for _, env := range os.Environ() {
				key, _, _ := strings.Cut(env, "=")
				switch strings.ToUpper(key) {
				case "TEST_DB", "TEST_MYSQL_DSN", "TEST_PG_DSN", "GPLUS_DB_CONFIG_CHILD", "GPLUS_DB_CONFIG_ENTRY":
					continue
				}
				cmd.Env = append(cmd.Env, env)
			}
			cmd.Env = append(cmd.Env,
				"GPLUS_DB_CONFIG_CHILD=1",
				"GPLUS_DB_CONFIG_ENTRY="+tc.entry,
				"TEST_DB="+tc.selected,
				"TEST_MYSQL_DSN="+tc.mysqlDSN,
				"TEST_PG_DSN="+tc.pgDSN,
			)
			output, err := cmd.CombinedOutput()
			if (err != nil) != tc.wantFailure {
				t.Fatalf("退出错误 %v，预期失败 %v，输出: %s", err, tc.wantFailure, output)
			}
			if !strings.Contains(string(output), tc.wantOutput) {
				t.Fatalf("输出未包含 %q: %s", tc.wantOutput, output)
			}
		})
	}
}
