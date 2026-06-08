# DataRule.Table 字段（跨表数据权限）设计

- **日期**：2026-06-08
- **目标版本**：v0.10
- **状态**：设计已批准，待实现

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

## Acceptance Criteria

每条 AC 含具体输入值 + 具体可观测输出，1:1 对应一个测试函数。
**方言无关化要求**：SQL 结构断言用 `stripIdentQuotes` 去引号后比对（CHANGELOG:27 教训）；
错误断言用 `err != nil` + 含子串，不写死方言专属引号/错误文本。

- **AC-1（跨表正路，真实作用于别名）**：ctx 注入 `DataRule{Table:"ext", Column:"dept_id", Condition:"=", Value:"1"}`，
  在自连接（主别名 + ext 别名）查询下，生成的 SQL WHERE 去引号后含 `ext.dept_id`；
  自连接种子数据下结果只含 ext 表 dept_id=1 的行——**删掉 Table 前缀则结果改变**（证明非死代码）。
- **AC-2（向后兼容：旧 workaround 仍生效）**：`DataRule{Table:"", Column:"ext.dept_id", Condition:"=", Value:"1"}`
  生成的 WHERE 去引号后含 `ext.dept_id`，与 AC-1 等价 SQL。
- **AC-3（fail-fast：Table + Column 含点）**：`DataRule{Table:"ext", Column:"dept.id", Condition:"=", Value:"1"}`
  → `q.DataRuleBuilder().GetError()` 非 nil，错误信息含列名 `dept.id`。
- **AC-4（注入防护：Table 非法）**：`DataRule{Table:"ext\";DROP--", Column:"dept_id", Condition:"=", Value:"1"}`
  → `GetError()` 非 nil（validTableName 拒绝），不生成任何 SQL。
- **AC-5（Updater 侧对称）**：ctx 注入 `DataRule{Table:"ext", Column:"dept_id", Condition:"=", Value:"1"}`，
  经 `Updater.DataRuleBuilder()` 后生成的 UPDATE WHERE 去引号后含 `ext.dept_id`
  （防"SELECT 生效但 UPDATE 漏隔离"的写操作权限漏洞）。
- **AC-6（零回归：单表裸列）**：`DataRule{Table:"", Column:"dept_id", Condition:"=", Value:"1"}`
  生成 WHERE 去引号后含 `dept_id`（无表前缀），与现有行为逐字节一致。
- **AC-7（多表多 rule）**：ctx 注入两条 `DataRule`，Table 分别为 `"ext"`/`"sub"`、Column 分别为 `"a"`/`"b"`
  → 生成的 WHERE 同时含 `ext.a` 与 `sub.b`（去引号后）。

## Architecture

### 数据结构（builder.go）

```go
type DataRule struct {
    Table     string   // 新增：表名或 JOIN 别名前缀（如 "ext"）；空字符串表示作用于主表
    Column    string   // 规则字段（裸列名；Table 为空时兼容 "table.col" 点前缀）
    Condition string
    Value     string
    Values    []string
}
```

### 单一真相源：resolveDataRuleColumn（关键）

`applyDataRule` 在 query.go:1005 与 update.go:659 有两份镜像实现。新增 Table 逻辑必须只有一处，
否则会重蹈 CHANGELOG:382「by-ID 路径系统性漏 DataRule」的双侧漂移覆辙。提取包级 helper：

```go
// resolveDataRuleColumn 解析 DataRule 的最终列名。
// Table 非空时：校验 validTableName(Table)，且 Column 必须是裸列名（不含点），
// 拼成 "Table.Column"；Table 为空时：原样返回 Column（兼容旧的点前缀写法）。
func resolveDataRuleColumn(rule DataRule) (column string, err error)
```

两处 `applyDataRule` 在现有 `validDataRuleColumn.MatchString(column)` 校验**之前**先调用本 helper，
得到最终 column 后再走原有 switch 操作符映射逻辑，其余代码不变。

### 共存语义（fail-fast）

| `Table` | `Column` | 行为 |
|---|---|---|
| 空 | `dept_id` / `ext.dept_id` | 走现有路径（Column 可含点前缀）→ 向后 100% 兼容 |
| `"ext"` | `dept_id`（裸列） | 拼 `ext.dept_id`，最终生成 `` `ext`.`dept_id` `` |
| `"ext"` | `dept.id`（含点） | fail-fast，返回 error（语义唯一，禁止两套等价写法并存） |
| 非法（含引号/运算符） | 任意 | `validTableName` 拒绝，返回 error |

`Table` 校验复用现成的 `validTableName`（builder.go:80，与 validDataRuleColumn 同源正则，已防注入）。

### 非目标（YAGNI）

- **不主动校验列名歧义**：gplus 只负责正确拼前缀；裸列在多表冲突时由数据库执行期报 ambiguous。
  主动检测需解析所有 JOIN 表 schema（别名表元数据未必可得），过重且可能误报，不做。
- 不改 InsertSelect 的"不应用 DataRule"语义（结构性写入，CHANGELOG:19）。
- 多段 `a.b.c` 由 quoteColumn 逐段转义天然支持，但不作为本次显式测试目标。

## 实现位置与工作量

| 改动点 | 文件 | 估算 |
|---|---|---|
| `DataRule` 加 `Table` 字段 + godoc | builder.go | ~3 行 |
| `resolveDataRuleColumn` helper | builder.go | ~12 行 |
| `applyDataRule` 接入 helper（Query 侧） | query.go | ~3 行 |
| `applyDataRule` 接入 helper（Updater 侧） | update.go | ~3 行 |
| 测试（7 AC） | 新增 `datarule_table_test.go` | ~180 行 |

核心实现约 20-25 行。

## 文档修订（CHANGELOG，还反向兼容债）

- 新增 v0.10 段：兑现 v0.8.0:275 的 `DataRule.Table` 承诺。
- 修正 v0.8.0:260 过时契约："DataRule.Column 不应写 alias 前缀"→ 跨表权限现由 `Table` 字段正式提供。
- 标注 v0.9.0:34 的 Column 点前缀 workaround 被 `Table` 字段取代（仍向后兼容，新代码引导用 `Table`）。

## 兼容性

`Table` 是纯增量字段。旧 `DataRule`（无 Table）零影响；line 34 的存量 `Column:"ext.dept_id"`
workaround 代码在 Table 上线后仍可用（Table 空走旧路径）。通过"Table 非空时 Column 禁含点"的
fail-fast 规则，根除 v0.8.0 预言的歧义陷阱。
