# Spec: 类型安全子查询（v0.6.0 minor）

**日期**：2026-04-26
**版本**：v0.6.0（minor，新增 API）
**类型**：新功能

## 背景与问题

gplus 当前所有条件构建器（`Query[T]` / `Updater[T]`）都基于字段指针实现类型安全。但**子查询（IN / EXISTS / 标量比较）只能走 `WhereRaw`**：

```go
// 当前：只能用 WhereRaw 字符串拼接
q.WhereRaw("age > (SELECT AVG(age) FROM users)")
q.WhereRaw("id IN (SELECT user_id FROM orders WHERE status = ?)", "paid")
```

这与 gplus "类型安全字段指针" 的核心卖点直接矛盾。`advanced_complex_test.go` 中已有多个用例靠 `WhereRaw` 绕过，存在体系性裂缝。

## 关键发现

`builder.go:326-339` 的 `applyWhere` 已经识别 `cond.value` 为 `*gorm.DB` 类型并按子查询处理：

```go
if subQuery, ok := cond.value.(*gorm.DB); ok {
    quotedCol := quoteColumn(cond.expr, qL, qR)
    sqlStr := fmt.Sprintf("%s %s (?)", quotedCol, cond.operator)
    if cond.isOr {
        d = d.Or(sqlStr, subQuery)
    } else {
        d = d.Where(sqlStr, subQuery)
    }
    continue
}
```

**底层机制已存在**，本期只需：
1. 用户层 20 个类型安全包装方法（Query 和 Updater 各 20，共 40 个方法体）
2. EXISTS / NOT EXISTS 加新 op 分支（无列名特化）

## 设计

### Subquerier 接口

```go
// gplus 包级新增类型，*Query[X] 自动满足
type Subquerier interface {
    ToDB(db *gorm.DB) *gorm.DB
    GetError() error
}
```

- 任意 `*Query[X]`（X 可与外层 T 不同）即可作为参数
- 提取 `sub.ToDB(db)` 拿 `*gorm.DB`，传给 `addCond` 作为 value
- `sub.GetError()` 错误链式追加到外层 errs（在子查询方法入口处一次性提取）

### API 表

每个操作符自动配套 `Or` 前缀变体。

| 类别 | 方法 | SQL 形态 |
|---|---|---|
| 集合 | `InSub(col, sub)` / `NotInSub(col, sub)` | `col IN (subquery)` / `col NOT IN (subquery)` |
| 标量 | `EqSub(col, sub)` / `NeSub(col, sub)` | `col = (subquery)` / `col <> (subquery)` |
| 标量 | `GtSub(col, sub)` / `GteSub(col, sub)` | `col > (subquery)` / `col >= (subquery)` |
| 标量 | `LtSub(col, sub)` / `LteSub(col, sub)` | `col < (subquery)` / `col <= (subquery)` |
| 存在 | `ExistsSub(sub)` / `NotExistsSub(sub)` | `EXISTS (subquery)` / `NOT EXISTS (subquery)`（无 col） |

签名示例：

```go
// query.go
func (q *Query[T]) InSub(col any, sub Subquerier) *Query[T]
func (q *Query[T]) OrInSub(col any, sub Subquerier) *Query[T]
func (q *Query[T]) ExistsSub(sub Subquerier) *Query[T]
func (q *Query[T]) OrExistsSub(sub Subquerier) *Query[T]

// update.go（同形态）
func (u *Updater[T]) InSub(col any, sub Subquerier) *Updater[T]
// ... 共 20 个 Updater 方法
```

总计：Query 20 个 + Updater 20 个 = **40 个方法**。

### 使用示例

```go
// 1. WHERE id IN (SELECT user_id FROM orders WHERE status='paid')
paidUserIDs := gplus.NewQuery[Order](ctx, db).
    Select(&order.UserID).
    Eq(&order.Status, "paid")

q := gplus.NewQuery[User](ctx, db).
    InSub(&user.ID, paidUserIDs).
    Order(&user.CreatedAt, false)

// 2. WHERE age > (SELECT AVG(age) FROM users WHERE deleted_at IS NULL)
avgAge := gplus.NewQuery[User](ctx, db).
    SelectRaw("AVG(age)")  // 标量子查询用 SelectRaw 兜底，本期不引入聚合 select 语法糖

q := gplus.NewQuery[User](ctx, db).GtSub(&user.Age, avgAge)

// 3. UPDATE users SET status='inactive' WHERE id IN (SELECT user_id FROM orders WHERE last_order < '2024-01-01')
inactiveOrders := gplus.NewQuery[Order](ctx, db).
    Select(&order.UserID).
    Lt(&order.LastOrderAt, cutoff)

u := gplus.NewUpdater[User](ctx, db).
    Set(&user.Status, "inactive").
    InSub(&user.ID, inactiveOrders)
repo.UpdateByCond(u)

// 4. WHERE EXISTS (SELECT 1 FROM orders WHERE orders.user_id = users.id)
hasOrders := gplus.NewQuery[Order](ctx, db).
    SelectRaw("1").
    WhereRaw("orders.user_id = users.id")  // 关联子查询用 WhereRaw 表达跨表引用

q := gplus.NewQuery[User](ctx, db).ExistsSub(hasOrders)
```

注：标量聚合（如 `SELECT AVG(age)`）和关联条件（`orders.user_id = users.id`）暂时仍用 `SelectRaw` / `WhereRaw` 表达。本期不引入聚合 select 语法糖与跨表列引用 API，否则 scope 翻倍。

### 文件改动估算

| 文件 | 改动 | 增量行数 |
|---|---|---|
| `consts.go` | 加 `OpExists`, `OpNotExists` | +5 |
| `builder.go` | `applyWhere` 加 EXISTS 分支（无列名特化），位于现有子查询分支前 | +15 |
| `query.go` | 20 个方法（每个 ~5 行：错误检查 + addCond） | +120 |
| `update.go` | 20 个方法（同 Query） | +120 |
| `query.go` / `update.go` | `Subquerier` 接口定义（一处即可，建议放 `query.go` 头部） | +10 |
| 测试文件 `query_subquery_test.go` | 表驱动覆盖 Query 全 20 个方法 + 错误传播 | +400 |
| 测试文件 `updater_subquery_test.go` | Updater 路径 + UpdateByCondTx 集成 | +200 |
| **合计** | | **~870 行** |

### applyWhere EXISTS 分支

在 `builder.go:326` 现有子查询识别分支**之前**插入：

```go
// EXISTS / NOT EXISTS 子查询：无列名，直接生成 EXISTS (subquery)
if cond.operator == OpExists || cond.operator == OpNotExists {
    if subQuery, ok := cond.value.(*gorm.DB); ok {
        sqlStr := fmt.Sprintf("%s (?)", cond.operator)
        if cond.isOr {
            d = d.Or(sqlStr, subQuery)
        } else {
            d = d.Where(sqlStr, subQuery)
        }
        continue
    }
}
```

注：`ExistsSub` 方法存储 condition 时，`cond.expr = ""`（无列名），只用 operator 区分。

## 错误处理

| 场景 | 行为 |
|---|---|
| `sub == nil` | 追加 `errors.New("gplus: subquery is nil")` 到外层 `errs` |
| `sub.GetError() != nil` | 错误向外层 `errs` 传播 |
| `col` 指针无效（非 In 类） | 复用现有 `addCond` 的列名解析错误路径 |
| In/Eq 类子查询未 `Select(&col)` | godoc 明确写明，运行时 GORM 报多列错误兜底 |
| 外层 `q.GetError()` 已有错误 | Repository 入口前置检查会提前 return |

## 测试策略

### query_subquery_test.go（~400 行）

1. **每个操作符 × 主路径**（10 个 sub-test）：
   - 子查询 + 外层查询，DryRun 断言 SQL 形态
   - 真实数据断言行命中（用 `setupAdvancedDB(t)` 含 `User` + `Order`）
2. **每个 Or 变体**（10 个 sub-test）：与 AND 条件混用
3. **错误路径**（4 个 sub-test）：
   - `sub == nil`
   - `sub.GetError()` 非空（子查询用了非法字段指针）
   - `col` 指针非法（In 类）
   - 外层 `q.GetError()` 兜底
4. **DataRule 与子查询交互**（2 个 sub-test）：
   - 外层和子查询各自带不同 DataRule
   - 子查询 DataRule 列在子表上不存在 → 与现状一致行为

### updater_subquery_test.go（~200 行）

1. **Updater 各操作符 × Set 组合**（5 个代表性 sub-test，不全量重测）
2. **`repo.UpdateByCondTx` 集成**：真实 UPDATE WHERE id IN (subquery)，断言 affected
3. **错误传播**：`UpdateByCondTx` 在 `u.GetError()` 处提前 return

### 覆盖率目标

维持当前 96%+，子查询方法目标 100% 覆盖。

## 兼容性

- **纯新增 API**，不修改现有方法签名
- `Subquerier` 接口公开但只有 `*Query[T]` 内置实现，外部用户无需主动实现
- 现有 `WhereRaw` 子查询路径继续可用，新 API 是更类型安全的替代

## 风险

| 风险 | 缓解 |
|---|---|
| 调用方传未 `Select` 单列的 sub 给 In/Eq | godoc 示例明确 + GORM 运行时多列错误兜底；不在 builder 层做 Select 计数校验（实现成本不值） |
| EXISTS 子查询未指定 SELECT 列 | godoc 明确建议 `SelectRaw("1")`；不强制注入 SELECT 1 以保留调用方控制权 |
| 子查询内 DataRule 列不在该表 | 与现状一致；DataRule 设计文档需补一条"子查询场景下 DataRule 列必须存在于子查询表" |
| 错误注入路径绕过 | 复用现有 `q.errs` 机制，所有 builder 错误最终在 `GetError()` 汇总 |
| `Subquerier` 接口被外部冒名实现 | 接口公开是 Go 习惯（accept interfaces），不阻止；测试覆盖 `*Query[T]` 即可 |

## 发布计划

- v0.6.0 minor 版本
- CHANGELOG 详列 40 个新方法和 `Subquerier` 接口
- README "版本历史" 加 v0.6.0 章节，加子查询使用示例
- 单独 git tag

## 后续不在本期范围

- **SELECT 子查询**（关联子查询出现在 SELECT 列表）：返回类型如何承载额外列是 ORM 通病难题，单独立项
- **ANY / ALL 变体**（`> ANY (subquery)` 等）：业务使用频率低，留扩展空间
- **聚合 Select 语法糖**（替代 `SelectRaw("AVG(age)")`）：等子查询稳定后再考虑
- **跨表列引用 API**（替代关联子查询的 `WhereRaw("orders.user_id = users.id")`）：需要 Table alias 体系，scope 大
