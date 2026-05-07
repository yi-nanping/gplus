# v0.8.2 Oracle 数据库支持设计

> **版本**：v0.8.2（草案）
> **日期**：2026-05-07
> **作者**：通过 brainstorming skill 协作产出
> **状态**：待用户复核 → 进入 writing-plans
> **前置版本**：v0.8.1（PG 三方言验证）
> **后续候选**：达梦数据库（dm）支持

---

## 1. 背景与动机

### 1.1 下游需求

v0.8.1 已完成 PG 三方言 CI 验证（sqlite + MySQL 8.0 + PostgreSQL 16），证实 v0.8.0 alias 体系在三方言下零库代码 bug。但下游项目存在**国产化与企业场景需求**：

- 部分企业级下游需用 Oracle 12c+（金融、政务、电信）
- 后续还需支持达梦数据库 dm（信创场景，沿用 Oracle 兼容模式）

本期仅做 Oracle，达梦留给下一轮（先把 Oracle 框架铺好，达梦多半 80% 复用）。

### 1.2 Oracle 与已有方言的差异

| 维度 | sqlite/mysql/pg | Oracle |
|---|---|---|
| **GORM 官方驱动** | ✅ 有 | ❌ 无，需第三方 |
| **Docker 镜像** | ✅ Docker Hub 公开 | ⚠️ `oracle/database-free:23c-slim` 在 Docker Hub，但启动慢（~3 min）、镜像大（~3 GB） |
| **CI service 集成** | ✅ 秒级启动 | ⚠️ 启动慢 + license 复杂度 |
| **占位符** | `?` / `$N` | `:1, :2`（命名占位符，由驱动 rebase） |
| **AUTO_INCREMENT** | 标准 | 12c+ `IDENTITY` 列；11g 及更老用 sequence + trigger |
| **LIMIT/OFFSET** | 标准 | 12c+ `FETCH FIRST N ROWS ONLY`；11g 及更老用 ROWNUM 嵌套 |
| **空字符串 vs NULL** | 区分 | **Oracle 把 `''` 自动转 NULL**（致命差异） |
| **ON CONFLICT** | 标准 | 不存在，用 `MERGE INTO` |
| **BOOL 类型** | 标准 | 不存在，用 `NUMBER(1)` 模拟 |
| **默认命名 case** | lowercase | UPPERCASE（GORM Dialector 通常做 lowercase 强转） |
| **CLOB/TEXT 字段 WHERE** | 直接 `=`/`LIKE`/`IN` 即可 | **Oracle 不允许** CLOB 列直接做 WHERE，需 `DBMS_LOB.SUBSTR` 或 `TO_CHAR` 转换；若 Dialector 把 Go `string` 长字段映射成 CLOB，`LikeRight`/`In` 会报 `ORA-00932` |
| **NULLS 排序默认** | sqlite/PG 升序 NULL 在前 | Oracle **升序 NULL 排末尾**（NULLS LAST 默认），与 PG/SQLite 相反——含 NULL 列的 ORDER BY 结果集顺序不一致 |
| **RETURNING 批量** | 支持批量 `INSERT ... RETURNING` | Oracle 12c `RETURNING INTO` **仅支持单行**——`SaveBatch`/`UpsertBatch` 走 RETURNING 路径会失败 |
| **TIMESTAMP 时区** | 标准 TIMESTAMPTZ | 有 `TIMESTAMP WITH TIME ZONE` vs `TIMESTAMP WITH LOCAL TIME ZONE` 两种，Dialector 映射决定夏令时边界行为 |
| **字符集** | UTF-8 默认 | 取决于实例 `NLS_CHARACTERSET`，若是 `WE8ISO8859P1` 则中文测试数据会损坏；docker 启动需指定 `ORACLE_CHARACTERSET=AL32UTF8` |
| **标识符长度** | 大多数 64 字符以上 | 12c R1 及更老 **30 字符上限**；12c R2+ 128 字符——超长标识符报 `ORA-00972` |

## 2. 决策摘要

### 2.1 范围决策

| 项 | 决策 | 理由 |
|---|---|---|
| Oracle 版本目标 | **12c+（含 23c Free）** | 现代特性齐全，11g 已 EOL；老版本以后再加 |
| GORM Dialector | **`github.com/godoes/gorm-oracle v1.6.18`** | 活跃维护 2024+；社区推荐 |
| Go 驱动 | **`github.com/sijms/go-ora/v2`**（transitive） | 纯 Go，无 Oracle Client C 库依赖 |
| 测试隔离 | **`//go:build oracle` build tag** | 默认 build/test 不触及，CI 不变 |
| CI 集成 | **不做** | Oracle 启动慢、license 复杂；build tag 留给下游验证入口 |
| 本地验证 | **作者用 docker 起 `oracle/database-free:23c-slim` 实测** | 等价 PG 那次本地迭代 |
| 达梦 | **不在本期** | 留给下一轮；Oracle 框架铺好后达梦复用率高 |

### 2.2 架构决策

| 决策点 | 选择 | 备选 |
|---|---|---|
| 测试代码组织 | 自包含 build tag 文件，不动 `testdb_test.go` | 在 `testdb_test.go` 加 oracle 分支 → 默认 build 拉 driver，污染 |
| 库代码改动 | **仅 `builder.go: getQuoteChar` 加 oracle 分支** | 改 update.go / query.go 的 LIMIT/OFFSET 重写器 → 12c+ 不需要 |
| 驱动方言名 | `db.Name() == "oracle"`（gorm-oracle Dialector 默认） | — |
| Oracle 命名 case | **依赖 gorm-oracle Dialector 默认行为** | 实测后再决定要不要库代码层强转 |
| 工作流 | 每 commit 作者本地实测 + 用户 review GitHub commit | CI 不参与 |

## 3. 架构

### 3.1 文件改动清单

**新建（3 个，全 build tag 隔离）：**

| 文件 | build tag | 内容 |
|---|---|---|
| `oracle_setup_test.go` | `//go:build oracle` | `setupOracleDB` helper、`defaultOracleDSN`、`truncateOracleTables`（DROP TABLE PURGE + AutoMigrate） |
| `oracle_contract_test.go` | `//go:build oracle` | Dialector 契约断言：`db.Name() == "oracle"`、`getQuoteChar` 返回双引号 |
| `oracle_integration_test.go` | `//go:build oracle` | 5 个测试：BasicCRUD / WhereConditions / OrderGroupHaving / JoinQuery / QuoteColumn |
| `alias_oracle_test.go` | `//go:build oracle` | 3 个测试：自连接 / alias 字段 q.Eq / correlated EXISTS |

**修改（4 个，必需）：**

| 文件 | 改动 | build tag |
|---|---|---|
| `go.mod` | `require github.com/godoes/gorm-oracle v1.6.18` | 默认 |
| `go.sum` | 加 transitive deps（sijms/go-ora、emirpasic/gods） | 默认 |
| `builder.go` | `getQuoteChar` 加 `case "oracle":` 分支返回 `"`（**唯一库代码改动**） | 默认 |
| `missing_coverage_test.go` | `TestQuoteColumn_Dialects` / `TestGetQuoteChar_Dialects` 加 oracle case 覆盖 | 默认 |

**不动**：

- `testdb_test.go`（不在默认 import 中带 Oracle driver）
- 其他库代码（query.go / update.go / repository.go / alias.go / subquery.go / schema.go / debug.go）
- CI 配置（`.github/workflows/ci.yml` 保持 sqlite + mysql + pg）
- 现有所有测试

### 3.2 测试运行流程

```text
默认（无 build tag）：
  go test ./...
  → 跑 sqlite/mysql/pg 路径（CI 也走这条）
  → Oracle 测试文件因 //go:build oracle 不参与编译
  → 行为不变

Oracle 验证（手动）：
  docker run -d --name ora -p 1521:1521 ... oracle/database-free:23c-slim
  export TEST_ORACLE_DSN="oracle://system:oracle@127.0.0.1:1521/FREEPDB1"
  go test -tags=oracle -v ./...
  → 默认测试 + Oracle 测试都跑
  → Oracle 测试本地拿 DSN 连，无 DSN 时 t.Skip
```

### 3.3 setupOracleDB 与 truncateOracleTables

`oracle_setup_test.go` 提供：

```go
//go:build oracle

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
    if dsn == "" { dsn = defaultOracleDSN }
    db, err := gorm.Open(oracle.Open(dsn), &gorm.Config{
        Logger: logger.Default.LogMode(logger.Info),
    })
    if err != nil { t.Skipf("Oracle 不可用，跳过: %v", err) }
    applyDBPoolLimits(t, db)  // 复用既有 helper
    if err := db.AutoMigrate(&MySQLUser{}); err != nil {
        t.Fatalf("迁移失败: %v", err)
    }
    truncateOracleTables(t, db, &MySQLUser{})
    t.Cleanup(func() { truncateOracleTables(t, db, &MySQLUser{}) })
    return NewRepository[int64, MySQLUser](db), db
}

// truncateOracleTables：DROP TABLE PURGE + AutoMigrate 策略
//
// 决策原因（数据库审计建议）：
//   - Oracle TRUNCATE 不重置 IDENTITY 序列
//   - ALTER TABLE MODIFY IDENTITY 流程复杂（先去掉 IDENTITY 再重建）
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
        // 显式 PURGE 避免回收站积压
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

### 3.4 getQuoteChar 改动

`builder.go` 现状：

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

**唯一库代码改动**。Oracle 与 sqlite/postgres 同走双引号分支。

## 4. 数据流与错误处理

### 4.1 测试失败 → 修复路径

PG 那次的工作流复用：作者本地实测发现 FAIL → 分析根因（库 bug vs 测试方言假设）→ 修复 → commit。

预期高概率踩坑（先打预防针）：

| 场景 | 预期问题 | 应对 |
|---|---|---|
| **空字符串 = NULL** | `IsNull` 测试种子用空字符串 → Oracle 自动转 NULL，断言失败 | 显式 NULL 种子 + 注释说明 |
| **表名 UPPERCASE** | `my_sql_users` 在 Oracle 默认成 `MY_SQL_USERS` | 实测 Dialector 行为，按以下 fallback 链处理：<br>1. Dialector 默认 lowercase 强转 → 不需做任何事<br>2. Dialector 不强转但接受 lowercase → 加 GORM `gorm:"column:..."` 标签到测试 struct<br>3. Dialector 完全 case-sensitive 且不强转 → 本期 patch 库代码（getQuoteChar 之外的额外改动），更新 §3.1 改动清单 |
| **CLOB/TEXT WHERE 限制（高概率）** | Go `string` 长字段映射成 CLOB 时 `LikeRight`/`In` 会报 `ORA-00932` | 测试 struct 所有 `string` 字段**强制加 `gorm:"size:N"` tag**（N ≤ 4000 字节），避免 Dialector 默认成 CLOB；NLS 字符语义下中文要按 `size * 4` 估算 |
| **NULLS 排序默认** | Oracle 升序 NULL 排末尾，与 PG/SQLite 相反 | `OrderGroupHaving` 测试避免对含 NULL 列的排序顺序做强断言；如必要用 `Order RAW("col NULLS FIRST")` 显式指定 |
| **RETURNING 批量** | `SaveBatch`/`UpsertBatch` 走 RETURNING 路径在 Oracle 12c 上失败 | 实测发现失败时，将相关测试在 Oracle 下 `t.Skip` 并加 README 注明；不修库代码 |
| **TIMESTAMP WITH TIME ZONE** | go-ora 把 `time.Time` 映射到 `TIMESTAMP WITH TIME ZONE` 还是 `TIMESTAMP WITH LOCAL TIME ZONE` 决定夏令时边界行为 | 本期不专项测试时区边界；测试 struct 仅用 `time.Time` 通用语义，不依赖具体时区类型；如发现夏令时下断言失败，记入技术债 |
| **SELECT FOR UPDATE / SKIP LOCKED** | gplus `GetByLock` 走 GORM Lock 接口，go-ora/gorm-oracle 是否正确翻译 `FOR UPDATE` 和 12c+ `SKIP LOCKED` 未知 | 本期不专项测试 GetByLock；如未来下游用到再补；记入 §9.1 推迟项 |
| **标识符长度（30/128 字符）** | 长表名/列名超限报 `ORA-00972` | 测试 struct 保持短命名（≤30 字符以兼容 12c R1）；CI 不验 12c R1，但 spec 明确支持版本基线 |
| **`RETURNING "id"` 语法** | GORM 默认会调用，Oracle 12c+ 支持但语法可能略不同 | 实测，gorm-oracle Dialector 应已处理 |
| **LikeRight 大小写敏感** | Oracle 默认大小写敏感（同 PG） | 测试用首字母大写匹配（同 PG 修复策略） |
| **HAVING 别名** | Oracle 严格 SQL（同 PG）不允许 | 用聚合表达式（同 PG 修复策略） |
| **占位符 `:N`** | gorm-oracle Dialector 应自动 rebase | 不应是问题，但测试要避免占位符断言 |

### 4.2 本地 docker 起不来的回退

如果作者本地 Oracle Free docker 起不来：

- 回退方案：写代码 + 用户本地或 Oracle 测试环境跑 `go test -tags=oracle` → 反馈日志 → 作者远程修复
- 风险：迭代慢（来回贴日志），但仍可推进
- 预防：Commit 1 之前先验证本地 docker 起得来

## 5. 测试策略

### 5.1 测试层次

复用 PG 那一轮的两层结构：

**第一层：alias 体系 SQL 生成验证**（`alias_oracle_test.go`）
- 不依赖真实数据，只验证 `q.ToSQL(db)` 生成的 SQL 在 Oracle 方言下正确
- 验证点：双引号转义、JOIN 语法、EXISTS 子查询、字面量内联
- 镜像 `alias_pg_test.go` 三个测试

**第二层：CRUD 真实执行验证**（`oracle_integration_test.go`）
- 跑真实 INSERT/UPDATE/DELETE/SELECT
- 5 个测试镜像 `pg_integration_test.go`：
  1. `TestOracle_BasicCRUD` — Save / GetById / List / Count / UpdateById / DeleteById
  2. `TestOracle_WhereConditions` — Ne / LikeRight / In / NotIn / Between / IsNull / GetOne
  3. `TestOracle_OrderGroupHaving` — Order / Page / GroupBy + RawScan / UpdateByCond / DeleteByCond
  4. `TestOracle_JoinQuery` — LeftJoin 自连接（双引号转义验证）
  5. `TestOracle_QuoteColumn` — `getQuoteChar` 直接验证 Oracle 双引号

### 5.2 验收标准

每个 commit：
1. 默认 `go test ./...`（无 oracle tag）通过——保证默认编译/测试不破坏
2. `go test -tags=oracle ./...` 在作者本地 docker 上 8 个 Oracle 测试全过
3. `go vet ./...` 干净
4. `go build ./...` 干净

## 6. 落地计划

按 brainstorming → writing-plans 流程，**spec 仅描述目标**，详细 step-by-step 由 writing-plans 阶段产出。本节仅给 commit 切分骨架：

| Commit | 内容 | 验收 |
|---|---|---|
| 1 | 依赖 + `oracle_setup_test.go` 基础 helper | 默认 build/test 不破坏；oracle tag 编译过 |
| 2 | `builder.go: getQuoteChar` 加 oracle 分支 + 既有方言测试加 oracle case + 新建 `oracle_contract_test.go` 承载 Dialector Name 契约断言 | 默认 quote 测试加 oracle case 全过；Oracle 实例返回 `db.Name() == "oracle"` |
| 3 | `oracle_integration_test.go` 5 个 CRUD 测试 | 本地 docker 全过 |
| 4 | `alias_oracle_test.go` 3 个 alias 测试 | 本地 docker 全过 |
| 5 | README 方言矩阵 + Oracle 限制清单 + CHANGELOG v0.8.2 | 文档评审 |

## 7. 风险

| 风险 | 概率 | 影响 | 缓解 |
|---|---|---|---|
| godoes/gorm-oracle 与 GORM v1.31 兼容性 | 中 | 阻塞 | Commit 1 完成后立即 `go build` 验证 |
| Oracle 默认 UPPERCASE 与 gplus snake_case 冲突 | 中 | 测试失败可能要修库 | 实测 Dialector 行为；不行的话本期 Patch，长期 v0.9 重构 |
| **CLOB/TEXT WHERE 限制** | 高 | 测试失败 | 见 §4.1 缓解策略（强制所有 string 字段加 size tag） |
| 空字符串 = NULL 在 IsNull 测试 | 高 | 测试失败 | 测试中显式 NULL 种子 + 注释说明 |
| 作者本地 docker 起不来 Oracle Free | 低 | 工作流回退 | Commit 1 之前先验证 docker；起不来切"用户本地验证"模式 |
| Commit 2 库改动影响其他测试 | 低 | 不应影响 | `getQuoteChar` 加 case 不破坏现有逻辑（postgres/sqlite 已用同样的双引号） |
| GORM 升级时 gorm-oracle 不兼容 | 中 | 长期债 | CHANGELOG 标注；下游需要时再适配 |
| Oracle license 风险 | 低 | 下游使用限制 | Oracle Free 商用 OK，README 提示下游自查 license |
| **下游 go.sum 膨胀**（HIGH-1，方案 P 接受） | 中 | 下游 `go mod tidy` 写入 sijms/go-ora 等 transitive | 接受单模块带可选 driver 模式（同 GORM 自身）。**澄清**：build tag 仅隔离测试代码编译——`go mod tidy` 不受 build tag 影响，仍会拉取所有 require 模块到 go.sum，但二进制不会链接 driver 代码。go.sum 膨胀是单模块带可选 driver 的固有代价；下游介意可用 `replace` 排除 |
| **`db.Name() == "oracle"` 字符串契约** | 低 | 上游 Dialector 改名导致库默认分支 fallback | Commit 2 新建 `oracle_contract_test.go` 承载断言（不放 missing_coverage_test.go 因为它无 build tag） |
| **标识符长度 30/128 字符上限** | 低 | 测试 struct 长名报 `ORA-00972` | 命名约束在 Commit 1 测试 struct 自检时确认 |
| **NULLS LAST 排序默认与 PG 相反** | 中 | OrderBy 含 NULL 列断言失败 | 见 §4.1 缓解策略 |
| **RETURNING 不支持批量** | 中 | `SaveBatch`/`UpsertBatch` 失败 | 见 §4.1 缓解策略 |

## 8. 验收清单

- [ ] 默认 `go test ./...`（无 oracle tag）通过——0 个 Oracle 测试参与
- [ ] `go test -tags=oracle ./...` 在作者本地 docker 上 8 个 Oracle 测试全过
- [ ] `go vet ./...` 干净
- [ ] `go build ./...` 干净
- [ ] CI 跑通（应该完全不受影响）
- [ ] README 方言矩阵更新含 Oracle，标注"build tag 跑法"
- [ ] CHANGELOG v0.8.2 段完整
- [ ] 库代码改动局限于 `builder.go: getQuoteChar` 一处
- [ ] `getQuoteChar` 既有 default 分支 fallback 行为不变（向下兼容）
- [ ] v0.8.0 alias 体系在 Oracle 下生成正确 SQL（双引号 + 字面量内联 + EXISTS）

## 9. 范围与债

### 9.1 不在本期范围

| 项 | 推到何时 |
|---|---|
| Oracle 11g 及更老（sequence + trigger 自增、ROWNUM 重写） | 用户后续要求时 |
| Oracle CI service 集成 | 评估 CI 时间影响后决定 |
| 达梦数据库 dm | 下一轮（v0.8.3 候选） |
| 子模块拆分（gplus/oracle 独立 go.mod） | 当出现第 2/3 个可选 driver（如达梦+Oracle+SQLServer）时再做，符合 Rule of Three |
| `SaveBatch`/`UpsertBatch` 在 Oracle 下的批量 RETURNING 适配 | 若实测失败，本期仅 `t.Skip` 标注；库代码批量 RETURNING 重写推到 v0.9+ |
| `GetByLock` / `SELECT FOR UPDATE` / `SKIP LOCKED` 在 Oracle 下专项验证 | 本期不测；下游用到再补 |
| TIMESTAMP WITH TIME ZONE 时区边界专项测试 | 本期不测；如夏令时下断言失败再补 |
| 命名 case 库代码层强转（如果 Dialector 不处理） | 见 §4.1 表名 UPPERCASE fallback 链 |

### 9.2 已知技术债

| 债 | 说明 |
|---|---|
| TD-9：Oracle 测试无 CI 守护 | build tag 测试腐烂风险，依赖下游手动跑发现问题 |
| TD-10：第三方 Dialector 维护风险 | gorm-oracle 由社区维护，GORM 升级时可能滞后 |
| TD-11：Oracle 11g 不支持 | 老版本仍存在企业场景，本期不做 |
| TD-12：单模块带可选 driver | go.sum 膨胀（6+ transitive），主流 Go 库共有问题。**注意**：build tag 仅隔离测试编译，不影响 `go mod tidy` 拉取。下游可 `replace` 排除 |
| TD-13：批量 RETURNING 不重写 | `SaveBatch`/`UpsertBatch` 在 Oracle 下若失败仅 `t.Skip`，未做库层适配 |

---

## 附录 A：参考资料

- gorm-oracle: https://github.com/godoes/gorm-oracle
- sijms/go-ora: https://github.com/sijms/go-ora
- Oracle Database Free 23c: https://www.oracle.com/database/free/
- v0.8.1 PG 三方言验证 spec：本仓 `docs/superpowers/specs/`（无独立 spec，工作流入口为 v0.8.0 alias 体系 spec 的延续）
