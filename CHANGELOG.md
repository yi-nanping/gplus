# Changelog

所有版本变更记录遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/) 格式，版本号遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)。

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
