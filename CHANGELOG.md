# Changelog

所有版本变更记录遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/) 格式，版本号遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)。

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
