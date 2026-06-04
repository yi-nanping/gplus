# SelectRaw 参数绑定 — `SelectRaw(expr string, args ...any)` 设计

> **日期**：2026-06-04（rev.2，已并入 5 视角审计 task `wa6kkk3k3` 结论）
> **范围**：让 `SelectRaw` 支持参数绑定，使投影表达式中的 `?` 走参数化绑定（进 `Statement.Vars`），跨方言安全。**所有不带 args 的现有查询行为字节级不变。**
> **来源**：InsertSelect（Round 2）多专家审计副产物——5/5 专家一致认定「给 SelectRaw 加绑定参数」与现有 `selects []string` 单次物化模型结构性不兼容，是独立于 InsertSelect 的核心 builder 改动，故拆为先行 Round 1。
> **依赖关系**：本特性是 `2026-06-04-insert-select-design.md`（Round 2）的前置依赖。
> **审计状态**：架构经 5 视角 + SQLite 探针实测背书（双路径零回归成立、绑定路径成立、迁移触点确为 6 处、注入面缩小）。rev.2 修复 2 个 must-fix（DISTINCT 丢失盲区、测试编译破坏漏列）+ 4 个 AC 口径项。

---

## Goal

`q.SelectRaw("age + ?", 1)` 中的 `1` 必须作为**绑定参数**进入 `Statement.Vars`，而非被拼成 SQL 字面量、也不被 `quoteColumn` 误当列名加引号。多个带参 `SelectRaw` 与 `Select` 字段指针**混用时投影列顺序与 vars 顺序严格保留**。所有不带 args 的现有用法（`Select`、`SelectRaw("AVG(age)")`、`Distinct`）生成 SQL 字节级不变。

### 为什么现状做不到（审计实测）

- `ScopeBuilder.selects` 是 `[]string`（builder.go:77），`applySelects` 经一次 `db.Select(quoteColumns(b.selects, qL, qR))`（builder.go:276-277）提交一个 `[]string`。
- GORM v1.31.1 的 `Select` 在 `[]string` 重载下**拒绝非 string 类型 args**（传 int 直接 `AddError("unsupported select args")`）；只有 `Select(单个 string, args...)` 且 `?数 >= len(args)` 才走 `clause.Expr{SQL,Vars}` 绑定路径（实测背书）。
- 裸 `?` 是单 token、无运算符/括号 → `quoteColumn`（builder.go:559+）会把它当列名加引号成 `"?"`，破坏占位符（实测：`quoteColumn("?", '"','"') => "?"` 被破坏）。

## Acceptance Criteria

> 测试用 SQLite in-memory（与现有测试一致）。「生成 SQL / Vars」通过 `db.Session(&gorm.Session{DryRun:true})` 物化后读 `Statement.SQL.String()` / `Statement.Vars` 断言。回归类 AC（AC-6/7/10）用**写死 SQL 字面量 `==` 全等**（非 `Contains`），版本敏感是刻意的回归门禁。每条 AC 1:1 对应一个测试函数。

**绑定值正确性**
- **AC-1（单参绑定，值正确）**：表有 3 行 `age = 18,19,20`。`q.SelectRaw("age + ?", 1)` 经 List/Scan 取该列 → 得 `[19,20,21]`。
- **AC-2（单参走 Vars 而非字面量）**：同 `q.SelectRaw("age + ?", 1)`，DryRun 后 `Statement.Vars` 含 `1`，且 `Statement.SQL` 含裸 `?`（不含字面量 `+ 1` 拼接、不含 `"?"`）。
- **AC-3（多 SelectRaw args 顺序）**：`q.SelectRaw("?", 7).SelectRaw("age + ?", 100)` → 投影第 1 列值=`7`、第 2 列值=各行 `age+100`；`Statement.Vars` 顺序严格 = `[7, 100]`。
- **AC-4（混用 Select 指针 + SelectRaw 带参，交错顺序保留 + SELECT 子句字面断言）**：`q.Select(&m.ID).SelectRaw("?", 9).Select(&m.Age)` → 投影列顺序为 `id, 9, age`（第 2 列值=9）；`Statement.Vars = [9]`；且 DryRun SQL 的 SELECT 子句**确切**为单串路径形态 `SELECT "id", ?, "age" FROM ...`（注意逗号后**带空格**，见 Architecture 路径差异说明）。
- **AC-13（单 expr 多占位符展开顺序）**：`q.SelectRaw("age + ? - ?", 5, 2)` → 该列值 = 各行 `age+3`；`Statement.Vars = [5, 2]`（验证单个 selectItem 的多 args 顺序展开）。

**DISTINCT 交互（rev.2 新增，修复实测盲区）**
- **AC-12（Distinct + SelectRaw(args) 保留 DISTINCT）**：`q.Distinct(&m.Age).SelectRaw("age + ?", 1)` 生成 SQL **含 `DISTINCT`** 且 `Statement.Vars = [1]`。
  > 背景：args 路径走 `db.Select(string,args)` 会在 `Statement.Distinct` 仍为 false 时固化 SELECT 子句，导致 DISTINCT 丢失（实测：无修复时生成 `SELECT age + ? FROM ...` 无 DISTINCT）。实现层修复见 Architecture。

**向后兼容（零回归，写死 SQL 全等）**
- **AC-6（无参 SelectRaw 字节级不变）**：`q.SelectRaw("COUNT(*) AS cnt")` DryRun 生成的 SQL `==` 写死基线串（变更前后逐字节相等）。
- **AC-7（Select 字段指针字节级不变）**：`q.Select(&m.Name, &m.Age)` DryRun 生成 SQL `== 'SELECT "name","age" FROM ...'`（逗号**无空格**，slice 路径形态，实测基线稳定）。
- **AC-10（Distinct 无 args 字节级不变）**：`q.Distinct(&m.Age)` 生成 SQL `== 'SELECT DISTINCT "age" FROM ...'`（验证 `selectItem` 迁移未破坏 builder.go:127 BuildCount distinct 分支与无 args 路径）。

**错误与生命周期**
- **AC-8（空 expr 仍报错）**：`q.SelectRaw("", 1)` 累积错误 `"gplus: SelectRaw expr cannot be empty"`，`GetError()` 非 nil，且不污染 selects（后续 BuildQuery 无该项、Vars 不含 1）。
- **AC-9（Clear 重置 args）**：`q.SelectRaw("?", 1)` 后调用 `Clear()`，再 `BuildQuery` 生成 SQL 无该投影列、`Statement.Vars` 不含 `1`。
- **AC-11（Updater 共享路径不破坏）**：`Updater[T]` 仅有 `Select`（update.go:337，无 SelectRaw/Distinct）。验证 `u.Select(&m.Name)` 经 `selectItem` 迁移后行为不变，且现有 `updater_subquery_test` / `update` 相关测试全绿。
- **AC-14（godoc args 绑定语义）**：`SelectRaw` godoc 明确区分 `expr`=原生片段（禁拼用户输入）/ `args`=绑定值（安全通道），措辞对齐 `WhereRaw` godoc（query.go:258-261）范式。

## Architecture

### 数据结构迁移（核心）

`ScopeBuilder.selects` 由 `[]string` 升级为有序结构，保留调用顺序：

```go
type selectItem struct {
    expr  string  // 列名（isRaw=false，已解析）或原生表达式（isRaw=true）
    args  []any   // 仅 isRaw 且带绑定参数时非空
    isRaw bool    // true=SelectRaw 原生表达式，不经 quoteColumn；false=Select/Distinct 列名，需转义
}

// ScopeBuilder
selects []selectItem   // 原 []string
```

> 字段名 `isRaw` 对齐 gplus 既有约定（`condition.isRaw` builder.go:19、`orderItem.isRaw` builder.go:30）。

### 各写入点改动

| 位置 | 现状 | 改动 |
|---|---|---|
| `query.go` `Select(cols...)`（:240） | append 列名 string | append `selectItem{expr:name, isRaw:false}` |
| `query.go` `SelectRaw(expr)`（:249-256） | append expr string | 签名加 `args ...any`；append `selectItem{expr, args, isRaw:true}` |
| `query.go` `Distinct(cols...)`（:725） | append 列名 string | append `selectItem{expr:name, isRaw:false}` |
| `update.go` `Select(cols...)`（:337，共享 ScopeBuilder.selects） | append 列名 string | append `selectItem{expr:name, isRaw:false}`。**Updater 无 SelectRaw/Distinct**，仅此一处 |
| **`missing_coverage_test.go:220-221`** | `u.selects[0]`（按 string 用：`strings.Contains` + `%q`） | **迁移后编译失败**，改为 `u.selects[0].expr`。（全仓唯一按 string 索引取 selects 元素的点；生产代码全部经 append/len/复位，安全） |

> 生产读取点经 grep 全仓核实仅 3 处：`builder.go:127`（BuildCount distinct）、`builder.go:214`（Clear）、`builder.go:276-277`（applySelects）。无隐藏读者（IsEmpty 只读 conditions、Pluck 不碰 selects）。

### `applySelects` 改动（builder.go:272-283）

```
若 len(selects)==0 → 不变（omits 段同样保持不变：db.Omit(quoteColumns(b.omits,...)...)）。
否则按调用顺序遍历 selects 构造 parts + flatArgs：
  - isRaw==false：part = quoteColumn(expr, qL, qR)
  - isRaw==true ：part = expr（原样，不转义）；flatArgs = append(flatArgs, item.args...)（顺序）
分支：
  - len(flatArgs)==0：维持现有形态 db.Select(quoteColumns 等价的 []string)        ← 零回归路径（slice，逗号无空格）
  - len(flatArgs)>0 ：
       prefix := ""; if b.distinct { prefix = "DISTINCT "; 标记 distinct 已内联 }
       db = db.Select(prefix + strings.Join(parts, ", "), flatArgs...)             ← 绑定路径（单串，逗号+空格）
```

**两条路径的逗号差异（rev.2 显式记录）**：slice 路径 `db.Select([]string{...})` 渲染为逗号**无空格**（`SELECT "name","age"`）；args 单串路径 `strings.Join(parts, ", ")` 为逗号**加空格**（`SELECT "name", "age"`）。此差异**仅出现在「混用 Select 指针 + SelectRaw(args)」的新增能力场景**——该场景无历史基线，故非回归。AC-4 须以单串路径实际形态断言，**不可**复用 AC-7 的 slice 路径基线。

**DISTINCT 修复（rev.2，AC-12）**：args 路径下若 `b.distinct`，把 `DISTINCT ` 前缀并入 combined string，并令 `applyDistinct` **不重复调用** `db.Distinct()`（避免双重 DISTINCT）。根因：`db.Select(string,args)` 经 `clause.Expr` 当场物化 SELECT 子句，捕获彼时 `Statement.Distinct=false`；而 `applyDistinct`（BuildQuery:146 / BuildCount:130）在其后才设 true，对已物化的 SELECT 子句是 no-op。**BuildCount distinct 分支（builder.go:127-130）同序，须同步应用此修复**，确保 COUNT 子查询保留 DISTINCT 且 Vars 不重复。

### 其他触点

| 位置 | 改动 |
|---|---|
| `builder.go:127` BuildCount `b.distinct && len(b.selects)>0` | `len` 语义不变（项数）；distinct+args 修复须覆盖此分支 |
| `builder.go:214` Clear `b.selects = b.selects[:0]` | **改为 `b.selects = nil`**，移入 builder.go:209-212 的「含嵌套引用切片置 nil」区块（`selectItem.args []any` 含嵌套引用，与 conditions/joins/preloads 同类，避免 backing array 持续持有已 Clear 的 args） |
| `builder.go:547` `quoteColumns([]string)` | 保留；无 args 路径用它处理 isRaw==false 项的等价输出 |

## Error Handling

- `SelectRaw("", args...)` → 复用现有错误 `"gplus: SelectRaw expr cannot be empty"`（query.go:251），args 一并丢弃，不写入 selects（AC-8）。
- 不新增 sentinel。`Select`/`Distinct` 非法字段指针错误路径不变。

## Testing

- 新增 `select_raw_args_test.go`（package gplus）。
- 复用 `TestUser`（含 `Age` 字段）做投影值断言；DryRun 断言用现有测试里的 `db.Session(DryRun)` 模式。
- AC-1..AC-14 各一测试函数，命名描述行为（如 `TestSelectRaw_binds_single_arg_into_vars`、`TestSelectRaw_with_distinct_keeps_distinct`）。
- **回归基线写死**：AC-6/AC-7/AC-10 必须把期望 SQL 串作为字面量常量写进测试做 `==` 全等断言（实测基线稳定：`SELECT "name","age" FROM \`test_users\``、`SELECT DISTINCT "age" FROM \`test_users\``）。**不可复用** `assertSQL`（query_sql_test.go:36 是 `Contains` 语义，守不住字节级相等）。
- 同步修改 `missing_coverage_test.go:220-221`（`u.selects[0]` → `u.selects[0].expr`），否则测试包 build fail。
- 覆盖率门禁 ≥ 80%（项目当前 94.0%，不得回退）。

## Out of Scope

- InsertSelect 本体（Round 2）。
- `Select` 字段指针带 args（无意义，字段指针即列名）。
- PG/MySQL 真机方言验证（绑定路径方言无关；按现有 SQLite 基线）。
- 按逗号 split `SelectRaw("a, b")` 单串多列（保持单串=1 项语义，InsertSelect Round 2 再约束）。
- **占位符数 < args 的调用方错误**（如 `SelectRaw("?", 1, 2)`）：属调用方误用，GORM 在**执行期**报 `unsupported select args`（DryRun 不报，var 悬挂），不为此写 AC、不额外兜底。
- `expr` 内字面量 `?` 或 GORM 命名参数 `@`：属 GORM raw 固有 caveat（字面量 `?` 会被计入绑定），godoc 提示「`?` 仅作绑定占位符」即可，非本特性缺陷。

## 已知风险

- **R1（零回归是硬约束）**：`[]string → []selectItem` 迁移贯穿 6 个触点。任何无 args 查询 SQL 若与变更前不一致即为回归。AC-6/AC-7/AC-10 写死全等 + 现有全量测试（94%）+ `missing_coverage_test.go` 修正共同把守；实现期须先跑 `go test ./...` 取绿基线再改。
- **R2（GORM 绑定路径前提，已实测背书）**：combined string 的 `?` 总数 = 各 isRaw 项 `?` 之和 = `len(flatArgs)`，`?数 >= len(args)` 条件天然满足，走 `clause.Expr{Vars}`。
- **R3（DISTINCT 修复的双路径耦合）**：DISTINCT 内联修复同时改 applySelects 与 applyDistinct/BuildCount 的协作，须确保 (a) 无 args 路径 DISTINCT 行为不变（AC-10）、(b) args 路径 DISTINCT 保留且不重复（AC-12）、(c) BuildCount 子查询正确。
