# v0.8.4 KingbaseES（人大金仓 V9R1C10）PG 兼容模式支持 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 gplus 库（基于 GORM v1.31.x 的 Go 泛型增强）加 KingbaseES V9R1C10 PG 兼容模式支持，验证 v0.8.0 alias 体系 + Repository CRUD 在 KingbaseES PG-compat 方言下正确工作。

**Architecture:** Build tag (`//go:build kingbase`) 隔离测试代码，CI 不变（保持 sqlite + mysql + pg）；库代码改动局限于 `builder.go: getQuoteChar` 把 `case "postgres", "sqlite", "dm":` 合并为 `case "postgres", "sqlite", "dm", "kingbase":` 共用双引号 quoter；新建 4 个 kingbase build-tag 测试文件 + `third_party/kingbase-gokb/` vendor 目录（**仅当 plan Task 0 验证 license 允许 redistribute**）。

**Tech Stack:** Go 1.24 / GORM v1.31.1 / `gorm.io/driver/postgres v1.6.0`（**复用既有 PG dialect**，无新 GORM 依赖）/ `kingbase.com/gokb`（官网 2025-08-12 版，vendor + replace）/ KingbaseES V9R1C10 docker tar（kingbase.com.cn 官网下载）

**Spec reference:** `docs/superpowers/specs/2026-05-09-kingbase-support-design.md`（已经过 brainstorming + 11 专家两轮审计 + 34 必修 + §11 16 项工程师自驱待定项 + §9.1 9 项 README 必含）

**Verification commands:**
- 默认测试（无 kingbase）：`go test -race -count=1 ./...`
- KingbaseES 测试（需启动 docker）：`go test -tags=kingbase -race -count=1 -v ./...`
- 强制 KingbaseES 测试（防 Skip 误报）：`$env:TEST_KINGBASE_REQUIRED='1'; go test -tags=kingbase -race -count=1 -v ./...`
- 多方言并行（**仅本机 5 容器环境**）：`go test -tags="oracle dm kingbase" -race -count=1 ./...`
- vet：`go vet ./...` + `go vet -tags=kingbase ./...` + `go vet ./third_party/kingbase-gokb/...`
- build：`go build ./...` + `go build -tags=kingbase ./...`

**Docker 运行环境（用户本机：Windows 11 + WSL2 Ubuntu-24.04 + Docker Engine 29.1.3）：**

| 环境 | 命令前缀 |
|---|---|
| WSL2 + Docker Engine | `wsl -d Ubuntu-24.04 -e docker ...` |

下文所有 `docker xxx` 命令均按 WSL wrapper 写。WSL2 mirrored 网络模式下 `54321:54321` 端口在 Windows `localhost:54321` 直接可达，DSN 无需改动。

---

## File Structure

| 文件 | 类型 | 职责 | build tag |
|---|---|---|---|
| `third_party/kingbase-gokb/` | **新建目录** | Gokb 完整解压（含 `kingbase.com/gokb/*.go` 子包 + 必须有 `go.mod`），通过 `go.mod replace` 指令重定向 | 默认（含 LICENSE / README） |
| `go.mod` / `go.sum` | 修改 | 加 `require kingbase.com/gokb v0.0.0-00010101000000-000000000000` + `replace kingbase.com/gokb => ./third_party/kingbase-gokb` | 默认 |
| `.gitignore` | 修改 | 加 `!/third_party/kingbase-gokb/**` 精确 allowlist（避免与既有 `*.so/*.dll/*.test` deny 冲突） | 默认 |
| `builder.go` | 修改 | `getQuoteChar` 把 `case "postgres", "sqlite", "dm":` 改为 `case "postgres", "sqlite", "dm", "kingbase":` + 注释泛化（**唯一库代码（非测试）改动 1 行**） | 默认 |
| `missing_coverage_test.go` | 修改 | `TestGetQuoteChar_Dialects` 加 kingbase 子测试（`TestQuoteColumn_Dialects` 不动） | 默认 |
| `kingbase_setup_test.go` | **新建** | `defaultKingbaseDSN` / `kingbaseDriverName` 常量 / `setupKingbaseDB` / `truncateKingbaseTables` (DROP TABLE CASCADE + AutoMigrate) / `database_mode` fail-fast 校验 | `//go:build kingbase` |
| `kingbase_contract_test.go` | **新建** | Dialector 契约：`db.Name() == "postgres"`（A3 实测确定）+ `getQuoteChar` 在 `"kingbase"` mock 方言下返回双引号 quoter | `//go:build kingbase` |
| `kingbase_integration_test.go` | **新建** | 5 个 CRUD 集成测试（BasicCRUD / WhereConditions 含 IsNull/Empty/ON CONFLICT WHERE / OrderGroupHaving / JoinQuery / QuoteColumn） | `//go:build kingbase` |
| `alias_kingbase_test.go` | **新建** | 3 个 alias 体系测试（自连接 / alias 字段 q.Eq / correlated EXISTS） | `//go:build kingbase` |
| `README.md` | 修改 | 方言矩阵加 KingbaseES + 新增"KingbaseES 数据库支持"9 项必含子节（含错误诊断 + 生产部署） | 默认 |
| `CHANGELOG.md` | 修改 | 加 v0.8.4 段（按下游用户阅读优先级排序的 7 子节） | 默认 |

**不动**：

- `testdb_test.go`（不引入 KingbaseES driver 到默认编译路径）
- 其他库代码（query.go / update.go / repository.go / alias.go / subquery.go / schema.go / debug.go）
- CI 配置（`.github/workflows/ci.yml`）
- 现有所有测试（包括 v0.8.2 Oracle / v0.8.3 DM 测试）

**Commit 序列（4 commit，C2 合并 vendor + deps + builder + setup + contract）：**

1. `Task 1` **vendor + deps + builder + setup + contract**：解压 Gokb 到 `third_party/kingbase-gokb/` + .gitignore + go.mod replace + builder.go 1 行 case + missing_coverage_test.go + setup helper + contract 测试（一次性 commit 保证从 commit 到 commit 都 build 通过；避免 Commit 1 单独 vendor 后 `go vet` 踩 third_party 子树风险）
2. `Task 2` **integration**：`kingbase_integration_test.go` 5 测试
3. `Task 3` **alias**：`alias_kingbase_test.go` 3 测试
4. `Task 4` **docs**：`README.md` + `CHANGELOG.md` v0.8.4 段

最后 `Task 5`：tag v0.8.4 + push GitHub。

**🚦 Commit 1 起点硬条件**：Task 0 全部 ✓ 才能开 commit 1，尤其 T8（db.Name() 实测）+ T11（database_mode 诊断 SQL 实测）必须先于 commit 1，否则 contract 测试断言写不出 / setup 守卫无 SQL 可用。

---

## Task 0: 环境前置检查 + 用户前置 6 项 + 工程师自驱 16 项

**目的**：spec §11.1 列出 6 项用户人工前置（U1-U6），§11.2 列出 16 项工程师自驱实测（T1-T16），本 task 集中解决全部，写入本 plan 后续 task 替换占位符。

**不产生 commit**——本 task 只输出"事实表"，更新本 plan 文件 + 产出 `docs/superpowers/plans/2026-05-09-kingbase-task0-results.md`（commit 1 PR 描述引用）。

### Task 0.1 环境前置检查

- [ ] **Step 1: 环境前置检查（T1）**

```powershell
# 检查 Go 工具链
go version  # 期望 go1.24.x

# 检查 WSL2 + Ubuntu-24.04
wsl -l -v  # 期望 Ubuntu-24.04 列表 STATE=Running

# 检查 WSL 内 docker engine
wsl -d Ubuntu-24.04 -e docker version  # 期望 Server 段显示版本号 29.x

# 检查 docker 网络可达
wsl -d Ubuntu-24.04 -e docker run --rm hello-world  # 期望 "Hello from Docker!"

# 验证既有 4 容器仍可用（baseline 对照组，避免环境劣化误判）
wsl -d Ubuntu-24.04 -e docker ps -a  # 期望 dm8-test / mysql8 / pg16 / oracle-free 列出

# 检查 kingbase.com.cn 官网可访问
curl -I https://www.kingbase.com.cn/download.html  # 期望 HTTP 200
```

预期：全部通过。任一失败需先解决环境问题（参考 `docs/dev-setup/wsl2-keep-alive.md`）。

- [x] **Step 2: 配置 GOPROXY（作者本地实施前置）** — 已配置（v0.8.3 DM 时验证）

```powershell
go env GOPROXY  # 验证：https://goproxy.cn,direct
```

### Task 0.2 用户人工前置 6 项（U1-U6 / 🔴 阻塞工程师自驱）

> **用户操作**：以下步骤需要在浏览器走验证码 + 联系销售流程，**不在工程师可自驱范围**。等待期间工程师可并行做 T2-T5/T13-T16（不依赖 license）。

- [ ] **Step 3: U1 下载 Gokb（验证码弹窗）**

操作：

1. 浏览器打开 https://www.kingbase.com.cn/download.html
2. 点击 tab "接口驱动"
3. 滚动到 GOLANG 一栏（描述："Gokb 驱动是基于 database/sql 包..."）
4. 右下角红色下载图标 → 点击 "不限CPU_不限OS"
5. 弹窗 "下载验证" → 按页面提示填验证码 → 点 "确认"
6. 浏览器开始下载 Gokb-V009R001C010-XXXXX.zip（约 496K）

记录到 `task0-results.md`：
- 下载文件名（含版本号后缀）
- 下载完整 URL（如有）
- SHA256（U6 任务）

- [ ] **Step 4: U2 下载 V9R1C10 Docker tar（验证码弹窗）**

操作：

1. 同页面 tab "数据库" → V9R1C10
2. KingbaseES 数据库 Docker 镜像段落 → X64_Linux → 点击下载（约 730MB）
3. 验证码弹窗 → 填写下载

记录到 `task0-results.md`：
- 下载文件名
- 文件大小
- SHA256（推荐校验防损坏）

- [ ] **Step 5: U3 申请 license.dat（🔴 SLA 不可控，2 周阈值）**

操作：

1. 同页面 → "授权文件" 按钮（V9R1C10 旁边）
2. 填申请表（公司名 / 邮箱 / 联系电话 / 用途说明）
3. 提交后等销售邮件回复（**SLA 1-3 工作日，最多 2 周**）
4. 收到 license.dat 邮件附件后保存到 `D:\downloads\kingbase-license.dat`

记录到 `task0-results.md`：
- 申请提交时间
- 销售回复时间
- license 类型（试用 / 生产）+ 限制（max_connect=10 ?）
- license 有效期

**Abort 阈值**：若 2 周内拿不到 license.dat，**abort v0.8.4**，推到 v1.0 后重新启动。

- [ ] **Step 6: U4 license 文件审查（A1 关键决策点）**

```powershell
# 解压 Gokb zip 到临时目录看 LICENSE 文件
$tmp = "D:\tmp\gokb-license-check"
mkdir $tmp -Force
Expand-Archive -Path "D:\downloads\Gokb-V009R001C010-*.zip" -DestinationPath $tmp -Force

# 查 LICENSE / LICENSE.txt / README
Get-ChildItem $tmp -Recurse -Include "LICENSE*", "README*", "EULA*" | ForEach-Object {
  Write-Host "=== $($_.FullName) ==="
  Get-Content $_.FullName
}
```

**关键判断**：

| 发现 | 决策 |
|---|---|
| MIT / Apache 2.0 / BSD 等开源 license | ✅ 继续 vendor 进 git 路径 |
| 任何形式的 "no redistribute" / "授权用户专用" / "仅限内部使用" 字样 | ❌ **abort vendor 进 git 方案**，切 README 引导路径（推翻 §3.1） |
| 无 LICENSE 文件 | ⚠️ **保守对待**——视为禁止 redistribute（联系金仓销售确认） |

记录到 `task0-results.md`：
- LICENSE 类型
- 决策（继续 / abort）

- [ ] **Step 7: U5 vendor 安全扫描（2S6）**

```powershell
# 安装工具（如未装）
go install github.com/securego/gosec/v2/cmd/gosec@latest
go install honnef.co/go/tools/cmd/staticcheck@latest

# 跑 gosec（卡 HIGH 级 SQL 注入 / cmd exec / 网络回连）
gosec -severity=high D:\tmp\gokb-license-check/...

# 跑 staticcheck
staticcheck D:\tmp\gokb-license-check/...
```

预期：HIGH 级清零。若有 HIGH 级 finding，**记录到 task0-results.md**，决定是否 abort：

- 误报（false positive）→ 标记并继续
- 真实问题（exec / unsafe pointer / 网络回连） → **abort**，联系金仓售后

- [ ] **Step 8: U6 SHA256 校验官网 zip（2S7）**

```powershell
# 计算 hash
Get-FileHash "D:\downloads\Gokb-V009R001C010-*.zip" -Algorithm SHA256

# 同样校验 docker tar
Get-FileHash "D:\downloads\kingbase-V9R1C10-x64-linux.tar" -Algorithm SHA256
```

记录到 `task0-results.md`：
- Gokb zip SHA256
- docker tar SHA256
- 两个值都写入 README §9.1（防 fork 投毒）

### Task 0.3 工程师自驱 T2-T5（不依赖 license，等待期可并行做）

- [ ] **Step 9: T4 vendor 含 go.mod 验证 + 解压到 third_party/**

```powershell
# 解压到 gplus 仓库 third_party/ 目录
$gplusRoot = "D:\Projects\golang\gplus"
$vendorDir = "$gplusRoot\third_party\kingbase-gokb"
mkdir $vendorDir -Force

# 把 zip 解压到 vendorDir
Expand-Archive -Path "D:\downloads\Gokb-V009R001C010-*.zip" -DestinationPath $vendorDir -Force

# 验证 go.mod 是否存在
ls $vendorDir\go.mod
```

如果 `go.mod` 不存在，手工创建 `third_party/kingbase-gokb/go.mod`：

```
module kingbase.com/gokb

go 1.24
```

> **注意**：`require` 块内容暂留空，T2 实测后填 transitive deps。

记录到 `task0-results.md`：
- vendor 解压后实际文件树（用 `tree third_party\kingbase-gokb /F /A` 截图）
- 解压后体积（`(Get-ChildItem third_party\kingbase-gokb -Recurse | Measure-Object -Property Length -Sum).Sum / 1MB` MB）
- go.mod 是否原生带（决定是否需手工补）

- [ ] **Step 10: T2 Gokb cgo + transitive deps 验证**

```powershell
# 临时 module 测试 Gokb 拉取
$tmp = "D:\tmp\gokb-deps-check"
mkdir $tmp -Force
cd $tmp

# 写最小 main.go 触发 import
@"
package main

import (
    "database/sql"
    _ "kingbase.com/gokb"
)

func main() {
    _, _ = sql.Open("kingbase", "")
}
"@ | Out-File main.go -Encoding utf8

# 写 go.mod 用 replace 指向解压后路径
@"
module check
go 1.24
require kingbase.com/gokb v0.0.0-00010101000000-000000000000
replace kingbase.com/gokb => $($gplusRoot -replace '\\','/')/third_party/kingbase-gokb
"@ | Out-File go.mod -Encoding utf8

# 拉依赖
go mod tidy

# 验证无 cgo
go list -deps . | Select-String -Pattern "C$" -SimpleMatch

# 列 transitive deps
go list -m all
```

记录到 `task0-results.md`：
- 是否含 cgo（期望：无）
- transitive deps 完整清单（用于 §5.1 spec 文档）

**Abort 条件**：若发现含 cgo（与官网"完全 Golang 编写"不符），**回退到自定义 Dialector 路径**（不在 v0.8.4 范围）。

- [ ] **Step 11: T3 Gokb 注册名实测**

```powershell
cd D:\Projects\golang\gplus\third_party\kingbase-gokb

# grep sql.Register 调用
Select-String -Path "*.go" -Pattern "sql\.Register" -Recurse
```

预期输出形如：
```
init.go: sql.Register("kingbase", &Driver{})
init.go: sql.Register("postgres", &Driver{})
```

或仅一个名字。记录到 `task0-results.md`：

- 注册名清单
- **决定 `kingbaseDriverName` 常量值**：默认 `"kingbase"`；若仅注册 `"postgres"`，改 `"postgres"` 但需在 setup 加 `sql.Drivers()` 检测避免 lib/pq 共存冲突

- [ ] **Step 12: T5 vendor 子树 vet 验证（B4）**

```powershell
cd D:\Projects\golang\gplus

# vet vendor 子树
go vet ./third_party/kingbase-gokb/...
```

预期：无错误。若 vet 报错（如 vendor 含 `_test.go` 用了老式 `// +build` 语法或其他问题），**记录到 task0-results.md** + 决定是否：

- 修复 vendor 内代码（不推荐，会污染上游升级路径）
- 加 `third_party/kingbase-gokb/.go-build-ignore` 标记（推荐）
- 改 build tag 文件结构隔离

- [ ] **Step 13: T13/T14/T15/T16 Gokb 协议层实测（grep 检查）**

```powershell
cd D:\Projects\golang\gplus\third_party\kingbase-gokb

# T13 CheckNamedValue
Select-String -Path "*.go" -Pattern "CheckNamedValue" -Recurse | Select-Object Path, Line, LineNumber

# T14 LISTEN/NOTIFY
Select-String -Path "*.go" -Pattern "Listen|Notify" -Recurse | Select-Object Path, Line, LineNumber

# T15 COPY FROM STDIN
Select-String -Path "*.go" -Pattern "CopyFrom|CopyData|CopyIn" -Recurse | Select-Object Path, Line, LineNumber

# T16 stderr 日志噪声预检（看 init() 是否有 log.Printf）
Select-String -Path "init*.go", "driver*.go" -Pattern "log\.Printf|fmt\.Fprintln.*os\.Stderr" -Recurse
```

记录到 `task0-results.md`：
- T13: CheckNamedValue 是否实现（影响 `sql.Named` 命名参数支持）
- T14: LISTEN/NOTIFY 是否实现
- T15: COPY FROM STDIN 是否实现
- T16: 启动期 stderr 噪声（决定 setup 是否需 `log.SetOutput(io.Discard)`）

按结果**修订 spec §7 已知限制**："未实现" → 改 "未验证" 或删除。

### Task 0.4 工程师自驱 T6-T11（依赖 license + 容器启动）

> 此节须等 U3 license.dat 到位后才能开始。

- [ ] **Step 14: T6 加载 docker tar + 启动容器**

```powershell
# load image
wsl -d Ubuntu-24.04 -e cp /mnt/d/downloads/kingbase-V9R1C10-x64-linux.tar /tmp/
wsl -d Ubuntu-24.04 -e docker load -i /tmp/kingbase-V9R1C10-x64-linux.tar

# 看 image tag
wsl -d Ubuntu-24.04 -e docker images | findstr -i kingbase
```

记录到 `task0-results.md`：`__锁定 image tag：__kingbase/kingbasees:vXXX`

```powershell
# copy license.dat 到 WSL 可访问路径
wsl -d Ubuntu-24.04 -e cp /mnt/d/downloads/kingbase-license.dat /tmp/

# docker run 启动（先用最小 env，看哪些被识别）
wsl -d Ubuntu-24.04 -e docker run -d --name kingbase-test \
  -p 54321:54321 \
  -e DB_MODE=pg \
  -e SYSTEM_USER=system \
  -e SYSTEM_PWD=Test_Kingbase_2026 \
  -e ENCODING=UTF8 \
  -v /tmp/kingbase-license.dat:/home/kingbase/license.dat \
  --restart=no \
  <锁定的 image_tag>

# 等待启动（30s-1min）
wsl -d Ubuntu-24.04 -e docker logs -f kingbase-test
```

预期：日志最后出现 "database is ready" 或类似 ready 标志。若启动失败：
- license 路径错 → 用 `docker exec` 检查 `/home/kingbase/license.dat` 是否存在
- env 变量名不对 → 看日志报错，对照镜像内 README 调整

**验证 env 白名单（T6）**：

```powershell
wsl -d Ubuntu-24.04 -e docker exec kingbase-test env | findstr -i "DB_MODE SYSTEM ENCODING"
```

记录哪些 env 真实生效到 `task0-results.md`：`__env 白名单：__`。不识别的在 plan Task 1 docker 启动命令中删除。

- [ ] **Step 15: T7 探测 system 默认密码 + 首登策略**

```powershell
# 进入容器
wsl -d Ubuntu-24.04 -e docker exec -it kingbase-test bash

# 容器内：试不同方式登录（KingbaseES 用 ksql）
ksql -U system -d test -p 54321  # 会提示输密码
# 输入：Test_Kingbase_2026（Step 14 设置的）
```

如果首登强制改密：

```sql
-- 容器内 ksql 中执行
ALTER USER system WITH PASSWORD 'Prod_Kingbase_2026!';
\q
```

记录新密码到 `task0-results.md`。

- [ ] **Step 16: T8 实测 db.Name() 实际返回值（A3 验证）**

> spec A3 决策：postgres.Dialector.Name() 硬编码 "postgres"。本步骤是验证，不是猜测。

新建临时探测脚本（不入版本控制）：

```powershell
# Windows 路径
mkdir D:\tmp\kingbase-probe -Force
cd D:\tmp\kingbase-probe
go mod init probe

# 用 replace 指向 gplus 的 vendor
@"
module probe
go 1.24
require (
    kingbase.com/gokb v0.0.0-00010101000000-000000000000
    gorm.io/driver/postgres v1.6.0
    gorm.io/gorm v1.31.1
)
replace kingbase.com/gokb => D:/Projects/golang/gplus/third_party/kingbase-gokb
"@ | Out-File go.mod -Encoding utf8
go mod tidy
```

写 `probe.go`：

```go
package main

import (
    "database/sql"
    "fmt"
    "log"

    _ "kingbase.com/gokb"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"
)

func main() {
    dsn := "host=127.0.0.1 port=54321 user=system password=Test_Kingbase_2026 dbname=test sslmode=disable"

    sqlDB, err := sql.Open("kingbase", dsn)
    if err != nil {
        log.Fatalf("sql.Open failed: %v", err)
    }
    if err := sqlDB.Ping(); err != nil {
        log.Fatalf("Ping failed: %v", err)
    }

    db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
    if err != nil {
        log.Fatalf("gorm.Open failed: %v", err)
    }

    fmt.Printf("db.Name() = %q\n", db.Name())
    fmt.Printf("sql.Drivers() = %v\n", sql.Drivers())
}
```

运行：

```powershell
go run probe.go
```

预期输出：
```
db.Name() = "postgres"
sql.Drivers() = [kingbase postgres ...] 或 [kingbase ...]
```

**A3 决策验证**：`db.Name()` 必为 `"postgres"`（gorm.io/driver/postgres v1.6.0 `postgres.go:49-51` 硬编码确认）。若返回别的，**回滚 spec A3 决策**——这意味着 postgres dialect 行为非确定性，可能要切自定义 Dialector 路径。

记录到 `task0-results.md`：
- db.Name() 实际值
- sql.Drivers() 注册名清单（验证 T3）

- [ ] **Step 17: T9 MySQLUser 字段是否撞 KingbaseES 保留字**

```powershell
# 容器内 ksql 跑（先用 PG 核心函数）
wsl -d Ubuntu-24.04 -e docker exec -it kingbase-test ksql -U system -d test -p 54321 -c "SELECT word FROM pg_get_keywords() WHERE word IN ('name','age','email')"
```

预期：返回空（这三个字段不是保留字）。若返回非空：

```powershell
# fallback 用 sys_get_keywords（KB 兼容名）
wsl -d Ubuntu-24.04 -e docker exec -it kingbase-test ksql -U system -d test -p 54321 -c "SELECT word FROM sys_get_keywords() WHERE word IN ('name','age','email')"
```

记录到 `task0-results.md`：
- pg_get_keywords() 是否可用（spec §1.2 信心确认）
- 哪些字段撞保留字
- 若 MySQLUser 字段撞保留字 → 改测试 struct 字段名（avoid `name/age/email` → `username/user_age/user_email`）

- [ ] **Step 18: T10 实测 KingbaseES 底层 PG 版本**

```powershell
wsl -d Ubuntu-24.04 -e docker exec -it kingbase-test ksql -U system -d test -p 54321 -c "SELECT version()"
wsl -d Ubuntu-24.04 -e docker exec -it kingbase-test ksql -U system -d test -p 54321 -c "SHOW server_version"
```

预期输出形如：
```
KingbaseES V9R1C10 ... based on PostgreSQL 12.x
12.x
```

记录到 `task0-results.md`：
- KingbaseES 实际版本号
- 底层 PG 版本号
- 写入 README §9.1 第 6 项 + §7 已知限制（确定 PG 13/14 特性可用范围）

- [ ] **Step 19: T11 database_mode 诊断 SQL 三选一定型**

```powershell
# 三选一逐个试
wsl -d Ubuntu-24.04 -e docker exec -it kingbase-test ksql -U system -d test -p 54321 -c "SELECT current_setting('database_mode')"
wsl -d Ubuntu-24.04 -e docker exec -it kingbase-test ksql -U system -d test -p 54321 -c "SHOW database_mode"
wsl -d Ubuntu-24.04 -e docker exec -it kingbase-test ksql -U system -d test -p 54321 -c "SELECT setting FROM sys_settings WHERE name='database_mode'"
```

记录到 `task0-results.md`：
- 哪条 SQL 可用（设为唯一定型 SQL）
- 实际返回值（pg / 0 / oracle / 1 等）
- **此结果决定 `kingbase_setup_test.go` 的 setup 守卫 SQL** —— 不留 fallback（2A2 fail-fast）

**关键决策**：

| 实测结果 | spec/setup 修订 |
|---|---|
| `current_setting('database_mode')` 返回 `"pg"` | spec/setup 保持现状（默认） |
| `current_setting` 返回 `"0"` | setup 双值容忍 `dbMode != "pg" && dbMode != "0"` ✓ |
| `current_setting` 报 `unrecognized configuration parameter` | 改 `SHOW database_mode` 或 `sys_settings` |
| 三选一全失败 | **abort v0.8.4**——KB 没暴露 database_mode 到 SQL，无法验证 PG-compat 模式 |

- [ ] **Step 20: 整合 task0-results.md 提交**

```powershell
cd D:\Projects\golang\gplus

# 用所有上面记录的实测结果产出最终文档
# (内容由实施过程逐步累积，非批量生成)

# 暂不 commit，本文件仅作 commit 1 PR 描述引用
# 等 commit 4 docs 提交时一起进
```

确认 `task0-results.md` 包含 16 项实测结果（U1-U6 + T1-T16），方可进入 Task 1。

---

## Task 1: vendor + deps + builder + setup + contract（C2 单 commit 合并）

**目的**：一次性落地 vendor 树 + go.mod replace + builder.go 1 行 case + missing_coverage_test.go + setup helper + contract 测试。避免 commit 1 单独 vendor 后 `go vet` 踩 third_party 子树的 B4 风险。

**🚦 commit 1 起点硬条件检查**：

- [ ] Task 0 全部 ✓（U1-U6 + T1-T16，含 task0-results.md）
- [ ] U4 license 审查 ✓（不禁止 redistribute）
- [ ] T8 db.Name() 实测确认为 "postgres"
- [ ] T11 database_mode SQL 定型为唯一 SQL
- [ ] T3 Gokb 注册名锁定 → kingbaseDriverName 常量值确定

任一未完成不得开始 Task 1。

### Task 1.1 .gitignore + vendor 树（已在 Task 0 解压）

- [ ] **Step 1: 修改 .gitignore 加 vendor allowlist**

修改 `.gitignore`（追加到末尾）：

```diff
 # 但 docs/dev-setup/local/ 是本机笔记目录，不进 git
 docs/dev-setup/local/
+
+# vendor 第三方驱动（KingbaseES Gokb，allowlist 模式下需显式精确加白）
+!/third_party/kingbase-gokb/**
```

- [ ] **Step 2: 验证 vendor 加白生效**

```powershell
cd D:\Projects\golang\gplus

# 检查 third_party/ 内容是否被 git 看到
git status third_party/
# 期望：所有 vendor 文件以 untracked 显示（不是 ignored）

# 列出已 staged 的 vendor 文件数（与解压目录文件数对账）
git add third_party/kingbase-gokb/
git ls-files --staged third_party/kingbase-gokb/ | Measure-Object -Line
# 与解压后实际文件数对比应一致
```

记录到 `task0-results.md`：vendor 文件数对账。

### Task 1.2 go.mod / go.sum

- [ ] **Step 3: 修改 go.mod 加 require + replace**

修改 `go.mod`（在既有 require 块内合适位置）：

```go
require (
    github.com/glebarez/sqlite v1.11.0
    github.com/godoes/gorm-dameng v0.7.2
    github.com/godoes/gorm-oracle v1.6.18
    gorm.io/driver/mysql v1.6.0
    gorm.io/driver/postgres v1.6.0
    gorm.io/gorm v1.31.1
    kingbase.com/gokb v0.0.0-00010101000000-000000000000  // KingbaseES 官方驱动 v9.x，vendor + replace
)

replace kingbase.com/gokb => ./third_party/kingbase-gokb
```

- [ ] **Step 4: 跑 go mod tidy + 验证**

```powershell
go mod tidy
go mod verify
```

预期：无错误。若有：
- `replacement directory does not exist` → 检查 vendor 目录路径
- `missing go.sum entry` → vendor 内 `go.mod` 不全（手工补 require 块）

- [ ] **Step 5: 验证默认 build 不破**

```powershell
go build ./...    # 期望成功
go vet ./...      # 期望无错误
go test ./...     # 期望既有测试全过，无 KingbaseES 测试参与
```

### Task 1.3 builder.go 修订

- [ ] **Step 6: 修改 builder.go getQuoteChar**

`builder.go:233`（v0.8.3 现状）：

```go
case "postgres", "sqlite", "dm":
    // dm 走双引号 quoter（实测推翻 spec 早期假设）：
    // godoes/gorm-dameng migrator 实际用 `CREATE TABLE "my_sql_users"
    // ("id" BIGINT,"username" VARCHAR(64),...)` 带引号 lowercase 建表，
    // 列名在 DM 中存为 case-sensitive 小写 username。DM CASE_SENSITIVE=Y
    // + Oracle 兼容模式下，裸标识符 username 会被 UPPERCASE 解析为
    // USERNAME，导致 Error -2111 无效的列名。必须用双引号 "username"
    // 锁定小写匹配数据库中的真实列名。与 postgres/sqlite 行为一致。
    return "\"", "\""
```

改为：

```go
case "postgres", "sqlite", "dm", "kingbase":
    // dm/kingbase 走双引号 quoter：
    //   - dm: dameng migrator 引号 lowercase 建表（v0.8.3 实测）
    //   - kingbase: PG-compat 模式与 postgres dialect 一致；
    //     postgres.Dialector.Name() 实际返回 "postgres"，加 "kingbase" 字符串
    //     是为了契约一致性 + 未来自定义 Dialector 兜底（v0.8.4 实测确认）
    return "\"", "\""
```

### Task 1.4 missing_coverage_test.go 加 kingbase 子测试

- [ ] **Step 7: 改 missing_coverage_test.go**

定位 `TestGetQuoteChar_Dialects` 函数，在既有 dm 子测试后追加：

```go
t.Run("kingbase 方言返回双引号 quoter（与 postgres/dm 共用）", func(t *testing.T) {
    db := &gorm.DB{Config: &gorm.Config{Dialector: testMockDialector{"kingbase"}}}
    qL, qR := getQuoteChar(db)
    if qL != "\"" || qR != "\"" {
        t.Errorf("kingbase 期望双引号，实际 (%q,%q)", qL, qR)
    }
})
```

- [ ] **Step 8: 跑 missing_coverage 测试**

```powershell
go test -run TestGetQuoteChar_Dialects -v ./...
```

预期：3 个子测试（postgres / dm / kingbase）全过 + 既有 oracle 子测试也过。

### Task 1.5 kingbase_setup_test.go

- [ ] **Step 9: 新建 kingbase_setup_test.go**

完整内容（基于 spec §3.3 + 二轮 2A2/2S1 修订）：

```go
//go:build kingbase

package gplus

import (
    "database/sql"
    "fmt"
    "os"
    "testing"

    _ "kingbase.com/gokb"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"
    "gorm.io/gorm/logger"
)

// 警告：仅限本地 Docker 开发。system 是 KingbaseES 默认超级账户，绝不能用于生产。
//
// 防自相矛盾策略：defaultKingbaseDSN 故意留空，强制下游必须显式设置 TEST_KINGBASE_DSN。
const defaultKingbaseDSN = ""

// kingbaseDriverName 是 Gokb 驱动注册名，2S1：抽常量便于 plan T3 实测后单点修改
// （若 T3 实测注册名不是 "kingbase" → 改本常量即可，无需多文件同步改）
const kingbaseDriverName = "kingbase" // ⚠️ T3 实测后修改此值

// setupKingbaseDB 与 setupDMDB 同模式：非泛型，绑定 MySQLUser 复用既有测试 struct。
//
// 复用既有 helper（无 build tag，全包可见）：
//   - applyDBPoolLimits → testdb_test.go:24
//   - MySQLUser → mysql_integration_test.go:15
//   - Repository / NewRepository → repository.go
//
// 不前置 AutoMigrate：直接走 truncateKingbaseTables 的 DROP+AutoMigrate 路径。
// 与 PG/MySQL 测试同模式：DROP+CREATE 保证序列重置干净状态。
func setupKingbaseDB(t *testing.T) (*Repository[int64, MySQLUser], *gorm.DB) {
    t.Helper()

    dsn := os.Getenv("TEST_KINGBASE_DSN")
    if dsn == "" {
        dsn = defaultKingbaseDSN
    }
    if dsn == "" {
        if os.Getenv("TEST_KINGBASE_REQUIRED") == "1" {
            t.Fatalf("TEST_KINGBASE_DSN 未设置但 TEST_KINGBASE_REQUIRED=1")
        }
        t.Skip("TEST_KINGBASE_DSN 未设置，跳过 KingbaseES 测试（参见 README 章节）")
    }

    sqlDB, err := sql.Open(kingbaseDriverName, dsn)
    if err != nil {
        if os.Getenv("TEST_KINGBASE_REQUIRED") == "1" {
            t.Fatalf("KingbaseES 强制要求但不可用: %v", err)
        }
        t.Skipf("KingbaseES 不可用（sql.Open 失败）: %v", err)
    }

    if err := sqlDB.Ping(); err != nil {
        if os.Getenv("TEST_KINGBASE_REQUIRED") == "1" {
            t.Fatalf("KingbaseES 强制要求但 ping 失败: %v", err)
        }
        t.Skipf("KingbaseES 不可用（ping 失败）: %v", err)
    }

    // 2S9 诊断：打印已注册 driver 列表，便于发现 lib/pq + Gokb 共存时的名字冲突
    t.Logf("已注册 sql.Drivers(): %v", sql.Drivers())

    // E1 + 2A2：校验 PG-compat 模式生效（fail-fast，不降级 t.Logf）
    // SQL 由 plan T11 实测后定型——这里假设 current_setting 可用，否则改换 SQL
    var dbMode string
    if err := sqlDB.QueryRow(`SELECT current_setting('database_mode')`).Scan(&dbMode); err != nil {
        t.Fatalf("database_mode 校验失败（plan T11 应实测确定唯一 SQL，setup 不应到此分支）: %v", err)
    }
    if dbMode != "pg" && dbMode != "0" {
        t.Fatalf("KingbaseES 不在 PG-compat 模式（database_mode=%q，期望 'pg' 或 '0'），重起容器加 -e DB_MODE=pg", dbMode)
    }

    db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
        Logger: logger.Default.LogMode(logger.Info),
    })
    if err != nil {
        t.Fatalf("gorm.Open 失败: %v", err)
    }

    applyDBPoolLimits(t, db)
    repo := NewRepository[int64, MySQLUser](db)
    truncateKingbaseTables(t, db, &MySQLUser{})
    t.Cleanup(func() { truncateKingbaseTables(t, db, &MySQLUser{}) })
    return repo, db
}

// truncateKingbaseTables：DROP TABLE CASCADE + AutoMigrate 策略
//
// PG-compat 模式遵循 PG 行为：
//   - SERIAL/IDENTITY 序列与表绑定，DROP TABLE CASCADE 同时清理序列
//   - 不需要 PURGE（那是 Oracle 兼容模式语法）
//   - CASCADE 处理 FK 依赖
func truncateKingbaseTables(t *testing.T, db *gorm.DB, models ...any) {
    t.Helper()
    for _, m := range models {
        stmt := &gorm.Statement{DB: db}
        if err := stmt.Parse(m); err != nil {
            t.Fatalf("parse model 失败: %v", err)
        }
        sql := fmt.Sprintf(`DROP TABLE IF EXISTS "%s" CASCADE`, stmt.Table)
        if err := db.Exec(sql).Error; err != nil {
            t.Fatalf("DROP TABLE 失败: %v", err)
        }
    }
    if err := db.AutoMigrate(models...); err != nil {
        t.Fatalf("AutoMigrate 失败: %v", err)
    }
}
```

- [ ] **Step 10: 验证 setup 测试编译过**

```powershell
go build -tags=kingbase ./...   # 期望成功
go vet -tags=kingbase ./...      # 期望无错误
```

### Task 1.6 kingbase_contract_test.go

- [ ] **Step 11: 新建 kingbase_contract_test.go**

```go
//go:build kingbase

package gplus

import (
    "testing"

    "gorm.io/gorm"
)

// TestKingbaseDialectorContract 验证 GORM dialect + getQuoteChar 行为契约。
//
// E3：与 DM 一致，独立 setup（不调 setupKingbaseDB），仅验 dialect 而非 DB 行为。
// 即使 TEST_KINGBASE_DSN 未设也能跑（mock dialect 验证）。
func TestKingbaseDialectorContract(t *testing.T) {
    t.Run("DialectorName_必为_postgres（A3 实测确定）", func(t *testing.T) {
        // 通过真实 setup 验证 db.Name() 实际返回值（依赖 docker + license）
        _, db := setupKingbaseDB(t) // setup 内部会跳过守卫如果无 DSN
        if db.Name() != "postgres" {
            t.Errorf("db.Name() 期望 'postgres'（postgres.Dialector.Name() 硬编码），实际 %q", db.Name())
        }
    })

    t.Run("getQuoteChar_kingbase_方言返回双引号", func(t *testing.T) {
        // 用 mock dialect 验 case 字符串路径（不依赖 docker）
        db := &gorm.DB{Config: &gorm.Config{Dialector: testMockDialector{"kingbase"}}}
        qL, qR := getQuoteChar(db)
        if qL != "\"" || qR != "\"" {
            t.Errorf("kingbase 方言期望双引号 quoter，实际 (%q,%q)", qL, qR)
        }
    })
}
```

> **注意**：`testMockDialector` 已在 `missing_coverage_test.go:1219` 定义，全包可见，无需重复定义。

- [ ] **Step 12: 跑 contract 测试（无 DSN，验证 mock 路径）**

```powershell
# 不设 TEST_KINGBASE_DSN，跑测试
go test -tags=kingbase -run TestKingbaseDialectorContract -v ./...
```

预期：
- `DialectorName_必为_postgres` 子测试 → SKIP（setupKingbaseDB 因无 DSN 跳过）
- `getQuoteChar_kingbase_方言返回双引号` 子测试 → PASS

- [ ] **Step 13: 跑 contract 测试（带 DSN + REQUIRED=1，验证真实路径）**

```powershell
$env:TEST_KINGBASE_DSN = "host=127.0.0.1 port=54321 user=system password=Test_Kingbase_2026 dbname=test sslmode=disable"
$env:TEST_KINGBASE_REQUIRED = "1"

go test -tags=kingbase -run TestKingbaseDialectorContract -v ./...
```

预期：2 个子测试全过。

### Task 1.7 commit 1

- [ ] **Step 14: 验证 commit 1 完整 build + test 通过**

```powershell
# 默认 build（不应触达 KingbaseES）
go build ./...
go vet ./...
go test ./...

# KingbaseES build（带 DSN 跑）
$env:TEST_KINGBASE_REQUIRED = "1"
go test -tags=kingbase -run TestKingbaseDialectorContract -v ./...
```

预期：默认 build 全过 + KingbaseES contract 测试全过。

- [ ] **Step 15: stage commit 1 内容**

```powershell
git add third_party/kingbase-gokb/
git add .gitignore
git add go.mod go.sum
git add builder.go
git add missing_coverage_test.go
git add kingbase_setup_test.go
git add kingbase_contract_test.go

git status
```

预期 stage：
- `third_party/kingbase-gokb/` 大量文件（vendor 树）
- `.gitignore` modified
- `go.mod` / `go.sum` modified
- `builder.go` modified
- `missing_coverage_test.go` modified
- `kingbase_setup_test.go` new
- `kingbase_contract_test.go` new

- [ ] **Step 16: 创建 commit 1**

```powershell
git commit -m "feat(kingbase): vendor + deps + builder + setup + contract

- vendor: 解压 kingbase.com/gokb V9R1C10 到 third_party/kingbase-gokb/
  + .gitignore 加 !/third_party/kingbase-gokb/** 精确 allowlist
- deps: go.mod 加 require kingbase.com/gokb + replace 指向本地 vendor
  （Go 标准 zero pseudo-version 占位）
- builder.go: getQuoteChar 加 kingbase case 字符串（行为等价 postgres
  双引号 quoter；A3 实测确定 db.Name() 必为 postgres，加字符串是为
  契约一致性 + 未来自定义 Dialector 兜底）
- missing_coverage_test.go: TestGetQuoteChar_Dialects 加 kingbase 子测试
- kingbase_setup_test.go: setupKingbaseDB / truncateKingbaseTables /
  kingbaseDriverName 常量 / database_mode fail-fast 校验
- kingbase_contract_test.go: TestKingbaseDialectorContract 2 子测试
  （DialectorName 必 postgres + getQuoteChar 双引号）

Spec: docs/superpowers/specs/2026-05-09-kingbase-support-design.md
Plan: docs/superpowers/plans/2026-05-09-kingbase-support-plan.md
Task 0 results: docs/superpowers/plans/2026-05-09-kingbase-task0-results.md

Refs spec C2: 4 commit 序列合并 vendor + deps + builder + setup + contract"
```

**Commit 1 fail-recovery**：若 commit 后再跑测试发现失败：

```powershell
# 退回 staged 状态（保留 reflog 可读性）
git reset --soft HEAD~1

# 改完后重新 commit（禁用 amend）
# ...修复...
git commit -m "..."
```

若失败根因是 plan Task 0 实测错（如 db.Name() 实际不是 postgres），**回滚 Task 0 决策、修订 spec**，不要硬改 commit 1。

---

## Task 2: integration 5 测试

**目的**：实施 5 个 CRUD 集成测试（BasicCRUD / WhereConditions 含 IsNull/Empty/ON CONFLICT WHERE / OrderGroupHaving / JoinQuery / QuoteColumn），覆盖 gplus Repository 在 KingbaseES PG-compat 方言下的核心行为。

**镜像源**：从 `dm_integration_test.go` copy 5 个测试函数 → 改 `Test_DM_*` 为 `Test_Kingbase_*` + `setupDMDB` 改 `setupKingbaseDB`，按 PG-compat 行为差异调整：

- DROP TABLE 语法：DM 用 `PURGE` → KB 用 `CASCADE`（已在 setup helper 处理）
- IsNull 测试：DM 因 Oracle `''=NULL` 剔除 → **KB 加回（PG 严格区分）**
- ON CONFLICT 测试：DM 不支持 → **KB 加 partial update WHERE 子句**
- RETURNING：DM `t.Skip` → KB 不 skip（PG 完全支持）

### Task 2.1 BasicCRUD

- [ ] **Step 1: 新建 kingbase_integration_test.go 骨架 + BasicCRUD**

```go
//go:build kingbase

package gplus

import (
    "context"
    "testing"
)

func TestKingbase_BasicCRUD(t *testing.T) {
    ctx := context.Background()
    repo, _ := setupKingbaseDB(t)

    // Save
    user := MySQLUser{Username: "alice", Age: 30, Email: "alice@example.com"}
    if err := repo.Save(ctx, &user); err != nil {
        t.Fatalf("Save failed: %v", err)
    }
    if user.ID == 0 {
        t.Errorf("Save 后 ID 应自增，实际为 0")
    }

    // GetById
    got, err := repo.GetById(ctx, user.ID)
    if err != nil {
        t.Fatalf("GetById failed: %v", err)
    }
    if got.Username != "alice" {
        t.Errorf("GetById 期望 alice，实际 %q", got.Username)
    }

    // List
    list, err := repo.List(NewQuery[MySQLUser](ctx))
    if err != nil {
        t.Fatalf("List failed: %v", err)
    }
    if len(list) != 1 {
        t.Errorf("List 期望 1 条，实际 %d", len(list))
    }

    // Count
    count, err := repo.Count(NewQuery[MySQLUser](ctx))
    if err != nil {
        t.Fatalf("Count failed: %v", err)
    }
    if count != 1 {
        t.Errorf("Count 期望 1，实际 %d", count)
    }

    // UpdateById
    user.Age = 31
    if err := repo.UpdateById(ctx, &user); err != nil {
        t.Fatalf("UpdateById failed: %v", err)
    }
    got, _ = repo.GetById(ctx, user.ID)
    if got.Age != 31 {
        t.Errorf("Update 后 Age 期望 31，实际 %d", got.Age)
    }

    // DeleteById
    affected, err := repo.DeleteById(ctx, user.ID)
    if err != nil {
        t.Fatalf("DeleteById failed: %v", err)
    }
    if affected != 1 {
        t.Errorf("DeleteById affected 期望 1，实际 %d", affected)
    }
}
```

- [ ] **Step 2: 跑 BasicCRUD 测试**

```powershell
$env:TEST_KINGBASE_DSN = "host=127.0.0.1 port=54321 user=system password=Test_Kingbase_2026 dbname=test sslmode=disable"
$env:TEST_KINGBASE_REQUIRED = "1"

go test -tags=kingbase -run TestKingbase_BasicCRUD -v ./...
```

预期：PASS。

### Task 2.2 WhereConditions（含 IsNull / Empty / ON CONFLICT WHERE）

- [ ] **Step 3: 追加 WhereConditions 测试**

在 `kingbase_integration_test.go` 追加：

```go
func TestKingbase_WhereConditions(t *testing.T) {
    ctx := context.Background()
    repo, db := setupKingbaseDB(t)

    // seed 5 条记录（含 NULL email 测 IsNull / 空 email 测 Empty 区分）
    seed := []MySQLUser{
        {Username: "alice", Age: 25, Email: "alice@example.com"},
        {Username: "bob", Age: 30, Email: "bob@example.com"},
        {Username: "carol", Age: 35, Email: ""}, // 空字符串
        {Username: "dave", Age: 40, Email: "dave@example.com"},
        // eve 的 email 设为 NULL（直接 SQL 插入，gplus Save 走 zero-value 默认）
    }
    for _, u := range seed {
        if err := repo.Save(ctx, &u); err != nil {
            t.Fatal(err)
        }
    }
    // eve email 用 raw SQL 设 NULL
    if err := db.Exec(`INSERT INTO "my_sql_users" (username, age, email) VALUES (?, ?, NULL)`, "eve", 45).Error; err != nil {
        t.Fatal(err)
    }

    // Ne
    list, err := repo.List(NewQuery[MySQLUser](ctx).Ne(&MySQLUser{}.Username, "alice"))
    if err != nil {
        t.Fatal(err)
    }
    if len(list) != 4 {
        t.Errorf("Ne 期望 4 条，实际 %d", len(list))
    }

    // LikeRight 前缀
    list, err = repo.List(NewQuery[MySQLUser](ctx).LikeRight(&MySQLUser{}.Email, "alice"))
    if err != nil {
        t.Fatal(err)
    }
    if len(list) != 1 {
        t.Errorf("LikeRight 期望 1 条，实际 %d", len(list))
    }

    // In
    list, err = repo.List(NewQuery[MySQLUser](ctx).In(&MySQLUser{}.Username, []string{"alice", "bob"}))
    if err != nil {
        t.Fatal(err)
    }
    if len(list) != 2 {
        t.Errorf("In 期望 2 条，实际 %d", len(list))
    }

    // NotIn
    list, err = repo.List(NewQuery[MySQLUser](ctx).NotIn(&MySQLUser{}.Username, []string{"alice", "bob"}))
    if err != nil {
        t.Fatal(err)
    }
    if len(list) != 3 {
        t.Errorf("NotIn 期望 3 条，实际 %d", len(list))
    }

    // Between
    list, err = repo.List(NewQuery[MySQLUser](ctx).Between(&MySQLUser{}.Age, 28, 38))
    if err != nil {
        t.Fatal(err)
    }
    if len(list) != 2 {
        t.Errorf("Between 期望 2 条（bob 30 + carol 35），实际 %d", len(list))
    }

    // GetOne
    u, err := repo.GetOne(NewQuery[MySQLUser](ctx).Eq(&MySQLUser{}.Username, "alice"))
    if err != nil {
        t.Fatal(err)
    }
    if u.Age != 25 {
        t.Errorf("GetOne age 期望 25，实际 %d", u.Age)
    }

    // IsNull（PG 严格区分 ''/NULL，2B2 新增覆盖）
    list, err = repo.List(NewQuery[MySQLUser](ctx).IsNull(&MySQLUser{}.Email))
    if err != nil {
        t.Fatal(err)
    }
    if len(list) != 1 {
        t.Errorf("IsNull 期望 1 条（eve NULL），实际 %d", len(list))
    }
    if len(list) > 0 && list[0].Username != "eve" {
        t.Errorf("IsNull 期望 eve，实际 %q", list[0].Username)
    }

    // Empty 区分（PG 严格区分 ''/NULL，2B2 新增覆盖）
    list, err = repo.List(NewQuery[MySQLUser](ctx).Eq(&MySQLUser{}.Email, ""))
    if err != nil {
        t.Fatal(err)
    }
    if len(list) != 1 {
        t.Errorf("Empty 期望 1 条（carol 空字符串），实际 %d", len(list))
    }
    if len(list) > 0 && list[0].Username != "carol" {
        t.Errorf("Empty 期望 carol，实际 %q", list[0].Username)
    }

    // ON CONFLICT DO UPDATE WHERE（PG 支持 partial update WHERE 子句，2B2 新增覆盖）
    // 镜像源：pg_integration_test.go 或 mysql 的 OnConflict 测试 + 加 WHERE 子句
    upsertUser := MySQLUser{Username: "alice", Age: 99, Email: "alice@updated.com"}
    err = repo.InsertOnConflict(ctx, &upsertUser, OnConflict{
        Columns: []string{"username"},
        DoUpdates: map[string]any{
            "age":   99,
            "email": "alice@updated.com",
        },
        WhereExpr: `"my_sql_users"."age" < 50`, // PG 支持 partial WHERE
    })
    if err != nil {
        t.Fatalf("InsertOnConflict with WHERE failed: %v", err)
    }
    got, _ := repo.GetOne(NewQuery[MySQLUser](ctx).Eq(&MySQLUser{}.Username, "alice"))
    if got.Age != 99 {
        t.Errorf("ON CONFLICT WHERE 期望 alice age 更新为 99，实际 %d", got.Age)
    }
}
```

> **注意**：`OnConflict` 结构 + `WhereExpr` 字段实际签名以 gplus 既有 API 为准——若现有 OnConflict 不支持 WhereExpr 字段，**降级**：本子测试改为基础 `DoUpdates`，加 TD 跟进 v1.x。

- [ ] **Step 4: 跑 WhereConditions 测试**

```powershell
go test -tags=kingbase -run TestKingbase_WhereConditions -v ./...
```

预期：PASS。

### Task 2.3 OrderGroupHaving / JoinQuery / QuoteColumn

- [ ] **Step 5: 追加 OrderGroupHaving 测试**

```go
func TestKingbase_OrderGroupHaving(t *testing.T) {
    ctx := context.Background()
    repo, db := setupKingbaseDB(t)

    // seed
    seed := []MySQLUser{
        {Username: "alice", Age: 25, Email: "a@x.com"},
        {Username: "bob", Age: 30, Email: "b@x.com"},
        {Username: "carol", Age: 35, Email: "c@x.com"},
        {Username: "dave", Age: 40, Email: "d@x.com"},
    }
    for _, u := range seed {
        if err := repo.Save(ctx, &u); err != nil {
            t.Fatal(err)
        }
    }

    // OrderBy DESC
    list, err := repo.List(NewQuery[MySQLUser](ctx).OrderByDesc(&MySQLUser{}.Age))
    if err != nil {
        t.Fatal(err)
    }
    if len(list) != 4 || list[0].Age != 40 {
        t.Errorf("OrderByDesc 期望 dave 40 最前，实际 %v", list)
    }

    // Limit-Offset
    list, err = repo.List(NewQuery[MySQLUser](ctx).OrderByAsc(&MySQLUser{}.Age).Limit(2).Offset(1))
    if err != nil {
        t.Fatal(err)
    }
    if len(list) != 2 || list[0].Age != 30 {
        t.Errorf("Limit-Offset 期望 bob/carol，实际 %v", list)
    }

    // GroupBy + Having（用 RawScan 映射）
    type ageGroup struct {
        Age   int    `gorm:"column:age"`
        Count int64  `gorm:"column:cnt"`
    }
    var groups []ageGroup
    err = db.Raw(`
        SELECT age, COUNT(*) AS "cnt"
        FROM "my_sql_users"
        GROUP BY age
        HAVING COUNT(*) >= 1
        ORDER BY age
    `).Scan(&groups).Error
    if err != nil {
        t.Fatal(err)
    }
    if len(groups) != 4 {
        t.Errorf("GroupBy 期望 4 组，实际 %d", len(groups))
    }

    // UpdateByCond
    affected, err := repo.UpdateByCond(NewUpdater[MySQLUser](ctx).
        Eq(&MySQLUser{}.Age, 25).
        Set(&MySQLUser{}.Username, "alice2"))
    if err != nil {
        t.Fatal(err)
    }
    if affected != 1 {
        t.Errorf("UpdateByCond affected 期望 1，实际 %d", affected)
    }

    // DeleteByCond
    affected, err = repo.DeleteByCondTX(ctx, NewQuery[MySQLUser](ctx).Ge(&MySQLUser{}.Age, 35), nil)
    if err != nil {
        t.Fatal(err)
    }
    if affected != 2 {
        t.Errorf("DeleteByCond affected 期望 2（carol+dave），实际 %d", affected)
    }
}
```

- [ ] **Step 6: 追加 JoinQuery 测试（自连接）**

```go
func TestKingbase_JoinQuery(t *testing.T) {
    ctx := context.Background()
    repo, db := setupKingbaseDB(t)

    // seed: 自连接需在 my_sql_users 上做 ID-Username 自比对
    seed := []MySQLUser{
        {Username: "alice", Age: 25, Email: "a@x.com"},
        {Username: "bob", Age: 30, Email: "b@x.com"},
    }
    for _, u := range seed {
        if err := repo.Save(ctx, &u); err != nil {
            t.Fatal(err)
        }
    }

    // LEFT JOIN 自连接（Username = u2.Username）
    type joinResult struct {
        ID1       int64  `gorm:"column:id1"`
        Username1 string `gorm:"column:username1"`
        ID2       int64  `gorm:"column:id2"`
    }
    var results []joinResult
    err := db.Raw(`
        SELECT u1.id AS "id1", u1.username AS "username1", u2.id AS "id2"
        FROM "my_sql_users" u1
        LEFT JOIN "my_sql_users" u2 ON u1.username = u2.username AND u1.id <> u2.id
    `).Scan(&results).Error
    if err != nil {
        t.Fatal(err)
    }
    if len(results) != 2 {
        t.Errorf("LEFT JOIN 期望 2 行，实际 %d", len(results))
    }
}
```

- [ ] **Step 7: 追加 QuoteColumn 测试（独立 setup，绕过 DSN 守卫）**

```go
func TestKingbase_QuoteColumn(t *testing.T) {
    // E3：独立 setup，不调 setupKingbaseDB，直接用 mock dialect 验 quoteColumn 行为
    db := &gorm.DB{Config: &gorm.Config{Dialector: testMockDialector{"kingbase"}}}

    cases := []struct {
        input string
        want  string
    }{
        {"username", `"username"`},
        {"my_sql_users.username", `"my_sql_users"."username"`},
        {"username AS u", `"username" AS "u"`},
        {"COUNT(*)", "COUNT(*)"}, // 含括号跳过转义
    }

    for _, c := range cases {
        got := quoteColumn(db, c.input)
        if got != c.want {
            t.Errorf("quoteColumn(%q) 期望 %q，实际 %q", c.input, c.want, got)
        }
    }
}
```

- [ ] **Step 8: 跑全部 integration 5 测试**

```powershell
go test -tags=kingbase -run "TestKingbase_BasicCRUD|TestKingbase_WhereConditions|TestKingbase_OrderGroupHaving|TestKingbase_JoinQuery|TestKingbase_QuoteColumn" -v ./...
```

预期：5 测试全过。

### Task 2.4 commit 2

- [ ] **Step 9: stage + commit 2**

```powershell
git add kingbase_integration_test.go
git commit -m "test(kingbase): integration 5 测试 (BasicCRUD/Where/Order/Join/QuoteColumn)

- TestKingbase_BasicCRUD: Save/GetById/List/Count/UpdateById/DeleteById
- TestKingbase_WhereConditions: Ne/LikeRight/In/NotIn/Between/GetOne +
  IsNull/Empty 区分（PG 严格区分 ''/NULL，DM 因 Oracle 兼容剔除）+
  ON CONFLICT DO UPDATE WHERE（PG 支持 partial WHERE）
- TestKingbase_OrderGroupHaving: OrderByDesc/Limit-Offset/GroupBy+Having
  RawScan/UpdateByCond/DeleteByCondTX
- TestKingbase_JoinQuery: LEFT JOIN 自连接 + ON 条件
- TestKingbase_QuoteColumn: 独立 setup（E3，绕过 DSN 守卫），mock
  dialect 验 quoteColumn 双引号转义

镜像源：dm_integration_test.go 5 测试 + mysql/pg 测试加 IsNull/Empty/
ON CONFLICT WHERE 三个新覆盖点"
```

---

## Task 3: alias 3 测试

**目的**：验证 v0.8.0 alias 体系（自连接 / alias 字段 q.Eq / correlated EXISTS）在 KingbaseES PG-compat 方言下行为正确。

**镜像源**：`alias_dm_test.go` 3 测试函数 → 改名 + 改 setup 调用。

### Task 3.1 alias 自连接

- [ ] **Step 1: 新建 alias_kingbase_test.go**

```go
//go:build kingbase

package gplus

import (
    "context"
    "strings"
    "testing"
)

func TestKingbase_AliasSelfJoin_LeftJoinAs(t *testing.T) {
    ctx := context.Background()
    repo, _ := setupKingbaseDB(t)

    // seed
    seed := []MySQLUser{
        {Username: "alice", Age: 25, Email: "a@x.com"},
        {Username: "bob", Age: 30, Email: "b@x.com"},
    }
    for _, u := range seed {
        if err := repo.Save(ctx, &u); err != nil {
            t.Fatal(err)
        }
    }

    u1 := AliasOf[MySQLUser]("u1")
    u2 := AliasOf[MySQLUser]("u2")

    q := NewQuery[MySQLUser](ctx).
        From(u1).
        LeftJoinAs(u2, q2 -> q2.Eq(&u1.Username, &u2.Username))
    list, err := repo.List(q)
    if err != nil {
        t.Fatalf("AliasSelfJoin failed: %v", err)
    }
    if len(list) != 2 {
        t.Errorf("AliasSelfJoin 期望 2 行，实际 %d", len(list))
    }

    // 验 SQL 包含 双引号 quoter（PG 风）
    sql := q.DataRuleBuilder().BuildQuery().Statement.SQL.String()
    if !strings.Contains(sql, `"u1"`) || !strings.Contains(sql, `"u2"`) {
        t.Errorf("生成的 SQL 应含双引号 alias，实际：%s", sql)
    }
}
```

> **注意**：`AliasOf` / `LeftJoinAs` / `From` API 以 gplus 既有签名为准。本测试是 alias_dm_test.go 镜像，需对照实际 API 调整。

- [ ] **Step 2: 跑 alias 自连接**

```powershell
go test -tags=kingbase -run TestKingbase_AliasSelfJoin_LeftJoinAs -v ./...
```

预期：PASS。

### Task 3.2 alias 字段 q.Eq

- [ ] **Step 3: 追加 AliasField_InQEq 测试**

```go
func TestKingbase_AliasField_InQEq(t *testing.T) {
    ctx := context.Background()
    repo, _ := setupKingbaseDB(t)

    seed := []MySQLUser{
        {Username: "alice", Age: 25, Email: "a@x.com"},
        {Username: "bob", Age: 30, Email: "b@x.com"},
    }
    for _, u := range seed {
        if err := repo.Save(ctx, &u); err != nil {
            t.Fatal(err)
        }
    }

    u := AliasOf[MySQLUser]("u")
    q := NewQuery[MySQLUser](ctx).From(u).Eq(&u.Username, "alice")
    list, err := repo.List(q)
    if err != nil {
        t.Fatalf("AliasField_InQEq failed: %v", err)
    }
    if len(list) != 1 || list[0].Username != "alice" {
        t.Errorf("期望 alice，实际 %v", list)
    }
}
```

- [ ] **Step 4: 跑 alias 字段 q.Eq**

```powershell
go test -tags=kingbase -run TestKingbase_AliasField_InQEq -v ./...
```

预期：PASS。

### Task 3.3 correlated EXISTS

- [ ] **Step 5: 追加 SubQuery_OuterRef 测试**

```go
func TestKingbase_SubQuery_OuterRef(t *testing.T) {
    ctx := context.Background()
    repo, _ := setupKingbaseDB(t)

    seed := []MySQLUser{
        {Username: "alice", Age: 25, Email: "a@x.com"},
        {Username: "bob", Age: 30, Email: "b@x.com"},
        {Username: "carol", Age: 35, Email: "c@x.com"},
    }
    for _, u := range seed {
        if err := repo.Save(ctx, &u); err != nil {
            t.Fatal(err)
        }
    }

    outer := AliasOf[MySQLUser]("outer")
    inner := AliasOf[MySQLUser]("inner")

    // EXISTS (SELECT 1 FROM users inner WHERE inner.id = outer.id AND inner.age > 28)
    sub := NewQuery[MySQLUser](ctx).From(inner).
        Eq(&inner.ID, &outer.ID).
        Gt(&inner.Age, 28)

    q := NewQuery[MySQLUser](ctx).From(outer).Exists(sub)
    list, err := repo.List(q)
    if err != nil {
        t.Fatalf("SubQuery_OuterRef failed: %v", err)
    }
    if len(list) != 2 {
        t.Errorf("EXISTS 期望 2 行（bob+carol，age>28），实际 %d", len(list))
    }
}
```

- [ ] **Step 6: 跑 alias 全部 3 测试**

```powershell
go test -tags=kingbase -run "TestKingbase_AliasSelfJoin_LeftJoinAs|TestKingbase_AliasField_InQEq|TestKingbase_SubQuery_OuterRef" -v ./...
```

预期：3 测试全过。

### Task 3.4 commit 3

- [ ] **Step 7: stage + commit 3**

```powershell
git add alias_kingbase_test.go
git commit -m "test(kingbase): alias 3 测试（自连接/alias 字段 q.Eq/correlated EXISTS）

- TestKingbase_AliasSelfJoin_LeftJoinAs: alias 自连接 SQL 生成 + 双引号
  quoter 验证（PG 风）
- TestKingbase_AliasField_InQEq: q.Eq(&alias.Field) 行为
- TestKingbase_SubQuery_OuterRef: correlated EXISTS 子查询 + outer ref

镜像源：alias_dm_test.go 3 测试函数"
```

---

## Task 4: README + CHANGELOG（docs）

**目的**：完成 spec §9.1 9 项 README 章节 + §9.2 7 子节 CHANGELOG。所有 placeholder 用 Task 0 实测后的真实值替换（image_tag / SYSTEM_PWD / DSN / database_mode SQL / 错误码 / SHA256 等）。

### Task 4.1 README

- [ ] **Step 1: 修改 README.md 方言矩阵**

定位 README 现有方言矩阵表（与 dm 行同位置），追加 kingbase 行：

```markdown
| 方言 | 状态 | 测试方式 | 已知差异 |
|---|---|---|---|
| sqlite | ✅ | 默认（无 build tag） | - |
| mysql | ✅ | 默认（含 docker 起 mysql） | - |
| postgres | ✅ | 默认（含 docker 起 pg） | - |
| oracle | ✅ | `-tags=oracle` | 详见 Oracle 章节 |
| dm | ✅ | `-tags=dm` | 同 Oracle 行为（DM Oracle 兼容模式） |
| kingbase | ✅ | `-tags=kingbase` | 同 PG 行为（KingbaseES PG-compat 模式） |
```

- [ ] **Step 2: 修改 README "已知方言差异速查"加 KingbaseES 章节**

定位"已知方言差异速查"段，追加：

```markdown
#### KingbaseES V9R1C10（PG-compat 模式）

KingbaseES V9R1C10 PG-compat 模式继承 PG 全部行为（与 DM 走 Oracle-compat 不同）：

- ✅ RETURNING / ON CONFLICT 全支持（DM 不支持）
- ✅ `''` 与 NULL 严格区分（IsNull 可用，DM 因 Oracle 兼容剔除）
- ✅ 列名 lowercase 默认
- ✅ `$N` 占位符
- ✅ JSONB / Array / TIMESTAMPTZ 类型

详见下方 "KingbaseES 数据库支持" 章节。
```

- [ ] **Step 3: 新增 README "KingbaseES 数据库支持" 章节 9 项**

> 此节内容长，下面以 **outline + 关键代码块** 形式给出，实际 README 落地时按 Task 0 实测结果填实占位符。

定位 DM 章节后，新增：

````markdown
## KingbaseES 数据库支持

> 相比 DM 用户的 4 步集成（`go get gplus + go get gorm-dameng + 起 docker + 跑测试`），KingbaseES 用户多了 vendor + replace + 验证码下载 + license 申请，预计首次配置耗时 1-2 小时。

### 1. 完整安装路径（step-by-step）

#### ⓪ `.gitignore` 安全前置

```bash
echo "license.dat" >> .gitignore
echo ".env" >> .gitignore
# 若已误 commit license.dat：
git rm --cached license.dat && git commit -m "chore: remove license.dat from git"
```

#### ① 下载 Gokb（官网验证码弹窗）

URL: https://www.kingbase.com.cn/download.html → 接口驱动 → GOLANG 一栏 → 点击下载按钮 → 弹窗按页面提示填验证码 → 下载 zip → 解压到自己项目的 `third_party/kingbase-gokb/`

文件名：`__Task 0 实测填__`
SHA256：`__Task 0 U6 实测填__`

#### ② 下载 Docker tar

数据库 → V9R1C10 → X64_Linux Docker tar (730MB)

文件名：`__Task 0 实测填__`
SHA256：`__Task 0 U6 实测填__`

#### ③ 申请 license.dat

官网 → "授权文件" 按钮 → 填申请表（公司/邮箱/电话/用途）→ 销售邮件回复 license.dat（**SLA 1-3 工作日，最多 2 周**）

试用版：max_connect=10，有效期 1 年
生产版：联系金仓销售（marketing@kingbase.com.cn / 400-6011-188）

#### ④ 加载并启动容器

```bash
wsl -d Ubuntu-24.04 -e docker load -i /path/to/kingbase-V9R1C10.tar
wsl -d Ubuntu-24.04 -e docker run -d --name kingbase -p 54321:54321 \
  -e DB_MODE=pg -e SYSTEM_PWD='<your-strong-password-min-12chars>' \
  -v /path/to/license.dat:/home/kingbase/license.dat \
  __image_tag__  # Task 0 实测后填
```

> 注：URL/image_tag 时效性，最新参见 https://www.kingbase.com.cn

PowerShell 用反引号续行：

```powershell
wsl -d Ubuntu-24.04 -e docker run -d --name kingbase -p 54321:54321 `
  -e DB_MODE=pg -e SYSTEM_PWD='<your-strong-password-min-12chars>' `
  -v /path/to/license.dat:/home/kingbase/license.dat `
  __image_tag__
```

#### ⑤ 配置 go.mod（详见第 3 项完整片段）

#### ⑥ 设 DSN 环境变量

```bash
export TEST_KINGBASE_DSN="host=127.0.0.1 port=54321 user=system password=<实测密码> dbname=test sslmode=disable"
```

#### ⑦ 跑测试

```bash
TEST_KINGBASE_REQUIRED=1 go test -tags=kingbase -v ./...
```

### 2. `TEST_KINGBASE_DSN` 格式 BNF

```
host=<host> port=<port> user=<user> password=<password> dbname=<dbname> sslmode=disable [search_path=<schema>]
```

样例：

```
host=127.0.0.1 port=54321 user=system password=Test_2026 dbname=test sslmode=disable
host=127.0.0.1 port=54321 user=app_user password=Prod_2026 dbname=app dbname=app sslmode=disable search_path=app_schema
```

### 3. 下游集成完整 go.mod 片段

> **重要**：gplus 仓库内 `third_party/kingbase-gokb/` **不会通过 `go get` 传递给下游**。下游 require gplus 后**仍需自己解压 Gokb + 配 replace 指令**。

```go
module your-app
go 1.24

require (
    github.com/yi-nanping/gplus v0.8.4
    kingbase.com/gokb v0.0.0-00010101000000-000000000000
    gorm.io/driver/postgres v1.6.0
)

replace kingbase.com/gokb => ./third_party/kingbase-gokb
```

集成代码：

```go
import (
    "database/sql"
    _ "kingbase.com/gokb"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"
)

sqlDB, _ := sql.Open("kingbase", dsn)
gormDB, _ := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
```

下游 CI 配置：

```yaml
env:
  GOFLAGS: "-mod=mod"
```

### 4. 官方 Gokb 下载界面元素对照

- 页面 tab：选 **接口驱动**
- 滚动到 GOLANG 一栏
- 右下角红色下载图标 → "不限CPU_不限OS"
- 弹窗 "下载验证" 按页面提示填写
- SHA256 校验：`Get-FileHash <下载文件> -Algorithm SHA256` 对照本 README 给出的预期 hash

### 5. license 申请流程

详见 step ③。

### 6. 诊断 SQL

```sql
-- 验证 PG-compat 模式（Task 0 T11 实测定型为以下任一）
SELECT current_setting('database_mode');  -- 期望 'pg' 或 '0'
SELECT version();
SHOW server_version;
```

### 7. 错误诊断对照表

| 错误信号 | 来源 | 含义 | 修复 |
|---|---|---|---|
| `kingbase.com/gokb: cannot find module providing package` | Go toolchain | go.mod replace 路径错 | 改正斜杠 `./third_party/kingbase-gokb`；确认目录存在 |
| `replacement directory does not exist` | Go toolchain | replace target 不在仓库 | 重新解压 Gokb 到指定路径 |
| `unrecognized import path "kingbase.com/gokb"` | Go toolchain | CI / 下游 module proxy 模式触达非 git 路径 | 配 `GOFLAGS=-mod=mod` + `GOPROXY=off`，确保 third_party/ 在 checkout 中 |
| `password authentication failed for user "system"` | Gokb / lib/pq | DSN 密码与 `SYSTEM_PWD` 不一致 | 重起容器或核对 DSN |
| `license expired` / `connection limit exceeded` | KingbaseES | license 过期 / max_connect=10 超限 | 续 license / `db.SetMaxOpenConns(8)` |
| `database_mode != 'pg'` 测试 t.Fatalf | gplus setup 守卫 | 容器启动模式不对 | 重起容器加 `-e DB_MODE=pg` |
| `connect: connection refused / port 54321 not reachable` | TCP 层 / WSL2 | WSL2 mirrored 网络问题 | `wsl -d Ubuntu-24.04 -e docker ps`，参考 `docs/dev-setup/wsl2-keep-alive.md` |

### 8. 生产部署要点

- **license 挂载**：容器内 `/home/kingbase/license.dat`（金仓启动脚本硬编码不可改）；主机侧建议 `/etc/kingbase/license.dat`（chmod 600，owner=root）
- **license.dat 防泄漏**：含公司名/邮箱/客户编号/硬件指纹/有效期签名——`.gitignore` 显式加 `license.dat`
- **独立 schema**：DSN 加 `search_path=app_schema`
- **非 system 账户**：`CREATE USER app_user PASSWORD '...'; GRANT ALL ON SCHEMA app_schema TO app_user;`
- **连接池**：试用 `db.SetMaxOpenConns(8)`，生产按 license 限额 80%
- **WSL graceful stop**：`docker stop -t 30 kingbase`（避免 SIGKILL 致 license 损坏）
- **协议层限制**：详见下方"未验证场景"

### 9. 未验证场景兜底声明

v0.8.4 仅验证 V9R1C10 PG-compat 模式 + 单实例 + UTF8。

未验证：
- Oracle/MySQL/SQLServer 兼容模式
- DSC 集群 / 读写分离
- V8R6 及更老版本
- 国密 SM3/SM4 加密 / Kerberos 认证
- ARM 平台

下游需自行验证。
````

- [ ] **Step 4: 跑 README markdown 检查**

```powershell
# 验证 markdown 没有破坏现有结构
markdownlint README.md  # 如果装了 markdownlint
```

预期：无错误（或仅样式警告）。

### Task 4.2 CHANGELOG

- [ ] **Step 5: 修改 CHANGELOG.md 加 v0.8.4 段**

定位 CHANGELOG.md 顶部（在 v0.8.3 段上方），追加：

```markdown
## v0.8.4 (2026-05-09)

### 1. 支持版本与兼容性

- KingbaseES V9R1C10（人大金仓主力推广版本，2025-08）
- 仅 **PG-compat 模式**（`DB_MODE=pg` 显式开启）
- V8R6 及更老版本不支持

### 2. 已知限制（KingbaseES）

- license.dat 必需（试用 max_connect=10，生产联系销售）
- PG-compat 模式必须显式开启（默认可能 Oracle/MySQL/SQLServer 模式）
- Gokb v9.x 协议层未充分验证：
  - 不实现 `Conn.CheckNamedValue`
  - 不支持 `LISTEN/NOTIFY`
  - 不支持 `COPY FROM STDIN`
  - 实测结果详见 spec §7（plan T13/T14/T15）
- 官方分发无 docker pull 路径（走官网验证码弹窗 + license 申请）
- 下游 sparse checkout 排除 `third_party/` 会破坏 build

### 3. 新增（KingbaseES V9R1C10 支持）

- GORM Dialector 复用 `gorm.io/driver/postgres v1.6.0`（无新增 GORM 依赖）
- Go 驱动 `kingbase.com/gokb`（官网 2025-08-12 版，vendor 进 git via `third_party/kingbase-gokb/`）
- 测试隔离 `//go:build kingbase` build tag
- 9 测试覆盖（含 IsNull / Empty 区分 + ON CONFLICT DO UPDATE WHERE，相比 DM 多覆盖三个测试点）

### 4. 文档

- README 新增 "KingbaseES 数据库支持" 章节 9 项（含错误诊断对照表 + 生产部署要点 + SHA256 校验）
- spec：`docs/superpowers/specs/2026-05-09-kingbase-support-design.md`
- plan：`docs/superpowers/plans/2026-05-09-kingbase-support-plan.md`
- Task 0 实测结果：`docs/superpowers/plans/2026-05-09-kingbase-task0-results.md`

### 5. 库代码改动

`builder.go: getQuoteChar` 把 `case "postgres", "sqlite", "dm":` 合并为 `case "postgres", "sqlite", "dm", "kingbase":` + 注释泛化（**唯一库代码（非测试）改动 1 行**）。`postgres.Dialector.Name()` 硬编码 "postgres"（gorm.io/driver/postgres v1.6.0 实测），加 "kingbase" 字符串是为了契约一致性 + 未来自定义 Dialector 兜底。

### 6. 技术债

- TD-19: KingbaseES 测试无 CI 守护
- TD-20: Gokb driver 维护风险（官方版每年小更新）
- TD-21: KingbaseES Oracle/MySQL/SQLServer 兼容模式不支持（v0.8.4 仅 PG-compat）
- TD-22: KingbaseES V8R6 老版本不支持
- TD-23: `third_party/kingbase-gokb/` 进 git → 仓库体积增重 ~3-5MB
- TD-24: **v1.0 driver 解耦重构待做**：vendor 进 git 是临时方案
- 复用 TD-12（单模块带可选 driver）

### 7. 收尾说明

仅测试基建 + 1 行库代码 case 字符串新增；既有 v0.8.0 / v0.8.1 / v0.8.2 / v0.8.3 tag 不受影响；GORM 版本锁定保持 v1.31.x。

下一步候选：v1.0 driver 解耦重构（解 TD-24）→ v1.1 KingbaseES Oracle-compat → v1.2 OceanBase / 神舟通用。
```

### Task 4.3 commit 4

- [ ] **Step 6: 验证 docs 改动正确**

```powershell
# 看 README 的方言矩阵
Select-String -Path README.md -Pattern "kingbase" -Context 1,1

# 看 CHANGELOG v0.8.4 段
Select-String -Path CHANGELOG.md -Pattern "v0.8.4" -Context 0,5
```

- [ ] **Step 7: stage + commit 4**

```powershell
git add README.md CHANGELOG.md
git add docs/superpowers/plans/2026-05-09-kingbase-task0-results.md  # Task 0 产出文档

git commit -m "docs(kingbase): README v0.8.4 章节 + CHANGELOG + Task 0 实测结果

- README.md 方言矩阵加 kingbase 行
- README.md 已知方言差异速查 KingbaseES 章节
- README.md 新增 'KingbaseES 数据库支持' 章节 9 项：
  1. 完整安装路径（step ⓪-⑦，含 .gitignore 安全前置）
  2. TEST_KINGBASE_DSN 格式 BNF + 真实样例
  3. 下游集成完整 go.mod 片段 + CI 配置（GOFLAGS=-mod=mod）
  4. 官方 Gokb 下载界面元素对照
  5. license 申请流程
  6. 诊断 SQL（plan T11 实测后定型）
  7. 错误诊断对照表（7 条最常见，标注来源 Go toolchain/Gokb/KB/gplus）
  8. 生产部署要点（license 挂载 / 独立 schema / 连接池 / WSL graceful stop）
  9. 未验证场景兜底声明
- CHANGELOG.md v0.8.4 段（7 子节，沿用 v0.8.3 6 大类 + 收尾说明）
- task0-results.md：16 项实测记录"
```

---

## Task 5: tag v0.8.4 + push

**目的**：发布 v0.8.4。

- [ ] **Step 1: 完整自检**

```powershell
# 默认 build / vet / test
go build ./...
go vet ./...
go test -race -count=1 ./...

# KingbaseES 测试（带 docker）
$env:TEST_KINGBASE_DSN = "host=127.0.0.1 port=54321 user=system password=<实测密码> dbname=test sslmode=disable"
$env:TEST_KINGBASE_REQUIRED = "1"
go test -tags=kingbase -race -count=1 -v ./...

# 多方言并行（仅本机 5 容器环境）
go test -tags="oracle dm kingbase" -race -count=1 ./...

# 覆盖率检查
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | Select-String -Pattern "total"
```

预期：
- 默认测试全过
- KingbaseES 9 测试全过（不允许 t.Skip 误报）
- 覆盖率维持 v0.8.3 水平（≥ 94%）

- [ ] **Step 2: 检查 git 状态干净**

```powershell
git status  # 期望：nothing to commit, working tree clean
git log --oneline -10
```

- [ ] **Step 3: 创建 v0.8.4 tag**

```powershell
git tag -a v0.8.4 -m "v0.8.4: KingbaseES V9R1C10 PG-compat 模式支持

- 复用 gorm.io/driver/postgres v1.6.0（无新 GORM 依赖）
- vendor kingbase.com/gokb（官网 2025-08-12 版）via third_party/
- builder.go 1 行 case 字符串新增（行为等价 postgres 双引号 quoter）
- 9 测试覆盖（build tag 隔离 //go:build kingbase）
- README 9 项（含错误诊断 + 生产部署）

Spec: 11 专家两轮审计修订（5 + 6 专家 / 34 必修 / 33 建议）
Plan: docs/superpowers/plans/2026-05-09-kingbase-support-plan.md"

git tag --list | Select-Object -Last 5  # 验证 tag 已建
```

- [ ] **Step 4: push commits + tag**

```powershell
git push origin main
git push origin v0.8.4

# 验证 GitHub
gh release view v0.8.4 2>$null || gh api repos/yi-nanping/gplus/git/refs/tags/v0.8.4
```

- [ ] **Step 5: 更新项目 memory + 关闭 v0.8.4 候选**

```powershell
# 编辑 C:\Users\11851\.claude\projects\D--Projects-golang-gplus\memory\MEMORY.md
# 把 "最新已发布 tag" 从 v0.8.3 更新为 v0.8.4
# 加一条 "已学习决策"：KingbaseES PG-compat 复用 postgres dialect（vs DM 走 dameng dialect）
```

---

## 完成标准

全部 Task 0-5 ✓ 后，验收清单（来自 spec §10）全部勾选：

### plan 阶段前置

- [ ] A1 license 合规验证（U4）
- [ ] license.dat 已申请到位（U3，2 周阈值内）
- [ ] A2 Gokb 注册名实测（T3）+ kingbaseDriverName 常量值确定
- [ ] B1 vendor 含 go.mod（T4）
- [ ] B4 vendor 子树 vet 验证（T5）
- [ ] 2S6 安全扫描 gosec/staticcheck HIGH 级清零（U5）
- [ ] 2S7 SHA256 校验官网 zip 记入 README（U6）

### 实施验收

- [ ] `third_party/kingbase-gokb/` 解压 Gokb 完整 + 含 go.mod
- [ ] `go.mod` 加 `require` + `replace`（v0.0.0-00010101000000-000000000000 占位）
- [ ] `.gitignore` 加 `!/third_party/kingbase-gokb/**`
- [ ] `git ls-files third_party/kingbase-gokb/` 与解压目录文件数对账
- [ ] 4 个 build tag 测试文件完成
- [ ] `builder.go` 加 kingbase case 字符串
- [ ] `missing_coverage_test.go` 加 kingbase 子测试
- [ ] 默认 `go test ./...` 不变
- [ ] 默认 `go vet ./...` 通过
- [ ] `TEST_KINGBASE_REQUIRED=1 go test -tags=kingbase -v ./...` 9 测试全过
- [ ] PowerShell 多方言并行命令跑通（仅本机）
- [ ] CI 兼容性：仓库自身 GitHub Actions 默认 build 通过
- [ ] README 9 项 + CHANGELOG v0.8.4 段
- [ ] commit 序列 4 个 + tag v0.8.4 推到 GitHub

---

## 后续候选（不在本期）

- **v1.0**：driver 解耦重构（解 TD-24）→ 把所有 driver 推到下游 self-integrate，释放 `third_party/`
- **v1.1**：KingbaseES Oracle-compat 模式（如有用户需求）
- **v1.2**：OceanBase（信创第三大户）/ 神舟通用（信创第四）
- **v1.x**：批量 RETURNING 适配（解 TD-13），保留字列名自动加引号（解 TD-14）



