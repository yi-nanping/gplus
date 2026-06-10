# 类型化表达式投影 + InsertSelect 列映射设计（Expr / Model / InsertSelectMap）

> **状态：已审计修订（2026-06-10 审计闭环），可进入实施。** — 2026-06-10 初稿；同日审计发现 build 期解析与 `GetError()` 检查时序冲突（终端方法先查 errs 再 build，build 期才累积的错误拦不住 SQL），修订为 **SelectExpr 调用期解析**，与现有 `Eq(&sub.X)` 调用期解析（query.go:224 `resolveColumnNameAny`）同构。
>
> **本特性拆轮交付：**
> - **Round 1（独立可交付，一行改动量）**：导出规范单例获取入口 `Model[T]()`。
> - **Round 2（主体）**：最小表达式体系 `Col` / `Lit` / `Add` + `q.SelectExpr`，消灭投影侧裸 SQL 表达式片段。
> - **Round 3（薄封装，依赖 Round 1+2）**：`InsertSelectMap` / `InsertSelectMapTx` 成对列映射，消灭 target/source 顺序对位这一整类错误。
>
> **探针处置（审计后全部消解，无需实施前实测）：**
> 1. ~~GORM 对 Vars 中 clause.Column 的内联渲染~~ → **不再需要**：解析时机前移后，列引用在 `applySelects` 用既有 `quoteColumn(name, qL, qR)` 文本渲染（qL/qR 已是 applySelects 参数，builder.go:355），`Lit` 走既有 args 绑定路径（builder.go:373-389），全程不经过 `clause.Expr`/`clause.Column`。
> 2. ~~builder.go SELECT 渲染点能否拿到 `q`~~ → **问题消失**：解析在 `SelectExpr`（`q` 的方法）调用期完成，selectItem 只存已解析结果，`ScopeBuilder` 不需要 `q`。
> 3. ~~getModelInstance 首次调用自动注册行为~~ → **已有测试解答**：schema_test.go:171 证明自动初始化并可解析，missing_coverage_test.go:131 证明并发安全。AC-3 措辞已据此定稿。

> **日期**：2026-06-10
> **范围**：投影（SELECT 列）侧的类型化表达式 + InsertSelect 列映射 API。**不含** WHERE 侧表达式（`WhereRaw` 保留）、UPDATE SET 侧表达式、`Sub`/`Mul`/函数调用/CASE 等扩展算子。
> **触发来源**：下游 `gvs-server` `internal/domain/organization/closure_repository.go` 闭包表迁移到 `InsertSelect` 后的两处残留裸片段——`SelectRaw("ext.depth + sub.depth + 1")`（表达式列无类型化出口）与 `InsertSelectTx(..., []any{"ancestor_id","descendant_id","depth"}, q)`（规范单例无公开获取入口，被迫用字符串目标列）。

---

## Goal

让闭包表自连接搬移这类 `INSERT ... SELECT ... JOIN` 写操作做到**零手写 SQL 字符串**：

1. 表达式列（`depth + depth + 1`）用类型化算子树表达，字段引用走字段指针（alias 解析链路），字面量走绑定参数；
2. InsertSelect 目标列用字段指针表达（导出 `Model[T]()`）；
3. target/source 成对声明，列数不匹配与顺序错位从"运行时数据错"提升为"结构上不可能"。

**最小算子集（YAGNI）**：只实现 `Add`（变长 `+`）。下游唯一真实需求是闭包表 depth 算术；`Sub`/`Mul`/函数留待真实调用点出现再扩展。

## Acceptance Criteria

> 每条 AC 含具体输入值 + 具体可观测输出，1:1 对应测试函数。方言验证用 SQLite in-memory（与现有测试一致）。测试模型复用 `Closure{AncestorID, DescendantID, Depth uint}`（表 `closure`，列 `ancestor_id`/`descendant_id`/`depth`），`D=uint`。

### Round 1 — Model[T]

- **AC-1**（单例一致性 + 并发）：`m := Model[Closure]()` 返回非 nil；与 `r.NewQuery(ctx)` 返回的规范单例为**同一指针**；包级 `resolveColumnName(&m.Depth)` 返回 `("depth", nil)`。同一测试内附 `-race` 并发段：100 goroutine 并发调用全部返回同一指针（底层 `getModelInstance` 并发安全已有 missing_coverage_test.go:131 覆盖，此处仅冒烟确认导出包装无额外逻辑）。
- **AC-2**：~~独立并发测试~~ 并入 AC-1（审计 M-2：与现有覆盖重复，单行包装不值独立测试）。
- **AC-3**（首次调用自动注册）：对此前从未经 `NewQuery`/`RegisterModel` 触达的新类型 `FreshModel`，`Model[FreshModel]()` 返回可用单例，随后 `resolveColumnName(&fresh.Field)` 返回 `(列名, nil)`（`getModelInstance` 慢路径在 `modelInitMu` 保护下自动注册，行为已被 schema_test.go:171 证实）。

### Round 2 — Expr（Col / Lit / Add + SelectExpr）

> **AC-4/5/6 取值方式**：表达式列无 AS 别名，按名映射的 `FindAs` 不适用（限制见 Out of Scope）。in-package 测试以 `setupTestDB` 的 db 应用 `q.BuildQuery()` scope 后，GORM 单列 `Scan` 到标量（位置扫描）取值。

- **AC-4**（单表表达式 + Lit 绑定）：`closure` 含 1 行 `{1,5,0}`。`q.SelectExpr(Add(Col(&m.Depth), Lit(1)))` + `Eq(&m.DescendantID, 5)`，查询标量结果 = `1`（0+1）；DryRun 断言 Vars 含 `1`（字面量走绑定参数，不拼进 SQL 文本）。
- **AC-5**（alias 场景列解析）：`NewQueryAs(ctx,"ext")` + `As[Closure](q,"sub")` + `CrossJoinAs`，数据 `{1,5,0}`、`{5,7,0}`，条件 `Eq(&sub.AncestorID,5)`、`Eq(&ext.DescendantID,5)`。`SelectExpr(Add(Col(&ext.Depth), Col(&sub.Depth), Lit(1)))` 查询结果 = `1`（0+0+1），证明两个字段指针分别解析为 `ext.depth` / `sub.depth`（错配会因列歧义或值错而失败）。
- **AC-6**（Lit 注入防御）：`Lit("1; DROP TABLE closure;--")` 作为投影值执行后：`closure` 表仍存在、行数不变，查询返回值 = 该字符串本身（证明走绑定而非文本拼接）。
- **AC-7**（未注册地址）：`Col(&外部局部struct.Field)`（地址不在任何 alias 区间也不在全局 cache）→ **`SelectExpr` 调用期**经 `resolveColumnNameAny` 解析失败，`ErrFieldAddrUnregistered` 立即累积；终端方法（List/InsertSelect）的 `GetError()` 前置检查拒绝执行，不发 SQL（解析必须在调用期完成——终端方法先查 errs 再 build，build 期才累积的错误拦不住 SQL，见状态栏审计记录）。
- **AC-8**（revoked alias）：先 `Clear()` 再 `SelectExpr(Add(Col(&ext.Depth), Lit(1)))` → 调用期解析命中 revoked 区间，`ErrAliasRevoked` 累积到 `q.GetError()`，不发 SQL。
- **AC-9**（投影计数兼容）：一次 `SelectExpr` 恰好向 `q.selects` 追加 1 个 selectItem；与 `Select`/`SelectRaw` 混用时 `len(q.selects)` 计数正确，`InsertSelect` 现有列数校验（`ErrInsertSelectColMismatch`）行为不变。
- **AC-10**（空 Add 拒绝）：`SelectExpr(Add())`（0 操作数）→ `SelectExpr` 调用期检出 `ErrExprEmpty` 累积到 `q.GetError()`，不追加 selectItem，不发 SQL。

### Round 3 — InsertSelectMap

- **AC-11**（端到端闭包搬移，对照 insert-select-join AC-1）：数据 `{1,5,0}`、`{5,7,0}`。执行
  ```go
  q, ext := r.NewQueryAs(ctx, "ext")
  sub := As[Closure](q, "sub")
  q.CrossJoinAs(sub).Eq(&sub.AncestorID, 5).Eq(&ext.DescendantID, 5)
  m := Model[Closure]()
  affected, err := InsertSelectMap(r, ctx, []InsertCol{
      {Target: &m.AncestorID,   Src: Col(&ext.AncestorID)},
      {Target: &m.DescendantID, Src: Col(&sub.DescendantID)},
      {Target: &m.Depth,        Src: Add(Col(&ext.Depth), Col(&sub.Depth), Lit(1))},
  }, q)
  ```
  结果：`(1, nil)`；新增行**逐字段断言** `{ancestor_id:1, descendant_id:7, depth:1}`，总行数 3。
- **AC-12**（与手动投影互斥）：`q` 已调用过 `Select`/`SelectRaw`/`SelectExpr`（`len(q.selects)>0`）再传入 `InsertSelectMap` → 返回 `(0, ErrInsertSelectMapConflict)`，不发 SQL（投影由映射 API 独占设置，杜绝双源）。
- **AC-13**（Target 解析失败 + 零副作用）：3 对映射中第 2 对 `Target` 传未注册地址 → `(0, ErrColumnNotFound)` 传播，不发 SQL，**且 `len(q.selects) == 0`**（守卫/解析失败零追加，`q` 未被污染可复用——先全量解析所有 Target 与 Src，全部成功后才统一追加，见 Round 3 数据流）。
  > **错误码说明（实施实测对齐）**：Target 是目标表 T 的全局单例字段，走**包级 `resolveColumnName`**（schema.go:119），未注册返回 `ErrColumnNotFound`。这与 Round 2 AC-7 的 `ErrFieldAddrUnregistered` 不同——后者是 Col 经 src 的 **alias 链解析**（`resolveColumnNameAny`→query.go:1445）的错误。两路径错误码不同是设计使然：Target 不经 src alias 链，正好令 Target 解析失败不污染 src.errs（故 `q` 可复用）。若失败的是 **Src 的 Col**（经 alias 链），返回 `ErrFieldAddrUnregistered` 且会污染 src.errs（`q` 不可复用）。
- **AC-14**（空映射）：`cols == nil` 或 `len==0` → `(0, ErrInsertSelectColMismatch)`（复用现有 sentinel），不发 SQL。
- **AC-15**（事务回滚）：`InsertSelectMapTx(r, ctx, tx, cols, q)` 返回 `(1, nil)` 后 `tx.Rollback()` → 目标表行数回到初始值。

## Architecture

### 变更面（最小）

| Round | 文件 | 变更 |
|---|---|---|
| 1 | `schema.go` | 导出 `Model[T any]() *T`（包装现有 `getModelInstance`，schema.go:174）+ doc 注释 |
| 2 | `expr.go`（新建） | `Expr` 接口 + `Col` / `Lit` / `Add` 构造函数 + `ErrExprEmpty` |
| 2 | `query.go` | `SelectExpr(e Expr)` 方法：**调用期**经 `q.resolveColumnNameAny` 解析全部 Col 地址（错误立即累积 q.errs），解析成功后追加已降解的 selectItem |
| 2 | `builder.go` | `selectItem` 加已解析变体（`exprParts []exprPart`，part = 已解析列名 或 字面量）；`applySelects` 渲染：列名经既有 `quoteColumn(name, qL, qR)`，字面量渲染 `?` 并入 args 绑定路径（builder.go:373-389 既有机制），不引入 `clause.Expr` |
| 3 | `repository.go` | `InsertSelectMap` / `InsertSelectMapTx` + `InsertCol` 类型 + `ErrInsertSelectMapConflict` |

### API 签名

```go
// ---- Round 1（schema.go）----
// Model 返回 T 的规范单例（字段地址注册于全局 columnNameCache）。
// 用途：InsertSelect targetCols / InsertSelectMap Target 的字段指针来源。
// ⚠️ 单例是全局共享只读锚点，禁止写其字段值。
func Model[T any]() *T

// ---- Round 2（expr.go）----
// Expr 投影表达式节点。实现：colRef / litVal / addExpr。
// 操作数显式构造（Col/Lit），不做 any 推断——字符串操作数存在
// "列名 vs 字符串字面量"歧义，显式构造杜绝整类误用。
type Expr interface{ exprNode() }

func Col(fieldPtr any) Expr   // 字段引用：构造期仅存地址，SelectExpr 调用期经 q.resolveColumnNameAny 解析（支持 alias 实例地址）
func Lit(val any) Expr        // 字面量：渲染为 ? 绑定参数，永不拼入 SQL 文本
func Add(operands ...Expr) Expr // 变长 +：渲染为 "<col> + ? + ..."；0 操作数 → SelectExpr 期检出 ErrExprEmpty

// query.go
// SelectExpr 追加 1 个投影列（计数与 Select/SelectRaw 一致）。
// 调用期立即解析全部 Col 地址并累积错误（与 Eq 等条件方法同构）——
// 因此引用 alias 实例字段时，As/NewQueryAs 必须先于 SelectExpr 调用（与 Eq(&sub.X) 的既有约束一致）。
func (q *Query[T]) SelectExpr(e Expr) *Query[T]

// ---- Round 3（repository.go）----
// InsertCol 目标列与源表达式的成对映射
type InsertCol struct {
    Target any  // 目标表字段指针（Model[T]() 单例字段）；也接受合法标识符字符串（与 InsertSelect 同规则）
    Src    Expr // 源表达式：Col（单列）或任意 Expr 组合
}

// InsertSelectMap 成对列映射版 InsertSelect。
// q 必须未设置任何投影（由本 API 独占设置）；内部按 cols 顺序逐对
// 追加 Src 到 q.selects、解析 Target 到目标列，再复用 InsertSelectTx 主流程。
func InsertSelectMap[T any, S any, D comparable](
    r *Repository[D, T], ctx context.Context,
    cols []InsertCol, src *Query[S],
) (int64, error)

func InsertSelectMapTx[T any, S any, D comparable](
    r *Repository[D, T], ctx context.Context, tx *gorm.DB,
    cols []InsertCol, src *Query[S],
) (int64, error)
```

### Round 2 数据流（SelectExpr 渲染）

```
SelectExpr(Add(Col(&ext.Depth), Col(&sub.Depth), Lit(1)))
  1. 构造期：Col/Lit/Add 仅存操作数树，不解析地址（Col 是自由函数，手上没有 q）
  2. SelectExpr 调用期（q 在作用域内，alias 注册表可用）：
       Col(addr)  → q.resolveColumnNameAny(addr) → "ext.depth"（已解析列名）
       Lit(1)     → 字面量 part（build 期渲染 ? 并入 args）
       Add(...)   → 解析全部操作数；任一失败 → 错误立即累积 q.errs，不追加 selectItem
       全部成功   → append selectItem{exprParts: [...]}（计 1 列）
  3. build 期（applySelects，qL/qR 已在参数中，builder.go:355）：
       列名 part  → quoteColumn("ext.depth", qL, qR) → "ext"."depth"（既有 table.col 递归转义）
       字面量 part → SQL 渲染 "?"，值并入 flatArgs（builder.go:373-389 既有绑定路径）
       整体        → `"ext"."depth" + "sub"."depth" + ?`，args=[1]
  4. 错误时序保证：解析错误（ErrFieldAddrUnregistered / ErrAliasRevoked / ErrExprEmpty）
     在调用期已进 q.errs → 终端方法的 GetError() 前置检查必然拦截，不发 SQL。
     （若解析放在 build 期：终端方法先查 errs 再 build，错误拦不住 SQL——这正是审计否决原方案的原因）
```

### Round 3 数据流（InsertSelectMap）

```
InsertSelectMap(r, ctx, cols, src) / ...MapTx(..., tx, ...)
  1. 守卫：src==nil → ErrQueryNil；src.GetError()!=nil → 原样返回
  2. 守卫：len(src.selects)>0 → ErrInsertSelectMapConflict（投影独占）
  3. 守卫：len(cols)==0 → ErrInsertSelectColMismatch
  4. 全量解析（fail-fast，零追加）：先解析所有 Target（字段指针→**包级** resolveColumnName，
     未注册→ErrColumnNotFound，不污染 src.errs / 字符串→validDataRuleColumn 白名单）与所有
     Src 表达式（Col 地址经 src 的 resolveExprItem→resolveColumnNameAny，未注册→ErrFieldAddrUnregistered
     且污染 src.errs），任一失败立即返回错误，src.selects 不被触碰（AC-13 零副作用）
  5. 全部成功后统一追加：已解析 Src 逐个进 src.selects —— target/source 数量天然相等、
     顺序天然对位
  6. 复用 InsertSelectTx 既有主流程（守卫/转义/裸 ? 内联/Exec）。
     注：src 用了 Distinct/Omit 时由复用守卫报 ErrInsertSelectModifier（repository.go:1118）
```

### 设计决策

1. **操作数显式构造（Col/Lit），不做 any 自动推断**：`Add(&ext.Depth, 1)` 形式虽然更短，但字符串操作数无法区分"列名"与"字符串字面量"，且裸指针与字面量指针无法可靠区分。显式构造把歧义消灭在 API 形状上，代价是调用略长——闭包表场景一年写一次，可接受。
2. **InsertSelectMap 投影独占（AC-12）**：若允许 `q.Select*` 与 cols 混合，列对位保证立即失效（混入的投影破坏 cols 顺序映射），整个 API 的存在意义被掏空。互斥守卫是该 API 的核心不变式。
3. **Expr 只进投影侧**：WHERE 侧已有完整类型化条件（Eq/Ge/In/...），残余需求（函数条件等）由 `WhereRaw` 覆盖；UPDATE SET 侧表达式是独立特性（updater.go 体系），不混入本轮。
4. **InsertSelectMap 成功路径会变更调用方 `q`（追加投影）**：与 `InsertSelectTx` 不改 src 的行为不同，须在 doc 注释显式说明。副作用同时构成天然防重入——同一 `q` 二次调用必撞 `ErrInsertSelectMapConflict`（AC-12 守卫），不会静默重复插入。失败路径对 `q.selects` 零副作用（阶段 A 不追加）；但**仅 Target 解析失败时 `q` 可复用**（包级解析不碰 src.errs），**Src 表达式解析失败会污染 src.errs，`q` 不可复用须新建**（code review I-2 校正）。
5. **不提供 `ColE` 别名导出**（审计 M-3 删除）：与 `Col` 同实现的双导出名只增加 API 表面积并造成调用方风格分裂，`InsertCol.Src` 直接用 `Col`。

## Error Handling

新增 sentinel（与现有 `ErrXxx` 风格一致）：

```go
var (
    // expr.go
    ErrExprEmpty = errors.New("gplus: expression requires at least one operand")
    // repository.go
    ErrInsertSelectMapConflict = errors.New("gplus: InsertSelectMap requires source query without manual Select/SelectRaw/SelectExpr projection")
)
```

复用现有：`ErrQueryNil` / `ErrColumnNotFound`（Target 包级解析未注册）/ `ErrFieldAddrUnregistered`（Src/Col 经 alias 链未注册）/ `ErrAliasRevoked` / `ErrInsertSelectColMismatch` / `ErrInsertSelectColInvalid` / `ErrInsertSelectModifier`（src 用 Distinct/Omit 时经复用主流程报出）。

## 方言

- 列引用经既有 `quoteColumn(name, qL, qR)` 按方言引号渲染（与 Select/Group 同机制，无新转义路径）；字面量全部走绑定参数，无方言文本差异
- SQLite in-memory 为主测试方言；MySQL 本机可选手动回归（PG 本机不可用，DM / Oracle 无环境）
- **方言残留风险（无法本机覆盖，如实声明）**：`+` 算术为 SQL 标准算子，无方言分支预期；DM 的标识符大小写参数残留项与 insert-select spec 同款

## 下游迁移收益（gvs-server）

落地后 `closure_repository.go` 两条 SQL 的最终形态（零手写 SQL 字符串）：

```go
// sqlMoveSubtreeStep2Insert 替代（子树搬移 step2）
// ext/sub 是 alias 实例（地址解析为 "ext.col"/"sub.col"），target 列必须用 Model 单例字段指针
q, ext := r.NewQueryAs(ctx, "ext")
sub := gplus.As[organization.OrgClosure](q, "sub")
q.CrossJoinAs(sub).Eq(&sub.AncestorID, rootID).Eq(&ext.DescendantID, newParentID)
m := gplus.Model[organization.OrgClosure]()
gplus.InsertSelectMapTx(r, ctx, tx, []gplus.InsertCol{
    {Target: &m.AncestorID,   Src: gplus.Col(&ext.AncestorID)},
    {Target: &m.DescendantID, Src: gplus.Col(&sub.DescendantID)},
    {Target: &m.Depth,        Src: gplus.Add(gplus.Col(&ext.Depth), gplus.Col(&sub.Depth), gplus.Lit(1))},
}, q)

// sqlCopyAncestorMappings 替代（祖先链复制，单表无 JOIN）
// 片段自洽可复制：NewQuery 返回的 m2 即规范单例（与 Model[T]() 同一指针，AC-1），
// 无 alias 场景下 Target 与 Src 可共用 m2，无需额外 Model[T]() 调用
q2, m2 := r.NewQuery(ctx)
q2.Eq(&m2.DescendantID, parentID)
gplus.InsertSelectMapTx(r, ctx, tx, []gplus.InsertCol{
    {Target: &m2.AncestorID,   Src: gplus.Col(&m2.AncestorID)},
    {Target: &m2.DescendantID, Src: gplus.Lit(childID)},
    {Target: &m2.Depth,        Src: gplus.Add(gplus.Col(&m2.Depth), gplus.Lit(1))},
}, q2)
```

漂移防护对比：现状裸 SQL 整条无防护 → Round 1-3 后所有列引用走字段指针（改列名编译期/构建期报错），唯一残余假设是 `+` 算术语义本身。

## Out of Scope

- `Sub` / `Mul` / `Div` / SQL 函数（`COALESCE` 等）/ CASE 表达式——无真实下游调用点，出现再扩展
- **SelectExpr 的 AS 别名**（`SelectExprAs(e, "total")`）——表达式列无别名，普通读路径按名映射（`FindAs`/`FindOneAs`）不可用，只能单列位置扫描取值；本轮 SelectExpr 服务 InsertSelect 投影（按位置对位，无需列名），读路径别名留待真实需求
- WHERE / HAVING / ORDER BY 侧表达式（现有 `WhereRaw` / `OrderRaw` 保留）
- UPDATE SET 侧表达式（updater.go 体系独立立项）
- `InsertSelect`（字符串/指针 targetCols 版）的废弃——保留并存，Map 版是增强不是替代
