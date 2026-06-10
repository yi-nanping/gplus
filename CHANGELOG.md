# Changelog

所有版本变更记录遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/) 格式，版本号遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)。

## [0.12.0] - 2026-06-10

本版聚焦 2026-06-10 全项目审计的修复闭环：数据权限缺口、错误累积双轨制统一（Build* 窄腰 fail-closed）、死代码移除与文档债清理。含行为变更（见下方 ⚠️），按 v0.x 惯例记 MINOR 版本。

### 修复（安全/一致性）

- **Build\* 窄腰统一错误短路**：`ScopeBuilder` 四条构建路径（BuildQuery/BuildCount/BuildUpdate/BuildDelete）闭包入口统一检查本体 `errs` 与链级 `core.errs` 双桶，任一非空则注入聚合错误、不生成 SQL。取代 v0.8.0 决策 1B 的局部短路（仅 Query.BuildQuery × core.errs）。errs/core 字段下沉至 ScopeBuilder
- **FirstOrUpdate 数据权限补齐**：UPDATE 阶段接入 `u.DataRuleBuilder()`（与 UpdateByCond 对称）；按主键重读改为带 DataRule 的查询
- `Updater.Exists/NotExists(nil)` 错误写入桶对齐为本体 `errs`（与 Query 侧及 InSub 一致）

### ⚠️ 行为变更

- 直接 `.Scopes(q.Build*())` / `.Scopes(u.Build*())` 且不自查 `GetError()` 的调用方：builder 带错时从"执行条件残缺的 SQL"（fail-open，错误条件被静默丢弃）变为"`db.Error` 返回聚合错误、不执行"（fail-closed）。Repository / `ToDB` / debug 路径无变化（GetError 前置已拦）
- `FirstOrUpdate` 在 DataRule 生效时：u 侧 ctx 规则不匹配目标行 → UPDATE affected=0、返回未变更行；更新将行改出权限可见范围（如改 tenant_id）→ 返回 `gorm.ErrRecordNotFound` 且**事务整体回滚**（禁止经本方法把行移出自身权限范围）

### 移除

- 删除死代码导出符号 `ColumnInfo`（utils.go）与 `KeyAnd`/`KeyOr`（consts.go）——全库及已知下游（gvs-server）零引用。若有外部代码引用，迁移：`KeyAnd`/`KeyOr` 用字面量 `"AND"`/`"OR"`，`ColumnInfo` 无替代（从未被任何 API 消费）

### 文档

- README：DataRule `Condition` 操作符支持表（LIKE 自动双侧包 `%`、LEFT_LIKE/RIGHT_LIKE 补单侧）；`NewQueryAs` 主别名 First 路径/写路径限制警示（原仅在 CHANGELOG v0.9.0）；FindAs × SelectExpr 组合限制（表达式列无 AS 别名不可按名映射）；InsertSelectMap 成功后 q 被追加投影的约束警示与 InsertSelect 选型指南；修复 DataRule 示例废弃字段 `Op`/`Val` → `Condition`/`Value`；版本历史补 v0.7~v0.11 索引
- CLAUDE.md：错误处理双轨规则文档化；`DeleteByCondTX(ctx, q, tx)` 笔误修正为实际签名 `DeleteByCondTx(q, tx)`

## [0.11.0] - 2026-06-10

本版新增类型化投影表达式（`Expr`/`Col`/`Lit`/`Add` + `Query.SelectExpr`）、规范单例导出 `Model[T]()` 与成对列映射 `InsertSelectMap`，让 `INSERT...SELECT...JOIN` 写操作做到零手写 SQL 字符串。向后兼容，MINOR 版本。

### 新增

- **`Model[T]()` 规范单例导出**：返回类型 T 的规范单例指针（字段地址注册于全局 cache），作为 `InsertSelect` targetCols / `InsertSelectMap` Target 的字段指针来源。⚠️ 只读锚点，禁写字段值。
- **类型化投影表达式（`Expr` / `Col` / `Lit` / `Add` + `Query.SelectExpr`）**：消灭投影侧裸 SQL 表达式片段。
  - `Col(&model.Field)` 字段引用（调用期经 alias 链解析）/ `Lit(val)` 字面量（参数化绑定防注入）/ `Add(...)` 变长加法
  - `q.SelectExpr(e Expr)` 追加 1 个类型化投影列；Col 地址在 **SelectExpr 调用期**解析（错误立即累积，终端方法 `GetError()` 前置拦截，不发 SQL）
  - 最小算子集（YAGNI）：仅 `Add`；`Sub`/`Mul`/函数/CASE 留待真实调用点
  - 新 sentinel：`ErrExprEmpty`（Add 空操作数）/ `ErrExprUnknownNode`（未知节点，封闭接口内部防御）
- **`InsertSelectMap` / `InsertSelectMapTx` 成对列映射**：`InsertCol{Target, Src}` 逐对声明目标列与源表达式，列数不匹配与顺序错位从「运行时数据错」提升为「结构上不可能」。
  - 投影独占：src 不得有手动 Select/SelectRaw/SelectExpr（违反返回 `ErrInsertSelectMapConflict`）
  - 阶段 A 全量解析（fail-fast 零追加）→ 阶段 B 统一追加 → 复用 `InsertSelectTx` 主流程
  - Target 走包级 `resolveColumnName`（未注册→`ErrColumnNotFound`，不污染 src.errs，q 可复用）；Src 的 Col 走 alias 链（未注册→`ErrFieldAddrUnregistered`，污染 src.errs，q 不可复用）
  - 成功路径变更 src（追加投影），天然防重入（二次调用撞 `ErrInsertSelectMapConflict`）；失败路径对 src.selects 零副作用
- **下游收益**：`gvs-server` 闭包表自连接搬移可零手写 SQL（字段指针表达列引用，改列名构建期报错）。

### 测试

- 覆盖率维持 95.4%；新增 `model_export_test.go` / `expr_test.go` / `insert_select_map_test.go`，AC 1:1 映射测试。

---

## [0.10.0] - 2026-06-08

本版新增 `PageAs` 投影分页与 `DataRule.Table` 跨表数据权限字段，兑现 v0.8.0 路线图承诺并消除 v0.9.0 反向兼容债。向后兼容，MINOR 版本。

### 新增

- **`PageAs` / `PageAsTx` 投影分页**：包级泛型 `PageAs[T, Dest, D comparable]`，等价 `repo.Page` 但把结果投影到自定义 `Dest`（JOIN 多表 + VO 场景），走 GORM Query callback chain（下游隔离/审计 callback 触发，与 `FindAs` 一致）。
  - 返回 `(total int64, err error)`；`skipCount=true` 跳过 COUNT（`total` 恒 0），`false` 时若 COUNT=0 提前返回、不执行投影 Find
  - 与 `FindOneAs` 不同：内部用 `Find` 不追加 `LIMIT 1`，与 `q.Page()` 的 LIMIT/OFFSET 协同
- **`DataRule.Table` 跨表数据权限字段**：为数据权限规则显式指定作用的表名 / JOIN 别名前缀（如 `Table: "ext"`），生成 `ext.dept_id` 限定列。
  - 单一真相源 helper `resolveDataRuleColumn`：全部列名侧校验内聚（旧路径 Column 走 `validDataRuleColumn` 白名单；新路径 Table 单段校验 + 拼接结果防御性复校验，INV-1 最后防线）
  - Query 与 Updater 两侧接入塌缩为同一形态（INV-3 防双侧漂移），置于空值 early-return 之前（INV-2，保证 IS NULL / BETWEEN 等操作符也带 Table 前缀）
  - fail-fast：`Table` 非空时 `Column` 禁含点（禁两套等价写法）；`Table` 仅单段（拒 `schema.table`）、不 TrimSpace
  - 向后兼容：`Table` 空时旧 `Column:"ext.dept_id"` 点前缀写法零回归

### 变更（还反向兼容债）

- 兑现 v0.8.0 路线图「跨表 DataRule（DataRule.Table 字段）」承诺（v0.9.0 未交付，本版补齐）。
- v0.8.0 行为约束「DataRule.Column 不应写 alias 前缀」更新：跨表权限现由 `Table` 字段正式提供；旧 Column 点前缀仍向后兼容，新代码引导用 `Table`。
- v0.9.0 已知限制「DataRule 裸列自连接 ambiguous，须 Column 自带别名前缀」被取代：现可写 `Table:"ext", Column:"dept_id"`（裸列），由 gplus 拼前缀。

---

## [0.9.0] - 2026-06-05

本版聚焦查询/写入能力扩展（Round 1~3b）。新增均为向后兼容的公开 API，不破坏 v0.8.x 既有行为，故为 MINOR 版本。

### 新增

- **`SelectRaw` 参数绑定**（Round 1）：`SelectRaw(expr string, args ...any)` 支持 `?` 占位符参数绑定，走 GORM `clause.Expr{Vars}` 防注入。
  - `ScopeBuilder.selects` 由 `[]string` 升级为 `[]selectItem{expr, args, isRaw}`（零行为变更重构）
  - 双路径：无 args 走原 `[]string` 路径（零回归，逗号无空格）；有 args 拼单串走 `clause.Expr`
- **`InsertSelect` / `InsertSelectTx` 跨表写入**（Round 2 + 3b）：包级泛型 `InsertSelect[T, S, D comparable]`，生成 `INSERT INTO <T>(cols) SELECT ...`，子查询裸 `?` 内联无外层括号。
  - scenario 1：单表无 JOIN（Round 2）
  - scenario 2：自连接 `INSERT...SELECT...JOIN`（Round 3b，与主别名 FROM 物化组合零实现改动解锁）
  - 守卫链：nil → GetError → modifier 拒绝（distinct/omits）→ noProjection → 解析 targetCols（string 走白名单 / 指针走 `resolveColumnName`）→ colMismatch → exec，任一失败零副作用
  - 4 个 sentinel error：`ErrInsertSelectColMismatch` / `ErrInsertSelectNoProjection` / `ErrInsertSelectColInvalid` / `ErrInsertSelectModifier`
  - **不应用 DataRule**（结构性写入，不被数据权限过滤）
- **主别名 FROM 物化**（Round 3a）：`NewQueryAs(ctx, alias)` 在 SELECT 路径（`List` / `Count` / `Page`）物化为 `FROM <table> AS <alias>`，支撑自连接源 query 形态。
  - `validTableName` 正则防注入（与 `validDataRuleColumn` 同源单点）；`SubQuery`/`SubQueryAs` 清空主别名防子查询 FROM 污染；`Clear` 重置；`Table()` 覆盖优先

### 修复

- **`Distinct` 与 `SelectRaw(args)` 混用静默丢失 `DISTINCT`**：GORM Expression 路径忽略 `Statement.Distinct`，args 路径 `b.distinct` 时前置 `"DISTINCT "`（commit `c1ee736`）
- **gosec G115/G103**：加 `nosec` 注释 + 设计意图说明（commit `7ac57f8`）
- **测试方言无关化（修复 PG CI）**：`main_alias_from_test.go` / `insert_select_join_test.go` 写死了 SQLite 专属的标识符引号（双引号）与错误消息文本，CI 接入 PostgreSQL 后失败。新增 `stripIdentQuotes` helper 去引号后断言 FROM/AS 结构；错误断言改 `err!=nil` + 含表名（三方言公共子串）+ 零副作用；`closure_2024` DDL 改 GORM 建表（手写 `AUTOINCREMENT` 仅 SQLite 支持）+ 持久库 DROP 清理（commit `94d6d0f`）

### 已知限制（主别名 / InsertSelect）

- **主别名 First 路径不支持**：`GetOne` / `Last` / `GetByLock` / `FirstOrCreate` 下 GORM 自动 `ORDER BY <table>.id` 裸表名被别名遮蔽报错
- **主别名写路径不物化**：`Delete` / `Update` 走 `BuildDelete`/`BuildUpdate`，FROM 不物化别名（WHERE/SET 字段带别名前缀，裸表无该别名 → 真实执行失败）
- **自连接软删除表约束**：必须 `Unscoped()` + 手动两侧别名前缀（如 `ext.deleted_at IS NULL` / `sub.deleted_at IS NULL`），缺前缀会复活已删数据复制成未删行（不可逆污染）
- **DataRule 裸列自连接 ambiguous**：用户须 `Column` 自带别名前缀（0.10.0 `DataRule.Table` 已支持裸列跨表）
- 多段表名 `a.b.c` 不支持；`SubQueryAs` 自定义别名 FROM 不物化（既有限制）

### 方言风险（CI SQLite 覆盖不到，真机残留项）

- **MySQL**：官方允许 INSERT...SELECT 目标表出现在顶层 FROM（内部临时表；error 1093 仅限 UPDATE/DELETE 同表子查询）
- **达梦 DM**：JOIN INSERT...SELECT 连接列须 PK/UNIQUE + CASE_SENSITIVE/COMPATIBLE_MODE 大小写残留项；PostgreSQL 允许

### 内部 / 测试

- `selects []string` → `[]selectItem` 重构（零行为变更）
- gofmt -w 8 文件 + 本地 CRLF baseline 治理
- 新增测试：`select_raw_args_test.go` / `insert_select_test.go` / `insert_select_join_test.go` / `main_alias_from_test.go`（覆盖率 95%+）
- CI 加 PostgreSQL 16 service container 后三方言测试基线对齐

### 文档

- spec：`docs/superpowers/specs/2026-06-04-main-alias-from-design.md` + `2026-06-04-insert-select-join-design.md`
- plan：`docs/superpowers/plans/2026-06-04-*.md`
- 主 spec Round 3 占位段更新指向 Round 3b 已交付

### 兼容性

- 不破坏 v0.8.x 既有 API；GORM 版本锁定保持 v1.31.x
- `v0.8.x` tag 不受影响；下游 `go get @v0.9.0` 获得上述新增 API

---

## [0.8.3] - 2026-05-08

### 支持版本与兼容性

- **DM 8 Oracle 兼容模式**：v0.8.0 alias 体系 + Repository CRUD 在 DM 8 下行为锁定
  - **`COMPATIBLE_MODE=2` 必须显式开启**：docker run 加 `-e COMPATIBLE_MODE=2`，`SELECT PARA_VALUE FROM V$DM_INI WHERE PARA_NAME='COMPATIBLE_MODE'` 验证应返回 2
  - **DM 7 及更老版本不支持**：sequence + trigger 自增、ROWNUM 重写未实现（参见 TD-17）
  - **未验证场景**：国密加密 / Kerberos / DSC 集群 / DM MySQL/PG/TD 兼容模式（v0.8.4+ 候选）

### 已知限制（DM）

DM 8 Oracle 兼容模式继承 v0.8.2 Oracle 大部分限制：
- `''` = NULL（影响 IsNull / Empty 判断）
- 输出列名 UPPERCASE（RawScan 需 SQL 显式 `AS "col"` 锁定 lowercase）
- CLOB/TEXT WHERE 限制（string 字段须 `gorm:"size:N"` 约束）
- NULLS LAST 默认（升序 NULL 排末尾）
- RETURNING 仅支持单行（SaveBatch/UpsertBatch 走 RETURNING 路径需 t.Skip）
- 标识符长度上限（DM 8 实际 128，保留 ≤30 字符规范以兼容 Oracle 12c R1）
- ON CONFLICT 不支持（DM 用 `MERGE INTO`）

**与 Oracle 不同的关键差异（v0.8.3 实施期实测推翻 spec 早期假设）**：

- **`getQuoteChar` 返回双引号（不与 Oracle 共用空 quoter）**：godoes/gorm-dameng v0.7.2 migrator 实测用 `CREATE TABLE "my_sql_users" ("username" VARCHAR(64),...)` 引号 lowercase 建表，列名在 DM 中存为 case-sensitive 小写。DM CASE_SENSITIVE=Y + Oracle 兼容下裸标识符会被 UPPERCASE 解析为 `USERNAME`，触发 `Error -2111 无效的列名`，必须用双引号锁定小写匹配。dm 方言归入 `case "postgres", "sqlite", "dm"` 共用双引号 quoter。

DM 特有：
- **镜像默认密码版本差异**：dameng 镜像历史上 SYSDBA / SYSDBA001 / 自定义都见过，部分版本首登强制改密——以拉到的镜像 README 为准
- **Docker Hub 第三方镜像版本不保证**：主路径用 dameng 技术社区 tar + `docker load`（v0.8.3 实施期因社区下载受阻改走自构建 install.xml；下游可任选）

### 新增（DM 8 支持）

- **DM 数据库支持**：v0.8.0 alias 体系 + Repository CRUD 在 DM 8 Oracle 兼容模式下行为锁定
  - GORM Dialector：`github.com/godoes/gorm-dameng v0.7.2`（2025-08-22 release，与 v0.8.2 godoes/gorm-oracle 同作者）
  - Go 驱动：godoes/gorm-dameng **内置**（子包 `dm8/i18n` / `parser` / `security` / `util`，纯 Go 无 cgo，**不依赖 gitee.com/chunanyong/dm**——推翻 spec 早期假设）
  - **测试隔离**：`//go:build dm` build tag，**不进 CI**（DM 镜像同样大、license 复杂）
  - 跑测命令：`go test -tags=dm ./...`，需启动本地 docker DM 8 实例
  - **强制不漏跑**：`TEST_DM_REQUIRED=1 go test -tags=dm ./...`（DSN 不通时 t.Fatalf 而非 t.Skip）
- 新建 4 个 dm build-tag 测试文件：
  - `dm_setup_test.go`：`setupDMDB` helper + `truncateDMTables`（DROP TABLE PURGE + AutoMigrate 沿用 Oracle 决策）
  - `dm_contract_test.go`：Dialector 契约（`db.Name() == "dm"` + `getQuoteChar` 返回双引号）
  - `dm_integration_test.go`：5 个 CRUD 测试（BasicCRUD / Where / OrderGroupHaving / JoinQuery / QuoteColumn）
  - `alias_dm_test.go`：3 个 alias 体系测试（自连接 / alias 字段 q.Eq / correlated EXISTS）

### 文档

- README 方言矩阵加 DM
- README 已知方言差异速查加 DM 限制（双引号 quoter / 继承 Oracle / 镜像密码差异）
- README 新增 "DM 数据库支持" 章节（Quickstart / TEST_DM_DSN BNF / 下游生产侧集成 / quoter 策略与列名匹配 / 保留字对照表 / 错误码导航 / COMPATIBLE_MODE 诊断 SQL / 未验证场景兜底）
- README GOPROXY 配置提示（一般性建议，非 DM 特定——driver 自带不依赖 gitee）
- spec：`docs/superpowers/specs/2026-05-08-dm-support-design.md`（经过 brainstorming + 2 轮 6 专家审计 + 14 必修修订 + 13 待定项 + 7 README 缺口）
- plan：`docs/superpowers/plans/2026-05-08-dm-support-plan.md`（5 task / 5 commit + Task 0 待定项探测 + 实测值写回）

### 库代码改动

- **`builder.go: getQuoteChar`** 加 `dm` 到 `case "postgres", "sqlite", "dm":` 共用双引号 quoter——**唯一库代码（非测试）改动**
  - 实施期决策路径：commit `01cbddc` 按 spec §4.1 把 dm 与 oracle 合并 case "oracle", "dm" 共用空 quoter；实测发现 dameng migrator 用引号 lowercase 建表导致裸列名 case-sensitive 不匹配，commit `92108eb` 修正——dm 单独归入双引号 quoter 与 postgres/sqlite 共用，oracle 仍保留独立空 quoter case
  - 既有 `TestGetQuoteChar_Dialects` 加 dm 子测试覆盖（用 testMockDialector 模拟，避免默认 build 引入 driver）
- `TestQuoteColumn_Dialects` 不动（与既有方言一致——表驱动直接喂 quoter 字符不走 dialect 分支）

### 技术债

- **TD-15**：DM 测试无 CI 守护，依赖下游手动跑发现问题
- **TD-16**：第三方 Dialector 维护风险（gorm-dameng 由社区维护，GORM 升级时可能滞后）
- **TD-17**：DM 7 不支持（sequence + trigger 自增、ROWNUM 重写未实现）
- **TD-18**：DM MySQL/PG/TD 兼容模式不支持（v0.8.3 仅验证 Oracle 兼容；切到 MySQL 兼容需重测 quoter 策略——dameng migrator 在不同兼容模式下大小写策略可能不同）
- **TD-19**：dameng migrator 大小写策略与 Oracle migrator 不同（引号 lowercase vs UPPERCASE 不带引号）属第三方 driver 内部实现，未来 driver 升级可能改变策略——dm_contract_test.go 锁定 `getQuoteChar` 返回双引号契约作为变更预警

复用既有 TD：
- **TD-12**（单模块带可选 driver）：gorm-dameng 拉到 transitive
- **TD-13**（批量 RETURNING 适配）：DM 也不解决，推到 v0.9+
- **TD-14**（保留字列名自动加引号）：在 DM 下行为完全一致

### 收尾说明

仅测试基建 + 文档变更（除 `getQuoteChar` 一处分支扩展外），不涉及核心 API、Repository CRUD、alias 体系；GORM 版本锁定保持 v1.31.x；`v0.8.0` / `v0.8.1` / `v0.8.2` tag 不受影响。

下一步候选（v0.8.4）：DM MySQL 兼容模式（与 gplus 已有 mysql 路径冲突需重测 quoter）。
更远（v0.9+）：人大金仓 KingbaseES（信创第二大户，PG 兼容模式）/ 批量 RETURNING 适配（解 TD-13）/ 保留字列名自动加引号（解 TD-14）。

### 已知部署陷阱（post-tag 文档补丁，v0.8.3 tag 不变）

- **WSL2 用户：dm 容器跟随 distro idle stop 被 SIGTERM**：v0.8.3 实施期实测发现，WSL2 distro 在"无 wsl.exe 进程 attached"时约 60 秒后 auto stop，dockerd 收 SIGTERM 拖死所有容器（DM 走完整 graceful shutdown）。
- **vmIdleTimeout 路径实测无效（首次 commit 571696b 误判，本次 commit 修订）**：尝试过 `vmIdleTimeout=-1` 与 `vmIdleTimeout=4294967295` 在 WSL 2.6.3.0 下均不起作用（distro 仍按约 60 秒 idle stop）；distro 内 systemd `sleep infinity` keep-alive service 也无效（distro 内进程不影响 lifecycle 判断）。
- **实测有效的 workaround**：Windows 主机持续有一个 wsl.exe 进程 attached——三种方案：A. 保持终端 attached / B. PowerShell `Start-Process wsl ... sleep infinity -WindowStyle Hidden` 后台 / C. Windows 任务计划开机自动跑。详见 README "启动 DM 8 容器" 段。
- 此条目仅文档补丁，v0.8.3 git tag 指向不变，下游 `go get @v0.8.3` 行为不受影响。

---

## [0.8.2] - 2026-05-08

### 新增 (Oracle 12c+ 支持)

- **Oracle 数据库支持**：v0.8.0 alias 体系 + Repository CRUD 在 Oracle 12c+ 下行为锁定
  - GORM Dialector：`github.com/godoes/gorm-oracle v1.6.18`（社区维护，活跃 2024+）
  - Go 驱动：`github.com/sijms/go-ora/v2 v2.9.0`（纯 Go，无 Oracle Client C 库依赖）
  - **测试隔离**：`//go:build oracle` build tag，**不进 CI**（Oracle 启动慢，docker 镜像 ~3GB）
  - 跑测命令：`go test -tags=oracle ./...`，需启动本地 docker `gvenzl/oracle-free:23-slim`
- 新建 4 个 oracle build-tag 测试文件（commit `c2729e9` / `58722c1` / `9914402` / `7ddc452`）：
  - `oracle_setup_test.go`：`setupOracleDB` helper + `truncateOracleTables`（DROP TABLE PURGE + AutoMigrate 策略，避免 Oracle 回收站积压）
  - `oracle_contract_test.go`：Dialector 契约断言（`db.Name() == "oracle"` + `getQuoteChar` 返回空 quoter）
  - `oracle_integration_test.go`：5 个 CRUD 测试（BasicCRUD / Where / OrderGroupHaving / JoinQuery / QuoteColumn）
  - `alias_oracle_test.go`：3 个 alias 体系测试（自连接 / alias 字段 q.Eq / correlated EXISTS）

### 库代码改动

- **`builder.go: getQuoteChar`** 加 `case "oracle":` 分支返回**空 quoter**（commit `58722c1` + 实测修订 `7627ea6`）——**唯一库代码改动**
  - 实测修订原因：godoes/gorm-oracle migrator 用 UPPERCASE 不带引号 CREATE TABLE（列名实际存为 `USERNAME`），若 quoteColumn 加双引号转义会变 `"username"` 触发 ORA-00904 invalid identifier（双引号下大小写敏感）
  - 解决策略：oracle 分支独立返回 `"", ""`，让 Oracle 自身 UPPERCASE 解析裸标识符
  - 已知 trade-off：列名是 Oracle 保留字（`order` / `size` / `level` 等）时需用户手动用 RawSQL 加引号
- 既有 `TestGetQuoteChar_Dialects` / `TestQuoteColumn_Dialects` 加 oracle case 覆盖（用 testMockDialector 模拟，避免默认 build 引入 driver）

### 已知限制 (Oracle)

- **`getQuoteChar` 返回空 quoter**：与 PG/SQLite 双引号策略不同，避免 ORA-00904；列名是保留字时需手动加引号
- **`''` = NULL**：Oracle 自动把空字符串转 NULL，影响 IsNull / Empty 判断
- **输出列名默认 UPPERCASE**：RawScan 映射小写 struct tag 时需 SQL 显式 `AS "col"` 锁定 lowercase（参见 `TestOracle_OrderGroupHaving/GroupBy_Having_RawScan`）
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
- **TD-14**：Oracle 保留字列名（order/size/level 等）gplus 不会自动加引号——需用户手动用 RawSQL；空 quoter 策略的副作用

### 文档

- README 方言矩阵加 Oracle（标注 build tag 跑法）
- README 已知方言差异速查加 Oracle 限制（quoter 策略 / `''` / UPPERCASE / CLOB / NULLS LAST / RETURNING / 长度 / ON CONFLICT）
- spec：`docs/superpowers/specs/2026-05-07-oracle-support-design.md`（经过 brainstorming + 2 轮 4 专家审计）
- plan：`docs/superpowers/plans/2026-05-07-oracle-support-plan.md`（5 task / 27 step）

仅测试基建 + 文档变更（除 `getQuoteChar` 一处分支扩展外），不涉及核心 API、Repository CRUD、alias 体系；GORM 版本锁定保持 v1.31.x；`v0.8.0` / `v0.8.1` tag 不受影响。

下一步候选（v0.8.3）：达梦数据库 dm 支持（兼容 Oracle 模式，框架 80% 复用）

---

## [0.8.1] - 2026-05-07

### 新增 (PG 三方言验证)

- **CI 加 PostgreSQL 16 service container**：CI 现在跑 sqlite + MySQL 8.0 + PostgreSQL 16 三方言测试，新增 `TEST_PG_DSN` env，`go.mod` 加 `gorm.io/driver/postgres v1.6.0` 依赖（commit `d4ef749` + `acd27da`）
- **alias 体系 PG 行为锁定**（`alias_pg_test.go`）：3 个核心 SQL 生成测试验证 v0.8.0 alias 在 PG 方言下正确（commit `1a88c07` + `d732a1e`）
  - `TestPG_AliasSelfJoin_LeftJoinAs`：自连接 + `LEFT JOIN` + 双引号转义
  - `TestPG_AliasField_InQEq`：`q.Eq(&o.Amount)` → `"o"."amount"`
  - `TestPG_SubQuery_OuterRef_LiteralsRendered`：correlated EXISTS + 字面量内联
- **PG 集成测试 CRUD 全覆盖**（`pg_integration_test.go`）：5 个 PG-specific 集成测试镜像 MySQL 测试，验证 CRUD / Where / Order / Group / Having / Join / quoteColumn 在 PG 方言下行为一致（commit `27c6d37`）
- **结论**：v0.8.0 alias 体系 + Repository CRUD 在 PG 下**无库代码层面 bug**；`getQuoteChar` 已正确支持 PG 双引号；占位符 `$N` 由 GORM PG 驱动统一处理，库代码方言无关

### 修复 (测试基建)

- **MySQL 集成测试连接池泄漏导致 `Error 1040: Too many connections`**：3 处 `gorm.Open(mysql.Open)`（`setupMySQLDB` / `TestMySQL_QuoteColumn` / `openDB`）测试结束后未关闭底层 `*sql.DB`，GORM 默认 `MaxOpenConns=0`（无限制），多个 MySQL 集成测试反复 Open 后连接未释放，CI 跑到中途即触发 MySQL 8.0 默认 `max_connections=151` 上限，后续测试全部 `Skipf` 跳过。修复：抽 `applyDBPoolLimits` helper 复用（同时支持 PG），限制 `MaxOpenConns=5` / `MaxIdleConns=2` / `ConnMaxLifetime=1m`，并通过 `t.Cleanup` 关闭 `*sql.DB`（commit `fbcea7d` + `d4ef749`）
- **alias 测试断言写死 SQLite 引号风格**：`TestAliasField_InQEq_Works` 仅检查 `"o"."amount"` / `o.amount`，MySQL 反引号方言 `` `o`.`amount` `` 不匹配。改为方言无关：脱掉所有引号字符后判断含 `o.amount`（commit `0753ac3`）
- **5 处 setup helper 测试间清理仅覆盖 MySQL，PG 主键冲突**（`advanced_test` / `advanced_complex_test` / `repo_test` / `repo_datarule_byid_test` / `repo_onconflict_test`）：truncate 分支条件 `if db.Name() == "mysql"` 改为 `mysql || postgres`，避免 PG 多测试共用持久化表导致 INSERT 主键冲突（commit `c5206e1`）
- **4 处测试中潜伏的方言假设 bug**（PG 暴露）（commit `339d1b4`）：
  - `TestGORMAliasBehaviorProbe/JoinsWithArgs`：`?` 占位符断言扩展为 `?` 或 `$N`（PG 驱动占位符差异）
  - `TestComplex_QueryBuilder_MultiCondOr`：LIKE 大小写敏感差异，改用 `LikeRight("Al")` 前缀匹配（commit `35d961f` 进一步收紧到精准前缀）
  - `TestRepository_RawScan_Having` / `JoinGroupBy`：HAVING 引用 SELECT 别名 → 改为重复聚合表达式（PG 严格 SQL，MySQL/SQLite 是非标准扩展）
- **PG ON CONFLICT 裸列名歧义（SQLSTATE 42702）**：`TestInsertOnConflict_UpdateExprs` / `DoUpdatesAndExprs` 中 `score + excluded.score` 在 PG 下歧义。PG 严格语义要求裸列名 + EXCLUDED 同名时用表名限定，二元判断改三元 switch：MySQL `score + VALUES(score)` / PG `conflict_users.score + excluded.score` / SQLite `score + excluded.score`（commit `a907d0f`）

### 已知限制 (文档)

- **MySQL 1093 — UPDATE 目标表不能与子查询 FROM 同表**：`InSub / NotInSub / GtSub / LtSub / EqSub` 等 16 个 Updater 子查询方法在 MySQL 下，若子查询源表与 UPDATE 目标表相同会报 `Error 1093 (HY000): You can't specify target table 'T' for update in FROM clause`。SQLite / PostgreSQL 无此限制。README "已知陷阱" 章节新增一节，含 derived table workaround 示例（`SELECT * FROM (subq) AS t`）。`TestUpdater_GtSub_RealUpdate` 在 `db.Name() == "mysql"` 时 `t.Skip`，sqlite 路径仍覆盖语义；`TestUpdater_AllSub_DryRun/GtSub` 仍覆盖 SQL 生成（commit `0753ac3`）
- **PG ON CONFLICT 裸列名歧义**：用户在 `OnConflict.UpdateExprs` 中写自定义表达式时，PG 要求引用目标行用表名限定（如 `users.score + excluded.score`）。库代码与方言无关，约束属于用户表达式
- **HAVING 引用 SELECT 别名**：PG 严格 SQL 不允许，MySQL/SQLite 是扩展。建议统一写聚合表达式
- **LIKE 大小写敏感性**：MySQL 默认 `utf8mb4_general_ci` 不敏感、PG 默认敏感、SQLite 默认不敏感

### 文档

- README 新增"方言支持"章节，三方言支持矩阵 + 已知方言差异速查
- README "已知陷阱" 章节扩展（MySQL 1093 + derived table workaround 示例）

仅测试基建 + 文档变更，不涉及代码、API、行为；GORM 版本锁定保持 v1.31.x；`v0.8.0` tag 不受影响。

---

## [0.8.0] - 2026-05-06

### 新增

- **alias 体系**：类型安全的跨表列引用 / 同表自连接 / correlated EXISTS 子查询
  - `gplus.As[X](q, name)`：在 q 上注册 X 类型的 alias 实例
  - `gplus.NewQueryAs[T](ctx, name)`：主表起 alias 入口
  - `gplus.SubQuery[X](outer)` / `SubQueryAs[X](outer, name)`：派生子查询，支持跨层引用外层 alias
  - 7 种 JoinAs（Query）/ 2 种 JoinAs（Updater）：LeftJoinAs / RightJoinAs / InnerJoinAs / OuterJoinAs / FullJoinAs / CrossJoinAs / NaturalJoinAs
  - Exists / NotExists / OrExists / OrNotExists（Query + Updater 镜像）
- 7 个新错误哨兵：ErrAliasDuplicate / ErrAliasInvalidName / ErrFieldAddrUnregistered / ErrAliasNotInChain / ErrSubqueryOuterNil / ErrAliasQueryNil / ErrAliasRevoked
- Repository.NewQueryAs 便捷方法

### 行为约束（须知）

- **DataRule × alias 安全契约**：DataRule 仅作用主表，alias 副表用户自负责。详见 README "Alias 与跨表查询" 章节
- **DataRule.Column 不应写 alias 前缀**：v0.9 cross-table DataRule 通过新增 Table 字段提供，提前在 Column 写 alias 前缀会形成兼容性陷阱（0.10.0 `Table` 字段已正式提供）
- **JoinAs extraSQL 必须参数化**：禁止 fmt.Sprintf 拼接用户输入；占位符 `?` + extraArgs 走 GORM 参数化
- **As(q=nil) panic ErrAliasQueryNil**（API 入口编程错误）；其他错误均累积 + BuildQuery 短路（决策 1B）
- **Clear() 后 alias 实例失效**：Clear 翻转所有 alias entry 的 revoked 标记，后续使用累积 ErrAliasRevoked
- **GORM 版本锁定 v1.31.x**：升级前必须重跑 TestGORMAliasBehaviorProbe

### Deprecated

- `LeftJoin / RightJoin / InnerJoin / OuterJoin / FullJoin / CrossJoin / NaturalJoin`（Query + Updater 各 7 个）：使用对应 JoinAs 替代；v1.0 删除。仍保留用于 JOIN 子查询表 / USING 子句等 alias 不能表达的场景

### 不在本期范围

- ANY / ALL 24 方法 → v0.8.1
- SelectSub → v0.8.1（依赖 GORM Select 嵌套实测）
- 类型安全 ON extra 三元组 / 包级泛型 LeftJoinAs[L,R] → v0.9
- 跨表 DataRule（DataRule.Table 字段）→ v0.9（实际于 0.10.0 交付）
- UNION / WITH CTE / 窗口函数 → v1.0+

---

## [0.7.1] - 2026-05-01

### 修复 (文档)

- **v0.7.0「行为约束」段排查命令在嵌套括号场景漏检**：原命令中 `[^)]*` 在 `q.ToDB(r.GetDB()).Scan(&x)` 这类实参含括号的调用上匹配失败，整条 regex 在第一个 `)` 处断裂导致漏检。修正为 `.*` + 后续锚点强制回溯，支持任意嵌套深度
- 同步在 v0.7.0「行为约束」段追加「regex 启发式局限 + AST 工具兜底」警告与「regex 命中对照表」（4 行反例覆盖单层 / 嵌套 / 中间链 / 合规 Find）
- 来源：下游 gvs-server 项目落地 v0.7.0 时实测发现并验证修复
- 仅文档变更，不涉及代码、API、行为；GORM 版本锁定保持 v1.31.x

---

## [0.7.0] - 2026-05-01

### 新增

- **Query-chain-safe 投影查询 API**：根除 `db.Scan()` / `db.Row()` / `db.Rows()` 绕过 GORM Query callback chain 导致的下游隔离/审计 callback 失效问题
  - `FindAs[T, Dest, D]` / `FindAsTx[T, Dest, D]`：投影多行（dest 为 `*[]Dest`）
  - `FindOneAs[T, Dest, D]` / `FindOneAsTx[T, Dest, D]`：投影单行（dest 为 `*Dest`，无匹配返回 `gorm.ErrRecordNotFound`）
  - 内部走 `.Find` / `.First` → Query chain，下游挂在 Query chain 上的 callback 自动触发
  - Go 1.18+ 类型推导后调用形态：`gplus.FindAs(repo, q, &rows)`，无需写类型参数
- `ErrFindOneAsConflict` sentinel：FindOneAs 与 `q.Limit()/q.Page()` 组合时立即返回

### 修复

- **aggregate 路径绕过 Query callback chain**（`repository.go` 中 aggregate 函数）：Sum/Max/Min/Avg 内部 `.Scan(&ptr)` 改为 `.Find(&[]aggregateWrap[R])` 走 Query chain；下游 isolation/审计 callback 现可正确触发。NULL 语义保持不变（wrapper struct 中 `*R` 字段在 SQL NULL 下为 nil，已实测）

### 行为约束（须知）

- **`q.ToDB(db).Scan(...)` / `.Row()` / `.Rows()` 仍绕过 Query callback chain**：GORM v1.31.1 三者内部走 Row chain，gplus 无法拦截。**若下游挂有 isolation/审计 callback，这三种调用等同保留隔离漏洞，必须迁移到 `FindAs`/`FindOneAs`**。
  - 排查命令（互补两条）：
    ```bash
    # 1. 单行直链（高置信度）
    grep -rEn 'ToDB\(.*\)\.(Scan|Row|Rows)\(' . --include='*.go'
    # 2. 跨行场景（变量赋值后调用 / 中间链方法）— 需人工复查
    grep -rEn '\.ToDB\(' . --include='*.go'
    # 在结果文件中再 grep 是否有 .Scan/.Row/.Rows
    ```

    **regex 启发式的本质局限**：上述 grep 命令是行内启发式扫描，无法理解 Go AST。真正的深嵌套（同一行多次调用 ToDB / 跨行 builder pattern）仍可能漏检或误检。关键代码请人工 review，或使用 AST 工具作为兜底：
    - [ast-grep](https://ast-grep.github.io/)：结构化模式匹配，例如 `ast-grep --pattern '$Q.ToDB($$$).Scan($$$)' --lang go`
    - `golang.org/x/tools/go/analysis` 写自定义 lint analyzer，精确识别方法链
    - `go/parser` + `go/ast` 手写小工具

    **regex 命中对照表**（验证修正后的命令）：

    | 反例代码                                              | 旧 `[^)]*` | 新 `.*`  |
    |-------------------------------------------------------|-----------|----------|
    | `q.ToDB(db).Scan(&x)` (基础违规)                       | ✓ 命中    | ✓ 命中    |
    | `q.ToDB(r.GetDB()).Scan(&x)` (实参嵌套括号)            | ✗ 漏检    | ✓ 命中    |
    | `q.ToDB(r.GetDB()).WithContext(ctx).Scan(&x)` (中间链) | ✗ 漏检    | ✓ 命中    |
    | `q.ToDB(db).Model(&T{}).Find(&rows)` (Find 非违规)      | ✗ 不命中  | ✗ 不命中  |
- **新 API 不取代 `RawScan`**：Raw SQL 路径 Schema=nil，下游 isolation callback 在正确实现下短路；**若下游 callback 未做 `Schema == nil` 判断，行为不可预测**。涉及敏感数据的 Raw 查询必须在 SQL 中手写 WHERE，不可依赖 gplus DataRule 或下游 callback
- **aggregate 性能基线**：高频聚合（每秒数百次 Sum/Max/Min/Avg）下，callback chain 触发是新增主要开销（取决于下游 callback 数量与复杂度）。性能敏感场景需基准测试
- **GORM 版本锁定**：本修复基于 GORM v1.31.x 实测行为。升级到 v1.32+ 必须重跑 `TestGORMCallbackBehaviorProbe`，行为变化时第一时间感知

### 不在本期范围

- 已评估"拆 0.6.1（仅修 aggregate）+ 0.7.0（新增 API）"方案 — 因新增 API 非破坏、合并发布心智成本相同，**合并发布**

---

## [0.6.0] - 2026-04-30

### 新增

- **类型安全子查询**：消灭体系性 `WhereRaw` 子查询裂缝
  - `Subquerier` 接口（含 `gplusSubquery()` unexported guard 阻止外部冒名实现）
  - `Query[T]` 16 个新方法：`InSub` / `NotInSub` / `EqSub` / `NeSub` / `GtSub` / `GteSub` / `LtSub` / `LteSub` + 8 个 Or 变体
  - `Updater[T]` 16 个新方法（同形态）
  - 任意 `*Query[X]` 自动满足 `Subquerier`，X 可与外层 T 不同
- `ErrSubqueryNil` sentinel：`InSub(col, nil)` 时立即追加该错误
- `Query[T].SelectRaw(expr)`：原生 SQL SELECT 表达式，支持聚合函数（如 `AVG(age)`）和复杂表达式

### 修复

- `Query[T].ToDB(db)`：原本未调用 `Model(getModelInstance[T]())` 导致子查询 SQL 缺失表名
- `builder.go applyWhere`：Subquerier 子查询分支未显式聚合 `sub.GetError()`，错误经 `Session{NewDB:true}` 切断后静默丢失；现显式 `d.AddError(sub.GetError())` 传播

### 行为约束（须知）

- **延迟调用语义**：`sub` 传入 `InSub` 后仍可被修改，修改会反映到最终 SQL（与现有 `q.In(col, subQ.ToDB(db))` 一致）。godoc 推荐 sub 构建完成后再传入，传入后不要修改
- **sub.ToDB() 默认不应用 DataRule**：与 v0.5.x 既有语义保持一致；如需在子查询施加数据权限，须在传入前显式调 `sub.DataRuleBuilder()`
- **MySQL UPDATE 同表 IN 限制（ERROR 1093）**：`Updater.InSub`/`NotInSub` 在同表子查询场景下 MySQL 报错；可改写为 JOIN UPDATE 或子查询包临时表

### 测试

- 新增 `query_subquery_test.go`（~600 行）+ `updater_subquery_test.go`（~180 行）+ `subquery_test.go`（接口验证）
- 覆盖：32 方法主路径 + Or 变体 + 错误路径 + 延迟语义锁定 + DataRule 6 场景 + Session 隔离 + 嵌套子查询
- 测试覆盖率 ≥ 96.5%

### 不在本期范围

- **EXISTS / NOT EXISTS**：90% 真实场景为 correlated subquery，需 v0.7.0 alias 体系到位才能消灭关联条件 WhereRaw；提前发布会强制 v0.7.0 破坏性签名变更
- **ANY / ALL 变体**：v0.7.0 候选清单（提升优先级）
- **SELECT 子查询 / 跨表列引用 API**：需要 alias 体系，单独立项

---

## [0.5.1] - 2026-04-30

### 修复（安全）

- **DataRule 应用到 by-ID 路径**：v0.2.0 已修复 by-Cond 路径，但 7 个 by-ID 路径系统性遗漏 DataRule 应用，存在跨租户读 / 改 / 删 / 恢复风险
  - 影响方法：`GetById` / `GetByIdTx` / `GetByIds` / `GetByIdsTx` / `UpdateById` / `UpdateByIdTx` / `UpdateByIds` / `UpdateByIdsTx` / `DeleteById` / `DeleteByIdTx` / `DeleteByIds` / `DeleteByIdsTx` / `Restore` / `RestoreTx`（共 14 条调用路径）
  - 修复方式：每个方法内部构造临时 `Query[T]` 调用 `DataRuleBuilder()`，与 by-Cond 路径共享同一 DataRule 处理逻辑（单一真相源），未来新增 by-ID 写方法不会再遗漏
- **`ToUpdateSQL(nil)` 错误类型**：原返回 `ErrQueryNil`（语义属于 Query），改用 `fmt.Errorf("%w: %w", ErrUpdateEmpty, ErrQueryNil)` 双 wrap，使 `errors.Is` 对两者均返回 true，与 `Updater[T].ToSQL` 错误类型对齐同时不破坏旧调用方

### ⚠️ 行为变更（升级须知）

本次为 patch 版本但属于安全修复，存在以下可观察行为变化，建议升级前审视：

- 跨租户场景下 `affected` 由可能 >0 变成 =0；下游若有 `if affected == 0 { 报错/重试 }` 类逻辑会改变分支
- `UpdateByIdTx` 乐观锁 + DataRule 拦截当前共用 `ErrOptimisticLock`，调用方启用 DataRule 时**不应**无条件重试（重试无法绕过权限）
- 依赖 `err == ErrQueryNil` 硬比较 `ToUpdateSQL(nil)` 返回值的代码会失效；改用 `errors.Is(err, ErrQueryNil)` 即可（已双向兼容）

如生产环境业务逻辑依赖"by-ID 跨租户可读 / 改"行为（属于依赖未文档化 bug），请在升级前调整。

### 测试

- 新增 `repo_datarule_byid_test.go`（~380 行），含 3 组测试：跨租户拦截、白名单防注入、无 DataRule 零回归，共 14 个 sub-test
- 测试覆盖率 94.0% → 96.1%（+2.1pp）

---

## [0.5.0] - 2026-04-24

### 新增

- **乐观锁**：在模型字段上标注 `gplus:"version"` 即可启用，无需修改任何调用代码
  - `UpdateById` / `UpdateByIdTx` 自动追加 `WHERE version = oldVer`，SET 自动追加 `version = version + 1`
  - `affected == 0` 时返回 `ErrOptimisticLock`（版本冲突或记录不存在）
  - 更新成功后 entity.Version 自动回写为新值，可直接连续调用
  - 支持字段类型：`int` / `int32` / `int64` / `uint` / `uint32` / `uint64`
  - 支持嵌入字段中的 version（偏移量递归累加）
  - 无 version 字段的模型保持原有路径，零额外开销
- `ErrOptimisticLock`：版本冲突哨兵错误，可通过 `errors.Is` 判断

---

## [0.4.0] - 2026-04-23

### 新增

- `OnConflict` 冲突处理策略类型，支持四种模式：
  - `DoNothing`：冲突时跳过（INSERT IGNORE / DO NOTHING）
  - `DoUpdates`：冲突时按 EXCLUDED 覆盖指定列（字段指针或字符串列名）
  - `DoUpdateAll`：冲突时覆盖除主键外所有列
  - `UpdateExprs`：冲突时按自定义表达式更新（原子累加等）；可与 `DoUpdates` 组合
- `InsertOnConflict` / `InsertOnConflictTx`：单条带冲突处理的插入
- `InsertBatchOnConflict` / `InsertBatchOnConflictTx`：批量带冲突处理的插入；空切片无操作
- `ErrOnConflictInvalid`：互斥策略配置时返回的哨兵错误
- **调试支持**：`Query[T].ToSQL(db)` / `Query[T].ToCountSQL(db)` / `Updater[T].ToSQL(db)`，基于 GORM DryRun 模式输出参数已内联的 SQL，仅供调试展示
- `Repository` 提供 `ToSQL` / `ToCountSQL` / `ToUpdateSQL` 同名便捷方法，无需手动传 db
- `doc.go`：包级文档注释，便于 `go doc` / pkg.go.dev 浏览
- `example_test.go`：可执行示例（Repository / Query / Updater 基础用法）

### 修复

- `BuildCount`：`Distinct` + `Page` 时 COUNT 路径未应用 `DISTINCT` 子查询，导致 `total` 虚高
- `FirstOrUpdate`：创建后重读改用主键精确查找（通过 `gorm.Statement.Parse` 提取 `PrioritizedPrimaryField`），避免更新查询条件字段时按旧字段值找不到新记录

### 重构

- `query.go` / `update.go`：`errors.New(fmt.Sprintf(...))` 反模式替换为 `fmt.Errorf`

### 测试

- 支持 MySQL/SQLite 双模式集成测试，移除手写 SQL，方言一致性更可靠
- GROUP BY 测试补充 Select 列以兼容 MySQL 8.0 `ONLY_FULL_GROUP_BY`
- 新增回归测试：`TestPage_Distinct_Count_Consistent` / `TestFirstOrUpdate_UpdateQueryField`

---

## [0.1.0] - 2026-03-18

### 新增

**查询构建器 (`Query[T]`)**
- 全操作符支持：Eq/Ne/Gt/Gte/Lt/Lte/Like/NotLike/In/NotIn/IsNull/IsNotNull/Between
- 所有操作符对应 OR 变体（OrEq/OrLike 等）
- AND/OR 嵌套括号块（`AndGroup` / `OrGroup`）
- 7 种 Join 类型：Inner/Left/Right/Full/Cross/LeftOuter/RightOuter
- 分页（`Page`）、排序（`Order`）、分组（`Group`）、Having
- 悲观锁：FOR UPDATE / FOR SHARE / NOWAIT / SKIP LOCKED
- 软删除 + `Unscoped`
- 预加载（`Preload`）
- Select / Omit / Distinct
- 数据权限规则注入（`DataRule` + Context）
- RawQuery / RawExec / RawScan

**更新构建器 (`Updater[T]`)**
- 类型安全的 `Set(&model.Field, value)` 链式调用
- 非类型安全的 `SetMap(map[string]any)` 批量设置
- 与 Query 相同的条件构建能力

**Repository 模式 (`Repository[K, T]`)**
- 标准 CRUD：Create/Save/SaveBatch/CreateBatch/GetById/List/Count/Page
- 事务变体：所有方法均有对应 `XXXTx` 版本
- Pluck / PluckTx：提取单列值
- DeleteById / DeleteByCondTX
- GetByLock：悲观锁查询（强制要求事务）
- RawQuery / RawExec / RawScan

**基础设施**
- 两级 Schema 缓存（反射结果缓存 + 字段地址缓存），DCL 并发安全
- 错误累积机制（链式调用中积累错误，`GetError()` 统一上报）
- `DeleteByCondTX` 空条件保护（防止全表物理删除）

### 已知限制

- 不支持 UNION（需用 `RawQuery` 代替）
- 不支持批量 Update/Delete
- `SetMap` 跳过列名类型校验，类型安全性低于 `Set`

---

## [0.3.2] - 2026-04-05

### 修复

- `PluckTx` 在 GORM clause 定型前未提前应用 `Distinct`，导致 `Distinct` 标志丢失

---

## [0.3.1] - 2026-04-02

### 新增

- `Repository[K, T].NewQuery()` / `NewUpdater()`：便捷方法，无需重复指定泛型参数直接获得绑定该 Repository db 的 Query/Updater

### 修复

- `ToDB` 改用 `Session{NewDB: true}` 防止继承"脏" db 的已有条件，避免多次调用时 WHERE 子句叠加

---

## [0.3.0] - 2026-03-28

### 新增

**Repository 方法**
- `GetByIds` / `GetByIdsTx`：按主键列表批量查询
- `DeleteByIds` / `DeleteByIdsTx`：按主键列表批量删除
- `UpdateByIds` / `UpdateByIdsTx`：按主键列表批量更新，返回 `(affected, err)`
- `Exists` / `ExistsTx`：存在性检查，返回 `(bool, error)`
- `Sum` / `Max` / `Min` / `Avg`（含 Tx 变体）：聚合函数，NULL 安全
- `Chunk` / `ChunkTx`：主键游标分批处理，每批回调 `fn([]T) error`
- `FirstOrCreate`：原子查找或创建，返回 `(T, created bool, error)`
- `FirstOrUpdate`：原子查找或创建并更新，返回 `(T, created bool, error)`
- `ListMap` / `ListMapTx`：查询结果按 `keyFn` 转换为 `map[D]T`
- `Restore` / `RestoreTx`：按主键恢复软删除记录，返回 `(affected, err)`
- `RestoreByCond` / `RestoreByCondTx`：按条件批量恢复软删除（空条件返回 `ErrRestoreEmpty`）
- `IncrBy` / `IncrByTx` / `DecrBy` / `DecrByTx`：原子字段自增自减，返回 `(affected, err)`
- `Last` / `LastTx`：按主键倒序取第一条记录
- `IsEmpty()`：判断 Query/Updater 是否无任何条件（`WithScope` 不计入）

**Query / Updater**
- `WithScope(fn func(*gorm.DB) *gorm.DB)`：向 Query/Updater 注入自定义 GORM scope

**错误变量**
- `ErrDefaultsNil`：`FirstOrCreate` / `FirstOrUpdate` 传入 nil defaults 时返回
- `ErrRestoreEmpty`：`RestoreByCond` / `RestoreByCondTx` 无条件时返回

### 重构

- `GetError()` 摘要改用 `errors.New`，移除无占位符的 `fmt.Errorf`

---

## [0.2.1] - 2026-03-20

### 修复

- `applyGroupHaving`：`OrHaving` 条件被错误追加到 WHERE 而非 HAVING；`HavingGroup` OR 嵌套分组未正确构建 clause 树
- `Query[T].Clear()`：未重置 `errs` 和 `dataRuleApplied`，复用同一 Query 实例时状态泄漏
- `DataRule.Column`：缺少白名单正则校验，含括号/运算符的恶意表达式可绕过 `quoteColumn` 转义

### 重构

- 无占位符的 `fmt.Errorf` 替换为 `errors.New`；删除 Go 1.24 中已无必要的循环变量捕获

### 测试

- 覆盖率从 93.3% 提升至 94.0%
- 新增 `TestQuery_SQL` 综合 DryRun SQL 验证（20 个子测试）
- 补充 `Omit` / `HavingGroup` / `OrWhereRaw` / `CrossJoin` / `Query.Clear` 覆盖缺口

---

## [0.2.0] - 2026-03-19

### 新增

- `Upsert` / `UpsertTx`：单条 insert-or-update，底层调用 GORM `db.Save()`
- `UpsertBatch` / `UpsertBatchTx`：批量 insert-or-update
- `WhereRaw` / `OrWhereRaw`：`Query[T]` 和 `Updater[T]` 支持原生 SQL 条件片段
- `OrderRaw`：支持复杂原生 ORDER BY 表达式（FIELD/CASE WHEN/NULLS LAST 等），与 `Order` 混用时保留调用顺序
- `Updater[T].DataRuleBuilder()`：数据权限规则同步支持 UPDATE 操作
- `CreateBatchTx` 新增 `batchSize <= 0` 参数校验

### 修复（安全）

- `DeleteByCondTx` / `UpdateByCondTx` 未应用 DataRule，导致数据权限对写操作完全不生效（**安全漏洞**）

### 修复

- `Updater.Clear()` 保留 backing array 导致复用时旧错误残留
- `buildLeafSQL` 多参数 `WhereRaw` 展开错误，导致参数绑定失效

### 破坏性变更

- `UpdateByCondTX` 重命名为 `UpdateByCondTx`，统一 Tx 后缀大小写规范
- `DeleteByCondTX` 重命名为 `DeleteByCondTx`，统一 Tx 后缀大小写规范
- 所有内部错误信息由中文改为英文

### 文档

- `Save` / `SaveBatch` godoc 明确标注为纯 INSERT（非 upsert）
- `Page` / `PageTx` 补充 `skipCount=true` 时 `total` 恒为 0 的说明
- `RegisterModel` 补充并发使用时序警告
- `JoinOuter` / `OuterJoin` 标注非标准 SQL 警告
- README 修正错误示例代码，补充 Upsert/WhereRaw/OrderRaw 使用说明
