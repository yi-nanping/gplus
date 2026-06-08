# DataRule.Table 字段（跨表数据权限）设计

- **日期**：2026-06-08
- **目标版本**：v0.10
- **状态**：设计已批准（含多专家审计修订 r2），待实现

## Goal

为 `DataRule` 新增 `Table string` 字段，把"数据权限规则作用于哪张表/JOIN 别名"提升为一等 API 字段，
兑现 v0.8.0 路线图（CHANGELOG:275）承诺，并消除 v0.9.0 引入的反向兼容性债。

## Background（为何做、做什么）

调研结论（已核实，见 builder.go / query.go / schema.go）：

1. **跨表能力 v0.9.0 已可用**：`validDataRuleColumn`（builder.go:76）允许 `table.col` 点前缀；
   `applyDataRule` 把 column 当字符串传给 `q.Eq(column, val)` → `quoteColumn`（builder.go:665-671）
   按点分割逐段转义为 `` `ext`.`dept_id` ``。即用户写 `Column:"ext.dept_id"` 已能作用于别名表。
   这正是 CHANGELOG:34 那个 workaround。
2. **GORM 不会与之冲突**：gplus 的 WHERE 走 `db.Where("字符串片段", args...)` 路径
   （buildLeafSQL:379/390 → applyWhere:476），GORM 把字符串当 raw `clause.Expr`，
   **不解析列名、不自动加表名前缀**。Table 拼出的前缀是 SQL 里唯一的表限定符，无双重前缀冲突。
3. 因此 **`Table` 字段不是新能力，是 API 规范化**：把点前缀从 Column 字符串里拆成独立字段，
   语义显式、防手拼出错，并正式取代 v0.8.0:260 警告过的"在 Column 写前缀"陷阱写法。

## 安全不变量（实现期不可违反）

> 列名侧的全部注入防护 100% 压在 `validDataRuleColumn.MatchString` 这一道正则上
> （下游 `resolveColumnName` 对 string 零校验、`quoteColumn` 对复杂表达式直接放行）。
> 因此本设计的最高优先级不变量：

- **INV-1（最后防线不可移除）**：`resolveDataRuleColumn` 返回的**最终 column 字符串**，
  无论走新路径（拼接）还是旧路径（Table 空），都**必须经过 `validDataRuleColumn.MatchString`**。
  这道校验内聚在 helper 内部，是不可绕过的最后防线。
- **INV-2（调用时机）**：helper 必须在 `applyDataRule` 的 `value == ""` early-return **之前**调用，
  否则 `IS NULL` / `BETWEEN` 等分支拿到的仍是裸 `rule.Column`，Table 前缀被静默丢弃 → 数据权限漏洞。
- **INV-3（双侧塌缩）**：Query 侧（query.go）与 Updater 侧（update.go）的接入必须塌缩为同一行
  `col, err := resolveDataRuleColumn(rule)`，错误注入与 return 控制流相同，杜绝双侧漂移
  （CHANGELOG:382 by-ID 漏 DataRule 的历史教训）。

## Acceptance Criteria

每条 AC 含具体输入值 + 具体可观测输出，1:1 对应一个测试函数。
**方言无关化要求**：所有 SQL 结构断言一律 `stripIdentQuotes(sql)` 去引号后比对预期片段
（如 `ext.dept_id`，而非 `"ext"."dept_id"` 或反引号形式），错误断言用 `err != nil` + 含子串，
不写死方言专属引号/错误文本（CHANGELOG:27 PG CI 教训）。

### 正路与跨表（真实执行）

- **AC-1（跨表正路，裸列 + 死代码验证）**：ctx 注入 `DataRule{Table:"ext", Column:"dept_id", Condition:"=", Value:"1"}`
  （**必须用裸列 `dept_id`，不得用 `ext.dept_id` 点前缀**，否则无法区分新路径与旧 workaround）。
  在自连接（主别名 + ext 别名，两表均有 dept_id）场景真实执行：
  - 生成 SQL WHERE 去引号后含 `ext.dept_id`；
  - 种子数据须含 dept_id=1 与 dept_id=2 的行、且主表与 ext 表 dept_id 交叉（参照 insert_select_join_test.go AC-3 范式）；
  - 断言结果集只含 ext 表 dept_id=1 的行；
  - **强制对照子测试（防假绿，不可省）**：同一 DB、同种子上改用 `DataRule{Table:"", Column:"dept_id"}` 再执行一次，
    断言其结果与上面 Table:"ext" 版本**不同**（行数不同 / 行内容不同 / 或返回 ambiguous error）——
    必须是测试内的硬断言，证明 Table 前缀真实改变了查询行为、非死代码。
- **AC-2（向后兼容：旧 workaround 真实执行等价）**：`DataRule{Table:"", Column:"ext.dept_id", Condition:"=", Value:"1"}`
  在 AC-1 同款种子下真实执行，结果集与 AC-1 逐行一致（证明旧点前缀路径零回归，不只比 SQL 字符串）。
- **AC-6（零回归：单表裸列真实执行）**：`DataRule{Table:"", Column:"dept_id", Condition:"=", Value:"1"}`
  单表查询真实执行，WHERE 去引号后含裸 `dept_id`（无前缀），结果与现有 DataRule 行为逐行一致。
- **AC-7（多 rule，新旧混用 + IN 路径 + AND 语义真实执行）**：ctx 注入两条 rule——
  一条 `{Table:"ext", Column:"a", Condition:"IN", Values:[]string{"1","2"}}`（新路径 + 顺带覆盖 IN+Table 多值穿透）、
  一条 `{Table:"", Column:"sub.b", Condition:"=", Value:"9"}`（旧点前缀路径），WHERE 去引号后同时含 `ext.a` 与 `sub.b`；
  两条 rule 的值不同且可被独立字段验证（防参数绑定错位假绿）；
  种子设计为"同时满足两条件的行恰好 1 条、各只满足一条的行各 1 条"，断言结果恰 1 条且字段值正确
  （验证两条 rule 是 AND 且参数绑定不错位，新旧路径同查询共存无干扰）。

### 操作符穿透（验证 INV-2）

- **AC-8（IS NULL + Table）**：`DataRule{Table:"ext", Column:"dept_id", Condition:"IS NULL"}`
  → WHERE 去引号后含 `ext.dept_id IS NULL`（证明空值 early-return 之前已解析 Table，前缀未被截断）。
- **AC-9（BETWEEN + Table + 多值）**：`DataRule{Table:"ext", Column:"age", Condition:"BETWEEN", Values:[]string{"10","30"}}`
  → WHERE 去引号后含 `ext.age BETWEEN`（证明 Table 穿透到非 `=` 的多值分支）。

### fail-fast 与注入防护

- **AC-3（fail-fast：Table 非空 + Column 含点）**：`DataRule{Table:"ext", Column:"dept.id", Condition:"=", Value:"1"}`
  → `q.DataRuleBuilder().GetError()` 非 nil 且错误信息含**原始** `dept.id`；DryRun SQL 的 WHERE **不含** `dept`
  （证明 fail-fast 后未继续生成任何条件）。
- **AC-4（注入防护：table-driven 多 payload）**：对下列每个 `Table` 值 → `GetError()` 非 nil
  **且 DryRun SQL 的 WHERE 不含该输入片段**（负例断言，证明非法输入未拼进 SQL）：
  `ext";DROP--`（引号+分号）、`` ext`alias ``（反引号）、`ext.` / `.ext`（首尾点）、
  `ext ` / `ext\t` / `ext\n`（空白与控制字符）、`еxt`（西里尔同形 Unicode）。多段 `public.users` 由 AC-10 专测。
- **AC-10（Table 单段约束）**：`DataRule{Table:"public.users", Column:"id", Condition:"=", Value:"1"}`
  → `GetError()` 非 nil（Table 含点违反单段约束；与 AC-4 多段项呼应，单列以强调决策）。
- **AC-11（Table 首尾空格 fail-fast）**：`DataRule{Table:"ext ", Column:"dept_id", Condition:"=", Value:"1"}`
  → `GetError()` 非 nil（不做 TrimSpace，validTableName 拒）。

### Updater 侧对称（防写操作越权，每条与 Query 侧对应）

- **AC-5a（Updater 跨表正路）**：ctx 注入 `DataRule{Table:"ext", Column:"dept_id", Condition:"=", Value:"1"}`，
  经 `Updater.DataRuleBuilder()` 生成的 UPDATE WHERE 去引号后含 `ext.dept_id`。
- **AC-5b（Updater 注入防护）**：Updater + `Table:"ext\";DROP--"` → `Updater.GetError()` 非 nil，不生成 SQL。
- **AC-5c（Updater fail-fast）**：Updater + `Table:"ext", Column:"dept.id"` → `GetError()` 非 nil，含原始 `dept.id`。
- **AC-5d（Updater 零回归）**：Updater + `Table:"", Column:"dept_id"` → UPDATE WHERE 去引号后含裸 `dept_id`（无前缀）。
- **AC-5e（Updater 操作符穿透）**：Updater + `DataRule{Table:"ext", Column:"dept_id", Condition:"IS NULL"}`
  → UPDATE WHERE 去引号后含 `ext.dept_id IS NULL`（对称 AC-8，验证 Updater 侧 helper 也在 early-return 之前调用 / INV-2）。

## Architecture

### 数据结构（builder.go）

```go
type DataRule struct {
    // Table 表名或 JOIN 别名前缀（如 "ext"）；空字符串表示作用于主表。
    // 仅允许单段标识符（不含点）；含点 / 含空白 / 非法字符将被拒绝。
    Table     string
    // Column 规则字段。Table 非空时必须是裸列名（不含点）；
    // Table 为空时兼容 "table.col" 点前缀写法（旧 workaround，向后兼容；新代码建议用 Table）。
    Column    string
    Condition string
    Value     string
    Values    []string
}
```

### 核心：resolveDataRuleColumn（单一真相源 + 全部列名侧校验内聚）

```go
// resolveDataRuleColumn 解析 DataRule 的最终列名并完成全部列名侧安全校验。
// 返回的 column 可直接使用；err 非 nil 时已含完整上下文（含原始输入），
// 调用方只需 append 到 errs 并 return，【不得】再做任何额外列名校验。
//
// 内部顺序（安全关键，不可调整）：
//   1. Table == ""（旧路径，向后兼容）：
//        validDataRuleColumn.MatchString(Column) 失败 → error；否则原样返回 Column
//   2. Table != ""（新路径）：
//        a. Column 含 "." → error（含原始 Column）              // fail-fast：禁两套等价写法（AC-3）
//        b. !validTableName.MatchString(Table) || strings.Contains(Table, ".") → error
//                                                              // 决策：单段、不 TrimSpace（AC-10/11）
//        c. final := Table + "." + Column
//        d. validDataRuleColumn.MatchString(final) 失败 → error // INV-1 最后防线（防御性冗余，见下注）
//        e. 返回 final
func resolveDataRuleColumn(rule DataRule) (column string, err error)
```

- `Table` 单段校验（step b）：`validTableName.MatchString(Table)` 为真**且** `!strings.Contains(Table, ".")`。
  validTableName 允许 `schema.table` 单点，故需额外禁点以落实"单段"决策（伪代码与本句为同一逻辑的两种等价写法，实现以本句为准）。
- **step d 是防御性冗余（future-proof，禁止删除）**：在 step a/b 通过后，`final` 恒为 `ident.ident` 形式、
  必然匹配 `validDataRuleColumn`，故 step d 当前"永远通过"。保留它是 INV-1 的最后防线——
  一旦未来有人放宽 step b 的 Table 校验，step d 仍兜底。AC-4/10/11 的拒绝由 step b 完成，step d 不单独可测。
- **正则共用约束**：`validDataRuleColumn` 的单点允许（`ident.ident`）由旧路径（Column 点前缀）与新路径
  （step d 校验拼接结果）**共用**。不得为区分两路径而单独收紧旧路径正则，否则 step d 兜底失效。
- 错误用 `%q` 转义原始输入，沿用 query.go:1012 既有格式风格。

### 接入点（query.go:1005 / update.go:653 两侧，塌缩为同一形态）

```go
// applyDataRule 开头：现有首行 `column := rule.Column` 【整体替换】为下面两行，
// 且必须在 value=="" early-return 之前（INV-2）：
column, err := resolveDataRuleColumn(rule)   // 替换原 `column := rule.Column`
if err != nil {
    q.errs = append(q.errs, err)   // Updater 侧：u.errs
    return
}
// 删除原有的独立 validDataRuleColumn.MatchString(column) 校验块——已内聚进 helper（INV-1/INV-3）。
// 其后 value 空值检查、SQL/USE_SQL_RULES 拒绝、switch 操作符映射【保持不变】，各侧自留；
// 注意 switch 内所有 q.Eq(column,...) 等调用此时的 column 即 helper 返回值（含 Table 前缀），
// 不再是 rule.Column——这是 Table 前缀穿透到各操作符分支的关键（INV-2）。
```

> 注意：`SQL`/`USE_SQL_RULES` 拒绝的错误文案两侧不同（query 提示 RawQuery、update 提示 RawExec），
> 这部分**不进 helper**，各自保留；helper 只负责列名解析 + 列名侧校验。

### 共存语义（fail-fast）

| `Table` | `Column` | 行为 |
|---|---|---|
| 空 | `dept_id` / `ext.dept_id` | 旧路径：Column 过 validDataRuleColumn 后原样用（兼容点前缀）→ 向后 100% 兼容 |
| `"ext"` | `dept_id`（裸列） | 拼 `ext.dept_id`，最终过 validDataRuleColumn，生成 `` `ext`.`dept_id` `` |
| `"ext"` | `dept.id`（含点） | fail-fast error（语义唯一，禁两套等价写法）（AC-3） |
| `"public.users"`（含点） | 任意 | fail-fast error（Table 单段约束）（AC-10） |
| `"ext "` / 含空白 / 非法字符 | 任意 | validTableName 拒，error（不 TrimSpace）（AC-4/AC-11） |
| 任意 | `""`（空列） | validDataRuleColumn 拒（空串及拼出的 `ext.` 均不匹配正则）→ error（既有行为，helper 兜底，无新增逻辑） |

### 非目标（YAGNI）

- **不主动校验列名歧义**：gplus 只负责正确拼前缀；裸列在多表冲突时由数据库执行期报 ambiguous。
  主动检测需解析所有 JOIN 表 schema（别名表元数据未必可得），过重且可能误报，不做。
- **不支持多段 Table**（schema.table 跨 schema 权限）：Table 单段，拼接后恒 ≤2 段；有需求再立项。
- 不改 InsertSelect 的"不应用 DataRule"语义（结构性写入，CHANGELOG:19）。

## 实现位置与工作量

| 改动点 | 文件 | 估算 |
|---|---|---|
| `DataRule` 加 `Table` 字段 + godoc（含 Column 既有 godoc 同步） | builder.go | ~6 行 |
| `resolveDataRuleColumn` helper（含双校验 + 格式化 error） | builder.go | ~18 行 |
| `applyDataRule` 接入 helper + 删旧校验块（Query 侧） | query.go | ~5 行 |
| `applyDataRule` 接入 helper + 删旧校验块（Updater 侧） | update.go | ~5 行 |
| 测试（14 AC：AC-1~11 + AC-5a~5e） | 新增 `datarule_table_test.go` | ~320 行 |

核心实现约 30 行。

## 文档修订（CHANGELOG，还反向兼容债）

- 新增 v0.10 段：兑现 v0.8.0:275 的 `DataRule.Table` 承诺。
- 修正 v0.8.0:260 过时契约："DataRule.Column 不应写 alias 前缀"→ 跨表权限现由 `Table` 字段正式提供。
- 标注 v0.9.0:34 的 Column 点前缀 workaround 被 `Table` 字段取代（仍向后兼容，新代码引导用 `Table`）。
- README：补 `DataRule.Table` 的值必须与查询中 JOIN 别名（`As[X]` 注册名 / `NewQueryAs` 主别名）
  字符串一致，gplus 不校验别名存在性，拼错由数据库执行期报错（降低认知负担）。

## 兼容性

`Table` 是纯增量字段。旧 `DataRule`（无 Table）零影响；line 34 的存量 `Column:"ext.dept_id"`
workaround 代码在 Table 上线后仍可用（Table 空走旧路径）。通过"Table 非空时 Column 禁含点 + Table 单段"
的 fail-fast 规则，根除 v0.8.0 预言的歧义陷阱。
