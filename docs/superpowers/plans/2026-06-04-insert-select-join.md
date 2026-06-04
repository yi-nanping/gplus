# InsertSelect scenario 2 自连接（Round 3b）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用 3 个验证型 AC 锁死「scenario 2 自连接 `INSERT...SELECT...JOIN` 已被 Round 2 + Round 3a 组合零改动解锁」防回归，并把软删除前缀限制 + 出路、三库方言风险文档化。

**Architecture:** **零实现代码改动**——不改 `repository.go` 的 `InsertSelect`/`InsertSelectTx`（探针证明现有守卫链对自连接形态零改动可用）。纯新增测试文件 `insert_select_join_test.go`（含 `ClosureSD` 软删除模型 + 3 个 AC 测试）+ 更新主 spec 的 Round 3 占位段。

**Tech Stack:** Go 1.24（本机 `D:/Environment/golang/go1.21.11/bin/go.exe`），GORM + glebarez/sqlite in-memory，标准库 `testing` + `strings` + `gorm.io/gorm`（`DeletedAt`）。复用 Round 2 的 `InsertSelect`、Round 3a 的 `NewQueryAs`/`As`/`CrossJoinAs`/`SelectRaw`/`WhereRaw`/`Unscoped`。

**Spec:** `docs/superpowers/specs/2026-06-04-insert-select-join-design.md`（3 条 AC，两轮 3 视角审计 + 探针实测定稿）

> **本 round 是验证型**：3 个 AC 锁死的是「既有行为是否符合探针实测」，**测试预期写完即绿**（核心已在 Round 2/3a 实现）。这不是「先红后绿驱动新实现」的常规 TDD——若某测试写完不绿，说明发现了与探针实测不符的真实问题，**必须停下报告（不得改断言掩盖）**，而非补实现。

**已验证的真实签名（实现时照此调用，勿臆测）：**
- `func (r *Repository[D, T]) NewQueryAs(ctx context.Context, alias string) (*Query[T], *T)`（repository.go:143，可推断 T）
- `func As[X any](q AnyQuery, alias string) *X`（alias.go:154）→ `As[Closure](q, "sub")` 返回 `*Closure`
- `func (q *Query[T]) CrossJoinAs(alias any) *Query[T]`（query.go:1130）→ 传 `sub`（`*Closure`）
- `func (q *Query[T]) Unscoped() *Query[T]`（query.go:834）
- `func InsertSelect[T any, S any, D comparable](r *Repository[D, T], ctx context.Context, targetCols []any, src *Query[S]) (int64, error)`（repository.go:1106，类型全可推断）

**通用命令（bash 用完整 go 路径）：**
```bash
# 跑单测
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestInsertSelectJoin_xxx -v ./...
# 全量
D:/Environment/golang/go1.21.11/bin/go.exe test ./...
# 覆盖率
D:/Environment/golang/go1.21.11/bin/go.exe test -coverprofile=coverage.out ./... && D:/Environment/golang/go1.21.11/bin/go.exe tool cover -func=coverage.out | tail -1
```

---

## File Structure

| 文件 | 职责 | 变更 |
|---|---|---|
| `insert_select_join_test.go` | scenario 2 自连接验证测试 | 新建：`ClosureSD` 软删除模型 + AC-1/2/3 三个测试函数 |
| `docs/superpowers/specs/2026-06-04-insert-select-design.md` | Round 2 主 spec | 修改：Round 3 占位段（line 8、137）→ 指向 Round 3b 已交付 |

复用（不改）：`insert_select_test.go` 的 `Closure`/`assertClosureCount`、`repo_test.go` 的 `setupTestDB`。

---

## Task 1: 测试骨架 + ClosureSD 模型 + AC-1（无软删除自连接正路）

**AC:** AC-1

**Files:**
- Create: `insert_select_join_test.go`

- [ ] **Step 1: 写 AC-1 测试（新建文件含 package/import + ClosureSD 模型 + AC-1）**

新建 `insert_select_join_test.go`：

```go
package gplus

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

// ClosureSD 带软删除的闭包模型（scenario 2 软删除分支测试用，AC-2/AC-3）。
type ClosureSD struct {
	ID           int64          `gorm:"column:id;primaryKey;autoIncrement"`
	AncestorID   uint           `gorm:"column:ancestor_id"`
	DescendantID uint           `gorm:"column:descendant_id"`
	Depth        uint           `gorm:"column:depth"`
	DeletedAt    gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (ClosureSD) TableName() string { return "closure_sd" }

// AC-1：无软删除 closure 自连接 InsertSelect 真插入正确（P1 正路防回归）。
func TestInsertSelectJoin_inserts_row_when_no_softdelete(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	seeds := []Closure{{AncestorID: 1, DescendantID: 5, Depth: 0}, {AncestorID: 5, DescendantID: 7, Depth: 0}}
	for i := range seeds {
		if err := repo.Save(ctx, &seeds[i]); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	q, ext := repo.NewQueryAs(ctx, "ext")
	sub := As[Closure](q, "sub")
	q.CrossJoinAs(sub).WhereRaw("sub.ancestor_id = ?", 5).Eq(&ext.DescendantID, 5)
	q.SelectRaw("ext.ancestor_id").
		SelectRaw("sub.descendant_id").
		SelectRaw("ext.depth + sub.depth + 1")

	affected, err := InsertSelect(repo, ctx, []any{"ancestor_id", "descendant_id", "depth"}, q)
	if err != nil {
		t.Fatalf("InsertSelect 应成功，实际: %v", err)
	}
	if affected != 1 {
		t.Fatalf("affected 期望 1，实际 %d", affected)
	}
	// 逐字段断言新增行 {1,7,1}——禁止只数行数：列错位/错误投影仍满足行数=3（红队实测 {7,1,1} 假绿）。
	var got []Closure
	db.Order("id").Find(&got)
	if len(got) != 3 {
		t.Fatalf("总行数期望 3，实际 %d", len(got))
	}
	nw := got[2]
	if nw.AncestorID != 1 || nw.DescendantID != 7 || nw.Depth != 1 {
		t.Fatalf("新增行期望 {1,7,1}，实际 {%d,%d,%d}", nw.AncestorID, nw.DescendantID, nw.Depth)
	}
}
```

> 注：Task 1 的 import 块**不含** `strings`（AC-1 未用），Task 2 追加 AC-2 时再加，避免 `imported and not used` 编译错误。

- [ ] **Step 2: 跑 AC-1，预期 PASS（零改动可用）**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestInsertSelectJoin_inserts_row_when_no_softdelete -v ./...`
Expected: PASS。生成 SQL 形如 `INSERT INTO ... SELECT "ext"."ancestor_id","sub"."descendant_id",ext.depth + sub.depth + 1 FROM "closure" AS "ext" CROSS JOIN closure AS sub WHERE sub.ancestor_id = 5 AND "ext"."descendant_id" = 5`，新增行 `{1,7,1}`。
**若不绿**：说明 scenario 2 正路出现与 P1 探针不符的回归，停下报告（勿改断言）。

- [ ] **Step 3: Commit**

```bash
git add insert_select_join_test.go
git commit -m "test: scenario 2 无软删除自连接 InsertSelect 正路（Round 3b AC-1）"
```

---

## Task 2: AC-2（软删除模型自连接裸列报错，限制锁）

**AC:** AC-2

**Files:**
- Modify: `insert_select_join_test.go`（追加 AC-2；删除 Task 1 的 `strings` 哨兵）

- [ ] **Step 1: import 块加 `"strings"`，追加 AC-2 测试**

先在文件顶部 import 块加 `"strings"`（AC-2 用到），分组为 stdlib（`context`/`strings`/`testing`）→ 空行 → 第三方（`gorm.io/gorm`）。追加：

```go
// AC-2：软删除模型自连接（不 Unscoped）报裸表名错误，零副作用（限制锁）。
// GORM 软删除条件用裸表名 closure_sd.deleted_at，FROM 只有 ext/sub 别名 → 报错（与 Round 3a AC-11 同源）。
func TestInsertSelectJoin_softdelete_bare_column_fails(t *testing.T) {
	repo, db := setupTestDB[ClosureSD](t)
	ctx := context.Background()
	seeds := []ClosureSD{{AncestorID: 1, DescendantID: 5, Depth: 0}, {AncestorID: 5, DescendantID: 7, Depth: 0}}
	for i := range seeds {
		if err := repo.Save(ctx, &seeds[i]); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	q, ext := repo.NewQueryAs(ctx, "ext")
	sub := As[ClosureSD](q, "sub")
	q.CrossJoinAs(sub).WhereRaw("sub.ancestor_id = ?", 5).Eq(&ext.DescendantID, 5)
	q.SelectRaw("ext.ancestor_id").
		SelectRaw("sub.descendant_id").
		SelectRaw("ext.depth + sub.depth + 1")

	_, err := InsertSelect(repo, ctx, []any{"ancestor_id", "descendant_id", "depth"}, q)
	if err == nil || !strings.Contains(err.Error(), "no such column") || !strings.Contains(err.Error(), "closure_sd.deleted_at") {
		t.Fatalf("期望报 no such column: closure_sd.deleted_at（GORM 软删除裸表名被别名遮蔽），实际: %v", err)
	}
	// 零副作用：closure_sd 未删行数仍 2（GORM Count 自动排除软删，本例无软删）。
	var n int64
	db.Model(&ClosureSD{}).Count(&n)
	if n != 2 {
		t.Fatalf("行数期望 2（无插入副作用），实际 %d", n)
	}
}
```

- [ ] **Step 2: 跑 AC-2，预期 PASS（限制真实存在）**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestInsertSelectJoin_softdelete_bare_column_fails -v ./...`
Expected: PASS。实际错误 `SQL logic error: no such column: closure_sd.deleted_at (1)`，行数仍 2。
**若不绿**（例如不报错或文本不同）：若是 SQLite 驱动文本漂移，按真实文本调整断言但保持「限制确实导致失败」语义；若根本不报错（限制消失），停下报告——这是重要发现。

- [ ] **Step 3: Commit**

```bash
git add insert_select_join_test.go
git commit -m "test: scenario 2 软删除裸列自连接报错限制锁（Round 3b AC-2）"
```

---

## Task 3: AC-3（Unscoped + 两侧别名前缀，已删行不被复制）

**AC:** AC-3

**Files:**
- Modify: `insert_select_join_test.go`（追加 AC-3）

- [ ] **Step 1: 追加 AC-3 测试**

追加（种子 4 行：2 正常候选 + ext/sub 各 1 条已删干扰，使两侧前缀都被真实验证）：

```go
// AC-3：软删除模型 Unscoped + 手动两侧别名前缀正路，已删行不被复制（出路锁）。
// 两侧各种 1 条已删干扰行：删 sub 前缀会多 {1,88,1}、删 ext 前缀会多 {2,7,1} → 两侧前缀均非死代码。
func TestInsertSelectJoin_unscoped_with_alias_prefix_excludes_deleted(t *testing.T) {
	repo, db := setupTestDB[ClosureSD](t)
	ctx := context.Background()
	// 正常候选（未删）
	normal := []ClosureSD{{AncestorID: 1, DescendantID: 5, Depth: 0}, {AncestorID: 5, DescendantID: 7, Depth: 0}}
	for i := range normal {
		if err := repo.Save(ctx, &normal[i]); err != nil {
			t.Fatalf("seed normal: %v", err)
		}
	}
	// 干扰行（Save 后软删）：sub 侧 5->88，ext 侧 2->5
	deleted := []ClosureSD{{AncestorID: 5, DescendantID: 88, Depth: 0}, {AncestorID: 2, DescendantID: 5, Depth: 0}}
	for i := range deleted {
		if err := repo.Save(ctx, &deleted[i]); err != nil {
			t.Fatalf("seed deleted: %v", err)
		}
		if err := db.Delete(&deleted[i]).Error; err != nil { // 软删除：deleted_at 置当前时间
			t.Fatalf("soft delete: %v", err)
		}
	}

	q, ext := repo.NewQueryAs(ctx, "ext")
	sub := As[ClosureSD](q, "sub")
	q.Unscoped().CrossJoinAs(sub).
		WhereRaw("sub.ancestor_id = ?", 5).
		Eq(&ext.DescendantID, 5).
		WhereRaw("ext.deleted_at IS NULL").
		WhereRaw("sub.deleted_at IS NULL")
	q.SelectRaw("ext.ancestor_id").
		SelectRaw("sub.descendant_id").
		SelectRaw("ext.depth + sub.depth + 1")

	affected, err := InsertSelect(repo, ctx, []any{"ancestor_id", "descendant_id", "depth"}, q)
	if err != nil {
		t.Fatalf("InsertSelect 应成功，实际: %v", err)
	}
	if affected != 1 {
		t.Fatalf("affected 期望 1（两侧前缀排除已删干扰），实际 %d", affected)
	}
	// 新增未删行恰为 {1,7,1}（按 id 升序，新插入 id 最大）。
	var live []ClosureSD
	db.Order("id").Find(&live) // GORM 自动排除软删行：原 2 正常 + 1 新增 = 3
	if len(live) != 3 {
		t.Fatalf("未删行期望 3，实际 %d", len(live))
	}
	nw := live[2]
	if nw.AncestorID != 1 || nw.DescendantID != 7 || nw.Depth != 1 {
		t.Fatalf("新增行期望 {1,7,1}，实际 {%d,%d,%d}", nw.AncestorID, nw.DescendantID, nw.Depth)
	}
	// 已删干扰行未被复制成新未删行：未删行中查无 desc=88 也查无 anc=2（GORM Count 自动排除软删）。
	var leaked int64
	db.Model(&ClosureSD{}).Where("descendant_id = 88 OR ancestor_id = 2").Count(&leaked)
	if leaked != 0 {
		t.Fatalf("已删干扰行不应被复制成未删行，泄漏 %d 条", leaked)
	}
}
```

- [ ] **Step 2: 跑 AC-3，预期 PASS（出路可用 + 语义正确）**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestInsertSelectJoin_unscoped_with_alias_prefix_excludes_deleted -v ./...`
Expected: PASS。affected=1，新增 `{1,7,1}`，leaked=0。
**若不绿**：若 affected=4（两侧前缀都没生效）或 leaked>0（已删行被复制），停下报告——说明 Unscoped/WhereRaw 别名前缀行为与探针不符。

- [ ] **Step 3: 验证两侧前缀都是「活代码」（一次性手验，不留测试）**

临时把 AC-3 的 `.WhereRaw("ext.deleted_at IS NULL")` 注释掉，跑测试，**应失败**（affected=2，多出 `{2,7,1}`）；恢复。再把 `.WhereRaw("sub.deleted_at IS NULL")` 注释掉，跑测试，**应失败**（affected=2，多出 `{1,88,1}`）；恢复。确认两侧前缀删任一都会红（非死代码），随后还原到两侧都加的状态。此步仅本地手验，不产生提交。

- [ ] **Step 4: Commit**

```bash
git add insert_select_join_test.go
git commit -m "test: scenario 2 Unscoped 两侧别名前缀出路锁，已删行不复制（Round 3b AC-3）"
```

---

## Task 4: 更新主 spec Round 3 占位段 + 全量回归 + 覆盖率门禁

**Files:**
- Modify: `docs/superpowers/specs/2026-06-04-insert-select-design.md`（line 8、137）

- [ ] **Step 1: 更新 Round 2 主 spec 的 Round 3 占位段**

先 `Read` 该文件确认 line 8 / line 137 现状（行号是约数）。

line 8 现为：
```
> - **Round 3（推迟）**：scenario 2 自连接 `INSERT ... SELECT ... JOIN`。推迟原因：实测 `NewQueryAs` 的主表别名在 `ToDB` 物化路径会丢失（`FROM \`test_users\`` 不带 `AS ext`，而 SELECT 引用 `"ext".col` → 真库报别名未定义），需先单独修 `ToDB` 主别名应用，属独立 gap，不与 Round 2 耦合。
```
改为：
```
> - **Round 3a（已合入）**：主别名 FROM 物化（解除 ToDB 主别名丢失阻塞）→ `2026-06-04-main-alias-from-design.md`。
> - **Round 3b（已交付）**：scenario 2 自连接 `INSERT ... SELECT ... JOIN`，方案 A 纯验证+文档 → `2026-06-04-insert-select-join-design.md`。探针实测确认 Round 2 + Round 3a 组合已零改动解锁该能力。
```

line 137 现为：
```
- **scenario 2 自连接 `INSERT...SELECT...JOIN`**：推迟 Round 3（主表别名在 ToDB 物化路径丢失，需单独修）。
```
改为：
```
- ~~**scenario 2 自连接 `INSERT...SELECT...JOIN`**：推迟 Round 3~~ → **已由 Round 3b 交付**（`2026-06-04-insert-select-join-design.md`，零改动验证 + 软删除/方言文档化）。
```

- [ ] **Step 2: 全量 TestInsertSelectJoin 测试**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestInsertSelectJoin -v ./...`
Expected: PASS（3 个测试函数全绿）。

- [ ] **Step 3: go vet 静态检查**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe vet ./...`
Expected: 无输出（无警告）。

- [ ] **Step 4: 全量回归**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test ./...`
Expected: ok，全部 PASS（既有 insert_select / main_alias / alias 等测试不回归）。

- [ ] **Step 5: 覆盖率门禁（不低于基线 95.0%）**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -coverprofile=coverage.out ./... && D:/Environment/golang/go1.21.11/bin/go.exe tool cover -func=coverage.out | tail -1`
Expected: total ≥ 95.0%（纯新增测试不应降覆盖率；本 round 零实现改动，覆盖率应持平或微升）。

- [ ] **Step 6: 清理临时文件 + 确认工作区干净**

```bash
rm -f coverage.out
git status --short   # 应只剩 spec 的修改待提交
```

- [ ] **Step 7: Commit**

```bash
git add docs/superpowers/specs/2026-06-04-insert-select-design.md
git commit -m "docs: 主 spec Round 3 占位段更新指向 Round 3b 已交付"
```

---

## Self-Review 检查（写计划后自查，已完成）

- **Spec coverage**：AC-1 → Task 1；AC-2 → Task 2；AC-3 → Task 3；已知限制/方言风险文档化 → spec 已含（本 plan 不重复，Task 4 更新主 spec 交叉引用）；测试组织「AC-1 强断言」「InsertSelectTx 正交」→ 已落入 Task 1 注释 + plan 验证型说明。全部 spec 要求有任务覆盖。
- **零改动一致性**：架构声明零实现改动，4 个 task 无一修改 `repository.go`/`builder.go`/`query.go`，仅新增测试 + 改文档，与 spec「显式排除 InsertSelect 实现改动」一致。
- **类型一致性**：`ClosureSD`（含 `DeletedAt gorm.DeletedAt`）在 Task 1 定义、Task 2/3 复用；`InsertSelect(repo, ctx, []any{...}, q)`、`As[ClosureSD]`、`repo.NewQueryAs`、`q.Unscoped()`、`q.CrossJoinAs(sub)` 全程一致，与「已验证真实签名」块核对无误。
- **无占位符**：每个 task 含完整测试代码 + 精确命令 + 期望输出 + 不绿时的处置指引。
- **验证型语义**：plan header 明确本 round 测试预期写完即绿（非先红后绿），不绿=发现真实问题须停下报告，杜绝「改断言掩盖」。
- **依赖顺序**：Task 1（骨架+模型+AC-1）→ Task 2（AC-2，删 strings 哨兵）→ Task 3（AC-3，依赖 ClosureSD）→ Task 4（文档+回归+覆盖率）。
