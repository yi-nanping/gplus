# SelectRaw 参数绑定 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `SelectRaw(expr string, args ...any)` 支持参数绑定，使投影表达式中的 `?` 走 `Statement.Vars` 绑定，且所有不带 args 的现有查询 SQL 字节级不变。

**Architecture:** 把 `ScopeBuilder.selects` 从 `[]string` 升级为 `[]selectItem{expr, args, isRaw}`，保留 Select/SelectRaw 调用顺序。`applySelects` 双路径：无 args 时走原 `db.Select([]string)` 路径（零回归），有 args 时拼成单个 `db.Select(combinedExpr, flatArgs...)` 走 GORM 的 `clause.Expr{Vars}` 绑定路径。

**Tech Stack:** Go 1.24 · GORM v1.31.1 · glebarez/sqlite（测试）

> **Spec：** `docs/superpowers/specs/2026-06-04-selectraw-args-design.md`（rev.2，已并入 5 视角审计）。
> **基线（当前代码实测，DryRun，TestUser）：**
> - 无参 SelectRaw：`` SELECT COUNT(*) AS "cnt" FROM `test_users` ``（别名被 quoteColumn 加引号）
> - Select 指针：`` SELECT "username","age" FROM `test_users` ``（逗号无空格）
> - Distinct 无参：`` SELECT DISTINCT "age" FROM `test_users` ``
> **DISTINCT 实测：** args 路径 select 串前置 `DISTINCT ` 后，无论是否再调 `db.Distinct()`，均为单个 DISTINCT（GORM Expression 路径忽略 `Statement.Distinct`）。

---

## File Structure

| 文件 | 职责 | 改动 |
|---|---|---|
| `builder.go` | ScopeBuilder 定义 + applySelects + Clear | 新增 `selectItem` 类型；`selects` 改类型；applySelects 双路径；Clear 改 nil |
| `query.go` | Query 的 Select / SelectRaw / Distinct | 三处写入改 append `selectItem`；SelectRaw 加 `args ...any` |
| `update.go` | Updater.Select | 一处写入改 append `selectItem` |
| `missing_coverage_test.go` | 现有测试 | `u.selects[0]` → `u.selects[0].expr`（迁移后编译破坏） |
| `select_raw_args_test.go` | 本特性测试 | 新建，AC-1..AC-14 |

迁移触点经 grep 全仓核实仅 6 个生产点（写 4：query.go Select/SelectRaw/Distinct、update.go Select；读 3：builder.go applySelects/Clear/BuildCount，其中 BuildCount 只用 `len` 无需改）+ 1 个测试点。

---

## Task 1: selectItem 数据结构迁移（纯重构，零行为变更）

把 `selects []string` 升级为 `[]selectItem`，所有现有行为保持字节级不变。SelectRaw 本步**不加 args 参数**（留待 Task 2）。

**Files:**
- Modify: `builder.go`（新增类型 ~line 31 后；field line 77；applySelects 272-283；Clear 214）
- Modify: `query.go`（Select 240；SelectRaw 254；Distinct 725）
- Modify: `update.go`（Select 337）
- Modify: `missing_coverage_test.go:220-221`
- Test: `select_raw_args_test.go`（新建，本步加 AC-6/7/10 回归门禁）

**AC:** AC-6, AC-7, AC-10, AC-11（回归保持）

- [ ] **Step 1: 先写回归门禁测试（对当前代码即应通过）**

新建 `select_raw_args_test.go`：

```go
package gplus

import (
	"context"
	"testing"
)

// AC-6：无参 SelectRaw 字节级不变
func TestSelectRaw_noargs_sql_unchanged(t *testing.T) {
	db := newDryRunDB(t)
	q, _ := NewQuery[TestUser](context.Background())
	q.SelectRaw("COUNT(*) AS cnt")
	sql, _ := buildSQL(t, db, q)
	want := "SELECT COUNT(*) AS \"cnt\" FROM `test_users`"
	if sql != want {
		t.Errorf("SQL 回归:\n want=%s\n got =%s", want, sql)
	}
}

// AC-7：Select 字段指针字节级不变（逗号无空格）
func TestSelect_pointers_sql_unchanged(t *testing.T) {
	db := newDryRunDB(t)
	q, u := NewQuery[TestUser](context.Background())
	q.Select(&u.Name, &u.Age)
	sql, _ := buildSQL(t, db, q)
	want := "SELECT \"username\",\"age\" FROM `test_users`"
	if sql != want {
		t.Errorf("SQL 回归:\n want=%s\n got =%s", want, sql)
	}
}

// AC-10：Distinct 无 args 字节级不变
func TestDistinct_noargs_sql_unchanged(t *testing.T) {
	db := newDryRunDB(t)
	q, u := NewQuery[TestUser](context.Background())
	q.Distinct(&u.Age)
	sql, _ := buildSQL(t, db, q)
	want := "SELECT DISTINCT \"age\" FROM `test_users`"
	if sql != want {
		t.Errorf("SQL 回归:\n want=%s\n got =%s", want, sql)
	}
}
```

- [ ] **Step 2: 运行确认当前代码通过（建立基线）**

Run: `go test -run "TestSelectRaw_noargs_sql_unchanged|TestSelect_pointers_sql_unchanged|TestDistinct_noargs_sql_unchanged" ./...`
Expected: PASS（这三条锁定迁移前行为，迁移后必须仍 PASS）

- [ ] **Step 3: builder.go 新增 selectItem 类型**

在 `orderItem` 定义之后（builder.go:31 后）插入：

```go
// selectItem 存储单个 SELECT 投影项。
// isRaw=false：expr 为列名，需经 quoteColumn 转义；
// isRaw=true ：expr 为原生表达式，原样输出不转义，args 为其绑定参数（按出现顺序）。
type selectItem struct {
	expr  string
	args  []any
	isRaw bool
}
```

- [ ] **Step 4: builder.go 改 selects 字段类型（line 77）**

```go
	// selects 用于构建 Select 字段
	selects []selectItem
```

- [ ] **Step 5: builder.go applySelects 适配（line 272-283，本步仅无 args 路径）**

```go
// applySelects select
func (b *ScopeBuilder) applySelects(db *gorm.DB, qL, qR string) *gorm.DB {
	if len(b.selects) > 0 {
		cols := make([]string, len(b.selects))
		for i, it := range b.selects {
			cols[i] = it.expr
		}
		db = db.Select(quoteColumns(cols, qL, qR))
	}
	if len(b.omits) > 0 {
		db = db.Omit(quoteColumns(b.omits, qL, qR)...)
	}
	return db
}
```

> 重建 `[]string` 后调 `quoteColumns`，与迁移前 `db.Select(quoteColumns(b.selects,...))` 输出逐字节相同（含 raw 项也走 quoteColumn 的跳过启发式，行为不变）。

- [ ] **Step 6: builder.go Clear 改 nil（line 214）**

把 `b.selects = b.selects[:0]` 从「纯 string 切片 [:0]」块移到「含嵌套引用切片置 nil」块。结果：

```go
	// 含嵌套引用的切片（condition.group、joinInfo.args、preloadInfo.args、selectItem.args）
	// 置 nil 以释放内部引用，避免 backing array 持续持有内存
	b.conditions = nil
	b.havings = nil
	b.joins = nil
	b.preloads = nil
	b.selects = nil
	// 纯 string 切片无嵌套引用，[:0] 保留容量可安全复用
	b.omits = b.omits[:0]
	b.orders = b.orders[:0]
	b.groups = b.groups[:0]
```

- [ ] **Step 7: query.go 三处写入改 selectItem**

`Select`（line 240）：

```go
		q.selects = append(q.selects, selectItem{expr: name})
```

`SelectRaw`（line 254，**本步保持单参签名**）：

```go
		q.selects = append(q.selects, selectItem{expr: expr, isRaw: true})
```

`Distinct`（line 725）：

```go
		q.selects = append(q.selects, selectItem{expr: name})
```

- [ ] **Step 8: update.go Updater.Select 改 selectItem（line 337）**

```go
		u.selects = append(u.selects, selectItem{expr: name})
```

- [ ] **Step 9: 修 missing_coverage_test.go 编译破坏（line 220-221）**

```go
		if !strings.Contains(u.selects[0].expr, "username") {
			t.Errorf("selects[0] 期望包含 username，实际 %q", u.selects[0].expr)
		}
```

- [ ] **Step 10: 编译 + 全量回归**

Run: `go build ./... && go test ./...`
Expected: 全部 PASS（含 Step 1 三条回归门禁 + 现有 94% 覆盖测试全绿）。若任何现有测试红，说明迁移破坏了行为，必须修到字节级一致。

- [ ] **Step 11: Commit**

```bash
git add builder.go query.go update.go missing_coverage_test.go select_raw_args_test.go
git commit -m "refactor: selects []string 升级为 []selectItem（零行为变更）

为 SelectRaw 参数绑定铺路。selectItem{expr,args,isRaw} 保留调用顺序，
applySelects 暂仅无 args 路径，输出与迁移前字节级一致。Clear 改 nil
对齐含嵌套引用切片约定。修 missing_coverage_test 按 .expr 取值。"
```

---

## Task 2: SelectRaw(args) + applySelects 绑定路径

给 `SelectRaw` 加 `args ...any`，applySelects 增加绑定路径（仅当存在 args 时启用，无 args 路径不动）。

**Files:**
- Modify: `query.go` SelectRaw（line 249-256，加 args 参数）
- Modify: `builder.go` applySelects（双路径）
- Test: `select_raw_args_test.go`（追加 AC-1/2/3/4/5/8/13）

**AC:** AC-1, AC-2, AC-3, AC-4, AC-5, AC-8, AC-13

- [ ] **Step 1: 写失败测试（绑定值 + Vars + 顺序 + 混用 + 裸? + 空expr + 多占位符）**

追加到 `select_raw_args_test.go`（顶部 import 增加 `"sort"`）：

```go
// AC-1：单参绑定，值正确（age=18,19,20 → age+1=19,20,21）
func TestSelectRaw_binds_single_arg_value(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	for _, age := range []int{18, 19, 20} {
		if err := repo.Save(ctx, &TestUser{Age: age}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	q, _ := repo.NewQuery(ctx)
	q.SelectRaw("age + ?", 1)
	var got []int
	if err := db.Model(&TestUser{}).Scopes(q.DataRuleBuilder().BuildQuery()).Scan(&got).Error; err != nil {
		t.Fatalf("scan: %v", err)
	}
	sort.Ints(got)
	want := []int{19, 20, 21}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("期望 %v，实际 %v", want, got)
	}
}

// AC-2：单参走 Vars 而非字面量
func TestSelectRaw_single_arg_into_vars(t *testing.T) {
	db := newDryRunDB(t)
	q, _ := NewQuery[TestUser](context.Background())
	q.SelectRaw("age + ?", 1)
	sql, vars := buildSQL(t, db, q)
	assertSQL(t, sql, "age + ?")
	if len(vars) != 1 || vars[0] != 1 {
		t.Errorf("Vars 期望 [1]，实际 %v", vars)
	}
}

// AC-3：多 SelectRaw args 顺序 = 调用顺序
func TestSelectRaw_multi_args_order(t *testing.T) {
	db := newDryRunDB(t)
	q, _ := NewQuery[TestUser](context.Background())
	q.SelectRaw("?", 7).SelectRaw("age + ?", 100)
	_, vars := buildSQL(t, db, q)
	if len(vars) != 2 || vars[0] != 7 || vars[1] != 100 {
		t.Errorf("Vars 期望 [7 100]，实际 %v", vars)
	}
}

// AC-4：混用 Select 指针 + SelectRaw(args)，交错顺序 + SELECT 子句字面（单串路径，逗号带空格）
func TestSelectRaw_mixed_with_select_pointers(t *testing.T) {
	db := newDryRunDB(t)
	q, u := NewQuery[TestUser](context.Background())
	q.Select(&u.ID).SelectRaw("?", 9).Select(&u.Age)
	sql, vars := buildSQL(t, db, q)
	want := "SELECT \"id\", ?, \"age\" FROM `test_users`"
	if sql != want {
		t.Errorf("SQL:\n want=%s\n got =%s", want, sql)
	}
	if len(vars) != 1 || vars[0] != 9 {
		t.Errorf("Vars 期望 [9]，实际 %v", vars)
	}
}

// AC-5：裸 ? 不被 quoteColumn 误转义成 "?"
func TestSelectRaw_bare_placeholder_not_quoted(t *testing.T) {
	db := newDryRunDB(t)
	q, _ := NewQuery[TestUser](context.Background())
	q.SelectRaw("?", 5)
	sql, _ := buildSQL(t, db, q)
	if strings.Contains(sql, "\"?\"") {
		t.Errorf("裸 ? 被错误转义为 \"?\":\n %s", sql)
	}
	assertSQL(t, sql, "SELECT ?")
}

// AC-13：单 expr 多占位符展开顺序（age + 5 - 2 = age+3）
func TestSelectRaw_single_expr_multi_placeholder(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	for _, age := range []int{18, 19, 20} {
		if err := repo.Save(ctx, &TestUser{Age: age}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	q, _ := repo.NewQuery(ctx)
	q.SelectRaw("age + ? - ?", 5, 2)
	var got []int
	if err := db.Model(&TestUser{}).Scopes(q.DataRuleBuilder().BuildQuery()).Scan(&got).Error; err != nil {
		t.Fatalf("scan: %v", err)
	}
	sort.Ints(got)
	want := []int{21, 22, 23}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("期望 %v（age+3），实际 %v", want, got)
	}
}

// AC-8：空 expr 报错且不污染 selects
func TestSelectRaw_empty_expr_errors(t *testing.T) {
	db := newDryRunDB(t)
	q, _ := NewQuery[TestUser](context.Background())
	q.SelectRaw("", 1)
	if q.GetError() == nil {
		t.Fatal("空 expr 应累积错误")
	}
	_, vars := buildSQL(t, db, q)
	if len(vars) != 0 {
		t.Errorf("空 expr 不应留下 Vars，实际 %v", vars)
	}
}

// AC-9：Clear() 重置 args，后续 BuildQuery 无残留投影/Vars
func TestSelectRaw_clear_resets_args(t *testing.T) {
	db := newDryRunDB(t)
	q, _ := NewQuery[TestUser](context.Background())
	q.SelectRaw("age + ?", 1)
	q.Clear()
	sql, vars := buildSQL(t, db, q)
	if strings.Contains(sql, "age + ?") {
		t.Errorf("Clear 后不应残留投影:\n %s", sql)
	}
	if len(vars) != 0 {
		t.Errorf("Clear 后 Vars 应为空，实际 %v", vars)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test -run "TestSelectRaw_binds_single_arg_value|TestSelectRaw_single_arg_into_vars|TestSelectRaw_multi_args_order|TestSelectRaw_mixed_with_select_pointers|TestSelectRaw_bare_placeholder_not_quoted|TestSelectRaw_single_expr_multi_placeholder|TestSelectRaw_empty_expr_errors" ./...`
Expected: 编译失败（`SelectRaw` 还不接受第二个参数）→ 这本身就是 RED。

- [ ] **Step 3: query.go SelectRaw 加 args 参数（line 249-256）**

```go
func (q *Query[T]) SelectRaw(expr string, args ...any) *Query[T] {
	if expr == "" {
		q.errs = append(q.errs, errors.New("gplus: SelectRaw expr cannot be empty"))
		return q
	}
	q.selects = append(q.selects, selectItem{expr: expr, args: args, isRaw: true})
	return q
}
```

- [ ] **Step 4: builder.go applySelects 改双路径**

```go
// applySelects select
func (b *ScopeBuilder) applySelects(db *gorm.DB, qL, qR string) *gorm.DB {
	if len(b.selects) > 0 {
		hasArgs := false
		for _, it := range b.selects {
			if len(it.args) > 0 {
				hasArgs = true
				break
			}
		}
		if !hasArgs {
			// 零回归路径：与迁移前完全一致（逗号无空格）
			cols := make([]string, len(b.selects))
			for i, it := range b.selects {
				cols[i] = it.expr
			}
			db = db.Select(quoteColumns(cols, qL, qR))
		} else {
			// 绑定路径：单串 + 顺序展平 args（逗号带空格）
			parts := make([]string, len(b.selects))
			var flatArgs []any
			for i, it := range b.selects {
				if it.isRaw {
					parts[i] = it.expr
					flatArgs = append(flatArgs, it.args...)
				} else {
					parts[i] = quoteColumn(it.expr, qL, qR)
				}
			}
			db = db.Select(strings.Join(parts, ", "), flatArgs...)
		}
	}
	if len(b.omits) > 0 {
		db = db.Omit(quoteColumns(b.omits, qL, qR)...)
	}
	return db
}
```

- [ ] **Step 5: 运行新测试 + 全量回归**

Run: `go test ./...`
Expected: 全部 PASS（Task 2 七条 + Task 1 三条回归 + 现有全量）。AC-6/AC-7（无 args）仍走零回归路径不受影响。

- [ ] **Step 6: Commit**

```bash
git add query.go builder.go select_raw_args_test.go
git commit -m "feat: SelectRaw 支持参数绑定 SelectRaw(expr, args...)

applySelects 双路径：无 args 走原 []string 路径（零回归），有 args 拼单串
db.Select(combined, flatArgs...) 走 GORM clause.Expr{Vars} 绑定。raw 项不
转义、普通列经 quoteColumn，调用顺序与 Vars 顺序严格保留。"
```

---

## Task 3: 修复 Distinct + SelectRaw(args) 静默丢 DISTINCT

绑定路径下 `db.Select(string,args)` 走 `clause.Expr` 会忽略 `Statement.Distinct`，导致 `Distinct().SelectRaw(...,arg)` 丢失 DISTINCT。修法：args 路径在 `b.distinct` 时把 `DISTINCT ` 前缀并入 select 串（实测：之后 `applyDistinct` 的 `db.Distinct()` 不会产生重复 DISTINCT，无需改 applyDistinct/BuildCount 调用序）。

**Files:**
- Modify: `builder.go` applySelects（绑定路径加 DISTINCT 前缀）
- Test: `select_raw_args_test.go`（追加 AC-12）

**AC:** AC-12

- [ ] **Step 1: 写失败测试**

追加到 `select_raw_args_test.go`：

```go
// AC-12：Distinct + SelectRaw(args) 必须保留 DISTINCT 且 Vars 正确
func TestSelectRaw_with_distinct_keeps_distinct(t *testing.T) {
	db := newDryRunDB(t)
	q, u := NewQuery[TestUser](context.Background())
	q.Distinct(&u.Age).SelectRaw("age + ?", 1)
	sql, vars := buildSQL(t, db, q)
	assertSQL(t, sql, "DISTINCT", "age + ?")
	if len(vars) != 1 || vars[0] != 1 {
		t.Errorf("Vars 期望 [1]，实际 %v", vars)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test -run TestSelectRaw_with_distinct_keeps_distinct ./...`
Expected: FAIL — 生成 SQL 为 `SELECT age + ? FROM ...`，缺少 `DISTINCT`（assertSQL 报缺片段 `"DISTINCT"`）。

- [ ] **Step 3: builder.go applySelects 绑定路径加 DISTINCT 前缀**

把 Task 2 中绑定路径的 `db.Select(strings.Join(parts, ", "), flatArgs...)` 改为：

```go
			expr := strings.Join(parts, ", ")
			if b.distinct {
				expr = "DISTINCT " + expr
			}
			db = db.Select(expr, flatArgs...)
```

绑定路径分支完整形态：

```go
		} else {
			// 绑定路径：单串 + 顺序展平 args（逗号带空格）
			parts := make([]string, len(b.selects))
			var flatArgs []any
			for i, it := range b.selects {
				if it.isRaw {
					parts[i] = it.expr
					flatArgs = append(flatArgs, it.args...)
				} else {
					parts[i] = quoteColumn(it.expr, qL, qR)
				}
			}
			expr := strings.Join(parts, ", ")
			if b.distinct {
				expr = "DISTINCT " + expr
			}
			db = db.Select(expr, flatArgs...)
		}
```

- [ ] **Step 4: 运行新测试 + 全量回归**

Run: `go test ./...`
Expected: 全部 PASS。重点确认 AC-10（`TestDistinct_noargs_sql_unchanged`，无 args 走零回归路径）仍 PASS——证明 DISTINCT 前缀只影响 args 路径。

- [ ] **Step 5: Commit**

```bash
git add builder.go select_raw_args_test.go
git commit -m "fix: Distinct 与 SelectRaw(args) 混用时保留 DISTINCT

args 路径走 clause.Expr 会忽略 Statement.Distinct 导致 DISTINCT 静默丢失。
在 b.distinct 时把 DISTINCT 前缀并入 select 串修复；无 args 路径不受影响。"
```

---

## Task 4: godoc + 验证收口

补 SelectRaw godoc 的 args 绑定语义，跑全项目验证。

**Files:**
- Modify: `query.go` SelectRaw godoc（line 245-248）

**AC:** AC-14

- [ ] **Step 1: 更新 SelectRaw godoc（line 245-248）**

```go
// SelectRaw 添加原生 SELECT 字段表达式，支持参数绑定。
// expr 为原生 SQL 表达式片段，不经列名转义直接传入 GORM；expr 由调用方负责安全性，
// 严禁拼接用户输入。args 为 expr 中 ? 占位符的绑定值（参数化绑定，防 SQL 注入），
// 用户输入一律走 args。expr 中 ? 仅作绑定占位符，勿写字面量 ?。
// 示例：q.SelectRaw("AVG(age)").SelectRaw("age + ?", 1)
```

- [ ] **Step 2: 全项目验证（build / vet / test / 覆盖率）**

Run:
```bash
go build ./...
go vet ./...
go test -race ./...
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1
```
Expected：build/vet 无输出；test 全 PASS；总覆盖率 ≥ 94.0%（不得低于基线）。若覆盖率回退，补测未覆盖的 applySelects 分支。

- [ ] **Step 3: gofmt 确认**

Run: `gofmt -l builder.go query.go update.go select_raw_args_test.go`
Expected: 无输出（已格式化）。有输出则 `gofmt -w` 对应文件。

- [ ] **Step 4: Commit**

```bash
git add query.go
git commit -m "docs: SelectRaw godoc 补充 args 参数绑定语义"
```

---

## 完成标准（DoD）

- [ ] AC-1..AC-14 全部有对应测试且 PASS
- [ ] `go build ./...` / `go vet ./...` / `go test -race ./...` 全绿
- [ ] 覆盖率 ≥ 94.0%（不回退）
- [ ] 无 args 的现有查询 SQL 字节级不变（AC-6/7/10 + 现有全量测试背书）
- [ ] `gofmt -l` 无输出

## AC ↔ 测试 ↔ Task 映射

| AC | 测试函数 | Task |
|---|---|---|
| AC-1 | `TestSelectRaw_binds_single_arg_value` | 2 |
| AC-2 | `TestSelectRaw_single_arg_into_vars` | 2 |
| AC-3 | `TestSelectRaw_multi_args_order` | 2 |
| AC-4 | `TestSelectRaw_mixed_with_select_pointers` | 2 |
| AC-5 | `TestSelectRaw_bare_placeholder_not_quoted` | 2 |
| AC-6 | `TestSelectRaw_noargs_sql_unchanged` | 1 |
| AC-7 | `TestSelect_pointers_sql_unchanged` | 1 |
| AC-8 | `TestSelectRaw_empty_expr_errors` | 2 |
| AC-9 | `TestSelectRaw_clear_resets_args` | 2 |
| AC-10 | `TestDistinct_noargs_sql_unchanged` | 1 |
| AC-11 | 现有 `missing_coverage_test.go` + `updater` 测试全绿 | 1 |
| AC-12 | `TestSelectRaw_with_distinct_keeps_distinct` | 3 |
| AC-13 | `TestSelectRaw_single_expr_multi_placeholder` | 2 |
| AC-14 | SelectRaw godoc 含 args 绑定语义（人工/review 核对） | 4 |

> AC-14 无法写成自动化断言（godoc 文本），由 code-review 人工核对；其余 AC-1..AC-13 均 1:1 对应测试函数。
