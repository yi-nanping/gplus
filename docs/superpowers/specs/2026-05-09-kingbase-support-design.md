# v0.9 候选 KingbaseES（人大金仓 V9R1C10）PG 兼容模式支持设计

> **版本**：v0.9 候选（草案，brainstorming 6 节确认 + 用户混合路径决策）
> **日期**：2026-05-09
> **作者**：通过 brainstorming skill 协作产出
> **状态**：spec 已定，待 plan 实施
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
| **BLOB/CLOB** | `BYTEA` / `TEXT` | PG-compat 模式映射 BYTEA / TEXT（**plan Task 0 实测确认**） | postgres dialect 默认行为 |
| **默认命名 case** | lowercase | 同 PG | postgres dialect 处理 |
| **NULLS 排序默认** | NULLS LAST | 同 PG | 测试不依赖 NULL 顺序 |
| **TIMESTAMP 时区** | `TIMESTAMPTZ` / `TIMESTAMP` | 同 PG | postgres dialect 映射 |
| **字符集** | `client_encoding=UTF8` | `ENCODING=UTF8` env | 显式 UTF-8 |
| **标识符长度** | 63 字符 | 同 PG（63） | 测试 struct 远低于上限 |
| **license** | 不需要 | **需要 license.dat**（试用 max_connect=10） | 启动容器挂载 |
| **多模启动** | 单模式 | **需 `DB_MODE=pg` 显式开启 PG-compat**（默认可能 Oracle/MySQL/SQLServer） | env 变量必设（plan Task 0 实测） |

**结论**：KingbaseES V9R1C10 PG-compat 模式行为继承 PG 全部特性，**预期 gplus 库代码改动 0 行**（plan Task 0 `db.Name()` 实测后确认；若返回 `"postgres"` 则 0 行，若返回 `"kingbase"` 则 builder.go 加 1 行 case）。

## 2. 决策摘要

### 2.1 范围决策

| 项 | 决策 | 理由 |
|---|---|---|
| 目标版本 | **KingbaseES V9R1C10**（PG-compat 模式） | 官网当前主力推广版本 |
| 兼容模式 | **仅 PG-compat**（`DB_MODE=pg`） | Oracle-compat 与 v0.8.3 DM 重合，YAGNI；OceanBase / 神通推到 v1.0+ |
| Go 驱动 | **`kingbase.com/gokb`**（官网 2025-08-12 最新版，vendor + replace） | 官方维护，协议 PG 兼容；不引 `lib/pq` 防 `"postgres"` 注册名冲突 panic |
| GORM Dialect | **`postgres.New(postgres.Config{Conn: gokbDB})`** 喂 PG dialect | 不写自定义 Dialector，最大化复用 PG dialect 经验 |
| 测试隔离 | **`//go:build kingbase` build tag** | 与 oracle/dm 同模式 |
| Docker | **官方 V9R1C10 docker tar**（`docker load`，`-m pg`，端口 54321） | 不走 Docker Hub 第三方 |
| vendor 策略 | **`third_party/kingbase-gokb/` 进 git**（5MB） | 与 DM/Oracle 默认 require driver 一致性；v1.0 重构后释放 |
| CI | **不做** | 镜像/license 流程复杂，与 dm/oracle 同策略 |
| 发版号 | **plan Task 0 实测后定** | `db.Name() == "postgres"` → v0.8.4 patch；`== "kingbase"` → v0.9.0 minor |
| **v1.0 路径注记** | **本期 vendor 进 git 是临时方案** | v1.0 driver 解耦重构时会把 Gokb（含 DM/Oracle/MySQL/PG）全部推到下游 self-integrate |

### 2.2 架构决策

| 决策点 | 选择 | 理由 |
|---|---|---|
| 库代码改动 | **plan Task 0 实测后定**：若 `db.Name() == "postgres"` → 0 行；否则 `getQuoteChar` 加 case | 不预设；postgres dialect 默认 `"postgres"` 概率最高 |
| 测试代码组织 | 4 个 build tag 文件，与 dm 测试一一镜像 | 维护一致性 |
| 驱动注册名 | Gokb 自动注册 `"kingbase"` 与 `"postgres"`，**测试用 `"kingbase"`** | 避免与未来 lib/pq 共存时 `"postgres"` 注册名冲突 panic |
| 测试 struct | **复用 `MySQLUser`** | 跨方言通用，避免新增 schema |
| 标识符 case | postgres dialect 默认 lowercase | 与 PG 行为对齐 |
| 工作流 | 作者本地 WSL Docker 实测 + 用户 review GitHub commit | 同 v0.8.2/v0.8.3 路径 |

## 3. 架构

### 3.1 文件改动清单

**新建（5 项，1 个 vendor 目录树 + 4 个 build tag 测试文件）：**

| 路径 | 内容 | 进 git？ |
|---|---|---|
| `third_party/kingbase-gokb/` | Gokb 完整解压目录（约 5 MB），含 `kingbase.com/gokb/*.go` 子包；`go.mod replace kingbase.com/gokb => ./third_party/kingbase-gokb` 指向 | ✅ |
| `kingbase_setup_test.go` | `//go:build kingbase`；`setupKingbaseDB` helper、`defaultKingbaseDSN`、`truncateKingbaseTables` | ✅ |
| `kingbase_contract_test.go` | `//go:build kingbase`；契约断言：`db.Name() == ?`（plan Task 0 实测后定）+ `getQuoteChar` 行为（postgres dialect 默认 `"`/`"` 双引号 quoter） | ✅ |
| `kingbase_integration_test.go` | `//go:build kingbase`；5 测试镜像 v0.8.3 DM 整合：BasicCRUD / WhereConditions（**含 IsNull**）/ OrderGroupHaving / JoinQuery / QuoteColumn | ✅ |
| `alias_kingbase_test.go` | `//go:build kingbase`；3 测试镜像 v0.8.3 DM alias：自连接 / alias 字段 q.Eq / correlated EXISTS | ✅ |

> **路径选择理由**：用 `third_party/kingbase-gokb/`（不用 `vendor/`），避免与 Go modules 的 `vendor/` 目录语义冲突（后者由 `go mod vendor` 命令管理）；保持 import path `kingbase.com/gokb` 通过 go.mod `replace` 指令重定向。

**修改（4 项，3 个必需 + 1 个待定）：**

| 文件 | 改动 | 必需性 |
|---|---|---|
| `go.mod` | 加 `require kingbase.com/gokb v0.0.0-local` + `replace kingbase.com/gokb => ./third_party/kingbase-gokb` | 必需 |
| `go.sum` | 加 transitive deps（**plan Task 0 实测**：Gokb 自身可能依赖 `golang.org/x/crypto` 等纯 Go 包，无 cgo） | 必需 |
| `.gitignore` | 加 `!/third_party/**`（allowlist 模式下，否则 vendor 树非 .go 文件被忽略） | 必需 |
| `missing_coverage_test.go` | 若 `db.Name() == "kingbase"` → `TestGetQuoteChar_Dialects` 加 kingbase 子测试；若 `== "postgres"` → 不动 | **待定**（plan Task 0 实测） |
| `builder.go` | 若 `db.Name() == "kingbase"` → `getQuoteChar` 加 `case "postgres", "sqlite", "dm", "kingbase":`；若 `== "postgres"` → **不动** | **待定**（plan Task 0 实测） |

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
    -e SYSTEM_USER=system -e SYSTEM_PWD=<plan 实测后定> \
    -v kingbase-data:/opt/Kingbase/ES/V8 \
    -v /path/to/license.dat:/home/kingbase/license.dat \
    kingbasees:v9r1c10  # plan Task 0 实测后定 image tag

  export TEST_KINGBASE_DSN="host=127.0.0.1 port=54321 user=system password=<实测> dbname=test sslmode=disable"
  go test -tags=kingbase -v ./...
  → 默认测试 + KingbaseES 测试都跑
  → 无 DSN 时 t.Skip（默认）/ TEST_KINGBASE_REQUIRED=1 时 t.Fatal（CI 与作者本地实测）

并行多方言：
  go test -tags="oracle dm kingbase" ./...
  → 验证多方言 case 合并不冲突（仅当 builder.go 加了 kingbase case 时有意义）
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
// 标识符长度自检：MySQLUser → my_sql_users (12 chars)；id/username/age/email
// 字段全部 ≤8 chars——KingbaseES PG-compat 模式标识符上限 63 字符（沿用 PG），
// 远超测试需求。
//
// 保留字回避：MySQLUser 字段 name/age/email 不与 KingbaseES PG-compat 模式保留字
// 冲突。新增测试字段需主动避开 user / order / type / group / role / size / level
// 等 PG 共用保留字（postgres dialect 自动加双引号 quoter，但仍以避开为佳）。
//
// 不前置 AutoMigrate：直接走 truncateKingbaseTables 的 DROP+AutoMigrate 路径。
// 沿用 v0.8.3 DM 修订决策——避免重复 ALTER ADD column 报错，必须先 DROP 再 CREATE
// 才能保证从干净状态开始。
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

    // 关键：sql.Open("kingbase", dsn) 用 Gokb 的 "kingbase" 注册名
    // 避免与未来下游引入 lib/pq 时的 "postgres" 名冲突
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
wsl -d Ubuntu-24.04 -e docker run -d --name kingbase \
  -p 54321:54321 \
  -e DB_MODE=pg \
  -e SYSTEM_USER=system \
  -e SYSTEM_PWD=<plan 实测后定> \
  -e ENCODING=UTF8 \
  -v kingbase-data:/opt/Kingbase/ES/V8 \
  -v /path/to/license.dat:/home/kingbase/license.dat \
  --restart=no \
  <image_tag>

# 等待启动（约 30s-1min）
wsl -d Ubuntu-24.04 -e docker logs -f kingbase
```

**关键 env 变量**（plan Task 0 对照镜像 README 验证后定型）：

| 变量 | 含义 | 可识别性 |
|---|---|---|
| `DB_MODE=pg` | **PG-compat 模式**（不开则可能默认 Oracle/MySQL/SQLServer 模式） | **必须实测**——huzhihui 第三方镜像用 `EXTEND_INIT_PARAM=-m pg`，官方镜像可能不同 |
| `SYSTEM_USER` | 超级账户名（default `system`） | 多数镜像支持 |
| `SYSTEM_PWD` | 超级账户密码（一般强制 12+ 位 + 含数字字母符号） | **首登强制改密**风险，plan 阶段实测 |
| `ENCODING=UTF8` | 字符集 | PG 一致 |
| `LICENSE_PATH` | license 文件路径覆盖 | 镜像 README 验证 |

**端口决策**：54321（KingbaseES 默认；与本机 PG 16 容器的 5432 不冲突 ✓）

**命名卷决策**：`kingbase-data`（沿用项目本机 4 容器命名卷约定，参见 `docs/dev-setup/local/wsl-environment.md`）

**WSL2 mirrored 网络**：`54321:54321` 在 Windows `localhost:54321` 直接可达，DSN 无需改动。

### 3.6 测试覆盖明细

| 测试函数 | 镜像源 | 覆盖项 | 与 DM 差异 |
|---|---|---|---|
| `TestKingbaseDialectorContract` | TestDMDialectorContract | 2 子测试：`DialectorName` 实测断言 + `getQuoteChar` 返回 `"`/`"` 双引号（postgres dialect 默认） | DM 是空 quoter，KB 是双引号 |
| `TestKingbase_BasicCRUD` | TestDM_BasicCRUD | Save / GetById / List / Count / UpdateById / DeleteById | 同 |
| `TestKingbase_WhereConditions` | TestDM_WhereConditions | Ne / LikeRight / In / NotIn / Between / GetOne / **IsNull**（PG 严格区分 `''`/NULL） | **新增 IsNull**（DM 因 Oracle 兼容剔除） |
| `TestKingbase_OrderGroupHaving` | TestDM_OrderGroupHaving | OrderBy DESC / Limit-Offset / GroupBy+Having RawScan / UpdateByCond / DeleteByCond | 同 |
| `TestKingbase_JoinQuery` | TestDM_JoinQuery | LEFT JOIN 自连接 + ON 条件 | 同 |
| `TestKingbase_QuoteColumn` | TestDM_QuoteColumn | quoteColumn 输出双引号转义 | DM 是原样透传 |
| `TestKingbase_AliasSelfJoin_LeftJoinAs` | TestDM_AliasSelfJoin_LeftJoinAs | alias 自连接 SQL 生成 | 同 |
| `TestKingbase_AliasField_InQEq` | TestDM_AliasField_InQEq | `q.Eq(&alias.Field)` 行为 | 同 |
| `TestKingbase_SubQuery_OuterRef` | TestDM_SubQuery_OuterRef | correlated EXISTS | 同 |

合计 9 个测试（1 contract + 5 integration + 3 alias），与 DM 测试一一对应；其中 WhereConditions **多覆盖 IsNull 一项**（PG 行为支持，相比 DM 的 Oracle-compat 多覆盖一个测试点）。

## 4. builder.go 修订（待定 / plan Task 0 实测后定）

### 4.1 两种可能的实测结果

v0.8.3 实际 `builder.go:233` 状态：

```go
case "postgres", "sqlite", "dm":
    return "\"", "\""  // 双引号 quoter
case "oracle":
    return "", ""      // 空 quoter
```

**结果 A**：`db.Name() == "postgres"`（postgres dialect 默认行为，**概率最高**）
- **库代码 0 行改动** —— 既有 `case "postgres", "sqlite", "dm":` 自然覆盖 KingbaseES
- 版本号定位 → **v0.8.4 patch**

**结果 B**：`db.Name() == "kingbase"` 或其他非 `"postgres"` 字符串
- `getQuoteChar` 把现有 case 改为 `case "postgres", "sqlite", "dm", "kingbase":`
- 库代码 1 行 + 注释泛化
- 版本号定位 → **v0.9.0 minor**

### 4.2 builder.go 修订示例（仅结果 B 适用）

**改动前**（v0.8.3 实际状态，`builder.go:233`）：

```go
case "postgres", "sqlite", "dm":
    // dm 走双引号 quoter（v0.8.3 实测推翻早期假设）...
    return "\"", "\""
```

**改动后**（v0.9.0，仅当结果 B 时）：

```go
case "postgres", "sqlite", "dm", "kingbase":
    // KingbaseES PG-compat 模式与 postgres dialect 一致，走双引号 quoter
    return "\"", "\""
```

### 4.3 单元测试更新（仅结果 B 适用）

`missing_coverage_test.go` 仅在 `TestGetQuoteChar_Dialects` 加一个 kingbase 子测试：

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

**默认 build 影响**：
- `go test ./...`：build tag 隔离，不触达 Gokb，无副作用
- `go build`：因 build tag 不引用 Gokb，但 `go.sum` 锁定哈希
- 跨平台无副作用（无 cgo）
- **vendor 影响**：`third_party/` 不在 Go modules 标准 vendor 目录，不被 `go mod vendor` 接管，replace 指令显式生效

**.gitignore 配套修改**（项目用 allowlist 模式，否则 vendor 树非 .go 文件被忽略）：

```diff
 # 但 docs/dev-setup/local/ 是本机笔记目录，不进 git
 docs/dev-setup/local/
+
+# vendor 第三方驱动（KingbaseES Gokb，allowlist 模式下需显式加白）
+!/third_party/**
```

### 5.2 与 v0.8.3 DM 依赖对比

| 维度 | v0.8.3 DM | v0.9 KingbaseES |
|---|---|---|
| Driver 来源 | GitHub `godoes/gorm-dameng` | 官网 `kingbase.com/gokb`（非 git） |
| 引入方式 | 标准 `require` | `require` + **`replace` 指向本地 vendor** |
| GORM Dialect | 新增 `gorm-dameng v0.7.2` | **复用既有 `gorm.io/driver/postgres v1.6.0`**（无新增） |
| 仓库体积影响 | 0（远程 module） | +5MB（third_party/ 进 git） |
| transitive deps 数量 | 4 个新增 | 待 plan Task 0 实测，预期 0-2 个 |
| go.sum 增量 | ~5 行 | 待 plan Task 0 实测 |

### 5.3 项目 go 版本

`go.mod` 声明 `go 1.24`，build tag 仅用新式 `//go:build kingbase` 语法（Go 1.17+），不写老式 `// +build kingbase`。

## 6. 实施风险（按概率排序）

| 风险 | 概率 | 影响 | 应对 |
|---|---|---|---|
| `db.Name()` 返回值不是 `"postgres"`（postgres dialect 可能因 Conn 来源识别为 KingbaseES） | **中** | 中 | 契约测试第一时间暴露；可能加 `case "kingbase":` to builder.go |
| `DB_MODE=pg` env 变量名不被官方镜像识别 | 中 | 高 | plan Task 0 #4 读镜像 README 实测；不识别走 `-m pg` CLI 参数 |
| KingbaseES `system` PG-compat 模式权限不足建表 | 中 | 中 | DSN 用独立 schema 或 test_user；plan Task 0 #5 验证 |
| Gokb v9.x 协议层与 postgres dialect SQL 兼容存在边界 case | 中 | 中 | 9 个测试覆盖；遇到 t.Skip + 记 TD |
| Gokb 引入 cgo（与官网描述不符） | 低 | 高 | plan Task 0 #2 `go list -deps` 验证；中招则推迟到自定义 Dialector 路径 |
| **`go.mod replace` 本地路径在跨平台下失效**（Windows 反斜杠 vs Unix 正斜杠） | 低 | 高 | 用正斜杠 `./third_party/kingbase-gokb`，Go 工具链跨平台规范化 |
| Gokb migrator AutoMigrate 行为与 postgres dialect 不一致 | 低 | 中 | 用 postgres dialect 的 migrator（Conn 喂 dialect 的设计就是为了避开 Gokb migrator） |
| KingbaseES license.dat max_connect=10 限制并发测试 | 低 | 低 | go test 默认串行；并行测试时调小 SetMaxOpenConns |
| 官网下载流程被反爬 / 验证码升级 / license 拒发 | 低 | 高 | plan 写定时间盒（48h）；超过推迟到 v1.0 |

## 7. 已知限制

KingbaseES PG-compat 模式**继承 PG 全部行为**（与 DM 继承 Oracle 相反）：

- ✅ RETURNING / ON CONFLICT 全支持
- ✅ `''` 与 NULL 严格区分（IsNull 可用）
- ✅ 列名 lowercase 默认
- ✅ `$N` 占位符
- ✅ JSONB / Array / TIMESTAMPTZ 类型

**KingbaseES 独有限制**：

- **license.dat 必需**：试用版 max_connect=10，生产版需购买
- **PG-compat 模式必须显式开启**：默认可能是 Oracle/MySQL/SQLServer 模式（V9R1C10 四模兼容启动时选）
- **Gokb v9.x 协议层未充分验证**：相比 lib/pq/pgx 的成熟度，Gokb 测试覆盖较少
- **官方分发限制**：docker tar / Gokb / license 都需走官网验证码弹窗，无 CI 友好的 docker pull / `go get` 路径
- **PG 版本对应**：KingbaseES V9R1 基于 PG 12 衍生（推测，plan Task 0 实测 `SELECT version()` 确认）—— 高于此 PG 版本的特性（PG 13+ COPY FROM filter、PG 14+ multirange 等）可能不可用

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
| `README.md` 新增 "KingbaseES 数据库支持" 章节 | 详见 §9.1 必含 7 项 |
| `CHANGELOG.md` v0.X.Y 段 | 详见 §9.2 必含 7 子节（沿用 v0.8.3 6 大类深度） |
| `CLAUDE.md` | 不动（架构未变） |
| `docs/dev-setup/local/wsl-environment.md` | 加 KingbaseES 容器条目（5 容器：dm8/mysql8/pg16/oracle-free/kingbase）—— **本机笔记，不进 git** |

### 9.1 README "KingbaseES 数据库支持" 章节必含 7 项

1. **Quickstart 5 步**：
   - ① `go get github.com/yi-nanping/gplus@vX.Y.Z`（版本号 plan Task 0 后定）
   - ② 起 docker（KingbaseES 官方 V9R1C10 docker tar load 路径 + license 申请）
   - ③ 设 `TEST_KINGBASE_DSN` 环境变量
   - ④ `go test -tags=kingbase ./...`
   - ⑤ 错误码对照表导航

2. **`TEST_KINGBASE_DSN` 格式 BNF + 样例**：
   ```text
   host=<host> port=<port> user=<user> password=<password> dbname=<dbname> sslmode=disable [search_path=<schema>]
   ```
   2 个真实样例（含 schema 切换、UTF8 字符集）。具体值由 plan 阶段实测后写入。

3. **下游生产侧集成 KingbaseES**：明确两步姿势：
   ```go
   import _ "kingbase.com/gokb"  // 注：下游需自己 vendor + replace
   db, _ := sql.Open("kingbase", dsn)
   gormDB, _ := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
   ```
   gplus 自身不预先注册 dialector，下游需自己引入 driver 包。

4. **官方 Gokb 下载流程**：
   - URL: https://www.kingbase.com.cn/download.html → 接口驱动 → GOLANG → 不限CPU_不限OS
   - 弹窗"下载验证"（验证码必填，手机/邮箱选填，无需登录）
   - 解压到项目 `third_party/kingbase-gokb/`
   - go.mod replace 指向

5. **license 申请流程**：
   - 官网"授权文件"按钮（V9R1C10 数据库 + Docker 镜像配套）
   - 试用版 max_connect=10
   - 生产版联系金仓销售

6. **诊断 SQL**：验证 PG-compat 模式生效：
   ```sql
   SELECT current_setting('database_mode');  -- 期望 'pg' 或类似（plan Task 0 实测）
   SELECT version();  -- 期望含 'KingbaseES V9R1C10' 与底层 PG 版本
   ```

7. **未验证场景兜底声明**：
   - v0.X.Y 仅验证 V9R1C10 PG-compat 模式 + 单实例 + UTF8
   - 未验证：Oracle/MySQL/SQLServer 兼容模式、DSC 集群、读写分离、V8R6 及更老版本、国密 SM3/SM4 加密
   - 下游需自行验证

### 9.2 CHANGELOG vX.Y.Z 段必含子节（沿用 v0.8.3 6 大类深度）

1. **支持版本与兼容性**（用户首先看）：KingbaseES V9R1C10 PG-compat 模式，`DB_MODE=pg` 显式开启，V8R6 不支持
2. **已知限制（KingbaseES）**：license.dat 必需 / 兼容模式必须显式开启 / Gokb 协议层未充分验证 / 官方分发无 docker pull 路径
3. **新增（KingbaseES V9R1C10 支持）**：
   - GORM Dialector 复用 `gorm.io/driver/postgres v1.6.0`（无新依赖）
   - Go 驱动 `kingbase.com/gokb`（官网 2025-08-12 版，vendor 进 git via `third_party/kingbase-gokb/`）
   - 测试隔离 `//go:build kingbase` build tag
   - 9 测试覆盖（含 IsNull 测试，相比 DM 多覆盖一项）
4. **文档**：README 新增 "KingbaseES 数据库支持" 章节 7 项 + spec/plan 链接
5. **库代码改动**：plan Task 0 实测后定（0 行 / `builder.go: case "kingbase":` 1 行）
6. **技术债**：TD-19/20/21/22/23/24 + 复用 TD-12
7. **收尾说明**：仅测试基建 + (0|1) 行库代码；既有 tag 不受影响；下一步候选（v1.0 driver 解耦重构）

## 10. 验收清单

- [ ] `third_party/kingbase-gokb/` 解压 Gokb 完整到位
- [ ] `go.mod` 加 `require kingbase.com/gokb v0.0.0-local` + `replace` 指令
- [ ] `.gitignore` 加 `!/third_party/**`
- [ ] `kingbase_setup_test.go` / `kingbase_contract_test.go` / `kingbase_integration_test.go` / `alias_kingbase_test.go` 完成
- [ ] 默认测试 `go test ./...` 不变（不触及 KingbaseES）
- [ ] `TEST_KINGBASE_REQUIRED=1 go test -tags=kingbase ./...` 跑 9 个测试全过（不允许 t.Skip 误报）
- [ ] PowerShell 实测：`go test -tags=kingbase ./...` 与 `go test -tags="oracle dm kingbase" ./...` 多方言并行跑通
- [ ] **plan Task 0 完成后定**：`builder.go` 是否需改 + `missing_coverage_test.go` 是否加 kingbase 子测试
- [ ] README 方言矩阵 + 已知差异速查 + KingbaseES 数据库支持章节 7 项
- [ ] CHANGELOG vX.Y.Z 段写完（沿用 v0.8.3 6 大类深度）
- [ ] commit 序列（5 commit，沿用 v0.8.3 节奏）：
  1. `vendor`：解压 Gokb 到 `third_party/` + .gitignore 修订
  2. `deps + setup + contract`：go.mod replace + setup helper + contract 测试一起 commit（避免 contract 单 commit build 失败）
  3. `integration`：5 integration 测试
  4. `alias`：3 alias 测试
  5. `docs`：README + CHANGELOG vX.Y.Z 段
- [ ] 推 vX.Y.Z tag 到 GitHub

## 11. plan 阶段待定项汇总（writing-plans 必须解决）

| # | 待定项 | plan 阶段动作 | 影响 spec 哪里 |
|---|---|---|---|
| 1 | Gokb 当前下载真实 URL + 文件名 + 解压后版本号 | 用户在 Edge 走验证码弹窗下载 → 解压 → 记录版本 | §3.1 vendor 路径 + §10 验收清单首项 |
| 2 | Gokb 是否真无 cgo + transitive 依赖清单 | `go list -deps -test ./third_party/kingbase-gokb/...` | §5.1 transitive 列表 + §6 风险表"Gokb 引入 cgo"行 |
| 3 | KingbaseES V9R1C10 docker tar URL + load 后 image tag | 用户走验证码下载 → `docker load` → `docker images` | §3.5 替换 `<image_tag>` 占位 |
| 4 | docker run env 白名单（`DB_MODE=pg` 是否被识别） | `docker run --rm <image> env` + 容器内 README + `docker logs` | §3.5 env 列表定型；不识别走 CLI `-m pg` |
| 5 | KingbaseES `system` 默认密码 + 是否首登强制改密 | 镜像 README + 容器内 ksql 验证 | §9.1 第 2 项 DSN 样例 + setup 默认 DSN |
| 6 | `db.Name()` 实际返回字符串 | throwaway main.go：`gorm.Open(postgres.New(Config{Conn: gokbDB})) + fmt.Println(db.Name())` | §4 builder.go 是否需改 + §6 风险表第 1 行 |
| 7 | `db.Name()` 不是 `"postgres"` 时连锁修改清单 | spec 加 fail 时同步改 4 处的 checklist：builder.go / missing_coverage_test.go / 4 个 kingbase 测试文件 | §6 风险表第 1 行扩展 |
| 8 | `MySQLUser` 字段是否撞 KingbaseES PG-compat 保留字 | ksql 跑 `SELECT word FROM pg_get_keywords() WHERE word IN ('name','age','email')` | §3.3 setup 注释；若撞则改测试 struct |
| 9 | `KingbaseES V9R1C10` 底层 PG 版本号（影响 PG 13+ 特性可用性） | `SELECT version()` | §7 已知限制最后一行 |
| 10 | `DB_MODE=pg` 实际是否生效（诊断 SQL） | `SELECT current_setting('database_mode')` 或同等 | §9.1 第 6 项诊断 SQL 定型 |
| 11 | license 申请实际流程 + 试用版限制（max_connect=10 是否真） | 用户走官网"授权文件"流程，记录限制 | §9.1 第 5 项 license 章节 |
| 12 | docker engine + WSL2 + 网络可达性 baseline | `wsl -d Ubuntu-24.04 -e docker run hello-world` + 验证 dm8/pg16 等 baseline 容器仍可用 | 0 号前置检查 step |
| 13 | tar 拉取/license 申请失败的 abort 阈值 | plan 写定时间盒（建议 48h；超过推迟到 v1.0） | §6 风险表 |
| 14 | 版本号定位（v0.8.4 patch / v0.9.0 minor） | `db.Name()` 实测 + builder.go 改动量决定 | §2.1 决策摘要"发版号"行 |

## 12. 后续候选（不在本期）

- **v1.0 候选**：driver 解耦重构（**TD-24**）—— 把所有 driver（DM/Oracle/MySQL/PG/Gokb）推到下游 self-integrate；gplus 主仓库默认只 require `gorm + sqlite`；释放 `third_party/` 体积
- **v1.1 候选**：KingbaseES Oracle-compat 模式（如有用户需求）
- **v1.2 候选**：OceanBase（信创第三大户）/ 神舟通用（信创第四）
- **v1.x 候选**：批量 RETURNING 适配（解 TD-13），保留字列名自动加引号（解 TD-14）

---

**仅测试基建 + (0|1) 行库代码改动；GORM 版本锁定保持 v1.31.x；v0.8.0 / v0.8.1 / v0.8.2 / v0.8.3 tag 不受影响；vendor 进 git 是临时方案，v1.0 driver 解耦重构时释放。**

