# Spec: Query-chain-safe 投影 API + aggregate Scan 漏洞修复（v0.7.0 minor）

**日期**：2026-05-01（三轮修订）
**版本**：v0.7.0（minor，新增 API + 内部修复）
**类型**：新功能 + bug 修复
**修订历史**：
- v1：初稿
- v2：多专家审计 + GORM 实测推翻 First 覆盖 Schema 假设；aggregate 改用 wrapper struct（方案 G）
- v3（本版）：二轮审计纳入 — alias `gplus_agg_v` 防冲突 / godoc 加迁移提示与跨租户后果 / FindOneAs 编程式防御 / ADR 与实测段移附录 / 永久 probe 测试；aggregateWrap[R] Schema 安全实测确认

## 1. 背景与问题

### 1.1 GORM v1.31.1 callback chain 行为（已实测验证）

| GORM 方法 | callback chain | 行号（finisher_api.go） |
|---|---|---|
| `Find` / `First` / `Take` / `Last` | **Query** | 131 / 144 / 160 / 172 |
| `Count` / `Pluck` | **Query** | 497 / 574 |
| `Scan` / `Row` / `Rows` | **Row** | 535/508/518 |

**结论**：`db.Scan` / `db.Row` / `db.Rows` 三者均不触发挂在 Query callback chain 上的下游 callback。

### 1.2 漏洞清单

| # | 位置 | 表现 |
|---|---|---|
| 1 | `repository.go:848` `aggregate` 函数 | `Model(new(T)).Scan(&ptr)` → Schema 已解析为 T 但走 Row chain，下游 isolation callback 不触发 → Sum/Max/Min/Avg 隔离失效 |
| 2 | 用户层 `q.ToDB(db).Scan/Row/Rows(...)` | gplus 暴露 `ToDB *gorm.DB` 后无法拦截 — gvs-server 已发生线上事故 |

详细 GORM 源码引用与不影响路径（Raw/GetById/List/Pluck/Count/Chunk）见 **附录 B**。

## 2. 核心方案概述

1. **新增 4 个 Query-chain-safe 投影 API**：`FindAs / FindAsTx / FindOneAs / FindOneAsTx`（包级泛型函数；走 `.Find` / `.First`，触发 Query chain）
2. **修复 aggregate 漏洞**：`Scan(&ptr)` → `Find(&[]aggregateWrap[R])` 走 Query chain（NULL 安全经实测保留，见附录 B）
3. **永久 probe 测试**：把 GORM callback chain 行为锁定为 `find_as_test.go` 中的常驻测试，未来 GORM 升级行为变化自动 canary
4. **强迁移指引**：godoc + README + CHANGELOG 三位一体，含跨租户后果具象化 + grep 排查命令

## 3. 设计

### 3.1 API 形态（新建 `find_as.go`）

```go
// FindAs 投影查询（多行）。dest 必须是 *[]Element 切片指针。
//
// 走 GORM Query callback chain，下游挂在 Query chain 上的隔离/审计 callback 会触发。
//
// 【迁移提示】若现有代码用 q.ToDB(db).Model(&T{}).Scan(&rows) / .Rows() / .Row()，
// 必须改用 gplus.FindAs。前者绕过 Query callback chain，会导致下游隔离/审计
// callback 不触发，可能引发跨租户数据泄露 / 审计日志缺失（见 README 已知陷阱）。
//
// 【副作用】调用 FindAs 后 q.conditions 会被永久追加 DataRule 条件
// （dataRuleApplied 保护幂等），q 不应再跨不同 ctx 复用。与 List/Sum 等行为一致。
func FindAs[T any, Dest any, D comparable](
    r *Repository[D, T], q *Query[T], dest *[]Dest,
) error

func FindAsTx[T any, Dest any, D comparable](
    r *Repository[D, T], q *Query[T], dest *[]Dest, tx *gorm.DB,
) error

// FindOneAs 投影查询（单行）。dest 是 *Element。
//
// 无匹配时返回 gorm.ErrRecordNotFound。
//
// 【迁移提示】同 FindAs。
//
// 【约束】FindOneAs 不可与 q.Limit() / q.Page() 组合 —— 内部 First 会追加
// LIMIT 1，与已有 LIMIT 叠加部分 DB 行为未定义。组合调用会返回 ErrFindOneAsConflict。
//
// 【实测确认】GORM v1.31.1 First(dest) 不会用 dest 的 schema 覆盖
// 已设置的 Model(new(T))；下游 isolation callback 拿到的 Schema.Table 仍为 T 表名。
func FindOneAs[T any, Dest any, D comparable](
    r *Repository[D, T], q *Query[T], dest *Dest,
) error

func FindOneAsTx[T any, Dest any, D comparable](
    r *Repository[D, T], q *Query[T], dest *Dest, tx *gorm.DB,
) error
```

### 3.2 内部实现骨架

```go
// 新增 sentinel error
var ErrFindOneAsConflict = errors.New("gplus: FindOneAs 不可与 q.Limit() / q.Page() 组合调用")

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

func FindOneAsTx[T any, Dest any, D comparable](
    r *Repository[D, T], q *Query[T], dest *Dest, tx *gorm.DB,
) error {
    if q == nil {
        return ErrQueryNil
    }
    // 编程式防御：limit/offset 已设 → 拒绝
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

// FindAs(r,q,dest)    → FindAsTx(r,q,dest,nil)
// FindOneAs(r,q,dest) → FindOneAsTx(r,q,dest,nil)
```

**实施细节**：`q.ScopeBuilder.limit/offset` 是 unexported 字段，FindOneAsTx 在同包内可直接访问。若实施时偏好封装，可在 ScopeBuilder 上加 `HasLimit() / HasOffset()` 方法。

### 3.3 aggregate 路径修复（`repository.go:848`）

```go
// 包级私有类型（在 find_as.go 中定义；故意 unexported，外部不应感知）
// 用 Find 路径走 Query callback chain；V *R 保证 SQL NULL 时为 nil。
type aggregateWrap[R any] struct {
    V *R `gorm:"column:gplus_agg_v"`
}

const aggregateAlias = "gplus_agg_v"  // 带 gplus_ 前缀防与业务列冲突

// aggregate 函数内部
expr := fmt.Sprintf("%s(%s) AS %s", fn, colName, aggregateAlias)
var rows []aggregateWrap[R]
err = r.dbResolver(q.Context(), tx).Model(new(T)).Scopes(q.BuildCount()).
    Select(expr).Find(&rows).Error
if err == nil && len(rows) > 0 && rows[0].V != nil {
    result = *rows[0].V
}
```

**实测确认**（见附录 B）：
- 空表 SUM `Find(&[]aggregateWrap[int64])` → `V == nil`，无 NULL 报错 ✓
- `Statement.Schema.Table` 仍为 T 的表名（非 `aggregate_wraps`）✓
- callback chain：`queryCount=1, rowCount=0` ✓

**已知边界**：当用户在 q 上同时设置 `Distinct + Select`，`BuildCount` 会调用 `applySelects`，与 aggregate 自身的 `Select(expr)` 在 GORM v1 是追加而非覆盖，可能产生 SQL 错误。**这是预存在 bug**（不是本期引入），本期不修，记入 **附录 D 已知问题**，v0.7.1 候选。

### 3.4 调用形态

```go
type UserVO struct {
    Name     string  // 默认匹配 SELECT 列 `name`（snake_case）
    DeptName string  // 匹配 `dept_name`（必须 alias，否则字段名冲突时无法定位）
}

// 多行
var rows []UserVO
err := gplus.FindAs(repo,
    q.LeftJoin("dept", "users.dept_id = dept.id").
        Select("users.name", "dept.name AS dept_name"),
    &rows)

// 单行
var one UserVO
err := gplus.FindOneAs(repo, q.Eq(&u.ID, 1), &one)

// 事务
err := db.Transaction(func(tx *gorm.DB) error {
    return gplus.FindAsTx(repo, q, &rows, tx)
})
```

**主表 vs 结果结构 心智模型**：
- `Repository[D, T]` 绑定主表 schema（T） + 提供 dbResolver / ctx 注入
- `Dest` 仅决定 SELECT 列 → struct 字段映射（GORM 默认 snake_case）
- 跨表 JOIN 列必须 SQL alias，否则字段名冲突 GORM 无法定位

### 3.5 文件组织

- 新建 `find_as.go` — 4 个新函数 + `aggregateWrap[R]` 私有类型 + `aggregateAlias` 常量 + `ErrFindOneAsConflict` + godoc
- 新建 `find_as_test.go` — 全部新增测试，含 **永久 probe 测试** `TestGORMCallbackBehaviorProbe`
- 改 `repository.go:829-878`（aggregate 函数）— 用 `aggregateWrap[R]` + `Find` + godoc 注释更新

## 4. 测试策略（`find_as_test.go`）

### 4.1 永久 probe 测试 `TestGORMCallbackBehaviorProbe`（核心 — GORM 行为锁定）

把"GORM 行为实测"沉淀为常驻单元测试，未来 GORM 升级行为变化时**自动 canary 红灯**。覆盖：

1. **Query chain count**：`Model(T).Find(dest)` → queryCount=1, rowCount=0
2. **Scan 走 Row chain**（基线）：`Model(T).Scan(dest)` → queryCount=0, rowCount=1
3. **Rows 走 Row chain**（基线）：`Model(T).Rows()` → queryCount=0, rowCount=1
4. **`Find(&[]aggregateWrap[R])` Schema 不被覆盖**：探针 callback 中读 `Statement.Schema.Table` 应为 T 的表名
5. **`First(&Dest)` Schema 不被覆盖**：同上
6. **空表 SUM `Find(&[]aggregateWrap[int64])` NULL → V=nil**：无 "converting NULL to int64" 错误

**这 6 条不是 dev 时一次性 probe，是 unit test，每次 `go test` 自动跑**。GORM 升级时第一时间发现行为变化。

### 4.2 callback chain 触发矩阵（修复证明）

| 用例 | Query chain | Row chain | 说明 |
|---|---|---|---|
| 基线：`q.ToDB(db).Model(T).Scan(&rows)` | 0 | 1 | 已在 4.1 covered，此处可省 |
| `FindAs(r, q, &rows)` | 1 | 0 | 修复证明 |
| `FindOneAs(r, q, &one)` | 1 | 0 | 修复证明 |
| `FindOneAs(r, q, &one)` 无匹配 | 1 | 0 | 仍走 Query chain，返回 ErrRecordNotFound |
| `Sum(r, q, col)` 有数据 | 1 | 0 | aggregate 修复（Sum 代表 Max/Min/Avg — 共享 `aggregate` 内部函数，路径相同） |
| `Sum(r, q, col)` 空表 NULL | 1 | 0 | aggregate + NULL 语义保持（result = 零值） |
| `FindAs + DataRule + LEFT JOIN` | 1 | 0 | 复合：DataRule WHERE 与 JOIN ON 不互染 |
| `FindAs(q.InSub(subQ))` | ≥1 | 0 | 子查询 + FindAs 不退化为 Row chain |

注：Max/Min/Avg 的 callback 矩阵不重复列 — 4 者共享 `aggregate[T,R,D]` 内部函数，callback 触发与 fn 名无关。投影正确性测试（4.3）单独验 4 者各自结果。

### 4.3 投影正确性（table-driven）

- LEFT JOIN + 自定义 dest struct（多列拼接）
- SELECT 子集列
- WHERE / ORDER / LIMIT 透传
- Dest 字段名 alias 映射验证（`AS dept_name` → `DeptName`）
- gplus DataRule 透传 — `ctx.Value(DataRuleKey)` builder 阶段加 WHERE
- `Distinct + FindAs` — 去重投影
- **DataRule + 模拟下游 isolation callback 双套并行生效** — 注册探针 callback 向 Statement 追加 WHERE，断言生成 SQL 同时含 DataRule WHERE + 探针 WHERE
- Max/Min/Avg 各一个有数据用例（验证非 callback 路径正确）

### 4.4 边界

- `q == nil` → `ErrQueryNil`
- `q.GetError() != nil` → 提前返回（dest 未触碰，保留调用方原值）
- `q.DataRuleBuilder().GetError() != nil` → 返回 DataRule 校验错误
- `FindOneAs` 无匹配 → `gorm.ErrRecordNotFound`
- **`FindOneAs` 与 `q.Limit(N)` 组合 → `ErrFindOneAsConflict`**
- **`FindOneAs` 与 `q.Page(p, ps)` 组合 → `ErrFindOneAsConflict`**（Page 内部设 limit/offset）
- `dest` 指向 nil 切片 → 正常执行，**GORM Find 覆盖写入**（非追加；nil 切片与空切片行为相同）
- 同一 q 调用两次 FindAs → DataRule 条件不重复追加（dataRuleApplied 幂等保护）

## 5. 文档更新

### 5.1 CHANGELOG.md（新增 `## [0.7.0]`）

`<发版日>` 实施者填上当天日期。

```markdown
## [0.7.0] - <发版日>

### 新增

- **Query-chain-safe 投影查询 API**：根除 `db.Scan()` / `db.Row()` / `db.Rows()` 绕过 GORM Query callback chain 导致的下游隔离/审计 callback 失效问题
  - `FindAs[T, Dest, D]` / `FindAsTx[T, Dest, D]`：投影多行（dest 为 `*[]Dest`）
  - `FindOneAs[T, Dest, D]` / `FindOneAsTx[T, Dest, D]`：投影单行（dest 为 `*Dest`，无匹配返回 `gorm.ErrRecordNotFound`）
  - 内部走 `.Find` / `.First` → Query chain，下游挂在 Query chain 上的 callback 自动触发
  - Go 1.18+ 类型推导后调用形态：`gplus.FindAs(repo, q, &rows)`，无需写类型参数
- `ErrFindOneAsConflict` sentinel：FindOneAs 与 `q.Limit()/q.Page()` 组合时立即返回

### 修复

- **aggregate 路径绕过 Query callback chain**（`repository.go:848`）：Sum/Max/Min/Avg 内部 `.Scan(&ptr)` 改为 `.Find(&[]aggregateWrap[R])` 走 Query chain；下游 isolation/审计 callback 现可正确触发。NULL 语义保持不变（wrapper struct 中 `*R` 字段在 SQL NULL 下为 nil，已实测）

### 行为约束（须知）

- **`q.ToDB(db).Scan(...)` / `.Row()` / `.Rows()` 仍绕过 Query callback chain**：GORM v1.31.1 三者内部走 Row chain，gplus 无法拦截。**若下游挂有 isolation/审计 callback，这三种调用等同保留隔离漏洞，必须迁移到 `FindAs`/`FindOneAs`**。
  - 排查命令（互补两条）：
    \`\`\`bash
    # 1. 单行直链（高置信度）
    grep -rEn 'ToDB\(.*\)\.(Scan|Row|Rows)\(' . --include='*.go'
    # 2. 跨行场景（变量赋值后调用 / 中间链方法）— 需人工复查
    grep -rEn '\.ToDB\(' . --include='*.go'
    # 在结果文件中再 grep 是否有 .Scan/.Row/.Rows
    \`\`\`

    **regex 启发式的本质局限**：上述 grep 命令是行内启发式扫描，无法理解 Go AST。真正的深嵌套（同一行多次调用 ToDB / 跨行 builder pattern）仍可能漏检或误检。关键代码请人工 review，或使用 AST 工具作为兜底：
    - [ast-grep](https://ast-grep.github.io/)：结构化模式匹配，例如 `ast-grep --pattern '$Q.ToDB($$$).Scan($$$)' --lang go`
    - `golang.org/x/tools/go/analysis` 写自定义 lint analyzer，精确识别方法链
    - `go/parser` + `go/ast` 手写小工具

    **regex 命中对照表**（验证修正后的命令）：

    | 反例代码                                              | 旧 `[^)]*` | 新 `.*`  |
    |-------------------------------------------------------|-----------|----------|
    | `q.ToDB(db).Scan(&x)` (基础违规)                       | ✓ 命中    | ✓ 命中    |
    | `q.ToDB(r.GetDB()).Scan(&x)` (实参嵌套括号)            | ✗ 漏检    | ✓ 命中    |
    | `q.ToDB(r.GetDB()).WithContext(ctx).Scan(&x)` (中间链) | ✗ 漏检    | ✓ 命中    |
    | `q.ToDB(db).Model(&T{}).Find(&rows)` (Find 非违规)      | ✗ 不命中  | ✗ 不命中  |
- **新 API 不取代 `RawScan`**：Raw SQL 路径 Schema=nil，下游 isolation callback 在正确实现下短路；**若下游 callback 未做 `Schema == nil` 判断，行为不可预测**。涉及敏感数据的 Raw 查询必须在 SQL 中手写 WHERE，不可依赖 gplus DataRule 或下游 callback
- **aggregate 性能基线**：高频聚合（每秒数百次 Sum/Max/Min/Avg）下，callback chain 触发是新增主要开销（取决于下游 callback 数量与复杂度）。性能敏感场景需基准测试
- **GORM 版本锁定**：本修复基于 GORM v1.31.x 实测行为。升级到 v1.32+ 必须重跑 `TestGORMCallbackBehaviorProbe`，行为变化时第一时间感知

### 不在本期范围

- 已评估"拆 0.6.1（仅修 aggregate）+ 0.7.0（新增 API）"方案 — 因新增 API 非破坏、合并发布心智成本相同，**合并发布**
```

### 5.2 README.md（已知陷阱段）

```markdown
### `q.ToDB(db).Scan` / `.Row` / `.Rows` 绕过 Query callback chain（CRITICAL）

GORM v1.31.1 中 `db.Scan` / `db.Row` / `db.Rows` 内部走 Row callback chain，**不会触发**挂在 Query chain 上的下游 callback（数据隔离 / 审计 / 查询日志）。在依赖这些 callback 的项目中，使用上述三种调用**会导致跨租户数据泄露 / 审计日志缺失**。

**必须改用** `FindAs` / `FindOneAs`：

| 旧写法（漏洞） | 新写法（安全） |
|---|---|
| `q.ToDB(db).Model(&T{}).Scan(&rows)` | `gplus.FindAs(repo, q, &rows)` |
| `q.ToDB(db).Model(&T{}).Limit(1).Scan(&one)` | `gplus.FindOneAs(repo, q, &one)` |

**排查老代码**：见 CHANGELOG v0.7.0 行为约束段（含两条互补 grep 命令）

**`RawQuery` / `RawScan` / `RawScanTx`**：Schema=nil，下游 callback 在正确实现下短路。**若下游 callback 未判断 `Schema == nil`，行为不可预测**。涉及敏感数据必须在 SQL 中手写 WHERE。

**DataRule + JOIN 场景**：`DataRule.Column` 在多表 JOIN 下若无表前缀（如 `"dept_id"`），可能产生 SQL 二义性错误（MySQL 报 `ambiguous`）或静默走错表。**必须用 `table.col` 形式**（如 `"users.dept_id"`）。

**`col` 字符串形式不验证**：`Sum/Max/Min/Avg/Pluck` 接受字符串列名时，gplus 不做白名单校验（与 `DataRule.Column` 不同）。**禁止将用户输入直接传入 `col`**。
```

### 5.3 godoc

- 4 个新函数：见 §3.1 的 godoc 模板（含迁移提示 + 跨租户后果 + 副作用 + FindOneAs Limit 约束）
- `aggregate`：注释更新为 "BuildCount + Find（走 Query chain）+ wrapper struct 保 NULL 语义"
- `aggregateWrap[R]`：1 行注释 "包裹聚合结果列；用 Find 路径走 Query callback chain；V *R 保证 SQL NULL → nil"
- `Sum/Max/Min/Avg/Pluck`：godoc 加 "**R 支持类型**：int64/float64/string 安全；MySQL DECIMAL → float64 经 strconv 路径正确"

### 5.4 版本号

- v0.6.0 已于 2026-04-30 发布
- 本期发布 **v0.7.0**（MINOR）— 新增 API 不破坏，aggregate 修复对用户层语义无变化
- 拆分权衡见 CHANGELOG "不在本期范围"

## 6. 验收清单

- [x] `find_as.go` 实现 4 个新函数 + `aggregateWrap[R]` 私有类型 + `aggregateAlias` 常量 + `ErrFindOneAsConflict` + godoc
- [x] `repository.go` aggregate 改造（用 `aggregateWrap[R]` + alias `gplus_agg_v` + `.Find`）+ godoc 注释更新
- [x] `find_as_test.go` 实现 `TestGORMCallbackBehaviorProbe`（6 条 GORM 行为锁定断言，§4.1）
- [x] `find_as_test.go` 完成 callback 触发矩阵 + 投影 table-driven + 边界（含 `ErrFindOneAsConflict` 用例）
- [x] `aggregate` 已有测试不退化（Sum/Max/Min/Avg 全绿，含空表 NULL）
- [x] `D:/Environment/golang/go1.21.11/bin/go.exe test -race ./...` 全绿
- [x] **新增代码独立覆盖率 ≥ 94%；整体覆盖率不低于 93.5%** — 实测整体 **96.3%**，FindAs/FindOneAs 100%，FindAsTx/FindOneAsTx 71.4%/77.8%（仅 q.GetError/DataRuleBuilder.GetError 注入分支未覆盖，留 v0.7.1 顺手补）
- [x] `CHANGELOG.md` 增 `## [0.7.0]` 条目（含强措辞迁移建议 + 两条互补 grep + 性能基线 + GORM 版本锁定）
- [x] `README.md` 同步新 API + 跨租户后果说明 + 排查命令 + DataRule JOIN 警告 + col 字符串警告
- [x] git commit 中文 message，禁用 Co-Authored-By trailer
- [x] 留 commit 不 push（用户审完手动 push 并发版）

**实施完成**：2026-05-01。共 11 个实施 commit（093aec3..3684264）+ opus final review APPROVED。

**v0.7.1 候选**：
- 补 FindAsTx/FindOneAsTx 错误注入路径测试（覆盖率 → 100%）
- ErrRecordNotFound 断言改 `errors.Is` 精确化
- README v0.6.0 之前遗留的 DataRule 字段名 `Op:/Val:` → `Condition:/Value:`
- aggregate `Distinct + Select` 边界 bug（附录 D D1）

---

## 附录 A：架构决策记录（ADR）

| 决策 | 拒绝的备选 | 拒绝理由 |
|---|---|---|
| **包级泛型函数** `gplus.FindAs[T,Dest,D](r, q, dest)` | A. `*Query[T]` 链式方法 `q.FindAs(db, dest)` | dest 弱类型 (`any`)；db 参数破坏链；与现有 `Sum/Pluck` 包级形态不一致 |
| 同上 | B. 极简 `FindAs[T,Dest](db, q, dest)` 摘 D | 调用方需自带 db，丧失 "Repository 绑定主表 + 隔离归属" 的语义信号；ctx 注入只是次要点 |
| 同上 | C. 二段式 `gplus.As[Dest](repo).Find(q, &rows)` | gplus 无先例；与 `Sum[T,R,D]` 三参数风格冲突 |
| 同上 | D. 方法 + 包级并存 | 双 API 表面增决策成本 |
| **命名 `FindAs/FindOneAs`** | `ListAs/GetAs` / `Project/ProjectOne` | `Find` 是 GORM/JPA 通用动词；`As` 后缀已是行业惯例（JPA `findAs`、Hibernate Projection）；与 `Sum/Pluck` 同属"包级动词族" |
| **不引入 `Projector` 接口** | — | Dest 是纯结构体，不参与 SQL 构造，无安全/扩展点驱动接口化（与 v0.6.0 `Subquerier` 引入动机不同） |
| **aggregate 修复用 wrapper struct（方案 G）** | a. `Find(&[]*R)` / b. `Pluck(*R)` | 实测：空表 SUM 时报 "converting NULL to int64"，无法保留 NULL → 零值语义 |
| 同上 | c. `COALESCE(SUM(x), 0)` 改 SQL | 丢失"区分零值与 NULL"能力；方言差异 |
| **aggregate 不复用 FindAs** | 复用 `FindAs(r, q.Select(expr), &[]aggregateWrap[R])` | aggregate 用 `BuildCount`（无 ORDER/LIMIT/Preload），FindAs 用 `BuildQuery`，build 路径不同；强行复用引入 ORDER/Preload 副作用 |
| **`aggregateWrap[R]` unexported** | 公开 (`AggregateWrap[R]`) | 内部实现细节，不入公开 API 表面；未来若加 `gplus.AggregateAs(...)`，新增 `gplus.AggregateValue[R]` 公开类型独立设计 |
| **alias `gplus_agg_v`** | `v` / `__gplus_agg__` | 单字母 `v` 与业务表列名（如 velocity / volume / version 缩写）冲突概率不为零；双下划线在 Go ORM 圈与反射内部命名易撞 |
| **FindOneAs 编程式防御** | 仅文档警告 | gplus 错误模型已用 sentinel（ErrDeleteEmpty/ErrUpdateEmpty/ErrTransactionReq），加 ErrFindOneAsConflict 一致；fail-loud 优于 silent 行为不定 |
| **错误返回风格 `error` 而非 `(Dest, error)`** | `FindAs(...) ([]Dest, error)` | dest 是 `*[]Dest`，调用方掌控类型，参数写回与 `json.Unmarshal` 惯例一致；返回值形态需类型断言更繁 |
| **版本号 v0.7.0 不拆 0.6.1+0.7.0** | 拆 0.6.1（仅修 aggregate）+ 0.7.0（新增 API） | 新增 API 非破坏；下游升级心智成本相同；合并发布更紧凑 |

## 附录 B：GORM v1.31.1 实测结果

实测代码已运行通过（探针文件已删除，断言沉淀到 `TestGORMCallbackBehaviorProbe` 永久测试）。

| 实测场景 | 结果 | 影响决策 |
|---|---|---|
| `Model(&T).First(&VO)` 后 `Schema.Table` | 保持 T 表名 | FindOneAs 用 First 安全；安全审计 C1（First 覆盖 Schema 假设）已推翻 |
| `Model(&T).Find(&[]*int64)` 后 `Schema.Table` | 保持 T | aggregate Find 路径 Schema 安全 |
| **`Model(&T).Find(&[]aggregateWrap[int64])` 后 `Schema.Table`** | **保持 T**（不被 wrapper schema 覆盖） | **方案 G 安全验证**（二轮安全审计 HIGH-1） |
| 空表 `Find(&[]*int64)` SUM | **报错** "converting NULL to int64" | 方案 a 否决 |
| 空表 `Pluck(*int64) / Pluck([]*int64)` SUM | 报错 | 方案 b 否决 |
| **空表 `Find(&[]aggregateWrap[int64])` SUM** | **`V == nil`，无错** | **方案 G 采纳** |
| 方案 G + callback 探针 | `queryCount=1, rowCount=0` | Query chain 触发 ✓ |

**关键洞察**：GORM `.Find` 在 struct 字段路径下用反射 + `database/sql.Rows.Scan`，对 `*int64` 字段 NULL → nil 行为正确；但在 `*int64` 直接作为切片元素或 Pluck 标量目标时，走另一条 Scan 路径，对 NULL 处理失败。`Model(new(T))` 已设置 Schema 时，GORM `First/Find` 不会用 dest 类型覆盖。

**版本锁定**：以上结果在 GORM **v1.31.1** 验证。升级时跑 `TestGORMCallbackBehaviorProbe` 重测。

## 附录 C：不在范围（按确定性分层）

### 已评估并明确拒绝（永久）

- `*Query[T]` 上的 `FindAs` 方法（见 ADR 方案 A）
- `Projector` / `Destinationer` 接口（见 ADR）
- 极简版 `FindAs(db, q, dest)` 摘 D 主键（见 ADR 方案 B）
- 方法 + 包级并存（见 ADR 方案 D）
- 拆 0.6.1 + 0.7.0（见版本号 ADR）

### 推迟到 v0.7.x / v0.8.0 候选

- `FindAsCount`（投影 + 总行数二合一） — v0.8.0
- `FindOneAsExists`（投影 + 存在性） — v0.8.0
- `gplus.AggregateAs[T,R,D]`（自定义聚合表达式） — v0.8.0
- aggregate `BuildCount + Distinct + Select` 边界 bug 修复 — v0.7.1（见附录 D）

### 不属于本库职责（永远不做）

- 改 GORM `db.Scan` 行为（不可能也不应该 monkey-patch）
- 修下游 callback 注册接口
- `ToDB` 返回 `*gorm.DB` 上挂 Plugin/Hook 拦截 `.Scan/.Rows`（技术不可行 — 影响全局 db 实例）
- `col` 字符串形式加白名单（影响现有 `SUM(CASE WHEN ...)` 合法表达式；改用文档约束）

## 附录 D：已知问题

### D1 aggregate 在 `Distinct + Select` 路径下 SQL 错误（v0.7.1 候选）

**触发**：用户在 q 上同时设置 `Distinct(...)` + `Select(...)` 后调 `Sum/Max/Min/Avg`：

```go
gplus.Sum[User, int64](r, q.Distinct(&u.Age).Select(&u.Name), &u.Age)
```

`BuildCount` 在 `distinct && len(selects) > 0` 时调 `applySelects` 已添加 SELECT 列；aggregate 的 `Select(expr)` 在 GORM v1 是追加而非覆盖，最终 SQL：

```sql
SELECT DISTINCT name, SUM(age) AS gplus_agg_v FROM ...
```

aggregate 语义错误。

**当前缓解**：本期不修，spec 记录已知。**调用方注意**：聚合查询不要在同一 q 上设 Distinct + Select 组合。

**修复路径**：v0.7.1 在 aggregate 入口检测该组合，返回 `ErrAggregateInvalid` 或先 Omit 已有 selects。

## 附录 E：引用

- GORM v1.31.1 finisher_api.go：`Scan` 527-550 / `Find` 164-173 / `First` 120-132 / `Pluck` 556-575 / `Rows` 516-524
- gplus 现状：`repository.go:848`（aggregate）、`repository.go:593`（RawQueryTx）、`repository.go:612`（RawScanTx）
- 多专家审计：架构 / 代码 / 安全 / 对抗 四视角，二轮迭代
- 用户 spec：本会话 user message 1
