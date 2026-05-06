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
    revoked  bool        // N4：Clear() 时翻转，lookupAddr 命中 revoked 直接返回错误
                         //     防止 "残骸 alias 实例" 的 use-after-clear 静默错 SQL
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
              if entry.revoked:                          // N4：revoked 直接拒绝
                  累积 ErrAliasRevoked，return ""
              offset := addr - entry.addrLow
              schema := schemaFor(entry.typ)
              if col, ok := schema[offset]; ok:        // H2：offset 必须命中已知字段
                  return entry.name + "." + col, nil
              // offset 不在 schema：地址范围误命中，视为未命中
      current = current.outerQuery()

  顶层 fallback（v0.8.0 Task 14 修订）：
      从全局 columnNameCache 查找
      若 q.aliases 非空（alias 场景）：返回 "<主表>.<col>"（防多表 JOIN 歧义）
      否则：返回裸列名（v0.7.x 兼容）

  都失败：
      累积 ErrFieldAddrUnregistered，返回 ""
```

**两条防御**：
- **H2 offset 校验**：`addrLow <= addr < addrHigh` 命中区间后，必须再校验 `addr - addrLow` 是已知字段 offset；防止 GC 后地址重用 / 不同类型 size 巧合误命中
- **H5 修订（Task 14 发现）**：原 spec 设计 sub 严格闭合不回退全局 cache，但
  `getModelInstance[T]` 保证 `*T` 规范单例全局唯一——q1 和 q2 的 `&u.ID` 是同一指针，
  跨 Query 误用根本不存在。H5 防御场景架构上不可能发生。

  实际实施允许 sub 路径回退全局 cache，让 correlated subquery 能引用外层规范单例字段
  （这是 correlated subquery 的核心需求）。alias 实例字段仍由链遍历严格隔离（H5 的有效部分保留）。

**关键不变量**：

1. **alias 实例只读**——业务代码绝对不该 `o.Amount = 100`，只用于取字段地址（godoc 警告）
2. **alias name 在 Query 链中唯一**——`As()` 时检查 `current → outerQuery → ...` 全链
3. **alias 实例不入全局缓存**——避免 Query GC 后僵尸条目内存累积
4. **GC 安全**——`aliasEntry.instance any` 持有强引用，Query 存活期间实例不被 GC；`addrLow/addrHigh uintptr` 仅作 key 比较，不跨 GC 失效。`lookupAddr` 在 hot loop 中通过持有 entry 引用确保 instance 存活，无需 `runtime.KeepAlive`（因为 `for _, entry := range q.aliases` 的 entry 本身是值拷贝持有 instance）
5. **解析路径线性扫描**——通常 alias ≤ 5、outerQuery 链 ≤ 3 层，开销可忽略（实测在性能基线验证）

### 3.1.1 术语表

| 术语 | 含义 |
|---|---|
| `outerQueryRef` | `Query[T]` / `Updater[T]` 内 `*queryCore` 的字段，类型 `AnyQuery`，子查询时指向外层 |
| `outerQuery()` | `AnyQuery` 接口暴露的方法（v0.8.0 内部使用），返回 outerQueryRef |
| **outerQuery 链** | 叙述性术语，指 sub → outer → outer's outer → ... 的递归引用链路 |
| **alias 实例** | `gplus.As[X]` 创建的独立 `*X`，字段地址独立于规范单例 |
| **规范单例** | `getModelInstance[X]()` 返回的全局唯一 `*X`，字段地址注册在全局 columnNameCache |
| **alias name** | SQL 中 `AS <name>` 的标识符，如 `o`、`boss` |

### 3.3 类型擦除接口 `AnyQuery` + 内部共享类型 `queryCore`

**设计要点**（M1 + H10 协同）：

- `AnyQuery` 接口仅作 **phantom sentinel**——只暴露 `gplusCore() *queryCore` 一个 unexported method，外部包既无法冒名实现，也无法直接调用内部能力
- `queryCore` 是 unexported 抽象类型，承载 alias 体系的所有共享状态与行为；`Query[T]` 和 `Updater[T]` 各自**内嵌** `*queryCore`
- `queryCore.metadata` 字段预留扩展槽位（v0.8.0 仅 ctx；v0.9+ 可加 routing/tracing/sharding 而不破坏 AnyQuery 接口）

```go
// AnyQuery 是 Query[T] 和 Updater[T] 的 phantom 标签接口
// 业务代码无法实现（gplusCore 返回 unexported 类型）
type AnyQuery interface {
    gplusCore() *queryCore
}

// queryCore 承载 alias 体系的共享状态与方法
// unexported 类型，外部无法直接引用
type queryCore struct {
    aliases       map[string]aliasEntry
    outerQueryRef AnyQuery               // 子查询时指向外层；顶层为 nil
    metadata      coreMetadata           // 扩展槽位：v0.8.0 只用 ctx
    errs          []error
}

// coreMetadata 是横切关注点的扩展容器
// v0.8.0 只放 ctx；v0.9+ 加 routing / tracing / sharding 时不破坏 AnyQuery 接口
type coreMetadata struct {
    ctx context.Context
    // 预留：routing *RoutingHint
    // 预留：tracing *TraceContext
    // 预留：shardKey any
}

// queryCore 内部方法（包内可直接调用，包外不可见）
func (c *queryCore) addAlias(name string, typ reflect.Type, instance any) error
func (c *queryCore) lookupAddr(addr uintptr) (alias, col string, ok bool)
func (c *queryCore) outerQuery() AnyQuery   // 返回 outerQueryRef（提供给伪代码引用）
func (c *queryCore) context() context.Context
func (c *queryCore) appendErr(err error)
func (c *queryCore) getError() error

// Query[T] / Updater[T] 用命名字段持有 *queryCore（N3：不内嵌，避免 method promotion 把 queryCore 的
// unexported 方法泄漏到 Query[T] 的 method set，污染包内调用）
type Query[T any] struct {
    core *queryCore
    // ...其他 Query 特定字段（conditions / selects / joins / orders / ...）
}

func (q *Query[T]) gplusCore() *queryCore { return q.core }

// 同样 Updater[T]
type Updater[T any] struct {
    core *queryCore
    // ...
}
func (u *Updater[T]) gplusCore() *queryCore { return u.core }

// N3 设计后果：包内任何代码访问 queryCore 必须显式走 q.core.xxx() 或 q.gplusCore().xxx()，
// 不会出现 q.outerQuery() / q.appendErr() 这种"看起来像 Query[T] 自己方法"的歧义；
// 未来 v0.9+ 重构 queryCore 内部时，受影响范围严格限于 q.core 引用点

// 编译期断言
var _ AnyQuery = (*Query[struct{}])(nil)
var _ AnyQuery = (*Updater[struct{}])(nil)
```

**包级函数访问内部能力**：

```go
func As[X any](q AnyQuery, alias string) *X {
    core := q.gplusCore()      // 拿到 unexported 类型，但能用其方法
    // core.addAlias(...)
}
```

外部包：拿不到 `*queryCore` 的方法（unexported 类型 + unexported method），但能传 `AnyQuery` 给 gplus 的包级函数——这正是预期使用方式。

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
// 7 种 JoinAs 方法（Query），ON extra 必须参数化（防 SQL 注入）
//
// alias / leftCol / rightCol 接受字段地址，由于 Go method 不能新增类型参数，用 any 接收
// 运行时通过 resolveColumnName 解析；类型不匹配时 SQL 在 GORM 层报错
//
// extraSQL 是可选的额外 ON 条件 SQL 片段（如 "AND o.deleted_at IS NULL AND o.status = ?"）
// extraArgs 是 extraSQL 占位符 ? 对应的参数，**必须**通过此参数传入，禁止 fmt.Sprintf 拼接
// 该参数走 GORM 参数化预编译路径（与 db.Where / db.Joins 的 args 同语义）
func (q *Query[T]) LeftJoinAs (alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Query[T]
func (q *Query[T]) RightJoinAs(alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Query[T]
func (q *Query[T]) InnerJoinAs(alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Query[T]
func (q *Query[T]) OuterJoinAs(alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Query[T]
func (q *Query[T]) FullJoinAs (alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Query[T]

// CrossJoinAs / NaturalJoinAs 无 ON 条件
func (q *Query[T]) CrossJoinAs   (alias any) *Query[T]
func (q *Query[T]) NaturalJoinAs (alias any) *Query[T]

// Updater 镜像（M2 精简）：
//   - JoinAs 仅 LeftJoinAs / InnerJoinAs（UPDATE 中 Cross/Natural/Outer/Full Join 几乎无用）
//   - EXISTS 完整 4 个见 §4.5（Exists/NotExists/OrExists/OrNotExists）
//   - SubQuery 派生：使用包级 SubQuery[X](outer AnyQuery)，Updater 自动满足 AnyQuery
func (u *Updater[T]) LeftJoinAs (alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Updater[T]
func (u *Updater[T]) InnerJoinAs(alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Updater[T]
// 其余 5 种 JoinAs 在 Updater 上**不提供**；如确需复杂 UPDATE JOIN，回退 deprecated 旧 LeftJoin(string)
```

**alias 参数为何用 any 而非 *X**：method 不能引入新类型参数，无法写 `func (q *Query[T]) LeftJoinAs[X any](alias *X, ...)`。运行时通过 `lookupAddr` 校验 alias 实例确实属于当前 q 链。

**⚠️ extraSQL 不可拼接用户输入**（CRITICAL，godoc 须显著警告）：

```go
// ❌ SQL 注入：userInput 来自请求参数，被字符串拼接进 ON 条件
q.LeftJoinAs(o, &o.UserID, &u.ID,
    fmt.Sprintf("AND o.status = '%s'", userInput))     // ← 危险

// ✅ 正确：placeholder + args 走 GORM 参数化
q.LeftJoinAs(o, &o.UserID, &u.ID,
    "AND o.status = ?", userInput)                      // ← 安全
```

extraSQL 字面值在代码 review 时应可见为字符串字面量；任何动态构造（fmt.Sprintf / strings.Join 等）一律视为审计红线。

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
// outer 为 nil 时返回 dud *Query[X]（带预置 ErrSubqueryOuterNil），
// 与 v0.6.0 errs 累积哲学一致；调用方可在最终 q.GetError() 处统一感知，
// 不会因 nil 在链中传递导致 panic
//
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

### 4.7 API 表面统计（M2 精简版）

| 类别 | 数量 |
|---|---|
| Query 新增 | 1 NewQueryAs + 7 JoinAs + 4 EXISTS = **12** |
| Updater 新增 | 2 JoinAs（Left + Inner）+ 4 EXISTS = **6** |
| 包级函数新增 | As + SubQuery + SubQueryAs = **3** |
| Repository 新增 | NewQueryAs = **1**（Updater 通常从 Repository 取 db 后调用 `NewUpdater()`，再加 As() 即可，无需独立 `NewUpdaterAs` 入口） |
| 哨兵错误新增 | **7 个**（不含沿用 ErrSubqueryNil；含 N5 ErrAliasQueryNil + N4 ErrAliasRevoked） |
| **合计新方法/函数** | **22** |
| Deprecated（不删） | Query 7 旧 Join + Updater 7 旧 Join = 14 |

**v0.6.0 既有沿用**：`ErrSubqueryNil`（v0.6.0 引入，本期 EXISTS / NotExists 复用，不重复列入新增）

---

## 5. 数据流详解

### 5.1 Alias 注册（`gplus.As[X](q, "o")`）

```
1. 校验 alias name：
   ^[a-zA-Z_][a-zA-Z0-9_]{0,31}$
   非法 → q.appendErr(ErrAliasInvalidName)，return getModelInstance[X]()（fallback 防 nil）

2. 检查 q 是否 nil（N5 — 与 SubQuery(nil) 处理不同的理由）：
   if q == nil:
       panic(ErrAliasQueryNil)
   // SubQuery(outer) 中 outer==nil 时累积错误返回 dud sub —— 因为 outer 可能来自配置/参数，
   //   是"运行时数据"，应让调用方在 q.GetError() 处统一感知；
   // As(q, name) 中 q==nil 时直接 panic —— 因为 q 必须由 NewQuery 创建并直接传入，
   //   nil 必然是 API 入口编程错误，且无 q.appendErr 入口可挂错误。

3. 检查 name 在 q 链中是否已存在（N6 决策 1B：累积错误，BuildQuery 时短路）：
   current := q
   while current != nil:
       if name in current.aliases:
           q.appendErr(ErrAliasDuplicate)
           return current.aliases[name].instance.(*X)  // 返回首次注册实例
           // 注：BuildQuery 入口检查 len(errs) > 0 强制短路，
           //     重名错误不会被快乐路径生成的 SQL 掩盖
       current = current.outerQuery()

3. 创建独立实例：
   typ      := reflect.TypeOf((*X)(nil)).Elem()
   instance := reflect.New(typ).Interface().(*X)         // 堆分配，Go runtime 不移动堆对象
   addrLow  := uintptr(unsafe.Pointer(instance))         // 仅作 key 比较，不跨 GC
   addrHigh := addrLow + typ.Size()
   // GC 安全说明：aliasEntry.instance（any）持有强引用，
   // entry 存活期间 instance 必活；addrLow/addrHigh 只是该期间的稳定 key

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

`lookupAddr` 实现（由 `*queryCore` 提供）：

```go
func (c *queryCore) lookupAddr(addr uintptr) (alias, col string, ok bool) {
    for _, entry := range c.aliases {
        if entry.addrLow <= addr && addr < entry.addrHigh {
            // N4：检查 revoked — Clear() 后 entry.revoked = true，alias 实例残骸用法在此被拦截
            if entry.revoked {
                // 由调用方 resolveColumnName 累积 ErrAliasRevoked
                return "", "", false
            }
            offset := addr - entry.addrLow
            schema := reflectStructSchema(entry.typ)
            if name, found := schema[offset]; found {       // ← H2：offset 必须在 schema 已知字段中
                return entry.name, name, true
            }
            // 区间命中但 offset 不在 schema：地址范围误命中（GC 重用 / size 巧合），视为未命中
            // 不 return false 提前退出——继续遍历其他 entry（虽然实际不会命中第二个）
        }
    }
    return "", "", false
}
```

**N4 残骸 alias 防御**：
```go
func (q *Query[T]) Clear() *Query[T] {
    // 翻转所有 alias entry 的 revoked 标记 — 用户后续传 &o.Field 调用会被拦截
    for name, entry := range q.core.aliases {
        entry.revoked = true
        q.core.aliases[name] = entry
    }
    // 然后清空 aliases / outerQueryRef / errs / 其他条件...
    q.core.aliases = nil
    q.core.outerQueryRef = nil
    q.core.errs = nil
    // ...
    return q
}
```

注意：`resolveColumnName` 在 lookupAddr 命中 revoked entry 时（区间内但被拒），需在全局 fallback 判断**之前**累积 `ErrAliasRevoked`。具体：

```go
func resolveColumnName(addr uintptr) (string, error) {
    current := q
    for current != nil {
        if alias, col, ok := current.lookupAddr(addr); ok {
            return alias + "." + col, nil
        }
        // N4：检查是否因 revoked 被拦截 — 若是，立即累积 ErrAliasRevoked，不继续 fallback
        if current.hadRevokedHit(addr) {                    // 内部辅助：上次 lookupAddr 是否命中 revoked
            q.core.appendErr(ErrAliasRevoked)
            return "", ErrAliasRevoked
        }
        current = current.outerQuery()
    }
    // H5 修订（Task 14）：原设计要求 sub 严格闭合，实施过程发现该防御场景在架构上不存在
    // （getModelInstance[T] 保证 *T 规范单例全局唯一）。实际允许 sub 回退全局 cache，
    // 让 correlated subquery 引用外层规范单例字段。alias 实例字段地址仍由链遍历严格隔离。
    // ...全局 fallback（sub 和顶层 q 均走此路径）
}
```

**为什么 H2 校验关键**：
- 若 alias 实例 GC 后地址被新分配的对象（不同类型，size 相同）复用，`addr ∈ [low, high)` 仍可能命中已死 entry（虽然 `aliasEntry.instance any` 的强引用通常阻止 GC，但是防御性编码 — 用户在 Clear() 或子查询销毁后仍持有 instance 弱引用是可能的）
- 若用户错误地把字段地址传给 `lookupAddr`（例如 `&someStruct.X`，someStruct 与 alias 实例 size 巧合），区间命中但 offset 完全不在 alias 的 schema 中——必须拒绝

### 5.3 JoinAs 链路

`q.LeftJoinAs(alias, leftCol, rightCol, extraSQL, extraArgs...)`：

```
1. 校验 alias 实例属于 q 链：
   addr := uintptr(reflect.ValueOf(alias).Pointer())
   if not in any aliasEntry of q chain:
       q.appendErr(ErrAliasNotInChain)
       return q  // 跳过，保留链式

2. 解析 leftCol / rightCol：
   leftStr,  _ := q.resolveColumnName(addrOf(leftCol))
   rightStr, _ := q.resolveColumnName(addrOf(rightCol))

3. 构造 joinSQL（仅拼接结构化字面量，绝不拼用户输入）：
   tableName := schemaTableName(typeOf(alias))
   aliasName := lookupAliasName(alias)
   joinSQL   := fmt.Sprintf("LEFT JOIN %s AS %s ON %s = %s",
       quoteIdent(tableName), quoteIdent(aliasName),
       leftStr, rightStr)
   if extraSQL != "":
       joinSQL += " " + extraSQL                  // ← extraSQL 含占位符 ?，不含值

4. 追加到 q.joins，extraArgs 走 GORM 参数化路径（与 db.Where 的 args 同语义）：
   q.joins = append(q.joins, joinInfo{
       query:     joinSQL,
       args:      extraArgs,                      // ← C1：args 单独传给 GORM，不参与字符串拼接
       aliasName: aliasName,
   })

5. applyJoinsAs 时调用 db.Joins(j.query, j.args...)，
   GORM 内部对 j.args 做参数化预编译，与 ? 占位符一一对应
```

生成 SQL 形态（args 走预编译，不入字面量）：

```sql
-- joinSQL 字面量：
LEFT JOIN orders AS o ON o.user_id = users.id AND o.status = ?
                                                              ^
                                                       预编译参数槽位
-- extraArgs：
[]any{"paid"}                                    -- 经 GORM 参数化绑定，绝不进入 SQL 字符串
```

**C1 防御核心**：`joinSQL` 字符串中**永远只有占位符 `?`**，从不出现用户值。`extraArgs` 通过 GORM 的 `db.Joins(query, args...)` 路径传入，与 `db.Where` 同等参数化保护。即使下游写 `q.LeftJoinAs(o, &o.UserID, &u.ID, "AND o.status = ?", userInput)`，`userInput` 也只能进 `extraArgs`，不会拼进字符串。

### 5.4 SubQuery 派生

```go
func SubQuery[X any](outer AnyQuery) (*Query[X], *X) {
    if outer == nil {
        // H4：与 errs 累积哲学一致，返回带预置错误的 dud sub
        // 调用方在 q.GetError() 处统一感知；避免 panic 在链式调用中爆炸
        sub, x := NewQuery[X](context.Background())
        sub.gplusCore().appendErr(ErrSubqueryOuterNil)
        return sub, x
    }
    core := outer.gplusCore()
    ctx  := core.context()                       // 透传 ctx
    sub, x := NewQuery[X](ctx)
    sub.gplusCore().outerQueryRef = outer        // 关键：设置外层引用
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

### 6.1 新增哨兵错误（7 个，不含沿用）

```go
var (
    // alias 名字在 q.aliases 或 outerQuery 链中已存在（决策 1B：累积 + BuildQuery 短路，不再 panic）
    ErrAliasDuplicate = errors.New("gplus: alias name already registered in this query chain")

    // alias name 不符合白名单
    ErrAliasInvalidName = errors.New("gplus: invalid alias name (must match [a-zA-Z_][a-zA-Z0-9_]{0,31})")

    // 字段地址既不在当前 Query 的 aliases，也不在 outerQuery 链，也不在全局规范单例
    ErrFieldAddrUnregistered = errors.New("gplus: field address not registered to any model or alias in this query chain")

    // alias 实例传给了 JoinAs 但该实例从未在当前 q（含 outer 链）注册过
    ErrAliasNotInChain = errors.New("gplus: alias instance does not belong to this query chain")

    // SubQuery(nil) 调用：outer 为 nil（H4：与 errs 累积哲学一致，不再 panic）
    ErrSubqueryOuterNil = errors.New("gplus: SubQuery outer is nil")

    // As(nil, ...) 调用：q 为 nil（N5：API 入口编程错误，仅作 panic 时携带的语义标识）
    ErrAliasQueryNil = errors.New("gplus: As query is nil")

    // alias 实例在 Clear() 后仍被使用（N4：Clear 翻转 revoked，lookupAddr 命中即累积此错误）
    ErrAliasRevoked = errors.New("gplus: alias instance has been revoked by Clear()")
)
```

**沿用 v0.6.0 既有**（不计入新增）：
- `ErrSubqueryNil`（v0.6.0）：用于 `Exists(nil)` / `NotExists(nil)` 等子查询入参为 nil 时

### 6.2 错误累积链路（决策 1B：累积 + BuildQuery 短路代替 panic）

**panic 边界**（仅留给"无 q 句柄可挂错误"或"运行时崩溃"）：

| 触发点 | 行为 | 理由 |
|---|---|---|
| `As(nil, ...)` q 为 nil | **panic** `ErrAliasQueryNil`（N5） | q 必须由 NewQuery 创建直接传入；nil 必然是 API 入口编程错误，且无 q.appendErr 可挂错误 |
| `reflect.New` 失败（OOM 等运行时崩溃） | **panic** | 不可恢复 |

**累积错误 + BuildQuery 短路**（决策 1B 双重防御，与 v0.6.0 哲学一致）：

| 触发点 | 处理 |
|---|---|
| `As[X](q, name)` name 非法 | 累积 `ErrAliasInvalidName`，返回 fallback 实例 |
| `As[X](q, name)` name 重复（决策 1B） | 累积 `ErrAliasDuplicate`，返回**首次注册**的实例；BuildQuery 入口短路 |
| `As` 后 alias 被 Clear 残骸使用（N4） | `lookupAddr` 命中 revoked entry → 累积 `ErrAliasRevoked` |
| `LeftJoinAs(o, ...)` o 不在链 | 累积 `ErrAliasNotInChain`，跳过该 JOIN |
| `resolveColumnName(addr)` 失败 | 累积 `ErrFieldAddrUnregistered`，返回空字符串列名 |
| `Exists(nil)` / `NotExists(nil)` | 累积 `ErrSubqueryNil`（v0.6.0 既有） |
| `Exists(sub)` 时 sub 自身有 errs | 透传到 q.errs |
| `SubQuery(nil)` | 累积 `ErrSubqueryOuterNil`，返回 dud sub（H4） |

**BuildQuery 短路**（强制防御）：

```go
func (q *Query[T]) BuildQuery() *gorm.DB {
    if len(q.core.errs) > 0 {
        // 累积错误存在 → 强制返回带错误的 db，不生成实际 SQL
        // 即使调用方忘记调 GetError()，下游 Find/Scan 也会立即返回错误
        return db.Session(&gorm.Session{}).AddError(q.GetError())
    }
    // ...正常构建路径
}
```

**为什么累积 + 短路代替 panic**：
1. 与 v0.6.0/v0.7.0 错误累积哲学一致（gplus 全库零 panic 设计）
2. 生产 HTTP 服务一个 panic 即使 recover 也污染 goroutine 栈，且日志中 alias 重名 ≠ 业务 panic 难分类
3. BuildQuery 短路提供同等强度的"错误 SQL 不会生成"防御，且支持 `errors.Is(err, ErrAliasDuplicate)` 优雅判断

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

> ⚠️ **DataRule 不会自动应用到 alias 副表**。JOIN 进来的副表如果含敏感数据（如 `tenant_id`），必须采取以下两种合规做法之一：
>
> 1. **首选：在 JoinAs 的 `extraSQL` 参数手写副表的数据权限条件**（参数化形态）：
>    ```go
>    q.LeftJoinAs(o, &o.UserID, &u.ID,
>        "AND o.tenant_id = ?", tenantID)   // ← 副表 tenant 显式过滤
>    ```
> 2. **派生 sub 并显式调用 `sub.DataRuleBuilder()`**，将 sub 作为 JOIN 子查询表（v0.8.0 暂走 deprecated 旧 LeftJoin(string) 兜底）
>
> **N2：不推荐在 `DataRule.Column` 写 alias 前缀（如 `"o.tenant_id"`）。** v0.8.0 起 `DataRule.Column` 仅承诺**主表列**语义；alias 维度 DataRule 待 v0.9 通过新增 `DataRule.Table string` 字段提供。提前在 Column 写 alias 前缀，会让 v0.9 的 cross-table 自动注入逻辑无法机械区分"用户显式" vs "系统注入"——导致兼容性陷阱。
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
    // M3：显式断言子查询 SQL 不泄漏外层 WHERE/SELECT/FROM clauses
    // 若 GORM v1.31.x 实测 leak，需改用 db.Session(&gorm.Session{NewDB:true}).Model(...) 重建
    t.Run("Subquery_NoOuterClauseLeak_AssertedExplicitly", func(t *testing.T) {
        // 1. 外层 q 已积累 WHERE / SELECT
        // 2. 派生 sub 并 ToDB(outerDB.Session(NewDB:true))
        // 3. 断言生成的子查询 SQL 不含外层 WHERE 字面 / 不含外层 SELECT 列
    })
    // M3：extra args 走 GORM 参数化预编译，不入字面 SQL
    t.Run("JoinsWithArgs_ArgsParameterized_NotInlined", func(t *testing.T) {
        // db.Joins("LEFT JOIN ... ON ... AND status = ?", "paid")
        // 断言 DryRun SQL 包含 ? 占位符且不含 'paid' 字面
    })
}
```

升级 GORM 时此测试 fail 第一时间感知行为变化。

### 7.2 单元测试覆盖矩阵

| 测试文件（新增） | 覆盖点 | 子测试估计 |
|---|---|---|
| `alias_test.go` | As 创建 / **重复名累积 ErrAliasDuplicate**（决策 1B） / 非法名 / **As(nil) panic ErrAliasQueryNil**（N5） / 跨 query 复用检测 / 字段地址解析链 / **Clear 重置 aliases 与 outerQueryRef**（M5） / **Clear 后用 alias 实例累积 ErrAliasRevoked**（N4） | 15 |
| `query_joinas_test.go` | 7 种 JoinAs × ON 形态 × **extraSQL 参数化路径**（C1） | 18 |
| `query_subquery_correlated_test.go` | SubQuery 派生 / SubQuery(nil) 累积错误（H4） / 跨层 alias 引用 / 嵌套 sub 3 层 / **sub 引用外层规范单例字段（H5 修订验收）** | 12 |
| `query_exists_test.go` | Exists/NotExists/OrExists/OrNotExists × 简单/相关 sub | 12 |
| `updater_alias_test.go` | Updater 镜像（精简后：LeftJoinAs/InnerJoinAs/SubQuery/Exists） | 8 |
| `query_newqueryas_test.go` | NewQueryAs 主表 alias / 与副表 alias 冲突检测 / SQL 输出 | 8 |
| `alias_datarule_test.go` | DataRule × alias 三种合规模式 + **e2e 反例 RED-locked**（H9：副表敞开必须能被反例测试捕获，否则验收不过） | 7 |
| **合计** | | **80** |

**实现 vs 测试比例**：22 新方法/函数 + 7 新错误哨兵 + Clear 扩展 ≈ 30 实现单元；80 子测试 ≈ 1:2.7（含探针 / 集成 / 反例），符合 v0.6.0 / v0.7.0 节奏。

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
| **P1 内核** | `AnyQuery` 接口（phantom guard）+ `*queryCore` 内部类型；`queryCore.aliases / outerQueryRef / metadata` 字段；`As[X]` 创建函数；`resolveColumnName` 沿链查找（含 H2 offset 校验 / H5 修订：sub 允许回退全局 cache）；5 个新错误哨兵；**Query.Clear() / Updater.Clear() 重置 aliases + outerQueryRef + errs**（M5） | 4-5 | P0 |
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
| **包级泛型 `gplus.LeftJoinAs[L,R](q, alias, *L, *R, extraSQL, args...)`** | **v0.9** | 提供 leftCol/rightCol 编译期类型保证（绕开 method 类型参数限制） |
| 全局 alias（包级 var） | **v0.9 候选** | 模型 B；需 weak ref 设计；YAGNI |
| UNION / WITH CTE / 窗口函数 | **v1.0+** | 与 alias 体系正交，独立立项 |

---

## 9. 风险与缓解

| 风险 | 概率 | 影响 | 缓解 |
|---|---|---|---|
| GORM Joins 字符串模板对 alias 处理与预期不符 | 低 | 高 | P0 探针提前锁定；如失败先调整 SQL 拼装策略 |
| 跨 3 层以上 outerQuery 性能不达标 | 中 | 中 | P9 benchmark 后加 Query 局部缓存（addr → alias 提前算好） |
| DataRule × alias 副表敞开导致下游误用 | 中 | **高（安全）** | godoc + CHANGELOG 双重警告；example 给三种合规写法；security review |
| `any` 类型 leftCol/rightCol 导致运行时类型不匹配 | 中 | 低（SQL 报错快速暴露） | 测试覆盖类型不匹配场景；godoc 示例引导；**v0.9 升级路径**：包级泛型函数 `gplus.LeftJoinAs[L,R any](q, alias, *L, *R, extraSQL, args...)` 提供编译期类型保证（method 受 Go #49085 限制，包级函数可绕开） |
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
- [ ] **DataRule × alias 副表敞开 e2e 测试**（H9 + N1 PASS 条件明确化）：
  - 测试文件 `alias_datarule_e2e_test.go` 含**两段并列断言**，**两段都 PASS** 才算验收通过：
  - **段 A**（锁敞开契约 v0.8.0）：`TestDataRuleAliasContract_NoAutoInjectionToSideTable` —— 构造跨租户场景，未在 JoinAs extraSQL 加副表 tenant 过滤时，**断言能查到他租户副表数据**（即漏洞复现）。该段 PASS 表示 v0.8.0 "副表敞开"契约成立；若未来 v0.9+ 加 cross-table 自动注入，此段会 fail，提醒同步更新策略
  - **段 B**（验合规模式）：`TestDataRuleAliasContract_ExplicitExtraBlocksLeak` —— 在 JoinAs extraSQL 加 `"AND o.tenant_id = ?", tenantID`，**断言无泄漏**。该段 PASS 表示文档推荐的合规写法确实有效
  - 两段并存，CI 机械化判定明确：段 A 不再"漏洞复现成功"= 副表自动注入策略变更；段 B 失败 = 合规写法失效
- [ ] deprecated 旧 Join API 仍可编译，下游升级零破坏
- [ ] AnyQuery 接口的 phantom guard（unexported method `gplusCore() *queryCore`）阻止外部冒名实现
- [ ] `NewQueryAs` / `As` / `SubQuery` 所有错误路径测试覆盖
- [ ] **C1 防御**：`extraSQL` 含 `?` 占位符 + `extraArgs` 参数化路径在 DryRun 测试中可见参数不入字面 SQL
- [ ] **H6 防御（决策 1B）**：`As` name 重复**累积** `ErrAliasDuplicate` + BuildQuery 入口 `len(errs) > 0` 强制短路（不生成 SQL），两条防御均测试覆盖
- [ ] **N4 防御**：`Clear()` 翻转 alias entry.revoked，后续 `lookupAddr` 命中 revoked 累积 `ErrAliasRevoked`，测试覆盖 `TestQuery_Clear_AliasUseAfterClear` 必须 RED 验证
- [ ] **N5 防御**：`As(nil, ...)` panic `ErrAliasQueryNil`，测试覆盖（panic + recover 断言）
- [ ] **H5 修订验收**（Task 14 发现 H5 架构前提不成立）：sub 能引用外层规范单例字段
  （`TestSubQuery_OuterCanonicalSingletonReferenced`）+ 嵌套 3 层正确解析祖父/父/自身
  （`TestSubQuery_NestedThreeLayers`）；alias 字段地址仍由链遍历严格隔离

---

## 11. 不在本期范围

### 11.1 已规划版本

- **EXISTS 子查询里使用 ANY/ALL**：依赖 ANY/ALL 实现；v0.8.1
- **SelectSub**：依赖 GORM Select 嵌套子查询实测；v0.8.1
- **类型安全 ON extra 三元组 / 包级 LeftJoinAs[L,R]**：签名歧义复杂；v0.9
- **全局 alias 包级 var**（模型 B）：需 weak ref；v0.9 候选
- **JOIN 子查询表**（`JOIN (SELECT ...) sub`）：deprecated 旧 Join 暂时兜底；v0.9 单独设计
- **UNION / WITH CTE / 窗口函数**：与 alias 体系正交；v1.0+ 独立立项
- **Repository 直接的 EXISTS 短路**（`repo.ExistsByJoin(...)`）：组合形态太多，先看 v0.8.0 实战需求
- **PostgreSQL `UPDATE ... FROM <table> ...` 子句**（PG 特有 multi-table UPDATE 语法）：v0.8.0 Updater 仅镜像 LeftJoinAs/InnerJoinAs，不覆盖 PG `UPDATE FROM` 这种 UPDATE 主语句体级别的 multi-table 语义；如需，使用 `RawExec` 或 deprecated `LeftJoin(string)` 兜底

### 11.2 已知技术债（v0.8.0 不修，未来评估）

| ID | 技术债 | 影响 | 触发条件 |
|---|---|---|---|
| **TD-1**（**N2 升格语义契约**） | `DataRule.Column` 当前承诺"主表列"语义；godoc 已**明确禁止**用户写 alias 前缀（如 `"o.tenant_id"`）。v0.9 cross-table DataRule 通过新增 `DataRule.Table string` 字段提供。**此项已不是模糊技术债，而是 v0.8.0 锁定的语义契约**——godoc + e2e 反例（H9 段 A）共同保护 | v0.9 决定做 cross-table DataRule 时，加 `Table` 字段，旧 `Column` 语义不破坏 |
| **TD-2** | `outerQueryRef` 是单链表 | CTE / UNION 的 sibling sub 互相引用拓扑无法表达。**phantom guard 红利（N12）**：DAG 升级局限于 queryCore 内部字段重构（`outerQueryRef` 改成 `dag *queryDAG`），Query[T] / Updater[T] / 用户代码全部不感知，不破 AnyQuery 契约 | v1.0 加 CTE/UNION 时 |
| **TD-3** | `lookupAddr` 线性扫描 alias 数组 | 5 alias / 100ns 在 §7.4 阈值内；BI 多维场景（8-15 alias）会到 1μs/解析。**与 H10 metadata 协调（N12）**：先做 sharding metadata（用户可见 API 演进）再做 aliasIndex（纯性能优化），避免单 PR 同时改 queryCore 内部 schema 增加 review 负担 | 单 Query alias 数 ≥ 8 时切 hash map（lookup 抽接口 `aliasIndex`，sliceIndex → mapIndex 不破 API） |
| **TD-4** | alias 实例只读契约不可强制 | reflect / 误用导致字段被改写时 lookupAddr 仍命中，bug 隐藏 | v0.9 候选 debug 模式 sentinel `ErrAliasInstanceMutated` |
| **TD-5** | `joinInfo` 同时承载 raw 与 alias 两种形态 | v1.0 删 deprecated 旧 Join 时需清理；当前先加 `kind` 字段（rawJoin/aliasJoin）显式区分 | v1.0 删旧 Join API 时 |
| **TD-6** | `NewQueryAs` vs `As` 命名不对称 | `q, u := NewQueryAs[User](ctx,"u")` vs `o := As[Order](q,"o")` 读起来两套范式；godoc 解释为"NewQueryAs 等价于 NewQuery + 主表 As" | 命名稳定后不改；v1.0 评估是否合并 |
| **TD-7**（**N11 新增**） | 顶层 q 的 `resolveColumnName` 仍 fallback 全局 columnNameCache | 跨 Query 字段地址巧合静默命中规范单例的极端场景（`q1.Eq(&u_of_q2.X, ...)`），生成 SQL 引用未 FROM 的表，运行时报错而非 gplus 校验拦截。v0.7.x 已有同等行为，不破坏兼容 | v0.9 加 `Query.WithStrictColumnResolution()` 选项 + `ErrFieldAddrCrossModel` 哨兵 |
| **TD-8**（**Task 14 实施发现**） | H5 严格闭合设计的架构前提失效 | spec 原 H5 防御场景"跨 Query 字段地址巧合命中规范单例"不可能发生（`getModelInstance` 全局唯一）。实施已合理放宽 sub 回退全局 cache，让 correlated subquery 工作。spec §3.2 / §5.2 / §10 已同步修订；TD-7 仍有效（v0.9 加 StrictColumnResolution 防 string 列名误用而非地址误用） | 已在 v0.8.0 处理；spec 已同步 |

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

