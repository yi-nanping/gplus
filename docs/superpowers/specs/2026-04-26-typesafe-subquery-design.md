# Spec: 类型安全子查询（v0.6.0 minor）

**日期**：2026-04-26（二轮修订 2026-04-30）
**版本**：v0.6.0（minor，新增 API）
**类型**：新功能

## 背景与问题

gplus 当前所有条件构建器（`Query[T]` / `Updater[T]`）都基于字段指针实现类型安全。但**子查询（IN / 标量比较）只能走 `WhereRaw`**：

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

**底层机制已存在**，本期只需新增用户层类型安全包装方法 + Subquerier 接口。

## 范围决策

### 本期纳入

集合 / 标量 8 个核心操作符 + Or 变体 = **每边 16 方法（Query 16 + Updater 16 = 32 方法）**。

### 本期排除（含理由）

- **EXISTS / NOT EXISTS**：90% 真实场景为 correlated subquery（`WHERE orders.user_id = users.id`），需要跨表列引用 API 才能消灭 WhereRaw。当前签名 `ExistsSub(sub Subquerier)` 缺乏外层表别名信息，待 v0.7.0 引入 alias 体系时才能定型；提前发布会强制 v0.7.0 做破坏性签名变更
- **SELECT 子查询**：返回类型如何承载额外列是 ORM 通病难题，单独立项
- **ANY / ALL 变体**：v0.7.0 候选清单（提升优先级，因 EXISTS 暂缓）
- **聚合 Select 语法糖**（替代 `SelectRaw("AVG(age)")`）：等子查询稳定后再考虑
- **跨表列引用 API**（替代 correlated 子查询的 `WhereRaw("orders.user_id = users.id")`）：需要 Table alias 体系，scope 大

## 设计

### Subquerier 接口（新建 subquery.go）

```go
// 新建文件 subquery.go，集中接口 + 编译期断言
package gplus

// Subquerier 子查询契约。任意 *Query[X] 自动满足（X 可与外层 T 不同）。
// gplusSubquery() 私有方法限制接口只能由本包实现。
type Subquerier interface {
    // ToDB 返回可作为 GORM 子查询绑定参数的 *gorm.DB 对象
    ToDB(db *gorm.DB) *gorm.DB

    // GetError 返回构建过程累积的错误
    GetError() error

    gplusSubquery() // unexported guard: 阻止外部包冒名实现
}

// 编译期断言：*Query[T] 满足 Subquerier。
// 选 struct{} 作为占位 T 仅为验证方法集，gplusSubquery 不依赖 T，对任意 T 均成立。
var _ Subquerier = (*Query[struct{}])(nil)
```

`*Query[T]` 在 `query.go` 中新增：

```go
// gplusSubquery 标记 *Query[T] 为合法 Subquerier 实现（无副作用）
func (q *Query[T]) gplusSubquery() {}
```

**unexported guard 设计依据**：
- gplus 在 `builder.go:60-62` 有 `validDataRuleColumn` 正则白名单、`quoteColumn` 复杂表达式检测、v0.5.1 刚做完跨租户 patch — 项目对"用户可控字符串进入 SQL"持续加防御层
- 公开接口 + 任意外部实现可返回未经 `quoteColumn` / `validDataRuleColumn` 校验的 raw SQL DB，等于在安全栅栏开侧门
- `WhereRaw` 名字明示风险；`Subquerier` 名字暗示"安全的子查询"，用户更可能误以为它走 gplus 安全路径

### API 表

每个操作符自动配套 `Or` 前缀变体。

| 类别 | 方法 | SQL 形态 |
|---|---|---|
| 集合 | `InSub(col, sub)` / `NotInSub(col, sub)` | `col IN (subquery)` / `col NOT IN (subquery)` |
| 标量 | `EqSub(col, sub)` / `NeSub(col, sub)` | `col = (subquery)` / `col <> (subquery)` |
| 标量 | `GtSub(col, sub)` / `GteSub(col, sub)` | `col > (subquery)` / `col >= (subquery)` |
| 标量 | `LtSub(col, sub)` / `LteSub(col, sub)` | `col < (subquery)` / `col <= (subquery)` |

签名示例：

```go
// query.go
func (q *Query[T]) InSub(col any, sub Subquerier) *Query[T]
func (q *Query[T]) OrInSub(col any, sub Subquerier) *Query[T]

// update.go（同形态）
func (u *Updater[T]) InSub(col any, sub Subquerier) *Updater[T]
```

总计：Query 16 个 + Updater 16 个 = **32 个方法**。

### 立即 ToDB 快照语义（安全关键）

所有子查询方法（`InSub` / `EqSub` / `GtSub` 等）**入口处立即**调用 `sub.ToDB(q.db)` 取 `*gorm.DB` 快照，存进 `cond.value`。`builder.go:326-339` 的 `applyWhere` 现有路径正是基于 `cond.value.(*gorm.DB)` 类型断言识别子查询，本设计延续该既有契约。

实现伪代码：

```go
func (q *Query[T]) InSub(col any, sub Subquerier) *Query[T] {
    if sub == nil {
        q.errs = append(q.errs, ErrSubqueryNil)
        return q
    }
    if err := sub.GetError(); err != nil {
        q.errs = append(q.errs, err)
        return q
    }
    subDB := sub.ToDB(q.db)  // ← 关键：立即取快照，不存 sub 引用
    return q.addCond(false, col, OpIn, subDB)
}
```

**为何必须立即取快照**：

若改为存 `Subquerier` 引用延迟调用，调用方可在传入后绕过 DataRule：

```go
sub := NewQuery[Order](ctx).Select(&order.UserID).Eq(&order.TenantID, "A")
q.InSub(&user.ID, sub)              // 错误一次性提取 + ToDB 立即快照
sub.Or(&order.TenantID, "B")        // 此追加不影响最终 SQL（已快照）
```

立即快照保证 `InSub` 调用瞬间 `sub` 状态被冻结，与 `builder.go:326` 现有 `*gorm.DB` 子查询分支语义一致。

**调用方影响**：
- 子查询如需应用 DataRule，必须在传入 `InSub` **之前**显式调 `sub.DataRuleBuilder()`
- 子查询如需后续修改，必须重新构建 `sub` 并再次调用 `InSub`，不能复用同一引用

### 拒绝 any 重载的 ADR

考虑过的替代方案：把子查询作为 `In/Eq/Gt` 现有方法的 `any` 参数运行时分派（`switch v := val.(type) { case Subquerier: ... }`）。

不采用的理由：
1. **类型安全降级**：编译器无法在 `Eq(&col, "string")` vs `Eq(&col, sub)` 之间提示语义差异
2. **IDE 自动补全失去导航**：用户无法靠 `XxxSub` 后缀定位"这是子查询方法"
3. **godoc 必须用一段说明覆盖两种语义**：可读性下降
4. **错误信息上下文丢失**：`In` 的错误同时承载列名错误、值类型错误、子查询错误三个来源，难以区分

显式命名 32 个方法的成本可接受，符合 gplus 既有 `Eq/OrEq` / `Ne/OrNe` 重复模式（详见 query.go:219-237）。

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
    SelectRaw("AVG(age)")  // 标量聚合用 SelectRaw 兜底，本期不引入聚合 select 语法糖

q := gplus.NewQuery[User](ctx, db).GtSub(&user.Age, avgAge)

// 3. UPDATE users SET status='inactive' WHERE id IN (SELECT user_id FROM orders WHERE last_order < '2024-01-01')
inactiveOrders := gplus.NewQuery[Order](ctx, db).
    Select(&order.UserID).
    Lt(&order.LastOrderAt, cutoff)

u := gplus.NewUpdater[User](ctx, db).
    Set(&user.Status, "inactive").
    InSub(&user.ID, inactiveOrders)
repo.UpdateByCond(u)
```

注：标量聚合（如 `SELECT AVG(age)`）暂时仍用 `SelectRaw` 表达。本期不引入聚合 select 语法糖，否则 scope 翻倍。

### 文件改动估算

| 文件 | 改动 | 增量行数 |
|---|---|---|
| `subquery.go`（新建） | `Subquerier` 接口 + `gplusSubquery()` guard + 编译期断言 | +20 |
| `query.go` | 16 个方法（每个 ~5 行）+ `gplusSubquery()` 实现 | +85 |
| `update.go` | 16 个方法 + `gplusSubquery()` 实现 | +85 |
| `repository.go` | `ErrSubqueryNil` sentinel | +3 |
| 测试文件 `query_subquery_test.go` | 表驱动覆盖 Query 全 16 个方法 + 错误传播 + DataRule 交互 | +400 |
| 测试文件 `updater_subquery_test.go` | Updater 全 16 方法 DryRun + UpdateByCondTx 集成 | +250 |
| **合计** | | **~843 行** |

## godoc 要求

公开符号必须有完整 godoc：
- `Subquerier` 接口：契约说明 + 安全约束 + 使用示例
- 每个 `XxxSub` 方法：参数说明 + SQL 形态 + 使用约束（如"sub 必须构建完成再传入，传入后追加的条件不会传播错误"）
- `ErrSubqueryNil`：触发场景

参考 `query.go` 中 `NewQuery` / `DataRuleKey` 的 godoc 风格。

## 错误处理

### Sentinel

`repository.go` 新增：

```go
var ErrSubqueryNil = errors.New("gplus: subquery is nil")
```

加 sentinel 而非内联 `errors.New` 的依据：项目所有错误（`ErrQueryNil` / `ErrDeleteEmpty` / `ErrUpdateEmpty` 等）均为 package-level sentinel（repository.go:16-25），调用方可用 `errors.Is(err, gplus.ErrSubqueryNil)` 精确判断。

### 错误处理表

| 场景 | 行为 |
|---|---|
| `sub == nil` | 追加 `ErrSubqueryNil` 到外层 `errs` |
| `sub.GetError() != nil` | 直接 `q.errs = append(q.errs, sub.GetError())`（与现有 errs 累积路径一致），外层 `GetError()` 自然合并；不用双层 wrap 以避免 `"gplus query builder failed with N errors: gplus query builder failed with M errors: ..."` 冗余前缀 |
| `col` 指针无效 | 复用现有 `addCond` 的列名解析错误路径 |
| 子查询未 `Select(&col)` | godoc 明确写明，运行时 GORM 报多列错误兜底 |
| 外层 `q.GetError()` 已有错误 | Repository 入口前置检查会提前 return |
| **sub 在传入后继续追加错误条件** | **错误与条件均不会进入最终 SQL**（`InSub` 入口处取 `sub.ToDB` 快照，详见"立即 ToDB 快照语义"章节）；godoc 反例说明 |

错误时机陷阱说明：

```go
sub := gplus.NewQuery[Order](ctx, db).Select(&order.UserID)
q.InSub(&user.ID, sub)                 // 入口立即提取 sub.GetError() + 取 ToDB 快照
sub.Eq(&order.NotExistField, ...)      // 此处错误与条件均不会进入最终 SQL
```

`InSub` 在入口处一次性提取 `sub.GetError()` 并立即调用 `sub.ToDB(q.db)` 取 `*gorm.DB` 快照（详见"立即 ToDB 快照语义"章节）。传入之后向 `sub` 链式追加既不传播错误也不影响 SQL — 这是有意设计的安全语义，防止跨租户绕过。godoc 给反例说明。

## DataRule 与子查询的安全语义

**关键事实**：`ToDB()`（query.go:207-217）**不**自动调用 `DataRuleBuilder()`：

```go
func (q *Query[T]) ToDB(db *gorm.DB) *gorm.DB {
    session := db.Session(&gorm.Session{NewDB: true})
    if err := q.GetError(); err != nil {
        _ = session.AddError(err)
        return session
    }
    return q.BuildQuery()(session)  // ← 直接 BuildQuery，无 DataRuleBuilder
}
```

对比 `repository.go:172/192/213/...` 各 by-Cond 路径均显式 `q.DataRuleBuilder().GetError()` 后才走 BuildQuery — DataRule 应用一直是调用方主动行为。

**子查询继承此既有语义**：
- `sub.ToDB(db)` 默认**不**应用 sub 自身的 DataRule
- 调用方需主动 `sub.DataRuleBuilder().ToDB(db)` 才会让子查询应用 DataRule
- 设计依据：`DataRule.Column` 是字符串列名，不带表绑定。外层 `tenant_id` DataRule 在子查询表上可能列名相同语义不同（甚至不存在），自动应用会导致跨表静默扩散误杀

测试策略中加专门 sub-test 锁定该语义（详见测试章节 H2）。

## 测试策略

### TDD 分批计划

按 RED → GREEN → IMPROVE 分 4 批次执行，每批结束跑全量测试：

| 批次 | 范围 | 方法数 | 估算行数 |
|---|---|---|---|
| 1 | Query 集合（InSub/NotInSub）+ 标量主路径 6 个（EqSub/GtSub/LtSub/NeSub/GteSub/LteSub） | 8 | ~150 |
| 2 | Query 8 个 Or 变体 + 错误路径（4 sub-test） | 8 + 4 | ~150 |
| 3 | Updater 16 个方法（含 Or）+ UpdateByCondTx 集成 | 16 | ~250 |
| 4 | DataRule × 子查询交互（5 sub-test）+ 反向回归 + Session 复用 + 嵌套 | - | ~150 |

每批次完成后必须：① 全部测试 GREEN；② 跑 `go test -cover ./...` 确认无回归。

### query_subquery_test.go（~400 行）

**断言模式分配**：
- 操作符主路径：DryRun（断言 SQL 形态）+ 真实查询（断言行命中）双断言
- Or 变体：真实查询为主，DryRun 仅在边界场景
- 错误路径：纯 `assert.ErrorIs(err, ...)`，不需要真实查询

测试矩阵：

1. **8 个操作符 × 主路径**（8 个 sub-test）：每个用 `setupAdvancedDB(t)`（含 User + Order）
2. **8 个 Or 变体**（8 个 sub-test）：与 AND 条件混用
3. **错误路径**（4 个 sub-test）：
   - `sub == nil` → `errors.Is(err, ErrSubqueryNil)`
   - `sub.GetError() != nil` → 用测试辅助 `errSubquerier` 注入预设错误（详见下方）
   - `col` 指针非法（In 类）
   - 外层 `q.GetError()` 兜底
4. **DataRule × 子查询交互**（≥6 个 sub-test）：
   - 默认 `sub.ToDB()` **不**应用子查询 DataRule（锁定既有语义）
   - 显式 `sub.DataRuleBuilder().ToDB()` 应用 DataRule
   - 外层有 DataRule、子查询无 DataRule
   - 子查询 DataRule 列在子表不存在 → 与现状一致行为
   - 同一 sub 复用到两个外层 query（验证 dataRuleApplied 幂等）
   - **反向回归**：构造带 DataRule 的 ctx 调用 `sub.ToDB(db)`，DryRun 断言生成的 SQL **不含** DataRule WHERE 子句（防止未来 contributor 给 ToDB 隐式加 `DataRuleBuilder()` 破坏既有安全语义）
5. **Session 隔离**（1 个 sub-test）：同一 sub 传给两个外层 query → DryRun 对比两条最终 SQL，第二条的 WHERE 子句不含第一条的外层条件（断言可观测的 SQL 形态而非内部 Session 参数）
6. **嵌套子查询**（1 个 sub-test，可选）：subquery 内嵌 subquery，验证 db 实例传递链

### updater_subquery_test.go（~250 行）

**强化覆盖**（不再"5 个代表性"）：

1. **Updater 16 个方法 × DryRun**（16 个 sub-test）：每方法至少断言 SQL 形态
2. **核心操作符真实 UPDATE 集成**（4 个 sub-test）：InSub / EqSub / GtSub / NotInSub × `repo.UpdateByCondTx` 断言 affected
3. **错误传播**（2 个 sub-test）：`UpdateByCondTx` 在 `u.GetError()` 处提前 return
4. **MySQL 同表 IN 限制说明**：godoc 提示 + 测试不强制（SQLite 不报错），仅文档提示

### 测试辅助类型

```go
// query_subquery_test.go 内
type errSubquerier struct {
    err error
}

func (e *errSubquerier) ToDB(db *gorm.DB) *gorm.DB {
    return db.Session(&gorm.Session{NewDB: true})
}
func (e *errSubquerier) GetError() error { return e.err }
func (e *errSubquerier) gplusSubquery()  {} // 满足 guard
```

**注**：因 `gplusSubquery()` 是 unexported，外部包无法实现 `Subquerier`，但测试文件 `query_subquery_test.go` 与 `*Query[T]` 同属 `package gplus`，可正常实现接口。这正是 guard 设计的目的——**只有 gplus 包内代码能成为 Subquerier**。

### 覆盖率目标

维持当前 96.1%（v0.5.1 测试已达此水位），子查询方法目标 95%+（`sub.GetError()` 错误注入路径已通过 `errSubquerier` 覆盖，可冲 100%，但避免为追求 100% 引入畸形测试）。

## 兼容性

- **纯新增 API**，不修改现有方法签名
- `Subquerier` 接口公开但 `gplusSubquery()` guard 限制外部冒名实现，外部用户无需主动实现
- 现有 `WhereRaw` 子查询路径继续可用，新 API 是更类型安全的替代

## 风险

| 风险 | 缓解 |
|---|---|
| 调用方传未 `Select` 单列的 sub 给 In/Eq | godoc 示例明确 + GORM 运行时多列错误兜底；不在 builder 层做 Select 计数校验（实现成本不值） |
| 子查询内 DataRule 列不在该表 | 与现状一致；DataRule 设计文档需补一条"子查询场景下 DataRule 列必须存在于子查询表" |
| **sub 传入后追加条件** | `InSub` 立即取 `sub.ToDB(q.db)` 快照（详见"立即 ToDB 快照语义"章节），追加条件**不会**进入最终 SQL；godoc 显式反例。这是对跨租户绕过的安全锁定 |
| 错误注入路径绕过 | 复用现有 `q.errs` 机制，所有 builder 错误最终在 `GetError()` 汇总 |
| **MySQL UPDATE 同表 IN 子查询限制（ERROR 1093）** | `Updater.InSub` godoc 标注 MySQL 限制；不在 builder 层检测；用户可改写为 JOIN UPDATE 或子查询包一层临时表 |
| `Subquerier` 接口被外部冒名实现 | `gplusSubquery()` unexported guard 阻止外部实现 |

## 发布计划

- v0.6.0 minor 版本（v0.5.1 已发布，v0.6.0 槽位空闲；git 历史 commit 9e60a24 message 误用 "v0.6.0" 字样仅为 commit 命名问题，不占用版本号）
- CHANGELOG 详列 32 个新方法和 `Subquerier` 接口
- README "版本历史" 加 v0.6.0 章节，加子查询使用示例
- 单独 git tag `v0.6.0`

## 后续不在本期范围

- **EXISTS / NOT EXISTS**：等 v0.7.0 alias 体系到位后定型签名
- **SELECT 子查询**（关联子查询出现在 SELECT 列表）：返回类型如何承载额外列是 ORM 通病难题，单独立项
- **ANY / ALL 变体**（`> ANY (subquery)` 等）：v0.7.0 候选清单（提升优先级）
- **聚合 Select 语法糖**（替代 `SelectRaw("AVG(age)")`）：等子查询稳定后再考虑
- **跨表列引用 API**（替代 correlated 子查询的 `WhereRaw("orders.user_id = users.id")`）：需要 Table alias 体系，scope 大；EXISTS / correlated 场景的真正阻塞点

## 修订历史

- **2026-04-26**：首版
- **2026-04-30 三轮修订**（基于二轮多专家审计反馈）：
  - **M-Sec（HIGH，安全关键）**：新增"立即 ToDB 快照语义"专章，明确 `InSub` 入口立即调 `sub.ToDB(q.db)` 取 `*gorm.DB` 快照存进 `cond.value`，防止调用方在传入后追加条件绕过 DataRule
  - **M-Wrap（MEDIUM）**：`sub.GetError()` 改为直接 `append` 到外层 errs，移除双层 `fmt.Errorf` wrap 避免冗余前缀
  - **M-SessionTest（MEDIUM）**：Session 隔离测试断言点改为可观测 SQL 比较（DryRun 对比两条 SQL），不再断言内部 Session 参数
  - **M-Regression（MEDIUM）**：测试矩阵新增"反向回归"sub-test，构造带 DataRule 的 ctx 断言 `sub.ToDB(db)` 生成的 SQL 不含 DataRule WHERE 子句
  - **L-Assert（LOW）**：编译期断言 `*Query[any]` → `*Query[struct{}]` + 注释解释 T 占位选择
  - **L-Godoc（LOW）**：`Subquerier` godoc 移除"安全约束"段（受众已被 guard 阻止），`gplusSubquery` 改行内注释而非 godoc 块
  - **L-Batch4（LOW）**：批次 4 行数估算 ~100 → ~150（DataRule ctx 注入 + 反向回归实测量）

- **2026-04-30 二轮修订**（基于多专家审计反馈）：
  - **CRITICAL**：原 EXISTS 分支描述会导致死代码 → 砍掉 EXISTS（详见"范围决策"）
  - **HIGH**：本期不发 ExistsSub，因 correlated 场景需 alias 体系（v0.7.0）；提前发会强制 v0.7.0 破坏性变更
  - **HIGH**：`Subquerier` 加 `gplusSubquery()` unexported guard 阻止外部冒名实现
  - **HIGH**：DataRule 子查询行为文档化（沿用 ToDB 既有不触发 DataRuleBuilder 的语义），新增 5 个测试 sub-test 锁定
  - **HIGH**：新增 `ErrSubqueryNil` sentinel（与项目其它错误风格一致）
  - **HIGH**：错误处理表新增 "sub 传入后追加条件不传播" 行 + godoc 反例
  - **HIGH**：TDD 分 4 批次执行
  - **HIGH**：Updater 测试从 5 个升至 16 个方法 DryRun + 4 个真实 UPDATE 集成
  - **MEDIUM**：新建 `subquery.go` 集中接口 + 编译期断言
  - **MEDIUM**：`sub.GetError()` 用 `fmt.Errorf("...: %w", err)` wrap
  - **MEDIUM**：MySQL UPDATE 同表 IN 限制写入风险表
  - **MEDIUM**：覆盖率目标修正为 96.1%（v0.5.1 实际值）
  - **MEDIUM**：DryRun vs 真实数据断言分配明确
  - **MEDIUM**：新增"拒绝 any 重载"ADR
  - **LOW**：godoc 风格要求章节
  - **LOW**：`errSubquerier` 测试辅助类型方案
