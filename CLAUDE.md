# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 命令

> 本机 Go 二进制完整路径：`D:/Environment/golang/go1.21.11/bin/go.exe`（会自动下载 go1.24 toolchain）

```bash
# 运行所有测试
D:/Environment/golang/go1.21.11/bin/go.exe test ./...

# 运行指定测试函数
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestRepository_CRUD_And_Errors ./...

# 运行指定子测试
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestAdvanced_Features/SoftDelete_And_Unscoped ./...

# 以详细模式运行（通过 GORM logger 显示 SQL 日志）
D:/Environment/golang/go1.21.11/bin/go.exe test -v ./...

# 查看测试覆盖率
D:/Environment/golang/go1.21.11/bin/go.exe test -coverprofile=coverage.out ./... && D:/Environment/golang/go1.21.11/bin/go.exe tool cover -func=coverage.out
```

**已知的预存在测试失败**：无（所有测试均通过，覆盖率 94.0%）

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

嵌入在 `Query[T]` 和 `Updater[T]` 中的共享基础结构。保存条件、select、join、排序、分组、having、预加载、锁配置。提供四种构建路径：

- `BuildQuery()` — 完整 SELECT（select + distinct + where + joins + group/having + order/limit + lock + preloads）
- `BuildCount()` — COUNT 路径（仅 where + joins + group/having，无 select/order/limit）
- `BuildUpdate()` — UPDATE 路径（where + joins + selects 用于列限制）
- `BuildDelete()` — DELETE 路径（仅 where）

`quoteColumn` 处理方言感知的转义：检测 `()+-*/, ` 以跳过复杂表达式，递归处理 `table.col` 和 `col AS alias` 模式。

### `DataRuleBuilder`（query.go）

从 `ctx.Value(DataRuleKey)` 读取 `[]DataRule` 并将条件追加到查询中。由 `dataRuleApplied bool` 保护——对同一 `Query` 多次调用是安全的（幂等）。始终以 `q.DataRuleBuilder().BuildQuery()` 方式调用，在 repository 方法中不要直接调用 `q.BuildQuery()`。

`DataRule.Column` 须匹配白名单正则（字母/数字/下划线/点），含括号或运算符的表达式会被拒绝。

### 错误处理模式

`Query[T]` 和 `Updater[T]` 在链式调用过程中将错误累积到 `errs []error` 切片中（例如 `resolveColumnName` 失败时）。`GetError()` 返回带有摘要前缀的合并错误（`"gplus query builder failed with N errors"` / `"gplus updater failed with N errors"`）。Repository 方法会提前调用 `GetError()` 并在失败时立即返回。

### Repository API 关键签名

```go
// D = 主键类型，T = 模型类型
repo := gplus.NewRepository[uint, User](db)

// 写操作
repo.Save(ctx, &user)                          // 纯 INSERT（非 upsert）
repo.SaveBatch(ctx, users)                     // 批量 INSERT
repo.Upsert(ctx, &user)                        // insert-or-update（按主键）
repo.UpsertBatch(ctx, users)                   // 批量 upsert
repo.CreateBatch(ctx, ptrs, batchSize)         // 分批 INSERT，指定批大小
repo.UpdateById(ctx, &user)                    // 按主键更新非零字段
repo.UpdateByIds(ctx, ids, updater)            // 按主键列表批量更新，返回 (affected, err)
repo.UpdateByCond(updater)                     // 按条件批量更新，返回 (affected, err)
repo.IncrBy(updater, col, delta)               // 原子自增，返回 (affected, err)
repo.DecrBy(updater, col, delta)               // 原子自减，返回 (affected, err)
repo.DeleteById(ctx, id)                       // 按主键删除，返回 (affected, err)
repo.DeleteByIds(ctx, ids)                     // 按主键列表批量删除，返回 (affected, err)
repo.DeleteByCondTX(ctx, q, tx)               // 按条件删除（无条件时需 q.Unscoped()，否则返回 ErrDeleteEmpty）
repo.InsertOnConflict(ctx, &user, oc)          // 单条带冲突处理插入（DoNothing/DoUpdates/DoUpdateAll/UpdateExprs）
repo.InsertBatchOnConflict(ctx, users, oc)     // 批量带冲突处理插入，空切片无操作
repo.Restore(ctx, id)                          // 按主键恢复软删除，返回 (affected, err)
repo.RestoreByCond(q)                          // 按条件批量恢复软删除（空条件返回 ErrRestoreEmpty）

// 读操作
repo.GetById(ctx, id)                          // 按主键查单条
repo.GetByIds(ctx, ids)                        // 按主键列表批量查询
repo.GetOne(q)                                 // 按条件查单条
repo.Last(q)                                   // 按主键倒序取第一条
repo.List(q)                                   // 查列表
repo.ListMap(q, keyFn)                         // 查列表并转换为 map[D]T
repo.Page(q, skipCount)                        // 分页：返回 (list, total, err)，skipCount=true 跳过 COUNT
repo.Count(q)                                  // 按条件计数
repo.Exists(q)                                 // 按条件判断是否存在
repo.GetByLock(ctx, q, tx)                     // 加锁查询，需在事务中调用
repo.FirstOrCreate(q, defaults)               // 查找或创建，返回 (data, created, err)
repo.FirstOrUpdate(q, updater, defaults)      // 查找或创建并更新，返回 (data, created, err)
repo.Chunk(q, batchSize, fn)                   // 主键游标分批处理

// 包级泛型函数（需显式传 repo）
gplus.Pluck[T, R, D](r, q, col)               // 查询单列，返回 []R
gplus.Sum[T, R, D](r, q, col)                 // SUM 聚合，NULL 安全
gplus.Max[T, R, D](r, q, col)                 // MAX 聚合，NULL 安全
gplus.Min[T, R, D](r, q, col)                 // MIN 聚合，NULL 安全
gplus.Avg[T, R, D](r, q, col)                 // AVG 聚合，NULL 安全

// 原生 SQL
repo.RawQuery(ctx, sql, args...)              // 原生查询，返回 []T
repo.RawExec(ctx, sql, args...)               // 原生执行，返回 (affected, err)
repo.RawScan(ctx, dest, sql, args...)         // 原生查询映射到自定义结构

// Query/Updater 新增方法
q.WithScope(fn func(*gorm.DB)*gorm.DB)        // 注入自定义 GORM scope
q.IsEmpty()                                    // 判断是否无条件（WithScope 不计入）
```

### Repository 错误变量

| 变量 | 含义 |
|---|---|
| `ErrQueryNil` | 传入了 nil 的 Query/Updater |
| `ErrRawSQLEmpty` | 传入 `RawQuery`/`RawExec`/`RawScan` 的字符串为空 |
| `ErrDeleteEmpty` | `DeleteByCondTX` 在无条件且非 Unscoped 时被调用 |
| `ErrUpdateEmpty` | `Update` 被调用时 `setMap` 中没有字段 |
| `ErrUpdateNoCondition` | `Update` 有字段但没有 WHERE 条件时被调用 |
| `ErrTransactionReq` | `GetByLock` 在没有事务的情况下被调用 |
| `ErrDefaultsNil` | `FirstOrCreate`/`FirstOrUpdate` 传入 nil defaults |
| `ErrRestoreEmpty` | `RestoreByCond`/`RestoreByCondTx` 在无条件时被调用 |
| `ErrOnConflictInvalid` | `OnConflict` 中 DoNothing/DoUpdateAll/DoUpdates 互斥策略同时设置 |

### 测试辅助工具（`model_test.go`）

在所有测试文件中共享（同属 `package gplus`，可访问未导出符号如 `quoteColumn` 和 `resolveColumnName`）：
- `TestUser` 结构体，嵌入了 `BaseUser` — 用于 schema/query 测试
- `assertEqual(t, expected, actual, msg)` / `assertError(t, err, expectError, msg)` — 标准断言辅助函数
- `setupTestDB[T](t)` 在 `repo_test.go` 中 — 创建内存 SQLite DB 并自动迁移 `T`
- `setupAdvancedDB(t)` 在 `advanced_test.go` 中 — 创建包含 `UserWithDelete` + `Order` 的 DB，用于 join/preload/软删除测试