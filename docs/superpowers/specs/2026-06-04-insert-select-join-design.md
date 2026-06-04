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

- **输入**：`setupTestDB[Closure]`，种子闭包链 `{anc=1,desc=5,depth=0}`、`{anc=5,desc=7,depth=0}`。源 query：`repo.NewQueryAs(ctx,"ext")`（用 `repo.` 形式可推断 T；顶层 `NewQueryAs[Closure](ctx,"ext")` 须显式类型参数）+ `As[Closure](q,"sub")` + `q.CrossJoinAs(sub).WhereRaw("sub.ancestor_id = ?", 5).Eq(&ext.DescendantID, 5)` + `q.SelectRaw("ext.ancestor_id").SelectRaw("sub.descendant_id").SelectRaw("ext.depth + sub.depth + 1")`。`InsertSelect(repo, ctx, []any{"ancestor_id","descendant_id","depth"}, q)`。
- **可观测输出**：返回 `(1, nil)`；`closure` 表总行数 3；新增行恰为 `{anc=1, desc=7, depth=1}`（原 2 行不变）。

### AC-2：软删除模型自连接（不 Unscoped）报裸表名错误，零副作用（限制锁）

- **输入**：`setupTestDB[ClosureSD]`（`ClosureSD` 含 `DeletedAt gorm.DeletedAt`），种子 `{anc=1,desc=5,depth=0}`、`{anc=5,desc=7,depth=0}`。同 AC-1 形态的自连接源 query（**不调** `Unscoped()`）。`InsertSelect(repo, ctx, []any{"ancestor_id","descendant_id","depth"}, q)`。
- **可观测输出**：返回 `err != nil`，`err.Error()` 同时含 `"no such column"` 与 `"closure_sd.deleted_at"`；`closure_sd` 表行数仍为 2（无插入副作用）。

### AC-3：软删除模型 Unscoped + 手动别名前缀正路，已删行不被复制（出路锁）

- **输入**：`setupTestDB[ClosureSD]`，种子 `{anc=1,desc=5,depth=0}`、`{anc=5,desc=7,depth=0}`，再加 1 条 `{anc=5,desc=88,depth=0}`，Save 后用 `db.Delete(&row)` **软删除**它（GORM 将其 `deleted_at` 置为当前时间）。源 query：`q.Unscoped().CrossJoinAs(sub).WhereRaw("sub.ancestor_id = ?", 5).Eq(&ext.DescendantID, 5).WhereRaw("ext.deleted_at IS NULL").WhereRaw("sub.deleted_at IS NULL")` + 3 个同 AC-1 的 `SelectRaw`。`InsertSelect(repo, ctx, []any{"ancestor_id","descendant_id","depth"}, q)`。
- **验证机制**：`sub` 候选（`ancestor_id=5`）含未删 `desc=7` 和已删 `desc=88`。若无手动 `sub.deleted_at IS NULL`，Unscoped 会让两者都进源 → 多产生 `{1,88,1}`。手动前缀排除已删行。**query 同时加 `ext.deleted_at IS NULL` 与 `sub.deleted_at IS NULL` 两侧前缀以演示正确用法**（本 AC 仅在 `sub` 侧种已删行作区分点；真实迁移祖先侧 `ext` 同样可能含已删行，两侧都须过滤——见「已知限制」危险用法警告）。
- **可观测输出**：返回 `(1, nil)`；新增行恰为 `{anc=1, desc=7, depth=1}`；查无 `descendant_id=88` 的新增行（`SELECT count(*) FROM closure_sd WHERE descendant_id=88 AND deleted_at IS NULL = 0`，即已删行未被复制成新未删行）。

## 测试组织

- 新文件 `insert_select_join_test.go`（scenario 2，与 scenario 1 的 `insert_select_test.go` 区分），`package gplus`。
- 复用 `insert_select_test.go` 的 `Closure`、`assertClosureCount`、`repo_test.go` 的 `setupTestDB`。
- 本文件新增 `ClosureSD` 软删除模型（`TableName() = "closure_sd"`）。
- 测试函数名 1:1 对应 AC（如 `TestInsertSelectJoin_inserts_row_when_no_softdelete` / `_softdelete_bare_column_fails` / `_unscoped_with_alias_prefix_excludes_deleted`）。
- **AC-1 强断言要求**：必须逐字段断言新增行 `{anc=1, desc=7, depth=1}`，**禁止只用 `assertClosureCount(3)` 数行数**——列错位/错误投影仍满足行数=3（红队实测错位 targetCols 产生 `{7,1,1}` 仍是 3 行的假绿），只有精确字段断言能捕获。
- **InsertSelectTx 自连接变体不单独覆盖**：`InsertSelectTx` 仅多传 `tx *gorm.DB` 给 `dbResolver`，与源 query 形态正交；Round 2 的 InsertSelectTx 回滚 AC 已锁 Tx 路径，本轮不重复（避免 AC 膨胀）。

## 已知限制（文档化，AC 锁死）

- **软删除 closure 自连接需 Unscoped + 手动两侧别名前缀**：带 `deleted_at` 的 closure 表直接自连接 `InsertSelect` 会因 GORM 软删除条件用裸表名而报 `no such column: <table>.deleted_at`（AC-2 锁死，与 Round 3a AC-11 同源）。出路：源 query 调 `Unscoped()` 跳过自动软删除，并**为自连接的每一侧别名各自**手动补 `WhereRaw("<alias>.deleted_at IS NULL")`（如 `ext` 和 `sub` 两侧都加）以保持软删除语义（AC-3 锁死）。gplus **不自动改写**（拦 GORM softdelete callback 属过度工程，仅服务单一下游场景，YAGNI）。

- **⚠️ 危险用法警告（数据完整性）**：`Unscoped()` 在自连接 `InsertSelect` 中是**危险默认**——它同时关闭「软删除报错」与「软删除过滤」。**任一别名侧漏写 `<alias>.deleted_at IS NULL`**，已逻辑删除的行会被复制成目标表的**活跃（`deleted_at IS NULL`）新行**，造成不可逆数据污染（已删数据「复活」）。AC-3 只能锁「正确用法」，锁不住「下游忘记加前缀」的人为遗漏——下游 gvs-server 接入时**必须在 code review 强制检查**：每个软删除别名都带了 `deleted_at IS NULL` 前缀。

## 方言风险（CI 无法覆盖，下游真机验证）

- **自插入自连接形态**：`INSERT INTO closure (...) SELECT ... FROM closure AS ext JOIN closure AS sub`（目标表 = 源表）。CI 仅 SQLite，三库真机未验证，但据官方文档可给出较确定结论：
  - **MySQL：允许（非 error 1093）**。官方手册明确 INSERT...SELECT 的目标表**可出现在 SELECT 的顶层 FROM**（MySQL 内部建临时表中转）；error 1093「can't specify target table ... in FROM」**仅限 UPDATE/DELETE 的同表子查询**，不适用 INSERT...SELECT 顶层 FROM。gplus 的 `InsertSelectTx` 用裸 `?` 内联子查询**无外层括号**（Round 2 实测 + 本轮 P1 SQL 印证），正是顶层 FROM 形态，落在允许侧。**唯一真机验证点**：确认目标库 GORM 驱动版本不给内联子查询加外层括号（一旦加括号 → 变 "in a subquery" → 触发 1093）。来源：MySQL 8.4 手册 15.2.7.1（https://dev.mysql.com/doc/refman/8.4/en/insert-select.html）、Bug #6980（https://bugs.mysql.com/bug.php?id=6980）。
  - **PostgreSQL：允许，无限制**。INSERT 源 SELECT 复用标准 SELECT 语法，自连接别名唯一即可。来源：https://www.postgresql.org/docs/current/sql-insert.html。
  - **达梦 DM：语法层面有利，需真机确认引号大小写**。DM 官方「同一表的不同别名视为不同对象」（利于自连接），但裸前缀列解析依赖 `COMPATIBLE_MODE=2`，且 `CASE_SENSITIVE=Y` 下裸表名可能 UPPERCASE 后与双引号建表的小写真实表名不匹配。来源：https://eco.dameng.com/document/dm/zh-cn/pm/insertion-deletion-modification.html。
- **引号覆盖与缺口**：`getQuoteChar` 已覆盖 `dm`/`postgres`（双引号）、`mysql`/`tidb`（反引号）、`sqlserver`（方括号），**主别名** `<qL>closure<qR> AS <qL>ext<qR>` 三库引号正确。但 **`CROSS JOIN closure AS sub` 的副别名表名在物化路径为裸表名（不带引号）**（本轮 P1 SQL 实测），在 DM `CASE_SENSITIVE=Y` 库可能大小写不匹配——非「引号层完全无风险」，下游 DM 真机须一并确认。
- **结论：方案 A 不退化**。MySQL/PG 官方既接受、DM 语法层面有利，故「纯验证 + 文档」不会退化成「文档说不支持」，无需回退方案 B/C。残留真机项仅为 DM 引号大小写 + GORM 是否给内联子查询加括号。

## 显式排除（Out of Scope）

- **InsertSelect 实现改动**：不改 `repository.go`（探针证明零改动可用）。
- **软删除自动前缀改写**：不做（YAGNI，拦 GORM callback 过度工程）。
- **MySQL/PG/DM 真机验证**：本轮 SQLite 为准，方言形态记为已知风险待下游。
- **复合主键 closure / 其它 scenario（A2-A5/B）**：不在本轮。

## 覆盖率

纯测试新增，覆盖率不低于基线 95.0%。
