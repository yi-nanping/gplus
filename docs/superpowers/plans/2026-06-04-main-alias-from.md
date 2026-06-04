# 主别名 FROM 物化（Round 3a）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让顶层 `NewQueryAs(alias)` 设置的主表别名在 SELECT 路径（BuildQuery/BuildCount）物化为 `FROM <table> AS <alias>`，使引用主别名的查询在真实执行下合法，解除 InsertSelect scenario 2（Round 3b）的阻塞。

**Architecture:** 在 `ScopeBuilder` 新增 `mainAlias`/`mainAliasTable` 两字段 + 独立方法 `applyMainAlias(db,qL,qR)`，**仅由 BuildQuery/BuildCount 显式调用**（写路径 BuildUpdate/BuildDelete 不调用，结构性禁止 `DELETE/UPDATE ... AS alias` 跨方言非法）。`NewQueryAs` 在别名合法时预存这两个字段；`SubQuery`/`SubQueryAs` 调 `NewQueryAs` 后清空它们（子查询靠 ToDB 的 `Model()` 注入 FROM，不物化）。

**Tech Stack:** Go 1.24（go.mod 触发自动下载；本机用 `D:/Environment/golang/go1.21.11/bin/go.exe`），GORM + glebarez/sqlite in-memory 测试，标准库 `testing` + `strings`。

**Spec:** `docs/superpowers/specs/2026-06-04-main-alias-from-design.md`（11 条 AC，两轮 4 视角审计 + plan 探针实测定稿）

**关键探针实测结论（已删探针，2026-06-04）：**
- 带引号 `db.Table(`"closure" AS "ext"`)` 在 SQLite 真实执行成功，GORM 原样透传，`ToSQL` 含 `closure" AS "ext`。
- Count 路径 `Model+Table` → `SELECT count(*) FROM "closure" AS "ext"`，cnt 正确。
- 自连接（主表带引号 FROM + JOIN 不带引号）`Find` 成功返回期望行。
- **First 路径失败**：`.First()` 自动追加 `ORDER BY \`closure\`.\`id\``（裸表名主键），被别名遮蔽 → `no such column: closure.id`。`GetOne/Last/GetByLock/FirstOrCreate` 不支持主别名（已知限制 AC-11）。
- DataRule 裸列 `depth` 在自连接下 → `ambiguous column name: depth`（单表不报）。

**通用命令（PowerShell / bash 均用完整 go 路径）：**
```bash
# 跑单个测试
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestMainAlias_xxx -v ./...
# 全量
D:/Environment/golang/go1.21.11/bin/go.exe test ./...
# 覆盖率
D:/Environment/golang/go1.21.11/bin/go.exe test -coverprofile=coverage.out ./... && D:/Environment/golang/go1.21.11/bin/go.exe tool cover -func=coverage.out | tail -1
```

---

## File Structure

| 文件 | 职责 | 变更 |
|---|---|---|
| `builder.go` | ScopeBuilder 基类 | 新增 2 字段、`validTableName` 正则、`applyMainAlias` 方法；BuildQuery/BuildCount 接线；Clear 重置 |
| `query.go` | Query[T] + NewQueryAs | NewQueryAs 别名合法时预存 mainAlias/mainAliasTable |
| `subquery.go` | SubQuery/SubQueryAs | 调 NewQueryAs 后清空 mainAlias/mainAliasTable |
| `main_alias_from_test.go` | 新增测试文件 | AC-1..AC-11 共 11 个测试函数 + validTableName 表驱动单测 |

---

## Task 1: 核心物化 — 字段 + applyMainAlias + BuildQuery/Count 接线 + NewQueryAs 设值

**AC:** AC-1, AC-2, AC-3

**Files:**
- Modify: `builder.go`（ScopeBuilder 字段 ~82、validTableName ~77、applyMainAlias 新方法、BuildQuery ~151 / BuildCount ~128 接线）
- Modify: `query.go`（NewQueryAs ~46-58）
- Test: `main_alias_from_test.go`（新建）

- [ ] **Step 1: 写 AC-1 失败测试（仅用公开 API，实现前即可编译并红）**

新建 `main_alias_from_test.go`：

```go
package gplus

import (
	"context"
	"strings"
	"testing"
)

// AC-1：NewQueryAs 主别名查询经 List 真实执行，FROM 物化为 closure AS ext
func TestMainAlias_list_with_alias_emits_from_as(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	q, m := repo.NewQueryAs(ctx, "ext")
	q.Select(&m.AncestorID).Eq(&m.DescendantID, 5)

	list, err := repo.List(q)
	if err != nil {
		t.Fatalf("List 应成功，实际 err: %v", err)
	}
	if len(list) != 1 || list[0].AncestorID != 1 {
		t.Fatalf("期望 1 行 AncestorID=1，实际 %+v", list)
	}

	sql, _ := q.ToSQL(db)
	if !strings.Contains(sql, `closure" AS "ext`) {
		t.Errorf("FROM 应含 closure AS ext，实际 SQL: %s", sql)
	}
}
```

- [ ] **Step 2: 跑测试确认红**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestMainAlias_list_with_alias_emits_from_as -v ./...`
Expected: FAIL，`List 应成功，实际 err: ... no such column: ext.ancestor_id`（主别名未物化进 FROM）

- [ ] **Step 3: 加 ScopeBuilder 两字段**

`builder.go`，在 `tableName string`（~82）后插入：

```go
	// tableName 动态表名
	tableName string
	// mainAlias 主表别名（NewQueryAs 设置；仅 SELECT 路径 BuildQuery/BuildCount 物化为 FROM table AS alias）
	mainAlias string
	// mainAliasTable 主表别名对应的裸表名。
	// ScopeBuilder 是非泛型基类拿不到 queryCore.aliases map，故由有 T 的 NewQueryAs 预存（mainAliasTable 冗余于 aliasEntry.typ 是抽象边界的必然妥协）。
	mainAliasTable string
```

- [ ] **Step 4: 加 validTableName 正则**

`builder.go`，在 `validDataRuleColumn`（~76）下方插入：

```go
// validTableName 白名单校验主别名物化时的表名，防注入（防 Table("x\"; DROP--") 经 qL+table+qR 拼接被引号提前闭合）。
// 与 validDataRuleColumn 完全同源（单点）：允许 closure / closure_2024 / schema.table；多段 a.b.c 不支持（引号位置 + YAGNI）。
var validTableName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)?$`)
```

- [ ] **Step 5: 加 applyMainAlias 方法**

`builder.go`，在 `applyBaseTable`（~278 结束）后插入：

```go
// applyMainAlias 物化主表别名为 FROM <table> AS <alias>。
// 仅 SELECT 路径（BuildQuery/BuildCount）调用；写路径 BuildUpdate/BuildDelete 不调用
// （DELETE/UPDATE ... AS alias 在 MySQL/PG 多方言非法，结构性禁止）。
// table 优先用 b.tableName（用户 Table() 覆盖值），否则用 NewQueryAs 预存的 mainAliasTable。
func (b *ScopeBuilder) applyMainAlias(db *gorm.DB, qL, qR string) *gorm.DB {
	if b.mainAlias == "" {
		return db
	}
	table := b.tableName
	if table == "" {
		table = b.mainAliasTable
	}
	if !validTableName.MatchString(table) {
		return db // 表名非法：保守不物化（异常会在 GORM 执行层暴露）
	}
	return db.Table(qL + table + qR + " AS " + qL + b.mainAlias + qR)
}
```

- [ ] **Step 6: BuildQuery 接线**

`builder.go` BuildQuery（~151），在 `db = b.applyBaseTable(db)` 后插入一行：

```go
		// 基础条件
		db = b.applyBaseTable(db)
		// 主别名物化（仅 SELECT 路径）
		db = b.applyMainAlias(db, qL, qR)
		// 查询字段
		db = b.applySelects(db, qL, qR)
```

- [ ] **Step 7: BuildCount 接线**

`builder.go` BuildCount（~128），在 `db = b.applyBaseTable(db)` 后插入一行：

```go
		//  基础条件
		db = b.applyBaseTable(db)
		// 主别名物化（仅 SELECT 路径）
		db = b.applyMainAlias(db, qL, qR)
		// where
		db = b.applyWhere(db, qL, qR)
```

- [ ] **Step 8: NewQueryAs 预存别名字段**

`query.go` NewQueryAs（~46-58），在 `t := As[T](q, alias)` 后、`return` 前插入合法性判断：

```go
func NewQueryAs[T any](ctx context.Context, alias string) (*Query[T], *T) {
	q := &Query[T]{
		ctx:  ctx,
		core: newQueryCore(ctx),
		errs: make([]error, 0, 8),
		ScopeBuilder: ScopeBuilder{
			conditions: make([]condition, 0, 8),
		},
	}
	// 复用 As 的全部校验逻辑（name 正则 / 链查重 / 创建独立实例）
	t := As[T](q, alias)
	// 仅合法别名才物化 FROM：As 对非法别名已累积 ErrAliasInvalidName 并返回规范单例，
	// 不写 mainAlias 避免把坏别名拼进 db.Table("... AS 1bad") 生成语法错误 SQL。
	if aliasNameRegexp.MatchString(alias) {
		q.mainAlias = alias
		q.mainAliasTable = q.mainTableName()
	}
	return q, t
}
```

- [ ] **Step 9: 跑 AC-1 确认绿**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestMainAlias_list_with_alias_emits_from_as -v ./...`
Expected: PASS

- [ ] **Step 10: 加 AC-2（ToDB）+ AC-3（无别名零回归）测试**

`main_alias_from_test.go` 追加：

```go
// AC-2：q.ToDB(db) 物化路径真实执行，FROM 含别名
func TestMainAlias_todb_materializes_alias(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	q, m := repo.NewQueryAs(ctx, "ext")
	q.Select(&m.AncestorID).Eq(&m.DescendantID, 5)

	var rows []Closure
	if err := q.ToDB(db).Find(&rows).Error; err != nil {
		t.Fatalf("ToDB Find 应成功，实际 err: %v", err)
	}
	if len(rows) != 1 || rows[0].AncestorID != 1 {
		t.Fatalf("期望 1 行 AncestorID=1，实际 %+v", rows)
	}
}

// AC-3：无别名查询 FROM 为裸表名，不含 AS（零回归）
func TestMainAlias_no_alias_from_has_no_as(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	q, m := repo.NewQuery(ctx)
	q.Select(&m.AncestorID)

	sql, _ := q.ToSQL(db)
	if !strings.Contains(sql, "FROM `closure`") {
		t.Errorf("无别名 FROM 应为裸表名 `closure`，实际: %s", sql)
	}
	if strings.Contains(sql, " AS ") {
		t.Errorf("无别名查询 FROM 不应含 AS，实际: %s", sql)
	}
	if _, err := repo.List(q); err != nil {
		t.Fatalf("无别名 List 应成功，实际: %v", err)
	}
}
```

- [ ] **Step 11: 跑 AC-1/2/3 全绿**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestMainAlias -v ./...`
Expected: PASS（3 个测试）

- [ ] **Step 12: Commit**

```bash
git add builder.go query.go main_alias_from_test.go
git commit -m "feat: 主别名 FROM 物化核心（applyMainAlias + NewQueryAs 预存，AC-1/2/3）"
```

---

## Task 2: Clear 重置 + Table override 组合

**AC:** AC-5, AC-4

**Files:**
- Modify: `builder.go`（Clear ~207-232）
- Test: `main_alias_from_test.go`

- [ ] **Step 1: 写 AC-5 失败测试（Clear 后内部字段重置）**

`main_alias_from_test.go` 追加：

```go
// AC-5：Clear 后 mainAlias/mainAliasTable 重置，FROM 不含别名
func TestMainAlias_clear_resets_alias_fields(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	q, _ := repo.NewQueryAs(ctx, "ext")

	q.Clear()

	if q.mainAlias != "" || q.mainAliasTable != "" {
		t.Fatalf("Clear 后 mainAlias/mainAliasTable 应为空，实际 mainAlias=%q mainAliasTable=%q", q.mainAlias, q.mainAliasTable)
	}
	sql, _ := q.ToSQL(db)
	if strings.Contains(sql, `AS "ext"`) {
		t.Errorf("Clear 后 FROM 不应含 AS \"ext\"，实际: %s", sql)
	}
}
```

- [ ] **Step 2: 跑测试确认红**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestMainAlias_clear_resets_alias_fields -v ./...`
Expected: FAIL（`Clear 后 mainAlias/mainAliasTable 应为空`，因 Clear 未重置新字段）

- [ ] **Step 3: Clear 重置两字段**

`builder.go` Clear（~209），在 `b.tableName = ""` 后插入：

```go
	// 1. 基础字段复位
	b.tableName = "" //必须清除表名
	b.mainAlias = ""
	b.mainAliasTable = ""
	b.limit = 0
```

- [ ] **Step 4: 跑 AC-5 确认绿**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestMainAlias_clear_resets_alias_fields -v ./...`
Expected: PASS

- [ ] **Step 5: 加 AC-4（Table override 真实执行）测试**

`main_alias_from_test.go` 追加：

```go
// AC-4：NewQueryAs + Table("closure_2024") → FROM closure_2024 AS ext（Table 覆盖优先）
func TestMainAlias_table_override_uses_custom_table(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	// 建 closure_2024 表（同 schema）并播种
	if err := db.Exec(`CREATE TABLE closure_2024 (id integer PRIMARY KEY AUTOINCREMENT, ancestor_id integer, descendant_id integer, depth integer)`).Error; err != nil {
		t.Fatalf("create closure_2024: %v", err)
	}
	if err := db.Exec(`INSERT INTO closure_2024 (ancestor_id, descendant_id, depth) VALUES (7,5,0)`).Error; err != nil {
		t.Fatalf("seed closure_2024: %v", err)
	}

	q, m := repo.NewQueryAs(ctx, "ext")
	q.Table("closure_2024").Select(&m.AncestorID).Eq(&m.DescendantID, 5)

	list, err := repo.List(q)
	if err != nil {
		t.Fatalf("List 应成功，实际: %v", err)
	}
	if len(list) != 1 || list[0].AncestorID != 7 {
		t.Fatalf("期望 1 行 AncestorID=7，实际 %+v", list)
	}
	sql, _ := q.ToSQL(db)
	if !strings.Contains(sql, `closure_2024" AS "ext`) {
		t.Errorf("FROM 应含 closure_2024 AS ext，实际: %s", sql)
	}
}
```

- [ ] **Step 6: 跑 AC-4 确认绿（无需新实现，applyMainAlias 已支持 tableName 覆盖）**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestMainAlias_table_override_uses_custom_table -v ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add builder.go main_alias_from_test.go
git commit -m "feat: Clear 重置主别名字段 + Table override 组合（AC-5/4）"
```

---

## Task 3: 子查询零污染 — SubQuery/SubQueryAs 清空 mainAlias

**AC:** AC-7

**Files:**
- Modify: `subquery.go`（SubQuery ~43-45、SubQueryAs ~64-66）
- Test: `main_alias_from_test.go`

- [ ] **Step 1: 写 AC-7 失败测试（子查询 FROM 不被主别名污染）**

`main_alias_from_test.go` 追加：

```go
// AC-7：SubQuery 派生的子查询不被主别名物化波及（C-1 回归守卫）
func TestMainAlias_subquery_from_not_polluted(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	outer, ou := repo.NewQuery(ctx)
	sub, su := SubQuery[Closure](outer)
	sub.Select(&su.AncestorID).Eq(&su.DescendantID, 5)
	outer.InSub(&ou.AncestorID, sub)

	sql, _ := outer.ToSQL(db)
	if strings.Contains(sql, "closure AS closure") || strings.Contains(sql, `closure" AS "closure`) {
		t.Errorf("子查询 FROM 不应被主别名物化为 closure AS closure，实际: %s", sql)
	}
}
```

- [ ] **Step 2: 跑测试确认红**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestMainAlias_subquery_from_not_polluted -v ./...`
Expected: FAIL（子查询经 `NewQueryAs(ctx, "closure")` 设了 mainAlias="closure"，BuildQuery 物化出 `closure AS closure`）

- [ ] **Step 3: SubQuery 清空 mainAlias**

`subquery.go` SubQuery（~42-45），在 `sub, x := NewQueryAs[X](ctx, tableName)` 后插入清空：

```go
	// 自动以表名注册主表 alias（sub 默认主表 alias = 表名）
	tableName := aliasSchemaTableName(reflect.TypeOf((*X)(nil)).Elem())
	sub, x := NewQueryAs[X](ctx, tableName)
	// C-1：子查询不物化 FROM（FROM 由 ToDB 的 Model(getModelInstance[X]()) 注入，
	// 主别名仅供列解析）。清空避免 BuildQuery 发射 FROM <table> AS <table> 污染子查询。
	sub.mainAlias = ""
	sub.mainAliasTable = ""
	sub.gplusCore().outerQueryRef = outer
	return sub, x
```

- [ ] **Step 4: SubQueryAs 清空 mainAlias**

`subquery.go` SubQueryAs（~63-66），在 `sub, x := NewQueryAs[X](ctx, alias)` 后插入相同清空：

```go
	// NewQueryAs 内部调用 As(q, alias)，完成主表 alias 注册和 addrLow/addrHigh 计算
	sub, x := NewQueryAs[X](ctx, alias)
	// C-1：子查询不物化 FROM（同 SubQuery）。SubQueryAs 自定义别名的 FROM 物化属既有不支持限制。
	sub.mainAlias = ""
	sub.mainAliasTable = ""
	sub.gplusCore().outerQueryRef = outer
	return sub, x
```

- [ ] **Step 5: 跑 AC-7 确认绿**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestMainAlias_subquery_from_not_polluted -v ./...`
Expected: PASS

- [ ] **Step 6: 跑子查询既有测试确认零回归**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestQuery_InSub -v ./... && D:/Environment/golang/go1.21.11/bin/go.exe test -run TestQuery_Exists -v ./...`
Expected: PASS（既有子查询/EXISTS 测试不受影响）

- [ ] **Step 7: Commit**

```bash
git add subquery.go main_alias_from_test.go
git commit -m "feat: SubQuery/SubQueryAs 清空 mainAlias 防子查询 FROM 污染（C-1, AC-7）"
```

---

## Task 4: 自连接（解锁 Round 3b）+ Count/Page 路径验证

**AC:** AC-6, AC-10

**Files:**
- Test: `main_alias_from_test.go`（仅验证，核心已在 Task 1 完成）

- [ ] **Step 1: 写 AC-6 自连接测试（FindAs + 字段指针 + 引号形态）**

`main_alias_from_test.go` 追加：

```go
// AC-6：自连接主+副别名真实执行（scenario 2 源 query 形态），FindAs 投影
func TestMainAlias_selfjoin_executes_and_projects(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	for _, c := range []Closure{{AncestorID: 1, DescendantID: 5, Depth: 0}, {AncestorID: 5, DescendantID: 7, Depth: 0}} {
		if err := repo.Save(ctx, &c); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	q, ext := repo.NewQueryAs(ctx, "ext")
	sub := As[Closure](q, "sub")
	q.CrossJoinAs(sub).
		WhereRaw("sub.ancestor_id = ?", 5).
		Eq(&ext.DescendantID, 5) // 字段指针路径：解析为 "ext".descendant_id（带引号）
	q.SelectRaw("ext.ancestor_id").
		SelectRaw("sub.descendant_id").
		SelectRaw("ext.depth + sub.depth + 1 AS depth") // 第三列须 AS depth 才能映射到 Depth

	// 投影 DTO：字段名经 GORM snake_case 映射 ancestor_id/descendant_id/depth
	type projRow struct {
		AncestorID   uint
		DescendantID uint
		Depth        uint
	}
	var rows []projRow
	if err := FindAs[Closure, projRow](repo, q, &rows); err != nil {
		t.Fatalf("FindAs 应成功，实际: %v", err)
	}
	if len(rows) != 1 || rows[0] != (projRow{AncestorID: 1, DescendantID: 7, Depth: 1}) {
		t.Fatalf("期望 1 行 {1,7,1}，实际 %+v", rows)
	}

	sql, _ := q.ToSQL(db)
	// FROM 主表带引号；JOIN 副表不带引号（appendJoinAsNoOn 生成），两处断言形态不同
	if !strings.Contains(sql, `closure" AS "ext`) {
		t.Errorf("FROM 应含带引号主别名 closure AS ext，实际: %s", sql)
	}
	if !strings.Contains(sql, "CROSS JOIN closure AS sub") {
		t.Errorf("JOIN 应含不带引号副别名 closure AS sub，实际: %s", sql)
	}
}
```

- [ ] **Step 2: 跑 AC-6 确认绿（核心已完成，本测试验证 scenario 2 解锁）**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestMainAlias_selfjoin_executes_and_projects -v ./...`
Expected: PASS

> 若 FindAs 投影字段全为零值（GORM 映射问题），检查 SelectRaw 第三列是否有 `AS depth`、projRow 字段名是否对应 snake_case 列名。

- [ ] **Step 3: 写 AC-10 Count/Page 测试（BuildCount 路径物化）**

`main_alias_from_test.go` 追加：

```go
// AC-10：Count/Page 路径（BuildCount）主别名物化，total 与 list 表名一致
func TestMainAlias_count_and_page_materialize_alias(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	for _, c := range []Closure{{AncestorID: 1, DescendantID: 5, Depth: 0}, {AncestorID: 2, DescendantID: 5, Depth: 0}} {
		if err := repo.Save(ctx, &c); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Count 路径
	q1, m1 := repo.NewQueryAs(ctx, "ext")
	q1.Eq(&m1.DescendantID, 5)
	total, err := repo.Count(q1)
	if err != nil {
		t.Fatalf("Count 应成功，实际: %v", err)
	}
	if total != 2 {
		t.Fatalf("Count 期望 2，实际 %d", total)
	}
	countSQL, _ := q1.ToCountSQL(db)
	if !strings.Contains(countSQL, `closure" AS "ext`) {
		t.Errorf("ToCountSQL FROM 应含 closure AS ext，实际: %s", countSQL)
	}

	// Page 路径（COUNT 段 + 数据段一致）
	q2, m2 := repo.NewQueryAs(ctx, "ext")
	q2.Eq(&m2.DescendantID, 5)
	list, pageTotal, err := repo.Page(q2, false)
	if err != nil {
		t.Fatalf("Page 应成功，实际: %v", err)
	}
	if pageTotal != 2 || len(list) != 2 {
		t.Fatalf("Page 期望 total=2 len=2，实际 total=%d len=%d", pageTotal, len(list))
	}
}
```

- [ ] **Step 4: 跑 AC-10 确认绿**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestMainAlias_count_and_page_materialize_alias -v ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add main_alias_from_test.go
git commit -m "test: 自连接解锁 Round 3b + Count/Page 主别名物化（AC-6/10）"
```

---

## Task 5: 守卫与已知限制 — 写路径 / DataRule ambiguous / First 路径 / validTableName

**AC:** AC-8, AC-9, AC-11

**Files:**
- Test: `main_alias_from_test.go`（守卫由结构/正则保证，本任务以测试锁死行为）

- [ ] **Step 1: 写 AC-8（写路径不物化，守卫型）测试**

`main_alias_from_test.go` 追加：

```go
// AC-8：主别名 Query 传 DeleteByCondTx 走 BuildDelete，不物化别名（结构性禁止）
func TestMainAlias_delete_path_no_materialize(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	_ = db
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	q, m := repo.NewQueryAs(ctx, "ext")
	q.Eq(&m.DescendantID, 5)

	affected, err := repo.DeleteByCondTx(q, nil)
	if err != nil {
		t.Fatalf("DeleteByCondTx 应成功（FROM 不含 AS ext），实际: %v", err)
	}
	if affected != 1 {
		t.Fatalf("期望删除 1 行，实际 %d", affected)
	}
	assertClosureCount(t, db, 0)
}
```

- [ ] **Step 2: 跑 AC-8 确认绿（BuildDelete 不调 applyMainAlias，结构保证）**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestMainAlias_delete_path_no_materialize -v ./...`
Expected: PASS（若失败说明 BuildDelete 误调了 applyMainAlias）

- [ ] **Step 3: 写 AC-9（DataRule 自连接 ambiguous 已知限制）测试**

`main_alias_from_test.go` 追加：

```go
// AC-9：主别名 + DataRule 注入裸列 + 自连接 → ambiguous（已知限制，用户须在 DataRule.Column 自带 ext. 前缀）
func TestMainAlias_datarule_selfjoin_ambiguous(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	_ = db
	rules := []DataRule{{Column: "depth", Condition: ">=", Value: "0"}}
	ctx := context.WithValue(context.Background(), DataRuleKey, rules)
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	q, ext := repo.NewQueryAs(ctx, "ext")
	sub := As[Closure](q, "sub")
	q.CrossJoinAs(sub).WhereRaw("sub.ancestor_id = ?", 1).Eq(&ext.DescendantID, 5)
	q.SelectRaw("ext.ancestor_id")

	_, err := repo.List(q)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("期望 ambiguous 错误（DataRule 裸列 depth 在自连接下歧义），实际 err: %v", err)
	}
}
```

- [ ] **Step 4: 跑 AC-9 确认绿**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestMainAlias_datarule_selfjoin_ambiguous -v ./...`
Expected: PASS（err 含 `ambiguous column name: depth`）

- [ ] **Step 5: 写 AC-11（First 路径不支持，已知限制）测试**

`main_alias_from_test.go` 追加：

```go
// AC-11：主别名查询走 First 路径（GetOne）因 GORM 自动 ORDER BY closure.id 裸表名被别名遮蔽而失败（已知限制）
func TestMainAlias_first_path_not_supported(t *testing.T) {
	repo, _ := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	q, m := repo.NewQueryAs(ctx, "ext")
	q.Eq(&m.DescendantID, 5)

	_, err := repo.GetOne(q)
	if err == nil || !strings.Contains(err.Error(), "no such column") || !strings.Contains(err.Error(), "closure.id") {
		t.Fatalf("期望 First 路径报 no such column: closure.id（已知限制），实际 err: %v", err)
	}
}
```

- [ ] **Step 6: 跑 AC-11 确认绿**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestMainAlias_first_path_not_supported -v ./...`
Expected: PASS（err 含 `no such column: closure.id`）

- [ ] **Step 7: 写 validTableName 表驱动单测（M-1 注入守卫）**

`main_alias_from_test.go` 追加：

```go
// validTableName 守卫：接受合法表名，拒绝注入 payload（M-1）
func TestMainAlias_validTableName_guard(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"裸表名", "closure", true},
		{"带数字下划线", "closure_2024", true},
		{"单点 schema.table", "main.closure", true},
		{"引号闭合注入", `x"; DROP TABLE closure; --`, false},
		{"AS 注入", "closure AS evil", false},
		{"空格", "clo sure", false},
		{"空串", "", false},
		{"数字开头", "2closure", false},
		{"多段 a.b.c 不支持", "a.b.c", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validTableName.MatchString(tc.input); got != tc.want {
				t.Errorf("validTableName(%q) = %v，期望 %v", tc.input, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 8: 跑 validTableName 单测确认绿**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestMainAlias_validTableName_guard -v ./...`
Expected: PASS（全部 9 个 case）

- [ ] **Step 9: Commit**

```bash
git add main_alias_from_test.go
git commit -m "test: 写路径/DataRule ambiguous/First 限制/validTableName 守卫（AC-8/9/11）"
```

---

## Task 6: 全量回归 + 覆盖率门禁

**Files:** 无（验证任务）

- [ ] **Step 1: 跑全部新 AC 测试**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestMainAlias -v ./...`
Expected: PASS（11 个测试函数全绿）

- [ ] **Step 2: go vet 静态检查**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe vet ./...`
Expected: 无输出（无警告）

- [ ] **Step 3: 全量回归**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test ./...`
Expected: ok，全部 PASS（尤其 `alias_*_test.go`、`query_newqueryas_test.go`、`query_subquery*_test.go` 不回归）

- [ ] **Step 4: 覆盖率门禁 ≥ 80%（基线 95.0% 不得回退）**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -coverprofile=coverage.out ./... && D:/Environment/golang/go1.21.11/bin/go.exe tool cover -func=coverage.out | tail -1`
Expected: total ≥ 95.0%

- [ ] **Step 5: 清理 coverage.out（若产生）**

```bash
git status --short   # 确认无 coverage.out 等临时文件混入
rm -f coverage.out
```

- [ ] **Step 6: 最终确认无未提交变更**

Run: `git status --short`
Expected: 干净（所有变更已在 Task 1-5 提交）

---

## Self-Review 检查（写计划后自查，已完成）

- **Spec coverage**：AC-1→Task1、AC-2→Task1、AC-3→Task1、AC-4→Task2、AC-5→Task2、AC-6→Task4、AC-7→Task3、AC-8→Task5、AC-9→Task5、AC-10→Task4、AC-11→Task5、validTableName→Task5。全部 11 AC + 守卫有任务覆盖。
- **类型一致性**：`mainAlias string`/`mainAliasTable string`、`applyMainAlias(db, qL, qR)`、`validTableName`、`NewQueryAs`、`FindAs[Closure, projRow]`、`DeleteByCondTx(q, nil)`、`repo.Count`/`repo.Page`/`repo.GetOne` 全程一致，签名与仓库现状核对无误。
- **无占位符**：每步含完整测试/实现代码 + 精确命令 + 期望输出。
- **探针前置**：带引号 FROM、Count、自连接、First 失败、DataRule ambiguous 均已 plan 探针实测，断言文本据实定死。
- **依赖顺序**：Task1 核心 → Task2 Clear/override → Task3 子查询清空（依赖 Task1 的 mainAlias 字段）→ Task4 自连接/Count（依赖 Task1 物化）→ Task5 守卫 → Task6 回归。
