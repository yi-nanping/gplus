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
go get github.com/yi-nanping/gplus@v0.3.2
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

### 数据权限（DataRule）

`DataRule` 通过 `context.Context` 传入，由 Repository 方法自动应用到所有查询和写操作，无需在每处手动添加条件。适合多租户、行级权限等场景。

```go
// 定义数据权限规则（通常在中间件中设置）
rules := []gplus.DataRule{
    {Column: "tenant_id", Op: gplus.OpEq, Val: "tenant-abc"},
    {Column: "deleted_at", Op: gplus.OpIsNull},
}
ctx = context.WithValue(ctx, gplus.DataRuleKey, rules)

// 之后所有使用该 ctx 的查询都会自动附加上述条件
q, m := gplus.NewQuery[User](ctx)
q.Eq(&m.IsVip, true)
// 实际执行：WHERE is_vip = true AND tenant_id = 'tenant-abc' AND deleted_at IS NULL
users, err := repo.List(q)
```

> **注意**：`DataRule.Column` 仅支持字母/数字/下划线/点，含括号或运算符的表达式会被拒绝以防注入。

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

## 贡献

欢迎提交 Issue 和 Pull Request！

## 版本历史

### v0.3.2（最新）

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

## 许可证

MIT License