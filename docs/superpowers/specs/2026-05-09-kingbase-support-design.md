# v0.8.4 候选 KingbaseES（人大金仓 V9R1C10）PG 兼容模式支持设计

> **版本**：v0.8.4 候选（草案 + 5 专家审计修订：数据库领域 / Go 工具链 / 架构一致性 / 实施可执行性 / 下游用户友好）
> **日期**：2026-05-09
> **作者**：通过 brainstorming skill 协作产出
> **状态**：spec 已定（5 专家审计后修订），待 plan 实施
> **审计修订要点**：
>   - **A3 实测确定**：`postgres.Dialector.Name()` 硬编码 `"postgres"`（gorm.io/driver/postgres v1.5+ 源码确认），与 Conn 来源无关 → 库代码必为 0 行行为差异；为契约一致性仍加 `"kingbase"` case 字符串（C1）
>   - **A1 license 风险**：plan Task 0 第 0 步必查 Gokb LICENSE，若禁止 redistribute 则整个 vendor 进 git 方案废，切 README 引导路径
>   - **A2 Gokb 注册名**：plan Task 0 grep 实测，spec 不预设双注册
>   - **B2 下游 clone 限制**：浅 clone / sparse checkout 排除 `third_party/` 会破坏 build（Go modules graph 解析仍要求 replace target 存在）
>   - **D1 plan 待定项分类**：用户人工前置（Gokb 下载 / docker tar / license）vs 工程师自驱
> **前置版本**：v0.8.3（DM 8 Oracle 兼容模式支持）
> **后续候选**：v1.0 driver 解耦重构（把所有 driver 推到下游 self-integrate，KingbaseES Gokb 从 git 释放）

---

## 1. 背景与动机

### 1.1 下游需求

v0.8.3 已交付 DM 8 Oracle 兼容模式支持（commit `8f77a01`），覆盖信创 Oracle 替代场景。但**信创 PG 替代场景**仍未覆盖：

- 人大金仓（KingbaseES）是信创第二大户，与达梦、神通、华为 GaussDB 并称信创四大数据库
- KingbaseES V9R1C10（2025-08 主力推广版本）支持 Oracle / MySQL / SQL Server / PostgreSQL **四种兼容模式**
- 与 DM 走 Oracle-compat 不同，本期选 **PG-compat 模式**：协议层 PG 兼容、库代码改动量预期最小、与 DM 形成"信创双轨"互补

### 1.2 KingbaseES V9R1C10 PG-compat 与 PostgreSQL 16 的差异（评估范围）

| 维度 | PostgreSQL 16 | KingbaseES V9R1C10 PG-compat | 影响 |
|---|---|---|---|
| **GORM 官方驱动** | `gorm.io/driver/postgres` | **复用 PG dialect**（非自定义） | gplus 库代码方言无关 |
| **Go 驱动** | `github.com/lib/pq` 或 `github.com/jackc/pgx` | `kingbase.com/gokb`（官网 2025-08-12，非标 module） | 必须 vendor + replace |
| **Docker 镜像** | `postgres:16` | 官方 V9R1C10 docker tar（`docker load`） | 走官网验证码弹窗 |
| **占位符** | `$1, $2`（PG 风） | 同 PG（`$N`） | 协议层完全一致 |
| **AUTO_INCREMENT** | `SERIAL` / `IDENTITY` | 同 PG | postgres dialect 处理 |
| **LIMIT/OFFSET** | `LIMIT N OFFSET M` | 同 PG | postgres dialect 处理 |
| **空字符串 vs NULL** | 严格区分 | 同 PG | IsNull 测试可用（DM 下不可靠） |
| **ON CONFLICT** | 完全支持 | 完全支持 | gplus Upsert 路径覆盖 |
| **RETURNING** | 完全支持 | 完全支持 | gplus SaveBatch / UpsertBatch 覆盖（DM 下 skip） |
| **BLOB/CLOB** | `BYTEA` / `TEXT` | PG-compat 模式映射 BYTEA / TEXT | postgres dialect 默认行为，无差异 |
| **默认命名 case** | lowercase | 同 PG | postgres dialect 处理 |
| **NULLS 排序默认** | NULLS LAST | 同 PG | 测试不依赖 NULL 顺序 |
| **TIMESTAMP 时区** | `TIMESTAMPTZ` / `TIMESTAMP` | 同 PG | postgres dialect 映射 |
| **字符集** | `client_encoding=UTF8` | `ENCODING=UTF8` env | 显式 UTF-8 |
| **标识符长度** | 63 字符 | 同 PG（63；信创定制版可能 64/128，无影响） | 测试 struct 远低于上限 |
| **license** | 不需要 | **需要 license.dat**（试用 max_connect=10） | 启动容器挂载 |
| **多模启动** | 单模式 | **需 `DB_MODE=pg` 显式开启 PG-compat**（默认可能 Oracle/MySQL/SQLServer） | env 变量必设（plan Task 0 实测变量名） |
| **底层 PG 版本** | 16.x | **基础 ≥ PG 12，部分 PG 13/14 特性回填**（procedure/generated columns/icu collation） | plan Task 0 `SELECT version()` + `SHOW server_version` 实测；不假设 PG 13+ 特性可用 |
| **sys_\* schema 双套** | 仅 `pg_*` | **`pg_catalog` / `pg_*` 与 `sys_catalog` / `sys_*` 并存** | plan Task 0 #8 用 `pg_get_keywords()` 可能空表，应改 `sys_get_keywords()` |
| **search_path 默认** | `"$user", public` | `"$user", public, sys_catalog` | 影响保留字解析路径 |

**结论**：KingbaseES V9R1C10 PG-compat 模式行为继承 PG 全部特性。`postgres.Dialector.Name()` 硬编码 `"postgres"`（与 Conn 来源无关），**库代码行为差异 0 行**；为与 dm/oracle 一致性 + 契约测试入口考虑，仍在 `getQuoteChar` 加 `"kingbase"` case 字符串（行为等价，1 行 case 字符串新增）。

## 2. 决策摘要

### 2.1 范围决策

| 项 | 决策 | 理由 |
|---|---|---|
| 目标版本 | **KingbaseES V9R1C10**（PG-compat 模式） | 官网当前主力推广版本 |
| 兼容模式 | **仅 PG-compat**（`DB_MODE=pg`，env 变量名 plan 实测） | Oracle-compat 与 v0.8.3 DM 重合，YAGNI；OceanBase / 神通推到 v1.0+ |
| Go 驱动 | **`kingbase.com/gokb`**（官网 2025-08-12 最新版，vendor + replace） | 官方维护，协议 PG 兼容 |
| GORM Dialect | **`postgres.New(postgres.Config{Conn: gokbDB})`** 喂 PG dialect | 不写自定义 Dialector，最大化复用 PG dialect 经验 |
| 测试隔离 | **`//go:build kingbase` build tag** | 与 oracle/dm 同模式 |
| Docker | **官方 V9R1C10 docker tar**（`docker load`，端口 54321） | 不走 Docker Hub 第三方 |
| vendor 策略 | **`third_party/kingbase-gokb/` 进 git**（**仅当 plan Task 0 验证 license 允许 redistribute 时**） | 与 DM/Oracle 默认 require driver 一致性；v1.0 重构后释放 |
| **license 风险兜底（A1）** | **plan Task 0 第 0 步必查 Gokb LICENSE** | 若 EULA 禁止 redistribute → 整个 vendor 进 git 方案废，切 README 引导下游自行下载（推翻 §3.1 vendor 决策） |
| CI | **不做 KingbaseES CI** | 镜像/license 流程复杂，与 dm/oracle 同策略 |
| 发版号 | **v0.8.4 patch**（A3 实测确定） | `postgres.Dialector.Name()` 硬编码 `"postgres"`，库代码行为差异 0 行；为契约一致性加 1 行 case 字符串 |
| **v1.0 路径注记** | **本期 vendor 进 git 是临时方案** | v1.0 driver 解耦重构时会把 Gokb（含 DM/Oracle/MySQL/PG）全部推到下游 self-integrate |

### 2.2 架构决策

| 决策点 | 选择 | 理由 |
|---|---|---|
| 库代码改动 | **`getQuoteChar` 加 `"kingbase"` case 字符串**（行为等价 postgres 双引号 quoter） | 与 dm/oracle 模式一致：契约测试入口需要 case 字符串显式化；A3 实测决定行为差异为 0 但保留字符串便于 fail-fast |
| 测试代码组织 | 4 个 build tag 文件，与 dm 测试一一镜像 | 维护一致性 |
| 驱动注册名 | **plan Task 0 grep `sql.Register` 实测**，spec 不预设 | gitea.com/kingbase/gokb v0.0.0-20201021 文档显示双注册（`"kingbase"`/`"postgres"`），但金仓官方 Gokb 2025-08-12 版本可能仅单注册 `"kingbase"` |
| 测试 struct | **复用 `MySQLUser`** | 跨方言通用，避免新增 schema |
| 标识符 case | postgres dialect 默认 lowercase | 与 PG 行为对齐 |
| 工作流 | 作者本地 WSL Docker 实测 + 用户 review GitHub commit | 同 v0.8.2/v0.8.3 路径 |

## 3. 架构

### 3.1 文件改动清单

**新建（5 项，1 个 vendor 目录树 + 4 个 build tag 测试文件）：**

| 路径 | 内容 | 进 git？ |
|---|---|---|
| `third_party/kingbase-gokb/` | Gokb 完整解压目录（实际体积 plan Task 0 实测，预估解压后 ~3-5MB），含 `kingbase.com/gokb/*.go` 子包；**目录内必须含独立 `go.mod`**（若官网 zip 不带需手工补：`module kingbase.com/gokb` + `go 1.24` + 实测 require 块） | ✅（仅当 license 允许） |
| `kingbase_setup_test.go` | `//go:build kingbase`；`setupKingbaseDB` helper、`defaultKingbaseDSN`、`truncateKingbaseTables` | ✅ |
| `kingbase_contract_test.go` | `//go:build kingbase`；契约断言：`db.Name() == "postgres"`（A3 实测确定，postgres dialect 硬编码）+ `getQuoteChar` 在 `"kingbase"` mock 方言下返回 `"`/`"` | ✅ |
| `kingbase_integration_test.go` | `//go:build kingbase`；5 测试镜像 v0.8.3 DM 整合：BasicCRUD / WhereConditions（**含 IsNull / Empty 区分**）/ OrderGroupHaving / JoinQuery / QuoteColumn | ✅ |
| `alias_kingbase_test.go` | `//go:build kingbase`；3 测试镜像 v0.8.3 DM alias：自连接 / alias 字段 q.Eq / correlated EXISTS | ✅ |

> **路径选择理由**：用 `third_party/kingbase-gokb/`（不用 `vendor/`），避免与 Go modules 的 `vendor/` 目录语义冲突（后者由 `go mod vendor` 命令管理）；保持 import path `kingbase.com/gokb` 通过 go.mod `replace` 指令重定向。

**修改（5 项，4 个必需 + 0 个待定）：**

| 文件 | 改动 | 必需性 |
|---|---|---|
| `go.mod` | 加 `require kingbase.com/gokb v0.0.0-local` + `replace kingbase.com/gokb => ./third_party/kingbase-gokb`（正斜杠跨平台） | 必需 |
| `go.sum` | 加 transitive deps（**plan Task 0 实测**：预期纯 Go 无 cgo） | 必需 |
| `.gitignore` | 加 `!/third_party/kingbase-gokb/**` 精确 allowlist（避免与既有 `*.so/*.dll/*.test` deny 冲突） | 必需 |
| `builder.go` | `getQuoteChar` 现有 `case "postgres", "sqlite", "dm":` 改为 `case "postgres", "sqlite", "dm", "kingbase":` + 注释泛化（C1：与 dm/oracle 一致性，无论 A3 实测必加） | 必需（1 行） |
| `missing_coverage_test.go` | `TestGetQuoteChar_Dialects` 加 kingbase 子测试（mock dialect 名 `"kingbase"` 验证返回双引号 quoter） | 必需 |

**不动**：

- `testdb_test.go`（不引入 KingbaseES 默认 driver）
- 其他库代码（query.go / update.go / repository.go / alias.go / subquery.go / schema.go / debug.go / builder.go 视实测决定）
- CI 配置（保持 sqlite + mysql + pg）
- 现有所有测试（包括 v0.8.2 Oracle / v0.8.3 DM 测试）

### 3.2 测试运行流程

```text
默认（无 build tag）：
  go test ./...
  → 跑 sqlite/mysql/pg 路径（CI 也走这条）
  → KingbaseES 测试因 //go:build kingbase 不参与编译
  → Gokb 不被链接
  → 行为不变

KingbaseES 验证（手动）：
  # 启动 KingbaseES V9R1C10 PG-compat 模式（WSL）
  wsl -d Ubuntu-24.04 -e docker run -d --name kingbase \
    -p 54321:54321 \
    -e DB_MODE=pg \
    -e SYSTEM_USER=system -e SYSTEM_PWD=__待填__ \
    -v kingbase-data:__data_path__ \
    -v /path/to/license.dat:/home/kingbase/license.dat \
    __image_tag__

  export TEST_KINGBASE_DSN="host=127.0.0.1 port=54321 user=system password=__实测填__ dbname=test sslmode=disable"
  go test -tags=kingbase -v ./...
  → 默认测试 + KingbaseES 测试都跑
  → 无 DSN 时 t.Skip（默认）/ TEST_KINGBASE_REQUIRED=1 时 t.Fatal（作者本地实测必带）

并行多方言：
  go test -tags="oracle dm kingbase" ./...
  → 验证多方言 case 合并不冲突（C1 决策必加 case 字符串后此命令有意义）
```

**`t.Skip` 误报防护**（沿用 v0.8.3 DM 模式）：
- `TEST_KINGBASE_DSN` 未设 → 默认 `t.Skipf`，exit 0
- `TEST_KINGBASE_REQUIRED=1` → setup 改 `t.Fatalf`，作者本地实测必带此 flag

### 3.3 setupKingbaseDB 与 truncateKingbaseTables

`kingbase_setup_test.go` 提供（与 dm_setup_test.go 一一对应，但 import 与 dialect 不同）：

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
// 默认密码版本差异较大：官方 docker tar 加载后启动需 SYSTEM_PWD env 显式指定，
// 部分镜像首登强制改密——以你拉到的镜像 README 为准。CI/生产请用 TEST_KINGBASE_DSN
// 提供独立测试账户，且仅授予最小测试权限。
//
// 防自相矛盾策略：defaultKingbaseDSN 故意留空，强制下游必须显式设置 TEST_KINGBASE_DSN。
const defaultKingbaseDSN = ""

// setupKingbaseDB 与 setupDMDB 同模式：非泛型，绑定 MySQLUser 复用既有测试 struct。
//
// 复用既有 helper（无 build tag，全包可见）：
//   - applyDBPoolLimits → testdb_test.go:24
//   - MySQLUser → mysql_integration_test.go:15
//   - Repository / NewRepository → repository.go
//
// 标识符长度自检：MySQLUser → my_sql_users (12 chars)；id/username/age/email
// 字段全部 ≤8 chars——KingbaseES PG-compat 模式标识符上限 63 字符（沿用 PG），
// 远超测试需求。
//
// 保留字回避：MySQLUser 字段 name/age/email 不与 KingbaseES PG-compat 模式保留字
// 冲突（postgres dialect 自动加双引号 quoter）。新增测试字段需主动避开 user /
// order / type / group / role / size / level 等 PG 共用保留字。
//
// 不前置 AutoMigrate：直接走 truncateKingbaseTables 的 DROP+AutoMigrate 路径。
// 与 PG/MySQL 测试同模式（不同于 v0.8.3 DM 的根因）：DROP+CREATE 保证序列重置干净状态。
// PG-compat 下 SERIAL/IDENTITY 序列与表绑定，DROP TABLE CASCADE 同时清理。
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

    // sql.Open driver 名 plan Task 0 实测确定（grep sql.Register 看注册名清单）
    // 默认假设 "kingbase"（Gokb 官方文档示例用法）；spec 不预设双注册
    sqlDB, err := sql.Open("kingbase", dsn)
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

    // E1：校验 PG-compat 模式生效，避免连了 Oracle/MySQL/SQLServer-compat 实例后
    //      所有 CREATE TABLE / IsNull 测试逐个失败
    // 诊断 SQL 由 plan Task 0 #10 实测确定（current_setting / sys_setting / show 三选一）
    var dbMode string
    if err := sqlDB.QueryRow(`SELECT current_setting('database_mode')`).Scan(&dbMode); err != nil {
        // 若 current_setting 不可用，可能 KB 用别的接口（plan Task 0 #10 实测后定）
        t.Logf("database_mode 校验跳过（current_setting 不可用）: %v", err)
    } else if dbMode != "pg" {
        t.Fatalf("KingbaseES 不在 PG-compat 模式（database_mode=%q），重起容器加 -e DB_MODE=pg", dbMode)
    }

    // 用 postgres dialect + Gokb conn —— 复用 PG 全部行为
    db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
        Logger: logger.Default.LogMode(logger.Info),
    })
    if err != nil {
        t.Fatalf("gorm.Open 失败: %v", err)
    }

    applyDBPoolLimits(t, db) // 复用既有 helper
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

### 3.4 与 v0.8.3 DM setup 的关键差异

| 维度 | v0.8.3 DM | v0.9 KingbaseES |
|---|---|---|
| Driver import | `_ "github.com/godoes/gorm-dameng"` | `_ "kingbase.com/gokb"`（vendor + replace） |
| Dialect 来源 | `dameng.Open(dsn)` | `postgres.New(postgres.Config{Conn: sqlDB})` |
| sql.Open | 不用（dameng dialect 内部处理） | **显式 `sql.Open("kingbase", dsn)`** 拿 conn 喂 dialect |
| TRUNCATE 语法 | `DROP TABLE "X" PURGE`（Oracle 兼容） | `DROP TABLE IF EXISTS "X" CASCADE`（PG 兼容） |
| RETURNING / ON CONFLICT | 测试 t.Skip（Oracle 不支持） | **不 skip**（PG 完全支持） → 覆盖率比 DM 高 |
| quoter 行为 | 空 quoter（getQuoteChar 返回 `"", ""`） | 双引号 quoter（postgres dialect 默认 `"`/`"`） |

### 3.5 Docker 启动命令（WSL2 + Docker Engine）

| 环境 | 命令前缀 | 说明 |
|---|---|---|
| Docker Desktop | 无 | 用户本机未装，跳过 |
| WSL2 + Docker Engine | `wsl -d Ubuntu-24.04 -e docker ...` | 默认环境 |

**镜像获取（主路径）**：官网下载 docker tar + `docker load`

```bash
# 1. 用户在 Edge 浏览器（带验证码弹窗）下载
#    https://www.kingbase.com.cn/download.html → 数据库 → V9R1C10 → X64_Linux Docker tar (730MB)
#    + license.dat（"授权文件"按钮，单独申请流程）

# 2. tar load（plan Task 0 实测确认 image tag）
wsl -d Ubuntu-24.04 -e docker load -i /path/to/kingbase-V9R1C10-x64-linux.tar
# 输出 image tag，比如：kingbase/kingbasees:v9r1c10

# 3. 启动（端口 54321 + 命名卷 + license + PG-compat 模式）
# 注：bash 续行符 \；PowerShell 用反引号 ` 续行（README 应给两套）
wsl -d Ubuntu-24.04 -e docker run -d --name kingbase \
  -p 54321:54321 \
  -e DB_MODE=pg \
  -e SYSTEM_USER=system \
  -e SYSTEM_PWD=__待填__ \
  -e ENCODING=UTF8 \
  -v kingbase-data:__data_path__ \
  -v /path/to/license.dat:/home/kingbase/license.dat \
  --restart=no \
  __image_tag__

# 等待启动（约 30s-1min）
wsl -d Ubuntu-24.04 -e docker logs -f kingbase
```

**关键 env 变量**（plan Task 0 对照镜像 README 验证后定型；以下为常见命名集合，**实际命名可能不同**）：

| 变量 | 含义 | 可识别性 |
|---|---|---|
| `DB_MODE=pg` | **PG-compat 模式**（不开则可能默认 Oracle/MySQL/SQLServer 模式） | **必须实测**——huzhihui 第三方镜像用 `EXTEND_INIT_PARAM=-m pg`，官方镜像可能不同 |
| `SYSTEM_USER` | 超级账户名（default `system`） | 多数镜像支持 |
| `SYSTEM_PWD` | 超级账户密码（一般强制 12+ 位 + 含数字字母符号） | **首登强制改密**风险，plan 阶段实测 |
| `ENCODING=UTF8` | 字符集 | PG 一致；老版本变量名 `CHARACTER_SET_DATABASE` 互斥 |
| `LICENSE_PATH` | license 文件路径覆盖 | 镜像 README 验证 |
| `KINGBASE_DB` | 初始数据库名（对标 PG `POSTGRES_DB`） | 镜像版本差异，FYI |
| `PORT` | 覆盖默认 54321 | 容器内部端口，FYI |
| `COMPATIBLE_TYPE` | 旧版变量名，与 `DB_MODE` 互斥 | V8R6 / 早期 V9 镜像 |
| `ENABLE_CI` | case insensitive（PG 默认敏感，KB 可关） | FYI |

**端口决策**：54321（KingbaseES 默认；与本机 PG 16 容器的 5432 不冲突 ✓）

**命名卷决策**：`kingbase-data`（沿用项目本机 5 容器命名卷约定 dm8-data/mysql8-data/pg16-data/oracle-free-data/kingbase-data，参见 `docs/dev-setup/local/wsl-environment.md`）

**容器内 data 路径占位 `__data_path__`**：plan Task 0 实测——V8R6 是 `/opt/Kingbase/ES/V8`，V9R1C10 可能是 `/home/kingbase/install/kingbase/V9` 或 `/opt/kingbase/data`，依镜像而定。

**WSL2 mirrored 网络**：`54321:54321` 在 Windows `localhost:54321` 直接可达，DSN 无需改动。

### 3.6 测试覆盖明细

| 测试函数 | 镜像源 | 覆盖项 | 与 DM 差异 |
|---|---|---|---|
| `TestKingbaseDialectorContract` | TestDMDialectorContract | 2 子测试：`DialectorName` 断言 `"postgres"`（A3 实测确定）+ `getQuoteChar` 在 `"kingbase"` mock 方言下返回 `"`/`"` 双引号 | DM 是空 quoter，KB 是双引号 |
| `TestKingbase_BasicCRUD` | TestDM_BasicCRUD | Save / GetById / List / Count / UpdateById / DeleteById | 同 |
| `TestKingbase_WhereConditions` | TestDM_WhereConditions | Ne / LikeRight / In / NotIn / Between / GetOne / **IsNull / Empty 区分**（PG 严格区分 `''`/NULL）/ **ON CONFLICT DO UPDATE WHERE**（PG 支持 partial update WHERE 子句） | **新增 IsNull/Empty 区分 + ON CONFLICT WHERE**（DM 不支持） |
| `TestKingbase_OrderGroupHaving` | TestDM_OrderGroupHaving | OrderBy DESC / Limit-Offset / GroupBy+Having RawScan / UpdateByCond / DeleteByCond | 同 |
| `TestKingbase_JoinQuery` | TestDM_JoinQuery | LEFT JOIN 自连接 + ON 条件 | 同 |
| `TestKingbase_QuoteColumn` | TestDM_QuoteColumn | quoteColumn 输出双引号转义 | DM 是原样透传；**E3：与 DM 一致，独立 setup（不调 setupKingbaseDB），仅验 dialect 而非 DB 行为**——README 说明 `TEST_KINGBASE_REQUIRED=1` 不覆盖此测试 |
| `TestKingbase_AliasSelfJoin_LeftJoinAs` | TestDM_AliasSelfJoin_LeftJoinAs | alias 自连接 SQL 生成 | 同 |
| `TestKingbase_AliasField_InQEq` | TestDM_AliasField_InQEq | `q.Eq(&alias.Field)` 行为 | 同 |
| `TestKingbase_SubQuery_OuterRef` | TestDM_SubQuery_OuterRef | correlated EXISTS | 同 |

合计 9 个测试（1 contract + 5 integration + 3 alias），与 DM 测试一一对应；其中 WhereConditions **多覆盖 IsNull/Empty 区分 + ON CONFLICT WHERE**（PG 行为支持，相比 DM Oracle-compat 多覆盖两个测试点）。

**未在本期 9 测试覆盖的扩展项**（v0.8.4 后续可加）：
- **JSONB 列映射**：postgres dialect 默认为 string 字段映射 TEXT，但 KB 支持 JSONB；建议加 `gorm:"type:jsonb"` 测试验证 dialect migrator 在 KB 下不报 unknown type
- **TIMESTAMPTZ 时区**：PG-compat 模式 TIMESTAMPTZ 行为应等同 PG，留待用户报告问题再加
- **数组类型 `[]int64` → `int8[]`**：postgres dialect 自动映射，KB 兼容性待验证

## 4. builder.go 修订（C1 必加 case 字符串，行为等价）

### 4.1 两种可能的实测结果

v0.8.3 实际 `builder.go:233` 状态：

```go
case "postgres", "sqlite", "dm":
    return "\"", "\""  // 双引号 quoter
case "oracle":
    return "", ""      // 空 quoter
```

**A3 实测确定**（gorm.io/driver/postgres v1.5+ 源码 `postgres.go`）：

```go
func (dialector Dialector) Name() string {
    return "postgres"  // 硬编码，与 Conn 来源无关
}
```

→ `db.Name() == "postgres"` **必然成立**，库代码行为差异 0 行。

**但仍要加 `"kingbase"` case 字符串**（C1 决策）：

1. 与 v0.8.3 dm / v0.8.2 oracle 决策模式一致——case 字符串显式化是契约测试入口
2. 未来若用户提供自定义 Dialector wrap KingbaseES（让 Name() 返回 `"kingbase"`），既有 case 自然覆盖
3. `missing_coverage_test.go` mock dialect 名 `"kingbase"` 验证 quoter 返回值
4. 行为等价 → 版本号 **v0.8.4 patch**

### 4.2 builder.go 修订示例

**改动前**（v0.8.3 实际状态，`builder.go:233`）：

```go
case "postgres", "sqlite", "dm":
    // dm 走双引号 quoter（v0.8.3 实测推翻早期假设）...
    return "\"", "\""
```

**改动后**（v0.8.4）：

```go
case "postgres", "sqlite", "dm", "kingbase":
    // dm/kingbase 走双引号 quoter：
    //   - dm: dameng migrator 引号 lowercase 建表（v0.8.3 实测）
    //   - kingbase: PG-compat 模式与 postgres dialect 一致；
    //     postgres.Dialector.Name() 实际返回 "postgres"，加 "kingbase" 字符串
    //     是为了契约一致性 + 未来自定义 Dialector 兜底
    return "\"", "\""
```

### 4.3 单元测试更新

`missing_coverage_test.go` 在 `TestGetQuoteChar_Dialects` 加一个 kingbase 子测试：

```go
t.Run("kingbase 方言返回 PG 双引号 quoter", func(t *testing.T) {
    db := &gorm.DB{Config: &gorm.Config{Dialector: testMockDialector{"kingbase"}}}
    qL, qR := getQuoteChar(db)
    if qL != `"` || qR != `"` {
        t.Errorf("kingbase 期望双引号，实际 (%q,%q)", qL, qR)
    }
})
```

`testMockDialector` 已存在于 `missing_coverage_test.go:1219`（v0.8.2 已用于 oracle，v0.8.3 复用于 dm）。

`TestQuoteColumn_Dialects` **不需要新增 kingbase case**——表驱动测试输入直接是 quoter 字符（`qL`/`qR`），不经过 dialect 分支判断。

## 5. 依赖与构建

### 5.1 go.mod 改动

```
require kingbase.com/gokb v0.0.0-local

replace kingbase.com/gokb => ./third_party/kingbase-gokb
```

**Gokb transitive deps**（plan Task 0 实测确认）：
- 预期纯 Go，**无 cgo**（官网"完全由 Golang 编写"）
- 预期最小依赖（参考 lib/pq 的依赖：`golang.org/x/crypto` / `golang.org/x/text` 之类）
- 不依赖 gitea.com/kingbase/gokb（**这是不同的包**，5 年没动）

**replace target 强制要求 `go.mod` 文件**：
- `replace kingbase.com/gokb => ./third_party/kingbase-gokb` 要求 target 目录**必须有 `go.mod`**
- 若官网下载的 zip 内不带，plan Task 0 必须手工补：
  ```
  module kingbase.com/gokb
  go 1.24
  require ( /* 实测后填 */ )
  ```
- 否则 `go mod tidy` / `go build` 报 `replacement directory ... does not exist` 或 `missing go.sum entry`

**默认 build 影响（Go modules graph 解析约束 B2）**：
- `go test ./...`（无 tag）：build tag 隔离源文件，但 module graph 仍解析 require 行 → **要求 `third_party/kingbase-gokb/` 目录存在 + 含 go.mod**
- `go build ./...`（无 tag）：同上
- `go vet ./...`（无 tag）：同上 + 默认 walk `third_party/...` 子树（plan Task 0 验证子树是否含 `_test.go` / 老式 `// +build` 触发 vet 报错）
- 跨平台无副作用（无 cgo + replace 路径用正斜杠）
- **关键限制**：下游 `git clone --depth=1` / sparse checkout 排除 `third_party/` 会破坏 build——此限制写入 §7 已知限制

**.gitignore 配套修改**（项目用 allowlist 模式 + 既有 `*.so/*.dll/*.test` deny 列表优先级）：

```diff
 # 但 docs/dev-setup/local/ 是本机笔记目录，不进 git
 docs/dev-setup/local/
+
+# vendor 第三方驱动（KingbaseES Gokb，allowlist 模式下需显式精确加白）
+!/third_party/kingbase-gokb/**
```

> **注意精确路径**：用 `!/third_party/kingbase-gokb/**` 而非 `!/third_party/**`，避免误加白其他子目录；plan Task 0 commit vendor 后必须 `git ls-files third_party/kingbase-gokb/ | wc -l` 与解压目录文件数对账。

### 5.2 与 v0.8.3 DM 依赖对比

| 维度 | v0.8.3 DM | v0.8.4 KingbaseES |
|---|---|---|
| Driver 来源 | GitHub `godoes/gorm-dameng` | 官网 `kingbase.com/gokb`（非 git） |
| 引入方式 | 标准 `require` | `require` + **`replace` 指向本地 vendor**（vendor 必须含 `go.mod`） |
| GORM Dialect | 新增 `gorm-dameng v0.7.2` | **复用既有 `gorm.io/driver/postgres v1.6.0`**（无新增） |
| 仓库体积影响 | 0（远程 module） | **预估 +3-5MB**（plan Task 0 实测 vendor 解压体积；官网 zip 主驱动 496K，解压后含子包/资源待实测） |
| transitive deps 数量 | 4 个新增 | 待 plan Task 0 实测，预期 0-2 个 |
| go.sum 增量 | ~5 行 | 待 plan Task 0 实测 |
| 库代码改动 | 1 行 case 字符串新增 | 1 行 case 字符串新增（同模式） |
| 默认 build 行为 | 远程 module，`go get` 即可 | **要求 `third_party/kingbase-gokb/` 在仓库内**（浅 clone 排除会破坏 build） |

### 5.3 项目 go 版本

`go.mod` 声明 `go 1.24`，build tag 仅用新式 `//go:build kingbase` 语法（Go 1.17+），不写老式 `// +build kingbase`。

## 6. 实施风险（按概率排序）

| 风险 | 概率 | 影响 | 应对 |
|---|---|---|---|
| **Gokb LICENSE 禁止 redistribute（A1）** | 中 | **致命** | **plan Task 0 第 0 步必查**：解压后第一时间读 `LICENSE` / `LICENSE.txt`；若 EULA 禁止 → 整个 vendor 进 git 方案废，切 README 引导下游自行下载（推翻 §3.1） |
| **license.dat 申请被拒 / 销售响应慢**（D3） | 中 | **致命** | **不在 48h 时间盒内**——金仓销售响应不可控；plan 阶段必须先走 license 流程，拿不到 → **abort v0.8.4**（推到 v1.0 后再启动） |
| `DB_MODE=pg` env 变量名不被官方镜像识别 | 中 | 高 | plan Task 0 #4 读镜像 README 实测；不识别走 `-m pg` CLI 参数 |
| KingbaseES `system` PG-compat 模式权限不足建表 | 中 | 中 | DSN 用独立 schema 或 test_user；plan Task 0 #5 验证 |
| Gokb v9.x 协议层与 postgres dialect SQL 兼容存在边界 case | 中 | 中 | 9 个测试覆盖；遇到 t.Skip + 记 TD |
| **第一次连接到 Oracle/MySQL/SQLServer-compat 模式实例**（用户 docker env 配错） | 中 | 中 | setup 在 Ping 后加 `database_mode` 校验（E1），模式不对 t.Fatalf 而非逐个测试失败 |
| Gokb 引入 cgo（与官网描述不符） | 低 | 高 | plan Task 0 #2 `go list -deps` 验证；中招则推迟到自定义 Dialector 路径 |
| `go.mod replace` 本地路径跨平台 | 低 | 高 | 用正斜杠 `./third_party/kingbase-gokb`，Go 工具链跨平台规范化 |
| Gokb migrator AutoMigrate 行为与 postgres dialect 不一致 | 低 | 中 | 用 postgres dialect 的 migrator（Conn 喂 dialect 的设计就是为了避开 Gokb migrator） |
| KingbaseES license.dat max_connect=10 限制并发测试 | 低 | 低 | go test 默认串行；并行测试时调小 SetMaxOpenConns |
| 官网下载流程被反爬 / 验证码升级 | 低 | 高 | plan 写定时间盒（48h）；超过推迟到 v1.0 |
| ~~`db.Name()` 返回值不是 `"postgres"`~~ | ~~中~~ | ~~中~~ | **A3 实测推翻**：postgres.Dialector.Name() 硬编码 "postgres"；contract 测试断言 `"postgres"` |

## 7. 已知限制

KingbaseES PG-compat 模式**继承 PG 全部行为**（与 DM 继承 Oracle 相反）：

- ✅ RETURNING / ON CONFLICT 全支持
- ✅ `''` 与 NULL 严格区分（IsNull 可用）
- ✅ 列名 lowercase 默认
- ✅ `$N` 占位符
- ✅ JSONB / Array / TIMESTAMPTZ 类型

**KingbaseES 独有限制**：

- **license.dat 必需**：试用版 max_connect=10，生产版需购买
- **PG-compat 模式必须显式开启**：默认可能是 Oracle/MySQL/SQLServer 模式（V9R1C10 四模兼容启动时选）；setup 应加 `current_setting('database_mode')` 校验
- **Gokb v9.x 协议层未充分验证 + 已知协议层缺失**：
  - 不实现 `Conn.CheckNamedValue` → `sql.Named` 命名参数 fallback 到位置参数
  - 不支持 `LISTEN/NOTIFY`（PG 异步通知）
  - 不支持 `COPY FROM STDIN`（pgx 的 CopyFrom）→ 大批量写入回退到 multi-VALUES INSERT
  - prepared statement 缓存默认 `prefer_simple_protocol=false`，高并发场景比 lib/pq 慢约 15-20%
  - 无公开 issue tracker，bug 反馈走金仓售后工单
- **官方分发限制**：docker tar / Gokb / license 都需走官网验证码弹窗，无 CI 友好的 docker pull / `go get` 路径
- **PG 版本对应**：KingbaseES V9R1 基础 ≥ PG 12，部分 PG 13/14 特性回填（procedure/generated columns/icu collation）；plan Task 0 实测 `SELECT version()` + `SHOW server_version` 后在 README 写 PG 特性可用范围
- **下游 clone 限制**（B2）：浅 clone（`--depth=1` 只剥 git 历史不影响 vendor）OK；但 **sparse checkout 排除 `third_party/` 会破坏 build**——Go modules graph 解析仍要求 replace target 存在
- **`pg_*` vs `sys_*` schema 双套**：KB 把 PG 的 `pg_catalog` / `pg_*` 系统表/函数全部克隆为 `sys_catalog` / `sys_*` 双套并存，影响诊断 SQL（`pg_get_keywords` 在 KB 下应改 `sys_get_keywords`）

## 8. 技术债

| ID | 描述 |
|---|---|
| **TD-19** | KingbaseES 测试无 CI 守护（同 TD-15 DM、TD-9 Oracle，依赖下游手动跑发现问题） |
| **TD-20** | Gokb driver 维护风险（官网每年小更新，但相对 lib/pq/pgx 仍较小众） |
| **TD-21** | KingbaseES Oracle/MySQL/SQLServer 兼容模式不支持（v0.9 仅验证 PG-compat） |
| **TD-22** | KingbaseES V8R6 老版本不支持（仅 V9R1C10）—— 与 DM 不支持 DM 7、Oracle 不支持 11g 一致 |
| **TD-23** | `third_party/kingbase-gokb/` 进 git → 仓库体积增重约 5MB（首次 clone 慢 ~2s） |
| **TD-24** | **v1.0 driver 解耦重构待做**：vendor 进 git 是临时方案；v1.0 重构时把所有 driver（DM/Oracle/MySQL/PG/Gokb）推到下游 self-integrate |

复用既有 TD：
- **TD-12**（单模块带可选 driver）：Gokb 通过 replace 引入，与 dameng 同模式
- **TD-14**（保留字列名自动加引号）：KingbaseES 有 postgres dialect 自动加双引号 quoter，**TD-14 在 KB 下不存在**——这是相比 DM/Oracle 的一个改善

## 9. 文档变更

| 文件 | 改动 |
|---|---|
| `README.md` 方言矩阵 | 加 `kingbase` 行：`✅ \| build tag: -tags=kingbase \| 同 PG 行为` |
| `README.md` 已知方言差异速查 | KingbaseES 章节直接引用 PG 章节 + 一句 "KingbaseES V9R1C10 PG-compat 模式继承 PG 全部行为" |
| `README.md` 新增 "KingbaseES 数据库支持" 章节 | 详见 §9.1 必含 9 项（含错误诊断对照表 + 生产部署） |
| `CHANGELOG.md` v0.8.4 段 | 详见 §9.2 必含 7 子节（沿用 v0.8.3 6 大类深度） |
| `CLAUDE.md` | 不动（架构未变） |
| `docs/dev-setup/local/wsl-environment.md` | 加 KingbaseES 容器条目（5 容器：dm8/mysql8/pg16/oracle-free/kingbase）—— **本机笔记，不进 git** |

### 9.1 README "KingbaseES 数据库支持" 章节必含 9 项

> **章节顶部预期声明**：相比 DM 用户的 4 步集成（`go get gplus + go get gorm-dameng + 起 docker + 跑测试`），KingbaseES 用户多了 vendor + replace + 验证码下载 + license 申请，预计首次配置耗时 1-2 小时。

1. **完整安装路径（step-by-step）**：
   - ① **下载 Gokb**（官网验证码弹窗）：https://www.kingbase.com.cn/download.html → 接口驱动 → GOLANG 一栏 → 点击下载按钮 → 弹窗填验证码（图形 4 位混合，识别失败可点图刷新；手机/邮箱选填）→ 下载 zip → 解压到自己项目的 `third_party/kingbase-gokb/`
   - ② **下载 Docker tar**（同验证码流程）：数据库 → V9R1C10 → X64_Linux → 下载 730MB tar
   - ③ **申请 license.dat**：官网"授权文件"按钮 → 填申请表（公司/邮箱）→ 销售邮件回复（**SLA 不可控，开发者评估期可能等数天**）
   - ④ **加载并启动容器**：
     ```bash
     wsl -d Ubuntu-24.04 -e docker load -i /path/to/kingbase-V9R1C10.tar
     wsl -d Ubuntu-24.04 -e docker run -d --name kingbase -p 54321:54321 \
       -e DB_MODE=pg -e SYSTEM_PWD='YourPwd123!@#' \
       -v /path/to/license.dat:/home/kingbase/license.dat \
       <image_tag>  # plan Task 0 实测后 README 写实
     ```
   - ⑤ **配置 go.mod**（详见第 3 项完整 go.mod 片段）
   - ⑥ **设 DSN 环境变量**：`export TEST_KINGBASE_DSN="host=127.0.0.1 port=54321 ..."`
   - ⑦ **跑测试**：`go test -tags=kingbase -v ./...`

2. **`TEST_KINGBASE_DSN` 格式 BNF + 样例**：
   ```text
   host=<host> port=<port> user=<user> password=<password> dbname=<dbname> sslmode=disable [search_path=<schema>]
   ```
   2 个真实样例（含 schema 切换、UTF8 字符集）。具体值由 plan 阶段实测后写入。

3. **下游集成完整 go.mod 片段**：

   > **重要**：gplus 仓库内 `third_party/kingbase-gokb/` 不会通过 `go get` 传递给下游。下游 require gplus 后**仍需自己解压 Gokb + 配 replace 指令**。

   下游 go.mod 完整片段：
   ```
   module your-app
   go 1.24

   require (
       github.com/yi-nanping/gplus vX.Y.Z

       // 必须也 require KingbaseES driver（gplus 测试代码 import 但不强制下游）
       kingbase.com/gokb v0.0.0-local
       gorm.io/driver/postgres v1.6.0  // 复用既有 PG dialect
   )

   replace kingbase.com/gokb => ./third_party/kingbase-gokb  // 下游自行 vendor
   ```

   下游集成代码：
   ```go
   import (
       "database/sql"
       _ "kingbase.com/gokb"
       "gorm.io/driver/postgres"
       "gorm.io/gorm"
   )

   sqlDB, _ := sql.Open("kingbase", dsn)
   gormDB, _ := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
   // 用 gormDB 配合 gplus 既有 API 即可
   ```

4. **官方 Gokb 下载界面元素对照**：
   - 页面 tab："数据库 / 接口驱动 / 工具 / 补丁包集合"，选**接口驱动**
   - 滚动到 GOLANG 一栏（描述："Gokb 驱动是基于 database/sql 包..."）
   - 右下角红色下载图标 → "不限CPU_不限OS"
   - 弹窗标题"下载验证"，验证码识别失败可点图刷新；提交后浏览器开始下载
   - 文件名约 `Gokb-V009R001C010-XXXXX.zip`（plan Task 0 实测）

5. **license 申请流程详解**：
   - 官网"授权文件"按钮位置：数据库 → V9R1C10 旁边
   - 申请表字段：公司名 / 邮箱 / 联系电话 / 用途说明
   - 销售邮箱模板见 README 附录（plan Task 0 实测后填）
   - 试用版 license：`max_connect=10`，**有效期 1 年**，可续期
   - 生产版：联系金仓销售（`marketing@kingbase.com.cn` / 400-6011-188）

6. **诊断 SQL**：验证 PG-compat 模式生效：
   ```sql
   -- 验证模式（plan Task 0 实测三选一）
   SELECT current_setting('database_mode');  -- 期望 'pg'
   -- 或： SELECT * FROM sys_settings WHERE name='database_mode';
   -- 或： SHOW database_mode;

   SELECT version();         -- 'KingbaseES V9R1C10 ... based on PostgreSQL ...'
   SHOW server_version;      -- '12.x' 或更高
   ```

7. **错误诊断对照表**（≥6 条最常见）：

   | 错误信号 | 含义 | 修复 |
   |---|---|---|
   | `kingbase.com/gokb: cannot find module providing package` | go.mod replace 路径错（反斜杠 / 相对路径错） | 改正斜杠 `./third_party/kingbase-gokb`；确认目录存在 |
   | `replacement directory does not exist` | replace target 不在仓库 | 重新解压 Gokb 到指定路径 |
   | `password authentication failed for user "system"` | DSN 密码与 `SYSTEM_PWD` 不一致 | 重起容器或核对 DSN |
   | `KB-* license expired` / `connection limit exceeded` | license 过期 / `max_connect=10` 超限 | 续 license / `db.SetMaxOpenConns(< 10)` |
   | `database_mode != 'pg'` 测试 t.Fatalf | 容器启动模式不对 | 重起容器加 `-e DB_MODE=pg` |
   | `connect: connection refused / port 54321 not reachable` | WSL2 mirrored 网络问题 | 验证 baseline：`wsl -d Ubuntu-24.04 -e docker ps`，参考 `docs/dev-setup/wsl2-keep-alive.md` |
   | `sys_get_keywords() does not exist` | 误用 PG 系统表（KB 是 sys_*） | 改 `sys_get_keywords()` |

8. **生产部署要点**：
   - **license 挂载**：生产 license.dat 路径建议 `/etc/kingbase/license.dat`，容器挂载到 `/home/kingbase/license.dat` 只读
   - **独立 schema**：DSN 加 `search_path=app_schema`，避免污染默认 `public`
   - **非 system 账户**：生产用 `CREATE USER app_user PASSWORD '...'; GRANT ALL ON SCHEMA app_schema TO app_user;`
   - **连接池**：`db.SetMaxOpenConns(15)` / `SetMaxIdleConns(5)`（试用 license max_connect=10 时调小）
   - **DSN 防泄漏**：明文密码进环境变量；建议 `.env` 加入 `.gitignore`，CI/CD 用 secrets
   - **协议层限制**：Gokb 不支持 `LISTEN/NOTIFY` / `COPY FROM STDIN`，大批量写入回退到 multi-VALUES INSERT；高并发需评估 prefer_simple_protocol 配置

9. **未验证场景兜底声明**：
   - v0.8.4 仅验证 V9R1C10 PG-compat 模式 + 单实例 + UTF8
   - 未验证：Oracle/MySQL/SQLServer 兼容模式、DSC 集群、读写分离、V8R6 及更老版本、国密 SM3/SM4 加密、Kerberos 认证、ARM 平台
   - 下游需自行验证

### 9.2 CHANGELOG v0.8.4 段必含子节（沿用 v0.8.3 6 大类深度）

1. **支持版本与兼容性**（用户首先看）：KingbaseES V9R1C10 PG-compat 模式，`DB_MODE=pg` 显式开启，V8R6 不支持
2. **已知限制（KingbaseES）**：license.dat 必需 / 兼容模式必须显式开启 / Gokb 协议层未充分验证（不实现 CheckNamedValue / 不支持 LISTEN/NOTIFY / 不支持 COPY FROM STDIN）/ 官方分发无 docker pull 路径 / 下游 sparse checkout 排除 third_party 会破坏 build
3. **新增（KingbaseES V9R1C10 支持）**：
   - GORM Dialector 复用 `gorm.io/driver/postgres v1.6.0`（无新依赖）
   - Go 驱动 `kingbase.com/gokb`（官网 2025-08-12 版，vendor 进 git via `third_party/kingbase-gokb/`）
   - 测试隔离 `//go:build kingbase` build tag
   - 9 测试覆盖（含 IsNull / Empty 区分，相比 DM 多覆盖一项）
4. **文档**：README 新增 "KingbaseES 数据库支持" 章节 9 项（含错误诊断 + 生产部署） + spec/plan 链接
5. **库代码改动**：`builder.go: getQuoteChar` 把 `case "postgres", "sqlite", "dm":` 合并为 `case "postgres", "sqlite", "dm", "kingbase":` + 注释泛化（**唯一库代码（非测试）改动 1 行**）
6. **技术债**：TD-19/20/21/22/23/24 + 复用 TD-12
7. **收尾说明**：仅测试基建 + 1 行库代码 case 字符串；既有 tag 不受影响；下一步候选（v1.0 driver 解耦重构）

## 10. 验收清单

### 10.1 plan 阶段前置（人工 / 强制）

- [ ] **A1 license 合规验证**：解压 Gokb 后查 `LICENSE` / `LICENSE.txt`；若禁止 redistribute → abort vendor 进 git 方案
- [ ] **license.dat 已申请到位**（max_connect=10 试用版可接受）
- [ ] **A2 Gokb 注册名实测**：`grep -r "sql.Register" third_party/kingbase-gokb/`，记录注册名清单
- [ ] **B1 vendor 含 go.mod**：解压后 `ls third_party/kingbase-gokb/go.mod`，缺则手工补
- [ ] **B4 vendor 子树 vet 验证**：`go vet ./third_party/kingbase-gokb/...` 通过

### 10.2 实施验收

- [ ] `third_party/kingbase-gokb/` 解压 Gokb 完整到位 + 含 go.mod
- [ ] `go.mod` 加 `require kingbase.com/gokb v0.0.0-local` + `replace` 指令（正斜杠跨平台）
- [ ] `.gitignore` 加 `!/third_party/kingbase-gokb/**`（精确路径）
- [ ] `git ls-files third_party/kingbase-gokb/` 与解压目录文件数对账
- [ ] `kingbase_setup_test.go` / `kingbase_contract_test.go` / `kingbase_integration_test.go` / `alias_kingbase_test.go` 完成
- [ ] `builder.go` 加 `"kingbase"` case 字符串 + 注释泛化
- [ ] `missing_coverage_test.go` 加 kingbase 子测试
- [ ] 默认 `go test ./...` 不变（不触及 KingbaseES 测试）
- [ ] 默认 `go vet ./...` 通过（vendor 子树不报错）
- [ ] `TEST_KINGBASE_REQUIRED=1 go test -tags=kingbase -v ./...` 跑 9 个测试全过（不允许 t.Skip 误报）
- [ ] PowerShell 实测：`go test -tags=kingbase ./...` 与 `go test -tags="oracle dm kingbase" ./...` 多方言并行跑通
- [ ] README 方言矩阵 + 已知差异速查 + KingbaseES 数据库支持章节（7→9 项含错误诊断 + 生产部署）
- [ ] CHANGELOG v0.8.4 段写完（沿用 v0.8.3 6 大类深度）

### 10.3 commit 序列（4 commit，C2 合并 vendor + deps + setup + contract）

> v0.8.3 DM 是 5 commit，但 KingbaseES 因 Commit 1（vendor 单独）后 `go vet` 可能踩 third_party 子树（B4），故合并 vendor + go.mod replace + setup + builder.go + contract 一起。

1. **`vendor + deps + builder + setup + contract`**：解压 Gokb 到 `third_party/kingbase-gokb/` + .gitignore + go.mod replace + builder.go 加 case + missing_coverage_test.go + setup helper + contract 测试（一次性 commit 保证从 commit 到 commit 都 build 通过）
2. **`integration`**：5 integration 测试（含 IsNull / Empty 区分）
3. **`alias`**：3 alias 测试
4. **`docs`**：README + CHANGELOG v0.8.4 段

- [ ] 推 v0.8.4 tag 到 GitHub

## 11. plan 阶段待定项汇总（writing-plans 必须解决）

| # | 待定项 | plan 阶段动作 | 影响 spec 哪里 |
|---|---|---|---|
### 11.1 用户人工前置（🔴 不在工程师自驱范围 / D1）

| # | 待定项 | 用户操作 | 影响 |
|---|---|---|---|
| U1 | Gokb 下载（验证码弹窗） | Edge / Chrome 在 https://www.kingbase.com.cn/download.html → 接口驱动 → GOLANG → 验证码 → 下载 zip → 解压到 `third_party/kingbase-gokb/` | §3.1 vendor 内容、§10.1 验收 |
| U2 | docker tar 下载（同上） | 数据库 → V9R1C10 → X64_Linux Docker tar → docker load → `docker images` 取 image tag | §3.5 替换 `<image_tag>` |
| U3 | license 申请（联系金仓销售或填表） | 官网"授权文件" → 提交申请 → 邮件回复 license.dat → 挂载到容器 | §9.1 第 5 项 + §10.1 验收 |
| U4 | **A1 license 文件审查** | 解压 Gokb 后查 `LICENSE` / `LICENSE.txt`：若禁止 redistribute → 整个方案废，切 README 引导路径 | §2.1 vendor 策略行 |

**U1/U2/U3/U4 是 plan 启动前的硬前置**，全部完成后工程师才能动手。任一拒发 → abort v0.8.4。

### 11.2 工程师自驱（🟡 plan Task 0 实测）

| # | 待定项 | 工程师动作 | 依赖 |
|---|---|---|---|
| T1 | docker engine + WSL2 + 网络可达性 baseline | `wsl -d Ubuntu-24.04 -e docker run hello-world` + 验证既有 4 容器可用 | 无 |
| T2 | Gokb 是否真无 cgo + transitive 依赖清单 | `go list -deps -test ./third_party/kingbase-gokb/...` | U1 |
| T3 | Gokb 注册名清单（A2） | `grep -r "sql.Register" third_party/kingbase-gokb/` | U1 |
| T4 | vendor 含 go.mod 验证（B1） | `ls third_party/kingbase-gokb/go.mod`；缺则手工补 | U1 |
| T5 | vendor 子树 vet 验证（B4） | `go vet ./third_party/kingbase-gokb/...` | U1, T4 |
| T6 | docker run env 白名单（`DB_MODE=pg` 是否被识别） | `docker run --rm <image> env` + 容器内 README + `docker logs` | U2 |
| T7 | KingbaseES `system` 默认密码 + 首登策略 | 镜像 README + 容器内 ksql 验证 | U2, U3, T6 |
| T8 | `db.Name()` 实测确认（A3 验证） | throwaway main.go：`gorm.Open(postgres.New(Config{Conn: gokbDB})); fmt.Println(db.Name())` | U1, U2, U3, T4, T7 |
| T9 | `MySQLUser` 字段是否撞 KingbaseES PG-compat 保留字 | ksql 跑 `SELECT word FROM sys_get_keywords() WHERE word IN ('name','age','email')`（注意是 `sys_*` 不是 `pg_*`） | U2, U3, T7 |
| T10 | KingbaseES V9R1C10 底层 PG 版本号 | `SELECT version()` + `SHOW server_version` | U2, U3, T7 |
| T11 | `database_mode` 诊断 SQL 定型 | `SELECT current_setting('database_mode')` 或同等（plan 实测三选一） | U2, U3, T7 |
| T12 | tar 拉取/license 申请失败 abort 阈值 | plan 写定时间盒（48h 仅适用 docker tar；license 不可控不在时间盒内） | 无 |

### 11.3 依赖图

```text
U4 (license 文件审查) ─┐
U1 (Gokb 下载) ────────┴──→ T2/T3/T4 ──→ T5
                              │
                              └──→ T8 ─→ T9
                                        │
U2 (docker tar) ──┐                     │
U3 (license 申请) ─┴──→ T6 ─→ T7 ───────┴──→ T10/T11
                                       
T1 (baseline) 独立，最先跑
```

**关键路径**：U4（license 文件） → 决定整个 vendor 方案是否成立；U3（license.dat） → 决定 docker 能否启动；T8（db.Name 实测） → 锁定 contract 测试断言。

### 11.4 README 落地强制条件

**plan Task 0 完成前 README 章节不能合并**——所有 placeholder（vX.Y.Z 版本号 / DSN 样例 / image tag / 默认密码 / 诊断 SQL）必须由 plan 阶段实测填实，否则下游照抄不通。

## 12. 后续候选（不在本期）

- **v1.0 候选**：driver 解耦重构（**TD-24**）—— 把所有 driver（DM/Oracle/MySQL/PG/Gokb）推到下游 self-integrate；gplus 主仓库默认只 require `gorm + sqlite`；释放 `third_party/` 体积
- **v1.1 候选**：KingbaseES Oracle-compat 模式（如有用户需求）
- **v1.2 候选**：OceanBase（信创第三大户）/ 神舟通用（信创第四）
- **v1.x 候选**：批量 RETURNING 适配（解 TD-13），保留字列名自动加引号（解 TD-14）

---

**仅测试基建 + 1 行 builder.go case 字符串新增；GORM 版本锁定保持 v1.31.x；v0.8.0 / v0.8.1 / v0.8.2 / v0.8.3 tag 不受影响；vendor 进 git 是临时方案，v1.0 driver 解耦重构时释放。**

