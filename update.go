package gplus

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

type Updater[T any] struct {
	// ScopeBuilder 是 Updater 的核心，用于构建 SQL 语句
	ScopeBuilder
	// ctx 是上下文信息，用于跟踪请求和处理请求的生命周期
	ctx context.Context
	// setMap 是更新字段的映射表，用于存储待更新的字段和值
	setMap map[string]any
	// errs 是错误列表，用于存储执行过程中出现的错误
	errs []error
}

func NewUpdater[T any](ctx context.Context) (*Updater[T], *T) {
	model := getModelInstance[T]()
	return &Updater[T]{
		ctx: ctx,
		ScopeBuilder: ScopeBuilder{
			conditions: make([]condition, 0, 8),
		},
		setMap: make(map[string]any),
		errs:   make([]error, 0, 8),
	}, model
}

// Context 返回上下文信息
func (u *Updater[T]) Context() context.Context { return u.ctx }

// GetError 将所有累积的错误合并为一个返回
func (u *Updater[T]) GetError() error {
	if len(u.errs) == 0 {
		return nil
	}
	n := len(u.errs)
	word := "errors"
	if n == 1 {
		word = "error"
	}
	summary := fmt.Errorf("gplus updater failed with %d %s", n, word)
	return errors.Join(append([]error{summary}, u.errs...)...)
}

// Table 动态切换更新表名
func (u *Updater[T]) Table(name string) *Updater[T] {
	u.tableName = name
	return u
}

// IsEmpty 是否为空更新（没有设置任何更新字段）
func (u *Updater[T]) IsEmpty() bool {
	return len(u.setMap) == 0
}

// Set 设置更新值
// 示例: u.Set(&User.Name, "NewName")
func (u *Updater[T]) Set(col any, val any) *Updater[T] {
	u.setMap[mustColumn(col)] = val
	return u
}

// SetExpr 设置 SQL 表达式更新 (原子更新)
// 示例: u.SetExpr(&User.Age, "age + ?", 1) -> UPDATE ... SET age = age + 1
func (u *Updater[T]) SetExpr(col any, expr string, args ...any) *Updater[T] {
	u.setMap[mustColumn(col)] = gorm.Expr(expr, args...)
	return u
}

// SetMap 批量设置更新内容
func (u *Updater[T]) SetMap(m map[string]any) *Updater[T] {
	if len(m) == 0 {
		panic("gplus: SetMap called with empty map")
	}
	for k, v := range m {
		u.setMap[k] = v
	}
	return u
}

// --- 更新范围限制 (GORM Select/Omit) ---

// Select 指定只更新哪些字段 (即使 setMap 里有其他字段也不会更新)
func (u *Updater[T]) Select(cols ...any) *Updater[T] {
	for _, c := range cols {
		u.selects = append(u.selects, mustColumn(c))
	}
	return u
}

// Omit 指定排除哪些字段不更新
func (u *Updater[T]) Omit(cols ...any) *Updater[T] {
	for _, c := range cols {
		u.omits = append(u.omits, mustColumn(c))
	}
	return u
}

// --- 过滤条件补全 (Where 条件) ---

// 私有辅助方法：统一添加条件
func (u *Updater[T]) addCond(isOr bool, col any, op string, val any) *Updater[T] {
	u.conditions = append(u.conditions, condition{
		expr:     mustColumn(col),
		operator: op,
		value:    val,
		isOr:     isOr,
	})
	return u
}

// 基础比较

// Eq 等于
func (u *Updater[T]) Eq(col any, val any) *Updater[T] { return u.addCond(false, col, OpEq, val) }

// Ne 不等于
func (u *Updater[T]) Ne(col any, val any) *Updater[T] { return u.addCond(false, col, OpNe, val) }

// Gt 大于
func (u *Updater[T]) Gt(col any, val any) *Updater[T] { return u.addCond(false, col, OpGt, val) }

// Ge 大于等于
func (u *Updater[T]) Ge(col any, val any) *Updater[T] { return u.addCond(false, col, OpGe, val) }

// Lt 小于
func (u *Updater[T]) Lt(col any, val any) *Updater[T] { return u.addCond(false, col, OpLt, val) }

// Le 小于等于
func (u *Updater[T]) Le(col any, val any) *Updater[T] { return u.addCond(false, col, OpLe, val) }

// ---------------------模糊查询 ----------------------

// Like 模糊查询
func (u *Updater[T]) Like(col any, val string) *Updater[T] {
	return u.addCond(false, col, OpLike, "%"+val+"%")
}

// LeftLike 左模糊查询
func (u *Updater[T]) LeftLike(col any, val string) *Updater[T] {
	return u.addCond(false, col, OpLike, "%"+val)
}

// RightLike 右模糊查询
func (u *Updater[T]) RightLike(col any, val string) *Updater[T] {
	return u.addCond(false, col, OpLike, val+"%")
}

// NotLike 不模糊查询
func (u *Updater[T]) NotLike(col any, val string) *Updater[T] {
	return u.addCond(false, col, OpNotLike, "%"+val+"%")
}

//	-----------------集合与空值------------------

// In 包含
func (u *Updater[T]) In(col any, val any) *Updater[T] { return u.addCond(false, col, OpIn, val) }

// OrIn 包含
func (u *Updater[T]) OrIn(col any, val any) *Updater[T] { return u.addCond(true, col, OpIn, val) }

// NotIn 不包含
func (u *Updater[T]) NotIn(col any, val any) *Updater[T] { return u.addCond(false, col, OpNotIn, val) }

// OrNotIn 不包含
func (u *Updater[T]) OrNotIn(col any, val any) *Updater[T] { return u.addCond(true, col, OpNotIn, val) }

// IsNull 为空
func (u *Updater[T]) IsNull(col any) *Updater[T] { return u.addCond(false, col, OpIsNull, nil) }

// OrIsNull 为空
func (u *Updater[T]) OrIsNull(col any) *Updater[T] { return u.addCond(true, col, OpIsNull, nil) }

// IsNotNull 不为空
func (u *Updater[T]) IsNotNull(col any) *Updater[T] { return u.addCond(false, col, OpIsNotNull, nil) }

// OrIsNotNull 不为空
func (u *Updater[T]) OrIsNotNull(col any) *Updater[T] { return u.addCond(true, col, OpIsNotNull, nil) }

// -----------区间查询 (注意：传入 slice 以适配 Builder 逻辑)------------------

// Between 区间查询
func (u *Updater[T]) Between(col any, v1, v2 any) *Updater[T] {
	return u.addCond(false, col, OpBetween, []any{v1, v2})
}

// OrBetween 区间查询
func (u *Updater[T]) OrBetween(col any, v1, v2 any) *Updater[T] {
	return u.addCond(true, col, OpBetween, []any{v1, v2})
}

// NotBetween 区间查询
func (u *Updater[T]) NotBetween(col any, v1, v2 any) *Updater[T] {
	return u.addCond(false, col, OpNotBetween, []any{v1, v2})
}

// OrNotBetween 区间查询
func (u *Updater[T]) OrNotBetween(col any, v1, v2 any) *Updater[T] {
	return u.addCond(true, col, OpNotBetween, []any{v1, v2})
}

// --- OR 条件支持 ---

// OrEq 等于
func (u *Updater[T]) OrEq(col any, val any) *Updater[T] { return u.addCond(true, col, OpEq, val) }

// OrNe 不等于
func (u *Updater[T]) OrNe(col any, val any) *Updater[T] { return u.addCond(true, col, OpNe, val) }

// OrGt 大于
func (u *Updater[T]) OrGt(col any, val any) *Updater[T] { return u.addCond(true, col, OpGt, val) }

// OrGe 大于等于
func (u *Updater[T]) OrGe(col any, val any) *Updater[T] { return u.addCond(true, col, OpGe, val) }

// OrLt 小于
func (u *Updater[T]) OrLt(col any, val any) *Updater[T] { return u.addCond(true, col, OpLt, val) }

// OrLe 小于等于
func (u *Updater[T]) OrLe(col any, val any) *Updater[T] { return u.addCond(true, col, OpLe, val) }

// OrLike 模糊查询
func (u *Updater[T]) OrLike(col any, val string) *Updater[T] {
	return u.addCond(true, col, OpLike, "%"+val+"%")
}

// OrLeftLike 左模糊查询
func (u *Updater[T]) OrLeftLike(col any, val string) *Updater[T] {
	return u.addCond(true, col, OpLike, "%"+val)
}

// OrRightLike 右模糊查询
func (u *Updater[T]) OrRightLike(col any, val string) *Updater[T] {
	return u.addCond(true, col, OpLike, val+"%")
}

// OrNotLike 不模糊查询
func (u *Updater[T]) OrNotLike(col any, val string) *Updater[T] {
	return u.addCond(true, col, OpNotLike, "%"+val+"%")
}

// --- 特殊操作 ---

// And 开启一个带括号的 AND 嵌套块
//
//	示例：u.Eq(&User.Status, 1).And(func(sub *gplus.Updater[User]) {
//	   sub.Gt(&User.Age, 18).OrEq(&User.IsVip, true)
//	})
func (u *Updater[T]) And(fn func(sub *Updater[T])) *Updater[T] {
	if fn == nil {
		panic("gplus: And called with nil fn")
	}
	// 创建一个临时的子 Updater，共享泛型类型 T
	sub := &Updater[T]{
		ScopeBuilder: ScopeBuilder{conditions: make([]condition, 0)},
	}
	fn(sub)
	if len(sub.errs) > 0 {
		u.errs = append(u.errs, sub.errs...)
	}
	if len(sub.conditions) > 0 {
		u.conditions = append(u.conditions, condition{
			group: sub.conditions,
			isOr:  false,
		})
	}
	return u
}

// Or 开启一个带括号的 OR 嵌套块
func (u *Updater[T]) Or(fn func(sub *Updater[T])) *Updater[T] {
	if fn == nil {
		panic("gplus: Or called with nil fn")
	}
	sub := &Updater[T]{
		ScopeBuilder: ScopeBuilder{conditions: make([]condition, 0)},
	}
	fn(sub)
	if len(sub.errs) > 0 {
		u.errs = append(u.errs, sub.errs...)
	}
	if len(sub.conditions) > 0 {
		u.conditions = append(u.conditions, condition{
			group: sub.conditions,
			isOr:  true,
		})
	}
	return u
}

// UpdateMap 获取最终的更新 Map
func (u *Updater[T]) UpdateMap() map[string]any {
	return u.setMap
}

// Unscoped 物理更新，允许更新已被软删除的数据
func (u *Updater[T]) Unscoped() *Updater[T] {
	u.unscoped = true
	return u
}

// Clear 重写 Updater 的清除逻辑
func (u *Updater[T]) Clear() {
	// 1. 调用基类的 Clear 清除 Where/Join 等条件
	u.ScopeBuilder.Clear()

	// 2. 清除 Updater 特有的更新值 Map
	// 方式 A: 直接创建新 map (简单，GC 压力稍大)
	// u.setMap = make(map[string]any)

	// 方式 B: 遍历删除 (如果 map 很大，性能较差，但保留了 map 内存)
	// for k := range u.setMap {
	//    delete(u.setMap, k)
	// }

	// 方式 C (推荐): Go 1.21+ 提供了 clear() 内置函数
	// 如果你的 Go 版本 >= 1.21
	clear(u.setMap)

	// 兼容老版本的通用做法 (由于 map 无法像 slice 那样 [:0]，通常重新 make 是最安全的)
	// u.setMap = make(map[string]any)
}
