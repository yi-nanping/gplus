# Spec: Query-chain-safe 投影 API + aggregate Scan 漏洞修复（v0.7.0 minor）

**日期**：2026-05-01
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
| `Scan` | **Row**（内部 `tx.Rows()` → `tx.callbacks.Row().Execute(tx)`） | 535 → 518 |
| `Row` / `Rows` | **Row** | 508 / 518 |

**结论**：`db.Scan(dest)` 不会触发挂在 Query callback chain 上的下游 callback。

### 下游真实事故路径（gvs-server）

下游项目用 `db.Callback().Query().Before("gorm:query").Register("isolation:query", ...)` 挂数据隔离 callback 在 Query chain。一旦业务方写出：

```go
q.ToDB(db).Model(&User{}).Scan(&rows)
```

callback 完全不触发 → 机构管理员能查到子树外的用户。已发生过线上事故，目前下游靠"业务方手动改 `.Scan` → `.Find` + code review"兜底。

### gplus 自身的同类漏洞

**`repository.go:848`（`aggregate` 函数，Sum/Max/Min/Avg 共用）**：

```go
err = r.dbResolver(q.Context(), tx).
    Model(new(T)).Scopes(q.BuildCount()).Select(expr).Scan(&ptr).Error
```

- `Model(new(T))` 把 `Statement.Schema` 解析为 T 的 schema
- `.Scan(&ptr)` 走 Row chain → 下游 isolation callback 不触发
- 结果：聚合统计存在 isolation 失效隐患

### 不影响的路径（已审计确认）

- `RawQueryTx` / `RawScanTx`（repository.go:593 / :612）：Raw SQL 不调用 `Statement.Parse(model)`，Schema 为 nil。下游 callback 通常 `if stmt.Schema == nil { return }` 短路 → 本期不动
- `GetById/GetOne/Last/List/GetByIds/Pluck/Count/Chunk`：均走 `.First` / `.Find` / `.Pluck` / `.Count` / `FindInBatches` → Query chain 正常 ✓

## 关键发现

gplus 暴露 `q.ToDB(db) *gorm.DB` 后无法拦截用户在 `*gorm.DB` 上的方法调用 — 业务方能写 `.Scan`、`.Rows` 等任何绕过 Query chain 的 API。本期通过**提供等价的 Query-chain-safe 替代品**解决，而不是试图拦截用户行为。

## 范围决策

### 本期纳入

1. **新增 4 个包级泛型函数**（投影查询，强类型 dest）：
   - `FindAs` / `FindAsTx`（多行）
   - `FindOneAs` / `FindOneAsTx`（单行）
2. **修复 aggregate 路径**（Sum/Max/Min/Avg 内部 `.Scan` → `.Find`）
3. **测试**：callback 触发回归测试（基线 + 修复验证）+ 投影正确性 table-driven + 边界
4. **文档**：CHANGELOG / README / godoc 全部同步

### 本期排除（含理由）

- **`RawQuery` / `RawScan` / `RawScanTx`**：Raw SQL 路径 Schema=nil，下游 callback 自然短路；下游需要 isolation 必须手写 WHERE，属于另一类约束（已有下游文档）
- **不在 `*Query[T]` 上加 `Scan` 包装方法**：会让人误以为"用 q 上的 Scan 就安全"，反而增加心智负担；`q.ToDB(db).Scan(...)` 用户主动绕路，文档告诫即可
- **不改 GORM `db.Scan` 行为**：不可能也不应该 monkey-patch GORM
- **不动下游 callback 注册接口**：gplus 不感知具体业务 callback

## 设计

### 1. API 形态（新建 `find_as.go`）

```go
// FindAs 投影查询（多行）。dest 必须是 *[]Element 切片指针。
// 走 GORM Query callback chain，下游挂在 Query chain 上的隔离/审计 callback 会触发。
func FindAs[T any, Dest any, D comparable](
    r *Repository[D, T], q *Query[T], dest *[]Dest,
) error

// FindAsTx 支持事务的 FindAs。
func FindAsTx[T any, Dest any, D comparable](
    r *Repository[D, T], q *Query[T], dest *[]Dest, tx *gorm.DB,
) error

// FindOneAs 投影查询（单行）。dest 是 *Element。
// 无匹配时返回 gorm.ErrRecordNotFound（与 GetById 既有语义一致）。
// 走 GORM Query callback chain。
func FindOneAs[T any, Dest any, D comparable](
    r *Repository[D, T], q *Query[T], dest *Dest,
) error

// FindOneAsTx 支持事务的 FindOneAs。
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
    // First 在无匹配时返回 gorm.ErrRecordNotFound（GORM 默认行为）
}

// 非 Tx 版本直接转发：FindAs(r,q,dest) → FindAsTx(r,q,dest,nil)
```

**关键技术点**：
- `Model(new(T))` 必须保留 — 让 Statement.Schema 解析为 T，下游 isolation callback 才能拿到 schema 判断
- `q.BuildQuery()` 而非 `BuildCount` — 投影场景需要 SELECT/JOIN/ORDER/LIMIT/Preload
- `dest` 是 `*[]Dest` / `*Dest` — Go 类型推导能从形参推出 Dest，调用端无需写类型参数

#### 调用形态

```go
type UserVO struct {
    Name     string
    DeptName string
}

// 多行
var rows []UserVO
err := gplus.FindAs(repo, q.LeftJoin("dept", "users.dept_id = dept.id").
    Select("users.name", "dept.name AS dept_name"), &rows)

// 单行
var one UserVO
err := gplus.FindOneAs(repo, q.Eq(&u.ID, 1), &one)

// 事务
err := db.Transaction(func(tx *gorm.DB) error {
    return gplus.FindAsTx(repo, q, &rows, tx)
})
```

### 2. aggregate 路径修复（`repository.go:848`）

```go
// 改前
var ptr *R
err = r.dbResolver(q.Context(), tx).Model(new(T)).Scopes(q.BuildCount()).
    Select(expr).Scan(&ptr).Error
if err == nil && ptr != nil {
    result = *ptr
}

// 改后
var rows []*R
err = r.dbResolver(q.Context(), tx).Model(new(T)).Scopes(q.BuildCount()).
    Select(expr).Find(&rows).Error
if err == nil && len(rows) > 0 && rows[0] != nil {
    result = *rows[0]
}
```

**为什么用 `[]*R` 而不是 `[]R`**：保留 NULL 语义。空表/无匹配行时，SUM/MAX/MIN/AVG 返回 SQL NULL，用 `*R` 接收时 nil；用 `R` 接收会报"converting NULL to int64"。这是已记录的"NULL 安全（聚合函数）"陷阱。

**注释更新**：godoc 添加"走 GORM Query callback chain"声明。

### 3. 文件组织

- 新建 `find_as.go` — 4 个新函数 + godoc
- 新建 `find_as_test.go` — 全部新增测试
- 改 `repository.go:829-853`（aggregate 函数）— 1 处实现 + godoc 注释更新

### 4. 测试策略（`find_as_test.go`）

#### 4.1 callback chain 触发证明（核心 — 防止退化）

测试目标：搭建一个 callback probe（挂在 GORM Query chain 与 Row chain 各一个 callback，分别计数），验证下游"挂在 Query chain 上的 callback"在新 API 下被触发、在 `db.Scan` 基线下不被触发。

具体 GORM callback 注册 API（`Register` / `Before` / `After` / 内置 callback name）由实施阶段决定 — `find_as_test.go` 的 helper 实现细节，不在 spec 范围。

测试矩阵（`Model(new(T))` 已解析为 T 的 schema，下同）：

| 用例 | Query chain 触发数 | Row chain 触发数 | 说明 |
|---|---|---|---|
| 基线：`q.ToDB(db).Model(new(T)).Scan(&rows)` | 0 | 1 | 证明 Scan 绕过 Query chain（GORM 行为锁定） |
| `FindAs(r, q, &rows)` | 1 | 0 | 修复证明 |
| `FindOneAs(r, q, &one)` | 1 | 0 | 修复证明 |
| `Sum(r, q, col)` | 1 | 0 | aggregate 修复证明 |
| `Max/Min/Avg` | 各 1 | 各 0 | aggregate 修复证明 |

每条用例独立 setup probe + 独立 setup DB，避免计数相互污染。

#### 4.2 投影正确性（table-driven）

- LEFT JOIN + 自定义 dest struct（多列拼接）
- SELECT 子集列
- WHERE / ORDER / LIMIT 透传
- gplus 自身 DataRule 透传 — 验证 `ctx.Value(DataRuleKey)` 注入条件后 builder 阶段加 WHERE 生效，结果集只含权限内行（注意：与 §4.1 验证下游业务 callback 是两件事 — §4.1 测 callback 触发，§4.2 测 builder 加 WHERE）

#### 4.3 边界

- `q == nil` → `ErrQueryNil`
- `q.GetError() != nil` → 提前返回累积错误
- `q.DataRuleBuilder().GetError() != nil` → 返回 DataRule 校验错误
- `FindOneAs` 无匹配 → `gorm.ErrRecordNotFound`
- `dest` 指向 nil 切片（`var rows []UserVO; FindAs(r, q, &rows)`）→ 正常追加

### 5. 文档更新

#### 5.1 CHANGELOG.md（新增 `## [0.7.0]` 条目）

（`<发版日>` 在实际 commit CHANGELOG 时由实施者填上 commit 当天日期，非 spec 占位符）

```markdown
## [0.7.0] - <发版日>

### 新增

- **Query-chain-safe 投影查询 API**：根除 `db.Scan()` 绕过 GORM Query callback chain 导致的下游隔离/审计 callback 失效问题
  - `FindAs[T, Dest, D]` / `FindAsTx[T, Dest, D]`：投影多行（dest 为 `*[]Dest`）
  - `FindOneAs[T, Dest, D]` / `FindOneAsTx[T, Dest, D]`：投影单行（dest 为 `*Dest`，无匹配返回 `gorm.ErrRecordNotFound`）
  - 内部走 `.Find` / `.First` → Query chain，下游挂在 Query chain 上的 callback 自动触发
  - Go 类型推导后调用形态：`gplus.FindAs(repo, q, &rows)`，无需写类型参数

### 修复

- **aggregate 路径绕过 Query callback chain**（`repository.go:848`）：Sum/Max/Min/Avg 内部 `.Scan(&ptr)` 改为 `.Find(&rows)` 走 Query chain；下游 isolation/审计 callback 现可正确触发。NULL 语义保持不变（`[]*R` 中转）

### 行为约束（须知）

- **`q.ToDB(db).Scan(...)` 仍绕过 Query callback chain**：GORM v1.31.1 `db.Scan` 内部走 Row chain，gplus 无法拦截。新代码请改用 `FindAs` / `FindOneAs`；老代码若依赖"跳过 Query callback"特性可保留
- **新 API 不取代 `RawScan`**：Raw SQL 路径 Schema=nil，下游 callback 通常自然短路，本期不动 Raw 系列
```

#### 5.2 README.md

- Repository API 章节加 `FindAs` / `FindOneAs` 调用示例
- 「已知陷阱」段加："`q.ToDB(db).Scan(...)` 绕过 Query callback chain，请改用 `FindAs`/`FindOneAs`"

#### 5.3 godoc

- 4 个新函数：声明走 Query callback chain
- `aggregate`：注释更新，去掉"BuildCount + Scan"描述，改为"BuildCount + Find（走 Query chain）"

### 6. 版本号

- v0.6.0 已于 2026-04-30 发布（类型安全子查询）
- 本期发布 **v0.7.0**（MINOR）— 新增 API 不破坏现有调用，aggregate 修复对用户层语义无变化（NULL → 零值仍成立）

## 验收清单

- [ ] `find_as.go` 实现 4 个新函数 + godoc
- [ ] `repository.go:848` aggregate 改造（`.Scan` → `.Find`，`*R` → `[]*R`）+ godoc 注释更新
- [ ] `find_as_test.go` 完成 callback 触发矩阵 + 投影 table-driven + 边界
- [ ] aggregate 已有测试不退化（`Sum/Max/Min/Avg` 全绿，含 NULL 场景）
- [ ] `D:/Environment/golang/go1.21.11/bin/go.exe test -race ./...` 全绿
- [ ] 测试覆盖率 ≥ 94.0%（不下降）
- [ ] `CHANGELOG.md` 增 `## [0.7.0]` 条目
- [ ] `README.md` 同步新 API + 陷阱说明
- [ ] git commit 中文 message，禁用 Co-Authored-By trailer
- [ ] 留 commit 不 push（用户审完手动 push 并发版）

## 不在范围

- 不动 `RawQuery` / `RawScan` / `RawScanTx`
- 不改 GORM `db.Scan` 行为
- 不动下游 callback 注册接口
- 不在 `*Query[T]` 上加 `Scan` 包装方法

## 引用

- GORM v1.31.1 finisher_api.go 行号：`Scan` 527-550 / `Find` 164-173 / `First` 120-132 / `Pluck` 556-575 / `Rows` 516-524
- gplus 现状：`repository.go:848`（aggregate）、`repository.go:593`（RawQueryTx）、`repository.go:612`（RawScanTx）
- 用户 spec：本会话 user message 1
