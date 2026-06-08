# DataRule.Table 跨表数据权限 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `DataRule` 新增 `Table string` 字段，把"规则作用于哪张表/JOIN 别名"提升为一等 API，兑现 v0.8.0:275 承诺并消除 v0.9.0 反向兼容债。

**Architecture:** 新增单一真相源 helper `resolveDataRuleColumn(rule) (column, err)`，把全部列名侧校验内聚其中（INV-1 最后防线）；Query/Updater 两侧 `applyDataRule` 把开头的 `column := rule.Column` 整体塌缩为 `column, err := resolveDataRuleColumn(rule)`，置于 `value==""` early-return 之前（INV-2），两侧形态完全一致（INV-3）。`Table` 单段、不 TrimSpace、Table 非空时 Column 禁含点（fail-fast）。

**Tech Stack:** Go 1.24 + GORM + SQLite（glebarez/sqlite 内存库）；标准库 `go test`，方言无关断言（`stripIdentQuotes` 去引号）。

**Spec:** `docs/superpowers/specs/2026-06-08-datarule-table-design.md`（commit 6ff15ce，已批准）

**Go 测试命令（本机）：** `D:/Environment/golang/go1.21.11/bin/go.exe test ./...`（裸 `go` 亦可，go.mod 触发 go1.24 toolchain）

---

## 安全不变量（实现期不可违反，来自 spec §安全不变量）

- **INV-1**：`resolveDataRuleColumn` 返回的最终 column 字符串，无论新旧路径都必须经过 `validDataRuleColumn.MatchString`，内聚在 helper 内部，不可绕过。
- **INV-2**：helper 必须在 `applyDataRule` 的 `value == ""` early-return **之前**调用，否则 `IS NULL`/`BETWEEN` 分支拿到裸 `rule.Column`，Table 前缀被静默丢弃 → 数据权限漏洞。
- **INV-3**：Query 侧与 Updater 侧接入塌缩为同一行 `column, err := resolveDataRuleColumn(rule)`，错误注入与 return 控制流相同，杜绝双侧漂移。

---

## File Structure

| 文件 | 职责 | 改动 |
|---|---|---|
| `builder.go` | `DataRule` struct + `resolveDataRuleColumn` helper | 加 `Table` 字段 + godoc（~6 行）；新增 helper（~18 行） |
| `query.go` | Query 侧 `applyDataRule`（:1005-1014） | 替换首行 + 删旧校验块（~5 行净变） |
| `update.go` | Updater 侧 `applyDataRule`（:653-662） | 替换首行 + 删旧校验块（~5 行净变） |
| `datarule_table_test.go` | 14 AC 测试（AC-1~11 + AC-5a~5e） | 新建（~340 行） |
| `CHANGELOG.md` | v0.10 段 + 修正 v0.8.0:260 / 标注 v0.9.0:34 | 文档 |
| `README.md` | `DataRule.Table` 用法 + 别名一致性指引 | 文档 |

**复用的现有测试基础设施（同包 `package gplus`，勿重复定义）：**
- `stripIdentQuotes(s string) string` — query_sql_test.go:37，去标识符引号
- `newDryRunDB(t) *gorm.DB` — query_sql_test.go:14，DryRun 库
- `setupTestDB[T](t) (*Repository[int64,T], *gorm.DB)` — repo_test.go:12，真实执行库
- `As[T](q, alias)` / `repo.NewQueryAs(ctx, alias)` / `q.CrossJoinAs(...)` — 自连接范式（insert_select_join_test.go）

---

## 测试模型与 helper（Task 3 首次引入，置于 datarule_table_test.go 顶部）

```go
package gplus

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// drUser DataRule.Table 测试专用模型（自引用，用于自连接验证）。
type drUser struct {
	ID     int64  `gorm:"column:id;primaryKey;autoIncrement"`
	DeptID uint   `gorm:"column:dept_id"`
	Age    uint   `gorm:"column:age"`
	Name   string `gorm:"column:name"`
}

func (drUser) TableName() string { return "dr_users" }

// drDataRuleSQL 注入 rules 后用 DryRun 生成 SQL（去引号）并返回 q（供取 GetError）。
// 单表 drUser，不执行；用于操作符穿透（AC-8/9）与负例断言（AC-3/4/10/11）。
func drDataRuleSQL(t *testing.T, rules []DataRule) (q *Query[drUser], sql string) {
	t.Helper()
	db := newDryRunDB(t)
	ctx := context.WithValue(context.Background(), DataRuleKey, rules)
	var qq *Query[drUser]
	qq, _ = NewQuery[drUser](ctx)
	stmt := db.Session(&gorm.Session{DryRun: true}).
		Model(&drUser{}).
		Scopes(qq.DataRuleBuilder().BuildQuery()).
		Find(&[]drUser{}).Statement
	return qq, stripIdentQuotes(stmt.SQL.String())
}
```

---

## Task 列表

1. **Task 1** — `DataRule.Table` 字段 + `resolveDataRuleColumn` helper + Query 侧接入 + AC-3（端到端 fail-fast，打通链路）
2. **Task 2** — Query 侧注入防护/单段/空格端到端（AC-4 / AC-10 / AC-11）
3. **Task 3** — Query 跨表正路 + 兼容 + 零回归 + 多 rule + 操作符穿透（AC-1 / AC-2 / AC-6 / AC-7 / AC-8 / AC-9）
4. **Task 4** — Updater 侧接入 + 对称 AC（AC-5a~5e）
5. **Task 5** — 文档修订（CHANGELOG / README / godoc）+ 全量回归 + 覆盖率

---

## Task 1: `DataRule.Table` 字段 + `resolveDataRuleColumn` helper + Query 侧接入

打通"helper → Query 接入 → 端到端 GetError"全链路，并用 AC-3（fail-fast）验证。本 Task 同时落地数据结构、helper、Query 接入，因为 spec 所有 AC 都是端到端形式，helper 必须接入后才可端到端测。

**Files:**
- Modify: `builder.go:54-59`（`DataRule` struct 加 `Table` 字段）
- Modify: `builder.go`（`validTableName` 定义后，约 :80 之后新增 `resolveDataRuleColumn`）
- Modify: `query.go:1005-1014`（`applyDataRule` 开头替换）
- Create: `datarule_table_test.go`（含 package/import + `drUser` 模型 + `drDataRuleSQL` helper + AC-3）

- [ ] **Step 1: 写 AC-3 失败测试（新建 datarule_table_test.go，含模型 + helper + AC-3）**

```go
package gplus

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// drUser DataRule.Table 测试专用模型（自引用，用于自连接验证）。
type drUser struct {
	ID     int64  `gorm:"column:id;primaryKey;autoIncrement"`
	DeptID uint   `gorm:"column:dept_id"`
	Age    uint   `gorm:"column:age"`
	Name   string `gorm:"column:name"`
}

func (drUser) TableName() string { return "dr_users" }

// drDataRuleSQL 注入 rules 后用 DryRun 生成 SQL（去引号）并返回 q（供取 GetError）。
// 单表 drUser，不执行；用于操作符穿透（AC-8/9）与负例断言（AC-3/4/10/11）。
func drDataRuleSQL(t *testing.T, rules []DataRule) (q *Query[drUser], sql string) {
	t.Helper()
	db := newDryRunDB(t)
	ctx := context.WithValue(context.Background(), DataRuleKey, rules)
	qq, _ := NewQuery[drUser](ctx)
	stmt := db.Session(&gorm.Session{DryRun: true}).
		Model(&drUser{}).
		Scopes(qq.DataRuleBuilder().BuildQuery()).
		Find(&[]drUser{}).Statement
	return qq, stripIdentQuotes(stmt.SQL.String())
}

// AC-3: Table 非空 + Column 含点 → fail-fast，GetError 含原始 "dept.id"，DryRun WHERE 不含 dept
func TestDataRuleTable_failfast_when_table_set_and_column_has_dot(t *testing.T) {
	rules := []DataRule{{Table: "ext", Column: "dept.id", Condition: "=", Value: "1"}}
	q, sql := drDataRuleSQL(t, rules)
	err := q.DataRuleBuilder().GetError()
	if err == nil || !strings.Contains(err.Error(), "dept.id") {
		t.Fatalf("期望 fail-fast 错误含原始 dept.id，实际: %v", err)
	}
	if strings.Contains(sql, "dept") {
		t.Fatalf("fail-fast 后 WHERE 不应含 dept，实际 SQL: %s", sql)
	}
}
```

- [ ] **Step 2: 跑测试确认编译失败（RED）**

Run: `go test -run TestDataRuleTable_failfast ./...`
Expected: 编译失败 `unknown field 'Table' in struct literal of type DataRule`（`Table` 字段尚未定义）。

- [ ] **Step 3: `DataRule` struct 加 `Table` 字段（builder.go:54-59）**

将现有：
```go
// DataRule 对外开放的核心规则字段
type DataRule struct {
	Column    string   // 规则字段 (例如: "dept_id")；仅允许字母/数字/下划线及单个点分隔的表名前缀（如 "table.col"），含括号或运算符的表达式会被拒绝
	Condition string   // 规则条件 (例如: "=", "IN", "LIKE")
	Value     string   // 规则值   (例如: "1001"）；IN/NOT IN/BETWEEN 建议使用 Values
	Values    []string // IN/NOT IN/BETWEEN 的多值列表，优先于 Value 的逗号分隔解析
}
```
替换为：
```go
// DataRule 对外开放的核心规则字段
type DataRule struct {
	// Table 表名或 JOIN 别名前缀（如 "ext"）；空字符串表示作用于主表。
	// 仅允许单段标识符（不含点）；含点 / 含空白 / 非法字符将被拒绝。
	// Table 非空时 Column 必须是裸列名（不含点）；跨表数据权限的推荐写法。
	Table     string
	// Column 规则字段 (例如: "dept_id")。Table 非空时必须是裸列名；
	// Table 为空时兼容 "table.col" 点前缀写法（旧 workaround，向后兼容；新代码建议用 Table）。
	Column    string   // 含括号或运算符的表达式会被拒绝
	Condition string   // 规则条件 (例如: "=", "IN", "LIKE")
	Value     string   // 规则值   (例如: "1001"）；IN/NOT IN/BETWEEN 建议使用 Values
	Values    []string // IN/NOT IN/BETWEEN 的多值列表，优先于 Value 的逗号分隔解析
}
```

- [ ] **Step 4: 新增 `resolveDataRuleColumn` helper（builder.go，`validTableName` 定义后约 :81）**

```go
// resolveDataRuleColumn 解析 DataRule 的最终列名并完成全部列名侧安全校验（INV-1 最后防线）。
// 返回的 column 可直接使用；err 非 nil 时已含完整上下文（含原始输入），
// 调用方只需 append 到 errs 并 return，不得再做任何额外列名校验。
//
// 内部顺序（安全关键，不可调整）：
//  1. Table == ""（旧路径，向后兼容）：validDataRuleColumn 校验 Column 后原样返回；
//  2. Table != ""（新路径）：Column 禁含点（fail-fast）→ Table 单段校验 →
//     拼接 → 拼接结果再过 validDataRuleColumn（防御性冗余，future-proof）。
func resolveDataRuleColumn(rule DataRule) (string, error) {
	// 1. 旧路径：Table 空，Column 过白名单后原样用（兼容 "table.col" 点前缀 workaround）
	if rule.Table == "" {
		if !validDataRuleColumn.MatchString(rule.Column) {
			return "", fmt.Errorf("data rule: invalid column %q", rule.Column)
		}
		return rule.Column, nil
	}
	// 2a. fail-fast：Table 已提供前缀，Column 不得再含点（禁两套等价写法）
	if strings.Contains(rule.Column, ".") {
		return "", fmt.Errorf("data rule: column %q must not contain '.' when table %q is set", rule.Column, rule.Table)
	}
	// 2b. Table 单段校验：validTableName 允许 schema.table 单点，故额外禁点落实"单段"决策
	if !validTableName.MatchString(rule.Table) || strings.Contains(rule.Table, ".") {
		return "", fmt.Errorf("data rule: invalid table %q", rule.Table)
	}
	// 2c. 拼接
	final := rule.Table + "." + rule.Column
	// 2d. INV-1 最后防线（防御性冗余，future-proof，禁删）：拼接结果必过白名单
	if !validDataRuleColumn.MatchString(final) {
		return "", fmt.Errorf("data rule: invalid column %q", final)
	}
	return final, nil
}
```

> 注：builder.go 已 import `fmt`、`strings`、`regexp`（`validTableName` 使用）。若 goimports 提示缺失再补，正常无需改 import。

- [ ] **Step 5: Query 侧接入（query.go:1006-1014 整体替换）**

将现有 `applyDataRule` 开头：
```go
	column := rule.Column
	c := strings.ToUpper(strings.TrimSpace(rule.Condition))
	value := rule.Value

	// 白名单校验列名，防止含括号/运算符的恶意表达式绕过 quoteColumn 转义
	if !validDataRuleColumn.MatchString(column) {
		q.errs = append(q.errs, fmt.Errorf("data rule: invalid column %q", column))
		return
	}
```
替换为（INV-2：在 value=="" early-return 之前；INV-3：与 Updater 侧同形态）：
```go
	// 解析列名 + 全部列名侧校验内聚进 helper（含 Table 前缀拼接，INV-1/INV-2/INV-3）
	column, err := resolveDataRuleColumn(rule)
	if err != nil {
		q.errs = append(q.errs, err)
		return
	}
	c := strings.ToUpper(strings.TrimSpace(rule.Condition))
	value := rule.Value
```

> 其后 value 空值检查、SQL/USE_SQL_RULES 拒绝、switch 操作符映射全部保持不变；switch 内 `q.Eq(column,...)` 此时的 `column` 即 helper 返回值（含 Table 前缀）。

- [ ] **Step 6: 跑测试确认通过（GREEN）**

Run: `go test -run TestDataRuleTable_failfast ./...`
Expected: PASS

- [ ] **Step 7: 全量回归（确认零回归，旧 DataRule 测试不受影响）**

Run: `go test ./...`
Expected: 全部 PASS（含既有 `TestQuery_ApplyDataRule_*`、`TestQuery_SQL` 等）

- [ ] **Step 8: Commit**

```bash
git add builder.go query.go datarule_table_test.go
git commit -m "feat: DataRule.Table 字段 + resolveDataRuleColumn helper + Query 侧接入（AC-3）"
```

---

## Task 2: Query 侧注入防护 / 单段 / 空格端到端（AC-4 / AC-10 / AC-11）

> **TDD 说明**：`resolveDataRuleColumn` 已在 Task 1 一体实现（含全部校验分支），本 Task 测试是对已实现 helper 各拒绝分支的**覆盖与回归锁**，预期直接 PASS。若任一条 RED，说明 helper 对应分支有缺陷，**修 helper 不修测试**。

**Files:**
- Modify: `datarule_table_test.go`（追加 AC-4 / AC-10 / AC-11 三个测试函数）

- [ ] **Step 1: 追加 AC-4 / AC-10 / AC-11 测试**

```go
// AC-4: 注入 payload（table-driven）→ 每个 GetError 非 nil 且条件未拼进 SQL
func TestDataRuleTable_rejects_injection_payloads(t *testing.T) {
	payloads := []struct {
		name  string
		table string
	}{
		{"双引号分号", `ext";DROP--`},
		{"反引号", "ext`alias"},
		{"尾点", "ext."},
		{"首点", ".ext"},
		{"尾空格", "ext "}, // 与 AC-11 呼应，AC-11 单列强调"不 TrimSpace"决策
		{"Tab", "ext\t"},
		{"换行", "ext\n"},
		{"西里尔同形", "еxt"}, // 首字符 U+0435，非 ASCII [a-zA-Z_]
	}
	for _, p := range payloads {
		t.Run(p.name, func(t *testing.T) {
			rules := []DataRule{{Table: p.table, Column: "dept_id", Condition: "=", Value: "1"}}
			q, sql := drDataRuleSQL(t, rules)
			if q.DataRuleBuilder().GetError() == nil {
				t.Fatalf("payload %q 期望 GetError 非 nil", p.table)
			}
			// 负例断言：非法输入被拒后该 rule 不生成任何条件，SQL 不含列名
			if strings.Contains(sql, "dept_id") {
				t.Fatalf("payload %q 非法但条件被拼进 SQL: %s", p.table, sql)
			}
		})
	}
}

// AC-10: Table 含点（多段 public.users）违反单段约束 → GetError 非 nil
func TestDataRuleTable_rejects_multi_segment_table(t *testing.T) {
	rules := []DataRule{{Table: "public.users", Column: "id", Condition: "=", Value: "1"}}
	q, sql := drDataRuleSQL(t, rules)
	if q.DataRuleBuilder().GetError() == nil {
		t.Fatal("Table=public.users 含点违反单段约束，期望 GetError 非 nil")
	}
	if strings.Contains(sql, "public") {
		t.Fatalf("多段 Table 被拒后不应拼进 SQL: %s", sql)
	}
}

// AC-11: Table 首尾空格 → GetError 非 nil（不做 TrimSpace，validTableName 拒）
func TestDataRuleTable_rejects_table_with_trailing_space(t *testing.T) {
	rules := []DataRule{{Table: "ext ", Column: "dept_id", Condition: "=", Value: "1"}}
	q, _ := drDataRuleSQL(t, rules)
	if q.DataRuleBuilder().GetError() == nil {
		t.Fatal(`Table="ext "（含尾空格，不 TrimSpace）期望 GetError 非 nil`)
	}
}
```

- [ ] **Step 2: 跑测试确认通过（回归锁，预期 PASS）**

Run: `go test -run 'TestDataRuleTable_rejects' ./...`
Expected: PASS（三个测试 + AC-4 八个子测试全绿）。任一 RED → 修 `resolveDataRuleColumn` 对应分支。

- [ ] **Step 3: Commit**

```bash
git add datarule_table_test.go
git commit -m "test: Query 侧 DataRule.Table 注入防护/单段/空格端到端锁（AC-4/AC-10/AC-11）"
```

---

## Task 3: Query 跨表正路 + 兼容 + 零回归 + 多 rule + 操作符穿透（AC-1/2/6/7/8/9）

真实执行（AC-1/2/6/7，自连接 `Count`）+ DryRun 结构（AC-8/9 操作符穿透）。自连接范式与 `insert_select_join_test.go` 一致（`NewQueryAs` 主别名 `m` + `As[drUser]` 副别名 `ext` + `CrossJoinAs` + `WhereRaw` 错位连接）。

> **运行时风险点（RED 阶段确认）**：AC-1/2/7 依赖 Round 3a 主别名 FROM 物化在 `BuildCount` 路径生效（spec Round 3a：BuildCount:139 调 `applyMainAlias`）。`insert_select_join_test.go` 已验证 `NewQueryAs+CrossJoinAs+WhereRaw` 真实执行可行（走 BuildQuery），Count 走 BuildCount 同样物化主别名。若 RED 报主别名/裸列遮蔽错误，对照 Round 3a 已知限制排查，**不改 helper**（helper 由 AC-8/9 DryRun 独立验证）。

**Files:**
- Modify: `datarule_table_test.go`（追加 `setupDRDB` helper + AC-1/2/6/7/8/9）

- [ ] **Step 1: 追加 setupDRDB helper + AC-1（跨表正路 + 强制对照）**

```go
// setupDRDB 建库 + AutoMigrate drUser + 自连接对照种子。
// 种子让"裸 dept_id"与"ext.dept_id"在错位自连接（ext.id=m.id+1）下结果必然不同。
func setupDRDB(t *testing.T) (*Repository[int64, drUser], *gorm.DB) {
	t.Helper()
	repo, db := setupTestDB[drUser](t)
	seeds := []drUser{
		{ID: 1, DeptID: 2, Age: 9, Name: "a"},
		{ID: 2, DeptID: 1, Age: 5, Name: "b"},
		{ID: 3, DeptID: 1, Age: 9, Name: "c"},
	}
	for i := range seeds {
		if err := db.Create(&seeds[i]).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return repo, db
}

// AC-1: 跨表正路（裸列 dept_id + Table:"ext"），ext 侧过滤，且与裸列对照行为不同（防死代码）
func TestDataRuleTable_crosstable_filters_ext_side_and_differs_from_bare(t *testing.T) {
	rule := []DataRule{{Table: "ext", Column: "dept_id", Condition: "=", Value: "1"}}

	// (1) 结构：DryRun WHERE 含 ext.dept_id（裸列经 helper 拼前缀，非旧点前缀 workaround）
	_, sql := drDataRuleSQL(t, rule)
	if !strings.Contains(sql, "ext.dept_id") {
		t.Fatalf("WHERE 期望含 ext.dept_id（helper 拼前缀），实际 SQL: %s", sql)
	}

	// (2) 行为：错位自连接真实执行，ext.dept_id=1 过滤 ext 侧
	// 配对 ext.id=m.id+1 → (m1,ext2)(m2,ext3)；ext2/ext3 dept 均=1 → 2 行
	repo, _ := setupDRDB(t)
	ctxExt := context.WithValue(context.Background(), DataRuleKey, rule)
	qExt, _ := repo.NewQueryAs(ctxExt, "m")
	qExt.CrossJoinAs(As[drUser](qExt, "ext")).WhereRaw("ext.id = m.id + 1")
	extCount, err := repo.Count(qExt)
	if err != nil {
		t.Fatalf("ext 前缀版 Count 失败: %v", err)
	}
	if extCount != 2 {
		t.Fatalf("ext.dept_id=1 期望 2 行（配对 ext2/ext3 均 dept1），实际 %d", extCount)
	}

	// (3) 强制对照（防假绿，不可省）：裸 dept_id 自连接两表同名列 → 行为必不同
	// （SQLite 报 ambiguous，或解析到 m.dept_id 致行数不同）。仅"无错且行数相同"才判死代码。
	repo2, _ := setupDRDB(t)
	ctxBare := context.WithValue(context.Background(), DataRuleKey,
		[]DataRule{{Column: "dept_id", Condition: "=", Value: "1"}})
	qBare, _ := repo2.NewQueryAs(ctxBare, "m")
	qBare.CrossJoinAs(As[drUser](qBare, "ext")).WhereRaw("ext.id = m.id + 1")
	bareCount, bareErr := repo2.Count(qBare)
	if bareErr == nil && bareCount == extCount {
		t.Fatalf("Table:\"ext\" 与裸 dept_id 结果相同(count=%d)，Table 前缀未改变行为=死代码", bareCount)
	}
}
```

- [ ] **Step 2: 跑 AC-1（RED：helper 已实现，结构断言应 PASS；首次真实自连接确认 Count 路径）**

Run: `go test -run TestDataRuleTable_crosstable -v ./...`
Expected: PASS。若 (2) 报主别名物化错误，查 Round 3a BuildCount 限制（不改 helper）。

- [ ] **Step 3: 追加 AC-2（旧 workaround 真实执行等价）**

```go
// AC-2: 旧点前缀写法 Table:"" Column:"ext.dept_id" 真实执行，与 AC-1 的 Table:"ext" 等价（零回归）
func TestDataRuleTable_legacy_dotprefix_equivalent_to_table_field(t *testing.T) {
	repo, _ := setupDRDB(t)
	ctx := context.WithValue(context.Background(), DataRuleKey,
		[]DataRule{{Table: "", Column: "ext.dept_id", Condition: "=", Value: "1"}})
	q, _ := repo.NewQueryAs(ctx, "m")
	q.CrossJoinAs(As[drUser](q, "ext")).WhereRaw("ext.id = m.id + 1")
	count, err := repo.Count(q)
	if err != nil {
		t.Fatalf("旧点前缀版 Count 失败: %v", err)
	}
	// 与 AC-1 ext 前缀版逐结果一致 = 2 行（证明旧路径零回归）
	if count != 2 {
		t.Fatalf("旧 workaround ext.dept_id=1 期望 2 行（与 AC-1 等价），实际 %d", count)
	}
}
```

- [ ] **Step 4: 追加 AC-6（单表裸列零回归）**

```go
// AC-6: 单表裸列 Table:"" Column:"dept_id" 真实执行，WHERE 裸 dept_id 无前缀，结果不变
func TestDataRuleTable_single_table_bare_column_no_regression(t *testing.T) {
	repo, _ := setupDRDB(t)
	ctx := context.WithValue(context.Background(), DataRuleKey,
		[]DataRule{{Table: "", Column: "dept_id", Condition: "=", Value: "1"}})
	q, _ := NewQuery[drUser](ctx)
	count, err := repo.Count(q)
	if err != nil {
		t.Fatalf("单表裸列 Count 失败: %v", err)
	}
	// 种子 dept_id=1：id2,id3 → 2 行
	if count != 2 {
		t.Fatalf("单表 dept_id=1 期望 2 行，实际 %d", count)
	}
	// 结构：裸 dept_id 无表前缀（GORM 字符串路径不自动加前缀）
	_, sql := drDataRuleSQL(t, []DataRule{{Column: "dept_id", Condition: "=", Value: "1"}})
	if !strings.Contains(sql, "dept_id") || strings.Contains(sql, ".dept_id") {
		t.Fatalf("单表应裸 dept_id 无前缀，实际 SQL: %s", sql)
	}
}
```

- [ ] **Step 5: 追加 AC-7（多 rule 新旧混用 + IN + AND + 参数不错位）**

```go
// AC-7: 两条 rule（新路径 ext.dept_id IN + 旧点前缀 m.age=）AND 共存，参数绑定不错位
func TestDataRuleTable_multi_rule_new_old_mixed_AND_no_param_corruption(t *testing.T) {
	repo, db := setupTestDB[drUser](t)
	// 种子让两条件唯一同时满足 1 行：仅 (m1 age9, ext2 dept1) 满足 ext.dept∈{1,2} ∧ m.age=9
	seeds := []drUser{
		{ID: 1, DeptID: 2, Age: 9, Name: "a"},
		{ID: 2, DeptID: 1, Age: 5, Name: "b"},
		{ID: 3, DeptID: 1, Age: 9, Name: "c"},
	}
	for i := range seeds {
		if err := db.Create(&seeds[i]).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	rules := []DataRule{
		{Table: "ext", Column: "dept_id", Condition: "IN", Values: []string{"1", "2"}}, // 新路径 + IN+Table 多值
		{Table: "", Column: "m.age", Condition: "=", Value: "9"},                       // 旧点前缀，作用主表别名 m
	}
	ctx := context.WithValue(context.Background(), DataRuleKey, rules)
	q, _ := repo.NewQueryAs(ctx, "m")
	q.CrossJoinAs(As[drUser](q, "ext")).WhereRaw("ext.id = m.id + 1")
	count, err := repo.Count(q)
	if err != nil {
		t.Fatalf("多 rule Count 失败: %v", err)
	}
	// AND 语义 + 参数不错位：配对 (m1 age9,ext2 dept1) 唯一同时满足 → 1
	// （OR 误实现会 >1；参数错位如 ext.dept=9 会 0）
	if count != 1 {
		t.Fatalf("ext.dept_id IN(1,2) ∧ m.age=9 期望 1 行，实际 %d", count)
	}
	// 结构：WHERE 同时含 ext.dept_id 与 m.age（新旧路径共存）
	_, sql := drDataRuleSQL(t, rules)
	if !strings.Contains(sql, "ext.dept_id") || !strings.Contains(sql, "m.age") {
		t.Fatalf("WHERE 期望同时含 ext.dept_id 与 m.age，实际: %s", sql)
	}
}
```

- [ ] **Step 6: 追加 AC-8 / AC-9（操作符穿透，验证 INV-2）**

```go
// AC-8: IS NULL + Table → ext.dept_id IS NULL（证明空值 early-return 前已解析 Table 前缀）
func TestDataRuleTable_is_null_carries_table_prefix(t *testing.T) {
	_, sql := drDataRuleSQL(t, []DataRule{{Table: "ext", Column: "dept_id", Condition: "IS NULL"}})
	if !strings.Contains(sql, "ext.dept_id IS NULL") {
		t.Fatalf("IS NULL 应带 ext 前缀（INV-2），实际: %s", sql)
	}
}

// AC-9: BETWEEN + Table + 多值 → ext.age BETWEEN（证明 Table 穿透到非 = 多值分支）
func TestDataRuleTable_between_carries_table_prefix(t *testing.T) {
	_, sql := drDataRuleSQL(t, []DataRule{{Table: "ext", Column: "age", Condition: "BETWEEN", Values: []string{"10", "30"}}})
	if !strings.Contains(sql, "ext.age BETWEEN") {
		t.Fatalf("BETWEEN 应带 ext 前缀，实际: %s", sql)
	}
}
```

- [ ] **Step 7: 跑 Task 3 全部测试确认通过**

Run: `go test -run TestDataRuleTable ./...`
Expected: PASS（AC-1/2/3/4/6/7/8/9/10/11 全绿）

- [ ] **Step 8: Commit**

```bash
git add datarule_table_test.go
git commit -m "test: Query 侧 DataRule.Table 跨表正路/兼容/多rule/操作符穿透（AC-1/2/6/7/8/9）"
```

---

## Task 4: Updater 侧接入 + 对称 AC（AC-5a~5e）

Updater 侧 `applyDataRule` 尚未接入 helper（仍 `column := rule.Column`，忽略 `Table`），是**真未实现路径**——本 Task 测试是真 RED → 接入 → GREEN。接入形态必须与 Query 侧逐字对应（INV-3）。

**Files:**
- Modify: `update.go:654-662`（`applyDataRule` 开头替换，镜像 query.go）
- Modify: `datarule_table_test.go`（追加 `drUpdaterSQL` / `drUpdaterErr` helper + AC-5a~5e）

- [ ] **Step 1: 追加 Updater helper + AC-5a~5e 测试（RED）**

```go
// drUpdaterSQL 注入 rules 后用 ToSQL 生成 UPDATE SQL（去引号）。Set name 防 ErrUpdateEmpty。
func drUpdaterSQL(t *testing.T, rules []DataRule) (u *Updater[drUser], sql string) {
	t.Helper()
	db := newDryRunDB(t)
	ctx := context.WithValue(context.Background(), DataRuleKey, rules)
	uu, mu := NewUpdater[drUser](ctx)
	uu.Set(&mu.Name, "x")
	s, _ := uu.DataRuleBuilder().ToSQL(db)
	return uu, stripIdentQuotes(s)
}

// drUpdaterErr 注入 rules 后返回 Updater 累积错误（用于 fail-fast / 注入断言）。
func drUpdaterErr(t *testing.T, rules []DataRule) error {
	t.Helper()
	ctx := context.WithValue(context.Background(), DataRuleKey, rules)
	uu, mu := NewUpdater[drUser](ctx)
	uu.Set(&mu.Name, "x")
	uu.DataRuleBuilder()
	return uu.GetError()
}

// AC-5a: Updater 跨表正路 → UPDATE WHERE 含 ext.dept_id
func TestDataRuleTable_updater_crosstable_prefix(t *testing.T) {
	_, sql := drUpdaterSQL(t, []DataRule{{Table: "ext", Column: "dept_id", Condition: "=", Value: "1"}})
	if !strings.Contains(sql, "ext.dept_id") {
		t.Fatalf("Updater UPDATE WHERE 期望含 ext.dept_id，实际: %s", sql)
	}
}

// AC-5b: Updater 注入防护 → GetError 非 nil
func TestDataRuleTable_updater_rejects_injection(t *testing.T) {
	if drUpdaterErr(t, []DataRule{{Table: `ext";DROP--`, Column: "dept_id", Condition: "=", Value: "1"}}) == nil {
		t.Fatal("Updater 注入 payload 期望 GetError 非 nil")
	}
}

// AC-5c: Updater fail-fast（Table 非空 + Column 含点）→ 错误含原始 dept.id
func TestDataRuleTable_updater_failfast_column_dot(t *testing.T) {
	err := drUpdaterErr(t, []DataRule{{Table: "ext", Column: "dept.id", Condition: "=", Value: "1"}})
	if err == nil || !strings.Contains(err.Error(), "dept.id") {
		t.Fatalf("Updater fail-fast 期望错误含原始 dept.id，实际: %v", err)
	}
}

// AC-5d: Updater 零回归（单表裸列）→ UPDATE WHERE 裸 dept_id 无前缀
func TestDataRuleTable_updater_single_table_no_regression(t *testing.T) {
	_, sql := drUpdaterSQL(t, []DataRule{{Table: "", Column: "dept_id", Condition: "=", Value: "1"}})
	if !strings.Contains(sql, "dept_id") || strings.Contains(sql, ".dept_id") {
		t.Fatalf("Updater 单表应裸 dept_id 无前缀，实际: %s", sql)
	}
}

// AC-5e: Updater 操作符穿透（IS NULL + Table）→ ext.dept_id IS NULL（对称 AC-8，验证 Updater 侧 INV-2）
func TestDataRuleTable_updater_is_null_carries_prefix(t *testing.T) {
	_, sql := drUpdaterSQL(t, []DataRule{{Table: "ext", Column: "dept_id", Condition: "IS NULL"}})
	if !strings.Contains(sql, "ext.dept_id IS NULL") {
		t.Fatalf("Updater IS NULL 应带 ext 前缀（INV-2），实际: %s", sql)
	}
}
```

- [ ] **Step 2: 跑测试确认失败（RED）**

Run: `go test -run TestDataRuleTable_updater ./...`
Expected: FAIL。AC-5a/5e 因旧代码忽略 `Table` 生成裸 `dept_id`（无 ext 前缀）；AC-5b/5c 因旧代码忽略 `Table` 不报错（GetError nil）。

- [ ] **Step 3: Updater 侧接入（update.go:654-662 整体替换，镜像 Query 侧）**

将现有 `applyDataRule` 开头：
```go
	column := rule.Column
	c := strings.ToUpper(strings.TrimSpace(rule.Condition))
	value := rule.Value

	// 白名单校验列名，防止含括号/运算符的恶意表达式绕过 quoteColumn 转义
	if !validDataRuleColumn.MatchString(column) {
		u.errs = append(u.errs, fmt.Errorf("data rule: invalid column %q", column))
		return
	}
```
替换为（与 query.go 逐字对应，仅 `q.errs`→`u.errs`；INV-2/INV-3）：
```go
	// 解析列名 + 全部列名侧校验内聚进 helper（含 Table 前缀拼接，INV-1/INV-2/INV-3）
	column, err := resolveDataRuleColumn(rule)
	if err != nil {
		u.errs = append(u.errs, err)
		return
	}
	c := strings.ToUpper(strings.TrimSpace(rule.Condition))
	value := rule.Value
```

> 其后 value 空值检查、SQL/USE_SQL_RULES 拒绝（提示 RawExec）、switch 操作符映射保持不变。

- [ ] **Step 4: 跑测试确认通过（GREEN）**

Run: `go test -run TestDataRuleTable_updater ./...`
Expected: PASS（AC-5a~5e 全绿）

- [ ] **Step 5: 全量回归（确认 Updater 既有 DataRule 测试零回归）**

Run: `go test ./...`
Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add update.go datarule_table_test.go
git commit -m "feat: Updater 侧 DataRule.Table 接入（对称 Query 侧）+ AC-5a~5e"
```

---

## Task 5: 文档修订（CHANGELOG / README / godoc）+ 全量回归 + 覆盖率

> **外科手术原则**：CHANGELOG 历史条目（v0.8.0/v0.9.0 已发布段）**不改写原文**，仅在行尾追加轻量指向标注（保留历史完整性 + 可追溯），新内容全部进 v0.10 新段。

**Files:**
- Modify: `CHANGELOG.md`（顶部新增 v0.10 段 + 三处行尾标注）
- Modify: `README.md`（DataRule 章节补 `Table` 字段说明）

- [ ] **Step 1: CHANGELOG 顶部新增 v0.10.0 段**

在 `CHANGELOG.md:5`（`## [0.9.0] - 2026-06-05` 行）**之前**插入：
```markdown
## [0.10.0] - 2026-06-08

本版新增 `DataRule.Table` 跨表数据权限字段，兑现 v0.8.0 路线图承诺并消除 v0.9.0 反向兼容债。向后兼容，MINOR 版本。

### 新增

- **`DataRule.Table` 跨表数据权限字段**：为数据权限规则显式指定作用的表名 / JOIN 别名前缀（如 `Table: "ext"`），生成 `ext.dept_id` 限定列。
  - 单一真相源 helper `resolveDataRuleColumn`：全部列名侧校验内聚（旧路径 Column 走 `validDataRuleColumn` 白名单；新路径 Table 单段校验 + 拼接结果防御性复校验，INV-1 最后防线）
  - Query 与 Updater 两侧接入塌缩为同一形态（INV-3 防双侧漂移），置于空值 early-return 之前（INV-2，保证 IS NULL / BETWEEN 等操作符也带 Table 前缀）
  - fail-fast：`Table` 非空时 `Column` 禁含点（禁两套等价写法）；`Table` 仅单段（拒 `schema.table`）、不 TrimSpace
  - 向后兼容：`Table` 空时旧 `Column:"ext.dept_id"` 点前缀写法零回归

### 变更（还反向兼容债）

- 兑现 v0.8.0 路线图「跨表 DataRule（DataRule.Table 字段）」承诺（v0.9.0 未交付，本版补齐）。
- v0.8.0 行为约束「DataRule.Column 不应写 alias 前缀」更新：跨表权限现由 `Table` 字段正式提供；旧 Column 点前缀仍向后兼容，新代码引导用 `Table`。
- v0.9.0 已知限制「DataRule 裸列自连接 ambiguous，须 Column 自带别名前缀」被取代：现可写 `Table:"ext", Column:"dept_id"`（裸列），由 gplus 拼前缀。

---

```

- [ ] **Step 2: CHANGELOG 三处历史条目行尾追加指向标注**

| 行 | 现有内容（锚点） | 追加 |
|---|---|---|
| :275 | `- 跨表 DataRule（DataRule.Table 字段）→ v0.9` | 行尾改为 `→ v0.9（实际于 0.10.0 交付）` |
| :260 | `...提前在 Column 写 alias 前缀会形成兼容性陷阱` | 句尾追加 `（0.10.0 `Table` 字段已正式提供，见上）` |
| v0.9.0 段「DataRule 裸列自连接 ambiguous」行（约 :34） | `- **DataRule 裸列自连接 ambiguous**：用户须 `Column` 自带别名前缀` | 句尾追加 `（0.10.0 `DataRule.Table` 已支持裸列跨表）` |

精确 Edit（:275）：
```
- 跨表 DataRule（DataRule.Table 字段）→ v0.9
```
改为：
```
- 跨表 DataRule（DataRule.Table 字段）→ v0.9（实际于 0.10.0 交付）
```

精确 Edit（:260）：
```
- **DataRule.Column 不应写 alias 前缀**：v0.9 cross-table DataRule 通过新增 Table 字段提供，提前在 Column 写 alias 前缀会形成兼容性陷阱
```
改为：
```
- **DataRule.Column 不应写 alias 前缀**：v0.9 cross-table DataRule 通过新增 Table 字段提供，提前在 Column 写 alias 前缀会形成兼容性陷阱（0.10.0 `Table` 字段已正式提供）
```

精确 Edit（v0.9.0 已知限制段）：
```
- **DataRule 裸列自连接 ambiguous**：用户须 `Column` 自带别名前缀
```
改为：
```
- **DataRule 裸列自连接 ambiguous**：用户须 `Column` 自带别名前缀（0.10.0 `DataRule.Table` 已支持裸列跨表）
```

- [ ] **Step 3: README 补 `DataRule.Table` 说明**

先定位 README 的 DataRule 章节：`grep -n "DataRule" README.md`。在 DataRule 字段说明 / 示例之后追加：
```markdown
#### 跨表数据权限：`DataRule.Table`

JOIN 多表查询时，用 `Table` 字段把数据权限规则限定到指定表 / JOIN 别名：

```go
rules := []gplus.DataRule{
    {Table: "ext", Column: "dept_id", Condition: "=", Value: "1"}, // 生成 ext.dept_id = 1
}
ctx = context.WithValue(ctx, gplus.DataRuleKey, rules)
```

- `Table` 的值必须与查询中 JOIN 别名（`As[X]` 注册名 / `NewQueryAs` 主别名）**字符串一致**；gplus 不校验别名是否存在，拼错由数据库执行期报错。
- `Table` 仅允许单段标识符（不含点 / 空白）；`Table` 非空时 `Column` 必须是裸列名（不得再含点）。
- 向后兼容：不填 `Table`、在 `Column` 写 `"ext.dept_id"` 点前缀的旧写法仍可用，但新代码建议用 `Table`。
```

> godoc 已在 Task 1 随 `DataRule` struct 字段注释完成，无需额外改动。

- [ ] **Step 4: 全量回归 + 覆盖率**

```bash
go test ./...
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | Select-String total
```
Expected: 全部 PASS；总覆盖率 ≥ 94%（目标维持 ~95%，新增 helper 分支应被 14 AC 充分覆盖）。

- [ ] **Step 5: go vet 静态检查**

Run: `go vet ./...`
Expected: 无输出（干净）

- [ ] **Step 6: Commit**

```bash
git add CHANGELOG.md README.md
git commit -m "docs: CHANGELOG v0.10 DataRule.Table + README 跨表权限指引 + 还反向兼容债"
```

---

## Self-Review（plan 完成后自查，已执行）

**1. Spec 覆盖**（14 AC → task 映射）：
- AC-1 → Task 3 Step 1；AC-2 → Task 3 Step 3；AC-3 → Task 1 Step 1；AC-4 → Task 2 Step 1；
- AC-6 → Task 3 Step 4；AC-7 → Task 3 Step 5；AC-8/AC-9 → Task 3 Step 6；
- AC-10/AC-11 → Task 2 Step 1；AC-5a~5e → Task 4 Step 1。**14 AC 全覆盖，无遗漏。**
- INV-1（helper 最后防线）→ Task 1 Step 4 helper step 2d；INV-2（early-return 前调用）→ Task 1 Step 5 / Task 4 Step 3 接入位置 + AC-8/9/5e 验证；INV-3（双侧塌缩同形态）→ Task 1 Step 5 与 Task 4 Step 3 逐字对应。
- 文档修订（CHANGELOG :275/:260/:34 + README）→ Task 5。

**2. Placeholder 扫描**：无 TBD / TODO；每个测试步骤含完整可编译代码；每个实现步骤含完整 before/after 代码块。

**3. 类型一致性**：`drUser`（含 `Age` 字段）在 Task 1 定义、Task 3/4 复用；`drDataRuleSQL`（Task 1）/ `setupDRDB`（Task 3）/ `drUpdaterSQL`+`drUpdaterErr`（Task 4）helper 签名前后一致；`resolveDataRuleColumn(rule DataRule) (string, error)` 在 builder.go 定义、query.go/update.go 调用签名一致；`repo.Count` 返回 `(int64, error)`、`u.ToSQL(db)` 返回 `(string, error)` 均与现有代码核对无误。

**已知运行时风险点（RED 阶段确认，已在 Task 3 标注）**：AC-1/2/7 真实自连接依赖 Round 3a 主别名 FROM 物化在 `BuildCount` 生效；范式取自已通过的 `insert_select_join_test.go`。若 RED 暴露主别名/裸列遮蔽问题，排查 Round 3a 限制，不改 helper（helper 由 DryRun AC 独立验证）。

---

## 执行依赖顺序

Task 1（helper + Query 接入 + AC-3）→ Task 2（Query 注入端到端）→ Task 3（Query 真实执行）→ Task 4（Updater 接入，真 RED→GREEN）→ Task 5（文档 + 回归）。Task 2/3 依赖 Task 1 的 helper；Task 4 独立于 Task 2/3（仅依赖 Task 1 helper）；Task 5 最后。

**Subagent 分层建议**（按全局 CLAUDE.md）：Task 1/3/4 含算法/边界/真实执行 → `sonnet`；Task 2 端到端断言 → `sonnet`；Task 5 文档 → `haiku`；每 Task 完成后 `opus` final review（跨方法语义一致性 + INV-1/2/3 不变量复核）。

