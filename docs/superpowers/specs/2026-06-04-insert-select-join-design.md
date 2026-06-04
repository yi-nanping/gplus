# InsertSelect scenario 2 自连接（Round 3b）Design

> **本特性拆轮交付（承接）：**
> - Round 1（已合入）：`SelectRaw(args)` 参数绑定 → `2026-06-04-selectraw-args-design.md`
> - Round 2（已合入）：`InsertSelect`/`InsertSelectTx` 本体，scenario 1（单表无 JOIN）→ `2026-06-04-insert-select-design.md`
> - Round 3a（已合入）：主别名 FROM 物化 → `2026-06-04-main-alias-from-design.md`
> - **Round 3b（本文档）**：scenario 2 自连接 `INSERT...SELECT...JOIN`，**方案 A：纯验证 + 文档**

## 背景与定性

下游 gvs-server 闭包表子树迁移（closure_repository.go）需要 `INSERT INTO closure SELECT ... FROM closure ext JOIN closure sub`。Round 2 把它列为推迟项，前提是「`NewQueryAs` 主别名在 `ToDB` 物化路径丢失」。**Round 3a 已修复该前提**，故本轮重新探针实测，结论推翻原设想：

**scenario 2 的「INSERT...SELECT...JOIN 能力」已被 Round 2 + Round 3a 组合零改动解锁**（无软删除时）。Round 3b 不实现新能力，而是：
1. 用验证 AC 锁死已解锁的正路，防回归；
2. 把探针发现的**软删除前缀限制**及其**出路**用 AC 锁死 + 文档化；
3. 把 CI 无法覆盖的**三库方言风险**显式标注，交下游真机验证。

## 探针实测结论（2026-06-04，已删 zz_probe_round3b）

SQLite（glebarez）真实执行：

- **P1（无软删除）✅ 零改动可用**：`InsertSelect` 自连接源 query 真实插入，`affected=1`，正确产生 `{1,7,1}`。生成 SQL：
  `INSERT INTO `closure` (`ancestor_id`,`descendant_id`,`depth`) SELECT "ext"."ancestor_id","sub"."descendant_id",ext.depth + sub.depth + 1 FROM "closure" AS "ext" CROSS JOIN closure AS sub WHERE sub.ancestor_id = 5 AND "ext"."descendant_id" = 5`
- **P2（软删除）❌ 失败**：`ClosureSD`（带 `gorm.DeletedAt`）自连接时，GORM 自动追加 `` `closure_sd`.`deleted_at` IS NULL `` 用**裸表名**，FROM 只有 `ext`/`sub` 别名 → `no such column: closure_sd.deleted_at`。与 Round 3a AC-11（First 路径裸主键被别名遮蔽）**同源**。
- **P4（软删除 + Unscoped）✅ 可绕过**：`q.Unscoped()` 跳过 GORM 软删除 callback，SQL 不含 `deleted_at`，`affected=1`。**但 Unscoped 会把已软删除行也纳入 SELECT 源**，需手动补带别名前缀的软删除条件以保持语义。
- **方言引号**：`getQuoteChar` 已覆盖 `dm`/`postgres`（双引号）、`mysql`/`tidb`（反引号）、`sqlserver`（方括号）。主别名物化在三库各用正确引号。

## Goal

锁死「无软删除 closure 自连接 `InsertSelect` 已可用」防回归，并把软删除前缀限制 + 出路、三库自插入方言风险显式文档化。**零实现代码改动。**

## Tech Stack

Go 1.24（本机 `D:/Environment/golang/go1.21.11/bin/go.exe`），GORM + glebarez/sqlite in-memory，标准库 `testing` + `strings` + `gorm.io/gorm`（`DeletedAt`）。复用 Round 2 的 `InsertSelect`、Round 3a 的 `NewQueryAs`/`As`/`CrossJoinAs`/`SelectRaw`/`WhereRaw`/`Unscoped`。

## Acceptance Criteria

每条 AC 1:1 对应一个测试函数；测试名描述 AC 行为。

### AC-1：无软删除 closure 自连接 InsertSelect 真插入正确（P1 正路锁，防回归）

- **输入**：`setupTestDB[Closure]`，种子闭包链 `{anc=1,desc=5,depth=0}`、`{anc=5,desc=7,depth=0}`。源 query：`NewQueryAs(ctx,"ext")` + `As[Closure](q,"sub")` + `q.CrossJoinAs(sub).WhereRaw("sub.ancestor_id = ?", 5).Eq(&ext.DescendantID, 5)` + `q.SelectRaw("ext.ancestor_id").SelectRaw("sub.descendant_id").SelectRaw("ext.depth + sub.depth + 1")`。`InsertSelect(repo, ctx, []any{"ancestor_id","descendant_id","depth"}, q)`。
- **可观测输出**：返回 `(1, nil)`；`closure` 表总行数 3；新增行恰为 `{anc=1, desc=7, depth=1}`（原 2 行不变）。

### AC-2：软删除模型自连接（不 Unscoped）报裸表名错误，零副作用（限制锁）

- **输入**：`setupTestDB[ClosureSD]`（`ClosureSD` 含 `DeletedAt gorm.DeletedAt`），种子 `{anc=1,desc=5,depth=0}`、`{anc=5,desc=7,depth=0}`。同 AC-1 形态的自连接源 query（**不调** `Unscoped()`）。`InsertSelect(repo, ctx, []any{"ancestor_id","descendant_id","depth"}, q)`。
- **可观测输出**：返回 `err != nil`，`err.Error()` 同时含 `"no such column"` 与 `"closure_sd.deleted_at"`；`closure_sd` 表行数仍为 2（无插入副作用）。

### AC-3：软删除模型 Unscoped + 手动别名前缀正路，已删行不被复制（出路锁）

- **输入**：`setupTestDB[ClosureSD]`，种子 `{anc=1,desc=5,depth=0}`、`{anc=5,desc=7,depth=0}`，再加 1 条 `{anc=5,desc=88,depth=0}`，Save 后用 `db.Delete(&row)` **软删除**它（GORM 将其 `deleted_at` 置为当前时间）。源 query：`q.Unscoped().CrossJoinAs(sub).WhereRaw("sub.ancestor_id = ?", 5).Eq(&ext.DescendantID, 5).WhereRaw("sub.deleted_at IS NULL")` + 3 个同 AC-1 的 `SelectRaw`。`InsertSelect(repo, ctx, []any{"ancestor_id","descendant_id","depth"}, q)`。
- **验证机制**：`sub` 候选（`ancestor_id=5`）含未删 `desc=7` 和已删 `desc=88`。若无手动 `sub.deleted_at IS NULL`，Unscoped 会让两者都进源 → 多产生 `{1,88,1}`。手动前缀排除已删行。
- **可观测输出**：返回 `(1, nil)`；新增行恰为 `{anc=1, desc=7, depth=1}`；查无 `descendant_id=88` 的新增行（`SELECT count(*) FROM closure_sd WHERE descendant_id=88 AND deleted_at IS NULL = 0`，即已删行未被复制成新未删行）。

## 测试组织

- 新文件 `insert_select_join_test.go`（scenario 2，与 scenario 1 的 `insert_select_test.go` 区分），`package gplus`。
- 复用 `insert_select_test.go` 的 `Closure`、`assertClosureCount`、`repo_test.go` 的 `setupTestDB`。
- 本文件新增 `ClosureSD` 软删除模型（`TableName() = "closure_sd"`）。
- 测试函数名 1:1 对应 AC（如 `TestInsertSelectJoin_inserts_row_when_no_softdelete` / `_softdelete_bare_column_fails` / `_unscoped_with_alias_prefix_excludes_deleted`）。

## 已知限制（文档化，AC 锁死）

- **软删除 closure 自连接需 Unscoped + 手动别名前缀**：带 `deleted_at` 的 closure 表直接自连接 `InsertSelect` 会因 GORM 软删除条件用裸表名而报 `no such column: <table>.deleted_at`（AC-2 锁死，与 Round 3a AC-11 同源）。出路：源 query 调 `Unscoped()` 跳过自动软删除，并**手动** `WhereRaw("<alias>.deleted_at IS NULL")` 自带别名前缀以保持软删除语义（AC-3 锁死）。gplus **不自动改写**（拦 GORM softdelete callback 属过度工程，仅服务单一下游场景，YAGNI）。

## 方言风险（CI 无法覆盖，下游真机验证）

- **自插入自连接形态未真机验证**：`INSERT INTO closure SELECT ... FROM closure AS ext JOIN closure AS sub`（目标表 = 源表）在 **MySQL / PostgreSQL / 达梦 DM** 的接受度，CI 仅有 SQLite，未验证。推测（需下游核实）：PG 接受；MySQL 标准 INSERT...SELECT 同表允许、但自连接别名形态需核实；DM 兼容 Oracle 大概率接受。`getQuoteChar` 已覆盖三库标识符引号，引号层无风险。下游 gvs-server 接入时须在目标库真机执行确认。

## 显式排除（Out of Scope）

- **InsertSelect 实现改动**：不改 `repository.go`（探针证明零改动可用）。
- **软删除自动前缀改写**：不做（YAGNI，拦 GORM callback 过度工程）。
- **MySQL/PG/DM 真机验证**：本轮 SQLite 为准，方言形态记为已知风险待下游。
- **复合主键 closure / 其它 scenario（A2-A5/B）**：不在本轮。

## 覆盖率

纯测试新增，覆盖率不低于基线 95.0%。
