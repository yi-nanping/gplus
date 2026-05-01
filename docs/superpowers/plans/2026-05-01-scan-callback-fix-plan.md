# Plan: Query-chain-safe 投影 API + aggregate Scan 漏洞修复实施（v0.7.0）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development（推荐）or superpowers:executing-plans 实施。步骤用 `- [ ]` 复选框跟踪。

**Spec**：[`docs/superpowers/specs/2026-05-01-scan-callback-fix-design.md`](../specs/2026-05-01-scan-callback-fix-design.md)
**日期**：2026-05-01
**目标版本**：v0.7.0
**预估工作量**：~280 行代码 + ~600 行测试

## 目标与成功标准

**Goal**：新增 4 个包级泛型函数 `FindAs/FindAsTx/FindOneAs/FindOneAsTx`（投影查询走 Query callback chain），修复 `repository.go:848` aggregate 内部 `.Scan` → `.Find` 漏洞，沉淀 GORM 行为锁定为永久 probe 测试。

**Architecture**：
- 包级泛型函数（与 `Sum/Max/Min/Avg/Pluck` 同形态），Go 1.18+ 类型推导后调用形态 `gplus.FindAs(repo, q, &rows)` 无需写类型参数
- aggregate 用包级私有 `aggregateWrap[R]` wrapper struct + alias `gplus_agg_v` 防业务列冲突
- 6 条永久 probe 断言锁定 GORM v1.31.x 行为，未来升级自动 canary

**Tech Stack**：Go 1.24 / GORM v1.31.1 / SQLite（测试） / 标准库 testing

**完成判定**：
- 永久 probe `TestGORMCallbackBehaviorProbe` 6 条断言全绿
- callback 触发矩阵全部按预期 (Query chain count = 1, Row chain count = 0)
- aggregate 已有测试不退化（Sum/Max/Min/Avg 全绿，含空表 NULL 场景）
- `D:/Environment/golang/go1.21.11/bin/go.exe test -race ./...` 全量 PASS
- 新增代码独立覆盖率 ≥ 94%；整体覆盖率 ≥ 93.5%
- CHANGELOG / README / godoc 全部同步

## 文件结构

| 文件 | 操作 | 责任 |
|---|---|---|
| `find_as.go` | **新建** | 4 个新函数 + `aggregateWrap[R]` 私有类型 + `aggregateAlias` 常量 + `ErrFindOneAsConflict` sentinel + godoc |
| `find_as_test.go` | **新建** | 永久 probe + callback 矩阵 + 投影正确性 + 边界 |
| `repository.go:829-878` | 修改 | `aggregate` 函数内部 `.Scan(&ptr)` → `.Find(&[]aggregateWrap[R])` + godoc |
| `CHANGELOG.md` | 修改 | 增 `## [0.7.0]` 条目 |
| `README.md` | 修改 | 「已知陷阱」段加跨租户后果 / 排查命令 / DataRule JOIN / col 字符串警告 |

## 依赖图

```
S0 (前置核对) ──> S1 (永久 probe RED+GREEN — 锁 GORM 行为)
                  └──> S2 (find_as.go 类型/常量/sentinel 定义)
                        └──> S3 (aggregate 改造 — 已有测试不退化)
                              └──> S4 (callback 矩阵 RED)
                                    └──> S5 (FindAs/FindAsTx GREEN)
                                          └──> S6 (FindOneAs/FindOneAsTx + ErrFindOneAsConflict GREEN)
                                                └──> S7 (投影正确性 table-driven)
                                                      └──> S8 (边界 + DataRule + JOIN 复合)
                                                            ├──> S9 (godoc 补全)
                                                            ├──> S10 (CHANGELOG)
                                                            ├──> S11 (README)
                                                            └──> S12 (final 验证 + commit 复核)
```

S9/S10/S11 三者完全并行；S12 是终点。

---

## 任务步骤

### S0：前置核对（5 分钟，人工读，不派 subagent）

**目的**：核对 spec 模板与实际代码的偏差，避免后续任务踩老坑。

**检查点**：

- [ ] `repository.go:829-853` 现有 `aggregate` 函数实际签名 `func aggregate[T any, R any, D comparable](r *Repository[D, T], q *Query[T], fn string, col any, tx *gorm.DB) (result R, err error)` 与 spec §3.3 一致
- [ ] `query.go` 中 `Query[T]` 结构体内嵌 `ScopeBuilder`，`q.ScopeBuilder.limit / offset` 是同包 unexported 字段可直接访问
- [ ] `builder.go:108-129` `BuildCount` 行为符合 spec 假设（不含 ORDER/LIMIT/Preload）
- [ ] `model_test.go` 中 `setupTestDB[T]` / `assertEqual` / `assertError` 可被新测试文件复用
- [ ] `dbResolver` 私有方法签名 `func (r *Repository[D, T]) dbResolver(ctx context.Context, tx *gorm.DB) *gorm.DB` 与 spec §3.2 实现骨架兼容

**模型分级**：N/A（人工，不派 subagent）

---

### S1：永久 probe 测试 RED+GREEN（核心 — 锁 GORM 行为）

**Files:**
- Create: `D:/projects/gplus/find_as_test.go`

**目的**：把 GORM v1.31.x callback chain 行为沉淀为常驻单元测试，未来升级行为变化第一时间感知。这是 spec §4.1 的强制要求。

- [ ] **Step 1：创建测试文件骨架（包声明 + import）**

```go
package gplus

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// probeUser 是仅用于 GORM 行为锁定测试的最小模型
type probeUser struct {
	ID   uint `gorm:"primarykey"`
	Name string
	Age  int
}
```

- [ ] **Step 2：写永久 probe 测试 `TestGORMCallbackBehaviorProbe`**

把以下完整测试写入 `find_as_test.go`：

```go
// TestGORMCallbackBehaviorProbe 锁定 GORM v1.31.x 的 callback chain 行为。
// 任意一条断言失败 → GORM 行为已变 → 必须重审 spec §1.1 + 附录 B。
func TestGORMCallbackBehaviorProbe(t *testing.T) {
	openDB := func(t *testing.T) *gorm.DB {
		t.Helper()
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.AutoMigrate(&probeUser{}); err != nil {
			t.Fatal(err)
		}
		db.Create(&probeUser{Name: "alice", Age: 20})
		return db
	}

	// probe：返回 (queryCount*int, rowCount*int, observedTable*string, cleanup)
	setupProbe := func(t *testing.T, db *gorm.DB) (q, r *int, tbl *string, cleanup func()) {
		t.Helper()
		var qc, rc int
		var ot string
		err := db.Callback().Query().Before("gorm:query").
			Register("test:probe_query", func(d *gorm.DB) {
				qc++
				if d.Statement.Schema != nil {
					ot = d.Statement.Schema.Table
				}
			})
		if err != nil {
			t.Fatal(err)
		}
		err = db.Callback().Row().Before("gorm:row").
			Register("test:probe_row", func(d *gorm.DB) {
				rc++
			})
		if err != nil {
			t.Fatal(err)
		}
		return &qc, &rc, &ot, func() {
			_ = db.Callback().Query().Remove("test:probe_query")
			_ = db.Callback().Row().Remove("test:probe_row")
		}
	}

	t.Run("Find_走_Query_chain", func(t *testing.T) {
		db := openDB(t)
		qc, rc, _, cleanup := setupProbe(t, db)
		defer cleanup()
		var rows []probeUser
		_ = db.Model(&probeUser{}).Find(&rows).Error
		if *qc != 1 || *rc != 0 {
			t.Fatalf("Find: queryCount=%d rowCount=%d, 期望 1/0", *qc, *rc)
		}
	})

	t.Run("Scan_走_Row_chain（基线）", func(t *testing.T) {
		db := openDB(t)
		qc, rc, _, cleanup := setupProbe(t, db)
		defer cleanup()
		var rows []probeUser
		_ = db.Model(&probeUser{}).Scan(&rows).Error
		if *qc != 0 || *rc != 1 {
			t.Fatalf("Scan: queryCount=%d rowCount=%d, 期望 0/1（GORM 行为已变？）", *qc, *rc)
		}
	})

	t.Run("Rows_走_Row_chain（基线）", func(t *testing.T) {
		db := openDB(t)
		qc, rc, _, cleanup := setupProbe(t, db)
		defer cleanup()
		rows, _ := db.Model(&probeUser{}).Rows()
		if rows != nil {
			_ = rows.Close()
		}
		if *qc != 0 || *rc != 1 {
			t.Fatalf("Rows: queryCount=%d rowCount=%d, 期望 0/1（GORM 行为已变？）", *qc, *rc)
		}
	})

	t.Run("First_dest_VO_不覆盖_Schema", func(t *testing.T) {
		db := openDB(t)
		_, _, tbl, cleanup := setupProbe(t, db)
		defer cleanup()
		type VO struct {
			Name string
		}
		var vo VO
		_ = db.Model(&probeUser{}).First(&vo).Error
		if *tbl != "probe_users" {
			t.Fatalf("First(&VO) 后 Schema.Table=%q, 期望 'probe_users'（GORM 行为已变 → spec C1 复活）", *tbl)
		}
	})

	t.Run("Find_aggregateWrap_不覆盖_Schema", func(t *testing.T) {
		db := openDB(t)
		_, _, tbl, cleanup := setupProbe(t, db)
		defer cleanup()
		var rows []aggregateWrap[int64]
		_ = db.Model(&probeUser{}).Select("SUM(age) AS " + aggregateAlias).Find(&rows).Error
		if *tbl != "probe_users" {
			t.Fatalf("Find(&[]aggregateWrap[int64]) 后 Schema.Table=%q, 期望 'probe_users'（aggregate isolation 失效）", *tbl)
		}
	})

	t.Run("空表_SUM_aggregateWrap_NULL_为_nil", func(t *testing.T) {
		emptyDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		_ = emptyDB.AutoMigrate(&probeUser{})
		var rows []aggregateWrap[int64]
		err = emptyDB.Model(&probeUser{}).Select("SUM(age) AS " + aggregateAlias).Find(&rows).Error
		if err != nil {
			t.Fatalf("空表 SUM Find: 报错 %v（NULL 处理失败 → 方案 G 失效）", err)
		}
		if len(rows) != 1 || rows[0].V != nil {
			t.Fatalf("空表 SUM: rows=%v, 期望 1 行 V=nil", rows)
		}
	})

	_ = context.Background() // import 占位
}
```

- [ ] **Step 3：运行 — 应失败（aggregateWrap / aggregateAlias 未定义）**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestGORMCallbackBehaviorProbe -v ./...
```

期望：编译失败 `undefined: aggregateWrap` / `undefined: aggregateAlias`

- [ ] **Step 4：commit RED**

```bash
git add D:/projects/gplus/find_as_test.go
git commit -m "test: 永久 probe TestGORMCallbackBehaviorProbe RED（锁 GORM v1.31.x callback chain）"
```

**模型分级**：sonnet（GORM 行为细致测试，关键基线，不能错）

**潜在坑**：
- callback `Before("gorm:query")` 必须用此精确名 — GORM 内置 callback name `"gorm:query"` 在 `gorm.io/gorm@v1.31.1/callbacks/query.go:7` 注册
- `Model(&probeUser{}).Rows()` 调用后必须 `defer rows.Close()`，避免 SQLite 连接泄漏
- `setupProbe` 返回 `*int / *string` 指针让闭包内修改对外可见

---

### S2：`find_as.go` 类型/常量/sentinel 定义（GREEN minimum，让 S1 编译通过）

**Files:**
- Create: `D:/projects/gplus/find_as.go`

**目的**：定义 `aggregateWrap[R]` / `aggregateAlias` / `ErrFindOneAsConflict`，让 S1 测试能编译通过（但 4 个新函数还没实现 — 留给 S5/S6）。

- [ ] **Step 1：写最小 find_as.go 让 S1 编译过**

```go
// Package gplus — Query-chain-safe 投影查询 API
//
// 本文件提供 FindAs / FindOneAs / FindAsTx / FindOneAsTx 四个包级泛型函数，
// 让用户能写"投影查询走 GORM Query callback chain"，避开 db.Scan / db.Row / db.Rows
// 绕过 Query chain 的隐患（导致下游 isolation/审计 callback 不触发）。
//
// 详见 docs/superpowers/specs/2026-05-01-scan-callback-fix-design.md
package gplus

import "errors"

// aggregateAlias 是 aggregate 函数生成 SQL 时的列 alias 名。
// 加 gplus_ 前缀以避免与业务表真实列名冲突（实测过 v / __agg__ 等短命名易撞）。
const aggregateAlias = "gplus_agg_v"

// aggregateWrap 包裹聚合结果列；用 Find 路径走 Query callback chain；
// V *R 保证空表 SUM 返回 NULL 时为 nil（不报 "converting NULL to int64" 错误）。
//
// 故意 unexported：内部实现细节，不入公开 API 表面。未来若加用户层
// 聚合表达式 API，新增独立的公开类型（如 AggregateValue[R]）。
type aggregateWrap[R any] struct {
	V *R `gorm:"column:gplus_agg_v"`
}

// ErrFindOneAsConflict 表示 FindOneAs 与 q.Limit() / q.Page() 组合调用。
// 内部 First 会追加 LIMIT 1，与已有 LIMIT 叠加，部分 DB 行为未定义。
var ErrFindOneAsConflict = errors.New("gplus: FindOneAs 不可与 q.Limit() / q.Page() 组合调用")
```

- [ ] **Step 2：运行 S1 测试 — 现在 6 条 sub-test 应该全部 RUN（但部分可能失败，因为 aggregate 还用旧的 .Scan）**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestGORMCallbackBehaviorProbe -v ./...
```

期望：编译通过。**6 条 sub-test 应全 PASS**（永久 probe 不依赖 aggregate 函数本身，只依赖 GORM 原生行为 + aggregateWrap 类型已存在）

- [ ] **Step 3：commit GREEN**

```bash
git add D:/projects/gplus/find_as.go
git commit -m "feat: 新增 aggregateWrap[R] 私有类型 + ErrFindOneAsConflict sentinel"
```

**模型分级**：haiku（机械定义，无逻辑）

**潜在坑**：
- gorm tag `column:gplus_agg_v` 必须与常量 `aggregateAlias` 字面量一致 — 不能用字符串拼接
- 包注释保留（包 doc 在文件顶部首个非空行的 `// Package` 注释）— 否则 godoc 会丢失模块说明

---

### S3：aggregate 改造（让现有 Sum/Max/Min/Avg 测试不退化 + 走 Query chain）

**Files:**
- Modify: `D:/projects/gplus/repository.go:829-853`

**目的**：把 `aggregate` 函数内部 `.Scan(&ptr)` 改为 `.Find(&[]aggregateWrap[R])`，alias 用常量 `aggregateAlias`。

- [ ] **Step 1：读现有 aggregate 函数**

```bash
# 用 Read 工具读 repository.go:829-853，确认实际行号 + 现有逻辑
```

- [ ] **Step 2：替换 aggregate 函数体**

```go
// aggregate 内部通用聚合执行函数。
// 用 Find(&[]aggregateWrap[R]) 走 GORM Query callback chain，下游挂在 Query chain
// 上的 isolation/审计 callback 会被触发。wrapper struct 中 V *R 保证空表/无匹配时
// SQL NULL → nil（不报 "converting NULL to int64" 错误）。
//
// 修改历史：v0.7.0 之前为 .Scan(&ptr)，绕过 Query chain（隔离失效隐患）。
// 详见 docs/superpowers/specs/2026-05-01-scan-callback-fix-design.md
func aggregate[T any, R any, D comparable](r *Repository[D, T], q *Query[T], fn string, col any, tx *gorm.DB) (result R, err error) {
	if q == nil {
		return result, ErrQueryNil
	}
	if err = q.GetError(); err != nil {
		return result, err
	}
	colName, err := resolveColumnName(col)
	if err != nil {
		return result, err
	}
	if err = q.DataRuleBuilder().GetError(); err != nil {
		return result, err
	}
	expr := fmt.Sprintf("%s(%s) AS %s", fn, colName, aggregateAlias)
	var rows []aggregateWrap[R]
	// 聚合查询只需 WHERE/JOIN/GROUP BY，与 BuildCount 路径一致；走 Find 触发 Query callback chain
	err = r.dbResolver(q.Context(), tx).Model(new(T)).Scopes(q.BuildCount()).Select(expr).Find(&rows).Error
	if err == nil && len(rows) > 0 && rows[0].V != nil {
		result = *rows[0].V
	}
	return result, err
}
```

- [ ] **Step 3：运行现有 aggregate 测试 — 必须全绿**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run "TestRepository_Sum|TestRepository_Max|TestRepository_Min|TestRepository_Avg|TestAggregate" -v ./...
```

期望：全部 PASS（包括空表 NULL → 零值场景）

- [ ] **Step 4：运行全量测试 — 不退化**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test ./...
```

期望：全部 PASS

- [ ] **Step 5：commit**

```bash
git add D:/projects/gplus/repository.go
git commit -m "fix: aggregate 内部 .Scan(\$&ptr\$) → .Find(\$&[]aggregateWrap[R]\$) 走 Query callback chain

Sum/Max/Min/Avg 之前内部用 db.Scan(\$&ptr\$)，走 Row callback chain，绕过下游
isolation/审计 callback。改用 Find(\$&[]aggregateWrap[R]\$) 后走 Query chain，
下游隔离 callback 现可正确触发。NULL 语义保持（V *R 在 SQL NULL 时为 nil）。"
```

**模型分级**：sonnet（涉及 NULL 边界 + Schema 不变性，不能机械）

**潜在坑**：
- `fmt.Sprintf` 必须 import — 检查 `import` 段是否已含 `fmt`（现有 aggregate 已用 `fmt.Sprintf`，应已有）
- `aggregateWrap[R]` 与 `aggregateAlias` 在同包，可直接引用
- `q.GetError()` / `resolveColumnName(col)` / `q.DataRuleBuilder()` 调用顺序与原函数一致 — 不要改顺序

---

### S4：callback 触发矩阵测试 RED（FindAs/FindOneAs 还没实现）

**Files:**
- Modify: `D:/projects/gplus/find_as_test.go`

**目的**：写 spec §4.2 callback 触发矩阵测试，证明 FindAs/FindOneAs/aggregate 修复后走 Query chain。这一步先 RED（FindAs 还未实现）。

- [ ] **Step 1：在 find_as_test.go 末尾追加 callback 矩阵测试**

```go
// TestFindAs_CallbackChainMatrix 验证 FindAs/FindOneAs/aggregate 走 Query callback chain。
// 这是漏洞修复的核心证明 — 任意一条失败说明回归到了 Row chain。
func TestFindAs_CallbackChainMatrix(t *testing.T) {
	type matrixUser struct {
		ID   uint `gorm:"primarykey"`
		Name string
		Age  int
	}

	openDB := func(t *testing.T) *gorm.DB {
		t.Helper()
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.AutoMigrate(&matrixUser{}); err != nil {
			t.Fatal(err)
		}
		db.Create(&matrixUser{Name: "alice", Age: 20})
		db.Create(&matrixUser{Name: "bob", Age: 30})
		return db
	}

	type counts struct {
		query int
		row   int
	}
	probe := func(t *testing.T, db *gorm.DB) (*counts, func()) {
		t.Helper()
		c := &counts{}
		_ = db.Callback().Query().Before("gorm:query").
			Register("test:matrix_q", func(*gorm.DB) { c.query++ })
		_ = db.Callback().Row().Before("gorm:row").
			Register("test:matrix_r", func(*gorm.DB) { c.row++ })
		return c, func() {
			_ = db.Callback().Query().Remove("test:matrix_q")
			_ = db.Callback().Row().Remove("test:matrix_r")
		}
	}

	type matrixVO struct {
		Name string
	}

	t.Run("FindAs_有数据", func(t *testing.T) {
		db := openDB(t)
		c, cleanup := probe(t, db)
		defer cleanup()
		repo := NewRepository[uint, matrixUser](db)
		q, _ := NewQuery[matrixUser](context.Background())
		var rows []matrixVO
		err := FindAs[matrixUser, matrixVO, uint](repo, q, &rows)
		if err != nil {
			t.Fatalf("FindAs err=%v", err)
		}
		if c.query != 1 || c.row != 0 {
			t.Fatalf("FindAs: query=%d row=%d, 期望 1/0", c.query, c.row)
		}
		if len(rows) != 2 {
			t.Fatalf("FindAs len=%d, 期望 2", len(rows))
		}
	})

	t.Run("FindOneAs_有匹配", func(t *testing.T) {
		db := openDB(t)
		c, cleanup := probe(t, db)
		defer cleanup()
		repo := NewRepository[uint, matrixUser](db)
		q, mu := NewQuery[matrixUser](context.Background())
		q.Eq(&mu.Name, "alice")
		var one matrixVO
		err := FindOneAs[matrixUser, matrixVO, uint](repo, q, &one)
		if err != nil {
			t.Fatalf("FindOneAs err=%v", err)
		}
		if c.query != 1 || c.row != 0 {
			t.Fatalf("FindOneAs: query=%d row=%d, 期望 1/0", c.query, c.row)
		}
		if one.Name != "alice" {
			t.Fatalf("FindOneAs Name=%q, 期望 alice", one.Name)
		}
	})

	t.Run("FindOneAs_无匹配_返回_ErrRecordNotFound", func(t *testing.T) {
		db := openDB(t)
		c, cleanup := probe(t, db)
		defer cleanup()
		repo := NewRepository[uint, matrixUser](db)
		q, mu := NewQuery[matrixUser](context.Background())
		q.Eq(&mu.Name, "nobody")
		var one matrixVO
		err := FindOneAs[matrixUser, matrixVO, uint](repo, q, &one)
		if err == nil {
			t.Fatal("FindOneAs 期望 ErrRecordNotFound，实际 nil")
		}
		if c.query != 1 || c.row != 0 {
			t.Fatalf("FindOneAs(无匹配): query=%d row=%d, 期望 1/0", c.query, c.row)
		}
	})

	t.Run("Sum_有数据", func(t *testing.T) {
		db := openDB(t)
		c, cleanup := probe(t, db)
		defer cleanup()
		repo := NewRepository[uint, matrixUser](db)
		q, mu := NewQuery[matrixUser](context.Background())
		sum, err := Sum[matrixUser, int64, uint](repo, q, &mu.Age)
		if err != nil {
			t.Fatalf("Sum err=%v", err)
		}
		if c.query != 1 || c.row != 0 {
			t.Fatalf("Sum: query=%d row=%d, 期望 1/0（aggregate 修复）", c.query, c.row)
		}
		if sum != 50 {
			t.Fatalf("Sum=%d, 期望 50", sum)
		}
	})

	t.Run("Sum_空表_NULL_零值", func(t *testing.T) {
		emptyDB, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		_ = emptyDB.AutoMigrate(&matrixUser{})
		c, cleanup := probe(t, emptyDB)
		defer cleanup()
		repo := NewRepository[uint, matrixUser](emptyDB)
		q, mu := NewQuery[matrixUser](context.Background())
		sum, err := Sum[matrixUser, int64, uint](repo, q, &mu.Age)
		if err != nil {
			t.Fatalf("Sum 空表: err=%v（NULL 处理失败）", err)
		}
		if c.query != 1 || c.row != 0 {
			t.Fatalf("Sum 空表: query=%d row=%d, 期望 1/0", c.query, c.row)
		}
		if sum != 0 {
			t.Fatalf("Sum 空表=%d, 期望零值 0", sum)
		}
	})
}
```

- [ ] **Step 2：运行 — FindAs/FindOneAs 用例应失败（编译失败 `undefined: FindAs`），Sum 用例应通过（aggregate 已在 S3 修复）**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestFindAs_CallbackChainMatrix -v ./...
```

期望：编译失败 `undefined: FindAs` / `undefined: FindOneAs`

- [ ] **Step 3：commit RED**

```bash
git add D:/projects/gplus/find_as_test.go
git commit -m "test: callback 触发矩阵 RED（FindAs/FindOneAs/Sum 修复证明）"
```

**模型分级**：sonnet（多场景测试，需要细致）

**潜在坑**：
- `NewRepository[uint, matrixUser](db)` — 第二个类型参数是模型类型不是表名
- `NewQuery[matrixUser]` 返回 `(*Query[T], *T)`，第二个返回值是注册的字段指针实例

---

### S5：FindAs / FindAsTx 实现（GREEN）

**Files:**
- Modify: `D:/projects/gplus/find_as.go`

**目的**：实现 spec §3.2 的 `FindAs` / `FindAsTx`，让 S4 矩阵中 `FindAs_有数据` 用例 GREEN。

- [ ] **Step 1：在 find_as.go 追加 FindAs / FindAsTx**

```go
// FindAs 投影查询（多行）。dest 必须是 *[]Element 切片指针。
//
// 走 GORM Query callback chain，下游挂在 Query chain 上的隔离/审计 callback 会触发。
//
// 【迁移提示】若现有代码用 q.ToDB(db).Model(&T{}).Scan(&rows) / .Rows() / .Row()，
// 必须改用 gplus.FindAs。前者绕过 Query callback chain，会导致下游隔离/审计 callback
// 不触发，可能引发跨租户数据泄露 / 审计日志缺失（详见 README "已知陷阱"）。
//
// 【副作用】调用 FindAs 后 q.conditions 会被永久追加 DataRule 条件
// （dataRuleApplied 保护幂等），q 不应再跨不同 ctx 复用。与 List/Sum 等行为一致。
//
// 【调用形态】Go 1.18+ 类型推导后无需写类型参数：
//
//	var rows []UserVO
//	err := gplus.FindAs(repo, q, &rows)
func FindAs[T any, Dest any, D comparable](
	r *Repository[D, T], q *Query[T], dest *[]Dest,
) error {
	return FindAsTx[T, Dest, D](r, q, dest, nil)
}

// FindAsTx 支持事务的 FindAs。tx 为 nil 时与 FindAs 等价。
func FindAsTx[T any, Dest any, D comparable](
	r *Repository[D, T], q *Query[T], dest *[]Dest, tx *gorm.DB,
) error {
	if q == nil {
		return ErrQueryNil
	}
	if err := q.GetError(); err != nil {
		return err
	}
	if err := q.DataRuleBuilder().GetError(); err != nil {
		return err
	}
	return r.dbResolver(q.Context(), tx).
		Model(new(T)).Scopes(q.BuildQuery()).Find(dest).Error
}
```

- [ ] **Step 2：补 import — `gorm.io/gorm` 应已通过 aggregateWrap tag 间接需要，但 FindAsTx 直接引用 `*gorm.DB`，必须显式 import**

```go
// 文件顶部 import 段
import (
	"errors"

	"gorm.io/gorm"
)
```

- [ ] **Step 3：运行 callback 矩阵 — `FindAs_有数据` 应 PASS**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestFindAs_CallbackChainMatrix/FindAs_有数据 -v ./...
```

期望：PASS

- [ ] **Step 4：commit GREEN**

```bash
git add D:/projects/gplus/find_as.go
git commit -m "feat: FindAs / FindAsTx — Query-chain-safe 投影查询（多行）"
```

**模型分级**：sonnet（核心 API 实现 + godoc 模板）

**潜在坑**：
- `r.dbResolver(q.Context(), tx)` 同包私有方法可调用
- `q.BuildQuery()` 返回 `func(*gorm.DB) *gorm.DB`，配合 `.Scopes(...)` 使用
- `Model(new(T))` 必须保留 — 不要省略

---

### S6：FindOneAs / FindOneAsTx 实现（含 ErrFindOneAsConflict 防御，GREEN）

**Files:**
- Modify: `D:/projects/gplus/find_as.go`

**目的**：实现 spec §3.2 的 `FindOneAs` / `FindOneAsTx`，含 Limit/Offset 编程式防御。

- [ ] **Step 1：在 find_as.go 追加 FindOneAs / FindOneAsTx**

```go
// FindOneAs 投影查询（单行）。dest 是 *Element。
//
// 无匹配时返回 gorm.ErrRecordNotFound（与 GetById 既有语义一致）。
//
// 走 GORM Query callback chain，下游挂在 Query chain 上的隔离/审计 callback 会触发。
//
// 【迁移提示】若现有代码用 q.ToDB(db).Model(&T{}).Limit(1).Scan(&one)，必须改用
// gplus.FindOneAs。前者绕过 Query chain，可能引发跨租户数据泄露 / 审计日志缺失。
//
// 【约束】FindOneAs 不可与 q.Limit() / q.Page() 组合 —— 内部 First 会追加 LIMIT 1，
// 与已有 LIMIT 叠加部分 DB 行为未定义。组合调用会立即返回 ErrFindOneAsConflict。
//
// 【实测确认】GORM v1.31.x First(dest) 不会用 dest 的 schema 覆盖已设置的
// Model(new(T))；下游 isolation callback 拿到的 Schema.Table 仍为 T 表名。
// （由 TestGORMCallbackBehaviorProbe 永久守护）
func FindOneAs[T any, Dest any, D comparable](
	r *Repository[D, T], q *Query[T], dest *Dest,
) error {
	return FindOneAsTx[T, Dest, D](r, q, dest, nil)
}

// FindOneAsTx 支持事务的 FindOneAs。
func FindOneAsTx[T any, Dest any, D comparable](
	r *Repository[D, T], q *Query[T], dest *Dest, tx *gorm.DB,
) error {
	if q == nil {
		return ErrQueryNil
	}
	// 编程式防御：同 q 已设 limit/offset 时拒绝
	if q.ScopeBuilder.limit > 0 || q.ScopeBuilder.offset > 0 {
		return ErrFindOneAsConflict
	}
	if err := q.GetError(); err != nil {
		return err
	}
	if err := q.DataRuleBuilder().GetError(); err != nil {
		return err
	}
	return r.dbResolver(q.Context(), tx).
		Model(new(T)).Scopes(q.BuildQuery()).First(dest).Error
}
```

- [ ] **Step 2：运行 callback 矩阵 — FindOneAs 用例应 PASS**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestFindAs_CallbackChainMatrix -v ./...
```

期望：全部 6 个 sub-test PASS（包括 Sum 的 2 个）

- [ ] **Step 3：commit GREEN**

```bash
git add D:/projects/gplus/find_as.go
git commit -m "feat: FindOneAs / FindOneAsTx + ErrFindOneAsConflict 编程式防御"
```

**模型分级**：sonnet（防御逻辑 + godoc）

**潜在坑**：
- `q.ScopeBuilder.limit` 是 unexported 字段，同包可直接访问 — `Query[T]` 内嵌 `ScopeBuilder`，所以也可写 `q.limit`（Go promoted field）。**保持显式 `q.ScopeBuilder.limit` 更清晰**
- 防御要在 `q.GetError()` 之前 — 让用户尽早知道 API 误用

---

### S7：投影正确性 table-driven（Spec §4.3）

**Files:**
- Modify: `D:/projects/gplus/find_as_test.go`

**目的**：覆盖 spec §4.3 列举的投影场景：JOIN / SELECT 子集 / WHERE / ORDER / LIMIT / Distinct / DataRule。

- [ ] **Step 1：写 TestFindAs_ProjectionCorrectness**

```go
// TestFindAs_ProjectionCorrectness 验证 FindAs 在各种 Query 设置下投影结果正确。
func TestFindAs_ProjectionCorrectness(t *testing.T) {
	type projUser struct {
		ID     uint `gorm:"primarykey"`
		Name   string
		Age    int
		DeptID uint
	}
	type projDept struct {
		ID   uint `gorm:"primarykey"`
		Name string
	}
	type userVO struct {
		Name     string
		DeptName string
	}

	openDB := func(t *testing.T) (*gorm.DB, *Repository[uint, projUser]) {
		t.Helper()
		db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		_ = db.AutoMigrate(&projUser{}, &projDept{})
		db.Create(&projDept{ID: 1, Name: "Eng"})
		db.Create(&projDept{ID: 2, Name: "Sales"})
		db.Create(&projUser{Name: "alice", Age: 20, DeptID: 1})
		db.Create(&projUser{Name: "bob", Age: 30, DeptID: 1})
		db.Create(&projUser{Name: "carol", Age: 25, DeptID: 2})
		return db, NewRepository[uint, projUser](db)
	}

	t.Run("LEFT_JOIN_+_alias_映射", func(t *testing.T) {
		db, repo := openDB(t)
		_ = db
		q, _ := NewQuery[projUser](context.Background())
		q.LeftJoin("proj_depts", "proj_users.dept_id = proj_depts.id").
			Select("proj_users.name AS name", "proj_depts.name AS dept_name").
			Order("proj_users.id ASC")
		var rows []userVO
		if err := FindAs(repo, q, &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 3 {
			t.Fatalf("len=%d, 期望 3", len(rows))
		}
		if rows[0].Name != "alice" || rows[0].DeptName != "Eng" {
			t.Fatalf("rows[0]=%+v", rows[0])
		}
		if rows[2].Name != "carol" || rows[2].DeptName != "Sales" {
			t.Fatalf("rows[2]=%+v", rows[2])
		}
	})

	t.Run("WHERE_+_LIMIT_透传", func(t *testing.T) {
		_, repo := openDB(t)
		q, mu := NewQuery[projUser](context.Background())
		q.Gt(&mu.Age, 20).Order("id ASC").Limit(1)
		var rows []userVO
		if err := FindAs(repo, q, &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Name != "bob" {
			t.Fatalf("rows=%+v, 期望 [bob]", rows)
		}
	})

	t.Run("Distinct_+_FindAs", func(t *testing.T) {
		_, repo := openDB(t)
		q, mu := NewQuery[projUser](context.Background())
		q.Distinct(&mu.DeptID).Order("dept_id ASC")
		type deptIDVO struct {
			DeptID uint
		}
		var rows []deptIDVO
		if err := FindAs(repo, q, &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("Distinct 后 len=%d, 期望 2", len(rows))
		}
	})

	t.Run("DataRule_透传_builder_加_WHERE", func(t *testing.T) {
		_, repo := openDB(t)
		ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{
			{Column: "dept_id", Condition: "=", Value: uint(1)},
		})
		q, _ := NewQuery[projUser](ctx)
		var rows []userVO
		if err := FindAs(repo, q, &rows); err != nil {
			t.Fatal(err)
		}
		// DataRule 限定 dept_id=1，应只返回 alice / bob
		if len(rows) != 2 {
			t.Fatalf("DataRule 后 len=%d, 期望 2", len(rows))
		}
	})

	t.Run("FindOneAs_单行_alias", func(t *testing.T) {
		db, repo := openDB(t)
		_ = db
		q, mu := NewQuery[projUser](context.Background())
		q.LeftJoin("proj_depts", "proj_users.dept_id = proj_depts.id").
			Select("proj_users.name AS name", "proj_depts.name AS dept_name").
			Eq(&mu.Name, "carol")
		var one userVO
		if err := FindOneAs(repo, q, &one); err != nil {
			t.Fatal(err)
		}
		if one.Name != "carol" || one.DeptName != "Sales" {
			t.Fatalf("one=%+v", one)
		}
	})
}
```

- [ ] **Step 2：运行 — 全部 PASS**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestFindAs_ProjectionCorrectness -v ./...
```

期望：5 个 sub-test PASS

- [ ] **Step 3：commit**

```bash
git add D:/projects/gplus/find_as_test.go
git commit -m "test: FindAs/FindOneAs 投影正确性（JOIN/Distinct/DataRule/alias）"
```

**模型分级**：sonnet（多场景断言）

**潜在坑**：
- 表名 GORM 默认蛇形复数：`projUser` → `proj_users`，`projDept` → `proj_depts`
- LEFT JOIN 字符串中表名要用蛇形复数（`proj_depts`），不是 Go 类型名
- DataRule.Column `"dept_id"` 在单表场景无歧义；附录 D 提到的 JOIN 二义性问题在 DataRule 用例里未触发（DataRule 用例无 JOIN）

---

### S8：边界 + DataRule + JOIN 复合场景（Spec §4.4 + §4.3 复合用例）

**Files:**
- Modify: `D:/projects/gplus/find_as_test.go`

- [ ] **Step 1：写 TestFindAs_Boundary**

```go
// TestFindAs_Boundary 验证错误路径与防御逻辑。
func TestFindAs_Boundary(t *testing.T) {
	type bUser struct {
		ID   uint `gorm:"primarykey"`
		Name string
		Age  int
	}
	type bVO struct {
		Name string
	}

	openDB := func(t *testing.T) *Repository[uint, bUser] {
		t.Helper()
		db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		_ = db.AutoMigrate(&bUser{})
		db.Create(&bUser{Name: "alice", Age: 20})
		return NewRepository[uint, bUser](db)
	}

	t.Run("q_为_nil_返回_ErrQueryNil", func(t *testing.T) {
		repo := openDB(t)
		var rows []bVO
		if err := FindAs[bUser, bVO, uint](repo, nil, &rows); err != ErrQueryNil {
			t.Fatalf("err=%v, 期望 ErrQueryNil", err)
		}
		var one bVO
		if err := FindOneAs[bUser, bVO, uint](repo, nil, &one); err != ErrQueryNil {
			t.Fatalf("FindOneAs err=%v, 期望 ErrQueryNil", err)
		}
	})

	t.Run("FindOneAs_+_Limit_返回_ErrFindOneAsConflict", func(t *testing.T) {
		repo := openDB(t)
		q, _ := NewQuery[bUser](context.Background())
		q.Limit(5)
		var one bVO
		if err := FindOneAs(repo, q, &one); err != ErrFindOneAsConflict {
			t.Fatalf("err=%v, 期望 ErrFindOneAsConflict", err)
		}
	})

	t.Run("FindOneAs_+_Page_返回_ErrFindOneAsConflict", func(t *testing.T) {
		repo := openDB(t)
		q, _ := NewQuery[bUser](context.Background())
		q.Page(2, 10) // 内部设 limit + offset
		var one bVO
		if err := FindOneAs(repo, q, &one); err != ErrFindOneAsConflict {
			t.Fatalf("err=%v, 期望 ErrFindOneAsConflict", err)
		}
	})

	t.Run("FindOneAs_无匹配_返回_ErrRecordNotFound", func(t *testing.T) {
		repo := openDB(t)
		q, mu := NewQuery[bUser](context.Background())
		q.Eq(&mu.Name, "nobody")
		var one bVO
		err := FindOneAs(repo, q, &one)
		if err == nil {
			t.Fatal("期望 ErrRecordNotFound, 实际 nil")
		}
	})

	t.Run("dest_nil_切片_覆盖写入", func(t *testing.T) {
		repo := openDB(t)
		q, _ := NewQuery[bUser](context.Background())
		var rows []bVO // nil 切片
		if err := FindAs(repo, q, &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("len=%d, 期望 1", len(rows))
		}
	})

	t.Run("同一_q_两次_FindAs_DataRule_幂等", func(t *testing.T) {
		// 验证 dataRuleApplied 幂等保护
		ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{
			{Column: "age", Condition: ">=", Value: 18},
		})
		repo := openDB(t)
		q, _ := NewQuery[bUser](ctx)
		var rows1, rows2 []bVO
		_ = FindAs(repo, q, &rows1)
		_ = FindAs(repo, q, &rows2)
		if len(rows1) != len(rows2) {
			t.Fatalf("两次结果不一致: %d vs %d", len(rows1), len(rows2))
		}
	})

	t.Run("FindAs_+_DataRule_+_LEFT_JOIN_复合", func(t *testing.T) {
		// 复合场景：DataRule WHERE + JOIN ON 不互染
		type joinUser struct {
			ID     uint `gorm:"primarykey"`
			Name   string
			DeptID uint
		}
		type joinDept struct {
			ID   uint `gorm:"primarykey"`
			Name string
		}
		db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		_ = db.AutoMigrate(&joinUser{}, &joinDept{})
		db.Create(&joinDept{ID: 1, Name: "Eng"})
		db.Create(&joinDept{ID: 2, Name: "Sales"})
		db.Create(&joinUser{Name: "alice", DeptID: 1})
		db.Create(&joinUser{Name: "bob", DeptID: 2})

		ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{
			// 用 table.col 形式避免 JOIN 后二义性
			{Column: "join_users.dept_id", Condition: "=", Value: uint(1)},
		})
		repo := NewRepository[uint, joinUser](db)
		q, _ := NewQuery[joinUser](ctx)
		q.LeftJoin("join_depts", "join_users.dept_id = join_depts.id").
			Select("join_users.name AS name", "join_depts.name AS dept_name")

		type vo struct {
			Name     string
			DeptName string
		}
		var rows []vo
		if err := FindAs(repo, q, &rows); err != nil {
			t.Fatal(err)
		}
		// DataRule 限定 dept_id=1 → 只 alice；JOIN 拿 dept name "Eng"
		if len(rows) != 1 || rows[0].Name != "alice" || rows[0].DeptName != "Eng" {
			t.Fatalf("rows=%+v", rows)
		}
	})
}
```

- [ ] **Step 2：运行 — 全部 PASS**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestFindAs_Boundary -v ./...
```

期望：7 个 sub-test PASS

- [ ] **Step 3：commit**

```bash
git add D:/projects/gplus/find_as_test.go
git commit -m "test: FindAs/FindOneAs 边界 + DataRule×JOIN 复合场景

含：
- ErrQueryNil / ErrFindOneAsConflict (Limit/Page) / ErrRecordNotFound 路径
- nil 切片覆盖写入语义
- 同 q 两次 FindAs 的 DataRule 幂等
- DataRule WHERE + LEFT JOIN ON 互不污染"
```

**模型分级**：sonnet（边界场景，需要细致）

**潜在坑**：
- `Page(2, 10)` 行为：实际看 `Query[T].Page` 实现 — 通常是 `limit = pageSize; offset = (page-1) * pageSize`
- DataRule.Column `"join_users.dept_id"` 必须用表前缀避免 JOIN 二义性（spec 附录 D 已警告）

---

### S9：godoc 补全（并行 S10/S11）

**Files:**
- Modify: `D:/projects/gplus/find_as.go`（aggregateWrap 注释已在 S2 写好）
- Modify: `D:/projects/gplus/repository.go`（Sum/Max/Min/Avg/Pluck 加 R 支持类型说明）

**目的**：spec §5.3 godoc 要求。

- [ ] **Step 1：检查 find_as.go 中 4 个新函数的 godoc 是否完整**（应已在 S5/S6 完成，本步只复核）

- [ ] **Step 2：在 repository.go Sum/Max/Min/Avg/Pluck 函数 godoc 段加 R 支持类型说明**

例如 `Sum` 函数前注释加：

```go
// Sum 对指定列求和，R 为数值类型。
//
// 【R 支持类型】int64 / float64 / string 安全；MySQL DECIMAL 列建议 R=float64
// （database/sql 通过 strconv 路径正确转换）；time.Time / sql.NullX 作为 R 无意义。
//
// 【callback chain】走 Query callback chain（v0.7.0 起），下游 isolation/审计
// callback 会触发。
//
// 【col 字符串警告】传字符串列名时 gplus 不做白名单校验，禁止将用户输入直接传入 col。
func Sum[T any, R any, D comparable](r *Repository[D, T], q *Query[T], col any) (R, error) {
```

Max/Min/Avg/Pluck 同样模板。

- [ ] **Step 3：commit**

```bash
git add D:/projects/gplus/find_as.go D:/projects/gplus/repository.go
git commit -m "docs: Sum/Max/Min/Avg/Pluck godoc 加 R 支持类型 + callback chain + col 警告"
```

**模型分级**：haiku（机械文本）

---

### S10：CHANGELOG.md（并行 S9/S11）

**Files:**
- Modify: `D:/projects/gplus/CHANGELOG.md`

- [ ] **Step 1：在 CHANGELOG 顶部 `# Changelog` 标题后插入 v0.7.0 条目**

直接复用 spec §5.1 给出的 markdown 模板（含强迁移建议 + 两条互补 grep + 性能基线 + GORM 版本锁定 + 不在本期范围）。`<发版日>` 填 commit 当天日期 `2026-05-01`。

注意 markdown 转义：spec 模板中的 `\`\`\`bash ... \`\`\`` 在 CHANGELOG 中是真的代码块（不需要转义）。

- [ ] **Step 2：commit**

```bash
git add D:/projects/gplus/CHANGELOG.md
git commit -m "docs: CHANGELOG 增 v0.7.0 条目（投影 API + aggregate 修复 + 行为约束）"
```

**模型分级**：haiku（文本同步）

---

### S11：README.md「已知陷阱」段（并行 S9/S10）

**Files:**
- Modify: `D:/projects/gplus/README.md`

- [ ] **Step 1：先 Read README.md 找「已知陷阱」/「Known Issues」/「Caveats」段**

如果不存在该段，在合适位置（Repository API 章节之后）新建。

- [ ] **Step 2：直接复用 spec §5.2 给出的 markdown 模板**

含 4 段：
1. `q.ToDB(db).Scan/Row/Rows` 跨租户后果具象化 + 必须改用映射表
2. RawQuery/RawScan fail-secure 警告
3. DataRule + JOIN 列二义性必须 `table.col`
4. col 字符串无白名单警告

- [ ] **Step 3：在 Repository API 章节加 FindAs/FindOneAs 调用示例**

直接复用 spec §3.4 的"调用形态"代码块。

- [ ] **Step 4：commit**

```bash
git add D:/projects/gplus/README.md
git commit -m "docs: README 增 FindAs/FindOneAs 示例 + 已知陷阱段（跨租户/排查/JOIN/col）"
```

**模型分级**：haiku（文本同步）

---

### S12：final 验证 + 覆盖率检查 + commit 复核

**目的**：spec 验收清单全勾，覆盖率达标，commit 历史干净。

- [ ] **Step 1：全量测试 + race**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -race ./...
```

期望：全部 PASS（含已有 + 新增 ~600 行测试）

- [ ] **Step 2：覆盖率检查**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -coverprofile=coverage.out ./...
D:/Environment/golang/go1.21.11/bin/go.exe tool cover -func=coverage.out | tail -20
```

期望：
- `find_as.go` 各函数行覆盖率 ≥ 94%
- 整体 `total:` 行 ≥ 93.5%

- [ ] **Step 3：go vet**

```bash
D:/Environment/golang/go1.21.11/bin/go.exe vet ./...
```

期望：无新增警告

- [ ] **Step 4：commit 历史复核**

```bash
git log --oneline -15
```

期望：S1-S11 各 1-2 个 commit，message 中文 + 无 Co-Authored-By trailer

- [ ] **Step 5：spec 验收清单逐项打勾**

打开 `docs/superpowers/specs/2026-05-01-scan-callback-fix-design.md` 第 §6 验收清单，每条 `[ ]` 改为 `[x]`，commit：

```bash
git add docs/superpowers/specs/2026-05-01-scan-callback-fix-design.md
git commit -m "docs(spec): v0.7.0 验收清单全部完成"
```

- [ ] **Step 6：留 commit 不 push** — 用户审完手动 push 并发版

**模型分级**：sonnet（决定是否绿；多 commit 复核需要细致）

**潜在坑**：
- 覆盖率分母变化 — 新增 `find_as.go` ~250 行 + 4 个新函数，整体分子分母同涨；用 `tool cover -func` 看每个函数行数
- 如有覆盖率不达标的函数，回到对应任务补测试，不要硬塞凑数测试

---

## Self-Review

（writing-plans skill 自审 — 由 plan 作者完成）

### 1. spec 覆盖检查

| Spec 节 | 对应任务 | 状态 |
|---|---|---|
| §3.1 API 形态 4 个函数 | S5 (FindAs) + S6 (FindOneAs) | ✓ |
| §3.2 内部实现 ErrFindOneAsConflict | S2 (定义) + S6 (用) | ✓ |
| §3.3 aggregate wrapper struct + alias | S2 (定义) + S3 (用) | ✓ |
| §3.4 调用形态文档 | S11 README | ✓ |
| §3.5 文件组织 | S1-S3 (新建/修改) | ✓ |
| §4.1 永久 probe 6 条 | S1 | ✓ |
| §4.2 callback 矩阵 | S4 (RED) + S5/S6 (GREEN) | ✓ |
| §4.3 投影正确性 | S7 | ✓ |
| §4.4 边界 | S8 | ✓ |
| §5.1 CHANGELOG | S10 | ✓ |
| §5.2 README | S11 | ✓ |
| §5.3 godoc | S5/S6 (4 函数) + S9 (Sum/Max/Min/Avg/Pluck) | ✓ |
| §6 验收清单 | S12 | ✓ |
| 附录 D D1 已知问题 | spec 自身已记录，无任务对应（v0.7.1） | N/A |

**结论**：spec 全覆盖，无遗漏。

### 2. 占位符扫描

无 "TBD" / "TODO" / "fill in" / "similar to" — 每步代码块完整。

### 3. 类型一致性

- `aggregateWrap[R]` / `aggregateAlias` / `ErrFindOneAsConflict` 定义在 S2，S3/S5/S6 复用 — 名字一致 ✓
- `FindAs/FindOneAs/FindAsTx/FindOneAsTx` 签名在 S5/S6 严格按 spec §3.1 ✓
- 测试文件 helper 命名（`probe` / `setupProbe` / `openDB`）在 S1/S4/S7/S8 间一致 ✓

---

## 执行交接

Plan 已写完。两种执行选项：

**1. Subagent-Driven（推荐）** — `superpowers:subagent-driven-development`
- 每个任务派 fresh subagent，task 之间 review
- 按 CLAUDE.md "Subagent 执行策略"分模型：
  - S1/S3/S4/S5/S6/S7/S8/S12 → sonnet
  - S2/S9/S10/S11 → haiku
  - 全部完成后 final review → opus
- 优势：context 隔离、并行潜力大、单 task 失败不污染后续

**2. Inline Execution** — `superpowers:executing-plans`
- 在当前 session 执行，checkpoint review
- 优势：零 subagent 启动开销、context 共享

**推荐 Subagent-Driven**。原因：
- 任务总数 12 个，单线程会冗长
- S9/S10/S11 三者天然并行
- final review 需要 opus 跨方法语义审，单独派更经济
