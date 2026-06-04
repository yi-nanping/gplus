# 主别名 FROM 物化 — Round 3a（InsertSelect scenario 2 前置 bug fix）

> **状态**：可进 plan — 2026-06-04 brainstorm + **两轮** 4 视角多专家审计后定稿。两轮审计修正已全部并入（见文末「审计修正记录」）。
>
> **拆轮背景**：本特性是 InsertSelect scenario 2（自连接 `INSERT...SELECT...JOIN`，见 `2026-06-04-insert-select-design.md` Round 3）的**独立先行前置**。Round 3a（本文档）先修主别名 FROM 物化的 latent bug；Round 3b 再做 InsertSelect scenario 2，复用 Round 2 现有 `InsertSelect` 入口（源 query 用 `NewQueryAs+CrossJoinAs+WhereRaw` 表达即可，无需新 API）。

> **日期**：2026-06-04
> **范围**：修 `ScopeBuilder` 让**顶层** `NewQueryAs(alias)` 设置的主表别名物化进 SELECT 路径（`BuildQuery`/`BuildCount`）的 FROM 子句（`FROM <table> AS <alias>`），使引用主别名的 SELECT/WHERE 在真实执行（List/ToDB/ToSQL/Count）下合法。**不波及子查询路径，不波及写路径（Update/Delete）**。

---

## 实测推翻的原始前提（bug 定性）

InsertSelect spec 的 Round 3 推迟说明称「主别名在 List/Find 路径会应用、唯独 ToDB 物化丢失」。2026-06-04 一次性探针（已删）实测**推翻此前提**：

| 路径 | 生成的 FROM | 真实执行 |
|---|---|---|
| List / ToSQL | `SELECT "ext"."id" FROM \`test_users\` WHERE "ext"."username"=?` | FROM **不带 `AS ext`** |
| ToDB 物化 | 同上 | 报 `no such column: ext.id` |

**两条路径都不把主别名注入 FROM**。List/ToSQL「看起来正常」仅因 `ToSQL` 不真正打库（只拼串）；真实 `List` 一个引用主别名的查询同样 `no such column` 失败。

**根因**：`NewQueryAs(alias)`（query.go:46）内部 `As[T](q, alias)` 只把别名注册进 `q.core.aliases` 供 `resolveColumnName` 把 `&t.Field` 解析成 `"alias".col`（影响 SELECT/WHERE），但 `ScopeBuilder` 从不发射 `FROM table AS alias`——`applyBaseTable`（builder.go:269）只认 `b.tableName`（`Table()` 设的）。数据模型里根本没有「主别名」概念——主别名与副别名（`As`+`CrossJoinAs`/`LeftJoinAs`）在 `aliases` map 里无区别，副别名通过 `applyJoins`（`db.Joins` 拼 `... AS alias`）物化，主别名无对应物化点。

现有自连接 e2e（`alias_datarule_e2e_test.go:88`）能「过」，是因仅 `strings.Contains(sql,"boss")` 断言、**从不真实执行**，掩盖了主别名 FROM 缺失。

## Goal

新增 `applyMainAlias` 物化逻辑：**顶层** `NewQueryAs` 设置主别名后，**仅 SELECT 路径**（`BuildQuery`/`BuildCount`）发射 `FROM <table> AS <alias>`，使主别名查询在真实执行下合法。**不扩大范围**：不新增 `NewUpdaterAs`（YAGNI）；不改副别名物化路径；不波及子查询；不修 DataRule 列前缀。

## 读方法 → Build 路径对照表（审计依据，消除路径问号）

| Repository 读方法 | Build 路径 | 主别名是否物化 | 备注 |
|---|---|---|---|
| `List`/`ListTx` | BuildQuery | ✅ | `Find(&[]T)`，不调 `.Model()` |
| `GetOne`/`First`/`Last`/`GetByLock` | BuildQuery | ✅ | `.First()` 追加 `ORDER BY pk LIMIT 1`（First 路径，探针验证） |
| `Exists` | BuildQuery | ✅ | `BuildQuery().Limit(1).Find()`（**非** BuildCount） |
| `Chunk` | BuildQuery | ✅ | FindInBatches 主键游标 |
| `Count` | BuildCount | ✅ | `.Model(new(T)).Scopes(BuildCount()).Count()` |
| `Page`（COUNT 段） | BuildCount | ✅ | `Model+Session{}` clone=2（探针验证） |
| `Page`（数据段） | BuildQuery | ✅ | 同上 session |
| `FirstOrCreate`/`FirstOrUpdate`（查询段） | BuildCount | ✅ | `.First()`（First 路径，探针验证） |
| `ToSQL` | BuildQuery | ✅ | `.Model(new(T))` |
| `ToCountSQL` | BuildCount | ✅ | `.Model(new(T))` |
| `ToDB`（Round 3b 入口） | BuildQuery | ✅ | `.Session(NewDB:true).Model()` |
| `DeleteByCond` | BuildDelete | ❌ | 写路径，`applyMainAlias` 不调用 |
| `UpdateByCond`/`UpdateById`/`IncrBy` 等 | BuildUpdate | ❌ | 写路径（用 Updater，无 NewUpdaterAs 入口，mainAlias 恒空） |

> **物化逻辑统一在 `applyMainAlias`，由 BuildQuery/BuildCount 显式调用**，故上表所有 BuildQuery/BuildCount 行自动一致受益。BuildUpdate/BuildDelete 不调用 `applyMainAlias`，结构性保证写路径不物化。

## Acceptance Criteria

> 每条 AC 含具体输入值 + 具体可观测输出，1:1 对应测试函数。方言用 SQLite in-memory（与现有测试一致）。
> 测试模型用 `Closure{AncestorID, DescendantID, Depth uint}`（`insert_select_test.go` 已定义，`TableName()` 返回 `"closure"`，主键 `D=uint`，**无 deleted_at 字段**）。
> **AC-1/AC-2/AC-4/AC-6/AC-10 一律真实执行打库**（非仅 ToSQL 字符串断言）。SQL 文本断言一律用 `strings.Contains`/`NotContains`（Go RE2 **不支持** lookahead `(?!)`，禁用）。

- **AC-1（复现 → 修复，List 真实执行）**：`closure` 含 1 行 `{ancestor_id:1, descendant_id:5, depth:0}`。
  `q, m := r.NewQueryAs(ctx, "ext")`；`q.Select(&m.AncestorID).Eq(&m.DescendantID, 5)`；执行 `r.List(q)`。
  **修复前（红）**：`r.List(q)` 返回 error 含 `no such column: ext.ancestor_id`。
  **修复后（绿）**：返回 `([]Closure{{AncestorID:1,...}}, nil)`，长度 1；`q.ToSQL(db)` 含 `Contains("closure\" AS \"ext")`（带方言引号，见 H-3）。
  > TDD：先写「断言 List 成功且返回 1 行」的测试 → 实现前红（报 no such column）→ 实现后绿。
- **AC-2（ToDB 物化真实执行）**：同 AC-1 的 `q`，把 `q.ToDB(db).Find(&rows)` 真实执行（rows 为 `[]Closure`）。
  **修复前**：报 `no such column: ext.ancestor_id`。**修复后**：成功，返回 1 行 `{AncestorID:1}`。
- **AC-3（无别名零回归）**：`q, m := r.NewQuery(ctx)`（无别名）；`q.Select(&m.AncestorID)`。`sql := q.ToSQL(db)` 满足 `Contains(sql, "FROM \`closure\`")` 且 `!Contains(sql, " AS ")`（用 Contains + NotContains 组合，**不用** lookahead 正则）；`r.List(q)` 正常返回。
- **AC-4（Table override 组合，真实执行）**：先建表 `closure_2024`（同 Closure schema）并播种 1 行 `{ancestor_id:7, descendant_id:5, depth:0}`。`q, m := r.NewQueryAs(ctx, "ext")`；`q.Table("closure_2024").Select(&m.AncestorID).Eq(&m.DescendantID, 5)`；`r.List(q)` 真实执行成功返回该行（`AncestorID==7`）；`q.ToSQL(db)` 含 `Contains("closure_2024\" AS \"ext")`（显式 `Table()` 覆盖值优先于 T 反射表名）。
- **AC-5（Clear 重置，含内部字段断言）**：`q, _ := r.NewQueryAs(ctx, "ext")`；`q.Clear()` 后：(a) 内部字段 `q.mainAlias == ""` **且** `q.mainAliasTable == ""`（package 内测试可访问）；(b) `q.ToSQL(db)` 满足 `!Contains(sql, "AS \"ext\"")`。
- **AC-6（自连接主+副别名真实执行，含字段指针路径，解锁 Round 3b）**：`closure` 含行 `{1,5,0}`、`{5,7,0}`。
  `q, ext := r.NewQueryAs(ctx, "ext")`；`sub := As[Closure](q, "sub")`；
  `q.CrossJoinAs(sub).WhereRaw("sub.ancestor_id = ?", 5).Eq(&ext.DescendantID, 5)`；
  `q.SelectRaw("ext.ancestor_id").SelectRaw("sub.descendant_id").SelectRaw("ext.depth + sub.depth + 1")`；
  用 `gplus.FindAs[Closure, projRow](r, q, &rows)` 真实执行（`projRow` = `struct{ AncestorID, DescendantID, Depth uint }`，字段名经 GORM snake_case 映射 `ancestor_id`/`descendant_id`/`depth`——**SelectRaw 投影列须与 projRow 字段蛇形名对齐**：`SelectRaw("ext.ancestor_id")` → `AncestorID`，`SelectRaw("sub.descendant_id")` → `DescendantID`，`SelectRaw("ext.depth + sub.depth + 1 AS depth")` → `Depth`（第三列需 `AS depth` 别名才能映射到 `Depth`）)。
  - **字段指针覆盖**：WHERE 用 `Eq(&ext.DescendantID, 5)`（经 `resolveColumnName`→`"ext".descendant_id` 带引号），验证字段指针解析的别名引号与 FROM 的 `"closure" AS "ext"` 引号一致。
  - **可观测输出**：`q.ToSQL(db)` 满足 `Contains("closure\" AS \"ext")`（FROM 主表**带引号**）**且** `Contains("CROSS JOIN closure AS sub")`（JOIN 副表由 `appendJoinAsNoOn` 生成，**不带引号**，两处断言形态不同）；`FindAs` 执行成功，`rows` 长度 1 且 `rows[0] == projRow{AncestorID:1, DescendantID:7, Depth:1}`。
  此条直接执行 scenario 2 源 query 形态，证明 Part A 已解除 Part B 阻塞（不经 InsertSelect）。
- **AC-7（子查询零污染，C-1 回归守卫）**：`outer, ou := r.NewQuery(ctx)`；`sub, su := SubQuery[Closure](outer)`（sub 主别名 = 表名 `"closure"`，构造器内已清空 mainAlias）；`sub.Select(&su.AncestorID).Eq(&su.DescendantID, 5)`；`outer.InSub(&ou.AncestorID, sub)`。`sql := outer.ToSQL(db)` 满足 `!Contains(sql, "closure AS closure")` 且 `!Contains(sql, "closure\" AS \"closure")`（子查询不被主别名物化波及）。
- **AC-8（写路径不物化，守卫型，H-2）**：`q, m := r.NewQueryAs(ctx, "ext")`；`q.Eq(&m.DescendantID, 5)`；`r.DeleteByCondTx(q, nil)`（签名 `(q *Query[T], tx *gorm.DB) (int64,error)`，ctx 取自 `q.Context()`）真实执行返回 `(1, nil)`；该路径生成的 SQL **不含** `AS ext`（`DELETE FROM closure AS ext` 在 MySQL/PG 非法）。
  > **守卫型 AC（非复现型）**：BuildDelete 不调 `applyMainAlias`，实现前后都不含 `AS ext`，不要求先红后绿；本 AC 锁死「写路径永不物化主别名」防回归。
- **AC-9（DataRule+自连接 ambiguous 已知限制，验证型）**：`closure` 含若干行；ctx 注入 `DataRule{Column:"depth", Condition:">=", Value:"0"}`；`q, ext := r.NewQueryAs(ctx, "ext")` + `As[Closure](q,"sub")` + `CrossJoinAs` 自连接。真实执行 `r.List(q)`。**断言**：返回 error 且错误串含子串 `ambiguous`（锁定「DataRule 裸列名在多别名自连接下 ambiguous」为已知限制——用户须在 `DataRule.Column` 自带 `ext.` 前缀规避，Round 3a 不负责加前缀）。
  > plan 阶段须用一次性探针确认 SQLite 对此场景的确切错误文本（预期含 `ambiguous`），据此**定死**单一断言；若实测**不报** ambiguous，则 AC-9 改为断言「返回行集 = DataRule 过滤后预期」并把已知限制降级为文档说明。AC 在 plan 阶段定死，不在测试期二选一。
- **AC-10（COUNT/Page 路径真实执行，BuildCount 物化）**：`closure` 含 2 行 `{1,5,0}`、`{2,5,0}`。`q, m := r.NewQueryAs(ctx, "ext")`；`q.Eq(&m.DescendantID, 5)`。
  - `r.Count(q)` 真实执行返回 `(2, nil)`；`q.ToCountSQL(db)` 含 `Contains("closure\" AS \"ext")`（证明 BuildCount 路径 `Model(new(T))+db.Table("...AS...")` 下 FROM 别名生效，COUNT 与 SELECT 表名一致）。
  - `list, total, err := r.Page(q, false)` 返回 `total==2`、`len(list)==2`（验证 Page 的 COUNT 段 + 数据段在 `Model+Session{}` clone=2 下别名一致，total 与 list 不错配）。

## Architecture

### 变更面（最小）

| 文件 | 变更 |
|---|---|
| `builder.go` | `ScopeBuilder` 新增 `mainAlias string` + `mainAliasTable string`；**新增方法 `applyMainAlias(db, qL, qR) *gorm.DB`**（含 `validTableName` 守卫）；`BuildQuery`/`BuildCount` 在 `applyBaseTable` 后显式调用 `applyMainAlias`；**`BuildUpdate`/`BuildDelete` 不调用**（结构性保证写路径不物化）；`Clear()` 重置两字段；新增 `validTableName` 正则 |
| `query.go` | `NewQueryAs[T]`：`aliasNameRegexp.MatchString(alias)` 合法时才写 `q.mainAlias=alias`、`q.mainAliasTable=q.mainTableName()` |
| `subquery.go` | `SubQuery`/`SubQueryAs` 调 `NewQueryAs` 后立即 `sub.mainAlias=""`、`sub.mainAliasTable=""`（子查询靠 ToDB 的 `Model()` 注入 FROM，不经 applyMainAlias 物化） |

> 不新增 sentinel 错误（别名非法仍走现有 `ErrAliasInvalidName`；表名非法见 Error Handling）。不新增公开 API。`applyBaseTable` 签名**保持原样**（`applyBaseTable(db)`），零回归。

### 数据流

```
NewQueryAs[T](ctx, alias)
  → As[T](q, alias)                              // 现有：注册 aliases map（列解析）
  → if aliasNameRegexp.MatchString(alias):        // 仅合法别名才物化 FROM
        q.mainAlias = alias
        q.mainAliasTable = q.mainTableName()       // 复用现有方法（query.go:1268）

SubQuery[X](outer) / SubQueryAs[X](outer, alias)
  → sub, x := NewQueryAs[X](ctx, ...)
  → sub.mainAlias = ""; sub.mainAliasTable = ""    // C-1 修复：子查询不物化 FROM

BuildQuery():                                       // SELECT 路径
  db = applyBaseTable(db)                            // 现有，未改
  db = applyMainAlias(db, qL, qR)                    // 新增，显式调用
  db = applySelects(...) ...
BuildCount():                                       // COUNT 路径
  db = applyBaseTable(db)
  db = applyMainAlias(db, qL, qR)                    // 新增，显式调用
  ...
BuildUpdate() / BuildDelete():                      // 写路径——不调用 applyMainAlias（结构性禁物化）

applyMainAlias(db, qL, qR):
    if b.mainAlias == "" { return db }                                   // 无别名零影响
    table := b.tableName ; if table == "" { table = b.mainAliasTable }   // Table() 覆盖优先
    if !validTableName.MatchString(table):                              // M-1 注入守卫
        return db   // 校验失败：不物化（builder 错误由 GetError 在 repo 方法前置拦截）
    return db.Table(qL+table+qR + " AS " + qL+b.mainAlias+qR)            // 机制1
```

### 关键设计决策（含两轮审计回应）

- **C-1（子查询零污染）**：`SubQuery`/`SubQueryAs` 复用 `NewQueryAs` 入口（subquery.go:43,64）。修复在两构造器内清空 `mainAlias`——子查询的 FROM 由 `ToDB` 的 `Model(getModelInstance[T]())` 注入，无需 applyMainAlias 物化。顶层 `NewQueryAs`（含 Round 3b 的 InsertSelect 源 query，`outerQueryRef==nil`）照常物化。全仓 grep 确认 `NewQueryAs` 仅 4 个调用点（用户直调、`Repository.NewQueryAs` 透传、`SubQuery`、`SubQueryAs`），无第 5 个隐藏入口。AC-7 守此。
- **H-2（结构性禁写路径物化）**：`DELETE FROM closure AS ext`/`UPDATE closure AS ext` 在 MySQL/PG 多方言非法。物化抽成独立方法 `applyMainAlias`，**只由 BuildQuery/BuildCount 显式调用**，BuildUpdate/BuildDelete 根本不调用——「写路径不物化」是结构性保证（不调用即不可能物化），而非 bool 传参纪律。`DeleteByCondTx(q, tx)` 接受 `Query` 走 BuildDelete，主别名 Query 传它不会物化。AC-8 守此。
- **H-3（FROM 引号形态定死）**：FROM 用 `qL+table+qR + " AS " + qL+alias+qR`（带方言引号，如 SQLite `"closure" AS "ext"`），与 `quoteColumn` 对别名引用的转义同源（同一次 `getQuoteChar`）。`applyMainAlias(db, qL, qR)` 收 qL/qR（复用 Build* 入口已算的引号，与 `applyWhere(db,qL,qR)` 签名惯例一致）。**注意**：副别名 JOIN 由 `appendJoinAsNoOn`（query.go:1202）生成 `CROSS JOIN closure AS sub`**不带引号**——主表 FROM 带引号、JOIN 副表不带引号，两处断言形态不同（AC-6 已区分）。SQLite 容忍混用。
- **mainAliasTable 冗余说明（MEDIUM-1 回应）**：`mainAliasTable` 的值在 `aliases[mainAlias].typ`（aliasEntry.typ）已隐含，但 `ScopeBuilder` 是非泛型基类、拿不到 `queryCore.aliases` map，故 `applyMainAlias` 无法像 `appendJoinAs`（query.go:1164 实时 `aliasSchemaTableName(typ)`）那样实时解析，只能由有 T 的 `NewQueryAs` 预存字符串。这是抽象边界的必然妥协，非疏忽。
- **别名合法性前置（code-reviewer 回应）**：`As` 不返回 ok 布尔；`NewQueryAs` 用 `aliasNameRegexp.MatchString(alias)` 显式二次判断，合法才写 `mainAlias`，避免非法别名（如 `"1bad"`）被拼进 `db.Table("... AS 1bad")` 生成语法错误 SQL。

### 物化机制（实测背书，机制1）

探针（SQLite 真实执行，已删）证实 `db.Table("closure AS ext")` 生成 `FROM closure AS ext`，GORM **原样透传不误转义**，`"ext".col` 引用 + 真实执行成功；与 `db.Joins("JOIN closure AS sub ON ...")` 同时使用时两者共存、scenario 2 形态执行返回正确行。故复用现有 `db.Table()` 路径即可，无需 `clause.From`。

> **实现期探针清单（HIGH-1 + 两轮回应，写进 plan）**：孤立 `db.Table` 探针不足以覆盖生产路径的 `db.Table()` vs `Model()`/`Find(dest)` FROM 优先级。实现期须用一次性探针**分别**验证以下路径下 FROM 均含 `AS ext`：
> 1. `List`/`ListTx`（repository.go:275，**不调 `.Model()`**，靠 `Find(&[]T)` dest 反射）
> 2. `ToSQL`（debug.go:24，`.Model(new(T))`）/ `ToDB`（query.go:323，`.Session(NewDB:true).Model()`）
> 3. `Count`（repository.go，`.Model(new(T)).Scopes(BuildCount()).Count()`）
> 4. `Page`（repository.go:337，`Model(new(T)).Session(&gorm.Session{})` **clone=2** + BuildCount/BuildQuery 两段，COUNT 与 SELECT 表名须一致）
> 5. `First` 路径（GetOne/Last/GetByLock/FirstOrCreate/FirstOrUpdate 查询段，`.First()` 追加 `ORDER BY pk LIMIT 1`，验证裸主键 `id` 在 `FROM closure AS ext` 下可解析）
> 6. `FindAs`（find_as.go:48，`Model(new(T)).Scopes(BuildQuery()).Find(dest)`，AC-6 用此路径）
>
> GORM 中 `Statement.Table != ""`（`db.Table()` 设过）优先于 Model/dest 反射，预期各路径一致，但须实测确认（尤其 ToDB 的 `Session{NewDB:true}` clone=1 与 Page 的 `Session{}` clone=2 后 Table 是否保留）。
> **诚实标注**：已删探针只实测了**无引号** `db.Table("closure AS ext")` 可执行；H-3 定的**带引号** `db.Table(\`"closure" AS "ext"\`)` 形态**尚未实测**。plan 阶段须探针确认带引号形态在 SQLite 真实执行成功；若被 GORM 误转义/执行失败，则退回无引号形态并重评 R1。

## Error Handling

- **别名非法**（不符 `aliasNameRegexp`）：现有 `As` 累积 `ErrAliasInvalidName` 不变；`NewQueryAs` 因 `MatchString` 失败**不写** `mainAlias`，避免给坏别名物化 FROM。
- **表名非法**（M-1 注入守卫）：`b.tableName`（用户 `Table(name)` 入参）在 `applyMainAlias` 用 `validTableName` 正则校验，不符则**不物化**（保守跳过；表名异常本就会在 GORM 执行层报错）。防止 `Table("x\"; DROP--")` 经 `qL+table+qR` 拼接被引号提前闭合注入。
  - `validTableName = ^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)?$`（**单点**，与 `validDataRuleColumn` 完全同源：允许 `closure`/`closure_2024`/`schema.table`，拒绝多段 `a.b.c`、引号、空格、分号、空串、数字开头）。**收紧为单点的理由**：多段表名经 `qL+table+qR` 会拼成 `"a.b.c"`（整串一对引号）而非正确的 `"a"."b"."c"`，引号位置错误；且无 AC 覆盖多段表名（AC-4 仅用单段 `closure_2024`），属 YAGNI，故不支持多段。
- **无别名**（`NewQuery`）：`mainAlias == ""`，`applyMainAlias` 首行返回，零行为变化（AC-3）。

## Testing

- 新增测试文件 `main_alias_from_test.go`（package gplus，可访问未导出 `mainAlias`/`mainAliasTable` 字段与 `As`/`SelectRaw`/`validTableName` 等）。
- AC-1..AC-10 各对应一个测试函数，命名描述行为（如 `TestMainAlias_list_with_alias_emits_from_as`），AAA 结构。
- 真实执行打库：AC-1/AC-2/AC-4/AC-6/AC-8/AC-9/AC-10；SQL 文本断言（`Contains`/`NotContains`，**禁用 lookahead 正则**）：AC-3/AC-5/AC-7。
- First 路径（GetOne/Last/GetByLock/FirstOrCreate/FirstOrUpdate）的主别名物化**由 plan 阶段探针验证**（物化逻辑统一在 BuildQuery/BuildCount，AC-1/2/10 已覆盖两条 Build 路径），不为每个 First 方法单独写 AC（避免 AC 膨胀，符合最小验证）。
- 覆盖率门禁 ≥ 80%（项目当前 95.0%，不得回退）。
- 回归：全量 `go test ./...` 通过；尤其 `alias_*_test.go`、`query_newqueryas_test.go`、`query_subquery*_test.go`、`*_integration_test.go`（多在 build tag/env 后，本地 SQLite 路径必绿）。回归面低：`applyMainAlias` 仅在 `mainAlias != ""` 触发，所有 `NewQuery`（无别名）路径首行返回零影响；子查询因 C-1 清空 mainAlias，FROM 不变。

## 显式排除（Out of Scope）

- **NewUpdaterAs**：不新增（无调用点，YAGNI）。写路径 `AS` 多方言不可移植，故 BuildUpdate/BuildDelete 结构性不调 `applyMainAlias`。
- **副别名物化路径**：不改（现有 `applyJoins` 正确）。
- **子查询主别名物化**：`SubQuery`/`SubQueryAs` 主别名仅供列解析，FROM 不物化（清空 mainAlias）。`SubQueryAs("o")` 自定义别名在真实执行下的 FROM 别名（`FROM t AS o`）**仍不支持**——属既有限制（修复前 SubQueryAs 主别名同样从不物化 FROM，清空 mainAlias 是维持「原本就坏、无调用点触发真实执行」原状，未新引入破绽）。本轮不修。
- **First 路径单独 AC**：物化逻辑统一在 BuildQuery/BuildCount，First 路径（GetOne/Last/GetByLock/FirstOrCreate/FirstOrUpdate）的主别名物化由探针验证，不单列 AC。`.First()` 追加 `ORDER BY pk LIMIT 1` 的裸主键在主别名 FROM 下若异常，记为已知风险（plan 探针先确认）。
- **多段表名（`a.b.c`）**：`validTableName` 单点，不支持（引号位置 + YAGNI）。
- **DataRule 列加别名前缀（AC-9 已知限制）**：主别名 + DataRule 注入列 + 自连接时，DataRule 生成裸列名在多别名下 ambiguous；用户须在 `DataRule.Column` 自带 `ext.` 前缀规避。Round 3a 不修 DataRule 系统。InsertSelect 源 query 不注入 DataRule（Round 2 已定），不影响 Round 3b。
- **InsertSelect scenario 2 本体**：Round 3b（依赖本轮合入）。
- **MySQL/PG 真机方言验证**：本轮以 SQLite 为准（与现有基线一致）；`getQuoteChar` 已覆盖各方言引号，记为已知风险。

## 已知风险

- **R1（跨方言引号一致性）**：FROM 的 `qL+table+qR AS qL+alias+qR` 与 SELECT/WHERE 中 `quoteColumn` 对别名引用的转义须一致（同一 `getQuoteChar` 来源，已在 H-3 锁定）。SQLite 已实测（无引号形态）；带引号形态 + MySQL/PG/DM/Oracle 待 plan 探针/真机。AC-6 用字段指针路径覆盖 `resolveColumnName`→`quoteColumn` 与 FROM 别名引号端到端一致性。
- **R2（既有 e2e 断言变化）**：`alias_datarule_e2e_test.go` 等 string-contains 断言修复后仍 PASS（FROM 新增 `AS ext` 不破坏 `Contains("boss")`）；子查询测试因 C-1 修复（mainAlias 清空）FROM 不变，零影响。须全量回归确认。
- **R3（软删除模型 deleted_at 前缀）**：Closure 模型无 `deleted_at`，AC 不涉软删除。带软删除字段的模型在主别名 + 自连接下，GORM 自动 `deleted_at IS NULL` 是否带别名前缀未验证——记为已知风险，下游 InsertSelect scenario 2（Round 3b）若用软删除 closure 表须实测。
- **R4（First 路径裸主键）**：`.First()` 追加 `ORDER BY pk LIMIT 1` 的裸主键 `id`/`WHERE id=?` 在 `FROM closure AS ext` 下能否解析（单表无歧义预期 OK，自连接下未涉）——plan 探针先确认。

## 审计修正记录

### 第一轮（4 视角：architect/code-reviewer/security/红队）

| 编号 | 级别 | 来源 | 修正 |
|---|---|---|---|
| C-1 | CRITICAL | code-reviewer + 红队（交叉验证，已亲核 subquery.go:43,64） | SubQuery/SubQueryAs 清空 mainAlias；新增 AC-7 |
| H-2 | HIGH | 红队 | 写路径禁物化；新增 AC-8 |
| H-3 | HIGH | architect + 红队 | FROM 引号形态定死 |
| H-1d | HIGH | 红队 | DataRule+自连接 ambiguous 文档化；AC-9 |
| HIGH-1 | HIGH | architect | 实现期多路径探针清单 |
| HIGH(别名合法性) | HIGH | code-reviewer | NewQueryAs 用 aliasNameRegexp 前置判断 |
| M-1 | MEDIUM | security | applyMainAlias 加 validTableName 守卫 |

### 第二轮（验证修正 + 找新洞）

| 编号 | 级别 | 来源 | 修正 |
|---|---|---|---|
| 物化实现 | HIGH | architect | **bool 参数 → 结构方案 `applyMainAlias` 独立方法**（写路径不调用，结构性禁物化）；applyBaseTable 保持原签名 |
| COUNT/Page 缺 AC | HIGH | architect + 红队 | **新增 AC-10**（Count/Page 真实执行，BuildCount 物化） |
| First 路径未覆盖 | HIGH | architect | 探针验证 + 显式排除声明 + R4 |
| AC-6 Scan 零值陷阱 | HIGH | code-reviewer | 改用 `FindAs` + projRow 字段名对齐（`AS depth`）+ 断言具体值 |
| FindAs「不存在」误判 | HIGH | 红队（find_as.go:48 实存） | 订正；AC-6 用 FindAs（顺带覆盖 Model+BuildQuery 组合） |
| AC-3 lookahead 正则 | HIGH | code-reviewer | Go RE2 不支持 `(?!)`；改 Contains + NotContains |
| AC-6 引号形态 | HIGH | code-reviewer + 红队 | FROM 带引号 vs JOIN 不带引号，AC-6 明确区分 |
| AC-8 无法先红后绿 | MEDIUM | code-reviewer | 标注守卫型（非复现型） |
| 读方法→Build 对照表 | MEDIUM | architect | 新增对照表（Exists 走 BuildQuery 非 BuildCount） |
| AC-5 漏 mainAliasTable | MEDIUM | code-reviewer | AC-5(a) 补 `mainAliasTable==""` |
| Page Session clone=2 | MEDIUM | architect | 探针清单补第 4 条 |
| validTableName 多点 bug | LOW | 红队 + code-reviewer | 收紧单点 `?`（引号位置 + YAGNI），与 validDataRuleColumn 真同源 |
| AC-7 断言方式 | LOW | code-reviewer | 明确 `!Contains("closure AS closure")` |
| AC-9 措辞歧义 | LOW | code-reviewer | 「软删除无关行」→「若干行」 |
| L2-1 签名笔误 | LOW | code-reviewer + 红队 | AC-8/正文统一 `DeleteByCondTx(q, tx)` 真实签名 |
