# v0.8.3 达梦数据库（DM 8）支持设计

> **版本**：v0.8.3（草案，2 轮 6 专家审计修订）
> **日期**：2026-05-08
> **作者**：通过 brainstorming skill 协作产出
> **状态**：待用户复核 → 进入 writing-plans
> **审计轮次**：
>   - 1 轮（10 必修 + 12 建议）：DM 数据库领域 / gplus 架构一致性 / Go build/依赖
>   - 2 轮（4 必修 + 13 plan 待定项 + 7 README 缺口）：对抗性新风险 / 实施工程师可执行性 / 下游用户友好
> **前置版本**：v0.8.2（Oracle 12c+ 支持）
> **后续候选**：DM MySQL 兼容模式 / 人大金仓 KingbaseES 支持

---

## 1. 背景与动机

### 1.1 下游需求

v0.8.2 已交付 Oracle 12c+ 支持（commit `138ad8f`），下游金融/政务/电信场景已可用。但**信创国产化场景**仍未覆盖：

- 达梦数据库（DM 8）是国产数据库主流选择（与人大金仓、神舟通用、华为 GaussDB 并称信创四大数据库）
- DM 8 出厂默认 Oracle 兼容模式，行为与 Oracle 12c+ 高度一致
- v0.8.2 实施期间 memory `oracle-quoter.md` 已锁定决策："`getQuoteChar` 在 oracle 分支返回空 quoter，dm 适配时同样适用"
- v0.8.2 CHANGELOG 已写明："下一步候选（v0.8.3）：达梦数据库 dm 支持（兼容 Oracle 模式，框架 80% 复用）"

### 1.2 DM 8 与 Oracle 12c+ 的差异（评估范围）

| 维度 | Oracle 12c+ | DM 8 Oracle 兼容模式 | 影响 |
|---|---|---|---|
| **GORM 官方驱动** | ❌ 无 | ❌ 无 | 同需第三方 Dialector |
| **Docker 镜像** | `oracle/database-free:23c-slim` | dameng 技术社区 tar 包（主路径） | 官方分发为 tar + `docker load`；Docker Hub 上同名 `dameng/dm8` 是第三方上传，版本不保证、不作为主路径 |
| **占位符** | `:1, :2`（命名） | `?`（与 MySQL 一致，由 dameng 驱动统一） | gplus 库代码方言无关，零影响 |
| **AUTO_INCREMENT** | 12c+ `IDENTITY` 列 | DM 8 `IDENTITY` 列（同语法） | 由 Dialector migrator 处理 |
| **LIMIT/OFFSET** | 12c+ `FETCH FIRST N ROWS ONLY` | 同 Oracle 12c+ | 由 Dialector 统一 |
| **空字符串 vs NULL** | `''` 自动转 NULL | 同 Oracle | 测试 seed 数据避免 empty string |
| **ON CONFLICT** | 不支持，用 `MERGE INTO` | 同 Oracle | 测试中 SaveBatch/UpsertBatch 走 RETURNING 路径需 t.Skip |
| **BOOL 类型** | 不存在，用 `NUMBER(1)` | 同 Oracle | Dialector 处理 |
| **默认命名 case** | UPPERCASE | UPPERCASE | **沿用空 quoter 策略** |
| **CLOB/TEXT WHERE** | 不允许直接 `=`/`LIKE`/`IN` | 同 Oracle | string 字段需 `gorm:"size:N"` 显式约束 |
| **NULLS 排序默认** | NULLS LAST | 同 Oracle | 测试断言不依赖 NULL 顺序 |
| **RETURNING 批量** | 12c `RETURNING INTO` 仅单行 | 同 Oracle | SaveBatch/UpsertBatch 沿用 t.Skip |
| **TIMESTAMP 时区** | TIMESTAMP WITH (LOCAL) TIME ZONE | 同 Oracle | Dialector 映射决定 |
| **字符集** | `NLS_CHARACTERSET` | `UNICODE_FLAG` 控制 UTF-8/GB18030 | 启动 docker 显式 `UNICODE_FLAG=1 LENGTH_IN_CHAR=1` |
| **标识符长度** | 12c R1 30 / 12c R2+ 128 | DM 8 128（v8.1+） | 测试 struct 保留 ≤30 字符兼容 |

**结论**：DM 8 Oracle 兼容模式行为继承 Oracle 全部特性，**预期 gplus 库代码改动 ≈ 0 行（仅 builder.go 一行 case 合并）**。

## 2. 决策摘要

### 2.1 范围决策

| 项 | 决策 | 理由 |
|---|---|---|
| DM 版本目标 | **DM 8（Oracle 兼容模式）** | 现役主流，DM 7 已老；godoes/gorm-dameng 也按 DM 8 验证 |
| GORM Dialector | **`github.com/godoes/gorm-dameng`** | 与 v0.8.2 godoes/gorm-oracle 同作者，API 风格一致；社区主流选择 |
| Go 驱动 | **`gitee.com/chunanyong/dm`**（transitive） | DM 官方驱动，由 gorm-dameng 自动引入 |
| 测试隔离 | **`//go:build dm` build tag** | 默认 build/test 不触及，CI 不变；与 oracle tag 同模式 |
| CI 集成 | **不做** | DM 镜像大、license 复杂；build tag 留给下游与作者本地验证入口 |
| 本地验证 | **WSL2 + docker 起 dameng 官方 DM 8 镜像（tar load 主路径）** | 沿用 v0.8.2 Oracle plan 的 WSL wrapper 写法 |
| 兼容模式 | **仅 Oracle 兼容（DM 默认 COMPATIBLE_MODE=2）** | MySQL/PG/TD 兼容模式留给以后；Oracle 兼容与 v0.8.2 经验复用率最大化 |
| 发版 | **v0.8.3 tag** | 沿用 v0.8.0 → v0.8.1 → v0.8.2 节奏 |

### 2.2 架构决策（实施方案 A）

| 决策点 | 选择 | 备选 | 理由 |
|---|---|---|---|
| 库代码改动 | **`builder.go: getQuoteChar` 一行 `case "oracle", "dm":`** | dm 独立分支（重复代码）/ 抽 `isOracleCompat` helper（过早抽象） | YAGNI；只有两个方言共享空 quoter，第三个出现时再考虑抽象 |
| 测试代码组织 | **自包含 build tag 文件，与 oracle 测试一一镜像** | dm/ 子目录 / 独立 test package | 与 Oracle 文件结构对应、对比维护方便 |
| 驱动方言名 | **`db.Name() == "dm"`**（待实测确认） | "dameng" / "DM" | godoes/gorm-dameng 默认行为，契约测试第一时间暴露偏差 |
| DM 命名 case | **依赖 gorm-dameng Dialector 默认行为** | 库代码强转 | 实测后再决定 |
| 工作流 | **每 commit 作者本地 WSL Docker 实测 + 用户 review GitHub commit** | CI 不参与 | 与 v0.8.2 Oracle 路径一致 |

## 3. 架构

### 3.1 文件改动清单

**新建（4 个，全 build tag 隔离）：**

| 文件 | build tag | 内容 |
|---|---|---|
| `dm_setup_test.go` | `//go:build dm` | `setupDMDB` helper、`defaultDMDSN`、`truncateDMTables`（DROP TABLE + AutoMigrate） |
| `dm_contract_test.go` | `//go:build dm` | Dialector 契约断言：`db.Name() == "dm"`、`getQuoteChar` 返回空 quoter |
| `dm_integration_test.go` | `//go:build dm` | 5 个测试：BasicCRUD / WhereConditions / OrderGroupHaving / JoinQuery / QuoteColumn |
| `alias_dm_test.go` | `//go:build dm` | 3 个测试：自连接 / alias 字段 q.Eq / correlated EXISTS |

**修改（4 个，必需）：**

| 文件 | 改动 | build tag |
|---|---|---|
| `go.mod` | `require github.com/godoes/gorm-dameng vX.Y.Z`（最新稳定版） | 默认 |
| `go.sum` | 加 transitive deps（gitee.com/chunanyong/dm 等） | 默认 |
| `builder.go` | `getQuoteChar` 把 `case "oracle":` 改成 `case "oracle", "dm":` + 注释泛化（**唯一库代码（非测试）改动**） | 默认 |
| `missing_coverage_test.go` | `TestQuoteColumn_Dialects` / `TestGetQuoteChar_Dialects` 加 dm case 覆盖 | 默认 |

**不动**：

- `testdb_test.go`（不在默认 import 中带 DM driver）
- 其他库代码（query.go / update.go / repository.go / alias.go / subquery.go / schema.go / debug.go）
- CI 配置（`.github/workflows/ci.yml` 保持 sqlite + mysql + pg）
- 现有所有测试（包括 v0.8.2 Oracle 测试）

### 3.2 测试运行流程

```text
默认（无 build tag）：
  go test ./...
  → 跑 sqlite/mysql/pg 路径（CI 也走这条）
  → DM 测试文件因 //go:build dm 不参与编译
  → 行为不变

DM 验证（手动）：
  # 启动 DM 8（WSL 写法，本机仅 WSL2 有 docker）
  wsl -d Ubuntu-24.04 -e docker run -d --name dm8 -p 5236:5236 \
    -e UNICODE_FLAG=1 -e LENGTH_IN_CHAR=1 -e PAGE_SIZE=16 \
    -e EXTENT_SIZE=32 -e LOG_SIZE=2048 -e INSTANCE_NAME=DM8TEST \
    dameng/dm8

  # 注意：DSN 中的密码以你拉到的镜像 README 为准（常见 SYSDBA / SYSDBA001，
  # 部分版本首登强制改密）。下方密码仅为占位，plan 阶段实测后写入 README。
  export TEST_DM_DSN="dm://SYSDBA:&lt;实测密码&gt;@127.0.0.1:5236"
  go test -tags=dm -v ./...
  → 默认测试 + DM 测试都跑
  → DM 测试本地拿 DSN 连，无 DSN 时 t.Skip

并行 Oracle + DM：
  go test -tags="oracle dm" ./...    # bash / WSL / PowerShell 5.1/7 都可
  → 验证空 quoter case 合并不冲突
  注：PowerShell 5.1/7 中单/双引号皆可正常透传 -tags 字符串到 go test，
  无需特殊处理（前一轮 spec 写"双引号会被吞"是事实错误，已修订）。
```

**`t.Skip` 误报防护**：当 `TEST_DM_DSN` 不通时，`setupDMDB` 默认 `t.Skipf` 跳过——`go test -tags=dm` 退出码仍为 0，无法区分"全过"和"全 skip"。作者本地与 future CI 验证时设置 `TEST_DM_REQUIRED=1` 让 setup 改走 `t.Fatalf`，避免误报（与 Oracle 后续也应同步加该 helper）。

### 3.3 setupDMDB 与 truncateDMTables

`dm_setup_test.go` 提供（与 oracle_setup_test.go 一一对应）：

```go
//go:build dm

// 警告：仅限本地 Docker 开发。SYSDBA 是 DM 默认 DBA 超级账户，绝不能用于生产。
// 默认密码版本差异较大：dameng 镜像历史上 `SYSDBA` / `SYSDBA001` 都见过，且
// 部分版本首登强制改密——以你拉到的镜像 README 为准。CI/生产请用 TEST_DM_DSN
// 提供独立测试账户，且仅授予最小测试权限。
//
// 防自相矛盾策略：defaultDMDSN 故意留空字符串，强制下游必须显式设置
// TEST_DM_DSN——避免 spec 写死的密码与镜像实际版本不一致导致 connect fail。
// plan 阶段先 docker exec 进入容器跑 disql 验证默认密码，把验证后的 DSN
// 写入 plan 文档（不是 spec 常量），下游用户从 README 抄完整 DSN。
const defaultDMDSN = ""  // 强制 TEST_DM_DSN 显式设置，避免密码版本差异隐性 fail

// setupDMDB 与 setupOracleDB 同模式：非泛型，绑定 MySQLUser 复用既有测试 struct。
//
// 标识符长度自检：MySQLUser → my_sql_users (12 chars)；id/username/age/email
// 字段全部 ≤8 chars——沿用 Oracle 12c R1 的 30 字符上限规范（DM 8 实际 128，
// 但保留与既有 Oracle 测试 struct 一致便于跨方言通用）。
//
// 保留字回避：MySQLUser 字段 name/age/email 不与 DM 8 Oracle 兼容模式保留字
// 冲突。新增测试字段需主动避开 comment / type / group / role / order / size /
// level / number / date 等 DM/Oracle 共用保留字（空 quoter 策略下不会自动加引号）。
//
// 不前置 AutoMigrate：直接走 truncateDMTables 的 DROP+AutoMigrate 路径建表。
// 沿用 Oracle commit `7627ea6` 的修订决策——godoes/gorm-dameng migrator 也假定
// 走 Oracle 兼容路径，已存在表 ALTER ADD 极可能报 ORA-01430 column already exists
// 等价错误，必须先 DROP 再 CREATE 才能保证从干净状态开始。
func setupDMDB(t *testing.T) (*Repository[int64, MySQLUser], *gorm.DB) {
    t.Helper()
    dsn := os.Getenv("TEST_DM_DSN")
    if dsn == "" { dsn = defaultDMDSN }
    if dsn == "" {
        if os.Getenv("TEST_DM_REQUIRED") == "1" {
            t.Fatalf("TEST_DM_DSN 未设置但 TEST_DM_REQUIRED=1，DM 实测被强制要求")
        }
        t.Skip("TEST_DM_DSN 未设置，跳过 DM 测试（参见 README 章节）")
    }
    db, err := gorm.Open(dameng.Open(dsn), &gorm.Config{
        Logger: logger.Default.LogMode(logger.Info),
    })
    if err != nil {
        if os.Getenv("TEST_DM_REQUIRED") == "1" {
            t.Fatalf("DM 强制要求但不可用: %v", err)  // 防 Skip 误报
        }
        t.Skipf("DM 不可用，跳过: %v", err)
    }
    applyDBPoolLimits(t, db)  // 复用既有 helper
    repo := NewRepository[int64, MySQLUser](db)
    truncateDMTables(t, db, &MySQLUser{})  // 直接 DROP+AutoMigrate，不前置
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
    // 与 truncateOracleTables 实现一致：DROP TABLE "X" PURGE + AutoMigrate
}
```

### 3.4 Docker 启动命令（WSL2 + Docker Engine）

| 环境 | 命令前缀 | 说明 |
|---|---|---|
| Docker Desktop | 无 | 用户本机未装，跳过 |
| WSL2 + Docker Engine | `wsl -d Ubuntu-24.04 -e docker ...` | 默认环境 |

**镜像获取（主路径）**：dameng 官方分发为 tar 包，需从 [dameng 技术社区](https://eco.dameng.com/) 或 dameng 官网下载（具体 URL 在 plan 阶段实操确认）：

```bash
# 主路径：tar 包 + docker load（dameng 官方分发）
wsl -d Ubuntu-24.04 -e docker load -i /path/to/dm8.tar

# Fallback：Docker Hub 第三方镜像（版本不保证、不作为主路径）
# wsl -d Ubuntu-24.04 -e docker pull dameng/dm8
```

**启动命令**（环境变量列表 *以镜像 README 为准*——下方仅为常见可识别变量样例，DM 镜像不同版本可能识别不同 env，plan 阶段需先 `docker run --rm <image> env` 或读 README 确认）：

```bash
# 启动 DM 8 实例（单行，避免 PowerShell 续行符不透传）
wsl -d Ubuntu-24.04 -e docker run -d --name dm8 -p 5236:5236 \
  -e INSTANCE_NAME=DM8TEST \
  -e PAGE_SIZE=16 \
  -e UNICODE_FLAG=1 \
  -e CASE_SENSITIVE=Y \
  -e COMPATIBLE_MODE=2 \
  <dm8_image_tag>

# 等待 ~30s-1min 启动（DM 比 Oracle 起得快）
wsl -d Ubuntu-24.04 -e docker logs -f dm8
```

**关键 env 变量含义**（plan 阶段对照镜像 README 验证后定型）：
- `COMPATIBLE_MODE=2`：**显式开启 Oracle 兼容模式**，不假设镜像默认（DM 镜像随版本 default 不同）
- `PAGE_SIZE=16`：页大小（默认 8 太小不够长 VARCHAR），影响最大记录尺寸
- `UNICODE_FLAG=1`：UTF-8 字符集（默认 GB18030 会让中文测试数据损坏）
- `CASE_SENSITIVE=Y`：保持大小写敏感（Oracle 兼容模式默认行为）
- `INSTANCE_NAME=DM8TEST`：实例名

可能未被识别（plan 阶段验证）：
- `LENGTH_IN_CHAR`：spec 初稿假设的"长度按字符算"，部分 DM 镜像版本不支持此 env
- `EXTENT_SIZE`、`LOG_SIZE`：dminit 阶段参数，部分镜像不通过 env 透传

**WSL2 mirrored 网络**：`5236:5236` 在 Windows `localhost:5236` 直接可达，DSN 无需改动。

### 3.5 测试覆盖明细

| 测试函数 | 镜像源 | 覆盖项 |
|---|---|---|
| `TestDMDialectorContract` | TestOracleDialectorContract | 含 2 个子测试：`DialectorName_是_dm`（核实 `db.Name() == "dm"`）+ `getQuoteChar_返回空_quoter`（核实返回 `"", ""`）。**入口必须保持 `_, db := setupDMDB(t)` 调用**——这样 `TEST_DM_REQUIRED=1` 守卫覆盖契约测试；后续重构若把契约测试改成不调 setup 的 mock dialector 形式（如 `missing_coverage_test.go` 风格），守卫会失效，需在 README 显式说明 |
| `TestDM_BasicCRUD` | TestOracle_BasicCRUD | Save / GetById / List / Count / UpdateById / DeleteById |
| `TestDM_WhereConditions` | TestOracle_WhereConditions | Ne / LikeRight 前缀 / In / NotIn / Between / GetOne（不含 IsNull——沿用 Oracle 实测决策：Oracle/DM `''=NULL` 语义下 IsNull 测试不可靠，已剔除） |
| `TestDM_OrderGroupHaving` | TestOracle_OrderGroupHaving | OrderBy DESC / Limit-Offset / GroupBy+Having RawScan（用 `AS "col"` 锁定 lowercase）/ UpdateByCond / DeleteByCond |
| `TestDM_JoinQuery` | TestOracle_JoinQuery | LEFT JOIN 自连接 + ON 条件 |
| `TestDM_QuoteColumn` | TestOracle_QuoteColumn | quoteColumn 输出原样（空 quoter 行为） |
| `TestDM_AliasSelfJoin_LeftJoinAs` | TestOracle_AliasSelfJoin_LeftJoinAs | alias 自连接 SQL 生成 |
| `TestDM_AliasField_InQEq` | TestOracle_AliasField_InQEq | `q.Eq(&alias.Field)` 行为 |
| `TestDM_SubQuery_OuterRef` | TestOracle_SubQuery_OuterRef | correlated EXISTS |

合计 9 个测试（1 contract + 5 integration + 3 alias），与 Oracle 测试一一对应。

## 4. builder.go 修订

### 4.1 改动前后对比

**改动前**（v0.8.2）：

```go
case "oracle":
    // godoes/gorm-oracle migrator 用 UPPERCASE 不带引号 CREATE TABLE
    // （列名实际存为 USERNAME 等大写），若 quoteColumn 加双引号转义会变 "username"
    // 而 Oracle 双引号下大小写敏感 → ORA-00904 invalid identifier。
    // 这里返回空 quoter，让 Oracle 自身 UPPERCASE 解析裸标识符。
    // 已知陷阱：列名是 Oracle 保留字（order/size/level 等）时需用户手动加引号。
    return "", ""
```

**改动后**（v0.8.3）：

```go
case "oracle", "dm":
    // godoes/gorm-{oracle,dameng} migrator 用 UPPERCASE 不带引号 CREATE TABLE
    // （列名实际存为 USERNAME 等大写），若 quoteColumn 加双引号转义会变 "username"
    // 而 Oracle/DM 双引号下大小写敏感 → ORA-00904 invalid identifier。
    // 这里返回空 quoter，让 Oracle/DM 自身 UPPERCASE 解析裸标识符。
    // 已知陷阱：列名是保留字（order/size/level 等）时需用户手动加引号。
    return "", ""
```

### 4.2 单元测试更新

`missing_coverage_test.go` 仅在 `TestGetQuoteChar_Dialects` 加一个 dm 子测试（沿用 Oracle 同套写法）：

```go
t.Run("dm 方言返回空 quoter 与 oracle 共用", func(t *testing.T) {
    db := &gorm.DB{Config: &gorm.Config{Dialector: testMockDialector{"dm"}}}
    qL, qR := getQuoteChar(db)
    if qL != "" || qR != "" {
        t.Errorf("dm 期望空字符串，实际 (%q,%q)", qL, qR)
    }
})
```

`testMockDialector` 已存在于 `missing_coverage_test.go:1219`（v0.8.2 已用于 oracle 测试），无需新增；用其模拟 dm 方言名，避免默认 build 引入 dameng driver。

`TestQuoteColumn_Dialects` **不需要新增 dm case**——该表驱动测试输入直接是 quoter 字符（`qL`/`qR`），不经过 dialect 分支判断；空 quoter 透传行为已被 oracle 实测通过 v0.8.2 commit `7627ea6` 验证（Oracle 同样未在该表中加 case）。

## 5. 依赖与构建

### 5.1 go.mod 改动

```
require (
    github.com/godoes/gorm-dameng vX.Y.Z  // plan 阶段用 `go list -m -versions` 锁定具体版本
)
```

会引入 transitive：
- `gitee.com/chunanyong/dm`（DM 官方 Go 驱动，**纯 Go 实现，无 cgo**）
- 其它 dameng/dm 关联包

**项目 go 版本**：`go.mod` 声明 `go 1.24`，build tag 仅用新式 `//go:build dm` 语法（Go 1.17+），不写老式 `// +build dm`。

### 5.2 默认 build 影响

- `go test ./...`：DM 测试文件 build tag 隔离，不参与编译，不需要 dameng driver 加载
- `go build`：dameng driver 因 build tag 不被引用，但 `go.sum` 锁定其哈希
- **跨平台无副作用**：dameng/dm 驱动为纯 Go 无 cgo，`CGO_ENABLED=0` 也可编译；下游 `go build` 默认不会触达驱动 init
- 下游 `go mod tidy`：写入 dameng transitive 到 go.sum（**TD-12 重申**：单模块带可选 driver 副作用）

### 5.3 GOPROXY 与 gitee.com 拉取

`gitee.com/chunanyong/dm` 在 `proxy.golang.org`（Go 默认 GOPROXY）历史上多次缓存失败/超时，下游用户需配置 fallback：

```bash
# 国内开发者推荐（goproxy.cn 已镜像 gitee 包）
go env -w GOPROXY=https://goproxy.cn,direct

# 国外 / proxy.golang.org 拉取失败时（让 gitee 走 direct 模式）
go env -w GOPRIVATE=gitee.com/*
go env -w GOPROXY=https://proxy.golang.org,direct
```

README 的"DM 支持"章节需明确给出此提示，避免下游用户首次 `go mod download` 卡死。

## 6. 实施风险（按概率排序）

| 风险 | 概率 | 影响 | 应对 |
|---|---|---|---|
| `db.Name()` 不返回 `"dm"`（可能是 `"dameng"`/`"DM"`） | 中 | 中 | 契约测试第一时间暴露，改 builder.go case 字符串 |
| **DM migrator 重复 ALTER ADD column 报错（ORA-01430 等价）** | 中 | 中 | 沿用 Oracle commit `7627ea6` 修订路径：setup 不前置 AutoMigrate，直接走 truncate 的 DROP+CREATE 路径 |
| dameng 镜像 SYSDBA 默认密码版本差异 | 中 | 中 | plan 阶段 docker exec 进入容器跑 disql 验证，再写死 defaultDMDSN |
| dameng 镜像 docker run env 变量不被识别 | 中 | 中 | plan 阶段读镜像 README 后定型；不识别的 env 改走 `dminit` CLI 或 dm.ini |
| Docker Hub `dameng/dm8` 第三方镜像版本不准 | 高 | 高 | 主路径用 dameng 技术社区 tar + `docker load`；Docker Hub 仅 fallback |
| **`t.Skip` 误报**（DSN 不通时 exit 0，验收清单"全过"无法区分"全 skip"） | 中 | 中 | `TEST_DM_REQUIRED=1` 时 setup 改 `t.Fatalf`；作者本地实测必带此 flag |
| DM Oracle 兼容模式默认未开启 | 低 | 高 | 启动 docker 时加 `COMPATIBLE_MODE=2` 显式指定 |
| `gitee.com/chunanyong/dm` 在 GOPROXY 拉取失败 | 中 | 中 | README 给出 `GOPROXY=https://goproxy.cn,direct` 与 `GOPRIVATE=gitee.com/*` fallback 提示 |
| DM CLOB/字符集映射差异 | 低 | 中 | 测试 fail 时通过 `gorm:"size:N"` 显式约束修复 |
| DM 占位符不兼容（不是 `?`） | 低 | 中 | godoes/gorm-dameng Dialector 应已统一处理 |

## 7. 已知限制（沿用 Oracle 全套）

DM 8 Oracle 兼容模式继承 Oracle 全部限制：

- **空 quoter 策略**：列名是保留字（order/size/level 等）时需用户手动加引号
- **`''` = NULL**：DM Oracle 兼容模式继承 Oracle 行为，影响 IsNull / Empty 判断
- **输出列名 UPPERCASE**：RawScan 映射小写 struct tag 时需 SQL 显式 `AS "col"` 锁定 lowercase
- **CLOB/TEXT WHERE 限制**：Go `string` 长字段映射成 CLOB 时 `LikeRight`/`In` 报错；所有 string 字段须显式 `gorm:"size:N"` 约束
- **NULLS LAST 默认**：升序排序 NULL 排末尾
- **RETURNING 仅支持单行**：`SaveBatch`/`UpsertBatch` 走 RETURNING 路径在 DM 失败，测试 `t.Skip`
- **标识符长度上限**：DM 8 128 字符，但保留 ≤30 字符规范（与 Oracle 12c R1 兼容）
- **ON CONFLICT 不支持**：DM 用 `MERGE INTO`，gplus `OnConflict` 在 DM 下需用户手动改写

## 8. 技术债

| ID | 描述 |
|---|---|
| **TD-15** | DM 测试无 CI 守护（同 TD-9，依赖下游手动跑发现问题） |
| **TD-16** | godoes/gorm-dameng 维护风险（同 TD-10，社区维护，GORM 升级时可能滞后） |
| **TD-17** | DM 7 不支持（同 TD-11 的 Oracle 11g） |
| **TD-18** | DM MySQL/PG 兼容模式不支持（v0.8.3 仅验证 Oracle 兼容） |

复用既有 TD：
- **TD-12**（单模块带可选 driver）：gorm-dameng 拉到 transitive
- **TD-13**（批量 RETURNING 适配）：DM 也不解决，推到 v0.9+
- **TD-14**（保留字列名自动加引号）：在 DM 下行为完全一致——DM 8 Oracle 兼容模式下 `order/size/level/number/date/comment/type/group/role` 等保留字列名同样不会自动加引号，需用户手动 RawSQL 加引号或修改 struct 字段名避开

## 9. 文档变更

| 文件 | 改动 |
|---|---|
| `README.md` 方言矩阵 | 加 DM 行：`dm \| ✅ \| build tag: -tags=dm \| 同 Oracle 限制` |
| `README.md` 已知方言差异速查 | DM 章节直接引用 Oracle 章节 + 一句 "DM 8 Oracle 兼容模式继承全部 Oracle 限制" |
| `README.md` 新增 "DM 数据库支持" 章节 | 下方 §9.1 7 项内容缺口必含 |
| `CHANGELOG.md` v0.8.3 段 | 沿用 v0.8.2 模板深度，详见 §9.2 必含子节 |
| `CLAUDE.md` | 不动（架构未变） |

### 9.1 README "DM 数据库支持" 章节必含 7 项

下游用户视角缺口（一轮 spec 仅覆盖测试侧 + GOPROXY，生产侧零字）。本章节必含：

1. **Quickstart 5 步**：① `go get github.com/yi-nanping/gplus@v0.8.3` ② 起 docker（dameng 技术社区 tar 路径）③ 设 `TEST_DM_DSN` 环境变量 ④ `go test -tags=dm ./...` ⑤ 错误码对照表导航
2. **`TEST_DM_DSN` 格式 BNF + 样例**：
   ```text
   dm://&lt;user&gt;:&lt;password&gt;@&lt;host&gt;:&lt;port&gt;[/&lt;schema&gt;][?&lt;params&gt;]
   ```
   至少 2 个真实样例（含 schema 切换、字符集参数）。具体值由 plan 阶段实测后写入。
3. **下游生产侧集成 DM**：明确 `import _ "github.com/godoes/gorm-dameng"`（或显式 `gorm.Open(dameng.Open(dsn))`）的姿势；gplus 自身不预先注册 dialector，下游需自己引入 driver 包。
4. **保留字 → 措施对照表**：`order/size/level/comment/type/group/role/number/date` 等列名命中保留字时，优先级：① 改 struct tag `column:` 避开 ② 用 `RawSQL` 加双引号 ③ 等 v1.0 自动加引号能力（TD-14）
5. **错误码 → README 锚点导航**：`ORA-00904` 列名解析（空 quoter 策略）/ `ORA-00932` CLOB（加 `gorm:"size:N"`）/ `ORA-01430` migrator（不前置 AutoMigrate）/ 其它沿用 Oracle 章节
6. **诊断 SQL**：验证 `COMPATIBLE_MODE=2` 生效：
   ```sql
   SELECT PARA_VALUE FROM V$DM_INI WHERE PARA_NAME='COMPATIBLE_MODE';
   ```
7. **未验证场景兜底声明**：v0.8.3 仅验证 DM 8 Oracle 兼容模式 + 单实例 + UTF-8。未验证：国密 SM3/SM4 加密列、Kerberos 认证、DSC 集群、读写分离、DM 7 及更老版本——下游需自行验证。

### 9.2 `CHANGELOG.md` v0.8.3 段必含子节

沿用 v0.8.2 6 大类模板深度，但**子节顺序按下游用户阅读优先级**重排：

1. **支持版本与兼容性**（用户首先看）：DM 8 Oracle 兼容模式，`COMPATIBLE_MODE=2` 显式开启，DM 7 不支持
2. **已知限制（DM）**：沿用 Oracle 8 条 + DM 特有（COMPATIBLE_MODE 须显式 / 镜像默认密码版本差异 / Docker Hub 第三方镜像）
3. **新增（DM 8 支持）**：GORM Dialector / Go 驱动 / 测试隔离 build tag / 跑测命令
4. **文档**：README 新增 "DM 数据库支持" 章节 7 项 + GOPROXY 提示 + spec/plan 链接
5. **库代码改动**：`builder.go: getQuoteChar` 把 `case "oracle":` 合并为 `case "oracle", "dm":` + 注释泛化（**唯一库代码（非测试）改动**）
6. **技术债**：TD-15（CI 守护）/ TD-16（Dialector 维护）/ TD-17（DM 7 不支持）/ TD-18（仅 Oracle 兼容模式）+ 复用 TD-12/13/14
7. **收尾说明**：仅测试基建 + 1 行库代码 case 合并；GORM 版本锁定保持；既有 tag 不受影响；下一步候选（v0.8.4 DM MySQL 兼容 / v0.9 KingbaseES）

**`CHANGELOG.md` v0.8.3 段必含子节**（沿用 v0.8.2 6 大类模板深度）：

1. **新增（DM 8 Oracle 兼容模式支持）**：GORM Dialector / Go 驱动 / 测试隔离 build tag / 跑测命令
2. **库代码改动**：`builder.go: getQuoteChar` 把 `case "oracle":` 合并为 `case "oracle", "dm":` + 注释泛化（**唯一库代码（非测试）改动**）
3. **已知限制（DM）**：沿用 Oracle 8 条（空 quoter / `''=NULL` / UPPERCASE / CLOB / NULLS LAST / RETURNING 单行 / 标识符长度 / ON CONFLICT）+ DM 特有（`COMPATIBLE_MODE` 须显式开启 / 镜像默认密码版本差异 / Docker Hub 第三方镜像）
4. **技术债**：TD-15（CI 守护）/ TD-16（Dialector 维护）/ TD-17（DM 7 不支持）/ TD-18（仅 Oracle 兼容模式）+ 复用 TD-12/13/14
5. **文档**：README 矩阵 + GOPROXY 提示 + spec/plan 链接
6. **收尾说明**：仅测试基建 + 1 行库代码 case 合并；GORM 版本锁定保持；既有 tag 不受影响；下一步候选（v0.8.4 DM MySQL 兼容 / v0.9 KingbaseES）

## 10. 验收清单

- [ ] `builder.go: getQuoteChar` 改一行 `case "oracle", "dm":` + 注释更新
- [ ] `go.mod` 加 `godoes/gorm-dameng` 依赖（plan 阶段用 `go list -m -versions` 锁定最新稳定版具体版本号）
- [ ] 默认测试 `go test ./...` 不变（不触及 DM）
- [ ] `TEST_DM_REQUIRED=1 go test -tags=dm ./...` 跑 DM 8 Oracle 兼容模式 9 个测试全过（不允许 t.Skip 误报）
- [ ] PowerShell 实测：`go test -tags=dm ./...` 与 `go test -tags="oracle dm" ./...` 双方言并行跑通（PS 5.1/7 单双引号均可，无须特殊处理）
- [ ] `dm_setup_test.go` / `dm_contract_test.go` / `dm_integration_test.go` / `alias_dm_test.go` 完成
- [ ] `missing_coverage_test.go` 仅在 `TestGetQuoteChar_Dialects` 加 dm 子测试（不动 `TestQuoteColumn_Dialects`）
- [ ] README 方言矩阵 + 已知差异速查 + GOPROXY 提示加 DM
- [ ] CHANGELOG v0.8.3 段写完（沿用 v0.8.2 6 大类深度）
- [ ] commit 序列（5 commit，沿用 v0.8.2 节奏并修订避免 build 中断）：
  1. `deps`：加 godoes/gorm-dameng 依赖 + go.sum
  2. `builder + setup + contract`：builder.go case 合并 + missing_coverage_test.go dm 子测试 + dm_setup_test.go + dm_contract_test.go（一起 commit 避免 contract 单 commit build 失败）
  3. `integration`：dm_integration_test.go 5 个测试
  4. `alias`：alias_dm_test.go 3 个测试
  5. `docs`：README + CHANGELOG v0.8.3 段
- [ ] 推 v0.8.3 tag 到 GitHub

## 11. plan 阶段待定项汇总（writing-plans 必须解决）

spec 多处写"plan 阶段定型 / 实操确认"——本节集中索引，避免实施时遗漏。**writing-plans 必须把每项变成具体的 step**。

| # | 待定项 | plan 阶段动作 | 影响 spec 哪里 |
|---|---|---|---|
| 1 | gorm-dameng 当前实际版本号 | `go list -m -versions github.com/godoes/gorm-dameng` + 看 GitHub release 频率 | §5.1 替换 vX.Y.Z |
| 2 | DM 8 tar 镜像 URL + 文件名 + load 后 image tag | 登录 [eco.dameng.com](https://eco.dameng.com/) 找分发页，记录确切 URL + 跑 `docker images` 记 tag | §3.4 替换 `<dm8_image_tag>` 占位 |
| 3 | SYSDBA 默认密码（容器实际值） | `docker exec -it dm8 disql SYSDBA/<尝试>@localhost:5236` 试 SYSDBA / SYSDBA001 / 强制改密 | README §9.1 第 2 项 DSN 样例 |
| 4 | docker run env 白名单 | `docker run --rm <image> env` + 容器内 README + `docker logs` | §3.4 env 列表定型 |
| 5 | `db.Name()` 实际返回字符串 | throwaway main.go：`gorm.Open(dameng.Open(dsn)) + fmt.Println(db.Name())` 探测 | §4.1 builder.go case 字符串、§3.5 contract 测试断言 |
| 6 | `MySQLUser` 字段是否撞 DM 保留字 | disql 跑 `SELECT KEYWORD FROM V$RESERVED_WORDS WHERE KEYWORD IN ('NAME','AGE','EMAIL')` | 若撞保留字，§3.3 改测试 struct |
| 7 | docker engine + WSL2 + 网络可达性 | `wsl -d Ubuntu-24.04 -e docker run hello-world` + `curl https://eco.dameng.com` | 0 号前置检查 step |
| 8 | tar 拉取失败的 abort 阈值 | plan 写定时间盒（建议 24h；超过则推迟到 v0.8.4） | §6 风险表"Docker Hub 第三方"行 |
| 9 | godoes/gorm-dameng 是否真无 cgo | `go list -deps -test github.com/godoes/gorm-dameng \| grep -i cgo` | §5.2"纯 Go 无 cgo"断言依据 |
| 10 | `db.Name()` fail 时连锁修改清单 | spec 加 fail 时同步改 4 处的 checklist：builder.go L242 / missing_coverage_test.go dm 子测试 / 4 个 dm 测试文件 build tag 注释 | §6 风险表第 1 行扩展 |
| 11 | `TEST_DM_DSN` 格式 BNF + 真实样例 | plan 实测后写入 README §9.1 第 2 项 | README §9.1 第 2 项 |
| 12 | 下游生产侧 `import _ "github.com/godoes/gorm-dameng"` 姿势 | 写 README §9.1 第 3 项 + 一个最小可运行示例 | README §9.1 第 3 项 |
| 13 | `COMPATIBLE_MODE=2` 诊断 SQL 与镜像默认值实测 | 启动后 `SELECT PARA_VALUE FROM V$DM_INI WHERE PARA_NAME='COMPATIBLE_MODE'` 验证 | §3.4 + README §9.1 第 6 项 |

**作者本地实施前置**：在 plan 第 1 步前，作者本地需先 `go env -w GOPROXY=https://goproxy.cn,direct`，否则 plan 第 1 步 `go list -m -versions` 卡 proxy.golang.org 超时。

## 12. 后续候选（不在本期）

- **v0.8.4 候选**：DM MySQL 兼容模式（COMPATIBLE_MODE=4），需重测 quoter 策略与列名 case
- **v0.9 候选**：人大金仓 KingbaseES（信创第二大户，PG 兼容模式）
- **v1.0 候选**：批量 RETURNING 适配（解 TD-13），保留字列名自动加引号（解 TD-14）

---

**仅测试基建 + 1 行库代码 case 合并，不涉及核心 API、Repository CRUD、alias 体系；GORM 版本锁定保持 v1.31.x；v0.8.0 / v0.8.1 / v0.8.2 tag 不受影响。**
