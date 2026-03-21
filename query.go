package gplus

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type Query[T any] struct {
	ScopeBuilder
	ctx context.Context
	// errs 是错误列表，用于存储执行过程中出现的错误
	errs []error
	// dataRuleApplied 防止 DataRuleBuilder 对同一 Query 重复追加数据权限条件
	dataRuleApplied bool
}

// NewQuery 创建泛型查询构建器，同时返回类型 T 的规范实例指针。
// 所有字段指针参数（如 &model.Name）必须来自返回的 *T 实例。
// ctx 用于传递请求级上下文（DataRule、超时等），可传 context.Background()。
func NewQuery[T any](ctx context.Context) (*Query[T], *T) {
	// 确保模型已注册
	model := getModelInstance[T]()
	return &Query[T]{
		ctx:  ctx,
		errs: make([]error, 0, 8),
		ScopeBuilder: ScopeBuilder{
			conditions: make([]condition, 0, 8),
		},
	}, model
}

// Context 获取上下文
func (q *Query[T]) Context() context.Context {
	if q.ctx == nil {
		return context.Background()
	}
	return q.ctx
}

// IsEmpty 判断是否为空查询（无任何类型安全条件）。
// 注意：仅检查通过 Eq/In/Between 等类型安全 API 添加的条件；
// 通过 WithScope 注入的自定义 scope 函数不计入此判断。
func (q *Query[T]) IsEmpty() bool {
	return len(q.conditions) == 0
}

// IsUnscoped 是否为不带软删除的查询
func (q *Query[T]) IsUnscoped() bool {
	return q.unscoped
}

// GetError 将所有累积的错误合并为一个返回
func (q *Query[T]) GetError() error {
	if len(q.errs) == 0 {
		return nil
	}
	n := len(q.errs)
	word := "errors"
	if n == 1 {
		word = "error"
	}
	summary := errors.New(fmt.Sprintf("gplus query builder failed with %d %s", n, word))
	return errors.Join(append([]error{summary}, q.errs...)...)
}

// Clear 重写 Query 的清除逻辑
func (q *Query[T]) Clear() {
	q.ScopeBuilder.Clear()
	q.errs = q.errs[:0:0]
	q.dataRuleApplied = false
}

// WithScope 注入自定义 GORM scope 函数，作为封装层的逃生口。
// 适用于封装层无法覆盖的边缘查询场景，多次调用按顺序叠加执行。
//
// 注意事项：
//   - fn 不可为 nil
//   - 不要在 fn 内调用 Limit/Offset/Unscoped，会覆盖外层设置
//   - fn 应保持无状态、可重入，避免引入隐式副作用
//   - 优先使用类型安全的 API（Eq/In/WhereRaw 等），WithScope 作为最后手段
func (q *Query[T]) WithScope(fn func(*gorm.DB) *gorm.DB) *Query[T] {
	if fn == nil {
		q.errs = append(q.errs, errors.New("gplus: WithScope fn cannot be nil"))
		return q
	}
	q.scopes = append(q.scopes, fn)
	return q
}

// Page 针对page和pageSize的处理
func (q *Query[T]) Page(page, pageSize int) *Query[T] {
	// 默认page为第一页 pageSize为10
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	// limit 和 offset 是分页查询的关键参数
	limit := pageSize
	offset := pageSize * (page - 1)
	q.limit = limit
	q.offset = offset
	return q
}

// Table 动态指定表名
// 场景：分表查询或临时表操作
func (q *Query[T]) Table(name string) *Query[T] {
	q.tableName = name
	return q
}

// addCond 内部辅助方法
func (q *Query[T]) addCond(isOr bool, col any, op string, val any) *Query[T] {
	name, err := resolveColumnName(col)
	if err != nil {
		q.errs = append(q.errs, fmt.Errorf("gplus: invalid column pointer: %w", err))
		return q
	}
	q.conditions = append(q.conditions, condition{
		expr:     name,
		operator: op,
		value:    val,
		isOr:     isOr,
	})
	return q
}

// Select 指定查询字段
func (q *Query[T]) Select(cols ...any) *Query[T] {
	for _, c := range cols {
		name, err := resolveColumnName(c)
		if err != nil {
			q.errs = append(q.errs, fmt.Errorf("gplus: Select invalid column pointer: %w", err))
			continue
		}
		q.selects = append(q.selects, name)
	}
	return q
}

// WhereRaw 添加原生 SQL 条件（AND）。
// sql 为完整条件片段，args 为参数绑定值，防止 SQL 注入。
// 示例：q.WhereRaw("YEAR(created_at) = ?", 2024)
// 注意：sql 参数由调用方负责安全性，不可直接拼接用户输入。
func (q *Query[T]) WhereRaw(sql string, args ...any) *Query[T] {
	if sql == "" {
		q.errs = append(q.errs, errors.New("gplus: WhereRaw sql cannot be empty"))
		return q
	}
	var val any
	if len(args) == 1 {
		val = args[0]
	} else if len(args) > 1 {
		val = args
	}
	q.conditions = append(q.conditions, condition{
		expr:  sql,
		isRaw: true,
		isOr:  false,
		value: val,
	})
	return q
}

// OrWhereRaw 添加原生 SQL 条件（OR）。
// 参数安全要求与 WhereRaw 相同。
func (q *Query[T]) OrWhereRaw(sql string, args ...any) *Query[T] {
	if sql == "" {
		q.errs = append(q.errs, errors.New("gplus: OrWhereRaw sql cannot be empty"))
		return q
	}
	var val any
	if len(args) == 1 {
		val = args[0]
	} else if len(args) > 1 {
		val = args
	}
	q.conditions = append(q.conditions, condition{
		expr:  sql,
		isRaw: true,
		isOr:  true,
		value: val,
	})
	return q
}

// ToDB 将当前 Query 的条件转换为 GORM 的 DB 对象
// 注意：这不会执行查询，只会生成带有条件的 DB 实例，常用于子查询
// 1. 构建子查询 (查部门 ID)
// subQuery, _ := gplus.NewQuery[Dept](ctx)
// subQuery.Eq(&Dept.Name, "IT").Select(&Dept.Id)
// 2. 获取 DB 实例 (通常 Repository 会暴露 GetDB，或者直接从外部传入)
// 这里的 repo 是 UserRepo
// db := userRepo.GetDB()
// 3. 构建主查询 (查用户)
// mainQuery, _ := gplus.NewQuery[User](ctx)
// 【关键】：使用 ToDB 将 subQuery 转换为 GORM 对象，放入 In 条件中
// mainQuery.In(&User.DeptId, subQuery.ToDB(db))
// 4. 执行查询
// users, err := userRepo.List(mainQuery)
func (q *Query[T]) ToDB(db *gorm.DB) *gorm.DB {
	// 1. 使用 Session(&gorm.Session{}) 创建一个干净的 DB 会话，避免污染传入的 db
	// 2. 调用 BuildQuery() 获取闭包，并立即执行该闭包应用条件
	session := db.Session(&gorm.Session{})
	if err := q.GetError(); err != nil {
		// 将 builder 错误注入 DB 链，确保后续 GORM 操作返回该错误而非执行错误 SQL
		_ = session.AddError(err)
		return session
	}
	return q.BuildQuery()(session)
}

// Eq 等于
func (q *Query[T]) Eq(col any, val any) *Query[T] {
	return q.addCond(false, col, OpEq, val)
}

// Ne 不等于
func (q *Query[T]) Ne(col any, val any) *Query[T] {
	return q.addCond(false, col, OpNe, val)
}

// OrEq 等于(或)
func (q *Query[T]) OrEq(col any, val any) *Query[T] {
	return q.addCond(true, col, OpEq, val)
}

// OrNe 不等于(或)
func (q *Query[T]) OrNe(col any, val any) *Query[T] {
	return q.addCond(true, col, OpNe, val)
}

// Ge 大于等于
func (q *Query[T]) Ge(col any, val any) *Query[T] {
	return q.addCond(false, col, OpGe, val)
}

// OrGe 大于等于(或)
func (q *Query[T]) OrGe(col any, val any) *Query[T] {
	return q.addCond(true, col, OpGe, val)
}

// Le 小于等于
func (q *Query[T]) Le(col any, val any) *Query[T] {
	return q.addCond(false, col, OpLe, val)
}

// OrLe 小于等于(或)
func (q *Query[T]) OrLe(col any, val any) *Query[T] {
	return q.addCond(true, col, OpLe, val)
}

// Gt 大于
func (q *Query[T]) Gt(col any, val any) *Query[T] {
	return q.addCond(false, col, OpGt, val)
}

// OrGt 大于(或)
func (q *Query[T]) OrGt(col any, val any) *Query[T] {
	return q.addCond(true, col, OpGt, val)
}

// Lt 小于
func (q *Query[T]) Lt(col any, val any) *Query[T] {
	return q.addCond(false, col, OpLt, val)
}

// OrLt 小于(或)
func (q *Query[T]) OrLt(col any, val any) *Query[T] {
	return q.addCond(true, col, OpLt, val)
}

// Like 模糊查询
func (q *Query[T]) Like(col any, val string) *Query[T] {
	return q.addCond(false, col, OpLike, "%"+val+"%")
}

// OrLike 模糊查询(或)
func (q *Query[T]) OrLike(col any, val string) *Query[T] {
	return q.addCond(true, col, OpLike, "%"+val+"%")
}

// In 包含
func (q *Query[T]) In(col any, val any) *Query[T] {
	return q.addCond(false, col, OpIn, val)
}

// OrIn 包含(或)
func (q *Query[T]) OrIn(col any, val any) *Query[T] {
	return q.addCond(true, col, OpIn, val)
}

// NotIn 不包含
func (q *Query[T]) NotIn(col any, val any) *Query[T] {
	return q.addCond(false, col, OpNotIn, val)
}

// OrNotIn 不包含(或)
func (q *Query[T]) OrNotIn(col any, val any) *Query[T] {
	return q.addCond(true, col, OpNotIn, val)
}

// IsNull 为空
func (q *Query[T]) IsNull(col any) *Query[T] {
	return q.addCond(false, col, OpIsNull, nil)
}

// OrIsNull 为空(或)
func (q *Query[T]) OrIsNull(col any) *Query[T] {
	return q.addCond(true, col, OpIsNull, nil)
}

// IsNotNull 不为空
func (q *Query[T]) IsNotNull(col any) *Query[T] {
	return q.addCond(false, col, OpIsNotNull, nil)
}

// OrIsNotNull 不为空(或)
func (q *Query[T]) OrIsNotNull(col any) *Query[T] {
	return q.addCond(true, col, OpIsNotNull, nil)
}

// LikeLeft 左模糊查询
func (q *Query[T]) LikeLeft(col any, val string) *Query[T] {
	return q.addCond(false, col, OpLike, "%"+val)
}

// OrLikeLeft 左模糊查询(或)
func (q *Query[T]) OrLikeLeft(col any, val string) *Query[T] {
	return q.addCond(true, col, OpLike, "%"+val)
}

// LikeRight 右模糊查询
func (q *Query[T]) LikeRight(col any, val string) *Query[T] {
	return q.addCond(false, col, OpLike, val+"%")
}

// OrLikeRight 右模糊查询(或)
func (q *Query[T]) OrLikeRight(col any, val string) *Query[T] {
	return q.addCond(true, col, OpLike, val+"%")
}

// NotLike 不包含
func (q *Query[T]) NotLike(col any, val string) *Query[T] {
	return q.addCond(false, col, OpNotLike, "%"+val+"%")
}

// OrNotLike 不包含(或)
func (q *Query[T]) OrNotLike(col any, val string) *Query[T] {
	return q.addCond(true, col, OpNotLike, "%"+val+"%")
}

// Between 区间查询
func (q *Query[T]) Between(col any, val1 any, val2 any) *Query[T] {
	if val1 == nil || val2 == nil {
		q.errs = append(q.errs, errors.New("gplus: Between val1/val2 cannot be nil"))
		return q
	}
	return q.addCond(false, col, OpBetween, []any{val1, val2})
}

// OrBetween 区间查询(或)
func (q *Query[T]) OrBetween(col any, val1 any, val2 any) *Query[T] {
	if val1 == nil || val2 == nil {
		q.errs = append(q.errs, errors.New("gplus: OrBetween val1/val2 cannot be nil"))
		return q
	}
	return q.addCond(true, col, OpBetween, []any{val1, val2})
}

// NotBetween 区间查询（不包含边界）
func (q *Query[T]) NotBetween(col any, val1 any, val2 any) *Query[T] {
	if val1 == nil || val2 == nil {
		q.errs = append(q.errs, errors.New("gplus: NotBetween val1/val2 cannot be nil"))
		return q
	}
	return q.addCond(false, col, OpNotBetween, []any{val1, val2})
}

// OrNotBetween 区间查询（不包含边界）(或)
func (q *Query[T]) OrNotBetween(col any, val1 any, val2 any) *Query[T] {
	if val1 == nil || val2 == nil {
		q.errs = append(q.errs, errors.New("gplus: OrNotBetween val1/val2 cannot be nil"))
		return q
	}
	return q.addCond(true, col, OpNotBetween, []any{val1, val2})
}

// Order 排序
func (q *Query[T]) Order(col any, isAsc bool) *Query[T] {
	name, err := resolveColumnName(col)
	if err != nil {
		q.errs = append(q.errs, fmt.Errorf("gplus: Order invalid column pointer: %w", err))
		return q
	}
	direction := KeyDesc
	if isAsc {
		direction = KeyAsc
	}
	q.orders = append(q.orders, orderItem{expr: fmt.Sprintf("%s %s", name, direction)})
	return q
}

// OrderRaw 添加原生 ORDER BY 表达式，不经转义直接传入 GORM。
// 适用于含函数调用、CASE WHEN、NULLS LAST 等复杂排序场景。
// 调用顺序即为最终 SQL ORDER BY 的顺序，可与 Order 混用。
// 示例：q.OrderRaw("FIELD(status, 'active', 'pending')")
// 示例：q.OrderRaw("score DESC NULLS LAST")
// 注意：expr 参数由调用方负责安全性，不可直接拼接用户输入。
func (q *Query[T]) OrderRaw(expr string) *Query[T] {
	if expr == "" {
		q.errs = append(q.errs, errors.New("gplus: OrderRaw expr cannot be empty"))
		return q
	}
	q.orders = append(q.orders, orderItem{expr: expr, isRaw: true})
	return q
}

// Limit 分页
func (q *Query[T]) Limit(limit int) *Query[T] {
	q.limit = limit
	return q
}

// Offset 偏移
func (q *Query[T]) Offset(offset int) *Query[T] {
	q.offset = offset
	return q
}

// Omit 排除某些字段（不查询某些字段）
func (q *Query[T]) Omit(cols ...any) *Query[T] {
	for _, c := range cols {
		name, err := resolveColumnName(c)
		if err != nil {
			q.errs = append(q.errs, fmt.Errorf("gplus: Omit invalid column pointer: %w", err))
			continue
		}
		q.omits = append(q.omits, name)
	}
	return q
}

// Distinct 去重
// 支持传入字段指针或字符串，例如：q.Distinct(&user.Name, &user.Age)
// 如果不传参数，则默认为 DISTINCT *
func (q *Query[T]) Distinct(cols ...any) *Query[T] {
	// 调用去重方法 后 在这个生命周期中 去重都有效果
	q.distinct = true
	// 如果传入了特定列，将它们也作为 Select 字段处理
	for _, c := range cols {
		name, err := resolveColumnName(c)
		if err != nil {
			q.errs = append(q.errs, fmt.Errorf("gplus: Distinct invalid column pointer: %w", err))
			continue
		}
		q.selects = append(q.selects, name)
	}
	return q
}

// Group 分组
func (q *Query[T]) Group(cols ...any) *Query[T] {
	for _, c := range cols {
		name, err := resolveColumnName(c)
		if err != nil {
			q.errs = append(q.errs, fmt.Errorf("gplus: Group invalid column pointer: %w", err))
			continue
		}
		q.groups = append(q.groups, name)
	}
	return q
}

// Join 通用关联查询，支持自定义连接方式
// 示例：q.Join("profiles", gplus.JoinLeft, "profiles.user_id = users.id")
func (q *Query[T]) join(table, method, on string, args ...any) *Query[T] {
	if table == "" || method == "" {
		q.errs = append(q.errs, fmt.Errorf("gplus: join called with empty table=%q or method=%q", table, method))
		return q
	}
	q.joins = append(q.joins, joinInfo{method: method, table: table, on: on, args: args})
	return q
}

// LeftJoin 左连接：返回左表所有记录，即使右表无匹配
func (q *Query[T]) LeftJoin(table string, on string, args ...any) *Query[T] {
	return q.join(table, JoinLeft, on, args...)
}

// RightJoin 右连接：返回右表所有记录，即使左表无匹配
func (q *Query[T]) RightJoin(table string, on string, args ...any) *Query[T] {
	return q.join(table, JoinRight, on, args...)
}

// InnerJoin 内连接：仅返回两个表中匹配的记录（交集）
func (q *Query[T]) InnerJoin(table string, on string, args ...any) *Query[T] {
	return q.join(table, JoinInner, on, args...)
}

// OuterJoin 注意：裸 "OUTER JOIN" 不是标准 SQL，MySQL/PostgreSQL/SQLite 均不支持，
// 调用此方法将导致数据库语法错误。如需外连接，请使用 FullJoin ("FULL OUTER JOIN")。
func (q *Query[T]) OuterJoin(table string, on string, args ...any) *Query[T] {
	return q.join(table, JoinOuter, on, args...)
}

// FullJoin 全外连接：返回左右表中所有的记录
func (q *Query[T]) FullJoin(table string, on string, args ...any) *Query[T] {
	return q.join(table, JoinFull, on, args...)
}

// CrossJoin 交叉连接：返回笛卡尔积
// 注意：交叉连接通常不需要 ON 条件
func (q *Query[T]) CrossJoin(table string) *Query[T] {
	return q.join(table, JoinCross, "")
}

// NaturalJoin 自然连接：基于相同列名自动匹配
func (q *Query[T]) NaturalJoin(table string) *Query[T] {
	return q.join(table, JoinNatural, "")
}

// Unscoped 物理查询（包含被软删除的数据）
func (q *Query[T]) Unscoped() *Query[T] {
	q.unscoped = true
	return q
}

// LockWrite 加排他锁 (SELECT ... FOR UPDATE)
// 阻止其他事务读取或修改，直到本事务结束
func (q *Query[T]) LockWrite() *Query[T] {
	q.lockStrength = "UPDATE"
	return q
}

// LockRead 加共享锁 (SELECT ... FOR SHARE)
// 允许其他事务读取，但阻止其他事务修改
func (q *Query[T]) LockRead() *Query[T] {
	q.lockStrength = "SHARE"
	return q
}

// LockWithOpt 高级加锁 (支持 NOWAIT 或 SKIP LOCKED)
// strength: "UPDATE" / "SHARE"
// options: "NOWAIT" / "SKIP LOCKED"
func (q *Query[T]) LockWithOpt(strength, options string) *Query[T] {
	q.lockStrength = strength
	q.lockOptions = options
	return q
}

// And 开启一个带括号的 AND 嵌套块
func (q *Query[T]) And(fn func(sub *Query[T])) *Query[T] {
	if fn == nil {
		q.errs = append(q.errs, errors.New("gplus: And called with nil fn"))
		return q
	}
	sub := &Query[T]{
		ScopeBuilder: ScopeBuilder{conditions: make([]condition, 0)},
	}
	fn(sub)
	if len(sub.errs) > 0 {
		q.errs = append(q.errs, sub.errs...)
	}
	if len(sub.conditions) > 0 {
		q.conditions = append(q.conditions, condition{
			group: sub.conditions,
			isOr:  false,
		})
	}
	return q
}

// Having 添加分组过滤条件
// 示例: q.Having("COUNT(id)", OpGt, 10)
func (q *Query[T]) Having(col string, op string, val any) *Query[T] {
	if col == "" || op == "" {
		q.errs = append(q.errs, fmt.Errorf("gplus: Having called with empty col=%q or op=%q", col, op))
		return q
	}
	q.havings = append(q.havings, condition{
		expr:     col,
		operator: op,
		value:    val,
		isOr:     false,
	})
	return q
}

// OrHaving 添加 OR 分组过滤
func (q *Query[T]) OrHaving(col string, op string, val any) *Query[T] {
	if col == "" || op == "" {
		q.errs = append(q.errs, fmt.Errorf("gplus: OrHaving called with empty col=%q or op=%q", col, op))
		return q
	}
	q.havings = append(q.havings, condition{
		expr:     col,
		operator: op,
		value:    val,
		isOr:     true,
	})
	return q
}

// HavingGroup 嵌套 Having
func (q *Query[T]) HavingGroup(fn func(sub *Query[T])) *Query[T] {
	if fn == nil {
		q.errs = append(q.errs, errors.New("gplus: HavingGroup called with nil fn"))
		return q
	}
	sub := &Query[T]{ScopeBuilder: ScopeBuilder{havings: make([]condition, 0)}}
	fn(sub) // 开发者在 sub 里调用 Having/OrHaving
	if len(sub.errs) > 0 {
		q.errs = append(q.errs, sub.errs...)
	}
	if len(sub.havings) > 0 {
		q.havings = append(q.havings, condition{
			group: sub.havings,
			isOr:  false,
		})
	}
	return q
}

// Preload 预加载关联数据
// column: 结构体中的关联字段名（通常是字符串，如 "Orders" 或 "User.Role"）
// args: 可选的过滤条件，例如只预加载状态为已支付的订单
func (q *Query[T]) Preload(column string, args ...any) *Query[T] {
	if column == "" {
		q.errs = append(q.errs, errors.New("gplus: Preload called with empty column"))
		return q
	}
	if q.preloads == nil {
		q.preloads = make([]preloadInfo, 0)
	}
	q.preloads = append(q.preloads, preloadInfo{
		query: column,
		args:  args,
	})
	return q
}

// Or 开启一个带括号的 OR 嵌套块
func (q *Query[T]) Or(fn func(sub *Query[T])) *Query[T] {
	if fn == nil {
		q.errs = append(q.errs, errors.New("gplus: Or called with nil fn"))
		return q
	}
	sub := &Query[T]{
		ScopeBuilder: ScopeBuilder{conditions: make([]condition, 0)},
	}
	fn(sub)
	if len(sub.errs) > 0 {
		q.errs = append(q.errs, sub.errs...)
	}
	if len(sub.conditions) > 0 {
		q.conditions = append(q.conditions, condition{
			group: sub.conditions,
			isOr:  true,
		})
	}
	return q
}

// DataRuleBuilder 从上下文中提取规则并应用到查询中。
// 对同一个 Query 对象只执行一次，防止多次调用（如 Page 内的 Count+Find）重复追加条件。
func (q *Query[T]) DataRuleBuilder() *Query[T] {
	if q.dataRuleApplied {
		return q
	}
	q.dataRuleApplied = true
	if q.ctx == nil {
		return q
	}
	rules, ok := q.ctx.Value(DataRuleKey).([]DataRule)
	if !ok || len(rules) == 0 {
		return q
	}
	for _, rule := range rules {
		q.applyDataRule(rule)
	}
	return q
}

// splitTrimmed 按逗号分割字符串并对每个元素去除首尾空格
func splitTrimmed(s string) []string {
	parts := strings.Split(s, ",")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}

// applyDataRule 将单条 DataRule 转换为查询条件追加到 Query 中
func (q *Query[T]) applyDataRule(rule DataRule) {
	column := rule.Column
	c := strings.ToUpper(strings.TrimSpace(rule.Condition))
	value := rule.Value

	// 白名单校验列名，防止含括号/运算符的恶意表达式绕过 quoteColumn 转义
	if !validDataRuleColumn.MatchString(column) {
		q.errs = append(q.errs, fmt.Errorf("data rule: invalid column %q", column))
		return
	}

	// 1. 处理空值情况
	if value == "" && len(rule.Values) == 0 && c != "IS NULL" && c != "IS NOT NULL" {
		return
	}

	// 2. 禁止原生 SQL 注入：SQL/USE_SQL_RULES 条件类型存在 SQL 注入风险，
	// DataRule.Value 来自外部上下文，不可信任。
	// 如需执行原生 SQL，请使用 Repository.RawQuery/RawScan 并通过参数绑定传值。
	if c == "SQL" || c == "USE_SQL_RULES" {
		q.errs = append(q.errs, fmt.Errorf(
			"data rule [col: %s]: condition type %q is not allowed, use RawQuery with parameterized args instead",
			column, rule.Condition,
		))
		return
	}

	// 3. 映射常用操作符
	switch c {
	case "=", "EQ":
		q.Eq(column, value)
	case "<>", "!=", "NE":
		q.Ne(column, value)
	case ">", "GT":
		q.Gt(column, value)
	case ">=", "GE":
		q.Ge(column, value)
	case "<", "LT":
		q.Lt(column, value)
	case "<=", "LE":
		q.Le(column, value)
	case "IN":
		vals := rule.Values
		if len(vals) == 0 {
			vals = splitTrimmed(value)
		}
		q.In(column, vals)
	case "NOT IN":
		vals := rule.Values
		if len(vals) == 0 {
			vals = splitTrimmed(value)
		}
		q.NotIn(column, vals)
	case "LIKE":
		q.Like(column, value)
	case "LEFT_LIKE":
		q.LikeLeft(column, value)
	case "RIGHT_LIKE":
		q.LikeRight(column, value)
	case "IS NULL":
		q.IsNull(column)
	case "IS NOT NULL":
		q.IsNotNull(column)
	case "BETWEEN":
		var parts []string
		if len(rule.Values) == 2 {
			parts = rule.Values
		} else {
			parts = splitTrimmed(value)
		}
		if len(parts) != 2 {
			q.errs = append(q.errs, fmt.Errorf(
				"data rule [col: %s]: BETWEEN requires exactly 2 values, got %d",
				column, len(parts),
			))
			return
		}
		q.Between(column, parts[0], parts[1])
	default:
		q.errs = append(q.errs, fmt.Errorf(
			"data rule [col: %s]: unsupported condition %q; allowed: =, <>, >, >=, <, <=, IN, NOT IN, LIKE, LEFT_LIKE, RIGHT_LIKE, IS NULL, IS NOT NULL, BETWEEN",
			column, rule.Condition,
		))
	}
}
