# SelectRaw 参数绑定 — `SelectRaw(expr string, args ...any)` 设计

> **日期**：2026-06-04
> **范围**：让 `SelectRaw` 支持参数绑定，使投影表达式中的 `?` 走参数化绑定（进 `Statement.Vars`），跨方言安全。**保持所有现有查询行为字节级不变。**
> **来源**：InsertSelect（Round 2）多专家审计副产物——5/5 专家一致认定「给 SelectRaw 加绑定参数」与现有 `selects []string` 单次物化模型结构性不兼容，是独立于 InsertSelect 的核心 builder 改动，故拆为先行 Round 1。
> **依赖关系**：本特性是 `2026-06-04-insert-select-design.md`（Round 2）的前置依赖。

---

## Goal

`q.SelectRaw("age + ?", 1)` 中的 `1` 必须作为**绑定参数**进入 `Statement.Vars`，而非被拼成 SQL 字面量、也不被 `quoteColumn` 误当列名加引号。多个带参 `SelectRaw` 与 `Select` 字段指针**混用时投影列顺序与 vars 顺序严格保留**。所有不带 args 的现有用法（`Select`、`SelectRaw("AVG(age)")`、`Distinct`）生成 SQL 字节级不变。

### 为什么现状做不到（审计实测）

- `ScopeBuilder.selects` 是 `[]string`（builder.go:77），`applySelects` 经一次 `db.Select(quoteColumns(b.selects, qL, qR))`（builder.go:276-277）提交一个 `[]string`。
- GORM v1.31.1 的 `Select` 在 `[]string` 重载下**拒绝非 string 类型 args**（传 int 直接 `AddError("unsupported select args")`）；只有 `Select(单个 string, args...)` 且 `?数 >= len(args)` 才走 `clause.Expr{SQL,Vars}` 绑定路径。
- 裸 `?` 是单 token、无运算符/括号 → `quoteColumn`（builder.go:559+）会把它当列名加引号成 `"?"`，破坏占位符。

## Acceptance Criteria

> 测试用 SQLite in-memory（与现有测试一致）。「生成 SQL / Vars」通过 `db.Session(&gorm.Session{DryRun:true})` 物化后读 `Statement.SQL.String()` / `Statement.Vars` 断言。每条 AC 1:1 对应一个测试函数。

- **AC-1（单参绑定，值正确）**：表有 3 行 `age = 18,19,20`。`q.SelectRaw("age + ?", 1)` 经 List/Scan 取该列 → 得 `[19,20,21]`。
- **AC-2（单参走 Vars 而非字面量）**：同 `q.SelectRaw("age + ?", 1)`，DryRun 后 `Statement.Vars` 含 `1`，且 `Statement.SQL` 含裸 `?`（不含字面量 `+ 1` 拼接、不含 `"?"`）。
- **AC-3（多 SelectRaw args 顺序）**：`q.SelectRaw("?", 7).SelectRaw("age + ?", 100)` → 投影第 1 列值=`7`、第 2 列值=各行 `age+100`；`Statement.Vars` 顺序严格 = `[7, 100]`。
- **AC-4（混用 Select 指针 + SelectRaw 带参，交错顺序保留）**：`q.Select(&m.ID).SelectRaw("?", 9).Select(&m.Age)` → 投影列顺序为 `id, 9, age`（第 2 列值=9）；`Statement.Vars = [9]`。
- **AC-5（raw `?` 不被 quoteColumn 误转义）**：`q.SelectRaw("?", 5)` 生成 SQL 中占位符为裸 `?`，**非** `"?"`/`` `?` ``；执行后该列值=`5`。
- **AC-6（向后兼容：无参 SelectRaw 字节级不变）**：`q.SelectRaw("COUNT(*) AS cnt")`（无 args）DryRun 生成的 SELECT 子句字符串与本次变更前**完全相等**。
- **AC-7（向后兼容：Select 字段指针字节级不变）**：`q.Select(&m.Name, &m.Age)` DryRun 生成 SQL 与变更前**完全相等**（含方言转义引号）。
- **AC-8（空 expr 仍报错）**：`q.SelectRaw("", 1)` 累积错误 `"gplus: SelectRaw expr cannot be empty"`，`GetError()` 非 nil，且不污染 selects（后续 BuildQuery 无该项）。
- **AC-9（Clear 重置 args）**：`q.SelectRaw("?", 1)` 后调用 `Clear()`，再 `BuildQuery` 生成的 SQL 无该投影列、`Statement.Vars` 不含 `1`。
- **AC-10（Distinct 路径不被迁移破坏）**：`q.Distinct(&m.Age)` 生成 `SELECT DISTINCT ...` 与变更前一致（验证 `selectItem` 迁移未破坏 builder.go:127 的 `b.distinct && len(b.selects) > 0` 分支）。
- **AC-11（Updater 共享路径不破坏）**：`Updater[T]` 仅有 `Select`（update.go:337，无 SelectRaw/Distinct）。验证 `u.Select(&m.Name)` 经 `selectItem` 迁移后行为不变，且现有 `updater_subquery_test` / `update` 相关测试全绿（共享 ScopeBuilder 迁移零破坏）。

## Architecture

### 数据结构迁移（核心）

`ScopeBuilder.selects` 由 `[]string` 升级为有序结构，保留调用顺序：

```go
type selectItem struct {
    expr string   // 列名（raw=false，已解析）或原生表达式（raw=true）
    args []any    // 仅 raw 且带绑定参数时非空
    raw  bool      // true=SelectRaw 原生表达式，不经 quoteColumn；false=Select/Distinct 列名，需转义
}

// ScopeBuilder
selects []selectItem   // 原 []string
```

### 各写入点改动

| 位置 | 现状 | 改动 |
|---|---|---|
| `query.go` `Select(cols...)` | append 列名 string | append `selectItem{expr:name, raw:false}` |
| `query.go` `SelectRaw(expr)` | append expr string | 签名加 `args ...any`；append `selectItem{expr, args, raw:true}` |
| `query.go` `Distinct(cols...)` | append 列名 string | append `selectItem{expr:name, raw:false}` |
| `update.go` `Select(cols...)`（update.go:337，共享 ScopeBuilder.selects） | append 列名 string | append `selectItem{expr:name, raw:false}`。**Updater 无 SelectRaw/Distinct**，仅此一处 |

### `applySelects` 改动（builder.go:272-283）

```
若 len(selects)==0 → 不变。
否则按调用顺序遍历 selects：
  - raw==false：part = quoteColumn(expr, qL, qR)；无 args
  - raw==true ：part = expr（原样，不转义）；收集 args 到 flatArgs（顺序）
分支：
  - len(flatArgs)==0：维持现有形态 db.Select(quoteColumns 等价的 []string)  ← 零回归路径
  - len(flatArgs)>0 ：db.Select(strings.Join(parts, ", "), flatArgs...)        ← 绑定路径（单次调用，避免 GORM Select 覆盖语义）
```

> **零回归约束**：无 args 时生成的 SQL 必须与变更前**逐字节相同**（AC-6/AC-7 锁定）。实现上无 args 路径继续用 `quoteColumns([]selectItem→[]string)` 等价输出；仅当存在绑定参数时才切换到「单 combined string + args」形态。

### 其他触点

| 位置 | 改动 |
|---|---|
| `builder.go:127` BuildCount `b.distinct && len(b.selects)>0` | `len` 语义不变（项数），逻辑无需改，仅类型适配 |
| `builder.go:214` Clear `b.selects = b.selects[:0]` | 类型适配（清空 `[]selectItem`），args 随之清空 |
| `builder.go:547` `quoteColumns([]string)` | 保留；无 args 路径用它处理 raw==false 项的等价输出 |

## Error Handling

- `SelectRaw("", args...)` → 复用现有错误 `"gplus: SelectRaw expr cannot be empty"`（query.go:251），args 一并丢弃，不写入 selects（AC-8）。
- 不新增 sentinel。`Select`/`Distinct` 非法字段指针错误路径不变。

## Testing

- 新增 `select_raw_args_test.go`（package gplus）。
- 复用 `TestUser`（含 `Age` 字段）做投影值断言；DryRun 断言用现有测试里的 `db.Session(DryRun)` 模式。
- AC-1..AC-11 各一测试函数，命名描述行为（如 `TestSelectRaw_binds_single_arg_into_vars`）。
- **回归保护**：AC-6/AC-7 必须对比变更前后 DryRun SQL 字符串相等——实现期先记录基线 SQL 再改代码。
- 覆盖率门禁 ≥ 80%（项目当前 94.0%，不得回退）。

## Out of Scope

- InsertSelect 本体（Round 2）。
- `Select` 字段指针带 args（无意义，字段指针即列名）。
- PG/MySQL 真机方言验证（绑定路径方言无关；按现有 SQLite 基线）。
- 按逗号 split `SelectRaw("a, b")` 单串多列（保持单串=1 项语义，InsertSelect Round 2 再约束）。

## 已知风险

- **R1（零回归是硬约束）**：`[]string → []selectItem` 迁移面虽小但贯穿 Select/SelectRaw/Distinct/applySelects/Clear/BuildCount。任何无 args 查询 SQL 若与变更前不一致即为回归。AC-6/AC-7/AC-10 + 现有全量测试（94%）共同把守；实现期须先跑 `go test ./...` 取绿基线。
- **R2（GORM 绑定路径前提）**：绑定路径依赖 GORM `Select(string, args...)` 在 `?数 >= len(args)` 时走 `clause.Expr{Vars}`。combined string 的 `?` 总数 = 各 raw 项 `?` 之和 = `len(flatArgs)`（每个绑定值对应一个 `?`），条件天然满足。若用户 `SelectRaw("?", a, b)`（占位符少于 args）属调用方错误，GORM 行为即报错，不额外兜底。
