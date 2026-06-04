# InsertSelect — 跨表写时单表（`INSERT ... SELECT`）设计

> **状态：Round 2（已修订，可进 plan）** — 2026-06-04 修订并入 5 视角审计 6 项 must-fix + 用户范围决策。
>
> **本特性拆轮交付：**
> - **Round 1（已完成）**：`SelectRaw(args)` 参数绑定 → `2026-06-04-selectraw-args-design.md`。已合入（commit 806a8b4..413c142）。InsertSelect 依赖它。
> - **Round 2（本文档）**：InsertSelect 本体，**只覆盖 scenario 1（基础 `INSERT ... SELECT`，单表无 JOIN）**。
> - **Round 3（推迟）**：scenario 2 自连接 `INSERT ... SELECT ... JOIN`。推迟原因：实测 `NewQueryAs` 的主表别名在 `ToDB` 物化路径会丢失（`FROM \`test_users\`` 不带 `AS ext`，而 SELECT 引用 `"ext".col` → 真库报别名未定义），需先单独修 `ToDB` 主别名应用，属独立 gap，不与 Round 2 耦合。
>
> **修订已并入的 must-fix（原稿前提已被实测推翻处均已改写）：**
> 1. ✅ 删除 DryRun/Statement 物化整段 → 改一行 `r.dbResolver(ctx,tx).Exec(prefix+"?", src.ToDB(db))`。**已实测**：裸 `?` 内联子查询不产生 `(SELECT...)` 外层括号，Vars 顺序 = SELECT 投影参数在前、WHERE 参数在后。
> 2. ✅ 泛型签名 `[D,T,S any]` → `[T any, S any, D comparable]`（对齐全库 `Pluck[T,R,D comparable]`/`FindAs[T,Dest,D comparable]` 惯例）；D/T 从 `*Repository[D,T]`、S 从 `*Query[S]` 推断，调用可省略全部类型参数。
> 3. ✅ `targetCols` 原始字符串列名走 `validDataRuleColumn` 白名单 + `quoteColumn`（实测 `resolveColumnNameAny` 对字符串只查空不校验，query.go:193-198）；自拼 `INSERT INTO <table>(<cols>)` 的 table/cols 经 `getQuoteChar` 方言转义；新增注入复现 AC-8。
> 4. ✅ scenario 2 自连接推迟 Round 3（见上）。Round 2 不含 JOIN 场景。
> 5. ✅ 源 query 守卫拒绝 `Distinct`/`Omit`（`Distinct` 会向 `src.selects` 追加 selectItem 污染投影列计数）；新增边界 AC-9（modifier 拒绝）/AC-10（0 命中 happy-path）/AC-11（ctx 取消）/AC-12（空/nil targetCols）。
> 6. ✅ AC-5 改正向断言（裸 `?` 内联无外层括号 + SQLite 真实 Exec 成功）；sentinel 计数对齐为实际 3 个。
>
> 完整审计：6 agent / 539k tokens，task `wjojuebgk`。

> **日期**：2026-06-04
> **范围**：给 gplus 补 scenario 1 的 `INSERT ... SELECT`（单表写时多表，闭包表祖先链复制）。依赖 Round 1 的 `SelectRaw(args)`（已就绪）。
> **触发来源**：下游 `gvs-server` 的 `docs/dev/gplus-raw-sql-feedback.md` §A1 实测——闭包表维护是唯一真实能力缺口；scenario 1（`CopyAncestorMappings`，closure_repository.go:17,57）为本轮目标调用点。

---

## Goal

让使用方用 query builder 表达「以一个 SELECT 查询作为 INSERT 数据源」的单表写操作（无 JOIN），消除闭包表祖先链复制（scenario 1）对 `RawExec` + 手写 SQL 的依赖。**不扩大范围**：A2/A3/A4/A5/B 已可用 v0.8.0 表达（反馈文件 §D 实测）；scenario 2 自连接推迟 Round 3。

## Acceptance Criteria

> 每条 AC 含具体输入值 + 具体可观测输出，1:1 对应测试函数。方言验证用 SQLite in-memory（与现有测试一致）。测试用一张闭包形态模型 `Closure{AncestorID, DescendantID, Depth uint}`（列 `ancestor_id`/`descendant_id`/`depth`），主键 `D=uint`。`m` 为 `getModelInstance[Closure]()` 规范单例（经 `r.NewQuery(ctx)` 返回）。

- **AC-1**（基础 INSERT...SELECT，scenario 1）：`closure` 含 1 行 `{ancestor_id:1, descendant_id:5, depth:0}`。执行
  `InsertSelect(r, ctx, []any{&m.AncestorID, &m.DescendantID, &m.Depth}, src)`，
  其中 `src` = `r.NewQuery(ctx)` 后 `src.SelectRaw("ancestor_id").SelectRaw("?", 9).SelectRaw("depth + 1").Eq(&m.DescendantID, 5)`。
  结果：返回 `(int64(1), nil)`；`closure` 表**新增 1 行** `{ancestor_id:1, descendant_id:9, depth:1}`，原行保留（总行数 2）。
- **AC-2**（列数不匹配）：`targetCols` 长度=3，`src` 仅 2 个投影（`SelectRaw("ancestor_id").SelectRaw("depth")`）→ 返回 `(int64(0), ErrInsertSelectColMismatch)`，不执行任何 SQL（表行数不变）。
- **AC-3**（无投影拒绝）：`src` 未调用任何 `SelectRaw`/`Select`（`len(src.selects)==0`）→ 返回 `(int64(0), ErrInsertSelectNoProjection)`，拒绝退化成 `INSERT ... SELECT *`。
- **AC-4**（nil / builder 错误传播）：`src == nil` → 返回 `(int64(0), ErrQueryNil)`；`src` 上用非法字段指针（如 `src.SelectRaw("x").Eq(&otherStruct.Field, 1)` 使 `src.GetError() != nil`）→ 该错误原样返回，`(int64(0), <该错误>)`，不执行 SQL。
- **AC-5**（裸 ? 内联，无外层括号，执行式断言）：`InsertSelect` 返回 `(int64, error)` 不暴露 SQL 串，故无外层括号由**真实执行**证明——AC-1 同输入下 SQLite Exec 成功且插入期望行 `{1,9,1}`，即证明 SELECT 未被外层括号包裹（SQLite 对 `INSERT INTO t (cols) (SELECT...)` 报 `near "(": syntax error`，若内联带外层括号 AC-1 必失败）。Vars 顺序 `[9, 5]`（投影绑定 9 在前、WHERE 绑定 5 在后）由 Round 1 双路径保证。**已实测**。本条与 AC-1 同测试函数覆盖（见 plan AC 映射）。
- **AC-6**（事务变体 + 回滚）：`InsertSelectTx(r, ctx, tx, cols, src)` 在传入 `tx` 上执行返回 `(1, nil)`；随后 `tx.Rollback()` → 目标表无新增行（仍为初始行数）。
- **AC-7**（不注入 DataRule）：ctx 中存在匹配源表的 `DataRule`（如 `{Column:"depth", ...}` 过滤掉 `depth=0` 行）时，`InsertSelect` 生成的 SELECT **不含**该隔离条件——插入行数 = 无 DataRule 时的全量。验证：同一 ctx 下 `r.List(srcList)` 受 DataRule 过滤（行数减少），而 `InsertSelect` 源 SELECT 不过滤（插入全量）。
- **AC-8**（targetCols 注入防御）：`targetCols` 含原始字符串恶意 payload `"id) ; DROP TABLE closure; --"`（非字段指针）→ 经 `validDataRuleColumn` 白名单拒绝，返回 `(int64(0), ErrInsertSelectColInvalid)`，不执行任何 SQL（`closure` 表仍存在且行数不变）。
- **AC-9**（modifier 拒绝）：`src` 调用了 `Distinct(&m.AncestorID)`（向 `src.selects` 追加 selectItem 污染计数）或 `Omit(...)` → 返回 `(int64(0), ErrInsertSelectModifier)`，不执行 SQL。
- **AC-10**（0 命中 happy-path）：`src` 的 WHERE 匹配 0 行（如 `Eq(&m.DescendantID, 99999)`）→ 返回 `(int64(0), nil)`，无错误，目标表无新增行。
- **AC-11**（ctx 取消）：传入已 `cancel()` 的 ctx → `Exec` 返回 `context.Canceled`（原样透传到返回 error），目标表行数不变。
- **AC-12**（空/nil targetCols）：`targetCols == nil` 或 `len==0`，而 `src` 有投影 → 返回 `(int64(0), ErrInsertSelectColMismatch)`，不执行 SQL。

## Architecture

### 变更面（最小）

| 文件 | 变更 |
|---|---|
| `repository.go` | 新增包级泛型 `InsertSelect` / `InsertSelectTx`；新增 4 个 sentinel 错误（`ErrInsertSelectColMismatch` / `ErrInsertSelectNoProjection` / `ErrInsertSelectColInvalid` / `ErrInsertSelectModifier`）；`src==nil` 复用现有 `ErrQueryNil` |

> sentinel 计数说明：实际新增 4 个（ColMismatch / NoProjection / ColInvalid / Modifier），`ErrQueryNil` 复用现有。原稿"3 个"是 must-fix #5 加 modifier 守卫前的旧值，据实校正为 4。

### API 签名

```go
// T=目标表模型, S=源 Query 模型（scenario 1 自插入 S==T）, D=主键类型
// D/T 从 *Repository[D,T]、S 从 *Query[S] 推断，调用可省略全部类型参数。
func InsertSelect[T any, S any, D comparable](
    r *Repository[D, T], ctx context.Context,
    targetCols []any, src *Query[S],
) (int64, error)

func InsertSelectTx[T any, S any, D comparable](
    r *Repository[D, T], ctx context.Context, tx *gorm.DB,
    targetCols []any, src *Query[S],
) (int64, error)
```

### 数据流

```
InsertSelect(r, ctx, targetCols, src) / InsertSelectTx(..., tx, ...)
  1. 守卫：src==nil → ErrQueryNil
  2. 守卫：src.GetError()!=nil → 原样返回
  3. 守卫：src.distinct==true 或 len(src.omits)>0 → ErrInsertSelectModifier
  4. 投影列数 = len(src.selects)；==0 → ErrInsertSelectNoProjection
  5. 解析 targetCols（[]any）→ []string：
       - 字段指针：包级 resolveColumnName(ptr)（全局 cache，安全）
       - 原始字符串：先 validDataRuleColumn.MatchString 白名单，失败 → ErrInsertSelectColInvalid
  6. 守卫：len(targetCols)==0 或 len(targetCols)!=投影列数 → ErrInsertSelectColMismatch
  7. exec := r.dbResolver(ctx, tx)
  8. 取目标表名：aliasSchemaTableName(reflect.TypeOf(*new(T)))（处理 TableName() + 命名规则）
  9. 方言转义：qL,qR := getQuoteChar(exec)；表名、各列名用 qL+name+qR 包裹
 10. 组装前缀："INSERT INTO " + <qtable> + " (" + join(<qcols>, ",") + ") "
 11. res := exec.Exec(prefix + "?", src.ToDB(exec))   // 裸 ? 内联，无外层括号（已实测）
 12. 返回 (res.RowsAffected, res.Error)
```

### 投影列数判定

`Query` 把 `SelectRaw`/`Select`/`Distinct` 累积到 `src.selects []selectItem`（Round 1 后结构）。投影列数 = `len(src.selects)`。`Distinct` 会追加 selectItem 污染此计数，故步骤 3 守卫拒绝 `distinct`。**一个 `SelectRaw` 对应一个目标列**——`SelectRaw("a, b")` 单串多列会被算作 1 列，文档需提示多列拆多次（scenario 1 为逐列写法，不受影响）。

### 裸 ? 物化与括号安全（AC-5）

`src.ToDB(exec)` 返回带条件的 `*gorm.DB`；作为 Var 传入 `exec.Exec("... ?", subDB)` 时 GORM 内联其 SQL **不加外层括号**。**端到端实测**（SQLite，真实 Exec，已删探针）：
```
prefix   = INSERT INTO "probe_closure" ("ancestor_id","descendant_id","depth")
affected = 1, err = <nil>
rows     = {1,5,0}(原) + {1,9,1}(新)
```
故无需 DryRun 取 SQL 再手拼（原稿方案删除），一行 `Exec` 即可。`InsertSelect` 不暴露 SQL 串，无外层括号由真实插入成功反证（见 AC-5）。

## Error Handling

新增 sentinel（`repository.go`，与现有 `ErrXxx` 风格一致）：

```go
var (
    ErrInsertSelectColMismatch  = errors.New("gplus: InsertSelect target column count does not match source projection count")
    ErrInsertSelectNoProjection = errors.New("gplus: InsertSelect source query has no Select/SelectRaw projection")
    ErrInsertSelectColInvalid   = errors.New("gplus: InsertSelect target column name is not a valid identifier")
    ErrInsertSelectModifier     = errors.New("gplus: InsertSelect source query must not use Distinct/Omit")
)
```
- `src==nil` 复用现有 `ErrQueryNil`。
- `src.GetError()` 非 nil（非法字段指针等）→ 原样返回，不吞。
- 所有守卫在 `Exec` 之前完成，失败时保证目标表零副作用（AC-2/3/4/8/9/12）。
- ctx 取消（AC-11）由 `exec.Exec` 返回 `res.Error`（`context.Canceled`）原样透传。

## Testing

- 测试文件：新增 `insert_select_test.go`（package gplus，可访问未导出符号 `src.selects`/`validDataRuleColumn` 等）。
- 用 `setupTestDB[Closure]` 风格建内存 SQLite + 自动迁移 `Closure{AncestorID, DescendantID, Depth uint}`（闭包表形态，主键约定见辅助）。
- AC-1..AC-12 各对应一个测试函数，命名描述行为（如 `TestInsertSelect_copies_ancestor_chain_with_bound_descendant`），AAA 结构，1:1 对应 AC。
- bug 类边界（AC-8/9/11）先写红测试再加守卫让其绿。
- 覆盖率门禁 ≥ 80%（项目当前 94.8%，不得回退）。

## 显式排除（Out of Scope）

- **scenario 2 自连接 `INSERT...SELECT...JOIN`**：推迟 Round 3（主表别名在 ToDB 物化路径丢失，需单独修）。
- A2/A3/A4/A5/B（已可用 v0.8.0 表达，见反馈文件 §D 实测）。
- 流式 InsertBuilder（YAGNI，仅 1 个 scenario 1 调用点）。
- 源 SELECT 自动注入 DataRule（★B 决策：明确不注入，结构性写入不应被隔离静默过滤——AC-7 守此）。
- MySQL/PG 真机方言验证（记录为已知风险；本轮以 SQLite 为准，与现有测试基线一致）。

## 已知风险

- **R1（跨方言 Exec）**：AC-5 的裸 `?` 内联 SQLite 已实测背书；MySQL/PG 真机不在本轮 CI，标注待真机验证。`getQuoteChar` 已覆盖 mysql/pg/sqlite/sqlserver/dm/oracle 转义字符，降低风险。
- **R2（投影列数语义）**：`SelectRaw("a, b")` 单串多列会被当 1 列，与目标 2 列触发 AC-2。属预期行为，靠文档提示规避。
- **R3（targetCols 原始字符串）**：仅 `validDataRuleColumn`（`^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)?$`）白名单 + `quoteColumn`，不支持表达式列名（与 DataRule.Column 同等约束）。字段指针路径不受影响。
