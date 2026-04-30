# v0.6.0 类型安全子查询 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 v0.6.0 minor 版本新增 32 个类型安全子查询方法（Query 16 + Updater 16）+ Subquerier 接口，消灭体系性 WhereRaw 子查询裂缝。

**Architecture:** 引入 `Subquerier` 接口（含 `gplusSubquery()` unexported guard），用户层方法把 `Subquerier` 作为 `cond.value` 存入 condition；`builder.go:applyWhere` 新增 Subquerier 识别分支，外层 db 可用时延迟调 `sub.ToDB(d)` 转 `*gorm.DB` 后走现有子查询路径。延迟调用而非立即快照（受限于 `Query[T]` 不持有 db）。

**Tech Stack:** Go 1.24 / GORM v2 / SQLite（测试默认）/ MySQL（TEST_DB=mysql）。Go 二进制路径：`D:/Environment/golang/go1.21.11/bin/go.exe`（自动下载 go1.24 toolchain）。

**Spec 位置：** `docs/superpowers/specs/2026-04-26-typesafe-subquery-design.md`

---

## File Structure

| 文件 | 状态 | 责任 |
|---|---|---|
| `subquery.go` | 新建 | `Subquerier` 接口定义 + 编译期断言 |
| `repository.go` | 修改（+3 行） | 新增 `ErrSubqueryNil` sentinel |
| `builder.go` | 修改（+12 行） | `applyWhere` 新增 Subquerier 识别分支 |
| `query.go` | 修改（+70 行） | 16 个子查询方法 + `gplusSubquery()` 实现 |
| `update.go` | 修改（+70 行） | 16 个子查询方法 + `gplusSubquery()` 实现 |
| `query_subquery_test.go` | 新建（~420 行） | Query 全 16 方法 + 错误路径 + DataRule 交互 + Session 隔离 |
| `updater_subquery_test.go` | 新建（~250 行） | Updater 全 16 方法 DryRun + UpdateByCondTx 集成 |
| `CHANGELOG.md` | 修改 | v0.6.0 章节 |
| `README.md` | 修改 | 子查询使用示例 |

---

## Task 1: 基础设施（Subquerier 接口 + Sentinel + applyWhere 分支）

**Files:**
- Create: `D:\projects\gplus\subquery.go`
- Modify: `D:\projects\gplus\repository.go:15-26`（sentinel 块）
- Modify: `D:\projects\gplus\builder.go:319-340`（applyWhere 子查询分支）
- Modify: `D:\projects\gplus\query.go`（追加 `gplusSubquery()` 方法）
- Modify: `D:\projects\gplus\update.go`（追加 `gplusSubquery()` 方法）
- Test: `D:\projects\gplus\subquery_test.go`（新建）

- [ ] **Step 1: 写编译期断言验证测试**

新建 `D:\projects\gplus\subquery_test.go`：

```go
package gplus

import (
	"context"
	"testing"
)

// TestSubquerier_Interface_Implementation 验证 *Query[T] 满足 Subquerier 接口。
// 编译通过即表示接口契约成立；运行 TestSubquerier_Nil_Returns_ErrSubqueryNil 验证 sentinel。
func TestSubquerier_Interface_Implementation(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	var sub Subquerier = q
	if sub.GetError() != nil {
		t.Fatalf("expected nil error from fresh sub, got %v", sub.GetError())
	}
}
```

- [ ] **Step 2: 跑测试确认编译失败**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestSubquerier_Interface_Implementation ./...
```

预期：编译错误 `undefined: Subquerier`。

- [ ] **Step 3: 创建 subquery.go**

```go
package gplus

import "gorm.io/gorm"

// Subquerier 子查询契约。任意 *Query[X] 自动满足（X 可与外层 T 不同）。
// gplusSubquery() 私有方法限制接口只能由本包实现。
type Subquerier interface {
	// ToDB 返回可作为 GORM 子查询绑定参数的 *gorm.DB 对象
	ToDB(db *gorm.DB) *gorm.DB

	// GetError 返回构建过程累积的错误
	GetError() error

	gplusSubquery() // unexported guard: 阻止外部包冒名实现
}

// 编译期断言：*Query[T] 满足 Subquerier。
// 选 struct{} 作为占位 T 仅为验证方法集，gplusSubquery 不依赖 T，对任意 T 均成立。
var _ Subquerier = (*Query[struct{}])(nil)
```

- [ ] **Step 4: 在 query.go 末尾追加 gplusSubquery() 实现**

`D:\projects\gplus\query.go` 文件末尾追加：

```go
// gplusSubquery 私有 guard 方法，阻止外部包冒名实现 Subquerier 接口。
func (q *Query[T]) gplusSubquery() {}
```

注：本期 `*Updater[T]` 不实现 Subquerier 接口（用 update 作为子查询非典型用例，保留 v0.7.0 评估），故 update.go 不加 `gplusSubquery()` 方法。Subquerier 唯一内置实现为 `*Query[T]`。

- [ ] **Step 5: 在 repository.go 添加 ErrSubqueryNil sentinel**

修改 `D:\projects\gplus\repository.go:15-26` 的 var 块，在最后追加一行：

```go
var (
	ErrQueryNil          = errors.New("gplus: query cannot be nil")
	ErrRawSQLEmpty       = errors.New("gplus: raw sql cannot be empty")
	ErrDeleteEmpty       = errors.New("gplus: delete content is empty")
	ErrUpdateEmpty       = errors.New("gplus: update content is empty")
	ErrUpdateNoCondition = errors.New("gplus: update requires at least one condition to prevent full-table update")
	ErrTransactionReq    = errors.New("gplus: locking query must be executed within a transaction")
	ErrDefaultsNil       = errors.New("gplus: defaults cannot be nil, use &T{} to create a zero-value record explicitly")
	ErrRestoreEmpty      = errors.New("gplus: restore condition is empty")
	ErrOnConflictInvalid = errors.New("gplus: OnConflict config invalid: DoNothing is mutually exclusive with DoUpdates/DoUpdateAll/UpdateExprs; DoUpdateAll is mutually exclusive with DoUpdates/UpdateExprs")
	ErrOptimisticLock    = errors.New("gplus: optimistic lock conflict (version mismatch or row not found)")
	ErrSubqueryNil       = errors.New("gplus: subquery is nil")
)
```

- [ ] **Step 6: 修改 builder.go applyWhere 添加 Subquerier 识别分支**

修改 `D:\projects\gplus\builder.go:319-340`，在现有 `*gorm.DB` 子查询分支**之前**插入新分支：

```go
			clauseStr := cond.expr
			if clauseStr == "" {
				continue
			}

			// --- Subquerier 子查询（延迟调用） ---
			// 用户层 InSub/EqSub 等方法把 Subquerier 接口存进 cond.value；
			// 外层 db 可用时调 sub.ToDB(d) 转为 *gorm.DB，sub.GetError() 由 ToDB
			// 内部 session.AddError 经 GORM 错误链传播。
			if sub, ok := cond.value.(Subquerier); ok {
				subDB := sub.ToDB(d)
				quotedCol := quoteColumn(cond.expr, qL, qR)
				sqlStr := fmt.Sprintf("%s %s (?)", quotedCol, cond.operator)
				if cond.isOr {
					d = d.Or(sqlStr, subDB)
				} else {
					d = d.Where(sqlStr, subDB)
				}
				continue
			}

			// ---子查询核心逻辑 ---
			// 检查 cond.value 是否为 *gorm.DB 类型 (即子查询对象)
			if subQuery, ok := cond.value.(*gorm.DB); ok {
```

注：保留现有 `*gorm.DB` 分支，新分支放在它之前。Subquerier 优先匹配，因为更具体。

- [ ] **Step 7: 跑编译期断言测试**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe build ./...
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestSubquerier_Interface_Implementation ./...
```

预期：编译通过，测试 PASS。

- [ ] **Step 8: 跑全量测试确认无回归**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test ./...
```

预期：所有测试通过（应与基础设施前一致，未变行为）。

- [ ] **Step 9: Commit**

```bash
git add subquery.go subquery_test.go repository.go builder.go query.go
git commit -m "feat: 新增 Subquerier 接口与基础设施（v0.6.0 子查询）

- subquery.go: Subquerier 接口（含 gplusSubquery() unexported guard） + 编译期断言
- repository.go: ErrSubqueryNil sentinel
- builder.go: applyWhere 新增 Subquerier 识别分支（在 *gorm.DB 分支之前）
- query.go: *Query[T].gplusSubquery() 实现

Subquerier 唯一实现是 *Query[T]，Updater 不实现以保持职责清晰。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Query 集合操作符（InSub / NotInSub / OrInSub / OrNotInSub）

**Files:**
- Create: `D:\projects\gplus\query_subquery_test.go`
- Modify: `D:\projects\gplus\query.go`（追加 4 个方法）

- [ ] **Step 1: 写测试 — 4 个集合方法主路径 + Or 变体**

新建 `D:\projects\gplus\query_subquery_test.go`：

```go
package gplus

import (
	"context"
	"strings"
	"testing"
)

// TestQuery_InSub_Basic 验证 InSub 生成 SQL 形态 + 真实数据命中。
func TestQuery_InSub_Basic(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	// 准备数据：UserA(id=1) Amount=100/200, UserB(id=2) Amount=300, UserC 无订单
	users := []UserWithDelete{{Name: "UserA", Age: 20}, {Name: "UserB", Age: 30}, {Name: "UserC", Age: 25}}
	db.Create(&users)
	db.Create(&Order{UserID: 1, Amount: 100})
	db.Create(&Order{UserID: 1, Amount: 200})
	db.Create(&Order{UserID: 2, Amount: 300})

	// 子查询：所有有订单的 user_id
	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	q, u := NewQuery[UserWithDelete](ctx)
	q.InSub(&u.ID, subQ).Order(&u.ID, true)

	result, err := repo.List(q)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(result) != 2 || result[0].Name != "UserA" || result[1].Name != "UserB" {
		t.Fatalf("expected [UserA, UserB], got %+v", result)
	}
}

// TestQuery_NotInSub_Basic 验证 NotInSub。
func TestQuery_NotInSub_Basic(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	users := []UserWithDelete{{Name: "UserA", Age: 20}, {Name: "UserB", Age: 30}, {Name: "UserC", Age: 25}}
	db.Create(&users)
	db.Create(&Order{UserID: 1, Amount: 100})
	db.Create(&Order{UserID: 2, Amount: 300})

	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	q, u := NewQuery[UserWithDelete](ctx)
	q.NotInSub(&u.ID, subQ)

	result, err := repo.List(q)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(result) != 1 || result[0].Name != "UserC" {
		t.Fatalf("expected [UserC], got %+v", result)
	}
}

// TestQuery_OrInSub 验证 OrInSub 与 AND 条件混用。
func TestQuery_OrInSub(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	users := []UserWithDelete{{Name: "UserA", Age: 20}, {Name: "UserB", Age: 30}, {Name: "UserC", Age: 99}}
	db.Create(&users)
	db.Create(&Order{UserID: 1, Amount: 100})

	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	q, u := NewQuery[UserWithDelete](ctx)
	// age=99 OR id IN (subQ) → UserC（age=99）+ UserA（id IN subQ）
	q.Eq(&u.Age, 99).OrInSub(&u.ID, subQ).Order(&u.ID, true)

	result, err := repo.List(q)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 users, got %d: %+v", len(result), result)
	}
}

// TestQuery_OrNotInSub 验证 OrNotInSub 通过 SQL 形态 DryRun。
func TestQuery_OrNotInSub_DryRun(t *testing.T) {
	_, db := setupAdvancedDB(t)
	ctx := context.Background()

	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	q, u := NewQuery[UserWithDelete](ctx)
	q.Eq(&u.Age, 20).OrNotInSub(&u.ID, subQ)

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	if !strings.Contains(sql, "NOT IN") {
		t.Fatalf("expected SQL to contain NOT IN, got: %s", sql)
	}
}
```

- [ ] **Step 2: 跑测试确认编译失败**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestQuery_InSub_Basic ./...
```

预期：编译错误 `q.InSub undefined`。

- [ ] **Step 3: 在 query.go 实现 4 个集合方法**

在 `D:\projects\gplus\query.go` 的 `OrNotIn` 方法（第 305 行）之后追加：

```go
// InSub IN 子查询：col IN (subquery)。
//
// sub 必须为类型安全 *Query[X]；外部冒名实现被 gplusSubquery() guard 阻止。
// sub 应在传入前完成构建（包括 Select/Where/DataRuleBuilder），传入后再修改会
// 反映到最终 SQL（延迟调用语义）。
//
// sub 中需用 Select(&col) 限定单列；否则 GORM 运行时报多列错误。
func (q *Query[T]) InSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(false, col, OpIn, sub)
}

// OrInSub IN 子查询(或)。详见 InSub。
func (q *Query[T]) OrInSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(true, col, OpIn, sub)
}

// NotInSub NOT IN 子查询。详见 InSub。
func (q *Query[T]) NotInSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(false, col, OpNotIn, sub)
}

// OrNotInSub NOT IN 子查询(或)。详见 InSub。
func (q *Query[T]) OrNotInSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(true, col, OpNotIn, sub)
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run "TestQuery_InSub_Basic|TestQuery_NotInSub_Basic|TestQuery_OrInSub|TestQuery_OrNotInSub_DryRun" ./...
```

预期：4 个测试全部 PASS。

- [ ] **Step 5: 跑全量测试确认无回归**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test ./...
```

预期：所有测试通过。

- [ ] **Step 6: Commit**

```bash
git add query.go query_subquery_test.go
git commit -m "feat: Query 集合子查询方法 InSub/NotInSub + Or 变体（v0.6.0）

新增 4 个方法 + 4 个测试（DryRun + 真实数据混合断言）。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Query 标量比较子查询（EqSub/NeSub/GtSub/GteSub/LtSub/LteSub + Or 变体共 12 方法）

**Files:**
- Modify: `D:\projects\gplus\query_subquery_test.go`（追加测试）
- Modify: `D:\projects\gplus\query.go`（追加 12 方法）

- [ ] **Step 1: 写 6 个标量主路径测试 + 1 个 Or 变体真实查询**

在 `query_subquery_test.go` 末尾追加：

```go
// TestQuery_GtSub_Basic 验证 GtSub: WHERE age > (SELECT AVG(age) FROM users)。
func TestQuery_GtSub_Basic(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	users := []UserWithDelete{{Name: "Young", Age: 20}, {Name: "Avg", Age: 30}, {Name: "Old", Age: 40}}
	db.Create(&users)

	avgQ, _ := NewQuery[UserWithDelete](ctx)
	avgQ.SelectRaw("AVG(age)")

	q, u := NewQuery[UserWithDelete](ctx)
	q.GtSub(&u.Age, avgQ).Order(&u.ID, true)

	result, err := repo.List(q)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	// 平均 age=30，> 30 的只有 Old(40)
	if len(result) != 1 || result[0].Name != "Old" {
		t.Fatalf("expected [Old], got %+v", result)
	}
}

// TestQuery_ScalarSub_DryRun 表驱动覆盖 6 个标量子查询的 SQL 形态。
func TestQuery_ScalarSub_DryRun(t *testing.T) {
	_, db := setupAdvancedDB(t)
	ctx := context.Background()

	tests := []struct {
		name     string
		apply    func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier)
		wantOp   string
	}{
		{"EqSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.EqSub(&u.Age, sub) }, "="},
		{"NeSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.NeSub(&u.Age, sub) }, "<>"},
		{"GtSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.GtSub(&u.Age, sub) }, ">"},
		{"GteSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.GteSub(&u.Age, sub) }, ">="},
		{"LtSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.LtSub(&u.Age, sub) }, "<"},
		{"LteSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.LteSub(&u.Age, sub) }, "<="},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub, _ := NewQuery[UserWithDelete](ctx)
			sub.SelectRaw("AVG(age)")

			q, u := NewQuery[UserWithDelete](ctx)
			tt.apply(q, u, sub)

			sql, err := q.ToSQL(db)
			if err != nil {
				t.Fatalf("ToSQL failed: %v", err)
			}
			if !strings.Contains(sql, tt.wantOp+" (SELECT") {
				t.Fatalf("expected SQL to contain '%s (SELECT', got: %s", tt.wantOp, sql)
			}
		})
	}
}

// TestQuery_OrScalarSub_DryRun 验证 6 个 Or 标量变体 SQL 形态。
func TestQuery_OrScalarSub_DryRun(t *testing.T) {
	_, db := setupAdvancedDB(t)
	ctx := context.Background()

	tests := []struct {
		name  string
		apply func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier)
	}{
		{"OrEqSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.Eq(&u.Age, 0).OrEqSub(&u.Age, sub) }},
		{"OrNeSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.Eq(&u.Age, 0).OrNeSub(&u.Age, sub) }},
		{"OrGtSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.Eq(&u.Age, 0).OrGtSub(&u.Age, sub) }},
		{"OrGteSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.Eq(&u.Age, 0).OrGteSub(&u.Age, sub) }},
		{"OrLtSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.Eq(&u.Age, 0).OrLtSub(&u.Age, sub) }},
		{"OrLteSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.Eq(&u.Age, 0).OrLteSub(&u.Age, sub) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub, _ := NewQuery[UserWithDelete](ctx)
			sub.SelectRaw("AVG(age)")

			q, u := NewQuery[UserWithDelete](ctx)
			tt.apply(q, u, sub)

			sql, err := q.ToSQL(db)
			if err != nil {
				t.Fatalf("ToSQL failed: %v", err)
			}
			if !strings.Contains(strings.ToUpper(sql), "OR ") {
				t.Fatalf("expected SQL to contain OR, got: %s", sql)
			}
		})
	}
}
```

- [ ] **Step 2: 跑测试确认编译失败**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestQuery_ScalarSub_DryRun ./...
```

预期：编译错误，方法未定义。

- [ ] **Step 3: 在 query.go 实现 12 个标量方法**

在 `OrNotInSub` 方法之后追加 12 个方法。模板如下，全部 12 个用同一格式：

```go
// EqSub = 子查询：col = (subquery)。详见 InSub 关于 sub 生命周期约束。
func (q *Query[T]) EqSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(false, col, OpEq, sub)
}

// OrEqSub = 子查询(或)。详见 EqSub。
func (q *Query[T]) OrEqSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(true, col, OpEq, sub)
}

// NeSub <> 子查询：col <> (subquery)。详见 InSub。
func (q *Query[T]) NeSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(false, col, OpNe, sub)
}

// OrNeSub <> 子查询(或)。详见 NeSub。
func (q *Query[T]) OrNeSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(true, col, OpNe, sub)
}

// GtSub > 子查询：col > (subquery)。详见 InSub。
func (q *Query[T]) GtSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(false, col, OpGt, sub)
}

// OrGtSub > 子查询(或)。详见 GtSub。
func (q *Query[T]) OrGtSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(true, col, OpGt, sub)
}

// GteSub >= 子查询：col >= (subquery)。详见 InSub。
func (q *Query[T]) GteSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(false, col, OpGe, sub)
}

// OrGteSub >= 子查询(或)。详见 GteSub。
func (q *Query[T]) OrGteSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(true, col, OpGe, sub)
}

// LtSub < 子查询：col < (subquery)。详见 InSub。
func (q *Query[T]) LtSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(false, col, OpLt, sub)
}

// OrLtSub < 子查询(或)。详见 LtSub。
func (q *Query[T]) OrLtSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(true, col, OpLt, sub)
}

// LteSub <= 子查询：col <= (subquery)。详见 InSub。
func (q *Query[T]) LteSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(false, col, OpLe, sub)
}

// OrLteSub <= 子查询(或)。详见 LteSub。
func (q *Query[T]) OrLteSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(true, col, OpLe, sub)
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run "TestQuery_GtSub_Basic|TestQuery_ScalarSub_DryRun|TestQuery_OrScalarSub_DryRun" ./...
```

预期：全部 PASS（3 个父测试 + 12 个子测试）。

- [ ] **Step 5: 跑全量测试**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test ./...
```

预期：所有测试通过。

- [ ] **Step 6: Commit**

```bash
git add query.go query_subquery_test.go
git commit -m "feat: Query 标量比较子查询 12 方法（v0.6.0）

EqSub/NeSub/GtSub/GteSub/LtSub/LteSub + 6 个 Or 变体。
表驱动测试覆盖 SQL 形态 + 真实查询断言。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Query 错误路径 + 延迟语义锁定（5 sub-test）

**Files:**
- Modify: `D:\projects\gplus\query_subquery_test.go`（追加测试 + errSubquerier）

- [ ] **Step 1: 在 query_subquery_test.go 末尾追加 errSubquerier 测试辅助和 5 个错误路径测试**

```go
// errSubquerier 测试辅助：模拟一个返回预设错误的 Subquerier。
// 因 gplusSubquery() 是 unexported，外部包无法实现 Subquerier；
// 此辅助同包可用，正是 guard 设计目的（测试可模拟，外部不可冒名）。
type errSubquerier struct {
	err error
}

func (e *errSubquerier) ToDB(db *gorm.DB) *gorm.DB {
	session := db.Session(&gorm.Session{NewDB: true})
	if e.err != nil {
		_ = session.AddError(e.err)
	}
	return session
}

func (e *errSubquerier) GetError() error { return e.err }
func (e *errSubquerier) gplusSubquery()  {}

// TestQuery_InSub_NilSub 验证 sub == nil 立即追加 ErrSubqueryNil。
func TestQuery_InSub_NilSub(t *testing.T) {
	ctx := context.Background()
	q, u := NewQuery[UserWithDelete](ctx)
	q.InSub(&u.ID, nil)
	if !errors.Is(q.GetError(), ErrSubqueryNil) {
		t.Fatalf("expected ErrSubqueryNil, got %v", q.GetError())
	}
}

// TestQuery_InSub_SubError 验证 sub.GetError() 经 GORM 链传播。
func TestQuery_InSub_SubError(t *testing.T) {
	repo, _ := setupAdvancedDB(t)
	ctx := context.Background()

	subErr := errors.New("test sub error")
	sub := &errSubquerier{err: subErr}

	q, u := NewQuery[UserWithDelete](ctx)
	q.InSub(&u.ID, sub)

	_, err := repo.List(q)
	if err == nil {
		t.Fatalf("expected error from sub propagation, got nil")
	}
	if !strings.Contains(err.Error(), "test sub error") {
		t.Fatalf("expected sub error in chain, got: %v", err)
	}
}

// TestQuery_InSub_ColInvalid 验证非法 col 指针走 addCond 列名解析错误路径。
func TestQuery_InSub_ColInvalid(t *testing.T) {
	ctx := context.Background()

	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	q, _ := NewQuery[UserWithDelete](ctx)
	notRegistered := &struct{ X int }{}
	q.InSub(&notRegistered.X, subQ)

	if q.GetError() == nil {
		t.Fatalf("expected col resolution error, got nil")
	}
}

// TestQuery_InSub_OuterErrPriority 验证外层 q.GetError() 已有错误时 Repository 提前 return。
func TestQuery_InSub_OuterErrPriority(t *testing.T) {
	repo, _ := setupAdvancedDB(t)
	ctx := context.Background()

	q, u := NewQuery[UserWithDelete](ctx)
	q.errs = append(q.errs, errors.New("outer pre-existing error"))
	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)
	q.InSub(&u.ID, subQ)

	_, err := repo.List(q)
	if err == nil {
		t.Fatalf("expected error from outer errs, got nil")
	}
	if !strings.Contains(err.Error(), "outer pre-existing error") {
		t.Fatalf("expected outer error first, got: %v", err)
	}
}

// TestQuery_InSub_DeferredSemantics 锁定延迟调用语义。
// sub 在 InSub 后追加条件 → 最终 SQL 包含追加条件（防止未来"贴心"改为立即快照而不更新文档）。
func TestQuery_InSub_DeferredSemantics(t *testing.T) {
	_, db := setupAdvancedDB(t)
	ctx := context.Background()

	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID).Eq(&order.UserID, 1) // 初始条件

	q, u := NewQuery[UserWithDelete](ctx)
	q.InSub(&u.ID, subQ)

	// 传入后追加条件
	subQ.Eq(&order.Amount, 999)

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	// 延迟调用：追加的 amount=999 必须出现在最终 SQL 中
	if !strings.Contains(sql, "999") {
		t.Fatalf("expected SQL to contain 999 (deferred semantics), got: %s", sql)
	}
}
```

- [ ] **Step 2: 在 imports 块加入 errors 和 gorm.io/gorm**

`D:\projects\gplus\query_subquery_test.go` 顶部：

```go
package gplus

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"
)
```

- [ ] **Step 3: 跑测试**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run "TestQuery_InSub_NilSub|TestQuery_InSub_SubError|TestQuery_InSub_ColInvalid|TestQuery_InSub_OuterErrPriority|TestQuery_InSub_DeferredSemantics" ./...
```

预期：5 个测试全部 PASS（无需新增实现，已通过 Task 1-3 实现满足）。

- [ ] **Step 4: 跑全量测试**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test ./...
```

预期：所有测试通过。

- [ ] **Step 5: Commit**

```bash
git add query_subquery_test.go
git commit -m "test: Query 子查询错误路径 + 延迟语义锁定（v0.6.0）

5 个 sub-test：
- nil sub → ErrSubqueryNil
- sub.GetError() → GORM 链传播
- col 非法 → addCond 列解析错误
- 外层 errs 优先级
- 延迟调用语义锁定（sub 后续追加条件进入最终 SQL）

含 errSubquerier 测试辅助类型，因 gplusSubquery 是 unexported
故只能在同包测试文件内实现，正符合 guard 设计目的。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Updater 子查询 16 方法 + UpdateByCondTx 集成

**Files:**
- Create: `D:\projects\gplus\updater_subquery_test.go`
- Modify: `D:\projects\gplus\update.go`（追加 16 方法）

- [ ] **Step 1: 写 Updater 16 方法 DryRun 表驱动测试 + 4 个真实 UPDATE 集成**

新建 `D:\projects\gplus\updater_subquery_test.go`：

```go
package gplus

import (
	"context"
	"strings"
	"testing"
)

// TestUpdater_AllSub_DryRun 表驱动覆盖 16 个 Updater 子查询方法 SQL 形态。
func TestUpdater_AllSub_DryRun(t *testing.T) {
	_, db := setupAdvancedDB(t)
	ctx := context.Background()

	tests := []struct {
		name   string
		apply  func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier)
		wantOp string
	}{
		{"InSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.InSub(&m.ID, sub) }, "IN"},
		{"NotInSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.NotInSub(&m.ID, sub) }, "NOT IN"},
		{"EqSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.EqSub(&m.Age, sub) }, "="},
		{"NeSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.NeSub(&m.Age, sub) }, "<>"},
		{"GtSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.GtSub(&m.Age, sub) }, ">"},
		{"GteSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.GteSub(&m.Age, sub) }, ">="},
		{"LtSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.LtSub(&m.Age, sub) }, "<"},
		{"LteSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.LteSub(&m.Age, sub) }, "<="},
		{"OrInSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.Eq(&m.Age, 0).OrInSub(&m.ID, sub) }, "IN"},
		{"OrNotInSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.Eq(&m.Age, 0).OrNotInSub(&m.ID, sub) }, "NOT IN"},
		{"OrEqSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.Eq(&m.Age, 0).OrEqSub(&m.Age, sub) }, "="},
		{"OrNeSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.Eq(&m.Age, 0).OrNeSub(&m.Age, sub) }, "<>"},
		{"OrGtSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.Eq(&m.Age, 0).OrGtSub(&m.Age, sub) }, ">"},
		{"OrGteSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.Eq(&m.Age, 0).OrGteSub(&m.Age, sub) }, ">="},
		{"OrLtSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.Eq(&m.Age, 0).OrLtSub(&m.Age, sub) }, "<"},
		{"OrLteSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.Eq(&m.Age, 0).OrLteSub(&m.Age, sub) }, "<="},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subQ, order := NewQuery[Order](ctx)
			subQ.Select(&order.UserID)

			u, m := NewUpdater[UserWithDelete](ctx)
			u.Set(&m.Name, "X")
			tt.apply(u, m, subQ)

			sql, err := u.ToSQL(db)
			if err != nil {
				t.Fatalf("ToSQL failed: %v", err)
			}
			if !strings.Contains(strings.ToUpper(sql), tt.wantOp) {
				t.Fatalf("expected SQL to contain %q, got: %s", tt.wantOp, sql)
			}
		})
	}
}

// TestUpdater_InSub_RealUpdate 真实 UPDATE WHERE id IN (subquery)。
func TestUpdater_InSub_RealUpdate(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	users := []UserWithDelete{{Name: "UserA", Age: 20}, {Name: "UserB", Age: 30}, {Name: "UserC", Age: 25}}
	db.Create(&users)
	db.Create(&Order{UserID: 1, Amount: 100})
	db.Create(&Order{UserID: 2, Amount: 200})

	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	u, m := NewUpdater[UserWithDelete](ctx)
	u.Set(&m.Age, 99).InSub(&m.ID, subQ)

	affected, err := repo.UpdateByCond(u)
	if err != nil {
		t.Fatalf("UpdateByCond failed: %v", err)
	}
	if affected != 2 {
		t.Fatalf("expected 2 affected, got %d", affected)
	}

	// 验证：UserA + UserB age=99；UserC 无订单不受影响
	var got []UserWithDelete
	db.Order("id ASC").Find(&got)
	if len(got) != 3 {
		t.Fatalf("expected 3 users, got %d", len(got))
	}
	if got[0].Age != 99 || got[1].Age != 99 || got[2].Age != 25 {
		t.Fatalf("unexpected ages: %+v", got)
	}
}

// TestUpdater_GtSub_RealUpdate 真实 UPDATE WHERE age > (SELECT AVG(age))。
func TestUpdater_GtSub_RealUpdate(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	users := []UserWithDelete{{Name: "Young", Age: 20}, {Name: "Avg", Age: 30}, {Name: "Old", Age: 40}}
	db.Create(&users)

	avgQ, _ := NewQuery[UserWithDelete](ctx)
	avgQ.SelectRaw("AVG(age)")

	u, m := NewUpdater[UserWithDelete](ctx)
	u.Set(&m.Name, "Senior").GtSub(&m.Age, avgQ)

	affected, err := repo.UpdateByCond(u)
	if err != nil {
		t.Fatalf("UpdateByCond failed: %v", err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 affected (Old, age=40>30), got %d", affected)
	}
}

// TestUpdater_NotInSub_RealUpdate 真实 UPDATE WHERE id NOT IN (subquery)。
func TestUpdater_NotInSub_RealUpdate(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	users := []UserWithDelete{{Name: "UserA", Age: 20}, {Name: "UserB", Age: 30}, {Name: "UserC", Age: 25}}
	db.Create(&users)
	db.Create(&Order{UserID: 1, Amount: 100})

	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	u, m := NewUpdater[UserWithDelete](ctx)
	u.Set(&m.Age, 0).NotInSub(&m.ID, subQ)

	affected, err := repo.UpdateByCond(u)
	if err != nil {
		t.Fatalf("UpdateByCond failed: %v", err)
	}
	// UserB(2) + UserC(3) 不在订单中，UserA(1) 有订单
	if affected != 2 {
		t.Fatalf("expected 2 affected, got %d", affected)
	}
}

// TestUpdater_InSub_NilSub 验证 sub == nil 错误。
func TestUpdater_InSub_NilSub(t *testing.T) {
	ctx := context.Background()
	u, m := NewUpdater[UserWithDelete](ctx)
	u.InSub(&m.ID, nil)
	if u.GetError() == nil {
		t.Fatalf("expected error, got nil")
	}
}
```

- [ ] **Step 2: 跑测试确认编译失败**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestUpdater_AllSub_DryRun ./...
```

预期：编译错误，方法未定义。

- [ ] **Step 3: 在 update.go 实现 16 个 Updater 子查询方法**

在 `D:\projects\gplus\update.go` 末尾追加。模板与 query.go 完全对称：

```go
// InSub IN 子查询：col IN (subquery)。详见 Query.InSub。
func (u *Updater[T]) InSub(col any, sub Subquerier) *Updater[T] {
	if sub == nil {
		u.errs = append(u.errs, ErrSubqueryNil)
		return u
	}
	return u.addCond(false, col, OpIn, sub)
}

// OrInSub IN 子查询(或)。
func (u *Updater[T]) OrInSub(col any, sub Subquerier) *Updater[T] {
	if sub == nil {
		u.errs = append(u.errs, ErrSubqueryNil)
		return u
	}
	return u.addCond(true, col, OpIn, sub)
}

// NotInSub NOT IN 子查询。
//
// 注意 MySQL ERROR 1093：UPDATE 同表 IN 子查询限制（如
//   UPDATE users WHERE id IN (SELECT id FROM users WHERE x)
// 报错）。可改写为 JOIN UPDATE 或子查询包一层临时表。
func (u *Updater[T]) NotInSub(col any, sub Subquerier) *Updater[T] {
	if sub == nil {
		u.errs = append(u.errs, ErrSubqueryNil)
		return u
	}
	return u.addCond(false, col, OpNotIn, sub)
}

// OrNotInSub NOT IN 子查询(或)。
func (u *Updater[T]) OrNotInSub(col any, sub Subquerier) *Updater[T] {
	if sub == nil {
		u.errs = append(u.errs, ErrSubqueryNil)
		return u
	}
	return u.addCond(true, col, OpNotIn, sub)
}

// EqSub = 子查询。
func (u *Updater[T]) EqSub(col any, sub Subquerier) *Updater[T] {
	if sub == nil {
		u.errs = append(u.errs, ErrSubqueryNil)
		return u
	}
	return u.addCond(false, col, OpEq, sub)
}

// OrEqSub = 子查询(或)。
func (u *Updater[T]) OrEqSub(col any, sub Subquerier) *Updater[T] {
	if sub == nil {
		u.errs = append(u.errs, ErrSubqueryNil)
		return u
	}
	return u.addCond(true, col, OpEq, sub)
}

// NeSub <> 子查询。
func (u *Updater[T]) NeSub(col any, sub Subquerier) *Updater[T] {
	if sub == nil {
		u.errs = append(u.errs, ErrSubqueryNil)
		return u
	}
	return u.addCond(false, col, OpNe, sub)
}

// OrNeSub <> 子查询(或)。
func (u *Updater[T]) OrNeSub(col any, sub Subquerier) *Updater[T] {
	if sub == nil {
		u.errs = append(u.errs, ErrSubqueryNil)
		return u
	}
	return u.addCond(true, col, OpNe, sub)
}

// GtSub > 子查询。
func (u *Updater[T]) GtSub(col any, sub Subquerier) *Updater[T] {
	if sub == nil {
		u.errs = append(u.errs, ErrSubqueryNil)
		return u
	}
	return u.addCond(false, col, OpGt, sub)
}

// OrGtSub > 子查询(或)。
func (u *Updater[T]) OrGtSub(col any, sub Subquerier) *Updater[T] {
	if sub == nil {
		u.errs = append(u.errs, ErrSubqueryNil)
		return u
	}
	return u.addCond(true, col, OpGt, sub)
}

// GteSub >= 子查询。
func (u *Updater[T]) GteSub(col any, sub Subquerier) *Updater[T] {
	if sub == nil {
		u.errs = append(u.errs, ErrSubqueryNil)
		return u
	}
	return u.addCond(false, col, OpGe, sub)
}

// OrGteSub >= 子查询(或)。
func (u *Updater[T]) OrGteSub(col any, sub Subquerier) *Updater[T] {
	if sub == nil {
		u.errs = append(u.errs, ErrSubqueryNil)
		return u
	}
	return u.addCond(true, col, OpGe, sub)
}

// LtSub < 子查询。
func (u *Updater[T]) LtSub(col any, sub Subquerier) *Updater[T] {
	if sub == nil {
		u.errs = append(u.errs, ErrSubqueryNil)
		return u
	}
	return u.addCond(false, col, OpLt, sub)
}

// OrLtSub < 子查询(或)。
func (u *Updater[T]) OrLtSub(col any, sub Subquerier) *Updater[T] {
	if sub == nil {
		u.errs = append(u.errs, ErrSubqueryNil)
		return u
	}
	return u.addCond(true, col, OpLt, sub)
}

// LteSub <= 子查询。
func (u *Updater[T]) LteSub(col any, sub Subquerier) *Updater[T] {
	if sub == nil {
		u.errs = append(u.errs, ErrSubqueryNil)
		return u
	}
	return u.addCond(false, col, OpLe, sub)
}

// OrLteSub <= 子查询(或)。
func (u *Updater[T]) OrLteSub(col any, sub Subquerier) *Updater[T] {
	if sub == nil {
		u.errs = append(u.errs, ErrSubqueryNil)
		return u
	}
	return u.addCond(true, col, OpLe, sub)
}
```

- [ ] **Step 4: 跑测试**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run "TestUpdater_AllSub_DryRun|TestUpdater_InSub_RealUpdate|TestUpdater_GtSub_RealUpdate|TestUpdater_NotInSub_RealUpdate|TestUpdater_InSub_NilSub" ./...
```

预期：全部 PASS（4 个父测试 + 16 个子测试 + 错误路径）。

- [ ] **Step 5: 跑全量测试**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test ./...
```

预期：所有测试通过。

- [ ] **Step 6: Commit**

```bash
git add update.go updater_subquery_test.go
git commit -m "feat: Updater 子查询 16 方法（v0.6.0）

InSub/NotInSub/EqSub/NeSub/GtSub/GteSub/LtSub/LteSub + 8 个 Or 变体。
DryRun 表驱动覆盖全 16 方法 SQL 形态，3 个真实 UPDATE 集成
（InSub/GtSub/NotInSub）经 repo.UpdateByCond 验证 affected。

NotInSub godoc 标注 MySQL ERROR 1093（同表 IN 子查询限制）。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: DataRule × 子查询交互（6 sub-test）

**Files:**
- Modify: `D:\projects\gplus\query_subquery_test.go`（追加 DataRule 测试组）

- [ ] **Step 1: 在 query_subquery_test.go 末尾追加 DataRule 交互测试**

```go
// TestQuery_SubDataRule_Default_NotApplied 验证 sub.ToDB() 默认不应用 DataRule。
// 这锁定 query.go:207-217 ToDB 不调 DataRuleBuilder 的既有语义。
func TestQuery_SubDataRule_Default_NotApplied(t *testing.T) {
	_, db := setupAdvancedDB(t)

	// ctx 带 DataRule
	ctxWithRule := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Column: "age", Condition: OpEq, Value: 999},
	})

	subQ, order := NewQuery[Order](ctxWithRule)
	subQ.Select(&order.UserID)

	q, u := NewQuery[UserWithDelete](context.Background())
	q.InSub(&u.ID, subQ)

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	// 默认 sub 不应用 DataRule，age=999 不应出现在子查询 SQL 中
	if strings.Contains(sql, "999") {
		t.Fatalf("default sub.ToDB should NOT apply DataRule, got SQL: %s", sql)
	}
}

// TestQuery_SubDataRule_Explicit_Applied 验证显式 sub.DataRuleBuilder().ToDB() 应用 DataRule。
func TestQuery_SubDataRule_Explicit_Applied(t *testing.T) {
	_, db := setupAdvancedDB(t)

	ctxWithRule := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Column: "user_id", Condition: OpEq, Value: 999},
	})

	subQ, order := NewQuery[Order](ctxWithRule)
	subQ.Select(&order.UserID).DataRuleBuilder() // 显式应用

	q, u := NewQuery[UserWithDelete](context.Background())
	q.InSub(&u.ID, subQ)

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	if !strings.Contains(sql, "999") {
		t.Fatalf("explicit DataRuleBuilder should apply, got SQL: %s", sql)
	}
}

// TestQuery_SubDataRule_OuterOnly 外层有 DataRule，子查询无。
func TestQuery_SubDataRule_OuterOnly(t *testing.T) {
	_, db := setupAdvancedDB(t)

	ctxOuter := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Column: "age", Condition: OpEq, Value: 18},
	})

	subQ, order := NewQuery[Order](context.Background()) // 子查询无 DataRule
	subQ.Select(&order.UserID)

	q, u := NewQuery[UserWithDelete](ctxOuter)
	q.InSub(&u.ID, subQ).DataRuleBuilder() // 外层应用

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	if !strings.Contains(sql, "18") {
		t.Fatalf("outer DataRule should apply to outer SQL, got: %s", sql)
	}
}

// TestQuery_SubDataRule_ColumnNotInSubTable 子查询表无 DataRule 列时与现状一致行为。
func TestQuery_SubDataRule_ColumnNotInSubTable(t *testing.T) {
	repo, _ := setupAdvancedDB(t)

	// DataRule 列 "age" 在 Order 表上不存在（Order 字段：ID/UserID/Amount/Remark）
	ctxWithRule := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Column: "age", Condition: OpEq, Value: 18},
	})

	subQ, order := NewQuery[Order](ctxWithRule)
	subQ.Select(&order.UserID).DataRuleBuilder() // 显式应用 → SQL 引用不存在的列

	q, u := NewQuery[UserWithDelete](context.Background())
	q.InSub(&u.ID, subQ)

	_, err := repo.List(q)
	// 与现状一致：SQLite 会返回 "no such column" 错误
	if err == nil {
		t.Fatalf("expected SQL error for missing column 'age' in Order table")
	}
}

// TestQuery_SubDataRule_ReuseIdempotent 同一 sub 多次调 DataRuleBuilder 应幂等。
func TestQuery_SubDataRule_ReuseIdempotent(t *testing.T) {
	_, db := setupAdvancedDB(t)

	ctxWithRule := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Column: "user_id", Condition: OpEq, Value: 1},
	})

	subQ, order := NewQuery[Order](ctxWithRule)
	subQ.Select(&order.UserID).DataRuleBuilder().DataRuleBuilder() // 调两次

	q, u := NewQuery[UserWithDelete](context.Background())
	q.InSub(&u.ID, subQ)

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	// 幂等：user_id=1 只出现一次（不是 user_id=1 AND user_id=1）
	count := strings.Count(sql, "user_id` = 1")
	if count != 1 {
		// SQLite 用双引号转义
		count = strings.Count(sql, `"user_id" = 1`)
	}
	if count > 1 {
		t.Fatalf("DataRuleBuilder should be idempotent, got %d occurrences in SQL: %s", count, sql)
	}
}

// TestQuery_SubDataRule_ReverseRegression 反向回归：构造带 DataRule 的 ctx 调 sub.ToDB(db)，
// 断言生成的 SQL 不含 DataRule WHERE 子句。防止未来 contributor 给 ToDB 加 DataRuleBuilder
// 隐式调用而破坏既有安全语义。
func TestQuery_SubDataRule_ReverseRegression(t *testing.T) {
	_, db := setupAdvancedDB(t)

	ctxWithRule := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Column: "user_id", Condition: OpEq, Value: 999},
	})

	subQ, order := NewQuery[Order](ctxWithRule)
	subQ.Select(&order.UserID)

	// 直接走 sub 自身的 ToSQL（与 sub.ToDB 等价的可观测路径）
	sql, err := subQ.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	// 反向锁定：未显式 DataRuleBuilder() 时 SQL 不应含 DataRule 条件
	if strings.Contains(sql, "999") {
		t.Fatalf("ToDB without explicit DataRuleBuilder must NOT apply DataRule, got: %s", sql)
	}
}
```

- [ ] **Step 2: 跑测试**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run "TestQuery_SubDataRule" ./...
```

预期：6 个测试全部 PASS（无需新实现，验证既有语义）。

- [ ] **Step 3: 跑全量测试**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test ./...
```

预期：所有测试通过。

- [ ] **Step 4: Commit**

```bash
git add query_subquery_test.go
git commit -m "test: DataRule × 子查询交互 6 sub-test（v0.6.0）

锁定 sub.ToDB() 默认不应用 DataRule 的既有语义：
- 默认不应用
- 显式 DataRuleBuilder() 应用
- 外层有/子查询无的场景
- 子查询表无 DataRule 列时报错
- DataRuleBuilder 幂等
- 反向回归：未显式 DataRuleBuilder 时 SQL 不含 DataRule

防止未来给 ToDB 隐式加 DataRuleBuilder 破坏安全契约。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Session 隔离 + 嵌套子查询

**Files:**
- Modify: `D:\projects\gplus\query_subquery_test.go`（追加测试）

- [ ] **Step 1: 写 Session 隔离 + 嵌套子查询测试**

```go
// TestQuery_SubSession_Isolation 同一 sub 传给两个外层 query → 两条最终 SQL 互相独立。
// 不断言内部 Session 参数（不可观测），改为对比可观测 SQL 形态。
func TestQuery_SubSession_Isolation(t *testing.T) {
	_, db := setupAdvancedDB(t)
	ctx := context.Background()

	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	q1, u1 := NewQuery[UserWithDelete](ctx)
	q1.Eq(&u1.Age, 100).InSub(&u1.ID, subQ)

	q2, u2 := NewQuery[UserWithDelete](ctx)
	q2.Eq(&u2.Age, 200).InSub(&u2.ID, subQ)

	sql1, err1 := q1.ToSQL(db)
	if err1 != nil {
		t.Fatalf("q1.ToSQL: %v", err1)
	}
	sql2, err2 := q2.ToSQL(db)
	if err2 != nil {
		t.Fatalf("q2.ToSQL: %v", err2)
	}

	if !strings.Contains(sql1, "100") || strings.Contains(sql1, "200") {
		t.Fatalf("sql1 should contain only outer cond 100, got: %s", sql1)
	}
	if !strings.Contains(sql2, "200") || strings.Contains(sql2, "100") {
		t.Fatalf("sql2 should contain only outer cond 200, got: %s", sql2)
	}
}

// TestQuery_NestedSubquery 子查询内嵌套子查询。
func TestQuery_NestedSubquery(t *testing.T) {
	_, db := setupAdvancedDB(t)
	ctx := context.Background()

	// 内层：所有 Amount=100 的 user_id
	innerSub, order1 := NewQuery[Order](ctx)
	innerSub.Select(&order1.UserID).Eq(&order1.Amount, 100)

	// 中层：user_id 在 innerSub 内的所有 Order
	midSub, order2 := NewQuery[Order](ctx)
	midSub.Select(&order2.UserID).InSub(&order2.UserID, innerSub)

	// 外层：id 在 midSub 内的 user
	q, u := NewQuery[UserWithDelete](ctx)
	q.InSub(&u.ID, midSub)

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	// 嵌套：SELECT 子查询应出现两次
	count := strings.Count(strings.ToUpper(sql), "SELECT")
	if count < 3 {
		t.Fatalf("expected >= 3 SELECT (outer + 2 nested subqueries), got %d in: %s", count, sql)
	}
}
```

- [ ] **Step 2: 跑测试**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run "TestQuery_SubSession_Isolation|TestQuery_NestedSubquery" ./...
```

预期：2 个测试 PASS。

- [ ] **Step 3: 跑全量测试 + 覆盖率**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -coverprofile=coverage.out ./... && D:/Environment/golang/go1.21.11/bin/go.exe tool cover -func=coverage.out | tail -5
```

预期：所有测试通过，total 覆盖率 ≥ 96%。

- [ ] **Step 4: Commit**

```bash
git add query_subquery_test.go
git commit -m "test: Session 隔离 + 嵌套子查询（v0.6.0）

- 同一 sub 复用到两个外层 query：DryRun 对比 SQL 验证互相独立
- 三层嵌套：内层 Eq → 中层 InSub → 外层 InSub，断言 SQL 含 3+ SELECT

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: 文档收尾（godoc + CHANGELOG + README）

**Files:**
- Modify: `D:\projects\gplus\CHANGELOG.md`（新增 v0.6.0 章节）
- Modify: `D:\projects\gplus\README.md`（新增子查询使用示例）

- [ ] **Step 1: 在 CHANGELOG.md 顶部（## [0.5.1] 之前）追加 v0.6.0 章节**

```markdown
## [0.6.0] - 2026-04-30

### 新增

- **类型安全子查询**：消灭体系性 `WhereRaw` 子查询裂缝
  - `Subquerier` 接口（含 `gplusSubquery()` unexported guard 阻止外部冒名实现）
  - `Query[T]` 16 个新方法：`InSub` / `NotInSub` / `EqSub` / `NeSub` / `GtSub` / `GteSub` / `LtSub` / `LteSub` + 8 个 Or 变体
  - `Updater[T]` 16 个新方法（同形态）
  - 任意 `*Query[X]` 自动满足 `Subquerier`，X 可与外层 T 不同
- `ErrSubqueryNil` sentinel：`InSub(col, nil)` 时立即追加该错误

### 行为约束（须知）

- **延迟调用语义**：`sub` 传入 `InSub` 后仍可被修改，修改会反映到最终 SQL（与现有 `q.In(col, subQ.ToDB(db))` 一致）。godoc 推荐 sub 构建完成后再传入，传入后不要修改
- **sub.ToDB() 默认不应用 DataRule**：与 v0.5.x 既有语义保持一致；如需在子查询施加数据权限，须在传入前显式调 `sub.DataRuleBuilder()`
- **MySQL UPDATE 同表 IN 限制（ERROR 1093）**：`Updater.InSub`/`NotInSub` 在同表子查询场景下 MySQL 报错；可改写为 JOIN UPDATE 或子查询包临时表

### 测试

- 新增 `query_subquery_test.go`（~420 行）+ `updater_subquery_test.go`（~250 行）
- 覆盖：32 方法主路径 + Or 变体 + 错误路径 + 延迟语义锁定 + DataRule 6 场景 + Session 隔离 + 嵌套
- 测试覆盖率 ≥ 96%

### 不在本期范围

- **EXISTS / NOT EXISTS**：90% 真实场景为 correlated subquery，需 v0.7.0 alias 体系到位才能消灭关联条件 WhereRaw；提前发布会强制 v0.7.0 破坏性签名变更
- **ANY / ALL 变体**：v0.7.0 候选清单（提升优先级）
- **SELECT 子查询 / 跨表列引用 API**：需要 alias 体系，单独立项

---

```

- [ ] **Step 2: 在 README.md "版本历史" 章节添加 v0.6.0 子章节**

先用 Grep 工具定位 README.md 中 "v0.5.1" 标题的行号：

```
Grep: pattern="v0\.5\.1", path="D:\\projects\\gplus\\README.md"
```

然后用 Edit 工具，在 v0.5.1 子章节标题**之前**插入下面的 v0.6.0 子章节：

```markdown
### v0.6.0 (2026-04-30)

类型安全子查询，消灭体系性 `WhereRaw` 裂缝。

```go
// 1. WHERE id IN (SELECT user_id FROM orders WHERE status='paid')
paidUserIDs, order := gplus.NewQuery[Order](ctx)
paidUserIDs.Select(&order.UserID).Eq(&order.Status, "paid")

q, user := gplus.NewQuery[User](ctx)
q.InSub(&user.ID, paidUserIDs).Order(&user.CreatedAt, false)
result, _ := repo.List(q)

// 2. UPDATE users SET status='inactive' WHERE id IN (SELECT user_id FROM orders WHERE last_order < cutoff)
inactiveOrders, order2 := gplus.NewQuery[Order](ctx)
inactiveOrders.Select(&order2.UserID).Lt(&order2.LastOrderAt, cutoff)

u, user3 := gplus.NewUpdater[User](ctx)
u.Set(&user3.Status, "inactive").InSub(&user3.ID, inactiveOrders)
repo.UpdateByCond(u)
```

新增 32 方法（Query 16 + Updater 16） + `Subquerier` 接口 + `ErrSubqueryNil` sentinel。

⚠️ **延迟调用语义**：`sub` 传入后仍可被修改，修改会反映到最终 SQL。推荐 sub 构建完成后再传入。

详见 CHANGELOG v0.6.0 章节和 spec 文档。
```

- [ ] **Step 3: 跑全量测试 + 覆盖率最终验证**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test ./... && D:/Environment/golang/go1.21.11/bin/go.exe test -coverprofile=coverage.out ./... && D:/Environment/golang/go1.21.11/bin/go.exe tool cover -func=coverage.out | tail -3
```

预期：所有测试通过，total 覆盖率 ≥ 96%。

- [ ] **Step 4: Commit + git tag**

```bash
git add CHANGELOG.md README.md
git commit -m "docs: 发布 v0.6.0（类型安全子查询）

CHANGELOG 新增 v0.6.0 章节详列 32 个新方法、Subquerier 接口、
延迟调用语义约束、MySQL ERROR 1093 限制说明。
README 版本历史新增 v0.6.0 子章节含使用示例。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"

git tag v0.6.0
```

---

## 自检清单（实施前确认）

- [ ] spec 全部 18+7+1=26 条修订项已落到代码（接口/sentinel/builder 分支/16x2 方法/6 DataRule/2 Session+嵌套/godoc）
- [ ] `*Query[T]` 实现 `gplusSubquery()`，`*Updater[T]` 不实现（保持职责清晰）
- [ ] `applyWhere` Subquerier 分支放在 `*gorm.DB` 分支**之前**，且都在 `clauseStr==""` 短路**之后**
- [ ] 32 方法每个都有 `if sub == nil { append ErrSubqueryNil; return }` 入口检查
- [ ] 错误路径测试中 `errSubquerier` 实现 `gplusSubquery()` — 因同包可实现，正是 guard 设计
- [ ] DataRule 反向回归测试断言"未显式 DataRuleBuilder 时 SQL 不含 DataRule"
- [ ] CHANGELOG / README 版本号一致为 v0.6.0
- [ ] 最终 git tag `v0.6.0`

---

## 失败回退预案

任一 Task 失败时：
1. **测试失败**：用 `git diff HEAD` 检查改动，逐文件回退或修复
2. **编译失败**：通常是接口实现不完整或方法签名不匹配，参考 spec "立即 ToDB" 章节伪代码
3. **整批回退**：每个 Task 是独立 commit，可 `git revert <commit>` 滚回单 Task；如需全滚则 `git reset --hard d76625f`（v0.5.1 发布点）

---

**Plan 完成。请用 `superpowers:subagent-driven-development` 或 `superpowers:executing-plans` 开始执行。**
