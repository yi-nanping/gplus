# Changelog

所有版本变更记录遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/) 格式，版本号遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)。

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
    grep -rEn 'ToDB\([^)]*\)\.(Scan|Row|Rows)\(' . --include='*.go'
    # 2. 跨行场景（变量赋值后调用 / 中间链方法）— 需人工复查
    grep -rEn '\.ToDB\(' . --include='*.go'
    # 在结果文件中再 grep 是否有 .Scan/.Row/.Rows
    ```
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
