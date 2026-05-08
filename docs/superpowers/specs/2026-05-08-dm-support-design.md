# v0.8.3 达梦数据库（DM 8）支持设计

> **版本**：v0.8.3（草案）
> **日期**：2026-05-08
> **作者**：通过 brainstorming skill 协作产出
> **状态**：待用户复核 → 进入 writing-plans
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
| **Docker 镜像** | `oracle/database-free:23c-slim` | `dameng/dm8`（dameng 技术社区获取） | DM 镜像源不在 Docker Hub，需手动拉取或 `docker load` tar |
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
| 本地验证 | **WSL2 + docker 起 `dameng/dm8`** | 沿用 v0.8.2 Oracle plan 的 WSL wrapper 写法 |
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
| `builder.go` | `getQuoteChar` 把 `case "oracle":` 改成 `case "oracle", "dm":` + 注释泛化（**唯一库代码改动**） | 默认 |
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

  export TEST_DM_DSN="dm://SYSDBA:SYSDBA001@127.0.0.1:5236"
  go test -tags=dm -v ./...
  → 默认测试 + DM 测试都跑
  → DM 测试本地拿 DSN 连，无 DSN 时 t.Skip

并行 Oracle + DM：
  go test -tags="oracle dm" ./...
  → 验证空 quoter case 合并不冲突
```

### 3.3 setupDMDB 与 truncateDMTables

`dm_setup_test.go` 提供（与 oracle_setup_test.go 一一对应）：

```go
//go:build dm

// 警告：仅限本地 Docker 开发。SYSDBA 是 DM 默认 DBA 超级账户，
// 密码 SYSDBA001 是 dameng/dm8 Docker 镜像的默认密码——绝不能用于生产。
// CI/生产请用 TEST_DM_DSN 提供独立测试账户，且仅授予最小测试权限。
const defaultDMDSN = "dm://SYSDBA:SYSDBA001@127.0.0.1:5236"

// setupDMDB 与 setupOracleDB 同模式：非泛型，绑定 MySQLUser 复用既有测试 struct。
//
// 标识符长度自检：MySQLUser → my_sql_users (12 chars)；id/username/age/email
// 字段全部 ≤8 chars——沿用 Oracle 12c R1 的 30 字符上限规范（DM 8 实际 128，
// 但保留与既有 Oracle 测试 struct 一致便于跨方言通用）。
func setupDMDB(t *testing.T) (*Repository[int64, MySQLUser], *gorm.DB) {
    t.Helper()
    dsn := os.Getenv("TEST_DM_DSN")
    if dsn == "" { dsn = defaultDMDSN }
    db, err := gorm.Open(dameng.Open(dsn), &gorm.Config{
        Logger: logger.Default.LogMode(logger.Info),
    })
    if err != nil { t.Skipf("DM 不可用，跳过: %v", err) }
    applyDBPoolLimits(t, db)  // 复用既有 helper
    repo := NewRepository[int64, MySQLUser](db)
    truncateDMTables(t, db, &MySQLUser{})
    t.Cleanup(func() { truncateDMTables(t, db, &MySQLUser{}) })
    return repo, db
}

// truncateDMTables：DROP TABLE + AutoMigrate 策略
//
// 决策原因（沿用 Oracle 路径）：
//   - DM Oracle 兼容模式 TRUNCATE 不重置 IDENTITY 序列
//   - ALTER TABLE MODIFY IDENTITY 流程复杂
//   - DROP + AutoMigrate 是最可靠的 IDENTITY 重置方式
//
// PURGE 子句：Oracle 必需，DM 是否支持待实测。若 DM 不支持 PURGE，
// 降级为不带 PURGE 的 DROP（可能触发回收站积压，需测试期决定是否绕开）。
func truncateDMTables(t *testing.T, db *gorm.DB, models ...any) {
    // ... 实施期实测后定型
}
```

### 3.4 Docker 启动命令（WSL2 + Docker Engine）

| 环境 | 命令前缀 | 说明 |
|---|---|---|
| Docker Desktop | 无 | 用户本机未装，跳过 |
| WSL2 + Docker Engine | `wsl -d Ubuntu-24.04 -e docker ...` | 默认环境 |

```bash
# 拉取镜像（首次 ~1-2 GB，dameng 官方镜像源）
wsl -d Ubuntu-24.04 -e docker pull dameng/dm8

# 启动 DM 8 实例（单行，避免 PowerShell 续行符不透传）
wsl -d Ubuntu-24.04 -e docker run -d --name dm8 -p 5236:5236 \
  -e UNICODE_FLAG=1 -e LENGTH_IN_CHAR=1 -e PAGE_SIZE=16 \
  -e EXTENT_SIZE=32 -e LOG_SIZE=2048 -e INSTANCE_NAME=DM8TEST \
  dameng/dm8

# 等待 ~30s-1min 启动（DM 比 Oracle 起得快）
wsl -d Ubuntu-24.04 -e docker logs -f dm8
```

**字符集说明**：
- `UNICODE_FLAG=1`：开启 UTF-8（默认 GB18030 会让中文测试数据损坏）
- `LENGTH_IN_CHAR=1`：VARCHAR 长度按字符算（默认按字节）

**WSL2 mirrored 网络**：`5236:5236` 在 Windows `localhost:5236` 直接可达，DSN 无需改动。

**镜像源 fallback**：如 `dameng/dm8` 在 Docker Hub 不可获取，从 dameng 技术社区下载 tar 包后 `wsl -d Ubuntu-24.04 -e docker load -i dm8.tar`。具体获取路径在 plan 阶段实操确认。

### 3.5 测试覆盖明细

| 测试函数 | 镜像源 | 覆盖项 |
|---|---|---|
| `TestDMDialectorContract` | TestOracleDialectorContract | `db.Name() == "dm"` + `getQuoteChar` 返回 `"", ""` |
| `TestDM_BasicCRUD` | TestOracle_BasicCRUD | Save / GetById / List / Count / UpdateById / DeleteById |
| `TestDM_WhereConditions` | TestOracle_WhereConditions | Ne / LikeRight 前缀 / In / NotIn / Between / GetOne |
| `TestDM_OrderGroupHaving` | TestOracle_OrderGroupHaving | OrderBy DESC / Limit-Offset / GroupBy+Having RawScan / UpdateByCond / DeleteByCond |
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

`missing_coverage_test.go` 的两个表驱动测试加 dm case：

```go
// TestGetQuoteChar_Dialects 加 case
{name: "DM", dbName: "dm", wantL: "", wantR: ""},

// TestQuoteColumn_Dialects 加 case
{dialect: "dm", in: "users.name", want: "users.name"},
```

用 testMockDialector 模拟 dm 方言，避免默认 build 引入 dameng driver。

## 5. 依赖与构建

### 5.1 go.mod 改动

```
require (
    github.com/godoes/gorm-dameng vX.Y.Z  // 待 plan 阶段定型最新版本
)
```

会引入 transitive：
- `gitee.com/chunanyong/dm`（DM 官方 Go 驱动）
- 其它 dameng/dm 关联包

### 5.2 默认 build 影响

- `go test ./...`：DM 测试文件 build tag 隔离，不参与编译，不需要 dameng driver 加载
- `go build`：dameng driver 因 build tag 不被引用，但 `go.sum` 锁定其哈希
- 下游 `go mod tidy`：写入 dameng transitive 到 go.sum（**TD-12 重申**：单模块带可选 driver 副作用）

## 6. 实施风险（按概率排序）

| 风险 | 概率 | 影响 | 应对 |
|---|---|---|---|
| `db.Name()` 不返回 `"dm"`（可能是 `"dameng"`/`"DM"`） | 中 | 中 | 契约测试第一时间暴露，改 builder.go case 字符串 |
| dameng/dm8 docker 镜像源不通 / 需付费 | 中 | 高 | 启动期先 docker pull 测试，失败时改用 dameng 技术社区 tar 包 + `docker load` |
| DM Oracle 兼容模式默认未开启 | 低 | 高 | 启动 docker 时加 `COMPATIBLE_MODE=2` 显式指定 |
| DM migrator 行为与 Oracle 不同（例如不支持 PURGE） | 中 | 中 | 实测期降级为无 PURGE 路径（在 plan 阶段标识为可能修订点） |
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
- **TD-14**（保留字列名自动加引号）：DM 同样不自动处理

## 9. 文档变更

| 文件 | 改动 |
|---|---|
| `README.md` 方言矩阵 | 加 DM 行：`dm \| ✅ \| build tag: -tags=dm \| 同 Oracle 限制` |
| `README.md` 已知方言差异速查 | DM 章节直接引用 Oracle 章节 + 一句 "DM 8 Oracle 兼容模式继承全部 Oracle 限制" |
| `CHANGELOG.md` v0.8.3 段 | 新增段：DM 数据库支持，记录 builder.go case 合并 + 4 个 build-tag 测试文件 |
| `CLAUDE.md` | 不动（架构未变） |

## 10. 验收清单

- [ ] `builder.go: getQuoteChar` 改一行 `case "oracle", "dm":` + 注释更新
- [ ] `go.mod` 加 `godoes/gorm-dameng` 依赖（最新稳定版）
- [ ] 默认测试 `go test ./...` 不变（不触及 DM）
- [ ] `go test -tags=dm ./...` 跑 DM 8 Oracle 兼容模式 9 个测试全过
- [ ] `go test -tags="oracle dm" ./...` 双方言并行跑全过（验证空 quoter case 合并不冲突）
- [ ] `dm_setup_test.go` / `dm_contract_test.go` / `dm_integration_test.go` / `alias_dm_test.go` 完成
- [ ] `missing_coverage_test.go` 加 dm case 覆盖
- [ ] README 方言矩阵 + 已知差异速查加 DM
- [ ] CHANGELOG v0.8.3 段写完
- [ ] commit 序列：deps → builder fix + contract test → setup → integration → alias → docs（沿用 v0.8.2 节奏）
- [ ] 推 v0.8.3 tag 到 GitHub

## 11. 后续候选（不在本期）

- **v0.8.4 候选**：DM MySQL 兼容模式（COMPATIBLE_MODE=4），需重测 quoter 策略与列名 case
- **v0.9 候选**：人大金仓 KingbaseES（信创第二大户，PG 兼容模式）
- **v1.0 候选**：批量 RETURNING 适配（解 TD-13），保留字列名自动加引号（解 TD-14）

---

**仅测试基建 + 1 行库代码 case 合并，不涉及核心 API、Repository CRUD、alias 体系；GORM 版本锁定保持 v1.31.x；v0.8.0 / v0.8.1 / v0.8.2 tag 不受影响。**
