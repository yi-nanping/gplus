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

## 快速开始

### 安装

```bash
go get github.com/yi-nanping/gplus
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
    pageQuery.Gt(&pageModel.Age, 18).Order("id DESC")
    
    results, total, err := repo.Page(pageQuery, false)
    if err != nil {
        fmt.Printf("分页查询失败: %v\n", err)
    } else {
        fmt.Printf("分页结果: 总计 %d 条, 当前页 %d 条\n", total, len(results))
    }

    // 4. 更新用户
    updater, updaterModel := gplus.NewUpdater[User](ctx)
    updater.Set(&updaterModel.Name, "李四").Eq(&updaterModel.ID, user.ID)
    
    affected, err := repo.Update(updater, nil)
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
repo := gplus.NewRepository[int, User](db)

// 基础 CRUD 操作
repo.Save(ctx, &user)                     // 插入
repo.UpdateById(ctx, &user)               // 根据实体ID更新
repo.DeleteById(ctx, 1)                   // 根据ID删除
repo.GetById(ctx, 1)                      // 根据ID查询
repo.List(query)                          // 查询列表
repo.Page(query, false)                   // 分页查询
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
      .Order("id DESC")
      .Limit(10)
      .Offset(0)

// 复杂条件
query.And(func(sub *gplus.Query[User]) {
    sub.Eq(&model.Age, 20).Or().Eq(&model.IsVip, true)
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
affected, err := repo.Update(updater, nil) // 第二个参数是事务，传nil表示不用事务
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
| `LikeLeft()` | `LIKE` | - | 左模糊查询 |
| `LikeRight()` | `LIKE` | - | 右模糊查询 |
| `OpNotIn()` | `NOT IN` | - | 不包含（Query专用） |
| `OpNotLike()` | `NOT LIKE` | - | 非模糊匹配（Query专用） |
| `EqOr()` | `=` | - | OR等于（Updater专用） |
| `OrEq()` | `=` | - | OR等于（Updater专用） |
| `OrGt()` | `>` | - | OR大于（Updater专用） |
| `OrGe()` | `>=` | - | OR大于等于（Updater专用） |
| `OrLt()` | `<` | - | OR小于（Updater专用） |
| `OrLe()` | `<=` | - | OR小于等于（Updater专用） |
| `OrLike()` | `LIKE` | - | OR模糊匹配（Updater专用） |

## 连接操作

支持多种连接类型，所有连接类型常量在 `consts.go` 中定义：

| 方法名 | 常量 | 说明 |
|--------|------|------|
| `LeftJoin()` | `JoinLeft` | 左连接：返回左表所有记录，即使右表无匹配 |
| `RightJoin()` | `JoinRight` | 右连接：返回右表所有记录，即使左表无匹配 |
| `InnerJoin()` | `JoinInner` | 内连接：仅返回两个表中匹配的记录（交集） |
| `OuterJoin()` | `JoinOuter` | 外连接 |
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

## 许可证

MIT License