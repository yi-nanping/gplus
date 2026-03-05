# CLAUDE.md

本文件为 Claude Code（claude.ai/code）在该仓库中工作时提供指导。

## 命令

```bash
# 运行所有测试
go test ./...

# 运行指定测试函数
go test -run TestRepository_CRUD_And_Errors ./...

# 运行指定子测试
go test -run TestAdvanced_Features/SoftDelete_And_Unscoped ./...

# 以详细模式运行（通过 GORM logger 显示 SQL 日志）
go test -v ./...
```

**已知的预存在测试失败**（未经明确要求不要修复）：
- `TestBuilder_QuoteColumn/带别名(AS)` — `quoteColumn` 检测到空格时提前退出，因此 `"users.name AS u_name"` 不会被处理
- `TestBuilder_QuoteColumn/表名.*` — `*` 在提前退出的特殊字符列表中

## 架构

### 核心数据流

```
用户代码
  → NewQuery[T](ctx) / NewUpdater[T](ctx)    (query.go / update.go)
      → getModelInstance[T]()                  (schema.go)  ← 首次调用时触发 registerModel
  → q.Eq(&model.Field, val).Order(...)        (query.go / update.go)
  → repo.List(q) / repo.Update(u, tx)         (repository.go)
      → q.DataRuleBuilder().BuildQuery()       (query.go → builder.go)
          → ScopeBuilder.applyWhere/Joins/...  (builder.go)
      → GORM 执行
```

### 两级 Schema 缓存（`schema.go` + `utils.go`）

这是实现类型安全字段指针的核心机制：

1. `utils.go / reflectStructSchema` — 通过反射解析结构体类型 → `map[字段偏移量 → 列名]`，以类型字符串为键缓存在 `columnCache` 中
2. `schema.go / registerModel` — 接收具体的 `*T` 实例，遍历 `reflectStructSchema`，将 `基地址 + 偏移量 → 绝对字段地址`，存入 `columnNameCache (sync.Map)`
3. `schema.go / getModelInstance[T]` — 返回类型 `T` 的**规范缓存指针**。该指针正是步骤 2 中使用其基地址的实例。**传递给查询方法的字段指针必须来自该实例。**
4. `schema.go / resolveColumnName` — 在 `columnNameCache` 中查找绝对字段地址。也接受普通 `string` 作为原始列名。

**关键不变量**：`NewQuery[T]` 和 `NewUpdater[T]` 均返回 `(builder, *T)`。`*T` 是已注册的单例。所有字段地址参数（`&model.Name`）必须来自该返回的指针，而不是另外创建的结构体。

### `ScopeBuilder`（builder.go）

嵌入在 `Query[T]` 和 `Updater[T]` 中的共享基础结构。保存条件、select、join、排序、分组、having、预加载、锁配置。提供三种构建路径：

- `BuildQuery()` — 完整 SELECT（select + distinct + where + joins + group/having + order/limit + lock + preloads）
- `BuildCount()` — COUNT 路径（仅 where + joins + group/having，无 select/order/limit）
- `BuildUpdate()` — UPDATE 路径（where + joins + selects 用于列限制）
- `BuildDelete()` — DELETE 路径（仅 where）

`quoteColumn` 处理方言感知的转义：检测 `()+-*/, ` 以跳过复杂表达式，递归处理 `table.col` 和 `col AS alias` 模式。

### `DataRuleBuilder`（query.go）

从 `ctx.Value(DataRuleKey)` 读取 `[]DataRule` 并将条件追加到查询中。由 `dataRuleApplied bool` 保护——对同一 `Query` 多次调用是安全的（幂等）。始终以 `q.DataRuleBuilder().BuildQuery()` 方式调用，在 repository 方法中不要直接调用 `q.BuildQuery()`。

### 错误处理模式

`Query[T]` 和 `Updater[T]` 在链式调用过程中将错误累积到 `errs []error` 切片中（例如 `resolveColumnName` 失败时）。`GetError()` 返回带有摘要前缀的合并错误（`"gplus query builder failed with N errors"` / `"gplus updater failed with N errors"`）。Repository 方法会提前调用 `GetError()` 并在失败时立即返回。

### Repository 错误变量

| 变量 | 含义 |
|---|---|
| `ErrQueryNil` | 传入了 nil 的 Query/Updater |
| `ErrRawSQLEmpty` | 传入 `RawQuery`/`RawExec`/`RawScan` 的字符串为空 |
| `ErrDeleteEmpty` | `DeleteByCondTX` 在无条件且非 Unscoped 时被调用 |
| `ErrUpdateEmpty` | `Update` 被调用时 `setMap` 中没有字段 |
| `ErrUpdateNoCondition` | `Update` 有字段但没有 WHERE 条件时被调用 |
| `ErrTransactionReq` | `GetByLock` 在没有事务的情况下被调用 |

### 测试辅助工具（`model_test.go`）

在所有测试文件中共享（同属 `gplus` 包）：
- `TestUser` 结构体，嵌入了 `BaseUser` — 用于 schema/query 测试
- `assertEqual(t, expected, actual, msg)` / `assertError(t, err, expectError, msg)` — 标准断言辅助函数
- `setupTestDB[T](t)` 在 `repo_test.go` 中 — 创建内存 SQLite DB 并自动迁移 `T`
- `setupAdvancedDB(t)` 在 `advanced_test.go` 中 — 创建包含 `UserWithDelete` + `Order` 的 DB，用于 join/preload/软删除测试

所有测试在包内运行（`package gplus`），可访问未导出的符号，如 `quoteColumn` 和 `resolveColumnName`。
