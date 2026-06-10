# GPlus - Go GORM 增强库

GPlus 是一个基于 GORM 的 Go 语言增强库，提供类型安全的查询构建器、Repository 模式和条件构建等功能，让数据库操作更加简洁、类型安全和高效。

## 特性

- 🚀 **类型安全查询**：通过泛型实现类型安全的查询构建
- 📦 **Repository 模式**：标准化的 CRUD 操作接口
- 🏗️ **流畅的条件构建**：链式调用构建复杂查询条件
- 🔒 **事务支持**：无缝的事务管理
- ⚡ **高性能**：智能缓存和优化，减少反射开销
- 📝 **分页查询**：内置分页支持
- 🔧 **更新构建器**：类型安全的更新操作构建
- 🔢 **聚合函数**：Sum/Max/Min/Avg，NULL 安全
- ♻️ **软删除恢复**：Restore/RestoreByCond 按主键或条件恢复
- 🔄 **分批处理**：Chunk/FirstOrCreate/FirstOrUpdate 高级操作
- ↕️ **原子增减**：IncrBy/DecrBy 无竞态字段更新

## 快速开始

### 安装

```bash
go get github.com/yi-nanping/gplus@v0.8.0
```

### 基础用法

```go
package main

import (
    "context"
    "fmt"
    "gorm.io/driver/sqlite"
    "gorm.io/gorm"
    "github.com/yi-nanping/gplus"
)

// 定义用户模型
type User struct {
    ID       uint   `gorm:"primaryKey;column:id"`
    Name     string `gorm:"column:name"`
    Age      int    `gorm:"column:age"`
    Email    string `gorm:"column:email"`
    IsVip    bool   `gorm:"column:is_vip"`
}

func main() {
    // 初始化 GORM 数据库连接
    db, err := gorm.Open(sqlite.Open("test.db"), &gorm.Config{})
    if err != nil {
        panic("failed to connect database")
    }

    // 创建 Repository
    repo := gplus.NewRepository[int, User](db)

    ctx := context.Background()

    // 1. 创建用户
    user := &User{Name: "张三", Age: 25, Email: "zhangsan@example.com", IsVip: false}
    err = repo.Save(ctx, user)
    if err != nil {
        fmt.Printf("创建用户失败: %v\n", err)
    }

    // 2. 查询单个用户
    query, model := gplus.NewQuery[User](ctx)
    query.Eq(&model.Name, "张三")
    
    result, err := repo.GetOne(query)
    if err != nil {
        fmt.Printf("查询用户失败: %v\n", err)
    } else {
        fmt.Printf("查询结果: %+v\n", result)
    }

    // 3. 分页查询
    pageQuery, pageModel := gplus.NewQuery[User](ctx)
    pageQuery.Gt(&pageModel.Age, 18).Order(&pageModel.ID, false)
    
    results, total, err := repo.Page(pageQuery, false)
    if err != nil {
        fmt.Printf("分页查询失败: %v\n", err)
    } else {
        fmt.Printf("分页结果: 总计 %d 条, 当前页 %d 条\n", total, len(results))
    }

    // 4. 更新用户
    updater, updaterModel := gplus.NewUpdater[User](ctx)
    updater.Set(&updaterModel.Name, "李四").Eq(&updaterModel.ID, user.ID)
    
    affected, err := repo.UpdateByCond(updater)
    if err != nil {
        fmt.Printf("更新用户失败: %v\n", err)
    } else {
        fmt.Printf("更新成功，影响行数: %d\n", affected)
    }

    // 5. 删除用户
    rowsAffected, err := repo.DeleteById(ctx, user.ID)
    if err != nil {
        fmt.Printf("删除用户失败: %v\n", err)
    } else {
        fmt.Printf("删除成功，影响行数: %d\n", rowsAffected)
    }
}
```

## 核心功能

### Repository 模式

```go
// 创建 Repository
repo := gplus.NewRepository[uint, User](db)

// 写操作
repo.Save(ctx, &user)                          // 纯 INSERT（非 upsert）
repo.SaveBatch(ctx, users)                     // 批量 INSERT
repo.Upsert(ctx, &user)                        // insert-or-update（按主键）
repo.UpsertBatch(ctx, users)                   // 批量 upsert
repo.CreateBatch(ctx, ptrs, batchSize)         // 分批 INSERT
repo.UpdateById(ctx, &user)                    // 按主键更新非零字段
repo.UpdateByIds(ctx, ids, updater)            // 按主键列表批量更新
repo.UpdateByCond(updater)                     // 按条件批量更新
repo.IncrBy(updater, col, delta)               // 原子自增
repo.DecrBy(updater, col, delta)               // 原子自减
repo.DeleteById(ctx, 1)                        // 按主键删除
repo.DeleteByIds(ctx, []uint{1, 2, 3})         // 按主键列表批量删除
repo.InsertOnConflict(ctx, &user, oc)          // 单条带冲突处理插入
repo.InsertBatchOnConflict(ctx, users, oc)     // 批量带冲突处理插入
repo.Restore(ctx, id)                          // 按主键恢复软删除
repo.RestoreByCond(q)                          // 按条件批量恢复软删除

// 读操作
repo.GetById(ctx, 1)                           // 按主键查单条
repo.GetByIds(ctx, []uint{1, 2, 3})            // 按主键列表批量查询
repo.GetOne(query)                             // 按条件查单条
repo.Last(query)                               // 按主键倒序取第一条
repo.List(query)                               // 查询列表
repo.ListMap(query, func(u User) uint { return u.ID }) // 列表转 map
repo.Page(query, false)                        // 分页查询
repo.Count(query)                              // 计数
repo.Exists(query)                             // 判断是否存在
repo.FirstOrCreate(query, &User{Name: "张三"}) // 查找或创建
repo.FirstOrUpdate(query, updater, &User{})    // 查找或创建并更新
repo.Chunk(query, 100, fn)                     // 分批处理（主键游标）
```

### 类型安全查询构建器

```go
// 创建查询
query, model := gplus.NewQuery[User](ctx)

// 条件构建
query.Eq(&model.Name, "张三")
      .Gt(&model.Age, 18)
      .In(&model.ID, []int{1, 2, 3})
      .Like(&model.Email, "%@example.com")
      .Order(&model.ID, false)
      .Limit(10)
      .Offset(0)

// 复杂条件（AND 嵌套块）
query.And(func(sub *gplus.Query[User]) {
    sub.Eq(&model.Age, 20).OrEq(&model.IsVip, true)
})

// 连接查询
query.LeftJoin("profiles", "users.id = profiles.user_id")

// 执行查询
results, err := repo.List(query)
```

### 更新构建器

```go
// 创建更新器
updater, model := gplus.NewUpdater[User](ctx)

// 设置更新字段
updater.Set(&model.Name, "新名字")
       .Set(&model.Age, 30)
       .SetExpr(&model.Version, "version + ?", 1)

// 设置条件
updater.Eq(&model.ID, 1)
       .Gt(&model.Status, 0)

// 执行更新
affected, err := repo.UpdateByCond(updater) // 第二个参数是事务，传nil表示不用事务
```

### 事务支持

```go
err := repo.Transaction(func(tx *gorm.DB) error {
    // 在事务中执行操作
    user1 := &User{Name: "用户1", Age: 20}
    if err := repo.SaveTx(ctx, user1, tx); err != nil {
        return err
    }

    user2 := &User{Name: "用户2", Age: 25}
    if err := repo.SaveTx(ctx, user2, tx); err != nil {
        return err
    }

    return nil
})
```

### 原生条件与排序

```go
// WhereRaw：添加原生 SQL 条件（防注入：用 ? 占位符）
query.WhereRaw("YEAR(created_at) = ?", 2024)
query.WhereRaw("age > ? AND age < ?", 18, 60)

// OrderRaw：添加复杂排序表达式（与 Order 可混用，保留调用顺序）
query.OrderRaw("FIELD(status, 'active', 'pending', 'closed')")
query.OrderRaw("score DESC NULLS LAST") // PostgreSQL
query.Order(&model.CreatedAt, false).OrderRaw("FIELD(priority, 1, 2, 3)")
```

### Upsert（insert-or-update）

```go
// 无主键：执行 INSERT
// 有主键：执行 UPDATE（覆盖所有字段）
repo.Upsert(ctx, &user)
repo.UpsertBatch(ctx, users)

// 注意：Save/SaveBatch 是纯 INSERT，不会更新已有记录
// 如需只更新部分字段，使用 UpdateById 或 UpdateByCond
```

### 聚合函数

```go
// Sum/Max/Min/Avg 为包级泛型函数，需显式传入 repo
// R 为返回类型（int64、float64 等），空表或无匹配时返回零值（NULL 安全）
q, m := gplus.NewQuery[User](ctx)
q.Gt(&m.Age, 18)

total, err := gplus.Sum[User, int64, uint](repo, q, &m.Age)
max, err   := gplus.Max[User, int64, uint](repo, q, &m.Age)
min, err   := gplus.Min[User, int64, uint](repo, q, &m.Age)
avg, err   := gplus.Avg[User, float64, uint](repo, q, &m.Age)
```

### OnConflict（按唯一键 upsert）

`InsertOnConflict` / `InsertBatchOnConflict` 支持数据库原生冲突处理，覆盖四种策略：

```go
_, m := repo.NewQuery(ctx)

// 1. 冲突时跳过（幂等写入）
repo.InsertOnConflict(ctx, &user, gplus.OnConflict{
    Columns:   []any{&m.Email},
    DoNothing: true,
})
// → INSERT INTO users(...) ON CONFLICT (email) DO NOTHING

// 2. 冲突时只更新指定列
repo.InsertOnConflict(ctx, &user, gplus.OnConflict{
    Columns:   []any{&m.Email},
    DoUpdates: []any{&m.Name, &m.UpdatedAt},
})
// → ON CONFLICT (email) DO UPDATE SET name=EXCLUDED.name, updated_at=EXCLUDED.updated_at

// 3. 冲突时覆盖除主键外所有列
repo.InsertOnConflict(ctx, &user, gplus.OnConflict{
    Columns:     []any{&m.Email},
    DoUpdateAll: true,
})

// 4. 冲突时原子表达式更新（批量累加计数器）
repo.InsertBatchOnConflict(ctx, stats, gplus.OnConflict{
    Columns:     []any{&m.UserID, &m.Date},
    UpdateExprs: map[string]any{"count": gorm.Expr("count + excluded.count")},
})
// → ON CONFLICT (user_id, date) DO UPDATE SET count = count + excluded.count
```

> **方言说明**：`Columns` 在 Postgres/SQLite 中必须指定；MySQL 按唯一索引自动判定，可省略。
> `UpdateExprs` 中的表达式语法因数据库而异（MySQL 用 `VALUES(col)`，Postgres/SQLite 用 `excluded.col`）。

### 软删除恢复

```go
// 按主键恢复单条
affected, err := repo.Restore(ctx, 1)

// 按条件批量恢复（空条件返回 ErrRestoreEmpty）
q, m := gplus.NewQuery[User](ctx)
q.Eq(&m.Status, "deleted")
affected, err := repo.RestoreByCond(q)
```

### 分批处理

```go
// 主键游标分批（非 OFFSET），批次内顺序稳定
q, _ := gplus.NewQuery[User](ctx)
err := repo.Chunk(q, 100, func(batch []User) error {
    for _, u := range batch {
        // 处理每条记录
    }
    return nil
})
```

### 查找或创建 / 查找或更新

```go
// FirstOrCreate：找到则返回，找不到则用 defaults 创建
q, m := gplus.NewQuery[User](ctx)
q.Eq(&m.Email, "test@example.com")
user, created, err := repo.FirstOrCreate(q, &User{Name: "新用户", Email: "test@example.com"})

// FirstOrUpdate：找到则按 updater 更新，找不到则用 defaults 创建
u, um := gplus.NewUpdater[User](ctx)
u.Set(&um.Name, "更新名字")
user, created, err := repo.FirstOrUpdate(q, u, &User{Name: "新用户"})
```

### 原子增减

```go
// 对 score 字段原子 +10，附带 WHERE 条件
u, m := gplus.NewUpdater[User](ctx)
u.Eq(&m.ID, 1)
affected, err := repo.IncrBy(u, &m.Score, 10)
affected, err  = repo.DecrBy(u, &m.Score, 5)
```

### ListMap（列表转 map）

```go
// 查询结果直接转换为 map，key 由回调函数决定
q, m := gplus.NewQuery[User](ctx)
q.Eq(&m.IsVip, true)
userMap, err := repo.ListMap(q, func(u User) uint { return u.ID })
// userMap 类型为 map[uint]User，可按 ID 直接取值
user := userMap[42]
```

### Pluck（提取单列）

```go
// 提取单列值，返回指定类型的切片
q, m := gplus.NewQuery[User](ctx)
q.Gt(&m.Age, 18).Order(&m.ID, true)
names, err := gplus.Pluck[User, string, uint](repo, q, &m.Name)
// names 类型为 []string
```

### FindAs / FindOneAs — 投影查询（Query-chain-safe）

走 GORM Query callback chain，下游挂在 Query chain 上的隔离/审计 callback 会触发。

```go
type UserVO struct {
    Name     string  // 默认匹配 SELECT 列 `name`（snake_case）
    DeptName string  // 匹配 `dept_name`（必须 alias，否则字段名冲突时无法定位）
}

// 多行
var rows []UserVO
q, _ := gplus.NewQuery[User](ctx)
q.LeftJoin("dept", "users.dept_id = dept.id").
    Select("users.name", "dept.name AS dept_name")
err := gplus.FindAs(repo, q, &rows)

// 单行（无匹配返回 gorm.ErrRecordNotFound）
var one UserVO
q, m := gplus.NewQuery[User](ctx)
q.Eq(&m.ID, 1)
err := gplus.FindOneAs(repo, q, &one)

// 事务
err := db.Transaction(func(tx *gorm.DB) error {
    q, m := gplus.NewQuery[User](ctx)
    q.Eq(&m.Status, "active")
    return gplus.FindAsTx(repo, q, &rows, tx)
})
```

**主表 vs 结果结构心智模型**：
- `Repository[D, T]` 绑定主表 schema（T）+ 提供 dbResolver / ctx 注入
- `Dest` 仅决定 SELECT 列 → struct 字段映射（GORM 默认 snake_case）
- 跨表 JOIN 列必须 SQL alias，否则字段名冲突 GORM 无法定位

> ⚠️ `SelectExpr`（v0.11.0+）的表达式列**无 AS 别名**，FindAs/FindOneAs 按名映射不可用。需读取表达式结果时改用 `SelectRaw("expr AS col_name", args...)` 配合 Dest 字段，或单列场景用 `RawScan`。

### PageAs / PageAsTx — 投影分页（Query-chain-safe）

等价于 `repo.Page`，但把分页结果投影到自定义 `Dest`（JOIN 多表 + VO 场景），走 GORM Query callback chain，下游挂在 Query chain 上的隔离/审计 callback 会触发（与 FindAs 一致）。

```go
type UserVO struct {
    Name     string
    DeptName string
}

q, _ := gplus.NewQuery[User](ctx)
q.LeftJoin("dept", "users.dept_id = dept.id").
    Select("users.name", "dept.name AS dept_name").
    Page(1, 20) // 第 1 页，每页 20 条

var rows []UserVO
total, err := gplus.PageAs(repo, q, &rows, false)
// total = 满足条件的总行数；skipCount=true 时 total 恒为 0

// 事务版本
err := db.Transaction(func(tx *gorm.DB) error {
    var rows []UserVO
    _, err := gplus.PageAsTx(repo, q, &rows, false, tx)
    return err
})
```

**要点**：
- 返回 `(total int64, err error)`；`skipCount=true` 跳过 COUNT（`total` 恒为 0），适合不需要总数的场景
- `skipCount=false` 时先执行 COUNT；若总数为 0 则提前返回，不执行投影 Find
- 与 `FindOneAs` 的区别：内部用 `Find` 不追加 `LIMIT 1`，正是要与 `q.Page()` 设的 `LIMIT/OFFSET` 协同
- **副作用**：调用后 `q` 会永久追加 DataRule 条件（`dataRuleApplied` 保护幂等），不应再跨不同 `ctx` 复用，与 `FindAs` 行为一致

### 类型化投影表达式 + InsertSelectMap（v0.11.0）

`INSERT ... SELECT ... JOIN` 这类跨表写操作可做到**零手写 SQL 字符串**：表达式列用类型化算子树（`Col`/`Lit`/`Add`）表达，目标列用字段指针（`Model[T]()`），target/source 成对声明，列数不匹配与顺序错位从「运行时数据错」提升为「结构上不可能」。

```go
// Model[T]() 返回规范单例指针，用于取目标表字段地址（⚠️ 只读，禁写字段值）
m := gplus.Model[Closure]()

// 闭包表自连接搬移：INSERT INTO closure(ancestor_id, descendant_id, depth)
//                    SELECT ext.ancestor_id, sub.descendant_id, ext.depth + sub.depth + 1 ...
q, ext := repo.NewQueryAs(ctx, "ext")
sub := gplus.As[Closure](q, "sub")
q.CrossJoinAs(sub).Eq(&sub.AncestorID, 5).Eq(&ext.DescendantID, 5)

affected, err := gplus.InsertSelectMap(repo, ctx, []gplus.InsertCol{
    {Target: &m.AncestorID,   Src: gplus.Col(&ext.AncestorID)},
    {Target: &m.DescendantID, Src: gplus.Col(&sub.DescendantID)},
    {Target: &m.Depth,        Src: gplus.Add(gplus.Col(&ext.Depth), gplus.Col(&sub.Depth), gplus.Lit(1))},
}, q)
// 事务版：gplus.InsertSelectMapTx(repo, ctx, tx, cols, q)
```

也可单独用 `q.SelectExpr` 追加类型化投影列：

```go
q, m := repo.NewQuery(ctx)
q.SelectExpr(gplus.Add(gplus.Col(&m.Depth), gplus.Lit(1))).Eq(&m.DescendantID, 5)
```

**要点**：
- `Col(&model.Field)` 字段引用（地址在 SelectExpr/InsertSelectMap **调用期**解析，改列名构建期即报错）；`Lit(val)` 字面量走参数化绑定（防注入）；`Add(...)` 变长加法（YAGNI：当前仅 `+`）
- `InsertSelectMap` 的 src **不得有手动投影**（Select/SelectRaw/SelectExpr），否则返回 `ErrInsertSelectMapConflict`（投影由映射 API 独占设置）
- ⚠️ **成功后 `q` 被永久追加投影**——同一 `q` 二次调用必撞 `ErrInsertSelectMapConflict`（天然防重入）。需多次 INSERT 时每次 `NewQuery` 新建独立 Query；失败路径对 `q.selects` 零副作用
- Target 解析失败（包级解析）返回 `ErrColumnNotFound` 且 `q` 可复用；Src 的 Col 失败（alias 链）返回 `ErrFieldAddrUnregistered` 且 `q` 不可复用须新建
- **选型**：新代码一律用 `InsertSelectMap`（成对声明，列对位由结构保证）；仅当目标列名来自动态来源（运行期字符串）或维护既有调用时用 `InsertSelect`

### 数据权限（DataRule）

`DataRule` 通过 `context.Context` 传入，由 Repository 方法自动应用到所有查询和写操作，无需在每处手动添加条件。适合多租户、行级权限等场景。

```go
// 定义数据权限规则（通常在中间件中设置）
rules := []gplus.DataRule{
    {Column: "tenant_id", Condition: "=", Value: "tenant-abc"},
    {Column: "deleted_at", Condition: "IS NULL"},
}
ctx = context.WithValue(ctx, gplus.DataRuleKey, rules)

// 之后所有使用该 ctx 的查询都会自动附加上述条件
q, m := gplus.NewQuery[User](ctx)
q.Eq(&m.IsVip, true)
// 实际执行：WHERE is_vip = true AND tenant_id = 'tenant-abc' AND deleted_at IS NULL
users, err := repo.List(q)
```

> **注意**：`DataRule.Column` 仅支持字母/数字/下划线/点，含括号或运算符的表达式会被拒绝以防注入。

`Condition` 支持的操作符（大小写不敏感，其余值返回错误；`SQL`/`USE_SQL_RULES` 显式拒绝防注入）：

| Condition | 说明 | 值来源 |
|---|---|---|
| `=` `<>` `>` `>=` `<` `<=` | 比较 | `Value` |
| `IN` / `NOT IN` | 多值包含 | `Values`（优先）或 `Value` 逗号分隔 |
| `LIKE` | 模糊匹配，自动双侧包 `%值%` | `Value` |
| `LEFT_LIKE` / `RIGHT_LIKE` | 自动补为 `%值` / `值%` | `Value` |
| `IS NULL` / `IS NOT NULL` | 空判断（无需值） | — |
| `BETWEEN` | 区间（恰好 2 个值） | `Values`（优先）或 `Value` 逗号分隔 |

#### 跨表数据权限：`DataRule.Table`

JOIN 多表查询时，用 `Table` 字段把数据权限规则限定到指定表 / JOIN 别名：

```go
rules := []gplus.DataRule{
    {Table: "ext", Column: "dept_id", Condition: "=", Value: "1"}, // 生成 ext.dept_id = 1
}
ctx = context.WithValue(ctx, gplus.DataRuleKey, rules)
```

- `Table` 的值必须与查询中 JOIN 别名（`As[X]` 注册名 / `NewQueryAs` 主别名）**字符串一致**；gplus 不校验别名是否存在，拼错由数据库执行期报错。
- `Table` 仅允许单段标识符（不含点 / 空白）；`Table` 非空时 `Column` 必须是裸列名（不得再含点）。
- 向后兼容：不填 `Table`、在 `Column` 写 `"ext.dept_id"` 点前缀的旧写法仍可用，但新代码建议用 `Table`。

### 悲观锁查询（GetByLock）

`GetByLock` 必须在事务中使用，否则返回 `ErrTransactionReq`。

```go
err := repo.Transaction(func(tx *gorm.DB) error {
    q, m := gplus.NewQuery[User](ctx)
    q.Eq(&m.ID, 1).LockForUpdate() // FOR UPDATE
    user, err := repo.GetByLock(ctx, q, tx)
    if err != nil {
        return err
    }
    // 在锁持有期间更新数据
    u, um := gplus.NewUpdater[User](ctx)
    u.Set(&um.Score, user.Score+10).Eq(&um.ID, user.ID)
    _, err = repo.UpdateByCondTx(u, tx)
    return err
})
```

### 便捷构建器（repo.NewQuery / repo.NewUpdater）

从 v0.3.0 起，Repository 提供 `NewQuery`/`NewUpdater` 方法，无需重复指定泛型参数：

```go
repo := gplus.NewRepository[uint, User](db)

// 等价于 gplus.NewQuery[User](ctx)，类型由 repo 自动推导
q, m := repo.NewQuery(ctx)
q.Eq(&m.IsVip, true)
users, err := repo.List(q)

// 等价于 gplus.NewUpdater[User](ctx)
u, um := repo.NewUpdater(ctx)
u.Set(&um.Name, "新名字").Eq(&um.ID, 1)
affected, err := repo.UpdateByCond(u)
```

### 自定义 Scope 注入

```go
// WithScope 注入任意 GORM scope，支持 Query 和 Updater
q, m := gplus.NewQuery[User](ctx)
q.Eq(&m.IsVip, true).
    WithScope(func(db *gorm.DB) *gorm.DB {
        return db.Scopes(myTenantScope)
    })
```

### 原生 SQL 查询

```go
// 查询原始 SQL
users, err := repo.RawQuery(ctx, "SELECT * FROM users WHERE age > ?", 18)

// 执行原始 SQL
affected, err := repo.RawExec(ctx, "UPDATE users SET status = ? WHERE id = ?", 1, 123)

// 查询到自定义结构
var userStats []UserStats
err = repo.RawScan(ctx, &userStats, "SELECT name, COUNT(*) as count FROM users GROUP BY name")
```

## 条件操作符

GPlus 支持丰富的条件操作符，所有操作符都在 `consts.go` 中定义：

| 方法名 | 操作符 | 常量 | 说明 |
|--------|--------|------|------|
| `Eq()` | `=` | `OpEq` | 等于 |
| `Ne()` | `<>` | `OpNe` | 不等于 |
| `Gt()` | `>` | `OpGt` | 大于 |
| `Ge()` | `>=` | `OpGe` | 大于等于 |
| `Lt()` | `<` | `OpLt` | 小于 |
| `Le()` | `<=` | `OpLe` | 小于等于 |
| `Like()` | `LIKE` | `OpLike` | 模糊匹配 |
| `NotLike()` | `NOT LIKE` | `OpNotLike` | 非模糊匹配 |
| `In()` | `IN` | `OpIn` | 包含 |
| `NotIn()` | `NOT IN` | `OpNotIn` | 不包含 |
| `IsNull()` | `IS NULL` | `OpIsNull` | 为空 |
| `IsNotNull()` | `IS NOT NULL` | `OpIsNotNull` | 不为空 |
| `Between()` | `BETWEEN` | `OpBetween` | 在范围内 |
| `NotBetween()` | `NOT BETWEEN` | `OpNotBetween` | 不在范围内 |
| `LikeLeft()` | `LIKE` | - | 左模糊查询（自动补 `%val`） |
| `LikeRight()` | `LIKE` | - | 右模糊查询（自动补 `val%`） |
| `OrEq()` | `=` | - | OR 等于 |
| `OrNe()` | `<>` | - | OR 不等于 |
| `OrGt()` | `>` | - | OR 大于 |
| `OrGe()` | `>=` | - | OR 大于等于 |
| `OrLt()` | `<` | - | OR 小于 |
| `OrLe()` | `<=` | - | OR 小于等于 |
| `OrLike()` | `LIKE` | - | OR 模糊匹配 |
| `OrIn()` | `IN` | - | OR 包含 |
| `OrIsNull()` | `IS NULL` | - | OR 为空 |
| `OrIsNotNull()` | `IS NOT NULL` | - | OR 不为空 |

## 连接操作

支持多种连接类型，所有连接类型常量在 `consts.go` 中定义：

| 方法名 | 常量 | 说明 |
|--------|------|------|
| `LeftJoin()` | `JoinLeft` | 左连接：返回左表所有记录，即使右表无匹配 |
| `RightJoin()` | `JoinRight` | 右连接：返回右表所有记录，即使左表无匹配 |
| `InnerJoin()` | `JoinInner` | 内连接：仅返回两个表中匹配的记录（交集） |
| `OuterJoin()` | `JoinOuter` | 裸 OUTER JOIN（非标准 SQL，多数数据库不支持，建议用 `FullJoin`） |
| `FullJoin()` | `JoinFull` | 全外连接：返回左右表中所有的记录 |
| `CrossJoin()` | `JoinCross` | 交叉连接：返回笛卡尔积 |
| `NaturalJoin()` | `JoinNatural` | 自然连接：基于相同列名自动匹配 |

使用示例：
```go
// 左连接
query.LeftJoin("dept", "user.dept_id = dept.id")

// 内连接  
query.InnerJoin("role", "user.role_id = role.id")

// 右连接
query.RightJoin("profile", "user.id = profile.user_id")

// 交叉连接
query.CrossJoin("settings")

// 自然连接
query.NaturalJoin("user_settings")
```

## 错误变量

| 变量 | 触发时机 |
|------|---------|
| `ErrQueryNil` | 传入 nil 的 Query/Updater |
| `ErrRawSQLEmpty` | `RawQuery`/`RawExec`/`RawScan` 传入空字符串 |
| `ErrDeleteEmpty` | `DeleteByCondTx` 无条件且未调用 `Unscoped()` |
| `ErrUpdateEmpty` | `UpdateByCond` 没有设置任何字段 |
| `ErrUpdateNoCondition` | `UpdateByCond` 有字段但没有 WHERE 条件 |
| `ErrTransactionReq` | `GetByLock` 未在事务中调用 |
| `ErrDefaultsNil` | `FirstOrCreate`/`FirstOrUpdate` 传入 nil defaults |
| `ErrRestoreEmpty` | `RestoreByCond`/`RestoreByCondTx` 无条件 |
| `ErrInsertSelectMapConflict` | `InsertSelectMap` 的 src 已有手动投影（Select/SelectRaw/SelectExpr） |
| `ErrExprEmpty` | `Add()` 无操作数（表达式至少需一个操作数） |

## 集成方式

### 方式一：直接内嵌 Repository（推荐）

将 `gplus.Repository` 内嵌到你的业务 Repository 结构体中，即可直接使用所有 CRUD 方法，同时可以在结构体上添加自定义业务方法。

```go
type UserRepository struct {
    gplus.Repository[uint, User]
}

func NewUserRepository(db *gorm.DB) *UserRepository {
    return &UserRepository{
        Repository: gplus.NewRepository[uint, User](db),
    }
}

// 添加自定义业务方法
func (r *UserRepository) FindActiveVips(ctx context.Context) ([]User, error) {
    q, m := gplus.NewQuery[User](ctx)
    q.Eq(&m.IsVip, true).Eq(&m.Status, "active")
    return r.List(q)
}
```

使用时：

```go
repo := NewUserRepository(db)

// 直接使用内嵌的通用方法
user, err := repo.GetById(ctx, 1)

// 使用自定义业务方法
vips, err := repo.FindActiveVips(ctx)
```

### 方式二：依赖注入（适合 DI 框架）

将 `*gplus.Repository` 作为字段注入，适合 Wire、Fx 等依赖注入框架。

```go
type UserService struct {
    userRepo *gplus.Repository[uint, User]
    orderRepo *gplus.Repository[uint, Order]
}

func NewUserService(
    userRepo *gplus.Repository[uint, User],
    orderRepo *gplus.Repository[uint, Order],
) *UserService {
    return &UserService{
        userRepo:  userRepo,
        orderRepo: orderRepo,
    }
}

func (s *UserService) GetUserOrders(ctx context.Context, userID uint) ([]Order, error) {
    q, m := gplus.NewQuery[Order](ctx)
    q.Eq(&m.UserID, userID).Order("created_at DESC")
    return s.orderRepo.List(q)
}
```

### 方式三：全局单例（简单项目）

适合小型项目或脚本，直接在包级别声明 Repository 变量。

```go
var (
    UserRepo  = gplus.NewRepository[uint, User](db)
    OrderRepo = gplus.NewRepository[uint, Order](db)
)
```

## 项目结构

```
gplus/
├── builder.go      # 查询构建器核心
├── consts.go       # 常量定义
├── query.go        # 查询构建器
├── repository.go   # Repository 模式实现
├── schema.go       # 模型结构解析
├── update.go       # 更新构建器
├── utils.go        # 工具函数
├── go.mod          # Go 模块定义
└── go.sum          # 依赖校验
```

## 性能优化

- **智能缓存**：模型结构解析结果缓存，减少反射开销
- **预分配内存**：查询条件切片预分配，减少内存分配次数
- **零分配设计**：关键路径避免内存分配

## 依赖

- Go 1.24+
- GORM v1.31.1+

## 方言支持

> 完整方言路线图（含未来候选方言 OceanBase / GaussDB / openGauss、v0.9 多模架构方向、新方言贡献流程）见 [docs/ROADMAP.md](docs/ROADMAP.md)

| 数据库 | 状态 | CI 验证 | 备注 |
|---|---|---|---|
| SQLite | ✅ 完整 | ✓ `:memory:` | 默认开发与单元测试方言；`getQuoteChar` 返回 `"` |
| MySQL 8.0+ | ✅ 完整 | ✓ `mysql:8.0` service | `getQuoteChar` 返回 `` ` ``；ON CONFLICT 用 `VALUES(col)` 表达式 |
| PostgreSQL 16+ | ✅ 完整 | ✓ `postgres:16` service | `getQuoteChar` 返回 `"`；ON CONFLICT 用 `excluded.col` 表达式 |
| Oracle 12c+ | ⚠️ build tag | ✗ 不在 CI（启动慢） | 用 `go test -tags=oracle` 跑；`getQuoteChar` 返回空 quoter（避免 ORA-00904，详见已知陷阱） |
| DM 8 (Oracle 兼容) | ⚠️ build tag | ✗ 不在 CI（镜像大） | 用 `go test -tags=dm` 跑；`getQuoteChar` 返回双引号（与 postgres 一致，dameng migrator 引号 lowercase 建表） |
| SQL Server | ⚠️ 部分 | ✗ | `getQuoteChar` 返回 `[ ]`；未在 CI 验证，alias 体系未实测 |
| TiDB | ⚠️ 别名走 MySQL 分支 | ✗ | `getQuoteChar` 返回反引号同 MySQL；未在 CI 验证 |

**已知方言差异**（详见"已知陷阱"章节）：
- MySQL 1093：UPDATE 目标表不能与子查询 FROM 同表
- PG `42702`：ON CONFLICT DO UPDATE 中裸列名 + EXCLUDED 同名时视为歧义，须用表名限定
- PG 严格 SQL：HAVING 不可引用 SELECT 列别名，须重复聚合表达式
- LIKE 大小写敏感性：MySQL 默认 `utf8mb4_general_ci` 不敏感、PG 默认敏感、SQLite 默认不敏感
- 占位符：MySQL/SQLite 用 `?`，PG 用 `$N`（驱动统一处理，库代码方言无关）
- Oracle 限制（详见 spec `docs/superpowers/specs/2026-05-07-oracle-support-design.md`）：
  - `gplus.getQuoteChar` 返回空 quoter——godoes/gorm-oracle migrator 用 UPPERCASE 不带引号建表，加双引号会触发 ORA-00904；列名是 Oracle 保留字（order/size/level）时需用户手动加引号
  - `''` 自动转 NULL（致命差异，影响 IsNull / Empty 判断）
  - 输出列名默认 UPPERCASE：RawScan 映射小写 struct tag 时需 SQL 显式 `AS "col"` 锁定 lowercase
  - CLOB/TEXT 字段不能直接 WHERE，所有 string 字段须显式 `gorm:"size:N"` 约束
  - NULLS LAST 排序默认（与 PG 升序相反）
  - RETURNING 仅支持单行（影响 SaveBatch/UpsertBatch，本期 t.Skip）
  - 标识符长度 30/128 字符上限
  - 不支持 ON CONFLICT（用 MERGE INTO）
- DM 8 限制（Oracle 兼容模式，详见 spec `docs/superpowers/specs/2026-05-08-dm-support-design.md` 与下方"DM 数据库支持"章节）：
  - `gplus.getQuoteChar` 返回双引号——**与 Oracle 不共用空 quoter**：godoes/gorm-dameng migrator 实测用引号 lowercase 建表（`CREATE TABLE "username" ...`），列存为 case-sensitive 小写，DM CASE_SENSITIVE=Y 下裸标识符会被 UPPERCASE 解析触发 `Error -2111 无效的列名`，必须用双引号锁定小写
  - 继承 Oracle 兼容模式全部限制：`''=NULL` / 输出 UPPERCASE / CLOB WHERE / NULLS LAST / RETURNING 单行 / 标识符长度 / 无 ON CONFLICT
  - DM 特有：`COMPATIBLE_MODE=2` 必须显式开启（docker run 加 `-e COMPATIBLE_MODE=2`，`SELECT PARA_VALUE FROM V$DM_INI WHERE PARA_NAME='COMPATIBLE_MODE'` 应返回 2）
  - 镜像默认密码版本差异：dameng 历史镜像有 `SYSDBA` / `SYSDBA001` / 首登强制改密——以拉到的镜像 README 为准

## Oracle 数据库支持（v0.8.2+）

> 仅本地/CI 验证场景。生产侧使用见下方"下游生产侧集成"。

### 1. Quickstart 4 步

1. **拉 gplus**：`go get github.com/yi-nanping/gplus@v0.8.3`
2. **拉 Oracle 23c Free 镜像**：`docker pull container-registry.oracle.com/database/free:latest`（参见下方"启动 Oracle 23c Free 容器"）
3. **设 DSN 环境变量**（可选，setup_test 有本地默认）：`export TEST_ORACLE_DSN="oracle://system:<密码>@127.0.0.1:1521/FREEPDB1"`
4. **跑测试**：`go test -tags=oracle -v ./...`（强制不漏跑：`TEST_ORACLE_REQUIRED=1 go test -tags=oracle ./...` —— DSN 不通时 `t.Fatalf` 而非 `t.Skip`）

### 2. TEST_ORACLE_DSN 格式

BNF：

```
TEST_ORACLE_DSN := "oracle://" <user> ":" <password> "@" <host> ":" <port> "/" <service_name>
```

样例：

```bash
# Oracle 23c Free Docker 镜像默认（system / oracle / FREEPDB1）
export TEST_ORACLE_DSN="oracle://system:oracle@127.0.0.1:1521/FREEPDB1"

# 切换到独立测试账户（生产/CI 强烈推荐，仅授 CONNECT + RESOURCE）
export TEST_ORACLE_DSN="oracle://testuser:<密码>@127.0.0.1:1521/FREEPDB1"

# 19c/12c 经典 SID 模式
export TEST_ORACLE_DSN="oracle://system:<密码>@127.0.0.1:1521/ORCL"
```

> **⚠ 安全提示**：`oracle_setup_test.go` 中的 `defaultOracleDSN = "oracle://system:oracle@127.0.0.1:1521/FREEPDB1"` **仅限本地 Docker 开发**。`system` 是 DBA 权限账户，`oracle` 是 Oracle 23c Free 镜像出厂默认密码——绝不能用于生产。CI/生产必须用 `TEST_ORACLE_DSN` 覆盖。

### 3. 下游生产侧集成

```go
import (
    oracle "github.com/godoes/gorm-oracle"
    "gorm.io/gorm"
    "github.com/yi-nanping/gplus"
)

func main() {
    db, _ := gorm.Open(oracle.Open("oracle://system:..."), &gorm.Config{})
    repo := gplus.NewRepository[int64, User](db)
    // ... 与 sqlite/mysql/pg 完全一样
}
```

gplus 自身**不预先注册** Dialector，下游需自己 `import _ "github.com/godoes/gorm-oracle"`（或显式 `gorm.Open(oracle.Open(...))`）。

### 4. quoter 策略与列名匹配（重要）

Oracle 走**空 quoter**（不加任何引号），**不与 DM 共用双引号 quoter**——这是 v0.8.2 立项时实测决策：

- godoes/gorm-oracle migrator 用 `CREATE TABLE my_sql_users (USERNAME VARCHAR2(64),...)` UPPERCASE 不带引号建表，列名在 Oracle 中存为 UPPERCASE
- 若 gplus 给列名加双引号（`"username"`），Oracle case-sensitive 解析为小写 `username`，与 DB 内 UPPERCASE 列名 `USERNAME` 不匹配，触发 `ORA-00904 invalid identifier`
- gplus 在 oracle 方言下 `getQuoteChar` 返回 `("", "")` 空 quoter，让裸标识符走 Oracle 默认 UPPERCASE 大小写无关解析路径

下游手写 RawSQL/WhereRaw 时遇到 Oracle 保留字列名（`order` / `size` / `level` / `comment` / `type` / `group` / `role` / `number` / `date` 等），需**手动**加双引号包裹（如 `WHERE "order" = ?`），gplus 不会自动加（参见 TD-14）。

### 5. 错误码导航

| 错误码 | 触发场景 | 措施 |
|---|---|---|
| `ORA-00904 invalid identifier` | gplus 给列名加双引号但 migrator UPPERCASE 不带引号建表（列存为大写）| 确认 `getQuoteChar` 在 oracle 方言返回空 quoter（v0.8.2+ 默认）；RawSQL 中保留字列名手动加双引号 |
| `ORA-00932 inconsistent datatypes` | string 字段映射 CLOB 后参与 LIKE/IN/=（Oracle CLOB 不支持等值比较）| struct 字段加 `gorm:"size:N"`（N ≤ 4000）显式映射 VARCHAR2；或用 `DBMS_LOB.SUBSTR(col, ...)` 截取后比较 |
| `ORA-01430 column being added already exists` | migrator 重复 ALTER ADD（已存在表再次 AutoMigrate）| setup 走 DROP+AutoMigrate 路径，参考 `oracle_setup_test.go` 的 `truncateOracleTables` |
| `ORA-00942 table or view does not exist` | schema 不对 / 表未创建 / 跨 schema 访问无权限 | 确认 `TEST_ORACLE_DSN` 末尾 service_name 与建表 schema 一致；跨 schema 用 `OWNER.TABLE` 限定 |
| `ORA-01017 invalid username/password` | Oracle 23c Free 默认 system/oracle 改过 / PDB 与 CDB 账户混淆 | docker run 时设 `ORACLE_PWD`；连 PDB（FREEPDB1）而非 CDB$ROOT |
| `ORA-12541 TNS: no listener` | 容器没起 / 端口未映射 / 监听器未启 | `docker ps` 看容器 + `docker logs` 看是否 "DATABASE IS READY TO USE!"；首次启动需 1-3 分钟 |
| `ORA-00001 unique constraint violated` | 主键/唯一索引冲突 | 用 `gplus.Upsert` 或 `OnConflict.DoNothing` 处理冲突场景 |
| `ORA-65096 invalid common user/role name` | 在 CDB 创建普通用户名未以 `C##` 开头 | 用 `oracle://user@host:port/FREEPDB1` 连 PDB 而非 CDB；或加 `C##` 前缀 |

### 6. 启动 Oracle 23c Free 容器

```bash
# 拉镜像（约 8GB，首次较慢）
docker pull container-registry.oracle.com/database/free:latest

# 启动（默认 system/oracle，PDB 名 FREEPDB1，端口 1521）
docker run -d --name oracle-free -p 1521:1521 \
  -e ORACLE_PWD=oracle \
  container-registry.oracle.com/database/free:latest

# 等待启动完成（首次约 1-3 分钟，看 logs 出现 "DATABASE IS READY TO USE!"）
docker logs -f oracle-free

# 验证连接
docker exec -it oracle-free sqlplus system/oracle@FREEPDB1
```

> 启动慢（>1 分钟）：Oracle 23c Free 首次启动需要初始化 PDB，是预期行为，与 spec §3.3 "Oracle 不在 CI"决策一致。
>
> WSL2 用户：与下方 DM 章节 "WSL2 用户必读" 同样适用——容器跟随 distro idle stop 风险一致，workaround 三方案（终端常开 / 后台 wsl.exe / 任务计划）通用。

## DM 数据库支持（v0.8.3+）

> 仅本地/CI 验证场景。生产侧使用见下方"下游生产侧集成"。

### 1. Quickstart 5 步

1. **拉 gplus**：`go get github.com/yi-nanping/gplus@v0.8.3`
2. **拉 DM 8 镜像**：从 [dameng 技术社区](https://eco.dameng.com/) 下载 DM 8 docker tar 包，`docker load` 后 `docker run` 启动（参见下方"启动 DM 8 容器"）
3. **设 DSN 环境变量**：`export TEST_DM_DSN="dm://SYSDBA:<密码>@127.0.0.1:5236"`（密码以镜像 README 为准）
4. **跑测试**：`go test -tags=dm -v ./...`（强制不漏跑：`TEST_DM_REQUIRED=1 go test -tags=dm ./...` —— DSN 不通时 `t.Fatalf` 而非 `t.Skip`）
5. **遇错查表**：见下方"错误码导航"

### 2. TEST_DM_DSN 格式

BNF：

```
TEST_DM_DSN := "dm://" <user> ":" <password> "@" <host> ":" <port> [ "/" <schema> ] [ "?" <params> ]
```

样例：

```bash
# 本地 docker 默认实例（密码以镜像 README 为准，常见 SYSDBA / SYSDBA001 / 自定义）
export TEST_DM_DSN="dm://SYSDBA:<密码>@127.0.0.1:5236"

# 指定 schema 切换
export TEST_DM_DSN="dm://SYSDBA:<密码>@127.0.0.1:5236/MYSCHEMA"

# 字符集参数（dameng 驱动支持的连接参数见 godoes/gorm-dameng README）
export TEST_DM_DSN="dm://SYSDBA:<密码>@127.0.0.1:5236?charset=utf8"
```

### 3. 下游生产侧集成

```go
import (
    dameng "github.com/godoes/gorm-dameng"
    "gorm.io/gorm"
    "github.com/yi-nanping/gplus"
)

func main() {
    db, _ := gorm.Open(dameng.Open("dm://SYSDBA:..."), &gorm.Config{})
    repo := gplus.NewRepository[int64, User](db)
    // ... 与 sqlite/mysql/pg 完全一样
}
```

gplus 自身**不预先注册** Dialector，下游需自己 `import _ "github.com/godoes/gorm-dameng"`（或显式 `gorm.Open(dameng.Open(...))`）。

### 4. quoter 策略与列名匹配（重要）

DM 8 走**双引号 quoter**（与 PostgreSQL/SQLite 一致），**不与 Oracle 共用空 quoter**——这是 v0.8.3 实施期实测推翻 spec 早期假设后的最终决策：

- godoes/gorm-dameng v0.7.2 migrator 用 `CREATE TABLE "my_sql_users" ("username" VARCHAR(64),...)` 引号 lowercase 建表，列名在 DM 中存为 case-sensitive 小写
- DM CASE_SENSITIVE=Y + Oracle 兼容模式下，裸标识符 `username` 会被 UPPERCASE 解析为 `USERNAME`，与 DB 内的小写实际列名不匹配，触发 `Error -2111 无效的列名`
- gplus 在 dm 方言下用 `"`/`"` 自动给列名加引号（gorm-dameng Dialector.QuoteTo 也会自动加引号），SELECT/UPDATE/DELETE 路径下匹配 case-sensitive 小写

下游手写 RawSQL/WhereRaw 时也需注意：列名必须用双引号包裹保持小写（`WHERE "username" = ?`），否则 DM 仍会 UPPERCASE 解析失败。

### 5. 保留字 → 措施对照表

DM 8 Oracle 兼容模式继承 Oracle 全部保留字（`order` / `size` / `level` / `comment` /
`type` / `group` / `role` / `number` / `date` 等）。即使走双引号 quoter，下游遇到保留字
列名时仍按优先级处理：

| 优先级 | 措施 | 示例 |
|---|---|---|
| 1（推荐） | 改 struct tag `column:` 避开 | `Order int gorm:"column:ord_no"` |
| 2 | 用 RawSQL/WhereRaw 加双引号 | `q.WhereRaw(\`"order" = ?\`, 100)` |
| 3 | 等 v1.0+ 保留字自动检测能力（参见 TD-14） | — |

### 6. 错误码导航

| 错误码 | 触发场景 | 措施 |
|---|---|---|
| `Error -2111 无效的列名` | 裸标识符大小写不匹配（如 `username` 被 UPPERCASE 解析为 `USERNAME`） | 用双引号锁定小写，或检查 `getQuoteChar` 返回的 quoter 是否是双引号（v0.8.3+） |
| `ORA-00932` 等价（数据类型不一致） | string 长字段映射 CLOB 后 LIKE/IN | struct 字段加 `gorm:"size:N"` 显式约束 |
| `ORA-01430` 等价（列已存在） | migrator 重复 ALTER ADD | setup 走 DROP+AutoMigrate 路径，参考 `dm_setup_test.go` 的 `truncateDMTables` |
| 网络通信异常 / connect failed | DSN 密码不对 / 端口不通 | 镜像默认密码以 README 为准；首登强制改密版本需先 `ALTER USER SYSDBA IDENTIFIED BY ...` |

### 7. 验证 COMPATIBLE_MODE=2 生效

```sql
-- 容器内 disql 执行
SELECT PARA_VALUE FROM V$DM_INI WHERE PARA_NAME='COMPATIBLE_MODE';
-- 应返回 2（Oracle 兼容）；若不是 2，docker run 加 -e COMPATIBLE_MODE=2 重建
```

### 8. 未验证场景兜底声明

v0.8.3 仅验证：DM 8 Oracle 兼容模式（COMPATIBLE_MODE=2）+ 单实例 + UTF-8 + 开发环境。**未验证**（下游需自行验证或避开）：

- 国密 SM3/SM4 加密列
- Kerberos 认证
- DSC 集群 / 读写分离 / 容灾双活
- DM 7 及更老版本（spec §1.2 排除）
- DM MySQL/PG/TD 兼容模式（v0.8.4+ 候选；切换后 quoter 策略需重测）

### 启动 DM 8 容器（WSL2 + Docker Engine）

> **⚠ WSL2 用户必读**：v0.8.3 实施期实测发现，WSL2 distro 在"无 wsl.exe 进程 attached"时会被 auto stop，distro stop 时 dockerd 被 SIGTERM 拖死所有内部容器（DM 容器走完整 graceful shutdown 序列，不是 crash 但服务不可用）。**关闭终端后约 1 分钟容器自动死**。
>
> **以下方案实测无效（请勿浪费时间尝试）**：
>
> - `.wslconfig` 设 `vmIdleTimeout=-1` 或 `vmIdleTimeout=4294967295` —— 实测（WSL 2.6.3.0）无论数值如何，distro 仍约 60 秒后 idle stop
> - distro 内 systemd `sleep infinity` keep-alive service —— distro 内进程不影响 lifecycle 判断
>
> **实测有效的 workaround（按推荐度排序）**：
>
> | 方案 | 优 | 劣 |
> |---|---|---|
> | A. 跑测试期间保持终端 attached（最简单） | 零配置 | 必须有人开着终端 |
> | B. PowerShell `Start-Process wsl -ArgumentList '-d','Ubuntu-24.04','-e','sleep','infinity' -WindowStyle Hidden` | 单条命令，立即生效 | 重启 Windows 后失效，需重新跑 |
> | C. Windows 任务计划：登录时自动跑 `wsl -d Ubuntu-24.04 -e sleep infinity`（隐藏窗口） | 一次配置，开机生效 | 需手动配置任务计划 |
>
> 三个方案的核心都是：**Windows 主机持续有一个 wsl.exe 进程 attached** 防止 distro idle stop。本机自验证：跑 `wsl -d Ubuntu-24.04 -e sleep 200` 后台进程，90 秒后 distro 仍 Running ✅。

```bash
# 加载 dameng 技术社区 tar 包（或自构建镜像）
wsl -d Ubuntu-24.04 -e docker load -i /mnt/d/downloads/dm8.tar

# 启动（单行，避免续行符不透传）
wsl -d Ubuntu-24.04 -e docker run -d --name dm8 -p 5236:5236 \
  -e INSTANCE_NAME=DM8TEST -e PAGE_SIZE=16 -e UNICODE_FLAG=1 \
  -e CASE_SENSITIVE=Y -e COMPATIBLE_MODE=2 <image_tag>
```

### GOPROXY 配置（一般性建议）

国内开发者拉取依赖推荐 GOPROXY 镜像加速（与 DM 支持无特定关系，gplus 通用）：

```bash
go env -w GOPROXY=https://goproxy.cn,direct
```

> **注**：spec 早期版本曾假设 godoes/gorm-dameng 通过 transitive 引入 `gitee.com/chunanyong/dm`，故强调 GOPRIVATE fallback。**plan 阶段 Task 0 实测推翻此假设**——godoes/gorm-dameng v0.7.2 driver 实现自带在子包 `dm8/i18n/parser/security/util`，所有依赖在 github.com 与 golang.org 上，标准 GOPROXY 即可。

## 贡献

欢迎提交 Issue 和 Pull Request！

## 版本历史

> 最新版本 **v0.11.1**（2026-06-10），完整变更记录见 [CHANGELOG.md](CHANGELOG.md)。v0.7 起的要点：
>
> - **v0.11**：typed-expr 类型化投影表达式（`Model[T]`/`Col`/`Lit`/`Add` + `SelectExpr`）+ `InsertSelectMap` 成对列映射
> - **v0.10**：`DataRule.Table` 跨表数据权限 + `PageAs` 投影分页
> - **v0.9**：`SelectRaw` 参数绑定 + `InsertSelect`/`InsertSelectTx` + `NewQueryAs` 主别名 FROM 物化
> - **v0.8**：alias 体系（`As`/`NewQueryAs`/`JoinAs`/`SubQueryAs` + EXISTS 子查询）；0.8.1~0.8.3 PG / Oracle / 达梦多方言验证
> - **v0.7**：`FindAs`/`FindOneAs` 投影查询（走 Query callback chain，下游 callback 不失效）

### v0.6.0（2026-04-30）

类型安全子查询，消灭体系性 `WhereRaw` 裂缝。

```go
// 1. WHERE id IN (SELECT user_id FROM orders WHERE status='paid')
paidUserIDs, order := gplus.NewQuery[Order](ctx)
paidUserIDs.Select(&order.UserID).Eq(&order.Status, "paid")

q, user := gplus.NewQuery[User](ctx)
q.InSub(&user.ID, paidUserIDs).Order(&user.CreatedAt, false)
result, _ := repo.List(q)

// 2. WHERE age > (SELECT AVG(age) FROM users)
avgAge, _ := gplus.NewQuery[User](ctx)
avgAge.SelectRaw("AVG(age)")

q2, user2 := gplus.NewQuery[User](ctx)
q2.GtSub(&user2.Age, avgAge)

// 3. UPDATE users SET status='inactive' WHERE id IN (SELECT user_id FROM orders WHERE last_order < cutoff)
inactiveOrders, order2 := gplus.NewQuery[Order](ctx)
inactiveOrders.Select(&order2.UserID).Lt(&order2.LastOrderAt, cutoff)

u, user3 := gplus.NewUpdater[User](ctx)
u.Set(&user3.Status, "inactive").InSub(&user3.ID, inactiveOrders)
repo.UpdateByCond(u)
```

新增 32 方法（Query 16 + Updater 16） + `Subquerier` 接口 + `ErrSubqueryNil` sentinel + `SelectRaw`。

⚠️ **延迟调用语义**：`sub` 传入后仍可被修改，修改会反映到最终 SQL。推荐 sub 构建完成后再传入。

详见 CHANGELOG v0.6.0 章节和 spec 文档。

### v0.5.1（2026-04-30）

**安全修复：**
- **DataRule 接入 7 条 by-ID 路径**：补 v0.2.0 by-Cond 修复后剩余的系统性遗漏（GetById/GetByIds/UpdateById/UpdateByIds/DeleteById/DeleteByIds/Restore，含 Tx 变体共 14 条调用），杜绝跨租户读 / 改 / 删 / 恢复风险
- `ToUpdateSQL(nil)` 错误类型由 `ErrQueryNil` 改为 `fmt.Errorf("%w: %w", ErrUpdateEmpty, ErrQueryNil)` 双 wrap，`errors.Is` 双向兼容旧调用方

**⚠️ 行为变更**：跨租户场景 `affected` 由 >0 变 =0；启用 DataRule 时 `UpdateById` 返回的 `ErrOptimisticLock` 可能由 DataRule 拦截而非版本冲突所致，调用方不应无条件重试。详见 CHANGELOG。

### v0.5.0（2026-04-24）

**新增功能：**
- **乐观锁**：模型字段标注 `gplus:"version"` 即自动启用，`UpdateById` / `UpdateByIdTx` 自动追加 `WHERE version = oldVer` 并 `SET version = version + 1`，affected==0 时返回 `ErrOptimisticLock`，更新成功后 `entity.Version` 自动回写
  - 支持 `int` / `int32` / `int64` / `uint` / `uint32` / `uint64`，含嵌入字段中的 version
  - 无 version 字段的模型保持原有路径，零额外开销
- 新增 `ErrOptimisticLock` 哨兵错误

### v0.4.0（2026-04-23）

**新增功能：**
- **OnConflict** upsert：`InsertOnConflict` / `InsertBatchOnConflict`（含 Tx 变体），支持四种策略：
  - `DoNothing`（冲突跳过）、`DoUpdates`（按 EXCLUDED 覆盖指定列）、`DoUpdateAll`（覆盖除主键外所有列）、`UpdateExprs`（自定义表达式，可与 `DoUpdates` 组合）
- **调试 SQL**：`Query.ToSQL` / `Query.ToCountSQL` / `Updater.ToSQL`（含 Repository 同名便捷方法），基于 GORM DryRun 模式输出参数已内联的 SQL
- 新增 `ErrOnConflictInvalid` 哨兵错误

**Bug 修复：**
- `Distinct` + `Page` 时 COUNT 路径未应用 `DISTINCT` 子查询，导致 `total` 虚高
- `FirstOrUpdate` 重读改用主键精确查找，避免更新查询条件字段时按旧字段找不到新记录

### v0.3.2

**Bug 修复：**
- 修复 `PluckTx` 未能在 GORM clause 定型前提前应用 `Distinct`，导致 `Distinct` 标志丢失

### v0.3.1

**新增功能：**
- `Repository` 新增 `NewQuery`/`NewUpdater` 便捷方法，无需重复指定泛型参数

**Bug 修复：**
- 修复 `ToDB` 未使用 `Session{NewDB: true}`，导致继承"脏" db 上已有的条件

### v0.3.0

**新增功能：**
- 新增 `IncrBy`/`DecrBy`（含 Tx 变体）：原子字段自增自减
- 新增 `WithScope`：向 Query/Updater 注入自定义 GORM scope
- 新增 `Last`/`LastTx`：按主键倒序取第一条记录
- 新增 `Restore`/`RestoreTx`：按主键恢复软删除记录
- 新增 `RestoreByCond`/`RestoreByCondTx`：按条件批量恢复软删除
- 新增 `ListMap`/`ListMapTx`：查询结果转换为 `map[D]T`
- 新增 `Sum`/`Max`/`Min`/`Avg`（含 Tx 变体）：聚合函数，NULL 安全
- 新增 `Chunk`/`ChunkTx`：主键游标分批处理
- 新增 `FirstOrCreate`：原子查找或创建
- 新增 `FirstOrUpdate`：原子查找或创建并更新
- 新增 `UpdateByIds`/`UpdateByIdsTx`：按主键列表批量更新
- 新增 `DeleteByIds`/`DeleteByIdsTx`：按主键列表批量删除
- 新增 `GetByIds`/`GetByIdsTx`：按主键列表批量查询
- 新增 `IsEmpty()`：判断 Query/Updater 是否无条件（WithScope 不计入）
- 新增错误变量 `ErrDefaultsNil`、`ErrRestoreEmpty`
- 新增 `.gitattributes`：统一行尾为 LF

### v0.2.1（2026-03-20）

**Bug 修复：**
- 修复 `applyGroupHaving` 两处实现缺陷：`OrHaving` 条件错误追加到 WHERE 而非 HAVING；`HavingGroup` OR 嵌套分组未正确构建 clause 树
- 修复 `Query[T].Clear()` 未重置 `errs` 和 `dataRuleApplied`，导致复用同一 Query 实例时状态泄漏
- 修复 `DataRule.Column` 缺少白名单校验，含括号/运算符的恶意表达式可绕过 `quoteColumn` 转义

**测试：**
- 覆盖率从 93.3% 提升至 94.0%
- 新增 `TestQuery_SQL` 综合 DryRun SQL 验证测试（20 个子测试）

### v0.2.0

- 泛型 Repository、Query Builder、Updater 初始版本
- 支持 DataRule 数据权限、软删除、悲观锁、预加载等特性

## 已知陷阱

### `q.ToDB(db).Scan` / `.Row` / `.Rows` 绕过 Query callback chain（CRITICAL）

GORM v1.31.1 中 `db.Scan` / `db.Row` / `db.Rows` 内部走 Row callback chain，**不会触发**挂在 Query chain 上的下游 callback（数据隔离 / 审计 / 查询日志）。在依赖这些 callback 的项目中，使用上述三种调用**会导致跨租户数据泄露 / 审计日志缺失**。

**必须改用** `FindAs` / `FindOneAs`：

| 旧写法（漏洞） | 新写法（安全） |
|---|---|
| `q.ToDB(db).Model(&T{}).Scan(&rows)` | `gplus.FindAs(repo, q, &rows)` |
| `q.ToDB(db).Model(&T{}).Limit(1).Scan(&one)` | `gplus.FindOneAs(repo, q, &one)` |

**排查老代码**：见 CHANGELOG v0.7.0 行为约束段（含两条互补 grep 命令）

### `RawQuery` / `RawScan` / `RawScanTx` 的 Schema=nil 问题

Raw SQL 路径 Schema=nil，下游 callback 在正确实现下应短路（`if Schema == nil { return }`）。**若下游 callback 未判断 `Schema == nil`，行为不可预测**。涉及敏感数据必须在 SQL 中手写 WHERE。

### DataRule + JOIN 列二义性

`DataRule.Column` 在多表 JOIN 下若无表前缀（如 `"dept_id"`），可能产生 SQL 二义性错误（MySQL 报 `ambiguous`）或静默走错表。**必须用 `table.col` 形式**（如 `"users.dept_id"`）。

### `col` 字符串形式不验证

`Sum/Max/Min/Avg/Pluck` 接受字符串列名时，gplus 不做白名单校验（与 `DataRule.Column` 不同）。**禁止将用户输入直接传入 `col`**。

### MySQL 限制：UPDATE 目标表不能与子查询 FROM 同表（错误 1093）

MySQL 不允许 `UPDATE T ... WHERE col > (SELECT ... FROM T)` —— 报 `Error 1093 (HY000): You can't specify target table 'T' for update in FROM clause`。SQLite / PostgreSQL 无此限制。

**适用场景**：`InSub / NotInSub / GtSub / LtSub / EqSub` 等 16 个 Updater 子查询方法在 MySQL 下，若子查询源表与 UPDATE 目标表相同会触发该错误。

**workaround**：用 derived table 包一层让 MySQL 视为不同表

```go
// ❌ MySQL 1093
avgQ, _ := gplus.NewQuery[User](ctx)
avgQ.SelectRaw("AVG(age)")
u, m := gplus.NewUpdater[User](ctx)
u.Set(&m.Name, "Senior").GtSub(&m.Age, avgQ)

// ✅ 用原生 SQL 包 derived table
u.Set(&m.Name, "Senior").
    WhereRaw("age > (SELECT avg_age FROM (SELECT AVG(age) AS avg_age FROM users) AS t)")
```

## Alias 与跨表查询（v0.8.0）

类型安全的跨表列引用、自连接、相关 EXISTS 子查询。

### 跨表列引用

```go
q, u := gplus.NewQuery[User](ctx)
o := gplus.As[Order](q, "o")
q.LeftJoinAs(o, &o.UserID, &u.ID, "").
    Eq(&o.Amount, 100)
// SQL: SELECT users.* FROM users
//      LEFT JOIN orders AS o ON o.user_id = users.id
//      WHERE o.amount = 100
```

### 同表自连接

```go
q, u := gplus.NewQueryAs[User](ctx, "u")
boss := gplus.As[User](q, "boss")
q.LeftJoinAs(boss, &u.BossID, &boss.ID, "")
```

> ⚠️ **`NewQueryAs` 主别名已知限制**（详见 CHANGELOG v0.9.0）：
> - **First 路径不可用**：`GetOne` / `Last` / `GetByLock` / `FirstOrCreate` / `FirstOrUpdate` 下 GORM 自动追加 `ORDER BY <裸表名>.id`，裸表名被别名遮蔽报错（如 `no such column: users.id`）。改用 `List` / `Page` / `Count` / `FindAs` / `ToDB` 等 SELECT 路径
> - **写路径不可用**：主别名 Query 传入 Delete / Update 真实执行会失败（BuildUpdate/BuildDelete 结构性不物化别名）

### Correlated EXISTS

```go
q, u := gplus.NewQuery[User](ctx)
sub, o := gplus.SubQuery[Order](q)
sub.Eq(&o.UserID, u.ID)
q.Exists(sub)
```

### ⚠️ DataRule × alias 安全契约

**DataRule 不会自动应用到 alias 副表。** 副表敏感字段（tenant_id 等）必须在 JoinAs 的 extraSQL 显式过滤：

```go
q.LeftJoinAs(o, &o.UserID, &u.ID,
    "AND o.tenant_id = ?", tenantID) // ← 显式 + 参数化
```

**禁止**用 fmt.Sprintf 拼接用户输入到 extraSQL（SQL 注入）。跨表数据权限请使用 `DataRule.Table` 字段（见上方「跨表数据权限」章节），在 `DataRule.Column` 直接写 alias 前缀（如 `"o.tenant_id"`）的旧写法仍向后兼容，但新代码建议用 `Table`。

## 许可证

MIT License