# 主别名 FROM 物化 — Round 3a（InsertSelect scenario 2 前置 bug fix）

> **状态**：可进 plan — 2026-06-04 brainstorm 完成，实测推翻原始前提后定稿。
>
> **拆轮背景**：本特性是 InsertSelect scenario 2（自连接 `INSERT...SELECT...JOIN`，见 `2026-06-04-insert-select-design.md` Round 3）的**独立先行前置**。Round 3a（本文档）先修主别名 FROM 物化的 latent bug；Round 3b 再做 InsertSelect scenario 2，复用 Round 2 现有 `InsertSelect` 入口（源 query 用 `NewQueryAs+CrossJoinAs+WhereRaw` 表达即可，无需新 API）。

> **日期**：2026-06-04
> **范围**：修 `ScopeBuilder.applyBaseTable` 让 `NewQueryAs(alias)` 设置的主表别名物化进 FROM 子句（`FROM <table> AS <alias>`），使引用主别名的 SELECT/WHERE 在真实执行（List/ToDB/ToSQL）下合法。

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

修 `applyBaseTable`：`NewQueryAs` 设置主别名后，BuildQuery/Count/Update/Delete 全路径发射 `FROM <table> AS <alias>`，使主别名查询在真实执行下合法。**不扩大范围**：不新增 `NewUpdaterAs`（YAGNI，无调用点）；不改副别名物化路径。

## Acceptance Criteria

> 每条 AC 含具体输入值 + 具体可观测输出，1:1 对应测试函数。方言用 SQLite in-memory（与现有测试一致）。
> 测试模型用 `Closure{AncestorID, DescendantID, Depth uint}`（`insert_select_test.go` 已定义，`TableName()` 返回 `"closure"`，主键 `D=uint`）。bug 类先写复现红测试，再加修复让其绿。

- **AC-1（复现 → 修复，List 真实执行）**：`closure` 含 1 行 `{ancestor_id:1, descendant_id:5, depth:0}`。
  `q, m := r.NewQueryAs(ctx, "ext")`；`q.Select(&m.AncestorID).Eq(&m.DescendantID, 5)`；执行 `r.List(q)`。
  **修复前**：返回 error 含 `no such column: ext.ancestor_id`（红）。
  **修复后**：返回 `([]Closure{{AncestorID:1,...}}, nil)`，长度 1；生成 SQL 的 FROM 含 `closure AS ext`。
- **AC-2（ToDB 物化真实执行）**：同 AC-1 的 `q`，`q.ToDB(db)` 作为外层 `NotIn`/`In` 子查询或直接 `.Find(&rows)` 真实执行。
  **修复前**：报 `no such column: ext.ancestor_id`。**修复后**：执行成功，返回 1 行。
- **AC-3（无别名零回归）**：`q, m := r.NewQuery(ctx)`（无别名）；`q.Select(&m.AncestorID)`。`q.ToSQL(db)` 生成 FROM = 裸 `closure`（**不含 ` AS `**，不含任何别名后缀）；`r.List(q)` 正常返回。
- **AC-4（Table override 组合）**：`q, m := r.NewQueryAs(ctx, "ext")`；`q.Table("closure_2024").Select(&m.AncestorID)`。`q.ToSQL(db)` 的 FROM 含 `closure_2024 AS ext`（显式 `Table()` 覆盖值优先于 T 反射表名）。
- **AC-5（Clear 重置）**：`q, _ := r.NewQueryAs(ctx, "ext")`；`q.Clear()` 后 `q.ToSQL(db)` 的 FROM **不含 `AS ext`**（mainAlias/mainAliasTable 已重置，复用 builder 不残留别名）。
- **AC-6（自连接主+副别名真实执行，解锁 Round 3b）**：`closure` 含行 `{1,5,0}`、`{5,7,0}`。
  `q, ext := r.NewQueryAs(ctx, "ext")`；`sub := As[Closure](q, "sub")`；
  `q.CrossJoinAs(sub).WhereRaw("sub.ancestor_id = ?", 5).WhereRaw("ext.descendant_id = ?", 5)`；
  `q.SelectRaw("ext.ancestor_id").SelectRaw("sub.descendant_id").SelectRaw("ext.depth + sub.depth + 1")`；
  经 `FindAs`/`RawScan` 风格真实执行（投影到 `{A,D,Dep uint}` DTO）。
  生成 FROM 含 `closure AS ext` 且 JOIN 含 `closure AS sub`；执行成功返回期望行集（`{ancestor_id:1, descendant_id:7, depth:1}`）。
  此条直接执行 scenario 2 源 query 形态，证明 Part A 已解除 Part B 阻塞（不经 InsertSelect）。

## Architecture

### 变更面（最小）

| 文件 | 变更 |
|---|---|
| `builder.go` | `ScopeBuilder` 新增 `mainAlias string` + `mainAliasTable string` 两字段；`applyBaseTable` 增加主别名分支；`Clear()` 重置两字段 |
| `query.go` | `NewQueryAs[T]` 注册别名后写入 `q.mainAlias = alias`、`q.mainAliasTable = aliasSchemaTableName(typeof T)` |

> 不新增 sentinel 错误（别名非法仍走现有 `ErrAliasInvalidName` 累积路径）。不新增公开 API。

### 数据流

```
NewQueryAs[T](ctx, alias)
  → As[T](q, alias)                          // 现有：注册 aliases map（列解析）
  → q.mainAlias = alias                       // 新增
  → q.mainAliasTable = aliasSchemaTableName(reflect.TypeOf(*new(T)))  // 新增：裸表名

BuildQuery()/BuildCount()/BuildUpdate()/BuildDelete()
  → applyBaseTable(db):
       if b.mainAlias != "":
           table := b.tableName ; if table == "" { table = b.mainAliasTable }   // Table() 覆盖优先
           qL, qR := getQuoteChar(db)
           db = db.Table(qL+table+qR + " AS " + qL+alias+qR)                     // 机制1，已实测
       else if b.tableName != "":
           db = db.Table(b.tableName)                                            // 现有路径，零回归
       if b.unscoped { db = db.Unscoped() }
       return db
```

> `getQuoteChar` 当前在 `applyBaseTable` 外（各 Build* 入口）计算并下传 qL/qR；`applyBaseTable` 现签名为 `applyBaseTable(db)`，需新增 qL/qR 参数或内部再调 `getQuoteChar(db)`。实现期二选一（探针确认 `db.Table` 透传不误转义后定）。

### 物化机制（实测背书，机制1）

探针（SQLite 真实执行，已删）证实 `db.Table("closure AS ext")` 生成 `FROM closure AS ext`，GORM **原样透传不误转义**，`"ext".col` 引用 + 真实执行成功；与 `db.Joins("JOIN closure AS sub ON ...")` 同时使用时两者共存、scenario 2 形态执行返回正确行。故复用现有 `b.tableName → db.Table()` 路径即可，无需 `clause.From`。

## Error Handling

- 别名非法（不符 `aliasNameRegexp`）：现有 `As` 累积 `ErrAliasInvalidName` 不变；此时 `As` 返回规范单例 fallback，`NewQueryAs` 仍会写 `mainAlias`——但 builder 错误会在 `GetError()` 提前拦截，不会执行到 SQL。**实现注意**：`mainAlias` 仅在 alias 合法时写入（避免给坏别名物化 FROM）。
- 无别名（`NewQuery`）：`mainAlias == ""`，走现有 `b.tableName` 分支，零行为变化（AC-3）。

## Testing

- 新增测试文件 `main_alias_from_test.go`（package gplus，可访问未导出 `mainAlias` 字段与 `As`/`SelectRaw` 等）。
- AC-1..AC-6 各对应一个测试函数，命名描述行为（如 `TestMainAlias_list_with_alias_emits_from_as`），AAA 结构。
- AC-1/AC-2/AC-6 用真实执行（非仅 ToSQL 字符串断言），确保不重蹈「string-contains 掩盖 latent bug」覆辙。
- 覆盖率门禁 ≥ 80%（项目当前 95.0%，不得回退）。
- 回归：全量 `go test ./...` 通过；尤其 `alias_*_test.go`、`query_newqueryas_test.go`、`mysql/pg/dm/oracle` 集成测试（多在 build tag/env 后，本地 SQLite 路径必绿）。

## 显式排除（Out of Scope）

- **NewUpdaterAs**：不新增（无调用点，YAGNI）。`applyBaseTable` 通用逻辑使 Update/Delete 路径自动具备能力，但无入口触发。
- **副别名物化路径**：不改（现有 `applyJoins` 正确）。
- **InsertSelect scenario 2 本体**：Round 3b（依赖本轮合入）。
- **MySQL/PG 真机方言验证**：本轮以 SQLite 为准（与现有基线一致）；`getQuoteChar` 已覆盖各方言引号，记为已知风险。

## 已知风险

- **R1（跨方言引号一致性）**：FROM 的 `qL+table+qR AS qL+alias+qR` 与 SELECT/WHERE 中 `quoteColumn` 对别名引用的转义须一致（同一 qL/qR 来源 `getQuoteChar`）。SQLite 已实测；MySQL/PG/DM/Oracle 引号字符已在 `getQuoteChar` 覆盖，降低风险。
- **R2（既有 e2e 断言变化）**：`alias_datarule_e2e_test.go` 等 string-contains 断言修复后仍 PASS（FROM 新增 `AS ext` 不破坏 `Contains("boss")`）；需全量回归确认无断言因 SQL 文本变化而失败。
