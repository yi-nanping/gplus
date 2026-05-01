# Spec: Query-chain-safe 投影 API + aggregate Scan 漏洞修复（v0.7.0 minor）

**日期**：2026-05-01（二轮修订：经多专家审计 + GORM 实测后重写 §1/§2/§4/§5）
**版本**：v0.7.0（minor，新增 API + 内部修复）
**类型**：新功能 + bug 修复

## 背景与问题

### GORM v1.31.1 callback chain 行为（已实测验证）

`gorm.io/gorm@v1.31.1/finisher_api.go`：

| GORM 方法 | 内部走的 callback chain | 行号 |
|---|---|---|
| `Find` / `First` / `Take` / `Last` | **Query** | 131 / 144 / 160 / 172 |
| `Count` | **Query** | 497 |
| `Pluck` | **Query** | 574 |
| `Scan` | **Row**（`tx.Rows()` → `tx.callbacks.Row().Execute(tx)`） | 535 → 518 |
| `Row` / `Rows` | **Row** | 508 / 518 |

**结论**：`db.Scan(dest)` / `db.Row()` / `db.Rows()` 三者均不会触发挂在 Query callback chain 上的下游 callback。

### 下游真实事故路径（gvs-server）

下游用 `db.Callback().Query().Before("gorm:query").Register("isolation:query", ...)` 挂数据隔离 callback 在 Query chain。一旦业务方写出：

```go
q.ToDB(db).Model(&User{}).Scan(&rows)   // 或 .Row() / .Rows()
```

callback 完全不触发 → 机构管理员能查到子树外的用户。已发生过线上事故，目前下游靠"业务方手动改 + code review"兜底。

### gplus 自身的同类漏洞

**`repository.go:848`（`aggregate` 函数，Sum/Max/Min/Avg 共用）**：

```go
err = r.dbResolver(q.Context(), tx).
    Model(new(T)).Scopes(q.BuildCount()).Select(expr).Scan(&ptr).Error
```

- `Model(new(T))` 把 `Statement.Schema` 解析为 T 的 schema
- `.Scan(&ptr)` 走 Row chain → 下游 isolation callback 不触发
- **结果**：聚合统计存在 isolation 失效隐患

### 不影响的路径（已审计确认）

- `RawQueryTx` / `RawScanTx`（repository.go:593 / :612）：Raw SQL 不调用 `Statement.Parse(model)`，Schema 为 nil
- `GetById/GetOne/Last/List/GetByIds/Pluck/Count/Chunk`：均走 `.First` / `.Find` / `.Pluck` / `.Count` / `FindInBatches` → Query chain ✓

## GORM 实测结果（写 spec 前的 CRITICAL 验证）

实测代码已运行通过，证据用于 §1 / §2 设计决策。

| 实测场景 | 结果 | 影响 |
|---|---|---|
| `Model(&T{}).First(&VO)` 后 `Statement.Schema.Table` | 保持 T 的表名（不被 VO 覆盖） | **FindOneAs 用 First 安全** — 安全审计 C1（First 覆盖 Schema 假设）**已被推翻** |
| `Model(&T{}).Select("SUM(age)").Find(&[]*int64)` 后 Schema | 保持 T | aggregate Find 路径 Schema 安全 |
| 空表 `Model(&T{}).Select("SUM(age)").Find(&[]*int64)` NULL | **报错** "converting NULL to int64 is unsupported" | **方案 a（`[]*R`）有 bug，必须放弃** |
| 空表 Pluck(`*int64`) / Pluck(`[]*int64`) | 同样报错 | Pluck 也不能用 |
| 空表 `Find(&[]wrap{V *int64})` Wrapper struct | **`V == nil` 正确** | **采纳为方案 G（aggregate 修复正确解）** |
| 方案 G + callback 探针 | `queryCount=1, rowCount=0` | 走 Query chain ✓ 修复目标达成 |

**关键洞察**：GORM `.Find` 在结构体字段路径下使用反射 + `database/sql.Rows.Scan`，对 `*int64` 字段 NULL → nil 行为正确；但在 `*int64` 直接作为切片元素或 Pluck 标量目标时，走的是另一条 Scan 路径，对 NULL 处理失败。

## ADR（架构决策记录）

| 决策 | 拒绝的备选 | 拒绝理由 |
|---|---|---|
| **包级泛型函数** `gplus.FindAs[T,Dest,D](r, q, dest)` | A. `*Query[T]` 链式方法 `q.FindAs(db, dest)` | dest 弱类型 (`any`)；db 参数破坏链；与现有 `Sum/Max/Min/Avg/Pluck` 包级泛型形态不一致 |
| 同上 | B. 极简版 `FindAs[T,Dest](db, q, dest)` 摘掉 D | 丢失 `r.dbResolver(ctx, tx)` 的 ctx 注入 + nil-tx 降级逻辑，调用方需手动传 db 等同退化 |
| 同上 | C. 二段式 `gplus.As[Dest](repo).Find(q, &rows)` | gplus 无先例；二段式增加 reader 心智成本；与 `Sum[T,R,D]` 三参数风格冲突 |
| 同上 | D. 方法 + 包级并存 | 双 API 表面增加用户决策成本；命名冲突风险 |
| **命名 `FindAs/FindOneAs`** | `ListAs/GetAs`（与现有 `List/GetById` 对齐） | `Find` 是 GORM/Java JPA/Hibernate 圈通用动词；`As` 后缀已是行业惯例（JPA `findAs(Class)`、Hibernate Projection）；与 `Sum/Max/Min/Avg/Pluck` 同属"包级动词族" |
| 同上 | `Project/ProjectOne` | 语义最准确但 gplus 无先例，引入新动词增加 API 表面 |
| **不引入 `Projector`/`Destinationer` 接口** | — | Dest 是纯结构体，不参与 SQL 构造，无安全/扩展点驱动接口化（与 v0.6.0 `Subquerier` 接口的引入动机不同） |
| **aggregate 修复用 wrapper struct（方案 G）** | a. `Find(&[]*R)` | 实测证明：空表 SUM 时 `[]*R` Scan 报错"converting NULL to int64"，无法保留 NULL → 零值语义 |
| 同上 | b. `Pluck(*R)` / `Pluck(&[]*R)` | 实测同样报错 |
| 同上 | c. `COALESCE(SUM(x), 0)` 改 SQL | 丢失"区分零值与 NULL"的能力，部分 DB 方言差异 |
| **版本号 v0.7.0 不拆 0.6.1+0.7.0** | 拆为 0.6.1（仅修 aggregate）+ 0.7.0（新增 API） | 新增 API 非破坏；下游升级心智成本相同；合并发布更紧凑 |

## 范围决策

### 本期纳入

1. **新增 4 个包级泛型函数**：`FindAs/FindAsTx/FindOneAs/FindOneAsTx`
2. **新增 1 个包级 generic 内部类型**：`aggregateWrap[R any]` (供 aggregate 用)
3. **修复 aggregate 路径**：`.Scan(&ptr)` → wrapper struct + `.Find`
4. **测试**：callback 触发矩阵（基线 + 修复证明 + 复合场景） + 投影正确性 + 边界
5. **文档**：CHANGELOG / README / godoc 全部同步，含强迁移建议 + grep 命令

### 本期排除

- `RawQuery` / `RawScan` / `RawScanTx` — Raw SQL 路径 Schema=nil
- 不改 GORM `db.Scan` 行为
- 不动下游 callback 注册接口
- 不在 `*Query[T]` 上加 `FindAs` 方法（见 ADR）
- 不引入 `Projector` 接口（见 ADR）

## 设计

### 1. API 形态（新建 `find_as.go`）

```go
// FindAs 投影查询（多行）。dest 必须是 *[]Element 切片指针。
// 走 GORM Query callback chain，下游挂在 Query chain 上的隔离/审计 callback 会触发。
//
// 调用 FindAs 后 q.conditions 会被永久追加 DataRule 条件（dataRuleApplied 保护幂等），
// q 不应再跨不同 ctx 复用。这与现有 List/Sum 等函数行为一致。
func FindAs[T any, Dest any, D comparable](
    r *Repository[D, T], q *Query[T], dest *[]Dest,
) error

func FindAsTx[T any, Dest any, D comparable](
    r *Repository[D, T], q *Query[T], dest *[]Dest, tx *gorm.DB,
) error

// FindOneAs 投影查询（单行）。dest 是 *Element。
// 无匹配时返回 gorm.ErrRecordNotFound。
//
// 实现注意：内部用 First — 已实测 GORM v1.31.1 First(dest) 不会用 dest schema 覆盖
// 已设置的 Model(new(T))，下游 isolation callback 拿到的 Schema.Table 仍为 T 的表名。
//
// 调用方注意：不应在同一 q 上同时调用 q.Limit() / q.Page()，否则 First 追加的
// LIMIT 1 与已有 LIMIT 叠加，部分 DB 行为未定义。q.Order() 设置的排序与 First
// 追加的主键 ASC 排序会并存（多列排序）。
func FindOneAs[T any, Dest any, D comparable](
    r *Repository[D, T], q *Query[T], dest *Dest,
) error

func FindOneAsTx[T any, Dest any, D comparable](
    r *Repository[D, T], q *Query[T], dest *Dest, tx *gorm.DB,
) error
```

#### 内部实现骨架

```go
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
    if err := q.GetError(); err != nil {
        return err
    }
    if err := q.DataRuleBuilder().GetError(); err != nil {
        return err
    }
    return r.dbResolver(q.Context(), tx).
        Model(new(T)).Scopes(q.BuildQuery()).First(dest).Error
}

// FindAs(r,q,dest)     → FindAsTx(r,q,dest,nil)
// FindOneAs(r,q,dest)  → FindOneAsTx(r,q,dest,nil)
```

**关键技术点**：
- `Model(new(T))` 必须保留 — 让 Statement.Schema 解析为 T；**实测证明** `First(dest_VO)` 不会覆盖该 Schema
- `q.BuildQuery()` 而非 `BuildCount` — 投影场景需要 SELECT/JOIN/ORDER/LIMIT/Preload
- `dest` 是 `*[]Dest` / `*Dest` — Go 1.21+ unified inference 支持跨参数推导，调用端无需写类型参数

#### 调用形态

```go
// repo *Repository[uint, User]      ← 提供主表 schema + dbResolver + ctx 注入
// q    *Query[User]                  ← 查询条件（绑定主表 User）
// rows []UserVO                      ← Dest 仅决定 SELECT 列映射，不必是主表实体

type UserVO struct {
    Name     string                   // 默认匹配 SELECT 列 `name`（snake_case）
    DeptName string                   // 匹配 `dept_name`（必须 alias，否则 GORM 无法定位列）
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
- `Repository[D, T]` 绑定**主表** schema（T） + 提供 dbResolver 与 ctx 注入路径
- `Dest` 仅决定 **SELECT 列 → struct 字段**的映射规则（GORM 默认 snake_case）
- 跨表 JOIN 列必须用 SQL alias（如 `dept.name AS dept_name`），否则字段名冲突时 GORM 无法定位列

### 2. aggregate 路径修复（`repository.go:848`）

**改用方案 G（包级 generic wrapper struct）**：

```go
// 包级新增类型（在 repository.go 或 find_as.go 内）
type aggregateWrap[R any] struct {
    V *R `gorm:"column:v"`
}

// aggregate 内部
expr := fmt.Sprintf("%s(%s) AS v", fn, colName) // 注意 alias 为 "v"
var rows []aggregateWrap[R]
err = r.dbResolver(q.Context(), tx).Model(new(T)).Scopes(q.BuildCount()).
    Select(expr).Find(&rows).Error
if err == nil && len(rows) > 0 && rows[0].V != nil {
    result = *rows[0].V
}
```

**为什么用 wrapper struct**：
- **NULL 安全**：实测 `Find(&[]aggregateWrap[R])` 在 SUM 返回 NULL 时 `V == nil`，与原 `Scan(&*R)` nil 语义等价
- **走 Query chain**：实测 `queryCount=1, rowCount=0` ✓
- **`[]*R` 直接当切片元素**：实测 NULL 直接报错"converting NULL to int64 is unsupported"，**不能用**

**性能影响**：相比原 `.Scan(&ptr)` 多一次切片头分配（24 字节）+ 一次反射字段访问。单次聚合查询可忽略。**高频聚合**场景（每秒数百次）下，callback chain 执行成本（取决于下游 callback 数量与逻辑）是新增主要开销，应基准测试评估。

### 3. 文件组织

- 新建 `find_as.go` — 4 个新函数 + `aggregateWrap[R]` 包级类型 + godoc
- 新建 `find_as_test.go` — 全部新增测试
- 改 `repository.go:829-853`（aggregate 函数）— 改用 `aggregateWrap[R]` + `.Find` + godoc 注释更新

### 4. 测试策略（`find_as_test.go`）

#### 4.1 callback chain 触发矩阵（核心 — 防止退化）

测试目标：搭建 callback probe（Query chain + Row chain 各挂一个计数 callback），验证下游"挂在 Query chain 上的 callback"在新 API / 修复后 aggregate 路径下被触发，在 `db.Scan` 基线下不被触发。

具体 GORM callback 注册 API（`Register` / `Before` / `After` / 内置 callback name）由实施阶段在 helper 中实现。

| 用例 | Query chain | Row chain | 说明 |
|---|---|---|---|
| 基线：`q.ToDB(db).Model(new(T)).Scan(&rows)` | 0 | 1 | Scan 绕过 Query chain（GORM 行为锁定） |
| 基线：`q.ToDB(db).Model(new(T)).Rows()` | 0 | 1 | Rows 绕过（同类漏洞旁路） |
| `FindAs(r, q, &rows)` | 1 | 0 | 修复证明 |
| `FindOneAs(r, q, &one)` | 1 | 0 | 修复证明 |
| `FindOneAs(r, q, &one)` 无匹配 | 1 | 0 | 仍走 Query chain，返回 ErrRecordNotFound |
| `Sum(r, q, col)` 有数据 | 1 | 0 | aggregate 修复证明 |
| `Sum(r, q, col)` 空表 NULL | 1 | 0 | aggregate 修复 + NULL 语义保持（result = 零值，无 err） |
| `Max/Min/Avg` 有数据 | 各 1 | 各 0 | aggregate 修复证明 |
| **`FindAs + DataRule + LEFT JOIN`** | 1 | 0 | 复合场景：DataRule WHERE 与 JOIN ON 不互染 |
| **`FindAs(q.InSub(subQ))`** | ≥1（含子查询路径） | 0 | 子查询 + FindAs 不退化为 Row chain |

每条用例独立 setup probe + 独立 setup DB（避免计数相互污染）。

#### 4.2 投影正确性（table-driven）

- LEFT JOIN + 自定义 dest struct（多列拼接）
- SELECT 子集列
- WHERE / ORDER / LIMIT 透传
- Dest 字段名 alias 映射规则验证（`AS dept_name` → `DeptName`）
- gplus 自身 DataRule 透传 — `ctx.Value(DataRuleKey)` 注入条件后 builder 阶段加 WHERE 生效，结果集只含权限内行
- `Distinct + FindAs` — 去重投影
- **DataRule + 模拟下游 isolation callback 双套并行生效** — 注册一个向 Statement 追加 WHERE 的探针 callback，断言生成 SQL 同时含 DataRule WHERE + 探针 WHERE

#### 4.3 边界

- `q == nil` → `ErrQueryNil`
- `q.GetError() != nil` → 提前返回累积错误（dest 未触碰，保留调用方原值）
- `q.DataRuleBuilder().GetError() != nil` → 返回 DataRule 校验错误
- `FindOneAs` 无匹配 → `gorm.ErrRecordNotFound`
- `dest` 指向 nil 切片（`var rows []UserVO; FindAs(r, q, &rows)`）→ 正常执行，**GORM Find 覆盖写入**（非追加；nil 切片与空切片行为相同）
- **同一 q 调用两次 FindAs**：DataRule 条件不重复追加（`dataRuleApplied` 幂等保护），结果一致

### 5. 文档更新

#### 5.1 CHANGELOG.md（新增 `## [0.7.0]` 条目）

`<发版日>` 在实际 commit 时填上当天日期。

```markdown
## [0.7.0] - <发版日>

### 新增

- **Query-chain-safe 投影查询 API**：根除 `db.Scan()` / `db.Row()` / `db.Rows()` 绕过 GORM Query callback chain 导致的下游隔离/审计 callback 失效问题
  - `FindAs[T, Dest, D]` / `FindAsTx[T, Dest, D]`：投影多行（dest 为 `*[]Dest`）
  - `FindOneAs[T, Dest, D]` / `FindOneAsTx[T, Dest, D]`：投影单行（dest 为 `*Dest`，无匹配返回 `gorm.ErrRecordNotFound`）
  - 内部走 `.Find` / `.First` → Query chain，下游挂在 Query chain 上的 callback 自动触发
  - Go 1.21+ 类型推导后调用形态：`gplus.FindAs(repo, q, &rows)`，无需写类型参数

### 修复

- **aggregate 路径绕过 Query callback chain**（`repository.go:848`）：Sum/Max/Min/Avg 内部 `.Scan(&ptr)` 改为 `.Find(&[]aggregateWrap[R])` 走 Query chain；下游 isolation/审计 callback 现可正确触发。NULL 语义保持不变（wrapper struct 中 `*R` 字段在 SQL NULL 下为 nil，已实测）

### 行为约束（须知）

- **`q.ToDB(db).Scan(...)` / `.Row()` / `.Rows()` 仍绕过 Query callback chain**：GORM v1.31.1 这三个方法内部走 Row chain，gplus 无法拦截。**若下游挂有 isolation/审计 callback，这三种调用等同保留隔离漏洞，必须迁移到 `FindAs`/`FindOneAs`**。
  - 排查命令：`grep -rEn 'ToDB\([^)]*\)\.(Scan|Row|Rows)\(' .`
- **新 API 不取代 `RawScan`**：Raw SQL 路径 Schema=nil，下游 isolation callback 在正确实现下会 `if Schema == nil { return }` 短路；**若下游 callback 未做该判断，行为不可预测**。涉及敏感数据的 Raw 查询必须在 SQL 中手写 WHERE，不可依赖 gplus DataRule 或下游 callback
- **aggregate 性能基线**：高频聚合（每秒数百次 Sum/Max/Min/Avg）下，callback chain 触发是新增主要开销（取决于下游 callback 数量与复杂度）。性能敏感场景需基准测试

### 不在本期范围

- 已评估"拆 0.6.1（仅修 aggregate）+ 0.7.0（新增 API）"方案，因新增 API 非破坏、合并发布心智成本相同，**合并发布**
```

#### 5.2 README.md

- Repository API 章节加 `FindAs` / `FindOneAs` 调用示例（含主表 vs 结果结构对照说明）
- 「已知陷阱」段加（强措辞 + 后果具象 + 排查命令）：

```markdown
### `q.ToDB(db).Scan` / `.Row` / `.Rows` 绕过 Query callback chain（CRITICAL）

GORM v1.31.1 中 `db.Scan` / `db.Row` / `db.Rows` 内部走 Row callback chain，**不会触发**挂在 Query chain 上的下游 callback（如数据隔离、审计、查询日志）。在依赖这些 callback 的项目中，使用上述三种调用**会导致跨租户数据泄露 / 审计日志缺失**。

**必须改用** `FindAs` / `FindOneAs`：

| 旧写法（漏洞） | 新写法（安全） |
|---|---|
| `q.ToDB(db).Model(&T{}).Scan(&rows)` | `gplus.FindAs(repo, q, &rows)` |
| `q.ToDB(db).Model(&T{}).Find(&rows)` 拿 row 后手动 Scan | `gplus.FindAs(repo, q, &rows)` |
| `q.ToDB(db).Model(&T{}).Limit(1).Scan(&one)` | `gplus.FindOneAs(repo, q, &one)` |

**排查老代码**：
\`\`\`bash
grep -rEn 'ToDB\([^)]*\)\.(Scan|Row|Rows)\(' .
\`\`\`

**`RawQuery` / `RawScan` / `RawScanTx`**：Schema 为 nil，下游 callback 在正确实现下短路。**若下游 callback 未判断 `Schema == nil`，行为不可预测**。涉及敏感数据必须在 SQL 中手写 WHERE。
```

#### 5.3 godoc

- 4 个新函数：声明走 Query callback chain + 主表 vs Dest 心智模型 + DataRule 副作用
- `aggregate`：注释更新为"BuildCount + Find（走 Query chain）+ wrapper struct 保 NULL 语义"

### 6. 版本号

- v0.6.0 已于 2026-04-30 发布（类型安全子查询）
- 本期发布 **v0.7.0**（MINOR）— 新增 API 不破坏现有调用，aggregate 修复对用户层语义无变化（NULL → 零值仍成立）
- 已评估拆为 0.6.1+0.7.0 方案，因新增 API 非破坏、下游升级心智成本相同，合并发布

## 验收清单

- [ ] `find_as.go` 实现 4 个新函数 + `aggregateWrap[R]` 类型 + godoc
- [ ] `repository.go:848` aggregate 改造（用 `aggregateWrap[R]` + `.Find`）+ godoc 注释更新
- [ ] **实施时再次实测**：`Find(&[]aggregateWrap[int64])` 在空表 SUM 下 `V == nil`、SQL 无 NULL 错误
- [ ] **实施时再次实测**：`First(&Dest)` 在 `Model(new(T))` 已设置时 Schema 不被覆盖（DryRun 验证）
- [ ] `find_as_test.go` 完成 callback 触发矩阵（含复合场景）+ 投影 table-driven + 边界
- [ ] aggregate 已有测试不退化（`Sum/Max/Min/Avg` 全绿，含空表 NULL 场景）
- [ ] `D:/Environment/golang/go1.21.11/bin/go.exe test -race ./...` 全绿
- [ ] 测试覆盖率 ≥ 94.0%（不下降）
- [ ] `CHANGELOG.md` 增 `## [0.7.0]` 条目（含强措辞迁移建议 + grep 命令 + 性能基线说明）
- [ ] `README.md` 同步新 API + 跨租户后果说明 + 排查命令
- [ ] git commit 中文 message，禁用 Co-Authored-By trailer
- [ ] 留 commit 不 push（用户审完手动 push 并发版）

## 不在范围

- `RawQuery` / `RawScan` / `RawScanTx`（Raw SQL 路径）
- GORM `db.Scan` 行为（不可能也不应该 monkey-patch）
- 下游 callback 注册接口
- `*Query[T]` 上的 `FindAs` 方法（见 ADR）
- `Projector` / `Destinationer` 接口（见 ADR）
- `FindAsCount` / `FindOneAsExists` / 聚合 + 投影合并 API（v0.8.0 候选）

## 引用

- GORM v1.31.1 finisher_api.go 行号：`Scan` 527-550 / `Find` 164-173 / `First` 120-132 / `Pluck` 556-575 / `Rows` 516-524
- gplus 现状：`repository.go:848`（aggregate）、`repository.go:593`（RawQueryTx）、`repository.go:612`（RawScanTx）
- 多专家审计：架构（架构 §1-§6）/ 代码（HIGH-A/B + MED）/ 安全（C1 已被实测推翻 + H2/H3 采纳）/ 对抗（§1/§4/§5/§6/§8 采纳）
- GORM 实测：见本 spec "GORM 实测结果" 段
- 用户 spec：本会话 user message 1
