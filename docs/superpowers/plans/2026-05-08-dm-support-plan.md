# v0.8.3 达梦数据库（DM 8）支持 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 gplus 库（基于 GORM v1.31.x 的 Go 泛型增强）加 DM 8 数据库支持（Oracle 兼容模式），验证 v0.8.0 alias 体系 + Repository CRUD 在 DM 方言下正确工作。

**Architecture:** Build tag (`//go:build dm`) 隔离测试代码，CI 不变（保持 sqlite + mysql + pg）；库代码改动局限于 `builder.go: getQuoteChar` 把 `case "oracle":` 合并为 `case "oracle", "dm":` 共用空 quoter 策略；新建 4 个 dm build-tag 测试文件分别承载：setup helper / Dialector 契约断言 / CRUD 集成 / alias 体系。整体路径与 v0.8.2 Oracle 镜像。

**Tech Stack:** Go 1.24 / GORM v1.31.x / `github.com/godoes/gorm-dameng vX.Y.Z`（Task 0 锁定具体版本）/ `gitee.com/chunanyong/dm`（DM 官方纯 Go 驱动，transitive）/ DM 8 docker 镜像（dameng 技术社区 tar 主路径）

**Spec reference:** `docs/superpowers/specs/2026-05-08-dm-support-design.md`（已经过 brainstorming + 2 轮 6 专家审计 + 14 必修修订 + §11 13 项 plan 待定项 + §9 7 项 README 缺口）

**Verification commands:**
- 默认测试（无 dm）：`go test -race -count=1 ./...`
- DM 测试（需启动 docker）：`go test -tags=dm -race -count=1 -v ./...`
- 强制 DM 测试（防 Skip 误报）：`$env:TEST_DM_REQUIRED='1'; go test -tags=dm -race -count=1 -v ./...`
- 双方言并行：`go test -tags="oracle dm" -race -count=1 ./...`（PS 5.1/7 单/双引号皆可）
- vet：`go vet ./...` + `go vet -tags=dm ./...`
- build：`go build ./...` + `go build -tags=dm ./...`

**Docker 运行环境（用户本机：Windows + WSL2，无 Docker Desktop）：**

| 环境 | 命令前缀 |
|---|---|
| WSL2 + Docker Engine | `wsl -d Ubuntu-24.04 -e docker ...` |

下文所有 `docker xxx` 命令均按 WSL wrapper 写。WSL2 mirrored 网络模式下 `5236:5236` 端口在 Windows `localhost:5236` 直接可达，DSN 无需改动。

---

## File Structure

| 文件 | 类型 | 职责 | build tag |
|---|---|---|---|
| `go.mod` / `go.sum` | 修改 | 加 `github.com/godoes/gorm-dameng vX.Y.Z`（具体版本 Task 0 锁定） | 默认 |
| `builder.go` | 修改 | `getQuoteChar` 把 `case "oracle":` 合并为 `case "oracle", "dm":` + 注释泛化（**唯一库代码（非测试）改动**） | 默认 |
| `missing_coverage_test.go` | 修改 | `TestGetQuoteChar_Dialects` 加 dm 子测试（`TestQuoteColumn_Dialects` 不动，与 oracle 一致） | 默认 |
| `dm_setup_test.go` | **新建** | `defaultDMDSN`（空字符串强制显式 DSN）/ `setupDMDB` / `truncateDMTables` (DROP TABLE PURGE + AutoMigrate) | `//go:build dm` |
| `dm_contract_test.go` | **新建** | Dialector 契约：`db.Name() == "dm"` + `getQuoteChar` 返回空 quoter（必经 setupDMDB 入口防绕过守卫） | `//go:build dm` |
| `dm_integration_test.go` | **新建** | 5 个 CRUD 集成测试（BasicCRUD / Where / OrderGroupHaving / JoinQuery / QuoteColumn） | `//go:build dm` |
| `alias_dm_test.go` | **新建** | 3 个 alias 体系测试（自连接 / alias 字段 q.Eq / correlated EXISTS） | `//go:build dm` |
| `README.md` | 修改 | 方言矩阵加 DM + 新增"DM 数据库支持"7 项必含子节 + GOPROXY 提示 | 默认 |
| `CHANGELOG.md` | 修改 | 加 v0.8.3 段（按下游用户阅读优先级排序的 7 子节） | 默认 |

**不动**：

- `testdb_test.go`（不引入 DM driver 到默认编译路径）
- 其他库代码（query.go / update.go / repository.go / alias.go / subquery.go / schema.go / debug.go）
- CI 配置（`.github/workflows/ci.yml`）
- 现有所有测试（包括 v0.8.2 Oracle 测试）

**Commit 序列（5 commit，避免 build 中断）**：

1. `Task 1` deps：`go.mod` + `go.sum`
2. `Task 2` builder + setup + contract：`builder.go` + `missing_coverage_test.go` + `dm_setup_test.go` + `dm_contract_test.go`（同 commit 避免 contract 单独 commit 编译失败）
3. `Task 3` integration：`dm_integration_test.go`
4. `Task 4` alias：`alias_dm_test.go`
5. `Task 5` docs：`README.md` + `CHANGELOG.md`

最后 `Task 6`：tag v0.8.3 + push GitHub。

---

## Task 0: 环境前置检查 + 13 项 plan 待定项探测

**目的**：spec §11 列出 13 项必须 plan 阶段实测后才能定型的待定项，本 task 集中解决全部，写入本 plan 后续 task 替换占位符。

**不产生 commit**——本 task 只输出"事实表"，更新本 plan 文件。

- [ ] **Step 1: 环境前置检查**

```powershell
# 检查 Go 工具链
go version  # 期望 go1.24.x

# 检查 WSL2 + Ubuntu-24.04
wsl -l -v  # 期望 Ubuntu-24.04 列表 STATE=Running

# 检查 WSL 内 docker engine
wsl -d Ubuntu-24.04 -e docker version  # 期望 Server 段显示版本号

# 检查 docker 网络可达
wsl -d Ubuntu-24.04 -e docker run --rm hello-world  # 期望 "Hello from Docker!"

# 检查 eco.dameng.com 可访问
curl -I https://eco.dameng.com  # 期望 HTTP 200/301/302
```

预期：全部通过。任一失败需先解决环境问题。记录 docker 版本号到 plan 备注。

- [ ] **Step 2: 配置 GOPROXY（作者本地实施前置）**

```powershell
go env -w GOPROXY=https://goproxy.cn,direct
go env -w GOPRIVATE=gitee.com/*
go env GOPROXY  # 验证
```

预期：GOPROXY=`https://goproxy.cn,direct`。否则 Step 3 的 `go list` 卡 proxy.golang.org 超时。

- [ ] **Step 3: 锁定 godoes/gorm-dameng 版本号（13 项 #1）**

```powershell
go list -m -versions github.com/godoes/gorm-dameng
```

预期：输出版本列表（如 `v1.0.0 v1.1.0 ...`）。**记录最新稳定版**到 plan：`__锁定版本：v?.?.?__`（在 Task 1 Step 2 用此版本）。

若返回 "no matching versions"，改用：

```powershell
# 浏览器访问 https://github.com/godoes/gorm-dameng/releases 看最新 release
# 然后用具体版本：go get github.com/godoes/gorm-dameng@vX.Y.Z
```

- [ ] **Step 4: 验证 godoes/gorm-dameng 是否真无 cgo（13 项 #9）**

```powershell
go list -deps github.com/godoes/gorm-dameng | findstr /I cgo
go list -f '{{.Imports}}' github.com/godoes/gorm-dameng
```

预期：无 cgo 依赖输出。若有 cgo，spec §5.2 "纯 Go 无 cgo" 断言不成立，需 abort 当前路径并改用 `gitee.com/chunanyong/dm` 直接 + 自写 GORM Dialector（重大 scope 变更）。

- [ ] **Step 5: 获取 DM 8 docker 镜像（13 项 #2）**

主路径：登录 [eco.dameng.com](https://eco.dameng.com/) → "下载中心" → "Docker 镜像" → 找 DM 8 最新镜像。

```powershell
# 假设 tar 文件已下载到 D:\downloads\dm8.tar
# 复制到 WSL 临时目录
wsl -d Ubuntu-24.04 -e cp /mnt/d/downloads/dm8.tar /tmp/

# load 镜像
wsl -d Ubuntu-24.04 -e docker load -i /tmp/dm8.tar

# 看 image tag
wsl -d Ubuntu-24.04 -e docker images | findstr dm8
```

**记录到 plan**：`__锁定 image tag：dameng/dm8:vX.X.X.X__`（在 Step 6 启动用）。

**Abort 阈值**：若 24h 内拉不到 dameng tar（注册账号 / 网络等），暂停 v0.8.3 推进，改 v0.8.4 优先级或重新评估。

- [ ] **Step 6: 启动 DM 8 容器（13 项 #4 / #13）**

```powershell
# 单行命令（避免续行符不透传）
wsl -d Ubuntu-24.04 -e docker run -d --name dm8 -p 5236:5236 -e INSTANCE_NAME=DM8TEST -e PAGE_SIZE=16 -e UNICODE_FLAG=1 -e CASE_SENSITIVE=Y -e COMPATIBLE_MODE=2 <Step5锁定的image_tag>

# 等待启动（DM 比 Oracle 快，~30s-2min）
wsl -d Ubuntu-24.04 -e docker logs -f dm8
```

预期：日志最后出现 "SYSTEM IS READY" 或类似 ready 标志。

**验证 env 白名单**（13 项 #4）：

```powershell
wsl -d Ubuntu-24.04 -e docker exec dm8 env | findstr /I "INSTANCE PAGE UNICODE CASE COMPATIBLE"
```

记录哪些 env 真实生效到 plan：`__env 白名单：__`。不识别的 env 在 plan Task 1 Step 1 docker 启动命令中删除。

**验证 COMPATIBLE_MODE=2 真生效**（13 项 #13）：

```powershell
# Step 7 探测 SYSDBA 密码后再跑此 SQL
# 暂留占位
```

- [ ] **Step 7: 探测 SYSDBA 默认密码（13 项 #3）**

```powershell
# 进入容器
wsl -d Ubuntu-24.04 -e docker exec -it dm8 bash

# 容器内：尝试 disql 不同密码
disql SYSDBA/SYSDBA@localhost:5236     # 试 SYSDBA
disql SYSDBA/SYSDBA001@localhost:5236  # 试 SYSDBA001
disql SYSDBA/dameng123@localhost:5236  # 部分版本
```

成功后**记录到 plan**：`__锁定密码：____`（写入 README §9.1 第 2 项 DSN 样例）。

若提示首登强制改密：

```sql
-- 容器内 disql 中执行
ALTER USER SYSDBA IDENTIFIED BY 'TestDM8_test123';
EXIT;
```

记录新密码到 plan。

- [ ] **Step 8: 验证 COMPATIBLE_MODE=2 生效**

```sql
-- 容器内 disql 中执行（用 Step 7 锁定的密码）
disql SYSDBA/<密码>@localhost:5236

-- DM 兼容模式诊断 SQL
SELECT PARA_VALUE FROM V$DM_INI WHERE PARA_NAME='COMPATIBLE_MODE';
```

预期：返回 `2`（Oracle 兼容）。若不是 2，docker run 加显式 env 或重建容器。

- [ ] **Step 9: 探测 db.Name() 实际返回值（13 项 #5）**

新建临时探测脚本（不入版本控制）：

```powershell
# Windows 路径
mkdir D:\tmp\dm-probe
cd D:\tmp\dm-probe
go mod init probe
go get github.com/godoes/gorm-dameng@<Step3锁定版本>
```

写 `probe.go`：

```go
package main

import (
	"fmt"
	"log"

	dameng "github.com/godoes/gorm-dameng"
	"gorm.io/gorm"
)

func main() {
	dsn := "dm://SYSDBA:<Step7密码>@127.0.0.1:5236"
	db, err := gorm.Open(dameng.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("connect failed: %v", err)
	}
	fmt.Printf("db.Name() = %q\n", db.Name())
}
```

运行：

```powershell
go run probe.go
```

预期输出：`db.Name() = "dm"`（spec 假设值）。

**记录到 plan**：`__db.Name() 实测值：__`。

**若不是 "dm"**（如返回 `dameng` / `DM`），**触发连锁修改清单**（13 项 #10）：

1. `builder.go` 第 239 行 case 字符串改为实测值
2. `missing_coverage_test.go` dm 子测试 testMockDialector 入参改为实测值
3. `dm_contract_test.go` 断言改为实测值
4. spec §4.1 注释、§3.5 contract 测试断言、§6 风险表第 1 行同步更新
5. `oracle, dm` case 合并改为 `oracle, <实测值>`

清理：删除 `D:\tmp\dm-probe` 目录。

- [ ] **Step 10: MySQLUser 字段保留字预查（13 项 #6）**

容器内 disql 执行：

```sql
-- 查 DM 8 保留字字典
SELECT KEYWORD FROM V$RESERVED_WORDS WHERE KEYWORD IN ('NAME', 'AGE', 'EMAIL', 'ID', 'USERNAME');
```

预期：返回空集（这些字段不撞保留字）。

若有命中：

- 命中 `NAME` / `AGE` / `EMAIL` 任一 → Task 1 不复用 `MySQLUser`，新建 `dmUser` struct（字段加 `gorm:"column:..."` 避开保留字），写入本 plan 备注。
- 全部不命中 → 直接复用 `MySQLUser`，按 spec 路径走。

**记录到 plan**：`__保留字命中：____`。

- [ ] **Step 11: 检查 testMockDialector 已存在**

```powershell
# 用 Grep 验证（非 grep 命令，省略此处）
# 期望：missing_coverage_test.go:1219 行已定义 testMockDialector
```

预期：已存在（spec 已确认）。Task 2 直接复用，不需新增 mock。

- [ ] **Step 12: 写入 plan 待定项汇总表**

把 Step 3 / 5 / 6 / 7 / 9 / 10 的实测值写入本文件顶部备注（在 "Tech Stack" 后加一段 "## Plan 阶段实测值"）。

**Task 0 结束验收**：
- [ ] 13 项待定项全部有具体值或明确处理路径
- [ ] DM 容器启动并就绪，db.Name() 实测值已记录
- [ ] 不产生 commit（仅更新 plan 文档）

---

## Task 1: 依赖（commit 1）

**Files:**
- Modify: `go.mod`, `go.sum`

**目的**：引入 `gorm-dameng` Dialector，验证默认 build 不破坏。

- [ ] **Step 1: 添加 gorm-dameng 依赖**

```powershell
go get github.com/godoes/gorm-dameng@<Task0_Step3锁定版本>
go mod tidy
```

预期：`go.mod` 新增 `require github.com/godoes/gorm-dameng vX.Y.Z`，`go.sum` 加 transitive deps（`gitee.com/chunanyong/dm` 等）。

- [ ] **Step 2: 验证默认 build 不破坏**

```powershell
go build ./...
go vet ./...
go test -race -count=1 ./...
```

预期：全部通过，无新错误。

- [ ] **Step 3: 验证 dm build tag 编译过**

```powershell
go build -tags=dm ./...
go vet -tags=dm ./...
```

预期：通过（此时还没有 dm 测试函数，所以 `go test -tags=dm ./...` 不会跑实测）。

- [ ] **Step 4: Commit**

```powershell
git add go.mod go.sum
git commit -m "feat(dm): 加 gorm-dameng 依赖

引入 GORM Dialector：godoes/gorm-dameng vX.Y.Z（与 v0.8.2 godoes/gorm-oracle
同作者）。Transitive 引入 gitee.com/chunanyong/dm（DM 官方纯 Go 驱动，无 cgo）。

默认 build 不触及 dameng driver（后续 dm 测试文件用 //go:build dm 隔离），
go test ./... / go vet ./... / go build ./... 全部不变。"
```

预期：commit 创建，工作树干净。

---

## Task 2: builder.go + setup + contract（commit 2）

**Files:**
- Modify: `builder.go:239` (oracle case 合并 dm)
- Modify: `missing_coverage_test.go` (TestGetQuoteChar_Dialects 加 dm 子测试)
- Create: `dm_setup_test.go`
- Create: `dm_contract_test.go`

**目的**：合并方言 case + 完成 setup helper + 锁定 Dialector 契约。这 4 个文件**同一 commit**——契约测试依赖 setup helper，分开 commit 会导致中间状态 build 失败。

- [ ] **Step 1: 修改 `builder.go` getQuoteChar oracle 分支合并 dm**

修改 `builder.go` 第 239-245 行。

修改前：

```go
case "oracle":
    // godoes/gorm-oracle migrator 用 UPPERCASE 不带引号 CREATE TABLE
    // （列名实际存为 USERNAME 等大写），若 quoteColumn 加双引号转义会变 "username"
    // 而 Oracle 双引号下大小写敏感 → ORA-00904 invalid identifier。
    // 这里返回空 quoter，让 Oracle 自身 UPPERCASE 解析裸标识符。
    // 已知陷阱：列名是 Oracle 保留字（order/size/level 等）时需用户手动加引号。
    return "", ""
```

修改后：

```go
case "oracle", "dm":
    // godoes/gorm-{oracle,dameng} migrator 用 UPPERCASE 不带引号 CREATE TABLE
    // （列名实际存为 USERNAME 等大写），若 quoteColumn 加双引号转义会变 "username"
    // 而 Oracle/DM 双引号下大小写敏感 → ORA-00904 invalid identifier。
    // 这里返回空 quoter，让 Oracle/DM 自身 UPPERCASE 解析裸标识符。
    // 已知陷阱：列名是保留字（order/size/level/number/date 等）时需用户手动加引号。
    return "", ""
```

**注意**：若 Task 0 Step 9 探测的 db.Name() 不是 `"dm"`，把 `"dm"` 替换为实测值。

- [ ] **Step 2: 修改 `missing_coverage_test.go` 加 dm 子测试**

在 `TestGetQuoteChar_Dialects` 函数内（约第 1249 行的 oracle 子测试**之后**）追加 dm 子测试。

定位：

```go
t.Run("oracle 方言返回空 quoter 避免 case 冲突", func(t *testing.T) {
    // ... 既有 oracle 子测试
})
```

在其后追加：

```go
t.Run("dm 方言返回空 quoter 与 oracle 共用", func(t *testing.T) {
    // 用 testMockDialector 模拟 DM，避免在默认 build 引入 gorm-dameng 依赖。
    // godoes/gorm-dameng migrator 也走 Oracle 兼容模式 UPPERCASE 不带引号建表，
    // 加双引号转义会触发 ORA-00904 等价错误，因此 dm 与 oracle 共用空 quoter case。
    db := &gorm.DB{Config: &gorm.Config{Dialector: testMockDialector{"dm"}}}
    qL, qR := getQuoteChar(db)
    if qL != "" || qR != "" {
        t.Errorf("dm 期望空字符串，实际 (%q,%q)", qL, qR)
    }
})
```

**注意**：testMockDialector 入参字符串若 Task 0 Step 9 探测值不是 `"dm"`，替换为实测值。

- [ ] **Step 3: 创建 `dm_setup_test.go`**

```go
//go:build dm

package gplus

import (
	"os"
	"testing"

	dameng "github.com/godoes/gorm-dameng"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// defaultDMDSN 故意留空字符串——强制下游必须显式设置 TEST_DM_DSN。
//
// 防自相矛盾策略（spec §3.3）：dameng 镜像默认密码版本差异较大（SYSDBA / SYSDBA001 / 部分
// 版本首登强制改密），spec 写死任一具体密码都会与某些镜像版本不一致导致 connect fail。
// 故 plan 阶段 Task 0 Step 7 实测密码后写入 README 的 TEST_DM_DSN 样例供下游用户参考，
// 而 setup helper 自身不预设默认。
const defaultDMDSN = ""

// setupDMDB 与 setupOracleDB 同模式：非泛型，绑定 MySQLUser 复用既有测试 struct。
//
// 标识符长度自检：MySQLUser → my_sql_users (12 chars)；id/username/age/email
// 字段全部 ≤8 chars——沿用 Oracle 12c R1 的 30 字符上限规范（DM 8 实际 128，
// 但保留与既有 Oracle 测试 struct 一致便于跨方言通用）。
//
// 保留字回避（Task 0 Step 10 已预查）：MySQLUser 字段 name/age/email 不与 DM 8
// Oracle 兼容模式保留字冲突。新增测试字段需主动避开 comment / type / group / role /
// order / size / level / number / date 等 DM/Oracle 共用保留字（空 quoter 策略下
// gplus 不会自动加引号，TD-14）。
//
// 不前置 AutoMigrate（spec §3.3）：直接走 truncateDMTables 的 DROP+AutoMigrate 路径
// 建表。沿用 v0.8.2 Oracle commit 7627ea6 的修订决策——godoes/gorm-dameng migrator
// 也假定走 Oracle 兼容路径，已存在表 ALTER ADD 极可能报 ORA-01430 column already
// exists 等价错误，必须先 DROP 再 CREATE 才能保证从干净状态开始。
//
// Skip 误报防护（spec §3.2）：TEST_DM_REQUIRED=1 时，DSN 未设或连接失败均改 t.Fatalf
// 避免 exit 0 误报。作者本地实施 / 未来 CI 引用 setup helper 时设此 env。
func setupDMDB(t *testing.T) (*Repository[int64, MySQLUser], *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("TEST_DM_DSN")
	if dsn == "" {
		dsn = defaultDMDSN
	}
	if dsn == "" {
		if os.Getenv("TEST_DM_REQUIRED") == "1" {
			t.Fatalf("TEST_DM_DSN 未设置但 TEST_DM_REQUIRED=1，DM 实测被强制要求")
		}
		t.Skip("TEST_DM_DSN 未设置，跳过 DM 测试（参见 README DM 数据库支持章节）")
	}

	db, err := gorm.Open(dameng.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		if os.Getenv("TEST_DM_REQUIRED") == "1" {
			t.Fatalf("DM 强制要求但不可用: %v", err)
		}
		t.Skipf("DM 不可用，跳过集成测试: %v", err)
	}
	applyDBPoolLimits(t, db)

	repo := NewRepository[int64, MySQLUser](db)
	truncateDMTables(t, db, &MySQLUser{})
	t.Cleanup(func() { truncateDMTables(t, db, &MySQLUser{}) })

	return repo, db
}

// truncateDMTables：DROP TABLE PURGE + AutoMigrate 策略
//
// 决策原因（沿用 Oracle 路径）：
//   - DM Oracle 兼容模式 TRUNCATE 不重置 IDENTITY 序列
//   - ALTER TABLE MODIFY IDENTITY 流程复杂
//   - DROP + AutoMigrate 是最可靠的 IDENTITY 重置方式
//
// PURGE 子句：DM 8 已确认支持 `DROP TABLE X PURGE` 语法（与 Oracle 兼容），
// 且有回收站机制（SF_RECYCLE_BIN_* 系列函数）。直接沿用 Oracle 路径无需修改。
func truncateDMTables(t *testing.T, db *gorm.DB, models ...any) {
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
		t.Logf("truncateDM: drop+migrate OK: %s", stmt.Table)
	}
}
```

**注意**：若 Task 0 Step 4 验证发现 godoes/gorm-dameng 不可用或 import 路径不同，调整 import 行。

- [ ] **Step 4: 创建 `dm_contract_test.go`**

```go
//go:build dm

package gplus

import (
	"testing"
)

// TestDMDialectorContract 锁定 gorm-dameng Dialector 的关键契约：
//   - db.Name() 必须返回 "dm"（getQuoteChar 依赖此字符串匹配）
//   - getQuoteChar(db) 必须返回空 quoter（避免 ORA-00904 等价错误，详见 builder.go dm 分支注释）
//
// 守卫入口：必须保持 setupDMDB(t) 调用作为 TEST_DM_REQUIRED 守卫覆盖入口
// （spec §3.5）。后续重构若把契约测试改成不调 setup 的 mock dialector 形式，
// 守卫会失效——届时需在 README 显式说明并加补偿守卫。
//
// 上游 Dialector 升级改名时，本测试 fail 第一时间暴露问题。
func TestDMDialectorContract(t *testing.T) {
	_, db := setupDMDB(t) // 守卫入口

	t.Run("DialectorName_是_dm", func(t *testing.T) {
		got := db.Name()
		if got != "dm" {
			t.Fatalf("Dialector Name 契约破坏：期望 \"dm\"，实际 %q（上游 Dialector 改名？需同步 builder.go: getQuoteChar 分支字符串 + missing_coverage_test.go dm 子测试 + spec §6 风险表第 1 行）", got)
		}
	})

	t.Run("getQuoteChar_返回空_quoter", func(t *testing.T) {
		qL, qR := getQuoteChar(db)
		if qL != "" {
			t.Errorf("DM qL 应为空字符串（避免 ORA-00904 大小写冲突），实际 %q", qL)
		}
		if qR != "" {
			t.Errorf("DM qR 应为空字符串，实际 %q", qR)
		}
	})
}
```

**注意**：若 Task 0 Step 9 db.Name() 实测值不是 `"dm"`，把 `"dm"` 替换为实测值（DialectorName_是_xxx 子测试名也同步改）。

- [ ] **Step 5: 验证默认 build 不破坏**

```powershell
go build ./...
go vet ./...
go test -race -count=1 ./...
```

预期：全部通过，无新错误。

- [ ] **Step 6: 验证 dm build tag 通过**

```powershell
go build -tags=dm ./...
go vet -tags=dm ./...
```

预期：通过。

- [ ] **Step 7: 跑 dm 测试（要求 docker 已起）**

```powershell
$env:TEST_DM_DSN="dm://SYSDBA:<Task0_Step7密码>@127.0.0.1:5236"
$env:TEST_DM_REQUIRED="1"
go test -tags=dm -race -count=1 -run "^TestDMDialectorContract$" -v ./...
```

预期：`TestDMDialectorContract` 含 2 个子测试 `DialectorName_是_dm` + `getQuoteChar_返回空_quoter` 全部 PASS。

若 `DialectorName_是_dm` fail：

1. 把 fail 输出的实际值记到 Task 0 Step 9 的探测表
2. 触发"db.Name() 连锁修改清单"（spec §11 #10）：改 5 处字符串，重跑测试
3. **不要直接改测试常量绕过失败**——这是契约测试，fail 表示 builder.go case 也错了

- [ ] **Step 8: 验证 missing_coverage 默认 build 测试**

```powershell
go test -race -count=1 -run "^TestGetQuoteChar_Dialects$" -v ./...
```

预期：含 dm 子测试 `dm 方言返回空 quoter 与 oracle 共用` PASS。

- [ ] **Step 9: Commit**

```powershell
git add builder.go missing_coverage_test.go dm_setup_test.go dm_contract_test.go
git commit -m "feat(dm): builder.go case 合并 + 测试基建 + 契约锁定

builder.go: getQuoteChar 把 case oracle 合并为 case oracle, dm 共用空 quoter
策略（spec §4.1）。godoes/gorm-dameng migrator 也走 Oracle 兼容 UPPERCASE 不
带引号建表，与 Oracle 同走空 quoter 避免 ORA-00904 等价错误。注释泛化为
oracle/DM。这是 v0.8.3 唯一库代码（非测试）改动。

测试基建：
- missing_coverage_test.go: TestGetQuoteChar_Dialects 加 dm 子测试，用既有
  testMockDialector 模拟 dm 方言名（不引入 dameng driver 到默认 build）。
  TestQuoteColumn_Dialects 不动（与 oracle 一致——表驱动直接喂 quoter 字符
  不走 dialect 分支）。
- dm_setup_test.go: setupDMDB 与 setupOracleDB 同模式，复用 MySQLUser 与
  applyDBPoolLimits helper。defaultDMDSN 故意留空强制显式 TEST_DM_DSN，避免
  写死的密码与镜像版本不一致导致 connect fail。Skip 误报防护：TEST_DM_REQUIRED=1
  时 t.Fatalf 升级。truncateDMTables 走 DROP TABLE PURGE + AutoMigrate（沿用
  Oracle commit 7627ea6 决策，DM 8 已确认支持 PURGE 子句）。
- dm_contract_test.go: 锁定 db.Name() 契约 + getQuoteChar 空 quoter 契约。
  入口必经 setupDMDB 调用作为 TEST_DM_REQUIRED 守卫覆盖入口。

setup + contract 同 commit 避免 contract 测试单独 commit 时 build 失败。"
```

预期：commit 创建。

---

## Task 3: integration 5 个 CRUD 测试（commit 3）

**Files:**
- Create: `dm_integration_test.go`

**目的**：验证 v0.8.0 alias 体系 + Repository CRUD 在 DM 8 Oracle 兼容模式下正确。覆盖 BasicCRUD / Where / OrderGroupHaving / JoinQuery / QuoteColumn 5 个测试，9 测试中的 5 个。

- [ ] **Step 1: 创建 `dm_integration_test.go`**

```go
//go:build dm

package gplus

import (
	"context"
	"os"
	"testing"

	dameng "github.com/godoes/gorm-dameng"
	"gorm.io/gorm"
)

// TestDM_BasicCRUD 验证 DM 方言下基本 CRUD（镜像 TestOracle_BasicCRUD）
func TestDM_BasicCRUD(t *testing.T) {
	repo, _ := setupDMDB(t)
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

// TestDM_WhereConditions 验证各类 WHERE 条件在 DM 方言下正确（镜像 TestOracle_WhereConditions）
//
// 不含 IsNull——沿用 Oracle 实测决策：DM Oracle 兼容模式 ''=NULL 语义下 IsNull 测试不可靠。
func TestDM_WhereConditions(t *testing.T) {
	repo, _ := setupDMDB(t)
	ctx := context.Background()

	seeds := []MySQLUser{
		{Name: "Alpha", Age: 10, Email: "a@test.com"},
		{Name: "Beta", Age: 20, Email: "b@test.com"},
		{Name: "Gamma", Age: 30, Email: "c@test.com"},
		{Name: "Delta", Age: 40, Email: "d@test.com"}, // DM ''=NULL，empty email 改占位避免歧义
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
		// 用前缀匹配避开 DM/Oracle case-sensitive LIKE
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

// TestDM_OrderGroupHaving 验证 ORDER BY / GROUP BY / HAVING 在 DM 方言下正确
func TestDM_OrderGroupHaving(t *testing.T) {
	repo, _ := setupDMDB(t)
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
		// DM 8 Oracle 兼容模式用 FETCH FIRST N ROWS ONLY，GORM Limit/Offset 自动适配
		q, u := NewQuery[MySQLUser](ctx)
		q.Order(&u.Age, true).Limit(2).Offset(0)
		result, err := repo.List(q)
		assertError(t, err, false, "Page 应成功")
		assertEqual(t, 2, len(result), "Limit(2) 应返回 2 条")
	})

	t.Run("GroupBy_Having_RawScan", func(t *testing.T) {
		// HAVING 用 COUNT(*) 而非别名（DM/Oracle 严格 SQL 不支持别名引用）
		// alias 加双引号 "age"/"cnt" 锁定 lowercase 输出列名，与 struct tag 对齐
		// （DM Oracle 兼容模式不带引号的列名默认输出 UPPERCASE，会与小写 struct tag 不匹配）
		type row struct {
			Age int `gorm:"column:age"`
			Cnt int `gorm:"column:cnt"`
		}
		var results []row
		err := repo.RawScan(ctx, &results,
			`SELECT age AS "age", count(*) AS "cnt" FROM my_sql_users GROUP BY age HAVING count(*) > ?`, 1)
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

// TestDM_JoinQuery 验证 LEFT JOIN ON 条件在 DM 方言下正确（镜像 TestOracle_JoinQuery）
func TestDM_JoinQuery(t *testing.T) {
	repo, _ := setupDMDB(t)
	ctx := context.Background()

	seeds := []MySQLUser{
		{Name: "JoinUser1", Age: 10},
		{Name: "JoinUser2", Age: 20},
	}
	for i := range seeds {
		assertError(t, repo.Save(ctx, &seeds[i]), false, "Save seed 应成功")
	}

	t.Run("LeftJoin_Self", func(t *testing.T) {
		// 自连接验证 JOIN 语句中列名转义不报错（DM 走空 quoter，与 Oracle 一致）
		q, _ := NewQuery[MySQLUser](ctx)
		q.Eq("my_sql_users.age", 10)
		q.LeftJoin("my_sql_users m2", "my_sql_users.id = m2.id")
		result, err := repo.List(q)
		assertError(t, err, false, "LeftJoin 应成功")
		assertEqual(t, 1, len(result), "LeftJoin 结果应为 1 条")
	})
}

// TestDM_QuoteColumn 直接验证 DM 方言下转义符和 quoteColumn 输出
func TestDM_QuoteColumn(t *testing.T) {
	dsn := os.Getenv("TEST_DM_DSN")
	if dsn == "" {
		t.Skip("TEST_DM_DSN 未设置，跳过")
	}

	db, err := gorm.Open(dameng.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("DM 不可用，跳过: %v", err)
	}
	applyDBPoolLimits(t, db)

	t.Run("getQuoteChar_返回空_quoter", func(t *testing.T) {
		qL, qR := getQuoteChar(db)
		assertEqual(t, "", qL, "DM qL 应为空字符串（避免 ORA-00904）")
		assertEqual(t, "", qR, "DM qR 应为空字符串")
	})

	// 空 quoter 下 quoteColumn 输出原样（让 DM 自身 UPPERCASE 解析裸标识符）
	cases := []struct {
		input string
		want  string
	}{
		{"name", "name"},
		{"users.name", "users.name"},
		{"users.name AS u_name", "users.name AS u_name"},
		{"count(id)", "count(id)"},
		{"users.*", "users.*"},
		{"", ""},
	}

	t.Run("quoteColumn_DM方言", func(t *testing.T) {
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

- [ ] **Step 2: 跑 5 个 integration 测试**

```powershell
$env:TEST_DM_DSN="dm://SYSDBA:<Task0_Step7密码>@127.0.0.1:5236"
$env:TEST_DM_REQUIRED="1"
go test -tags=dm -race -count=1 -run "^TestDM_(BasicCRUD|WhereConditions|OrderGroupHaving|JoinQuery|QuoteColumn)$" -v ./...
```

预期：5 个测试函数共 ~17 子测试全部 PASS。

**典型 fail 排查**：

- `ORA-00932` 等价错误（CLOB）→ Task 0 Step 10 没查到的字段需要 `gorm:"size:N"` 显式约束
- `ORA-00904` 等价错误（invalid identifier）→ Task 2 Step 1 builder.go 改动可能没生效，重新检查 case 字符串是否对得上 db.Name() 实测值
- DELETE 0 行（`DeleteByCond`）→ truncateDMTables 没生效，检查 PURGE 子句和 AutoMigrate 调用顺序

- [ ] **Step 3: 验证默认 build 不破坏**

```powershell
go build ./...
go vet ./...
go test -race -count=1 ./...
```

预期：默认 build 测试不变（dm 测试因 build tag 不参与）。

- [ ] **Step 4: Commit**

```powershell
git add dm_integration_test.go
git commit -m "test(dm): DM 8 集成测试 5 个 CRUD（镜像 v0.8.2 Oracle 路径）

5 个测试函数（共 ~17 子测试），与 oracle_integration_test.go 一一对应：

- TestDM_BasicCRUD: Save / GetById / List / Count / UpdateById / DeleteById
- TestDM_WhereConditions: Ne / LikeRight 前缀 / In / NotIn / Between / GetOne
  （不含 IsNull——沿用 Oracle 实测决策：DM Oracle 兼容模式 ''=NULL 语义下
  IsNull 测试不可靠）
- TestDM_OrderGroupHaving: OrderBy DESC / Page (FETCH FIRST) / GroupBy+Having
  RawScan（用 AS \"col\" 锁定 lowercase 输出）/ UpdateByCond / DeleteByCond
- TestDM_JoinQuery: LeftJoin_Self（自连接 + ON 条件转义）
- TestDM_QuoteColumn: 空 quoter 下 quoteColumn 输出原样（spec §3.5）

CRUD 全过验证 v0.8.0 alias 体系 + Repository 在 DM 方言下零库代码 bug。"
```

预期：commit 创建。

---

## Task 4: alias 3 个测试（commit 4）

**Files:**
- Create: `alias_dm_test.go`

**目的**：锁定 v0.8.0 alias 体系（自连接 / alias 字段 q.Eq / correlated EXISTS）在 DM 方言下行为，9 测试中的最后 3 个。

- [ ] **Step 1: 创建 `alias_dm_test.go`**

```go
//go:build dm

package gplus

import (
	"context"
	"strings"
	"testing"
)

// TestDM_AliasSelfJoin_LeftJoinAs 验证 alias 自连接 SQL 生成在 DM 方言下正确
// 镜像 TestOracle_AliasSelfJoin_LeftJoinAs。
func TestDM_AliasSelfJoin_LeftJoinAs(t *testing.T) {
	repo, _ := setupDMDB(t)
	ctx := context.Background()

	seeds := []MySQLUser{
		{Name: "Parent", Age: 30},
		{Name: "Child", Age: 10},
	}
	for i := range seeds {
		assertError(t, repo.Save(ctx, &seeds[i]), false, "Save seed 应成功")
	}

	q, u := NewQuery[MySQLUser](ctx)
	o := NewAlias[MySQLUser]("o")  // alias o
	q.LeftJoinAs(o, "o.age < my_sql_users.age").Eq(&u.Name, "Parent")

	// 这里仅断言 SQL 生成正确（不依赖结果集），方言无关
	sql, _ := q.DataRuleBuilder().BuildQuery().DryRunSQL(repo.DB(), &MySQLUser{})
	stripped := stripQuotes(sql)
	if !strings.Contains(stripped, "LEFT JOIN my_sql_users o") {
		t.Errorf("LeftJoinAs 在 DM 方言下应生成 LEFT JOIN my_sql_users o，实际 SQL: %s", sql)
	}
}

// TestDM_AliasField_InQEq 验证 q.Eq(&alias.Field) 在 DM 方言下行为
// 镜像 TestOracle_AliasField_InQEq。
func TestDM_AliasField_InQEq(t *testing.T) {
	repo, _ := setupDMDB(t)
	ctx := context.Background()

	q, _ := NewQuery[MySQLUser](ctx)
	o := NewAlias[MySQLUser]("o")
	q.LeftJoinAs(o, "o.id = my_sql_users.id").Eq(&o.Age, 25)

	sql, _ := q.DataRuleBuilder().BuildQuery().DryRunSQL(repo.DB(), &MySQLUser{})
	stripped := stripQuotes(sql)
	if !strings.Contains(stripped, "o.age") {
		t.Errorf("Eq(&alias.Age) 应生成 o.age，实际 SQL: %s", sql)
	}
}

// TestDM_SubQuery_OuterRef 验证 correlated EXISTS 子查询在 DM 方言下行为
// 镜像 TestOracle_SubQuery_OuterRef。
func TestDM_SubQuery_OuterRef(t *testing.T) {
	repo, _ := setupDMDB(t)
	ctx := context.Background()

	seeds := []MySQLUser{
		{Name: "ExistsUser", Age: 50},
	}
	for i := range seeds {
		assertError(t, repo.Save(ctx, &seeds[i]), false, "Save seed 应成功")
	}

	q, u := NewQuery[MySQLUser](ctx)
	o := NewAlias[MySQLUser]("o")
	subq, _ := NewQuery[MySQLUser](ctx)
	subq.WhereRaw("o.id = my_sql_users.id")
	q.LeftJoinAs(o, "1=1").Exists(subq.DataRuleBuilder()).Eq(&u.Name, "ExistsUser")

	result, err := repo.List(q)
	assertError(t, err, false, "correlated EXISTS 应成功")
	if len(result) != 1 {
		t.Errorf("correlated EXISTS: 期望 1 条 (ExistsUser)，实际 %d 条", len(result))
	}
}

// stripQuotes 去除 SQL 中所有引号字符（双引号/反引号/方括号），用于方言无关断言
// 镜像 alias_oracle_test.go 同名 helper。
func stripQuotes(s string) string {
	r := strings.NewReplacer(`"`, "", "`", "", "[", "", "]", "")
	return r.Replace(s)
}
```

**注意**：若 `alias_oracle_test.go` 已经定义 `stripQuotes`，本文件不需要重复定义（同 package 内会冲突）。Task 4 Step 2 编译时会报错暴露这个问题。

- [ ] **Step 2: 编译验证**

```powershell
go build -tags=dm ./...
```

预期：通过。若 `stripQuotes` redeclared 错误，从 `alias_dm_test.go` 中删除该函数定义（沿用 oracle 测试文件已有的）。

- [ ] **Step 3: 跑 3 个 alias 测试**

```powershell
$env:TEST_DM_DSN="dm://SYSDBA:<Task0_Step7密码>@127.0.0.1:5236"
$env:TEST_DM_REQUIRED="1"
go test -tags=dm -race -count=1 -run "^TestDM_(AliasSelfJoin_LeftJoinAs|AliasField_InQEq|SubQuery_OuterRef)$" -v ./...
```

预期：3 个测试全部 PASS。

- [ ] **Step 4: 跑全部 9 个 dm 测试**

```powershell
go test -tags=dm -race -count=1 -run "^TestDM" -v ./...
go test -tags=dm -race -count=1 -run "^TestDMDialectorContract$" -v ./...
```

预期：全部 9 个测试 PASS。

- [ ] **Step 5: 双方言并行验证**

```powershell
$env:TEST_DM_DSN="dm://SYSDBA:<Task0_Step7密码>@127.0.0.1:5236"
$env:TEST_ORACLE_DSN="oracle://system:oracle@127.0.0.1:1521/FREEPDB1"  # 若 Oracle 容器仍跑
$env:TEST_DM_REQUIRED="1"
$env:TEST_ORACLE_REQUIRED="1"  # 若 oracle setup 已加同等守卫
go test -tags="oracle dm" -race -count=1 -run "^Test(DM|Oracle)" -v ./...
```

预期：两方言测试函数全部 PASS（验证空 quoter case 合并不冲突）。若 Oracle 容器没起，跳过 Oracle 部分。

- [ ] **Step 6: Commit**

```powershell
git add alias_dm_test.go
git commit -m "test(dm): v0.8.0 alias 体系 DM 行为锁定（3 个测试）

3 个 alias 测试，与 alias_oracle_test.go 一一对应：

- TestDM_AliasSelfJoin_LeftJoinAs: alias 自连接 SQL 生成
- TestDM_AliasField_InQEq: q.Eq(&alias.Age) 行为
- TestDM_SubQuery_OuterRef: correlated EXISTS 子查询

至此 9 个 dm build-tag 测试全过：1 contract + 5 integration + 3 alias，
对照 v0.8.2 Oracle 测试一一镜像。验证 v0.8.0 alias 体系在 DM 8 Oracle
兼容模式下零库代码 bug。

双方言并行（go test -tags=\"oracle dm\"）通过验证 builder.go case 合并
oracle, dm 共用空 quoter 不冲突。"
```

预期：commit 创建。

---

## Task 5: 文档（commit 5）

**Files:**
- Modify: `README.md`
- Modify: `CHANGELOG.md`

**目的**：补齐 README "DM 数据库支持"章节 7 项必含子节 + CHANGELOG v0.8.3 段（按下游用户阅读优先级排序）。

- [ ] **Step 1: 修改 `README.md` 方言矩阵**

定位 README 方言矩阵段（v0.8.2 Oracle 已加过 oracle 行）。在 oracle 行下加 dm 行：

```markdown
| dm | ✅ | build tag: `-tags=dm` | DM 8 Oracle 兼容模式继承全部 Oracle 限制 |
```

- [ ] **Step 2: 修改 `README.md` 已知方言差异速查**

在 Oracle 章节下追加 DM 章节：

```markdown
### DM 数据库（DM 8 Oracle 兼容模式）

DM 8 Oracle 兼容模式继承 v0.8.2 Oracle 章节列出的全部限制（空 quoter / `''=NULL` /
UPPERCASE 输出 / CLOB WHERE / NULLS LAST / RETURNING 单行 / 标识符长度 / ON CONFLICT）。

DM 特有限制：
- **`COMPATIBLE_MODE=2` 必须显式开启**：dameng 镜像不同版本默认值不同，启动 docker 时
  显式 `-e COMPATIBLE_MODE=2`，并用 `SELECT PARA_VALUE FROM V$DM_INI WHERE
  PARA_NAME='COMPATIBLE_MODE'` 验证生效（应返回 2）
- **镜像默认密码版本差异**：dameng 镜像历史上 SYSDBA / SYSDBA001 都见过，部分版本
  首登强制改密——以拉到的镜像 README 为准
- **Docker Hub `dameng/dm8` 是第三方上传**：版本不保证，主路径用 dameng 技术社区
  tar 包 + `docker load`
```

- [ ] **Step 3: 修改 `README.md` 加 "DM 数据库支持" 章节（7 项必含）**

在 README 末尾或方言矩阵后新增章节：

````markdown
## DM 数据库支持（v0.8.3+）

> 仅本地/CI 验证场景。生产侧使用见下方"生产侧集成"。

### 1. Quickstart 5 步

1. **拉 gplus**：`go get github.com/yi-nanping/gplus@v0.8.3`
2. **拉 DM 8 镜像**：从 [dameng 技术社区](https://eco.dameng.com/) 下载 DM 8 docker tar 包，`docker load` 后 `docker run` 启动（参见下方"启动命令"）
3. **设 DSN 环境变量**：`export TEST_DM_DSN="dm://SYSDBA:<密码>@127.0.0.1:5236"`（密码以镜像 README 为准）
4. **跑测试**：`go test -tags=dm -v ./...`（强制：`TEST_DM_REQUIRED=1 go test -tags=dm ./...`）
5. **遇错查表**：见下方"错误码导航"

### 2. TEST_DM_DSN 格式

BNF：

```
TEST_DM_DSN := "dm://" <user> ":" <password> "@" <host> ":" <port> [ "/" <schema> ] [ "?" <params> ]
```

样例（Task 0 Step 7 实测后填入）：

```bash
# 本地 docker 默认实例（密码以镜像 README 为准，常见 SYSDBA / SYSDBA001）
export TEST_DM_DSN="dm://SYSDBA:<实测密码>@127.0.0.1:5236"

# 指定 schema 切换
export TEST_DM_DSN="dm://SYSDBA:<密码>@127.0.0.1:5236/MYSCHEMA"

# 字符集参数（dameng 驱动支持的连接参数见 gitee.com/chunanyong/dm README）
export TEST_DM_DSN="dm://SYSDBA:<密码>@127.0.0.1:5236?charset=utf8"
```

### 3. 下游生产侧集成

```go
import (
    dameng "github.com/godoes/gorm-dameng"
    "gorm.io/gorm"
    "github.com/yi-nanping/gplus"
)

func main() {
    db, _ := gorm.Open(dameng.Open("dm://SYSDBA:..."), &gorm.Config{})
    repo := gplus.NewRepository[int64, User](db)
    // ... 与 sqlite/mysql/pg 完全一样
}
```

gplus 自身**不预先注册** Dialector，下游需自己 `import _ "github.com/godoes/gorm-dameng"`（或显式 `gorm.Open(dameng.Open(...))`）。

### 4. 保留字 → 措施对照表

DM 8 Oracle 兼容模式继承 Oracle 全部保留字（`order` / `size` / `level` / `comment` /
`type` / `group` / `role` / `number` / `date` 等）。gplus 走空 quoter 策略不会
自动加引号（TD-14），下游遇到保留字列名时按优先级处理：

| 优先级 | 措施 | 示例 |
|---|---|---|
| 1（推荐） | 改 struct tag `column:` 避开 | `Order int \`gorm:"column:ord_no"\`` |
| 2 | 用 `RawSQL` / `WhereRaw` 加双引号 | `q.WhereRaw("\"order\" = ?", 100)` |
| 3 | 等 v1.0+ 自动加引号能力（参见 TD-14） | — |

### 5. 错误码导航

| 错误码 | 触发场景 | 措施 |
|---|---|---|
| `ORA-00904` invalid identifier | 列名命中保留字 + 空 quoter | 见上方"保留字 → 措施对照表" |
| `ORA-00932` inconsistent datatypes | string 长字段映射 CLOB 后 LIKE/IN | struct 字段加 `gorm:"size:N"` 显式约束 |
| `ORA-01430` column already exists | migrator 重复 ALTER ADD | setup 不前置 AutoMigrate，走 DROP+AutoMigrate（参考 `dm_setup_test.go` 的 `truncateDMTables`） |
| connect failed | DSN 密码不对 | 镜像默认密码以 README 为准；首登强制改密版本需先 `ALTER USER SYSDBA IDENTIFIED BY ...` |

### 6. 验证 COMPATIBLE_MODE=2 生效

```sql
-- 容器内 disql 执行
SELECT PARA_VALUE FROM V$DM_INI WHERE PARA_NAME='COMPATIBLE_MODE';
-- 应返回 2（Oracle 兼容）；若不是 2，docker run 加 -e COMPATIBLE_MODE=2 重建
```

### 7. 未验证场景兜底声明

v0.8.3 仅验证：DM 8 Oracle 兼容模式 + 单实例 + UTF-8。**未验证**（下游需自行验证或避开）：
- 国密 SM3/SM4 加密列
- Kerberos 认证
- DSC 集群 / 读写分离 / 容灾双活
- DM 7 及更老版本（spec §1.2 排除）
- DM MySQL/PG/TD 兼容模式（v0.8.4+ 候选）

### 启动 DM 8 容器（WSL2 + Docker Engine）

```bash
# 加载 dameng 技术社区 tar 包
wsl -d Ubuntu-24.04 -e docker load -i /mnt/d/downloads/dm8.tar

# 启动（单行，避免续行符不透传）
wsl -d Ubuntu-24.04 -e docker run -d --name dm8 -p 5236:5236 -e INSTANCE_NAME=DM8TEST -e PAGE_SIZE=16 -e UNICODE_FLAG=1 -e CASE_SENSITIVE=Y -e COMPATIBLE_MODE=2 <image_tag>
```

### GOPROXY 配置（gitee 包拉取）

`gitee.com/chunanyong/dm` 在 proxy.golang.org 历史上多次缓存失败/超时：

```bash
# 国内开发者推荐
go env -w GOPROXY=https://goproxy.cn,direct

# 国外 / proxy.golang.org 失败时
go env -w GOPROXY=https://proxy.golang.org,direct
go env -w GOPRIVATE=gitee.com/*
```
````

**注意**：上方所有 `<密码>` / `<image_tag>` 占位符在落 README 时**用 Task 0 实测值替换**（实施期 plan Step 7 Step 5 已记录）。

- [ ] **Step 4: 修改 `CHANGELOG.md` 加 v0.8.3 段**

定位 CHANGELOG 顶部（v0.8.2 段之前）。新增 v0.8.3 段，子节顺序按下游用户阅读优先级（spec §9.2）：

```markdown
## [0.8.3] - 2026-05-08

### 支持版本与兼容性

- **DM 8 Oracle 兼容模式**：v0.8.0 alias 体系 + Repository CRUD 在 DM 8 下行为锁定
  - **`COMPATIBLE_MODE=2` 必须显式开启**：docker run 加 `-e COMPATIBLE_MODE=2`，
    `SELECT PARA_VALUE FROM V$DM_INI WHERE PARA_NAME='COMPATIBLE_MODE'` 验证应返回 2
  - **DM 7 及更老版本不支持**：sequence + trigger 自增、ROWNUM 重写未实现（参见 TD-17）

### 已知限制（DM）

DM 8 Oracle 兼容模式继承 v0.8.2 Oracle 全部限制：
- 空 quoter 策略（保留字列名需用户手动加引号或改 struct tag）
- `''` = NULL（影响 IsNull / Empty 判断）
- 输出列名 UPPERCASE（RawScan 需 SQL 显式 `AS "col"` 锁定 lowercase）
- CLOB/TEXT WHERE 限制（string 字段须 `gorm:"size:N"` 约束）
- NULLS LAST 默认（升序 NULL 排末尾）
- RETURNING 仅支持单行（SaveBatch/UpsertBatch 走 RETURNING 路径需 t.Skip）
- 标识符长度上限（DM 8 实际 128，保留 ≤30 字符规范以兼容 Oracle 12c R1）
- ON CONFLICT 不支持（DM 用 `MERGE INTO`）

DM 特有：
- **镜像默认密码版本差异**：dameng 镜像历史上 SYSDBA / SYSDBA001 都见过，部分版本首登强制改密——以拉到的镜像 README 为准
- **Docker Hub 第三方镜像版本不保证**：主路径用 dameng 技术社区 tar + `docker load`
- **`gitee.com/chunanyong/dm` 在 GOPROXY 拉取**：国内 `goproxy.cn` 推荐，国外 `GOPRIVATE=gitee.com/*` fallback

### 新增（DM 8 支持）

- **DM 数据库支持**：v0.8.0 alias 体系 + Repository CRUD 在 DM 8 Oracle 兼容模式下行为锁定
  - GORM Dialector：`github.com/godoes/gorm-dameng vX.Y.Z`（与 v0.8.2 godoes/gorm-oracle 同作者）
  - Go 驱动：`gitee.com/chunanyong/dm`（DM 官方纯 Go 驱动，无 cgo，transitive）
  - **测试隔离**：`//go:build dm` build tag，**不进 CI**（DM 镜像同样大、license 复杂）
  - 跑测命令：`go test -tags=dm ./...`，需启动本地 docker DM 8 实例
  - **强制不漏跑**：`TEST_DM_REQUIRED=1 go test -tags=dm ./...`（DSN 不通时 t.Fatalf 而非 t.Skip）
- 新建 4 个 dm build-tag 测试文件（commit 序列见下方"提交记录"）：
  - `dm_setup_test.go`：`setupDMDB` helper + `truncateDMTables`（DROP TABLE PURGE + AutoMigrate 沿用 Oracle 决策）
  - `dm_contract_test.go`：Dialector 契约（`db.Name() == "dm"` + `getQuoteChar` 返回空 quoter）
  - `dm_integration_test.go`：5 个 CRUD 测试（BasicCRUD / Where / OrderGroupHaving / JoinQuery / QuoteColumn）
  - `alias_dm_test.go`：3 个 alias 体系测试（自连接 / alias 字段 q.Eq / correlated EXISTS）

### 文档

- README 方言矩阵加 DM
- README 已知方言差异速查加 DM 章节（直接引用 Oracle + 3 条 DM 特有）
- README 新增 "DM 数据库支持" 章节（Quickstart / TEST_DM_DSN BNF / 下游生产侧集成 / 保留字对照表 / 错误码导航 / COMPATIBLE_MODE 诊断 SQL / 未验证场景兜底）
- README GOPROXY 配置提示（gitee 包拉取 fallback）
- spec：`docs/superpowers/specs/2026-05-08-dm-support-design.md`（经过 brainstorming + 2 轮 6 专家审计 + 14 必修修订 + 13 待定项 + 7 README 缺口）
- plan：`docs/superpowers/plans/2026-05-08-dm-support-plan.md`（5 task / 5 commit + Task 0 待定项探测）

### 库代码改动

- **`builder.go: getQuoteChar`** 把 `case "oracle":` 合并为 `case "oracle", "dm":` + 注释泛化为 oracle/DM——**唯一库代码（非测试）改动**
- 既有 `TestGetQuoteChar_Dialects` 加 dm 子测试覆盖（用 testMockDialector 模拟，避免默认 build 引入 driver）
- `TestQuoteColumn_Dialects` 不动（与 oracle 一致——表驱动直接喂 quoter 字符不走 dialect 分支）

### 技术债

- **TD-15**：DM 测试无 CI 守护，依赖下游手动跑发现问题
- **TD-16**：第三方 Dialector 维护风险（gorm-dameng 由社区维护，GORM 升级时可能滞后）
- **TD-17**：DM 7 不支持（sequence + trigger 自增、ROWNUM 重写未实现）
- **TD-18**：DM MySQL/PG/TD 兼容模式不支持（v0.8.3 仅验证 Oracle 兼容；切到 MySQL 兼容需重测 quoter 策略）

复用既有 TD：
- **TD-12**（单模块带可选 driver）：gorm-dameng 拉到 transitive
- **TD-13**（批量 RETURNING 适配）：DM 也不解决，推到 v0.9+
- **TD-14**（保留字列名自动加引号）：在 DM 下行为完全一致

### 收尾说明

仅测试基建 + 文档变更（除 `getQuoteChar` 一处分支扩展外），不涉及核心 API、Repository CRUD、alias 体系；GORM 版本锁定保持 v1.31.x；`v0.8.0` / `v0.8.1` / `v0.8.2` tag 不受影响。

下一步候选（v0.8.4）：DM MySQL 兼容模式（与 gplus 已有 mysql 路径冲突需重测 quoter）。  
更远（v0.9+）：人大金仓 KingbaseES（信创第二大户，PG 兼容模式）/ 批量 RETURNING 适配（解 TD-13）/ 保留字列名自动加引号（解 TD-14）。

---
```

- [ ] **Step 5: 验证 README 与 CHANGELOG 渲染**

```powershell
# Markdown lint（如果有 markdownlint-cli 安装）
markdownlint README.md CHANGELOG.md

# 或者直接 git diff 查看
git diff README.md CHANGELOG.md | more
```

- [ ] **Step 6: Commit**

```powershell
git add README.md CHANGELOG.md
git commit -m "docs(dm): README 方言矩阵加 DM + CHANGELOG v0.8.3

README:
- 方言矩阵加 dm 行（build tag: -tags=dm）
- 已知方言差异速查加 DM 章节（继承 Oracle + 3 条 DM 特有：COMPATIBLE_MODE
  须显式 / 镜像密码版本差异 / Docker Hub 第三方）
- 新增 'DM 数据库支持' 章节 7 项必含子节（spec §9.1）：
  1. Quickstart 5 步
  2. TEST_DM_DSN 格式 BNF + 3 个样例
  3. 下游生产侧集成（gplus 不预先注册 dialector，下游需自己 import driver）
  4. 保留字 → 措施对照表（改 struct tag / RawSQL / 等 v1.0）
  5. 错误码导航（ORA-00904 / ORA-00932 / ORA-01430 / connect failed）
  6. COMPATIBLE_MODE=2 诊断 SQL
  7. 未验证场景兜底声明（国密 / Kerberos / DSC / DM 7 / 其它兼容模式）
- GOPROXY + GOPRIVATE 配置提示

CHANGELOG v0.8.3 段（按下游用户阅读优先级排序）：
- 支持版本与兼容性 → 已知限制 → 新增 → 文档 → 库代码改动 → 技术债 → 收尾
- TD-15/16/17/18 新增 + 复用 TD-12/13/14
- 收尾点明仅测试基建 + 1 行库代码改动 + 既有 tag 不受影响"
```

预期：commit 创建。

---

## Task 6: 发版 tag v0.8.3

**目的**：把 5 commit 打 tag，推 GitHub。

- [ ] **Step 1: 验证全部测试**

```powershell
# 默认 build
go build ./...
go vet ./...
go test -race -count=1 ./...

# DM 实测
$env:TEST_DM_DSN="dm://SYSDBA:<Task0_Step7密码>@127.0.0.1:5236"
$env:TEST_DM_REQUIRED="1"
go test -tags=dm -race -count=1 -v ./...

# 双方言（若 Oracle 容器仍跑）
go test -tags="oracle dm" -race -count=1 -v ./...
```

预期：默认 + dm 都 PASS。

- [ ] **Step 2: 看 commit 序列**

```powershell
git log --oneline v0.8.2..HEAD
```

预期：5 commit（deps / builder+setup+contract / integration / alias / docs）。

- [ ] **Step 3: 推 main + 打 tag**

```powershell
# 先推 main（用户审过逐 commit 后）
git push origin main

# 打 tag（与 main HEAD 同位置）
git tag -a v0.8.3 -m "v0.8.3: DM 8 Oracle 兼容模式支持

仅测试基建 + 文档变更（除 builder.go getQuoteChar 一处 case 合并外），
不涉及核心 API / Repository CRUD / alias 体系。

5 commit 序列：
- deps：godoes/gorm-dameng 依赖
- builder + setup + contract：case 合并 oracle, dm + setup helper + 契约
- integration：5 个 CRUD 测试
- alias：3 个 alias 体系测试
- docs：README + CHANGELOG

GORM 版本锁定保持 v1.31.x；v0.8.0 / v0.8.1 / v0.8.2 tag 不受影响。"

# 推 tag
git push origin v0.8.3
```

预期：GitHub 仓库出现 v0.8.3 tag。

- [ ] **Step 4: 验证 GitHub 状态**

浏览器访问 `https://github.com/yi-nanping/gplus/releases`：

- v0.8.3 tag 存在
- 5 commit 在 main 分支线性
- CI（默认 build）pass

- [ ] **Step 5: 更新 memory**

更新 `C:\Users\11851\.claude\projects\D--Projects-golang-gplus\memory\MEMORY.md`：

- 最新已发布 tag：v0.8.3
- 已发布 tag 序列加 v0.8.3
- 加 DM 决策的 memory 文件 `dm-quoter.md`（如果有新决策的话；否则只更新 oracle-quoter.md 的 "已应用到 v0.8.3" 标识）

---

## Self-Review

**Spec 覆盖**：
- §1 背景与动机 → Task 0 探测 + 5 commit 实施
- §2 决策摘要 → Task 1-5 落到具体 commit / file
- §3 架构 → Task 1-5 file structure 表
- §4 builder.go 修订 → Task 2 Step 1
- §5 依赖 → Task 1 + Task 0 Step 4 cgo 验证
- §6 实施风险 10 行 → Task 0 多步探测 + Task 2 Step 7 fail 排查 + Task 3 Step 2 fail 排查
- §7 已知限制 → README §9.1 + CHANGELOG 已知限制段（Task 5）
- §8 技术债 → CHANGELOG 技术债段（Task 5）
- §9 文档变更 → Task 5 全部
- §10 验收清单 11 项 → Task 1-5 commit + Task 6 tag
- §11 plan 阶段待定项 13 项 → Task 0 全部 13 步
- §12 后续候选 → CHANGELOG 收尾（Task 5）

**Placeholder 扫描**：
- `<Task0_Step3锁定版本>` / `<Task0_Step7密码>` / `<image_tag>` 等占位符均在 Task 0 步骤中明确填入路径
- 没有 "TBD" / "TODO" / "implement later"
- 每个代码 step 都有完整代码 / 完整命令

**Type 一致性**：
- `setupDMDB` 返回 `(*Repository[int64, MySQLUser], *gorm.DB)`，与 `setupOracleDB` 一致
- `truncateDMTables` 签名与 `truncateOracleTables` 一致
- `defaultDMDSN` 类型 string 与 oracle 一致（值不同：oracle 是非空，dm 是空字符串故意触发显式 DSN）
- testMockDialector 复用既有定义（`missing_coverage_test.go:1219`），不重新声明
- `assertEqual` / `assertError` / `IsNotFound` / `applyDBPoolLimits` 全部复用既有 helper

---

## Plan 阶段实测值（Task 0 Step 12 写入）

> 实施期填入；此处仅占位提醒。

- 锁定 godoes/gorm-dameng 版本：`__`
- 锁定 DM 8 image tag：`__`
- 锁定 SYSDBA 密码：`__`
- 探测 db.Name() 实测值：`__`
- 探测 docker run env 白名单：`__`
- 探测 MySQLUser 字段保留字命中：`__`
- godoes/gorm-dameng cgo 验证结果：`__`
- DM 容器启动 ready 标志：`__`
