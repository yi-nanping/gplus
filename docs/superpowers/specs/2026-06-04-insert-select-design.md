# InsertSelect — 跨表写时多表（`INSERT ... SELECT [... JOIN]`）设计

> **日期**：2026-06-04
> **范围**：给 gplus 补 `INSERT ... SELECT` 写时多表能力 + 修复 `SelectRaw` 不带绑定参数的子缺口。
> **触发来源**：下游 `gvs-server` 的 `docs/dev/gplus-raw-sql-feedback.md` 实测结论——穷举 21 处手写 SQL 后，gplus 唯一真实能力缺口是 A1（跨表 `INSERT ... SELECT`，2 个调用点，闭包表维护）。

---

## Goal

让使用方能用 query builder 表达「以一个 SELECT 查询（可含 JOIN / 自连接）作为 INSERT 数据源」的写操作，消除闭包表维护类场景对 `RawExec` + 手写 SQL 的依赖。**不扩大范围**：反馈文件已实测确认 A2/A3/A4/A5/B 在 v0.8.0 均可表达，本设计只做 A1。

## Acceptance Criteria

> 每条 AC 含具体输入值 + 具体可观测输出，1:1 对应测试函数。方言验证用 SQLite in-memory（与现有测试一致），跨方言风险见 AC-9。

- **AC-1**（SelectRaw 带参数绑定）：`q.SelectRaw("?", 42)` 后 `BuildQuery` 生成的 SELECT 片段为 `42`（作为绑定参数 `?`，vars 含 `42`），而非字面量拼接。验证：对一张含 `age` 列的表执行 `q.SelectRaw("age + ?", 1)`，查回值 = 各行 `age+1`。
- **AC-2**（SelectRaw 向后兼容）：`q.SelectRaw("COUNT(*) AS cnt")`（无 args）行为与变更前完全一致，现有调用点编译通过、结果不变。
- **AC-3**（SelectRaw 空 expr）：`q.SelectRaw("", 1)` 累积错误 `"gplus: SelectRaw expr cannot be empty"`，`GetError()` 非 nil（与现状一致）。
- **AC-4**（基础 INSERT...SELECT，对应场景 1）：源表 `closure` 含行 `{ancestor_id:1, descendant_id:5, depth:0}`，执行
  `InsertSelect[uint,Closure,Closure](r, ctx, []any{&m.AncestorID,&m.DescendantID,&m.Depth}, src)`，
  其中 `src` = `SelectRaw("ancestor_id").SelectRaw("?", 9).SelectRaw("depth + 1").Eq(&m.DescendantID, 5)`。
  结果：返回 `(affected=1, nil)`；`closure` 表新增 1 行 `{ancestor_id:1, descendant_id:9, depth:1}`。
- **AC-5**（INSERT...SELECT...JOIN 自连接，对应场景 2）：`closure` 含 `{1,1,0},{1,2,1},{2,2,0}`，
  `src` 用 `NewQueryAs(ctx,"ext")` + `As[Closure](src,"sub")` 表达
  `SELECT ext.ancestor_id, sub.descendant_id, ext.depth+sub.depth+1 FROM closure ext JOIN closure sub ON sub.ancestor_id=2 WHERE ext.descendant_id=2`。
  结果：返回 `(affected≥1, nil)`，新增行的 `(ancestor_id,descendant_id,depth)` 与等价手写 SQL 逐行一致。
- **AC-6**（列数不匹配）：`targetCols` 长度=3，`src` 仅 2 个投影列 → 返回 `(0, ErrInsertSelectColMismatch)`，不执行任何 SQL（表行数不变）。
- **AC-7**（无投影拒绝）：`src` 未调用任何 `Select`/`SelectRaw` → 返回 `(0, ErrInsertSelectNoProjection)`，拒绝退化成 `INSERT ... SELECT *`。
- **AC-8**（nil / builder 错误传播）：`src == nil` → 返回 `(0, ErrQueryNil)`；`src.GetError() != nil`（如非法字段指针）→ 该错误原样返回，不执行 SQL。
- **AC-9**（无多余括号，方言安全）：生成的 SQL 形如 `INSERT INTO <table> (col,col,col) SELECT ...`，SELECT 外**无包裹括号**（避免 MySQL/SQLite 对 `(SELECT...)` 的语法限制）。验证：对生成 SQL 串断言不以 `) (SELECT` 模式出现，且 SQLite 真实执行成功。
- **AC-10**（事务变体）：`InsertSelectTx(r, ctx, tx, cols, src)` 在传入 `tx` 上执行；事务回滚后目标表无新增行。
- **AC-11**（不注入 DataRule）：ctx 中存在会匹配源表的 `DataRule`（如 `WHERE org_code IN (...)`）时，`InsertSelect` 生成的 SELECT **不含**该隔离条件——SELECT 的 WHERE 仅为调用方在 `src` 上显式设置的条件。验证：对比 `List`（注入 DataRule，行数被过滤）与 `InsertSelect` 源 SELECT（不过滤），后者插入行数 = 无 DataRule 时的全量。

## Architecture

### 变更面（最小）

| 文件 | 变更 |
|---|---|
| `query.go` | `SelectRaw(expr string)` → `SelectRaw(expr string, args ...any)`；args 随 select 表达式注入 GORM `Select(expr, args...)` |
| `repository.go` | 新增包级泛型 `InsertSelect` / `InsertSelectTx`；新增 3 个 sentinel 错误 |
| `repository.go` | 复用 `dbResolver` / `getModelInstance[T]()` / `ToDB` 物化路径 |

### API 签名

```go
// D=主键类型, T=目标表模型, S=源 Query 模型（自插入时 S==T）
func InsertSelect[D, T, S any](
    r *Repository[D, T], ctx context.Context,
    targetCols []any, src *Query[S],
) (int64, error)

func InsertSelectTx[D, T, S any](
    r *Repository[D, T], ctx context.Context, tx *gorm.DB,
    targetCols []any, src *Query[S],
) (int64, error)

// SelectRaw 扩展（variadic，向后兼容）
func (q *Query[T]) SelectRaw(expr string, args ...any) *Query[T]
```

### 数据流

```
InsertSelect(r, ctx, targetCols, src)
  1. 守卫：src==nil → ErrQueryNil；src.GetError()!=nil → 原样返回
  2. 解析 targetCols（[]any，走 resolveColumnNameAny：字段指针 or 原始列名）→ []string
  3. 守卫：len(src 投影列)==0 → ErrInsertSelectNoProjection
          len(targetCols)!=len(src 投影列) → ErrInsertSelectColMismatch
  4. 物化 src → SELECT SQL + vars（沿用 ToDB 语义：BuildQuery，【不】走 DataRuleBuilder）
  5. 取目标表名：getModelInstance[T]() 经 GORM schema 解析
  6. 组装："INSERT INTO <table> (<cols>) " + <selectSQL>，合并 vars
  7. dbResolver(ctx, tx).Exec(sql, vars...) → 返回 (RowsAffected, Error)
```

### 投影列数的判定

`Query` 把 `Select`/`SelectRaw` 累积到 `q.selects []string`。投影列数 = `len(src.selects)`。AC-6/AC-7 据此校验。注意：`SelectRaw("a, b")` 这种单串多列写法会被算作 1 列——文档需提示「一个 SelectRaw 对应一个目标列」，多列请拆多次 `SelectRaw`（场景 1/2 均为逐列写法，不受影响）。

### SQL 物化与括号安全（AC-9 核心）

不使用 GORM 的 `Exec("... ?", subDB)` 子查询内联（它会给 SELECT 包裹括号 `(SELECT...)`，MySQL 8 / SQLite 对 `INSERT ... (SELECT...)` 有语法限制）。改为：
1. 用 `src` 物化出的 `*gorm.DB`，经 `Session{DryRun:true}` 取 `Statement.SQL.String()` 与 `Statement.Vars`，得到**不带外层括号**的裸 SELECT SQL + 顺序 vars。
2. 自行拼接 `INSERT INTO <table> (<cols>) ` 前缀 + 裸 SELECT，vars 原样透传给 `Exec`。

> 实现期必须验证 DryRun 取出的 SQL 占位符与 vars 顺序在目标方言下可直接 `Exec`（SQLite `?` 占位符路径已被现有测试覆盖；MySQL/PG 真机不在本轮 CI，风险记录于下）。

## Error Handling

新增 sentinel（`repository.go`，与现有 `ErrXxx` 风格一致）：

```go
var (
    ErrInsertSelectColMismatch  = errors.New("gplus: InsertSelect target column count does not match source projection count")
    ErrInsertSelectNoProjection = errors.New("gplus: InsertSelect source query has no Select/SelectRaw projection")
)
```
- `src==nil` 复用现有 `ErrQueryNil`。
- `src.GetError()` 非 nil（非法字段指针 / 别名错误等）→ 原样返回，不吞。
- 所有守卫在 `Exec` 之前完成，失败时保证目标表零副作用（AC-6/AC-7/AC-8）。

## Testing

- 测试文件：新增 `insert_select_test.go`（package gplus，可访问未导出符号）。
- 用 `setupTestDB[Closure]` 风格建内存 SQLite + 自动迁移一张含 `ancestor_id/descendant_id/depth` 的测试模型（闭包表形态）。
- SelectRaw args 的单元验证可复用现有 `TestUser` 模型。
- AC-1..AC-11 各对应一个测试函数，命名描述行为（如 `TestInsertSelect_copies_ancestor_chain_with_bound_literal`），AAA 结构。
- 覆盖率门禁 ≥ 80%（项目当前 94.0%，不得回退）。

## 显式排除（Out of Scope）

- A2/A3/A4/A5/B（已可用 v0.8.0 表达，见反馈文件 §D 实测）。
- 流式 InsertBuilder（YAGNI，仅 2 调用点）。
- 源 SELECT 自动注入 DataRule（★B 决策：明确不注入，结构性写入不应被隔离静默过滤）。
- MySQL/PG 真机方言验证（记录为已知风险；本轮以 SQLite 为准，与现有测试基线一致）。

## 已知风险

- **R1（方言括号）**：AC-9 的「裸 SELECT」拼接依赖 DryRun 取 SQL 的稳定性。若某方言下 DryRun SQL 不可直接 `Exec`，需在实现期改用方言感知拼接。SQLite 路径有现有测试背书；MySQL/PG 标注待真机验证。
- **R2（投影列数语义）**：`SelectRaw("a, b")` 单串多列会被当 1 列，与目标 2 列不匹配触发 AC-6。属预期行为，靠文档提示规避。
