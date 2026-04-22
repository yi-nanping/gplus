// Package gplus 是基于 GORM 的泛型增强库，提供类型安全的查询构建器、
// Repository 模式和数据权限规则注入等功能。
//
// # 快速开始
//
// 定义模型并创建 Repository：
//
//	type User struct {
//	    ID    uint   `gorm:"primaryKey"`
//	    Name  string `gorm:"column:name"`
//	    Email string `gorm:"column:email"`
//	    Age   int    `gorm:"column:age"`
//	}
//
//	repo := gplus.NewRepository[uint, User](db)
//
// # 查询构建
//
// NewQuery 返回一个类型安全的查询构建器和模型单例指针。
// 字段指针必须来自该返回的 *T 实例：
//
//	q, m := gplus.NewQuery[User](ctx)
//	q.Eq(&m.Name, "Alice").Gt(&m.Age, 18).Order(&m.ID, false)
//	users, err := repo.List(q)
//
// # 更新构建
//
//	u, m := gplus.NewUpdater[User](ctx)
//	u.Eq(&m.ID, 1).Set(&m.Name, "Bob")
//	affected, err := repo.UpdateByCond(u)
//
// # 分页
//
//	q, _ := gplus.NewQuery[User](ctx)
//	list, total, err := repo.Page(q.Limit(10).Offset(0), false)
//
// # 聚合
//
//	q, m := gplus.NewQuery[User](ctx)
//	q.Gt(&m.Age, 0)
//	avg, err := gplus.Avg[User, float64, uint](repo, q, &m.Age)
//
// # 事务
//
//	err := db.Transaction(func(tx *gorm.DB) error {
//	    _, err := repo.SaveTx(ctx, &user, tx)
//	    return err
//	})
//
// # 数据权限
//
// 通过 Context 注入 DataRule，对所有查询和写操作透明生效：
//
//	rules := []gplus.DataRule{{Column: "tenant_id", Value: 42}}
//	ctx = context.WithValue(ctx, gplus.DataRuleKey, rules)
//
// # 调试
//
// 使用 ToSQL / ToCountSQL / ToUpdateSQL 获取最终执行的 SQL，无需实际执行：
//
//	q, m := gplus.NewQuery[User](ctx)
//	q.Eq(&m.Name, "Alice")
//	sql, args, err := gplus.ToSQL[uint, User](repo, q)
package gplus
