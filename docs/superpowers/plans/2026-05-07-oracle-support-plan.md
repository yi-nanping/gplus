# v0.8.2 Oracle 数据库支持 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 gplus 库（基于 GORM v1.31.x 的 Go 泛型增强）加 Oracle 12c+ 数据库支持，验证 v0.8.0 alias 体系 + Repository CRUD 在 Oracle 方言下正确工作。

**Architecture:** Build tag (`//go:build oracle`) 隔离测试代码，CI 不变（保持 sqlite + mysql + pg 三方言）；库代码改动局限于 `builder.go: getQuoteChar` 加 oracle 分支返回双引号；新建 4 个 oracle build-tag 测试文件分别承载：setup helper / Dialector 契约断言 / CRUD 集成 / alias 体系。

**Tech Stack:** Go 1.24 / GORM v1.31.1+ / `github.com/godoes/gorm-oracle v1.6.18`（GORM Dialector，社区维护）/ `github.com/sijms/go-ora/v2 v2.9.0`（纯 Go Oracle 驱动，transitive）/ `oracle/database-free:23c-slim`（本地验证 docker 镜像）

**Spec reference:** `docs/superpowers/specs/2026-05-07-oracle-support-design.md`（已经过 brainstorming + 2 轮 4 专家审计 + 13 处修订）

**Verification commands:**
- 默认测试（无 oracle）：`go test -race -count=1 ./...`
- Oracle 测试（需启动 docker）：`go test -tags=oracle -race -count=1 -v ./...`
- 仅 Oracle 测试函数：`go test -tags=oracle -run "^TestOracle_" -v ./...`
- vet：`go vet ./...`
- build：`go build ./...`

**Docker 启动命令（本地 Oracle Free 23c）：**

```powershell
docker run -d --name oracle-free `
  -p 1521:1521 `
  -e ORACLE_PWD=oracle `
  -e ORACLE_CHARACTERSET=AL32UTF8 `
  gvenzl/oracle-free:23-slim
# 等待 ~3 分钟启动
docker logs -f oracle-free  # 看到 "DATABASE IS READY TO USE!" 即可
```

**Local DSN（默认本地用 docker 起的实例）：** `oracle://system:oracle@127.0.0.1:1521/FREEPDB1`

---

## File Structure

| 文件 | 类型 | 职责 | build tag |
|---|---|---|---|
| `go.mod` / `go.sum` | 修改 | 加 `github.com/godoes/gorm-oracle v1.6.18` | 默认 |
| `builder.go` | 修改 | `getQuoteChar` 加 `case "oracle":` 分支返回双引号（**唯一库代码改动**） | 默认 |
| `missing_coverage_test.go` | 修改 | `TestQuoteColumn_Dialects` / `TestGetQuoteChar_Dialects` 加 oracle case | 默认 |
| `oracle_setup_test.go` | **新建** | `defaultOracleDSN` / `setupOracleDB` / `truncateOracleTables` (DROP TABLE PURGE + AutoMigrate) | `//go:build oracle` |
| `oracle_contract_test.go` | **新建** | Dialector 契约断言：`db.Name() == "oracle"` + `getQuoteChar` 返回双引号 | `//go:build oracle` |
| `oracle_integration_test.go` | **新建** | 5 个 CRUD 集成测试（BasicCRUD / Where / OrderGroupHaving / JoinQuery / QuoteColumn） | `//go:build oracle` |
| `alias_oracle_test.go` | **新建** | 3 个 alias 体系测试（自连接 / alias 字段 q.Eq / correlated EXISTS） | `//go:build oracle` |
| `README.md` | 修改 | 方言矩阵加 Oracle + 已知陷阱补 Oracle 限制 | 默认 |
| `CHANGELOG.md` | 修改 | 加 v0.8.2 段 | 默认 |

**不动**：

- `testdb_test.go`（不引入 Oracle driver 到默认编译路径）
- 其他库代码（query.go / update.go / repository.go / alias.go / subquery.go / schema.go / debug.go）
- CI 配置（`.github/workflows/ci.yml`）
- 现有所有测试

---

## Task 1: 依赖 + 测试基建

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `oracle_setup_test.go`

**目的**：引入 `gorm-oracle` Dialector，实现 Oracle 测试基建（setup helper + truncate 策略），但不触发任何代码路径在默认 build 下编译。

- [ ] **Step 1: 启动本地 Oracle Free docker 实例**

```powershell
# 拉取镜像（首次 ~3-4 GB）
docker pull gvenzl/oracle-free:23-slim

# 启动容器
docker run -d --name oracle-free `
  -p 1521:1521 `
  -e ORACLE_PWD=oracle `
  -e ORACLE_CHARACTERSET=AL32UTF8 `
  gvenzl/oracle-free:23-slim

# 等待启动（~2-3 分钟）
docker logs -f oracle-free
```

预期：日志最后出现 `DATABASE IS READY TO USE!`。Ctrl+C 退出 logs 跟踪。

- [ ] **Step 2: 添加 gorm-oracle 依赖**

```powershell
go get github.com/godoes/gorm-oracle
go mod tidy
```

预期：`go.mod` 新增 `require github.com/godoes/gorm-oracle v1.6.18`，`go.sum` 加 transitive deps（`sijms/go-ora/v2`、`emirpasic/gods` 等）。

- [ ] **Step 3: 验证默认 build 不破坏**

```powershell
go build ./...
go vet ./...
go test -race -count=1 ./...
```

预期：全部通过，无新错误。

- [ ] **Step 4: 创建 `oracle_setup_test.go`**

```go
//go:build oracle

package gplus

import (
	"os"
	"testing"

	"gorm.io/driver/oracle"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// defaultOracleDSN 本地开发默认 DSN，CI 通过 TEST_ORACLE_DSN 覆盖。
//
// 警告：仅限本地 Docker 开发。`system` 是 Oracle DBA 权限账户（非 SYS 超级账户但
// 仍可执行所有数据库管理操作），密码 oracle 是 Oracle 23c Free Docker 镜像的默认
// 密码——绝不能用于生产。CI/生产请用 TEST_ORACLE_DSN 提供独立测试账户，且仅授予
// 最小测试权限（CONNECT + RESOURCE 即可）。
const defaultOracleDSN = "oracle://system:oracle@127.0.0.1:1521/FREEPDB1"

// setupOracleDB 与 PG/MySQL 同模式：非泛型，绑定 MySQLUser 复用既有测试 struct。
// 镜像 setupPGDB(t) 风格保持代码组织一致。
//
// 标识符长度自检：MySQLUser → my_sql_users (12 chars)；id/username/age/email
// 字段全部 ≤8 chars——满足 Oracle 12c R1 的 30 字符上限要求。
func setupOracleDB(t *testing.T) (*Repository[int64, MySQLUser], *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("TEST_ORACLE_DSN")
	if dsn == "" {
		dsn = defaultOracleDSN
	}

	db, err := gorm.Open(oracle.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Skipf("Oracle 不可用，跳过集成测试: %v", err)
	}
	applyDBPoolLimits(t, db)

	if err := db.AutoMigrate(&MySQLUser{}); err != nil {
		t.Fatalf("迁移 Oracle 表失败: %v", err)
	}

	repo := NewRepository[int64, MySQLUser](db)
	truncateOracleTables(t, db, &MySQLUser{})
	t.Cleanup(func() { truncateOracleTables(t, db, &MySQLUser{}) })

	return repo, db
}

// truncateOracleTables：DROP TABLE PURGE + AutoMigrate 策略
//
// 决策原因（数据库审计建议）：
//   - Oracle TRUNCATE 不重置 IDENTITY 序列
//   - ALTER TABLE MODIFY IDENTITY 流程复杂
//   - DROP + AutoMigrate 是最可靠的 IDENTITY 重置方式
//
// PURGE 必要性：Oracle DROP TABLE 默认进入回收站（USER_RECYCLEBIN），测试循环
// 反复 DROP 同名表会触发 ORA-38301 或空间耗尽。GORM Migrator().DropTable 不会
// 自动加 PURGE，必须用原生 SQL 显式 PURGE。
func truncateOracleTables(t *testing.T, db *gorm.DB, models ...any) {
	t.Helper()
	for _, m := range models {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(m); err != nil {
			t.Logf("parse table name failed: %v", err)
			continue
		}
		if err := db.Exec("DROP TABLE \"" + stmt.Table + "\" PURGE").Error; err != nil {
			t.Logf("drop table %s warn (可能不存在): %v", stmt.Table, err)
		}
		if err := db.AutoMigrate(m); err != nil {
			t.Fatalf("re-migrate %s failed: %v", stmt.Table, err)
		}
		t.Logf("truncateOracle: drop+migrate OK: %s", stmt.Table)
	}
}
```

- [ ] **Step 5: 验证 oracle build tag 编译过**

```powershell
go build -tags=oracle ./...
go vet -tags=oracle ./...
```

预期：通过，无错误。注意此时还没有 oracle 测试函数，所以 `go test -tags=oracle ./...` 不会跑 Oracle 实测。

- [ ] **Step 6: Commit**

```powershell
git add go.mod go.sum oracle_setup_test.go
git commit -m "test(oracle): 加 gorm-oracle 依赖 + setupOracleDB 基础 helper

引入 github.com/godoes/gorm-oracle v1.6.18 GORM Dialector + 纯 Go 驱动
sijms/go-ora（transitive），开始 Oracle 12c+ 支持验证。

新建 oracle_setup_test.go（//go:build oracle）：
- defaultOracleDSN: 本地 docker DSN（含 SYS 账户安全警告注释）
- setupOracleDB(t): 与 setupPGDB 同模式，非泛型绑定 MySQLUser
- truncateOracleTables: DROP TABLE PURGE + AutoMigrate（避免 USER_RECYCLEBIN 积压）

CI 不变（保持 sqlite + mysql + pg）；默认 build/test 路径行为不变。

下一步: Commit 2 - builder.go: getQuoteChar 加 oracle 分支 + 契约断言"
```

---

## Task 2: 库代码方言适配 + 契约断言

**Files:**
- Modify: `builder.go:228-242`
- Modify: `missing_coverage_test.go`
- Create: `oracle_contract_test.go`

**目的**：让 `getQuoteChar` 在 Oracle dialect 下返回双引号；新建契约测试锁定 Dialector 名称契约（防止上游改名导致库默认分支静默 fallback）。

- [ ] **Step 1: 修改 `builder.go: getQuoteChar` 加 oracle 分支**

打开 `builder.go`，找到第 232-241 行的 switch 语句。

旧代码：

```go
switch db.Name() {
case "postgres", "sqlite":
    return "\"", "\""
case "sqlserver":
    return "[", "]"
case "mysql", "tidb":
    return "`", "`"
default:
    return "", ""
}
```

改为：

```go
switch db.Name() {
case "postgres", "sqlite", "oracle":
    return "\"", "\""
case "sqlserver":
    return "[", "]"
case "mysql", "tidb":
    return "`", "`"
default:
    return "", ""
}
```

- [ ] **Step 2: 修改 `missing_coverage_test.go: TestGetQuoteChar_Dialects` 加 oracle case**

找到 `TestGetQuoteChar_Dialects` 函数（约 missing_coverage_test.go:1231 行附近）。在测试用例切片中加 oracle case：

```go
{name: "Oracle 返回双引号", dialect: "oracle", wantL: "\"", wantR: "\""},
```

具体位置：在现有 postgres / sqlite / mysql / sqlserver 用例中间合理位置插入即可。

- [ ] **Step 3: 修改 `missing_coverage_test.go: TestQuoteColumn_Dialects` 加 oracle case**

找到 `TestQuoteColumn_Dialects` 函数（约 missing_coverage_test.go:590 行附近）。如果该函数用 mock dialector + 测试用例表，加：

```go
{name: "Oracle 双引号", dialect: "oracle", input: "users.name", want: `"users"."name"`},
```

- [ ] **Step 4: 验证默认测试通过**

```powershell
go test -race -count=1 -run "TestGetQuoteChar_Dialects|TestQuoteColumn_Dialects" ./...
```

预期：全部 PASS。

```powershell
go test -race -count=1 ./...
```

预期：所有现有测试不破坏，全部 PASS。

- [ ] **Step 5: 创建 `oracle_contract_test.go`**

```go
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
```

- [ ] **Step 6: 运行 Oracle 契约测试**

```powershell
# 确认 docker oracle-free 在跑
docker ps --filter name=oracle-free

# 跑契约测试
go test -tags=oracle -race -count=1 -run "TestOracleDialectorContract" -v ./...
```

预期：两个子测试全部 PASS。如 fail：
- `DialectorName_是_oracle` fail → gorm-oracle Dialector 返回了非 "oracle" 字符串，需要查 Dialector 源码看实际名字，更新 builder.go switch case
- `getQuoteChar_返回双引号` fail → builder.go switch case 没生效，重检 Step 1

- [ ] **Step 7: 全量回归测试**

```powershell
go test -race -count=1 ./...
go test -tags=oracle -race -count=1 -v ./...
```

预期：默认测试 PASS；oracle tag 测试中契约测试 PASS（其他未实现的测试还没添加）。

- [ ] **Step 8: Commit**

```powershell
git add builder.go missing_coverage_test.go oracle_contract_test.go
git commit -m "feat(oracle): builder.go: getQuoteChar 加 oracle 分支 + 契约测试

库代码改动（唯一一处）：
- builder.go:233 case \"postgres\", \"sqlite\", \"oracle\" 同走双引号分支
  Oracle 与 sqlite/postgres 标识符引号一致，无独立逻辑
- missing_coverage_test.go: TestGetQuoteChar_Dialects/TestQuoteColumn_Dialects
  加 oracle case 覆盖

新建 oracle_contract_test.go（//go:build oracle）：
- TestOracleDialectorContract: 锁定 db.Name() == \"oracle\" 契约
- 锁定 getQuoteChar 返回双引号
- 上游 Dialector 改名时 fail 第一时间暴露

下一步: Commit 3 - oracle_integration_test.go 5 个 CRUD 测试"
```

---

## Task 3: Oracle CRUD 集成测试（5 个）

**Files:**
- Create: `oracle_integration_test.go`

**目的**：镜像 `pg_integration_test.go` 的 5 个测试结构，验证 Repository CRUD 在 Oracle 12c+ 下正确工作。预期会暴露方言差异（CLOB 限制、NULLS 排序、HAVING 别名等），按 spec §4.1 缓解策略调整。

**预期可能暴露的真实问题**：
- 表名/列名 case 处理（Dialector 是否 lowercase 强转）
- `RETURNING "id"` 在 Oracle 12c+ 是否能拿到自增 ID
- `LikeRight` 大小写敏感行为

- [ ] **Step 1: 创建 `oracle_integration_test.go` 骨架 + TestOracle_BasicCRUD**

```go
//go:build oracle

package gplus

import (
	"context"
	"os"
	"testing"

	"gorm.io/driver/oracle"
	"gorm.io/gorm"
)

// TestOracle_BasicCRUD 验证 Oracle 方言下基本 CRUD（镜像 TestPG_BasicCRUD）
func TestOracle_BasicCRUD(t *testing.T) {
	repo, _ := setupOracleDB(t)
	ctx := context.Background()

	alice := MySQLUser{Name: "Alice", Age: 20, Email: "alice@example.com"}
	bob := MySQLUser{Name: "Bob", Age: 25, Email: "bob@example.com"}
	assertError(t, repo.Save(ctx, &alice), false, "Save Alice 应成功")
	assertError(t, repo.Save(ctx, &bob), false, "Save Bob 应成功")

	t.Run("GetById", func(t *testing.T) {
		user, err := repo.GetById(ctx, alice.ID)
		assertError(t, err, false, "GetById 应成功")
		if user.Name != "Alice" {
			t.Errorf("GetById 返回错误记录，Name=%q", user.Name)
		}
	})

	t.Run("List", func(t *testing.T) {
		q, u := NewQuery[MySQLUser](ctx)
		q.Eq(&u.Name, "Bob")
		result, err := repo.List(q)
		assertError(t, err, false, "List 应成功")
		assertEqual(t, 1, len(result), "应找到 1 条记录")
		assertEqual(t, "Bob", result[0].Name, "Name 应为 Bob")
	})

	t.Run("Count", func(t *testing.T) {
		q, _ := NewQuery[MySQLUser](ctx)
		count, err := repo.Count(q)
		assertError(t, err, false, "Count 应成功")
		assertEqual(t, int64(2), count, "Count 应为 2")
	})

	t.Run("UpdateById", func(t *testing.T) {
		alice.Email = "alice_new@example.com"
		assertError(t, repo.UpdateById(ctx, &alice), false, "UpdateById 应成功")
		user, err := repo.GetById(ctx, alice.ID)
		assertError(t, err, false, "更新后 GetById 应成功")
		assertEqual(t, "alice_new@example.com", user.Email, "Email 应已更新")
	})

	t.Run("DeleteById", func(t *testing.T) {
		_, err := repo.DeleteById(ctx, bob.ID)
		assertError(t, err, false, "DeleteById 应成功")
		_, err = repo.GetById(ctx, bob.ID)
		if !IsNotFound(err) {
			t.Error("删除后 GetById 应返回 ErrRecordNotFound")
		}
	})
}
```

- [ ] **Step 2: 运行 TestOracle_BasicCRUD 看是否暴露问题**

```powershell
go test -tags=oracle -race -count=1 -run "TestOracle_BasicCRUD" -v ./...
```

预期：全部 PASS。如有 fail 按以下表查根因后修复：

| Fail 现象 | 根因 | 修复 |
|---|---|---|
| AutoMigrate 失败 ORA-00955 表已存在 | truncate 未执行或 docker 残留 | `docker exec oracle-free sqlplus system/oracle@FREEPDB1 <<< "DROP TABLE my_sql_users CASCADE CONSTRAINTS PURGE; EXIT;"` |
| Save 报 alice.ID 未填 | RETURNING 未生效 | 测试代码改为 `db.Create` 显式拿 ID 或写明 ORA 语义 |
| GetById 找不到（Name 大写不匹配）| Oracle 默认 UPPERCASE 表/列名 | 看 Dialector 行为，spec §4.1 fallback 链处理 |

- [ ] **Step 3: 加 TestOracle_WhereConditions**

在 oracle_integration_test.go 末尾追加：

```go
// TestOracle_WhereConditions 验证各类 WHERE 条件在 Oracle 双引号方言下正确（镜像 TestPG_WhereConditions）
func TestOracle_WhereConditions(t *testing.T) {
	repo, _ := setupOracleDB(t)
	ctx := context.Background()

	seeds := []MySQLUser{
		{Name: "Alpha", Age: 10, Email: "a@test.com"},
		{Name: "Beta", Age: 20, Email: "b@test.com"},
		{Name: "Gamma", Age: 30, Email: "c@test.com"},
		{Name: "Delta", Age: 40, Email: "d@test.com"}, // 注：Oracle '' = NULL，empty email 改用占位避免歧义
	}
	for i := range seeds {
		assertError(t, repo.Save(ctx, &seeds[i]), false, "Save seed 应成功")
	}

	t.Run("Ne", func(t *testing.T) {
		q, u := NewQuery[MySQLUser](ctx)
		q.Ne(&u.Name, "Alpha")
		result, err := repo.List(q)
		assertError(t, err, false, "Ne 应成功")
		if len(result) != 3 {
			t.Errorf("Ne: 期望 3 条，实际 %d 条", len(result))
		}
	})

	t.Run("LikeRight_Prefix", func(t *testing.T) {
		// 用前缀匹配避开 Oracle case-sensitive LIKE
		q, u := NewQuery[MySQLUser](ctx)
		q.LikeRight(&u.Name, "Alp")
		result, err := repo.List(q)
		assertError(t, err, false, "LikeRight 应成功")
		assertEqual(t, 1, len(result), "LikeRight Alp%: 应找到 1 条 (Alpha)")
	})

	t.Run("In", func(t *testing.T) {
		q, u := NewQuery[MySQLUser](ctx)
		q.In(&u.Age, []int{10, 30})
		result, err := repo.List(q)
		assertError(t, err, false, "In 应成功")
		assertEqual(t, 2, len(result), "In: 应找到 2 条")
	})

	t.Run("NotIn", func(t *testing.T) {
		q, u := NewQuery[MySQLUser](ctx)
		q.NotIn(&u.Age, []int{10, 30})
		result, err := repo.List(q)
		assertError(t, err, false, "NotIn 应成功")
		assertEqual(t, 2, len(result), "NotIn: 应找到 2 条")
	})

	t.Run("Between", func(t *testing.T) {
		q, u := NewQuery[MySQLUser](ctx)
		q.Between(&u.Age, 15, 35)
		result, err := repo.List(q)
		assertError(t, err, false, "Between 应成功")
		assertEqual(t, 2, len(result), "Between: 应找到 2 条")
	})

	t.Run("GetOne", func(t *testing.T) {
		q, u := NewQuery[MySQLUser](ctx)
		q.Eq(&u.Name, "Gamma")
		user, err := repo.GetOne(q)
		assertError(t, err, false, "GetOne 应成功")
		assertEqual(t, 30, user.Age, "GetOne age 应为 30")
	})
}
```

注意：未加 `IsNull` 子测试。Oracle `''` 自动转 NULL，IsNull 行为与 MySQL/PG 不同；本期不专项测试空字符串语义（避免与 spec §4.1 冲突）。

- [ ] **Step 4: 运行 TestOracle_WhereConditions**

```powershell
go test -tags=oracle -race -count=1 -run "TestOracle_WhereConditions" -v ./...
```

预期：全部 PASS。

- [ ] **Step 5: 加 TestOracle_OrderGroupHaving**

在文件末尾追加：

```go
// TestOracle_OrderGroupHaving 验证 ORDER BY / GROUP BY / HAVING 在 Oracle 方言下正确
func TestOracle_OrderGroupHaving(t *testing.T) {
	repo, _ := setupOracleDB(t)
	ctx := context.Background()

	seeds := []MySQLUser{
		{Name: "A", Age: 20},
		{Name: "B", Age: 20},
		{Name: "C", Age: 30},
	}
	for i := range seeds {
		assertError(t, repo.Save(ctx, &seeds[i]), false, "Save seed 应成功")
	}

	t.Run("OrderBy_DESC", func(t *testing.T) {
		q, u := NewQuery[MySQLUser](ctx)
		q.Order(&u.Age, false)
		result, err := repo.List(q)
		assertError(t, err, false, "OrderBy 应成功")
		if len(result) > 0 && result[0].Age != 30 {
			t.Errorf("OrderBy DESC: 期望第一条 age=30，实际 %d", result[0].Age)
		}
	})

	t.Run("Page", func(t *testing.T) {
		// Oracle 12c+ 用 FETCH FIRST N ROWS ONLY，GORM Limit/Offset 自动适配
		q, u := NewQuery[MySQLUser](ctx)
		q.Order(&u.Age, true).Limit(2).Offset(0)
		result, err := repo.List(q)
		assertError(t, err, false, "Page 应成功")
		assertEqual(t, 2, len(result), "Limit(2) 应返回 2 条")
	})

	t.Run("GroupBy_Having_RawScan", func(t *testing.T) {
		// HAVING 用 COUNT(*) 而非别名（Oracle 严格 SQL 不支持别名引用，与 PG 一致）
		type row struct {
			Age int `gorm:"column:age"`
			Cnt int `gorm:"column:cnt"`
		}
		var results []row
		err := repo.RawScan(ctx, &results,
			"SELECT age, count(*) AS cnt FROM my_sql_users GROUP BY age HAVING count(*) > ?", 1)
		assertError(t, err, false, "RawScan Group+Having 应成功")
		assertEqual(t, 1, len(results), "Having count>1 应只有 age=20 的组")
		if len(results) > 0 {
			assertEqual(t, 20, results[0].Age, "分组结果 age 应为 20")
		}
	})

	t.Run("UpdateByCond", func(t *testing.T) {
		u, m := NewUpdater[MySQLUser](ctx)
		u.Set(&m.Name, "A_updated").Eq(&m.Name, "A")
		rows, err := repo.UpdateByCond(u)
		assertError(t, err, false, "UpdateByCond 应成功")
		if rows != 1 {
			t.Errorf("UpdateByCond 应更新 1 行，实际 %d 行", rows)
		}
	})

	t.Run("DeleteByCond", func(t *testing.T) {
		q, m := NewQuery[MySQLUser](ctx)
		q.Eq(&m.Name, "C")
		rows, err := repo.DeleteByCond(q)
		assertError(t, err, false, "DeleteByCond 应成功")
		if rows != 1 {
			t.Errorf("DeleteByCond 应删除 1 行，实际 %d 行", rows)
		}
	})
}
```

- [ ] **Step 6: 运行 TestOracle_OrderGroupHaving**

```powershell
go test -tags=oracle -race -count=1 -run "TestOracle_OrderGroupHaving" -v ./...
```

预期：全部 PASS。

- [ ] **Step 7: 加 TestOracle_JoinQuery**

在文件末尾追加：

```go
// TestOracle_JoinQuery 验证 LEFT JOIN ON 条件中双引号转义
func TestOracle_JoinQuery(t *testing.T) {
	repo, _ := setupOracleDB(t)
	ctx := context.Background()

	seeds := []MySQLUser{
		{Name: "JoinUser1", Age: 10},
		{Name: "JoinUser2", Age: 20},
	}
	for i := range seeds {
		assertError(t, repo.Save(ctx, &seeds[i]), false, "Save seed 应成功")
	}

	t.Run("LeftJoin_Self", func(t *testing.T) {
		// 自连接验证 JOIN 语句中列名双引号转义不报错
		q, _ := NewQuery[MySQLUser](ctx)
		q.Eq("my_sql_users.age", 10)
		q.LeftJoin("my_sql_users m2", "my_sql_users.id = m2.id")
		result, err := repo.List(q)
		assertError(t, err, false, "LeftJoin 应成功")
		assertEqual(t, 1, len(result), "LeftJoin 结果应为 1 条")
	})
}
```

- [ ] **Step 8: 运行 TestOracle_JoinQuery**

```powershell
go test -tags=oracle -race -count=1 -run "TestOracle_JoinQuery" -v ./...
```

预期：PASS。如 fail 看 `LeftJoin` 中表别名用法在 Oracle 是否需要 `AS` 关键字（与 MySQL `m2` 不同 Oracle 也接受裸别名）。

- [ ] **Step 9: 加 TestOracle_QuoteColumn**

在文件末尾追加：

```go
// TestOracle_QuoteColumn 直接验证 Oracle 方言下转义符和 quoteColumn 输出
func TestOracle_QuoteColumn(t *testing.T) {
	dsn := os.Getenv("TEST_ORACLE_DSN")
	if dsn == "" {
		dsn = defaultOracleDSN
	}

	db, err := gorm.Open(oracle.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("Oracle 不可用，跳过: %v", err)
	}
	applyDBPoolLimits(t, db)

	t.Run("getQuoteChar_返回双引号", func(t *testing.T) {
		qL, qR := getQuoteChar(db)
		assertEqual(t, `"`, qL, "Oracle qL 应为双引号")
		assertEqual(t, `"`, qR, "Oracle qR 应为双引号")
	})

	cases := []struct {
		input string
		want  string
	}{
		{"name", `"name"`},
		{"users.name", `"users"."name"`},
		{"users.name AS u_name", `"users"."name" AS "u_name"`},
		{"count(id)", "count(id)"},
		{"users.*", `"users".*`},
		{"", ""},
	}

	t.Run("quoteColumn_Oracle方言", func(t *testing.T) {
		qL, qR := getQuoteChar(db)
		for _, c := range cases {
			got := quoteColumn(c.input, qL, qR)
			if got != c.want {
				t.Errorf("quoteColumn(%q) = %q, want %q", c.input, got, c.want)
			}
		}
	})
}
```

- [ ] **Step 10: 运行 TestOracle_QuoteColumn**

```powershell
go test -tags=oracle -race -count=1 -run "TestOracle_QuoteColumn" -v ./...
```

预期：PASS。

- [ ] **Step 11: 全量 Oracle 测试回归**

```powershell
go test -tags=oracle -race -count=1 -v ./...
```

预期：所有 Oracle 测试（契约 2 + CRUD 5）全部 PASS。如个别测试 fail，按 spec §4.1 错误处理表查根因后修复 → 修一个 commit 一次 fix 进当前 commit（不另起新 commit）。

- [ ] **Step 12: 默认测试回归（确保不破坏）**

```powershell
go test -race -count=1 ./...
go vet ./...
```

预期：默认测试 PASS。

- [ ] **Step 13: Commit**

```powershell
git add oracle_integration_test.go
git commit -m "test(oracle): Oracle CRUD 5 个集成测试

新建 oracle_integration_test.go（//go:build oracle），镜像 pg_integration_test.go
五个测试结构：
1. TestOracle_BasicCRUD - Save/GetById/List/Count/UpdateById/DeleteById
2. TestOracle_WhereConditions - Ne/LikeRight前缀/In/NotIn/Between/GetOne
   （未加 IsNull 子测试：Oracle '' = NULL 语义本期不专项测试）
3. TestOracle_OrderGroupHaving - Order/Page/GroupBy+Having/UpdateByCond/DeleteByCond
   （HAVING 用 COUNT(*) 而非别名，Oracle 严格 SQL 与 PG 一致）
4. TestOracle_JoinQuery - LeftJoin 自连接验证双引号转义
5. TestOracle_QuoteColumn - getQuoteChar + quoteColumn 直接验证

本地 docker oracle-free 23c 验证全部 PASS。

下一步: Commit 4 - alias_oracle_test.go 3 个 alias 体系测试"
```

---

## Task 4: alias 体系测试（3 个）

**Files:**
- Create: `alias_oracle_test.go`

**目的**：验证 v0.8.0 alias 体系在 Oracle 方言下生成正确 SQL。镜像 `alias_pg_test.go` 三个核心场景。

- [ ] **Step 1: 创建 `alias_oracle_test.go` 含三个测试**

```go
//go:build oracle

package gplus

import (
	"context"
	"strings"
	"testing"
)

// TestOracle_AliasSelfJoin_LeftJoinAs 验证 v0.8.0 alias 体系自连接在 Oracle 方言下生成正确 SQL。
//
// 期望：
//   - SQL 含 "boss" alias 标识符
//   - SQL 含 LEFT JOIN 关键字
//   - 列引用使用 Oracle 双引号转义（与 SQLite/PG 同分支）
//   - 不含反引号（MySQL 方言）
func TestOracle_AliasSelfJoin_LeftJoinAs(t *testing.T) {
	_, db := setupOracleDB(t)

	q, u := NewQueryAs[MySQLUser](context.Background(), "u")
	boss := As[MySQLUser](q, "boss")
	q.LeftJoinAs(boss, &u.ID, &boss.ID, "")

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("Oracle 自连接 ToSQL 失败: %v", err)
	}

	if !strings.Contains(sql, "boss") {
		t.Errorf("Oracle 自连接 SQL 应包含 boss alias，实际: %s", sql)
	}
	if !strings.Contains(strings.ToUpper(sql), "LEFT JOIN") {
		t.Errorf("Oracle 自连接 SQL 应含 LEFT JOIN，实际: %s", sql)
	}
	if !strings.Contains(sql, `"`) {
		t.Errorf("Oracle 自连接 SQL 应使用双引号转义标识符，实际: %s", sql)
	}
	if strings.Contains(sql, "`") {
		t.Errorf("Oracle SQL 不应含反引号（MySQL 方言），实际: %s", sql)
	}
}

// TestOracle_AliasField_InQEq 验证 alias 字段在 q.Eq 等类型安全方法中可用，
// 且生成的 SQL 在 Oracle 方言下使用双引号转义为 "o"."amount"。
func TestOracle_AliasField_InQEq(t *testing.T) {
	_, db := setupOracleDB(t)
	if err := db.AutoMigrate(&UserWithDelete{}, &Order{}); err != nil {
		t.Fatalf("Oracle AutoMigrate 失败: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE \"orders\" PURGE").Error
		_ = db.Exec("DROP TABLE \"user_with_deletes\" PURGE").Error
	})

	q, u := NewQuery[UserWithDelete](context.Background())
	o := As[Order](q, "o")
	q.LeftJoinAs(o, &o.UserID, &u.ID, "")
	q.Eq(&o.Amount, 100)

	if err := q.GetError(); err != nil {
		t.Fatalf("q.Eq with alias field accumulated error: %v", err)
	}

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("Oracle ToSQL 失败: %v", err)
	}

	// 方言无关断言：脱掉双引号 + 反引号后应含 o.amount
	clean := strings.NewReplacer(`"`, "", "`", "").Replace(sql)
	if !strings.Contains(clean, "o.amount") {
		t.Errorf("Oracle SQL 应含 'o.amount' alias 列引用（脱引号后），实际: %s", sql)
	}
	// Oracle 应有双引号
	if !strings.Contains(sql, `"o"."amount"`) {
		t.Errorf("Oracle SQL 应含双引号转义 \"o\".\"amount\"，实际: %s", sql)
	}
}

// TestOracle_SubQuery_OuterRef_LiteralsRendered 验证 correlated SubQuery 在 Oracle
// 方言下字面量内联与子查询渲染均正确。
//
// 注：q.ToSQL(db) 内部调用 GORM 的 db.ToSQL → Dialector.Explain，会把 :N 占位符
// 替换为字面量（参数内联），所以本测试不测占位符序号 rebase（拿不到原始 stmt.SQL），
// 改测内联后字面量与子查询关键字均出现在结果 SQL 中。
func TestOracle_SubQuery_OuterRef_LiteralsRendered(t *testing.T) {
	_, db := setupOracleDB(t)
	if err := db.AutoMigrate(&UserWithDelete{}, &Order{}); err != nil {
		t.Fatalf("Oracle AutoMigrate 失败: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE \"orders\" PURGE").Error
		_ = db.Exec("DROP TABLE \"user_with_deletes\" PURGE").Error
	})

	// 外层 Eq+Gt + correlated EXISTS（子查询用 &u.ID 做相关引用）
	q, u := NewQuery[UserWithDelete](context.Background())
	q.Eq(&u.Name, "alice").Gt(&u.Age, 18)
	sub, o := SubQuery[Order](q)
	sub.Eq(&o.UserID, &u.ID).Gt(&o.Amount, 50)
	q.Exists(sub)

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("Oracle SubQuery ToSQL 失败: %v", err)
	}

	// 子查询关键字
	if !strings.Contains(strings.ToUpper(sql), "EXISTS") {
		t.Errorf("Oracle SQL 应含 EXISTS，实际: %s", sql)
	}
	// 外层字面量内联
	if !strings.Contains(sql, "'alice'") {
		t.Errorf("Oracle SQL 应含外层字面量 'alice'（参数内联），实际: %s", sql)
	}
	if !strings.Contains(sql, "18") {
		t.Errorf("Oracle SQL 应含外层 age 值 18，实际: %s", sql)
	}
	// 内层字面量内联
	if !strings.Contains(sql, "50") {
		t.Errorf("Oracle SQL 应含子查询 amount 值 50，实际: %s", sql)
	}
	// 双引号转义
	if !strings.Contains(sql, `"`) {
		t.Errorf("Oracle SQL 应使用双引号转义标识符，实际: %s", sql)
	}
}
```

- [ ] **Step 2: 运行 alias_oracle 测试**

```powershell
go test -tags=oracle -race -count=1 -run "TestOracle_(AliasSelfJoin|AliasField|SubQuery)" -v ./...
```

预期：3 个测试全部 PASS。如 fail：
- 自连接 fail → 看 LeftJoinAs 在 Oracle dialect 下生成的 SQL，确认 alias 名是否正确写入
- alias 字段 fail → 看 `&o.Amount` 是否被正确解析为 `"o"."amount"`，可能涉及 quoteColumn 行为
- correlated EXISTS fail → 看 `&u.ID` 跨链引用是否正确，可能涉及 outerQuery 关联

如出现真实库 bug（不是测试问题）：v0.8.2 范围内**不修库**，记录到 spec §11.2 技术债，相关测试 `t.Skip` 并注明原因。

- [ ] **Step 3: 全量 Oracle 测试回归**

```powershell
go test -tags=oracle -race -count=1 -v ./...
```

预期：所有 Oracle 测试（契约 2 + CRUD 5 + alias 3）全部 PASS。

- [ ] **Step 4: 默认测试回归**

```powershell
go test -race -count=1 ./...
```

预期：默认测试 PASS（不受影响）。

- [ ] **Step 5: Commit**

```powershell
git add alias_oracle_test.go
git commit -m "test(oracle): v0.8.0 alias 体系 Oracle 行为锁定（3 个测试）

新建 alias_oracle_test.go（//go:build oracle），3 个核心 SQL 生成测试：

1. TestOracle_AliasSelfJoin_LeftJoinAs
   - 自连接（users JOIN users boss）在 Oracle 方言下生成正确 SQL
   - 双引号转义、LEFT JOIN、不含反引号

2. TestOracle_AliasField_InQEq
   - alias.col 在 q.Eq 下解析为 \"o\".\"amount\"
   - 镜像 query_exists_test.go: TestAliasField_InQEq_Works 的 Oracle 路径

3. TestOracle_SubQuery_OuterRef_LiteralsRendered
   - correlated EXISTS + 字面量内联（GORM ToSQL → Explain 替换 :N 占位符）
   - 镜像 alias_pg_test.go 同名测试的 Oracle 路径

本地 docker oracle-free 23c 验证全部 PASS。

下一步: Commit 5 - README + CHANGELOG + GitHub Release"
```

---

## Task 5: 文档收尾 + 发布

**Files:**
- Modify: `README.md`
- Modify: `CHANGELOG.md`

**目的**：在 README 方言矩阵添加 Oracle、CHANGELOG 加 v0.8.2 段、打 v0.8.2 tag、创建 GitHub Release。

- [ ] **Step 1: 修改 `README.md` 方言矩阵添加 Oracle**

打开 `README.md`，找到"方言支持"章节（约第 663 行附近）。

在表格中添加 Oracle 行（在 PostgreSQL 之后，SQL Server 之前）：

```markdown
| PostgreSQL 16+ | ✅ 完整 | ✓ `postgres:16` service | `getQuoteChar` 返回 `"`；ON CONFLICT 用 `excluded.col` 表达式 |
| Oracle 12c+ | ⚠️ build tag | ✗ 不在 CI（启动慢） | 用 `go test -tags=oracle` 跑；`getQuoteChar` 返回 `"`；详见已知陷阱 |
| SQL Server | ⚠️ 部分 | ✗ | `getQuoteChar` 返回 `[ ]`；未在 CI 验证，alias 体系未实测 |
```

- [ ] **Step 2: 修改 `README.md` 已知方言差异速查添加 Oracle**

在"已知方言差异"部分加 Oracle 条目：

```markdown
- Oracle 限制（详见 spec `docs/superpowers/specs/2026-05-07-oracle-support-design.md`）：
  - `''` 自动转 NULL（致命差异，影响 IsNull / Empty 判断）
  - CLOB/TEXT 字段不能直接 WHERE，所有 string 字段须显式 `gorm:"size:N"` 约束
  - NULLS LAST 排序默认（与 PG 升序相反）
  - RETURNING 仅支持单行（影响 SaveBatch/UpsertBatch，本期 t.Skip）
  - 标识符长度 30/128 字符上限
  - 不支持 ON CONFLICT（用 MERGE INTO）
```

- [ ] **Step 3: 修改 `CHANGELOG.md` 加 v0.8.2 段**

在 `CHANGELOG.md` 顶部 v0.8.1 段之前插入：

```markdown
## [0.8.2] - 2026-05-07

### 新增 (Oracle 12c+ 支持)

- **Oracle 数据库支持**：v0.8.0 alias 体系 + Repository CRUD 在 Oracle 12c+ 下行为锁定
  - GORM Dialector：`github.com/godoes/gorm-oracle v1.6.18`（社区维护，活跃 2024+）
  - Go 驱动：`github.com/sijms/go-ora/v2`（纯 Go，无 Oracle Client C 库依赖）
  - **测试隔离**：`//go:build oracle` build tag，**不进 CI**（Oracle 启动慢，docker 镜像 ~3GB）
  - 跑测命令：`go test -tags=oracle ./...`，需启动本地 docker `gvenzl/oracle-free:23-slim`
- 新建 4 个 oracle build-tag 测试文件（commit `XXX`）：
  - `oracle_setup_test.go`：`setupOracleDB` helper + `truncateOracleTables`（DROP TABLE PURGE + AutoMigrate 策略，避免 Oracle 回收站积压）
  - `oracle_contract_test.go`：Dialector 契约断言（`db.Name() == "oracle"` + `getQuoteChar` 返回双引号）
  - `oracle_integration_test.go`：5 个 CRUD 测试（BasicCRUD / Where / OrderGroupHaving / JoinQuery / QuoteColumn）
  - `alias_oracle_test.go`：3 个 alias 体系测试（自连接 / alias 字段 q.Eq / correlated EXISTS）

### 库代码改动

- **`builder.go: getQuoteChar`** 加 `case "oracle":` 分支返回双引号——Oracle 与 sqlite/postgres 同走双引号分支（**唯一库代码改动**）
- 既有 `TestGetQuoteChar_Dialects` / `TestQuoteColumn_Dialects` 加 oracle case 覆盖

### 已知限制 (Oracle)

- **`''` = NULL**：Oracle 自动把空字符串转 NULL，影响 IsNull / Empty 判断
- **CLOB/TEXT WHERE 限制**：Go `string` 长字段映射成 CLOB 时 `LikeRight`/`In` 报 `ORA-00932`，所有 string 字段须显式 `gorm:"size:N"` 约束
- **NULLS LAST 默认**：升序排序 NULL 排末尾，与 PG/SQLite 相反——含 NULL 列 ORDER BY 结果集顺序不一致
- **RETURNING 仅支持单行**：`SaveBatch`/`UpsertBatch` 走 RETURNING 路径在 Oracle 失败，本期相关测试 `t.Skip`
- **标识符长度上限**：12c R1 30 字符，12c R2+ 128 字符；测试 struct 须满足 ≤30 字符以兼容老版本
- **ON CONFLICT 不支持**：Oracle 用 `MERGE INTO`，gplus `OnConflict` 在 Oracle 下需用户手动改写

### 技术债

- **TD-9**：Oracle 测试无 CI 守护，依赖下游手动跑发现问题
- **TD-10**：第三方 Dialector 维护风险（gorm-oracle 由社区维护，GORM 升级时可能滞后）
- **TD-11**：Oracle 11g 不支持（sequence + trigger 自增、ROWNUM 重写未实现）
- **TD-12**：单模块带可选 driver——下游 `go mod tidy` 写入 sijms/go-ora 等 transitive 到 go.sum（build tag 仅隔离测试编译，不影响 `go mod tidy` 拉取）
- **TD-13**：批量 RETURNING 适配未做（推到 v0.9+）

### 文档

- README 方言矩阵加 Oracle（标注 build tag 跑法）
- README 已知方言差异速查加 Oracle 限制
- spec：`docs/superpowers/specs/2026-05-07-oracle-support-design.md`（经过 brainstorming + 2 轮 4 专家审计）

仅测试基建 + 文档变更（除 `getQuoteChar` 一处分支扩展外），不涉及库代码、API、行为；GORM 版本锁定保持 v1.31.x；`v0.8.0` / `v0.8.1` tag 不受影响。

下一步候选（v0.8.3）：达梦数据库 dm 支持（兼容 Oracle 模式，框架 80% 复用）

---

```

替换里面的 `commit \`XXX\`` 为实际 commit hash（执行时填写）。

- [ ] **Step 4: 验证默认测试通过**

```powershell
go test -race -count=1 ./...
go vet ./...
go build ./...
```

预期：全部 PASS。

- [ ] **Step 5: Commit 文档**

```powershell
git add README.md CHANGELOG.md
git commit -m "docs(oracle): README 方言矩阵加 Oracle + CHANGELOG v0.8.2

README 新增：
- 方言矩阵加 Oracle 12c+（build tag 跑法标注）
- 已知方言差异速查加 Oracle 限制（'' = NULL / CLOB / NULLS LAST /
  RETURNING / 标识符长度 / ON CONFLICT）

CHANGELOG v0.8.2 段：
- 新增段：Oracle 12c+ 支持（4 个 oracle build-tag 测试文件）
- 库代码改动：getQuoteChar 加 oracle 分支（唯一改动）
- 已知限制 + 5 项技术债（TD-9 ~ TD-13）

Commit 1-5 全部完成。

下一步: 推 main + 打 v0.8.2 tag + GitHub Release"
```

- [ ] **Step 6: 推送 main**

```powershell
git push origin main
```

预期：5 个 commit 推送成功。

- [ ] **Step 7: 创建 v0.8.2 annotated tag**

```powershell
git tag -a v0.8.2 -m "v0.8.2 - Oracle 12c+ 支持

新增：
- Oracle 12c+ 支持，使用 godoes/gorm-oracle Dialector + sijms/go-ora 纯 Go 驱动
- 4 个 oracle build-tag 测试文件（setup / contract / integration / alias）
- 8 个 Oracle 测试覆盖 v0.8.0 alias 体系 + Repository CRUD

库代码改动（唯一）：
- builder.go: getQuoteChar 加 oracle 分支返回双引号

已知限制（Oracle 严格 SQL）：
- '' = NULL（致命差异）
- CLOB WHERE 限制
- NULLS LAST 排序默认
- RETURNING 不支持批量
- 标识符长度 30/128 字符上限
- 无 ON CONFLICT（用 MERGE INTO）

测试隔离：build tag (//go:build oracle)，不进 CI；CI 保持 sqlite + mysql + pg
本地验证：go test -tags=oracle ./... + docker gvenzl/oracle-free:23-slim

GORM 锁定 v1.31.x；v0.8.0/v0.8.1 tag 不受影响。"

git push origin v0.8.2
```

预期：tag 推送成功。

- [ ] **Step 8: 创建 GitHub Release**

打开 https://github.com/yi-nanping/gplus/releases/new

- "Choose a tag" 选 `v0.8.2`
- 标题填 `v0.8.2 - Oracle 12c+ 支持`
- 描述填 CHANGELOG v0.8.2 段的内容（移除 commit hash 占位）
- 勾选 ☑ "Set as the latest release"
- 不勾 ☐ "Set as a pre-release"
- 点 "Publish release"

预期：Release 创建成功。

---

## 验收检查（执行完成后逐项核对 spec §8）

按 spec `docs/superpowers/specs/2026-05-07-oracle-support-design.md` §8 验收清单逐项核对：

- [ ] 默认 `go test ./...`（无 oracle tag）通过——0 个 Oracle 测试参与
- [ ] `go test -tags=oracle ./...` 在本地 docker 上 8 个 Oracle 测试全过（契约 2 + CRUD 5 + alias 3）
- [ ] 默认 `go vet ./...` 干净
- [ ] 默认 `go build ./...` 干净
- [ ] CI 跑通（应该完全不受影响）
- [ ] README 方言矩阵更新含 Oracle，标注"build tag 跑法"
- [ ] CHANGELOG v0.8.2 段完整
- [ ] 库代码改动局限于 `builder.go: getQuoteChar` 一处
- [ ] `getQuoteChar` 既有 default 分支 fallback 行为不变（向下兼容）
- [ ] v0.8.0 alias 体系在 Oracle 下生成正确 SQL（双引号 + 字面量内联 + EXISTS）
- [ ] v0.8.2 tag 推送 + GitHub Release 创建为 Latest

---

## 自审记录

写完上述 5 个 task 后做了以下自审：

**1. Spec 覆盖**：spec §1-§9 每段都映射到至少一个 task：
- §1 背景与动机 → 不需要 task（动机说明）
- §2 决策摘要 → 全部 task 覆盖（12c+ / sijms / build tag）
- §3 架构 → Task 1（基建）+ Task 2（库改） + Task 3-4（测试）
- §4 数据流与错误处理 → Task 3 错误处理表内嵌
- §5 测试策略 → Task 3（CRUD）+ Task 4（alias）
- §6 落地计划 → 本 plan 整体（5 个 task）
- §7 风险表 → 风险点已在各 task 的 Step 注释里
- §8 验收清单 → 验收检查段
- §9 范围与债 → 不在 plan 范围（v0.9+）

**2. Placeholder 扫描**：
- Step 11 Task 3 / Step 2 Task 4 提到"如 fail 按 spec §4.1 错误处理表查根因"——这不是 placeholder，是实际错误处理流程
- Step 3 Task 5 `commit \`XXX\`` 是占位符，但说明了"执行时填写实际 commit hash"——可接受
- 其他无 TBD/TODO/FIXME

**3. 类型一致性**：
- `MySQLUser` 在 Task 1 setup helper 中绑定，Task 3 BasicCRUD/WhereConditions 等使用 ✓
- `UserWithDelete` / `Order` 在 Task 4 alias 测试中使用，与项目现有类型一致 ✓
- `applyDBPoolLimits` 复用 testdb_test.go 既有 helper（无 build tag，默认可用） ✓
- `assertEqual` / `assertError` / `IsNotFound` 复用项目既有断言 helper ✓

**4. 已知简化**：
- Task 3 中 IsNull 子测试故意省略（Oracle '' = NULL 差异），与 spec §4.1 一致
- Task 3 中未测试 SaveBatch/UpsertBatch（spec §1.2 RETURNING 批量限制）
- Task 4 中 alias_oracle_test.go 三个测试比 alias_pg_test.go 简化——只保留核心 SQL 生成验证，不做相关 EXISTS 嵌套 3 层（PG 那边已经做过证明 GORM 行为正确）

**5. 工作流约定**：
- 每 task 内部按 bite-sized step 拆分（每步 2-5 分钟）
- Step 末尾的 commit 是原子 commit，不依赖未完成 step
- Task 之间可独立审查（subagent-driven 模式可一 task 一 subagent）
- 本地 docker 失败时回退路径在 spec §4.2 已说明
