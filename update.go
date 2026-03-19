package gplus

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	// dataRuleApplied 防止 DataRuleBuilder 对同一 Updater 重复追加数据权限条件
	dataRuleApplied bool
}

// NewUpdater 创建泛型更新构建器，同时返回类型 T 的规范实例指针。
// 所有字段指针参数（如 &model.Name）必须来自返回的 *T 实例。
// ctx 用于传递请求级上下文，可传 context.Background()。
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

// Context 返回上下文信息，若未设置则返回 context.Background()
func (u *Updater[T]) Context() context.Context {
	if u.ctx == nil {
		return context.Background()
	}
	return u.ctx
}

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
	name, err := resolveColumnName(col)
	if err != nil {
		u.errs = append(u.errs, fmt.Errorf("gplus: Set 无效列指针: %w", err))
		return u
	}
	u.setMap[name] = val
	return u
}

// SetExpr 设置 SQL 表达式更新 (原子更新)
// 示例: u.SetExpr(&User.Age, "age + ?", 1) -> UPDATE ... SET age = age + 1
func (u *Updater[T]) SetExpr(col any, expr string, args ...any) *Updater[T] {
	name, err := resolveColumnName(col)
	if err != nil {
		u.errs = append(u.errs, fmt.Errorf("gplus: SetExpr 无效列指针: %w", err))
		return u
	}
	u.setMap[name] = gorm.Expr(expr, args...)
	return u
}

// SetMap 批量设置更新内容
// 注意：map 的 key 必须是数据库列名（snake_case，如 "user_name"），
// 而非结构体字段名（如 "UserName"）。如需类型安全的列名解析，请改用 Set()。
func (u *Updater[T]) SetMap(m map[string]any) *Updater[T] {
	if len(m) == 0 {
		u.errs = append(u.errs, fmt.Errorf("gplus: SetMap 不能传入空 map"))
		return u
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
		name, err := resolveColumnName(c)
		if err != nil {
			u.errs = append(u.errs, fmt.Errorf("gplus: Select 无效列指针: %w", err))
			continue
		}
		u.selects = append(u.selects, name)
	}
	return u
}

// Omit 指定排除哪些字段不更新
func (u *Updater[T]) Omit(cols ...any) *Updater[T] {
	for _, c := range cols {
		name, err := resolveColumnName(c)
		if err != nil {
			u.errs = append(u.errs, fmt.Errorf("gplus: Omit 无效列指针: %w", err))
			continue
		}
		u.omits = append(u.omits, name)
	}
	return u
}

// --- 过滤条件补全 (Where 条件) ---

// 私有辅助方法：统一添加条件
func (u *Updater[T]) addCond(isOr bool, col any, op string, val any) *Updater[T] {
	name, err := resolveColumnName(col)
	if err != nil {
		u.errs = append(u.errs, fmt.Errorf("gplus: 无效列指针: %w", err))
		return u
	}
	u.conditions = append(u.conditions, condition{
		expr:     name,
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

// LikeLeft 左模糊查询
func (u *Updater[T]) LikeLeft(col any, val string) *Updater[T] {
	return u.addCond(false, col, OpLike, "%"+val)
}

// LikeRight 右模糊查询
func (u *Updater[T]) LikeRight(col any, val string) *Updater[T] {
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
	if v1 == nil || v2 == nil {
		u.errs = append(u.errs, fmt.Errorf("gplus: Between 参数 v1/v2 不能为 nil"))
		return u
	}
	return u.addCond(false, col, OpBetween, []any{v1, v2})
}

// OrBetween 区间查询
func (u *Updater[T]) OrBetween(col any, v1, v2 any) *Updater[T] {
	if v1 == nil || v2 == nil {
		u.errs = append(u.errs, fmt.Errorf("gplus: OrBetween 参数 v1/v2 不能为 nil"))
		return u
	}
	return u.addCond(true, col, OpBetween, []any{v1, v2})
}

// NotBetween 区间查询
func (u *Updater[T]) NotBetween(col any, v1, v2 any) *Updater[T] {
	if v1 == nil || v2 == nil {
		u.errs = append(u.errs, fmt.Errorf("gplus: NotBetween 参数 v1/v2 不能为 nil"))
		return u
	}
	return u.addCond(false, col, OpNotBetween, []any{v1, v2})
}

// OrNotBetween 区间查询
func (u *Updater[T]) OrNotBetween(col any, v1, v2 any) *Updater[T] {
	if v1 == nil || v2 == nil {
		u.errs = append(u.errs, fmt.Errorf("gplus: OrNotBetween 参数 v1/v2 不能为 nil"))
		return u
	}
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

// OrLikeLeft 左模糊查询(或)
func (u *Updater[T]) OrLikeLeft(col any, val string) *Updater[T] {
	return u.addCond(true, col, OpLike, "%"+val)
}

// OrLikeRight 右模糊查询(或)
func (u *Updater[T]) OrLikeRight(col any, val string) *Updater[T] {
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
		u.errs = append(u.errs, errors.New("gplus: And called with nil fn"))
		return u
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
		u.errs = append(u.errs, errors.New("gplus: Or called with nil fn"))
		return u
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

// UpdateMap 获取最终的更新 Map 的只读副本
// 返回副本而非原始引用，防止调用方绕过 Set/SetMap 的列名校验直接修改内部状态
func (u *Updater[T]) UpdateMap() map[string]any {
	cp := make(map[string]any, len(u.setMap))
	for k, v := range u.setMap {
		cp[k] = v
	}
	return cp
}

// Unscoped 物理更新，允许更新已被软删除的数据
func (u *Updater[T]) Unscoped() *Updater[T] {
	u.unscoped = true
	return u
}

// DataRuleBuilder 从上下文中提取规则并应用到更新条件中。
// 对同一个 Updater 对象只执行一次，防止多次调用重复追加条件。
func (u *Updater[T]) DataRuleBuilder() *Updater[T] {
	if u.dataRuleApplied {
		return u
	}
	u.dataRuleApplied = true
	if u.ctx == nil {
		return u
	}
	rules, ok := u.ctx.Value(DataRuleKey).([]DataRule)
	if !ok || len(rules) == 0 {
		return u
	}
	for _, rule := range rules {
		u.applyDataRule(rule)
	}
	return u
}

// applyDataRule 将单条 DataRule 转换为更新条件追加到 Updater 中
func (u *Updater[T]) applyDataRule(rule DataRule) {
	column := rule.Column
	c := strings.ToUpper(strings.TrimSpace(rule.Condition))
	value := rule.Value

	if value == "" && len(rule.Values) == 0 && c != "IS NULL" && c != "IS NOT NULL" {
		return
	}

	// 禁止原生 SQL 注入
	if c == "SQL" || c == "USE_SQL_RULES" {
		u.errs = append(u.errs, fmt.Errorf(
			"data rule [col: %s]: condition type %q is not allowed, use RawExec with parameterized args instead",
			column, rule.Condition,
		))
		return
	}

	switch c {
	case "=", "EQ":
		u.Eq(column, value)
	case "<>", "!=", "NE":
		u.Ne(column, value)
	case ">", "GT":
		u.Gt(column, value)
	case ">=", "GE":
		u.Ge(column, value)
	case "<", "LT":
		u.Lt(column, value)
	case "<=", "LE":
		u.Le(column, value)
	case "IN":
		vals := rule.Values
		if len(vals) == 0 {
			vals = splitTrimmed(value)
		}
		u.In(column, vals)
	case "NOT IN":
		vals := rule.Values
		if len(vals) == 0 {
			vals = splitTrimmed(value)
		}
		u.NotIn(column, vals)
	case "LIKE":
		u.Like(column, value)
	case "LEFT_LIKE":
		u.LikeLeft(column, value)
	case "RIGHT_LIKE":
		u.LikeRight(column, value)
	case "IS NULL":
		u.IsNull(column)
	case "IS NOT NULL":
		u.IsNotNull(column)
	case "BETWEEN":
		var parts []string
		if len(rule.Values) == 2 {
			parts = rule.Values
		} else {
			parts = splitTrimmed(value)
		}
		if len(parts) != 2 {
			u.errs = append(u.errs, fmt.Errorf(
				"data rule [col: %s]: BETWEEN 需要两个值，实际得到 %d 个",
				column, len(parts),
			))
			return
		}
		u.Between(column, parts[0], parts[1])
	default:
		u.errs = append(u.errs, fmt.Errorf(
			"data rule [col: %s]: unsupported condition %q; allowed: =, <>, >, >=, <, <=, IN, NOT IN, LIKE, LEFT_LIKE, RIGHT_LIKE, IS NULL, IS NOT NULL, BETWEEN",
			column, rule.Condition,
		))
	}
}

// Clear 重写 Updater 的清除逻辑
func (u *Updater[T]) Clear() {
	u.ScopeBuilder.Clear()
	clear(u.setMap)
	u.errs = u.errs[:0:0]
	u.dataRuleApplied = false
}
