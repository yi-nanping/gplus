# 主别名 FROM 物化 — Round 3a（InsertSelect scenario 2 前置 bug fix）

> **状态**：可进 plan — 2026-06-04 brainstorm + 4 视角多专家审计（architect/code-reviewer/security/红队）后定稿。审计修正已全部并入（见文末「审计修正记录」）。
>
> **拆轮背景**：本特性是 InsertSelect scenario 2（自连接 `INSERT...SELECT...JOIN`，见 `2026-06-04-insert-select-design.md` Round 3）的**独立先行前置**。Round 3a（本文档）先修主别名 FROM 物化的 latent bug；Round 3b 再做 InsertSelect scenario 2，复用 Round 2 现有 `InsertSelect` 入口（源 query 用 `NewQueryAs+CrossJoinAs+WhereRaw` 表达即可，无需新 API）。

> **日期**：2026-06-04
> **范围**：修 `ScopeBuilder.applyBaseTable` 让**顶层** `NewQueryAs(alias)` 设置的主表别名物化进 SELECT 路径（`BuildQuery`/`BuildCount`）的 FROM 子句（`FROM <table> AS <alias>`），使引用主别名的 SELECT/WHERE 在真实执行（List/ToDB/ToSQL）下合法。**不波及子查询路径，不波及写路径（Update/Delete）**。

---

## 实测推翻的原始前提（bug 定性）

InsertSelect spec 的 Round 3 推迟说明称「主别名在 List/Find 路径会应用、唯独 ToDB 物化丢失」。2026-06-04 一次性探针（已删）实测**推翻此前提**：

| 路径 | 生成的 FROM | 真实执行 |
|---|---|---|
| List / ToSQL | `SELECT "ext"."id" FROM \`test_users\` WHERE "ext"."username"=?` | FROM **不带 `AS ext`** |
| ToDB 物化 | 同上 | 报 `no such column: ext.id` |

**两条路径都不把主别名注入 FROM**。List/ToSQL「看起来正常」仅因 `ToSQL` 不真正打库（只拼串）；真实 `List` 一个引用主别名的查询同样 `no such column` 失败。

**根因**：`NewQueryAs(alias)`（query.go:46）内部 `As[T](q, alias)` 只把别名注册进 `q.core.aliases` 供 `resolveColumnName` 把 `&t.Field` 解析成 `"alias".col`（影响 SELECT/WHERE），但 `ScopeBuilder.applyBaseTable`（builder.go:269）**只认 `b.tableName`（`Table()` 设的），从不发射 `FROM table AS alias`**。数据模型里根本没有「主别名」概念——主别名与副别名（`As`+`CrossJoinAs`/`LeftJoinAs`）在 `aliases` map 里无区别，副别名通过 `applyJoins`（`db.Joins` 拼 `... AS alias`）物化，主别名无对应物化点。

现有自连接 e2e（`alias_datarule_e2e_test.go:88`）能「过」，是因仅 `strings.Contains(sql,"boss")` 断言、**从不真实执行**，掩盖了主别名 FROM 缺失。

## Goal

修 `applyBaseTable`：**顶层** `NewQueryAs` 设置主别名后，**仅 SELECT 路径**（`BuildQuery`/`BuildCount`）发射 `FROM <table> AS <alias>`，使主别名查询在真实执行下合法。**不扩大范围**：不新增 `NewUpdaterAs`（YAGNI）；不改副别名物化路径；不波及子查询；不修 DataRule 列前缀。

## Acceptance Criteria

> 每条 AC 含具体输入值 + 具体可观测输出，1:1 对应测试函数。方言用 SQLite in-memory（与现有测试一致）。
> 测试模型用 `Closure{AncestorID, DescendantID, Depth uint}`（`insert_select_test.go` 已定义，`TableName()` 返回 `"closure"`，主键 `D=uint`）。bug 类先写复现红测试，再加修复让其绿。
> **AC-1/AC-2/AC-4/AC-6 一律真实执行打库**（非仅 ToSQL 字符串断言），避免重蹈「string-contains 掩盖 latent bug」覆辙。

- **AC-1（复现 → 修复，List 真实执行）**：`closure` 含 1 行 `{ancestor_id:1, descendant_id:5, depth:0}`。
  `q, m := r.NewQueryAs(ctx, "ext")`；`q.Select(&m.AncestorID).Eq(&m.DescendantID, 5)`；执行 `r.List(q)`。
  **修复前**：返回 error 含 `no such column: ext.ancestor_id`（红）。
  **修复后**：返回 `([]Closure{{AncestorID:1,...}}, nil)`，长度 1；`q.ToSQL(db)` 的 FROM 含 `closure" AS "ext`（带方言引号，见 H-3 决策）。
- **AC-2（ToDB 物化真实执行）**：同 AC-1 的 `q`，把 `q.ToDB(db)` 直接 `.Find(&rows)` 真实执行。
  **修复前**：报 `no such column: ext.ancestor_id`。**修复后**：执行成功，返回 1 行 `{ancestor_id:1}`。
- **AC-3（无别名零回归，精确匹配）**：`q, m := r.NewQuery(ctx)`（无别名）；`q.Select(&m.AncestorID)`。`q.ToSQL(db)` 生成的 FROM 子句**精确匹配** `FROM \`closure\``（或带方言引号的裸表名），**其后不接 ` AS `**（用正则 `FROM\s+["\x60]?closure["\x60]?(?!\s+AS)` 断言，非裸 `Contains(" AS ")` 取反）；`r.List(q)` 正常返回。
- **AC-4（Table override 组合，真实执行）**：先建表 `closure_2024`（同 Closure schema）并播种 1 行。`q, m := r.NewQueryAs(ctx, "ext")`；`q.Table("closure_2024").Select(&m.AncestorID).Eq(&m.DescendantID, 5)`；`r.List(q)` 真实执行成功，返回该行；FROM 含 `closure_2024" AS "ext`（显式 `Table()` 覆盖值优先于 T 反射表名）。
- **AC-5（Clear 重置，含内部字段断言）**：`q, _ := r.NewQueryAs(ctx, "ext")`；`q.Clear()` 后：(a) 内部字段 `q.mainAlias == ""`（package 内测试可访问）；(b) `q.ToSQL(db)` 的 FROM 不含 `AS ext`。
- **AC-6（自连接主+副别名真实执行，含字段指针路径，解锁 Round 3b）**：`closure` 含行 `{1,5,0}`、`{5,7,0}`。
  `q, ext := r.NewQueryAs(ctx, "ext")`；`sub := As[Closure](q, "sub")`；
  `q.CrossJoinAs(sub).WhereRaw("sub.ancestor_id = ?", 5).Eq(&ext.DescendantID, 5)`；
  `q.SelectRaw("ext.ancestor_id").SelectRaw("sub.descendant_id").SelectRaw("ext.depth + sub.depth + 1")`；
  经 `db.Scopes(q.BuildQuery()).Scan(&rows)`（rows 为 `[]struct{A,D,Dep uint}`）真实执行。
  - **字段指针覆盖**：WHERE 用 `Eq(&ext.DescendantID, 5)`（经 `resolveColumnName`→`"ext".descendant_id` 带引号），验证字段指针解析的别名引号与 FROM 的 `"closure" AS "ext"` 引号一致。
  - **可观测输出**：FROM 含 `closure" AS "ext` 且 JOIN 含 `closure AS sub`；执行成功返回 1 行 `{A:1, D:7, Dep:1}`。
  此条直接执行 scenario 2 源 query 形态，证明 Part A 已解除 Part B 阻塞（不经 InsertSelect）。
- **AC-7（子查询零污染，C-1 回归守卫）**：`outer, ou := r.NewQuery(ctx)`；`sub, su := SubQuery[Closure](outer)`（sub 主别名 = 表名 `"closure"`）；`sub.Select(&su.AncestorID).Eq(&su.DescendantID, 5)`；`outer.InSub(&ou.AncestorID, sub)`。`outer.ToSQL(db)` 的子查询 FROM **不含** `closure AS closure`（子查询不被主别名物化波及——`SubQuery`/`SubQueryAs` 已清空 mainAlias）。
- **AC-8（写路径不物化，H-2 守卫）**：`q, m := r.NewQueryAs(ctx, "ext")`；`q.Eq(&m.DescendantID, 5)`；`r.DeleteByCondTx(q, nil)`（签名 `(q, tx) (int64,error)`，ctx 取自 `q.Context()`）生成的 SQL **不含** `AS ext`（`DELETE FROM closure AS ext` 在 MySQL/PG 非法，故写路径禁物化）；删除按 `WHERE descendant_id=5` 正常执行，返回 `(1, nil)`。
- **AC-9（DataRule+自连接 ambiguous 已知限制，验证型）**：`closure` 含软删除无关行；ctx 注入 `DataRule{Column:"depth", Condition:">=", Value:"0"}`；`q, ext := r.NewQueryAs(ctx, "ext")` + `As[Closure](q,"sub")` + `CrossJoinAs` 自连接。真实执行 `r.List(q)`。**断言**：返回 error 且错误串含子串 `ambiguous`（锁定「DataRule 裸列名在多别名自连接下 ambiguous」为已知限制——用户须在 `DataRule.Column` 自带 `ext.` 前缀规避，Round 3a 不负责加前缀）。此 AC 防止该限制被静默忽略。
  > plan 阶段须用一次性探针确认 SQLite 对此场景的确切错误文本（预期含 `ambiguous`），据此固定断言；若实测**不报** ambiguous（如优化器消歧），则 AC-9 改为断言「返回行集 = DataRule 过滤后预期」并把已知限制降级为文档说明。

## Architecture

### 变更面（最小）

| 文件 | 变更 |
|---|---|
| `builder.go` | `ScopeBuilder` 新增 `mainAlias string` + `mainAliasTable string`；`applyBaseTable` 改签名 `applyBaseTable(db, qL, qR, allowAlias bool)`，增加主别名分支（含 `validTableName` 守卫）；`BuildQuery`/`BuildCount` 传 `allowAlias=true`，`BuildUpdate`/`BuildDelete` 传 `false`；`Clear()` 重置两字段；新增 `validTableName` 正则 |
| `query.go` | `NewQueryAs[T]`：`aliasNameRegexp.MatchString(alias)` 合法时才写 `q.mainAlias=alias`、`q.mainAliasTable=q.mainTableName()` |
| `subquery.go` | `SubQuery`/`SubQueryAs` 调 `NewQueryAs` 后立即 `sub.mainAlias=""`、`sub.mainAliasTable=""`（子查询靠 ToDB 的 `Model()` 注入 FROM，不经 applyBaseTable 物化） |

> 不新增 sentinel 错误（别名非法仍走现有 `ErrAliasInvalidName`；表名非法见 Error Handling）。不新增公开 API。

### 数据流

```
NewQueryAs[T](ctx, alias)
  → As[T](q, alias)                              // 现有：注册 aliases map（列解析）
  → if aliasNameRegexp.MatchString(alias):        // 新增：仅合法别名才物化 FROM
        q.mainAlias = alias
        q.mainAliasTable = q.mainTableName()       // 复用现有方法（query.go:1268）

SubQuery[X](outer) / SubQueryAs[X](outer, alias)
  → sub, x := NewQueryAs[X](ctx, ...)
  → sub.mainAlias = ""; sub.mainAliasTable = ""    // 新增：子查询不物化 FROM（C-1 修复）

BuildQuery()/BuildCount()  → applyBaseTable(db, qL, qR, allowAlias=true)
BuildUpdate()/BuildDelete()→ applyBaseTable(db, qL, qR, allowAlias=false)

applyBaseTable(db, qL, qR, allowAlias):
    if allowAlias && b.mainAlias != "":
        table := b.tableName ; if table == "" { table = b.mainAliasTable }   // Table() 覆盖优先
        if !validTableName.MatchString(table):                               // M-1 注入守卫
            // 写入 errs（与现有错误累积模型一致），不物化
        else:
            db = db.Table(qL+table+qR + " AS " + qL+b.mainAlias+qR)           // 机制1
    else if b.tableName != "":
        db = db.Table(b.tableName)                                            // 现有路径，零回归
    if b.unscoped { db = db.Unscoped() }
    return db
```

### 关键设计决策（含审计回应）

- **C-1（子查询零污染）**：`SubQuery`/`SubQueryAs` 复用 `NewQueryAs` 入口（subquery.go:43,64）。修复在两构造器内清空 `mainAlias`——子查询的 FROM 由 `ToDB` 的 `Model(getModelInstance[T]())` 注入，无需 applyBaseTable 物化。顶层 `NewQueryAs`（含 Round 3b 的 InsertSelect 源 query，`outerQueryRef==nil`）照常物化。AC-7 守此。
- **H-2（仅 SELECT 路径）**：`DELETE FROM closure AS ext`/`UPDATE closure AS ext` 在 MySQL/PG 多方言非法。`DeleteByCondTX(ctx, q, tx)` 接受 `Query` 并走 `BuildDelete`，故主别名 Query 传 DeleteByCond 会触发写路径。`applyBaseTable` 加 `allowAlias bool`，仅 `BuildQuery`/`BuildCount` 传 true。AC-8 守此。
- **H-3（FROM 引号形态定死）**：FROM 用 `qL+table+qR + " AS " + qL+alias+qR`（带方言引号，如 SQLite `"closure" AS "ext"`），与 `quoteColumn` 对别名引用的转义同源（同一次 `getQuoteChar`）。`applyBaseTable` 改签名收 qL/qR（复用 Build* 入口已算的引号，与现有 `applyWhere(db,qL,qR)`/`applyGroupHaving(db,qL,qR)` 签名惯例一致），不在内部重复 `getQuoteChar`。AC 断言文本据此固定为 `closure" AS "ext`。
- **mainAliasTable 冗余说明（MEDIUM-1 回应）**：`mainAliasTable` 的值在 `aliases[mainAlias].typ`（aliasEntry.typ）已隐含，但 `ScopeBuilder` 是非泛型基类、拿不到 `queryCore.aliases` map，故 `applyBaseTable` 无法像 `appendJoinAs`（query.go:1164 实时 `aliasSchemaTableName(typ)`）那样实时解析，只能由有 T 的 `NewQueryAs` 预存字符串。这是抽象边界的必然妥协，非疏忽。
- **别名合法性前置（code-reviewer HIGH 回应）**：`As` 不返回 ok 布尔；`NewQueryAs` 用 `aliasNameRegexp.MatchString(alias)` 显式二次判断，合法才写 `mainAlias`，避免非法别名（如 `"1bad"`）被拼进 `db.Table("... AS 1bad")` 生成语法错误 SQL。

### 物化机制（实测背书，机制1）

探针（SQLite 真实执行，已删）证实 `db.Table("closure AS ext")` 生成 `FROM closure AS ext`，GORM **原样透传不误转义**，`"ext".col` 引用 + 真实执行成功；与 `db.Joins("JOIN closure AS sub ON ...")` 同时使用时两者共存、scenario 2 形态执行返回正确行。故复用现有 `b.tableName → db.Table()` 路径即可，无需 `clause.From`。

> **实现期三路径探针（HIGH-1 回应，写进 plan）**：孤立 `db.Table` 探针不足以覆盖生产路径的 `Table()` vs `Model()`/`Find(dest)` FROM 优先级。实现期必须**分别**用一次性探针验证三条真实路径下 FROM 均含 `AS ext`：(1) `List`/`ListTx`（repository.go:274，**不调 `.Model()`**，靠 `Find(&[]T)` dest 反射）；(2) `ToSQL`（debug.go:24，`.Model(new(T))`）；(3) `ToDB`（query.go:323，`.Session(NewDB:true).Model(...)`）。GORM 中 `Statement.Table != ""`（`db.Table()` 设过）优先于 Model/dest 反射，预期三路径一致，但须实测确认（尤其 ToDB 的 `Session{NewDB:true}` clone 后 Table 是否保留）。
> **诚实标注**：已删探针只实测了**无引号** `db.Table("closure AS ext")` 可执行；H-3 定的**带引号** `db.Table(\`"closure" AS "ext"\`)` 形态**尚未实测**。plan 阶段须探针确认带引号形态在 SQLite 真实执行成功（GORM 是否对含引号的 Table 串原样透传）；若带引号形态被 GORM 误转义/执行失败，则退回无引号形态并重新评估 R1 跨方言一致性。

## Error Handling

- **别名非法**（不符 `aliasNameRegexp`）：现有 `As` 累积 `ErrAliasInvalidName` 不变；`NewQueryAs` 因 `MatchString` 失败**不写** `mainAlias`，避免给坏别名物化 FROM。
- **表名非法**（M-1 注入守卫）：`b.tableName`（用户 `Table(name)` 入参）在主别名分支用 `validTableName` 正则校验，不符则写入 `errs`（经 `GetError()` 拦截，不执行到 SQL），防止 `Table("x\"; DROP--")` 经 `qL+table+qR` 拼接被引号提前闭合注入。`validTableName = ^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)*$`（允许 `schema.table` 分库分表形式，与 `validDataRuleColumn` 同源风格）。
- **无别名**（`NewQuery`）：`mainAlias == ""`，走现有 `b.tableName` 分支，零行为变化（AC-3）。

## Testing

- 新增测试文件 `main_alias_from_test.go`（package gplus，可访问未导出 `mainAlias` 字段与 `As`/`SelectRaw`/`validTableName` 等）。
- AC-1..AC-9 各对应一个测试函数，命名描述行为（如 `TestMainAlias_list_with_alias_emits_from_as`），AAA 结构。
- AC-1/AC-2/AC-4/AC-6/AC-8/AC-9 真实执行打库；AC-3/AC-5/AC-7 验证生成 SQL 文本（用精确/正则匹配，非脆弱 `Contains`）。
- 覆盖率门禁 ≥ 80%（项目当前 95.0%，不得回退）。
- 回归：全量 `go test ./...` 通过；尤其 `alias_*_test.go`、`query_newqueryas_test.go`、`query_subquery*_test.go`、`*_integration_test.go`（多在 build tag/env 后，本地 SQLite 路径必绿）。

## 显式排除（Out of Scope）

- **NewUpdaterAs**：不新增（无调用点，YAGNI）。写路径 `AS` 多方言不可移植，故 applyBaseTable 显式 `allowAlias=false` 排除写路径。
- **副别名物化路径**：不改（现有 `applyJoins` 正确）。
- **子查询主别名物化**：`SubQuery`/`SubQueryAs` 主别名仅供列解析，FROM 不物化（清空 mainAlias）。`SubQueryAs("o")` 自定义别名在真实执行下的 FROM 别名（`FROM t AS o`）**仍不支持**——属既有限制，本轮不修（无调用点触发真实执行）。
- **DataRule 列加别名前缀（AC-9 已知限制）**：主别名 + DataRule 注入列 + 自连接时，DataRule 生成裸列名在多别名下 ambiguous；用户须在 `DataRule.Column` 自带 `ext.` 前缀规避。Round 3a 不修 DataRule 系统。InsertSelect 源 query 不注入 DataRule（Round 2 已定），不影响 Round 3b。
- **InsertSelect scenario 2 本体**：Round 3b（依赖本轮合入）。
- **MySQL/PG 真机方言验证**：本轮以 SQLite 为准（与现有基线一致）；`getQuoteChar` 已覆盖各方言引号，记为已知风险。

## 已知风险

- **R1（跨方言引号一致性）**：FROM 的 `qL+table+qR AS qL+alias+qR` 与 SELECT/WHERE 中 `quoteColumn` 对别名引用的转义须一致（同一 `getQuoteChar` 来源，已在 H-3 锁定）。SQLite 已实测；MySQL/PG/DM/Oracle 引号字符已在 `getQuoteChar` 覆盖。AC-6 用字段指针路径覆盖 `resolveColumnName`→`quoteColumn` 与 FROM 别名引号端到端一致性。
- **R2（既有 e2e 断言变化）**：`alias_datarule_e2e_test.go` 等 string-contains 断言修复后仍 PASS（FROM 新增 `AS ext` 不破坏 `Contains("boss")`）；子查询测试因 C-1 修复（mainAlias 清空）FROM 不变，零影响。须全量回归确认。
- **R3（软删除模型 deleted_at 前缀）**：Closure 模型无 `deleted_at`，AC 不涉软删除。带软删除字段的模型在主别名 + 自连接下，GORM 自动 `deleted_at IS NULL` 是否带别名前缀未验证——记为已知风险，下游 InsertSelect scenario 2（Round 3b）若用软删除 closure 表须实测（org_closure 是否软删除待 Round 3b 核实）。

## 审计修正记录（2026-06-04，4 视角多专家审计）

| 编号 | 级别 | 来源 | 修正 |
|---|---|---|---|
| C-1 | CRITICAL | code-reviewer + 红队（交叉验证，已亲核 subquery.go:43,64） | SubQuery/SubQueryAs 清空 mainAlias；新增 AC-7 守卫 |
| H-2 | HIGH | 红队 | applyBaseTable 加 allowAlias；仅 SELECT 路径物化；新增 AC-8 |
| H-3 | HIGH | architect + 红队 | FROM 引号形态定死（带引号 + qL/qR 入参），AC 断言文本固定 |
| H-1d | HIGH | 红队 | DataRule+自连接 ambiguous 文档化为已知限制；新增验证型 AC-9 |
| HIGH-1 | HIGH | architect | 实现期三路径探针要求写进「物化机制」节 |
| HIGH(别名合法性) | HIGH | code-reviewer | NewQueryAs 用 aliasNameRegexp 前置判断 |
| M-1 | MEDIUM | security | applyBaseTable 主别名分支加 validTableName 守卫 |
| M-(FindAs/真实执行) | MEDIUM | code-reviewer + 红队 | AC-4/AC-6 改真实执行；去掉不存在的 FindAs，改 Scan 路径 |
| M-1(冗余) | MEDIUM | architect | mainAliasTable 冗余决策说明并入「关键设计决策」 |
| LOW(AC-3 脆弱) | LOW | architect | AC-3 改正则精确匹配 |
| LOW(字段指针) | LOW | architect | AC-6 增 Eq(&ext.X) 字段指针路径 |
| LOW(mainTableName) | LOW | code-reviewer | mainAliasTable 复用 q.mainTableName() |
