# v0.8.0 alias 体系设计

> **版本**：v0.8.0（草案）
> **日期**：2026-05-06
> **作者**：通过 brainstorming skill 协作产出
> **状态**：待用户复核 → 进入 writing-plans
> **前置版本**：v0.7.1
> **后续候选**：v0.8.1（ANY/ALL + SelectSub）/ v0.9（类型安全 ON extra + 全局 alias）

---

## 1. 背景与动机

### 1.1 现状缺口

v0.7.1 之前 gplus 的 JOIN / 子查询体系存在四个体系性缺口，下游项目（gvs-server 等）实测全部命中：

| 场景 | 当前可行写法 | 问题 |
|---|---|---|
| **A. 跨表列引用** | `q.LeftJoin("orders ON orders.user_id = users.id").WhereRaw("orders.amount = ?", 100)` | 失去 `&model.Field` 类型安全；列名拼写错误编译能过 |
| **B. correlated EXISTS** | 完全无原生支持，必须 `WhereRaw("EXISTS (SELECT 1 FROM orders WHERE orders.user_id = users.id)")` | 大段 raw SQL，无法复用 Subquerier |
| **C. 同表自连接** | 完全无支持（同表两次需要不同 alias，但当前体系只能引用规范单例） | 必须全 raw |
| **D. SELECT 子查询投影** | 完全无支持 | 必须全 raw |

四个场景的共同根因：**JOIN/子查询场景下没有"alias 句柄 + 列引用语义"**——`&model.Field` 字段指针只能解析到规范单例对应的表名前缀，无法表达"用 alias o 引用 Order 表"或"在子查询里引用外层表"。

### 1.2 设计目标

在不破坏 v0.6.0 / v0.7.0 既有契约的前提下，引入 **alias 体系**，使下列形态成为类型安全的一等公民：

```go
// 跨表列引用
o := gplus.As[Order](q, "o")
q.LeftJoinAs(o, &o.UserID, &u.ID).Eq(&o.Amount, 100)

// correlated EXISTS
sub, o2 := gplus.SubQuery[Order](q)
sub.EqCol(&o2.UserID, &u.ID)
q.Exists(sub)

// 同表自连接
boss := gplus.As[User](q, "boss")
q.LeftJoinAs(boss, &u.BossID, &boss.ID)

// 主表 alias
q, u := gplus.NewQueryAs[User](ctx, "u")
```

---

## 2. 设计决策摘要

| 议题 | 决策 | 理由（一句） |
|---|---|---|
| 列引用形态 | **路线 1：字段指针 + alias 包装实例** | 与现有 `&model.Field` 哲学一致，零代码生成 |
| Alias 生命周期 | **模型 A：Query 局部** | 避免全局可变状态；GC 友好；与 Query 短命语义一致 |
| 旧 Join API | **选项 B：deprecated 共存** | string JOIN 有合法用途（JOIN 子查询表 / USING）；deprecated 引导迁移 |
| 子查询形态 | **形态 2：派生式（包级函数）** | 与 v0.6.0 Subquerier 完全延续；method 受 Go 泛型限制不能新增类型参数 |
| 主表 alias | **选项 B：NewQueryAs 入口** | 自连接 SQL 输出对称；零破坏性 |
| ANY/ALL | **延期到 v0.8.1** | 表面成本 24 方法，实际使用频率低 |
| SelectSub | **延期到 v0.8.1** | 依赖 GORM Select 嵌套子查询行为实测 |
| DataRule × alias | **仅作主表，副表用户自负责（godoc 警告）** | 自动跨表注入太魔法；显式安全 > 隐式陷阱 |
| Updater | **同步镜像** | v0.2.0 起 Updater 与 Query 形态对齐承诺 |
| API 实现限制 | **As / SubQuery 必须包级函数** | Go 1.18+ method 不能新增类型参数（[issue #49085](https://github.com/golang/go/issues/49085)） |

---

## 3. 架构概览

### 3.1 数据模型变更

```
现有：
  Query[T] 持有
    - T 的规范单例（getModelInstance[T]）
    - errs []error
    - conditions / selects / joins / orders / ...

新增：
  Query[T] 增加
    - aliases       map[string]aliasEntry   // alias name → 注册项
    - outerQueryRef AnyQuery                // 子查询时指向外层 Query/Updater；顶层为 nil
                                            // （字段名加 Ref 后缀，避免与接口 method outerQuery() 冲突）

  aliasEntry {
    instance any         // *X 独立实例（reflect.New 创建，字段地址独立于规范单例）
    name     string      // SQL 中的 alias name
    typ      reflect.Type
    addrLow  uintptr     // 实例地址范围下界（含），用于 lookupAddr
    addrHigh uintptr     // 实例地址范围上界（不含）
  }
```

### 3.2 列名解析新流程

`resolveColumnName(addr uintptr)` 的伪代码：

```
解析顺序（沿 Query 链由内至外）：
  current := q
  while current != nil:
      for entry in current.aliases:
          if entry.addrLow <= addr < entry.addrHigh:
              offset := addr - entry.addrLow
              col   := schemaFor(entry.typ)[offset]
              return entry.name + "." + col, nil
      current = current.outerQuery

  顶层未命中：
      回退原有 columnNameCache（规范单例）→ "table.col"

  都失败：
      累积 ErrFieldAddrUnregistered，返回 ""
```

**关键不变量**：

1. **alias 实例只读**——业务代码绝对不该 `o.Amount = 100`，只用于取字段地址（godoc 警告）
2. **alias name 在 Query 链中唯一**——`As()` 时检查 `current → outerQuery → ...` 全链
3. **alias 实例不入全局缓存**——避免 Query GC 后僵尸条目内存累积
4. **解析路径线性扫描**——通常 alias ≤ 5、outerQuery 链 ≤ 3 层，开销可忽略（实测在性能基线验证）

### 3.3 类型擦除接口 `AnyQuery`

由于 `Query[T]` 和 `Updater[T]` 都需要支持 alias 体系，且 `As / SubQuery` 必须接收"任意类型参数的 Query 或 Updater"，新增类型擦除接口：

```go
// AnyQuery 是 Query[T] 和 Updater[T] 的共同抽象，用于 alias / SubQuery 派生场景
//
// 仅暴露内部使用所需的最小方法集。业务代码不应直接实现该接口
// （unexported method 阻止包外冒名实现）
type AnyQuery interface {
    addAlias(name string, typ reflect.Type, instance any) error
    lookupAddr(addr uintptr) (alias, col string, ok bool)
    outerQuery() AnyQuery
    context() context.Context     // 子查询派生时透传 ctx
    appendErr(err error)
    GetError() error

    // gplusAnyQuery 是 unexported guard，阻止外部冒名实现
    gplusAnyQuery()
}

// 编译期断言
var _ AnyQuery = (*Query[struct{}])(nil)
var _ AnyQuery = (*Updater[struct{}])(nil)
```

---

## 4. 公共 API（v0.8.0 范围）

### 4.1 Query 入口

```go
// NewQuery 现有，行为零变更
func NewQuery[T any](ctx context.Context) (*Query[T], *T)

// NewQueryAs 新增：给主表起 alias
//
// alias 必须满足正则 ^[a-zA-Z_][a-zA-Z0-9_]{0,31}$；非法时累积 ErrAliasInvalidName
//
// 返回的 *T 实例是独立 alias 实例（字段地址绑定到该 alias），而非规范单例
func NewQueryAs[T any](ctx context.Context, alias string) (*Query[T], *T)

// Repository 同步
func (r *Repository[K, T]) NewQueryAs(alias string) (*Query[T], *T)
```

### 4.2 Alias 创建

```go
// As 在 q（含 outerQuery 链）上注册一个 X 类型的 alias 实例
//
// alias name 必须在 q 的整条链中唯一，否则累积 ErrAliasDuplicate；
// 名字非法时累积 ErrAliasInvalidName
//
// 错误情形下返回首次注册的实例（防 nil 解引用 panic），错误在 q.GetError() 上报
//
// 返回的 *X 实例只用于取字段地址，不应被业务代码修改
func As[X any](q AnyQuery, alias string) *X
```

### 4.3 JOIN with alias

```go
// 7 种 JoinAs 方法，每种 JOIN 类型一对（Query 与 Updater 各一份）
//
// leftCol / rightCol 接受字段地址（*F）或 alias 实例的字段地址（*o.UserID 等）
// 由于 Go method 不能新增类型参数，这里用 any 接收，运行时通过 resolveColumnName 解析
//
// extra 接受 raw 字符串片段（如 "AND o.deleted_at IS NULL"），可带占位符 args
// v0.8.0 不实现类型安全的 ON extra 三元组，留待 v0.9
func (q *Query[T]) LeftJoinAs(alias any, leftCol any, rightCol any, extra ...any) *Query[T]
func (q *Query[T]) RightJoinAs(alias any, leftCol any, rightCol any, extra ...any) *Query[T]
func (q *Query[T]) InnerJoinAs(alias any, leftCol any, rightCol any, extra ...any) *Query[T]
func (q *Query[T]) OuterJoinAs(alias any, leftCol any, rightCol any, extra ...any) *Query[T]
func (q *Query[T]) FullJoinAs (alias any, leftCol any, rightCol any, extra ...any) *Query[T]

// CrossJoinAs / NaturalJoinAs 无 ON 条件
func (q *Query[T]) CrossJoinAs   (alias any) *Query[T]
func (q *Query[T]) NaturalJoinAs (alias any) *Query[T]

// Updater 镜像（同形态）
func (u *Updater[T]) LeftJoinAs(...)  // 与 Query 同
func (u *Updater[T]) RightJoinAs(...)
// ...其余 5 种
```

**alias 参数为何用 any 而非 *X**：method 不能引入新类型参数，无法写 `func (q *Query[T]) LeftJoinAs[X any](alias *X, ...)`。运行时通过 `lookupAddr` 校验 alias 实例确实属于当前 q 链。

**deprecated 旧 API 保留**：

```go
// Deprecated: use LeftJoinAs for type-safe column references.
// Will be removed in v1.0.
// Still useful for joining subquery tables / function-returning tables / USING clauses.
func (q *Query[T]) LeftJoin(table string, on string, args ...any) *Query[T]
// ...其他 6 种同样标记 deprecated
```

### 4.4 子查询派生

```go
// SubQuery 派生子查询：sub.outerQueryRef = outer，sub 默认主表 alias = 表名
//
// outer 必须非 nil，否则 panic（编程错误，不该静默）
// sub 的 ctx 来自 outer 的 ctx（透传）
// sub 的 errs 在 q.GetError() 时通过 outerQuery 链聚合上报
//
// sub 不自动应用 outer 的 DataRule（保持 v0.6.0 既有语义）
func SubQuery[X any](outer AnyQuery) (*Query[X], *X)

// SubQueryAs 派生子查询并指定主表 alias
func SubQueryAs[X any](outer AnyQuery, alias string) (*Query[X], *X)
```

### 4.5 EXISTS / NOT EXISTS

```go
// 复用 v0.6.0 Subquerier 接口（*Query[X] 自动满足）
func (q *Query[T]) Exists      (sub Subquerier) *Query[T]
func (q *Query[T]) NotExists   (sub Subquerier) *Query[T]
func (q *Query[T]) OrExists    (sub Subquerier) *Query[T]
func (q *Query[T]) OrNotExists (sub Subquerier) *Query[T]

// Updater 镜像（同形态，4 个方法）
func (u *Updater[T]) Exists      (sub Subquerier) *Updater[T]
func (u *Updater[T]) NotExists   (sub Subquerier) *Updater[T]
func (u *Updater[T]) OrExists    (sub Subquerier) *Updater[T]
func (u *Updater[T]) OrNotExists (sub Subquerier) *Updater[T]
```

### 4.6 Subquerier 接口（v0.6.0 既有，零变更）

```go
type Subquerier interface {
    ToDB(db *gorm.DB) *gorm.DB
    GetError() error
    gplusSubquery()  // unexported guard
}
// *Query[X] 自动满足。SubQuery 派生出的也是 *Query[X]
```

### 4.7 API 表面统计

| 类别 | 数量 |
|---|---|
| Query 新增 | 1 NewQueryAs + 7 JoinAs + 4 EXISTS = **12** |
| Updater 新增 | 7 JoinAs + 4 EXISTS = **11** |
| 包级函数新增 | As + SubQuery + SubQueryAs = **3** |
| Repository 新增 | NewQueryAs = **1** |
| 哨兵错误新增 | 4 个 |
| **合计新方法/函数** | **27** |
| Deprecated（不删） | 7 旧 Join × 2（Query + Updater 各一份）= 14 |

---

## 5. 数据流详解

### 5.1 Alias 注册（`gplus.As[X](q, "o")`）

```
1. 校验 alias name：
   ^[a-zA-Z_][a-zA-Z0-9_]{0,31}$
   非法 → q.appendErr(ErrAliasInvalidName)，return getModelInstance[X]()（fallback 防 nil）

2. 检查 name 在 q 链中是否已存在：
   current := q
   while current != nil:
       if name in current.aliases:
           q.appendErr(ErrAliasDuplicate)
           return current.aliases[name].instance.(*X)  // 返回首次注册实例
       current = current.outerQuery()

3. 创建独立实例：
   typ      := reflect.TypeOf((*X)(nil)).Elem()
   instance := reflect.New(typ).Interface().(*X)
   addrLow  := uintptr(unsafe.Pointer(instance))
   addrHigh := addrLow + typ.Size()

4. 触发 schema 缓存（如未缓存）：
   reflectStructSchema(typ)  // 偏移量 → 列名映射，按类型缓存

5. 写入 q.aliases：
   q.aliases[name] = aliasEntry{
       instance: instance, name: name, typ: typ,
       addrLow: addrLow, addrHigh: addrHigh,
   }

6. return instance
```

**线程安全**：`q.aliases` 在 Query 生命周期内非并发访问（Query 是请求局部对象，跨 goroutine 复用 Query 本身就是误用）。无需 sync.Map。

### 5.2 列名解析（`resolveColumnName(addr)`）

```
current := q
while current != nil:
    if alias, col, ok := current.lookupAddr(addr); ok:
        return alias + "." + col, nil
    current = current.outerQuery()

// 顶层未命中，回退全局规范单例
if col, ok := globalColumnNameCache.Load(addr); ok:
    return col, nil  // "table.col" 形态

// 都失败
q.appendErr(ErrFieldAddrUnregistered)
return "", ErrFieldAddrUnregistered
```

`lookupAddr` 实现：

```go
func (q *Query[T]) lookupAddr(addr uintptr) (alias, col string, ok bool) {
    for _, entry := range q.aliases {
        if entry.addrLow <= addr && addr < entry.addrHigh {
            offset := addr - entry.addrLow
            schema := reflectStructSchema(entry.typ)
            if c, found := schema[offset]; found {
                return entry.name, c, true
            }
        }
    }
    return "", "", false
}
```

### 5.3 JoinAs 链路

`q.LeftJoinAs(alias, leftCol, rightCol, extra...)`：

```
1. 校验 alias 实例属于 q 链：
   addr := uintptr(reflect.ValueOf(alias).Pointer())
   if not in any aliasEntry of q chain:
       q.appendErr(ErrAliasNotInChain)
       return q  // 跳过，保留链式

2. 解析 leftCol / rightCol：
   leftStr,  _ := q.resolveColumnName(addrOf(leftCol))
   rightStr, _ := q.resolveColumnName(addrOf(rightCol))

3. 构造 joinInfo：
   tableName := schemaTableName(typeOf(alias))
   aliasName := lookupAliasName(alias)
   joinSQL   := fmt.Sprintf("LEFT JOIN %s AS %s ON %s = %s",
       quoteIdent(tableName), quoteIdent(aliasName),
       leftStr, rightStr)
   if extra != nil:
       joinSQL += " " + extra[0].(string)  // 允许 raw 片段

4. 追加到 q.joins：
   q.joins = append(q.joins, joinInfo{
       query:    joinSQL,
       args:     extraArgs,
       aliasName: aliasName,  // 新增字段
   })
```

生成 SQL 形态：

```sql
LEFT JOIN orders AS o ON o.user_id = users.id [AND <extra raw>]
```

### 5.4 SubQuery 派生

```go
func SubQuery[X any](outer AnyQuery) (*Query[X], *X) {
    if outer == nil {
        panic("gplus: SubQuery outer is nil (programmer error)")
    }
    ctx := outer.context()                 // 透传 ctx
    sub, x := NewQuery[X](ctx)
    sub.outerQueryRef = outer              // 关键：设置外层引用
    return sub, x
}
```

子查询里的列解析路径示意：

```
sub.EqCol(&o2.UserID, &u.ID) 内部：

  addrL := uintptr(unsafe.Pointer(&o2.UserID))
  addrR := uintptr(unsafe.Pointer(&u.ID))

  // 解析 &o2.UserID
  → sub.lookupAddr(addrL)  → 命中 sub.aliases["orders"] → "orders.user_id"

  // 解析 &u.ID
  → sub.lookupAddr(addrR)  → 未命中
  → sub.outerQuery() = q
  → q.lookupAddr(addrR)    → 命中 q.aliases（主表 NewQueryAs 注册的）→ "u.id"

  最终条件：WHERE orders.user_id = u.id
```

### 5.5 Exists 的 SQL 生成

```go
func (q *Query[T]) Exists(sub Subquerier) *Query[T] {
    if sub == nil {
        q.appendErr(ErrSubqueryNil)
        return q
    }
    q.conditions = append(q.conditions, leafCondition{
        kind:    existsLeaf,
        subExpr: sub,
        op:      "EXISTS",
    })
    return q
}
```

`BuildQuery` 在 `applyWhere` 处理 `existsLeaf`：

```go
case existsLeaf:
    if subErr := leaf.subExpr.GetError(); subErr != nil {
        d.AddError(subErr)
    }
    subDB := leaf.subExpr.ToDB(d.Session(&gorm.Session{NewDB: true}))
    d = d.Where(leaf.op + " (?)", subDB)  // EXISTS / NOT EXISTS / OR EXISTS / OR NOT EXISTS
```

`OrExists` / `OrNotExists` 走 OR 分支构造（与现有 `OrEq` / `OrIn` 同 pattern）。

---

## 6. 错误处理

### 6.1 新增哨兵错误

```go
var (
    // alias 名字在 q.aliases 或 outerQuery 链中已存在
    ErrAliasDuplicate = errors.New("gplus: alias name already registered in this query chain")

    // alias name 不符合白名单
    ErrAliasInvalidName = errors.New("gplus: invalid alias name (must match [a-zA-Z_][a-zA-Z0-9_]{0,31})")

    // 字段地址既不在当前 Query 的 aliases，也不在 outerQuery 链，也不在全局规范单例
    ErrFieldAddrUnregistered = errors.New("gplus: field address not registered to any model or alias in this query chain")

    // alias 实例传给了 JoinAs 但该实例从未在当前 q（含 outer 链）注册过
    ErrAliasNotInChain = errors.New("gplus: alias instance does not belong to this query chain")
)
```

### 6.2 错误累积链路

复用 v0.6.0 累积机制，新增触发节点：

| 触发点 | 处理 |
|---|---|
| `As[X](q, name)` name 非法 | 累积 `ErrAliasInvalidName`，返回 fallback 实例 |
| `As[X](q, name)` name 重复 | 累积 `ErrAliasDuplicate`，返回首次注册的实例 |
| `LeftJoinAs(o, ...)` o 不在链 | 累积 `ErrAliasNotInChain`，跳过该 JOIN |
| `resolveColumnName(addr)` 失败 | 累积 `ErrFieldAddrUnregistered`，返回空字符串列名 |
| `Exists(nil)` | 累积 `ErrSubqueryNil`（v0.6.0 既有） |
| `Exists(sub)` 时 sub 自身有 errs | 透传到 q.errs |
| `SubQuery(nil)` | **panic**（编程错误，不静默） |

### 6.3 GetError 摘要

`q.GetError()` 沿 outerQuery 链聚合：

```
gplus query builder failed with 2 errors:
  - gplus: alias name already registered in this query chain
  - subquery error: gplus: field address not registered to any model or alias in this query chain
```

### 6.4 安全契约：DataRule × alias

**核心原则**：DataRule 仅作主表，alias 副表敞开。

godoc / README / CHANGELOG 三处必须明确警告：

> ⚠️ **DataRule 不会自动应用到 alias 副表**。JOIN 进来的副表如果含敏感数据（如 `tenant_id`），必须采取以下三种合规做法之一：
>
> 1. 在 JoinAs 的 `extra` 参数手写副表的数据权限条件
> 2. 派生 sub 并显式调用 `sub.DataRuleBuilder()`，将 sub 作为 JOIN 子查询表（暂走 deprecated 旧 Join API）
> 3. 在 DataRule.Column 字段显式带 alias 前缀（如 `"o.tenant_id"`），并依赖外层 q 应用
>
> **典型陷阱**：
> ```go
> q.LeftJoinAs(o, &o.UserID, &u.ID).Eq(&o.Amount, 100)
> // SQL: SELECT users.* FROM users
> //      LEFT JOIN orders AS o ON o.user_id = users.id
> //      WHERE o.amount = 100 AND users.tenant_id = ?  ← 主表 DataRule
> //                                                    ← orders 完全敞开！
> ```

CHANGELOG v0.8.0 新增"行为约束（须知）"段，单列此安全契约。

---

## 7. 测试策略

### 7.1 GORM 行为探针（永久 RED-locked）

参照 v0.7.0 `TestGORMCallbackBehaviorProbe` 模式，永久锁定 GORM v1.31.x 在以下场景的实测行为：

```go
// alias_probe_test.go
func TestGORMAliasBehaviorProbe(t *testing.T) {
    t.Run("Joins_AliasString_GeneratesExpectedSQL", ...)
    t.Run("Where_ExistsSubquery_SubqueryDBSubstitution", ...)
    t.Run("SelfJoin_SameTable_DifferentAlias_NoConflict", ...)
    t.Run("Session_NewDBTrue_BreaksOuterWhereInheritance", ...)
}
```

升级 GORM 时此测试 fail 第一时间感知行为变化。

### 7.2 单元测试覆盖矩阵

| 测试文件（新增） | 覆盖点 | 子测试估计 |
|---|---|---|
| `alias_test.go` | As 创建 / 重复名 / 非法名 / 跨 query 复用检测 / 字段地址解析链 | 12 |
| `query_joinas_test.go` | 7 种 JoinAs × ON 形态 × extra raw 片段 | 18 |
| `query_subquery_correlated_test.go` | SubQuery 派生 / 跨层 alias 引用 / 嵌套 sub 3 层 | 10 |
| `query_exists_test.go` | Exists/NotExists/OrExists/OrNotExists × 简单/相关 sub | 12 |
| `updater_alias_test.go` | Updater 镜像（JoinAs/SubQuery/Exists） | 10 |
| `query_newqueryas_test.go` | NewQueryAs 主表 alias / 与副表 alias 冲突检测 / SQL 输出 | 8 |
| `alias_datarule_test.go` | DataRule × alias 三种合规模式 + 一个反例（验证副表敞开警告） | 6 |
| **合计** | | **76** |

### 7.3 集成测试（MySQL/SQLite 双方言）

复用现有 `mysql_integration_test.go` 模式，新增：
- 自连接（users JOIN users boss）双方言行为
- correlated EXISTS 在 SQLite 与 MySQL 的 SQL 输出差异
- LEFT JOIN AS 在 MySQL `ONLY_FULL_GROUP_BY` 模式下的行为

### 7.4 性能基线

新增 `bench_alias_test.go`：

| Benchmark | 期望阈值 | 说明 |
|---|---|---|
| `BenchmarkResolveColumnName_NoAlias` | 基线 | 直查全局 columnNameCache |
| `BenchmarkResolveColumnName_OneAlias` | ≤ 50 ns/op | 线性扫描 1 个 alias |
| `BenchmarkResolveColumnName_FiveAliases` | ≤ 100 ns/op | 线性扫描 5 个 alias |
| `BenchmarkResolveColumnName_OuterChain3` | ≤ 300 ns/op | 跨 3 层 outerQuery |

如不达标加 Query 局部 hash map（addr → alias 提前算好），P9 阶段处理。

### 7.5 文档与 README

- README 新章节 **"Alias 与跨表查询"**，展示 4 大场景实例（A/B/C/D）
- godoc 在 `As` / `NewQueryAs` / `LeftJoinAs` / `SubQuery` / `Exists` 上加完整 example 块
- CHANGELOG v0.8.0 加 **"行为约束（须知）"** 段，强调 DataRule × alias 安全契约

---

## 8. 落地计划

### 8.1 阶段拆分

| 阶段 | 任务 | Commit 数 | 阻塞依赖 |
|---|---|---|---|
| **P0 探针** | `TestGORMAliasBehaviorProbe` 4 子测试，永久 RED-lock GORM v1.31.x 行为 | 1 | 无 |
| **P1 内核** | `AnyQuery` 接口；`Query.aliases / outerQueryRef` 字段；`As[X]` 创建函数；`resolveColumnName` 沿链查找；4 个新错误哨兵 | 3-4 | P0 |
| **P2 NewQueryAs** | 主表 alias 入口；schema 缓存按 alias 解析路径；name 冲突检测 | 2 | P1 |
| **P3 JoinAs（Query）** | 7 种 JoinAs；`joinInfo.aliasName`；`applyJoinsAs`；deprecated 标记旧 Join | 4 | P1 |
| **P4 SubQuery 派生** | `SubQuery / SubQueryAs`；outerQuery 链路；ctx 透传；errs 聚合 | 2 | P1 |
| **P5 Exists / NotExists** | 4 方法；`existsLeaf` 条件类型；BuildQuery 集成 | 2 | P3 + P4 |
| **P6 Updater 镜像** | 复制 P3-P5 到 Updater；表驱动测试 | 3 | P3-P5 |
| **P7 集成 + 双方言** | MySQL/SQLite 集成测试；自连接；correlated EXISTS | 1-2 | P6 |
| **P8 文档 + CHANGELOG** | README 新章节；godoc example；CHANGELOG v0.8.0 | 1 | P7 |
| **P9 性能基线** | `bench_alias_test.go` 4 benchmark；如不达标加 Query 局部缓存 | 1-2 | P7 |
| **合计** | | **20-22 commits** | |

预计 2-3 个工作日（按 v0.6.0 / v0.7.0 节奏估算）。

### 8.2 TDD 节奏（强制）

每个阶段先 RED：

1. 写测试，断言**期望 SQL 字面**（用 `q.ToSQL` DryRun 对比）+ **期望错误**（`errors.Is` 校验）
2. 运行测试，确认 fail 且原因是"未实现"而非"语法错"
3. 写最小实现让 GREEN
4. 提交 commit（commit message 含测试名 + 实现摘要）

**禁止**：先写实现再补测试。v0.7.0 已踩过坑（aggregate 修复一开始忘了 RED 探针）。

### 8.3 范围切线

| 功能 | 版本 | 理由 |
|---|---|---|
| `As[X]` + `NewQueryAs` | **v0.8.0** | 内核必备 |
| 7 种 `JoinAs` | **v0.8.0** | 跨表列引用主战场 |
| `SubQuery` / `Exists` / `NotExists` | **v0.8.0** | correlated subquery 主战场 |
| Updater 镜像 | **v0.8.0** | 对称承诺 |
| `SelectSub` | **v0.8.1** | 依赖 GORM Select 嵌套子查询实测 |
| `EqAny/NeAny/.../EqAll/...` 24 方法 | **v0.8.1** | 用得少，可延期 |
| `JoinAs` extra 三元组类型安全 ON | **v0.9** | 签名歧义复杂，独立设计 |
| 全局 alias（包级 var） | **v0.9 候选** | 模型 B；需 weak ref 设计；YAGNI |
| UNION / WITH CTE / 窗口函数 | **v1.0+** | 与 alias 体系正交，独立立项 |

---

## 9. 风险与缓解

| 风险 | 概率 | 影响 | 缓解 |
|---|---|---|---|
| GORM Joins 字符串模板对 alias 处理与预期不符 | 低 | 高 | P0 探针提前锁定；如失败先调整 SQL 拼装策略 |
| 跨 3 层以上 outerQuery 性能不达标 | 中 | 中 | P9 benchmark 后加 Query 局部缓存（addr → alias 提前算好） |
| DataRule × alias 副表敞开导致下游误用 | 中 | **高（安全）** | godoc + CHANGELOG 双重警告；example 给三种合规写法；security review |
| `any` 类型 leftCol/rightCol 导致运行时类型不匹配 | 中 | 低（SQL 报错快速暴露） | 测试覆盖类型不匹配场景；godoc 示例引导 |
| alias 实例跨 Query 复用（用户误用） | 低 | 中 | `ErrAliasNotInChain` 检测 + godoc 警告 |
| 子查询深层嵌套（≥4 层）解析性能 | 低 | 低 | 在病态情况，不优化（SQL 也无法 review） |

---

## 10. 验收清单（v0.8.0 Release Gate）

- [ ] 所有新测试 GREEN，覆盖率 ≥ 96.0%
- [ ] `TestGORMAliasBehaviorProbe` 4 子测试 RED-lock 通过
- [ ] MySQL + SQLite 双方言集成测试通过
- [ ] 性能基线达成（5 alias 解析 ≤ 100 ns/op）
- [ ] CHANGELOG / README / godoc 三处文档更新
- [ ] 至少 1 个下游项目（gvs-server）实测落地无回归
- [ ] DataRule × alias 安全契约在 godoc + README + CHANGELOG 三处警告齐全
- [ ] deprecated 旧 Join API 仍可编译，下游升级零破坏
- [ ] AnyQuery 接口的 unexported guard `gplusAnyQuery()` 阻止外部冒名实现
- [ ] `NewQueryAs` / `As` / `SubQuery` 所有错误路径测试覆盖

---

## 11. 不在本期范围

- **EXISTS 子查询里使用 ANY/ALL**：依赖 ANY/ALL 实现，v0.8.1
- **SelectSub**：依赖 GORM Select 嵌套子查询实测；v0.8.1
- **类型安全 ON extra 三元组**（`extra: (col, op, value)`）：签名歧义复杂；v0.9
- **全局 alias 包级 var**（模型 B）：需 weak ref，YAGNI；v0.9 候选
- **JOIN 子查询表**（`JOIN (SELECT ...) sub`）：deprecated 旧 Join 暂时兜底；v0.9 单独设计
- **UNION / WITH CTE / 窗口函数**：与 alias 体系正交；v1.0+ 独立立项
- **Repository 直接的 EXISTS 短路** （`repo.ExistsByJoin(...)`）：组合形态太多，先看 v0.8.0 实战需求

---

## 附录 A：与 v0.6.0 / v0.7.0 既有契约的兼容性

| 既有契约 | 是否破坏 | 说明 |
|---|---|---|
| `&model.Field` 字段指针解析 | **不破坏** | 全局 columnNameCache 仍是顶层 fallback |
| `q.InSub / NotInSub / EqSub / ...` v0.6.0 子查询方法 | **不破坏** | Subquerier 接口零变更 |
| `gplus.FindAs / FindOneAs` v0.7.0 投影 API | **不破坏** | 与 alias 体系正交 |
| `OnConflict` / 乐观锁 / DataRule | **不破坏** | 与 alias 体系正交 |
| 旧 `LeftJoin(string, string)` | **deprecated 但保留** | godoc 标记，v1.0 删除 |
| `q.Clear()` 重置语义 | **扩展**：需同时清空 `aliases` map 和 `outerQueryRef` | 测试覆盖 `TestQuery_Clear_AliasReset` |

---

## 附录 B：参考与上下文

- v0.6.0 类型安全子查询设计：`docs/superpowers/specs/2026-04-26-typesafe-subquery-design.md`
- v0.7.0 Query-chain-safe 投影 API：`CHANGELOG.md` v0.7.0 段
- Go 泛型 method 类型参数限制：[golang/go#49085](https://github.com/golang/go/issues/49085)
- GORM v1.31.x Joins / Where 子查询行为：以 `TestGORMAliasBehaviorProbe` 实测为准

