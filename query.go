package gplus

import (
	"context"
	"errors"
	"fmt"
	"reflect"
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
	// core 承载 alias 体系状态（v0.8.0）；懒惰初始化
	core *queryCore
}

// NewQuery 创建泛型查询构建器，同时返回类型 T 的规范实例指针。
// 所有字段指针参数（如 &model.Name）必须来自返回的 *T 实例。
// ctx 用于传递请求级上下文（DataRule、超时等），可传 context.Background()。
func NewQuery[T any](ctx context.Context) (*Query[T], *T) {
	// 确保模型已注册
	model := getModelInstance[T]()
	return &Query[T]{
		ctx:  ctx,
		core: newQueryCore(ctx),
		errs: make([]error, 0, 8),
		ScopeBuilder: ScopeBuilder{
			conditions: make([]condition, 0, 8),
		},
	}, model
}

// NewQueryAs 创建 Query 并给主表起 alias。
//
// 返回的 *T 是独立 alias 实例（字段地址绑定到 alias），而非规范单例；
// 使用 &t.Field 时解析为 "alias.col" 而非 "table.col"。
//
// alias 必须满足 ^[a-zA-Z_][a-zA-Z0-9_]{0,31}$，否则累积 ErrAliasInvalidName。
func NewQueryAs[T any](ctx context.Context, alias string) (*Query[T], *T) {
	q := &Query[T]{
		ctx:  ctx,
		core: newQueryCore(ctx),
		errs: make([]error, 0, 8),
		ScopeBuilder: ScopeBuilder{
			conditions: make([]condition, 0, 8),
		},
	}
	// 复用 As 的全部校验逻辑（name 正则 / 链查重 / 创建独立实例）
	t := As[T](q, alias)
	return q, t
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

// GetError 将所有累积的错误合并为一个返回（含 alias core 中的错误）
func (q *Query[T]) GetError() error {
	all := append([]error(nil), q.errs...) // 显式拷贝避免 slice aliasing
	all = append(all, q.core.errs...)
	if len(all) == 0 {
		return nil
	}
	n := len(all)
	word := "errors"
	if n == 1 {
		word = "error"
	}
	summary := fmt.Errorf("gplus query builder failed with %d %s", n, word)
	return errors.Join(append([]error{summary}, all...)...)
}

// BuildQuery 覆盖 ScopeBuilder.BuildQuery（promoted method），在闭包入口添加 v0.8.0 决策 1B errs 短路。
//
// 行为：
//   - 若 q.core.errs 非空（含 As 重名/Clear 后用残骸等错误）：返回的 closure 调用时
//     直接 db.AddError 返回，不生成 SQL
//   - 若 q.core.errs 为空：与 ScopeBuilder.BuildQuery 既有行为一致（DataRule + 条件构建）
//
// 这确保所有 .Scopes(q.BuildQuery()) 生产路径（repo.GetById/List/Page/FindAs 等）
// 在 As 重名 / Clear 残骸 / 字段地址未注册等错误下自动短路，不依赖调用方先调 GetError。
func (q *Query[T]) BuildQuery() func(db *gorm.DB) *gorm.DB {
	innerScope := q.ScopeBuilder.BuildQuery()
	return func(db *gorm.DB) *gorm.DB {
		if q.core != nil && len(q.core.errs) > 0 {
			session := db.Session(&gorm.Session{})
			_ = session.AddError(errors.Join(q.core.errs...))
			return session
		}
		return innerScope(db)
	}
}

// BuildQueryDB 将当前 Query 的条件应用到 db 并返回带条件的 *gorm.DB。
// v0.8.0 决策 1B：若 q.core.errs 非空（含 As 重名等错误），直接返回带聚合错误的 db，不生成 SQL。
// 防止重名 alias / Clear 后用 alias 等错误被快乐路径 SQL 静默掩盖。
//
// 与 BuildQuery()（返回闭包供 Scopes 使用）互补；
// BuildQueryDB 用于需要直接获得 *gorm.DB 的场景（如 DataRuleBuilder 链式调用末尾）。
func (q *Query[T]) BuildQueryDB(db *gorm.DB) *gorm.DB {
	return q.BuildQuery()(db)
}

// Clear 重写 Query 的清除逻辑
func (q *Query[T]) Clear() {
	// v0.8.0 N4：翻转所有 alias entry 的 revoked 标志，防 Clear 后用 alias 残骸。
	// 保留 entries（不清空 aliases map），让后续 lookupAddr 命中 revoked 区间触发 ErrAliasRevoked。
	// 同时清空 outerQueryRef 和 core.errs，避免悬空引用和旧错误干扰后续使用。
	if q.core != nil {
		for _, entry := range q.core.aliases {
			entry.revoked = true
		}
		q.core.outerQueryRef = nil
		q.core.errs = nil
	}
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

// resolveColumnNameAny 接受 col any（兼容 v0.6.0 既有签名），
// 内部走 v0.8.0 alias 链解析路径 + 全局 fallback。
//
// col 可能是字符串（直接列名）或字段指针（地址解析）：
//   - 字符串：直接返回（保持 v0.6.0 字符串列名行为）
//   - 指针且 q.core 已初始化：走 method resolveColumnName（alias 链 + 全局 cache）
//   - q.core == nil（And/Or 内部子 Query）或非指针：回退包级 resolveColumnName（全局 cache）
func (q *Query[T]) resolveColumnNameAny(col any) (string, error) {
	if s, ok := col.(string); ok {
		// 字符串列名直接校验并返回（与包级函数行为一致）
		if s == "" {
			return "", ErrColumnEmpty
		}
		return s, nil
	}
	if col == nil {
		return "", ErrInvalidPointer
	}
	// q.core == nil 时（如 And/Or 嵌套块的临时子 Query），回退包级路径（全局 cache）
	// 这等价于 v0.7.x 既有行为，不影响无 alias 场景
	if q.core == nil {
		return resolveColumnName(col)
	}
	addr := reflectPointerAddr(col)
	if addr == 0 {
		// 非指针类型或零地址，回退包级路径
		return resolveColumnName(col)
	}
	return q.resolveColumnName(addr)
}

// addCond 内部辅助方法
func (q *Query[T]) addCond(isOr bool, col any, op string, val any) *Query[T] {
	name, err := q.resolveColumnNameAny(col)
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
		name, err := q.resolveColumnNameAny(c)
		if err != nil {
			q.errs = append(q.errs, fmt.Errorf("gplus: Select invalid column pointer: %w", err))
			continue
		}
		q.selects = append(q.selects, name)
	}
	return q
}

// SelectRaw 添加原生 SELECT 字段表达式。
// expr 为原生 SQL 表达式，不经列名转义直接传入 GORM。
// 示例：q.SelectRaw("AVG(age)").SelectRaw("COUNT(*) as cnt")
// 注意：expr 参数由调用方负责安全性，不可直接拼接用户输入。
func (q *Query[T]) SelectRaw(expr string) *Query[T] {
	if expr == "" {
		q.errs = append(q.errs, errors.New("gplus: SelectRaw expr cannot be empty"))
		return q
	}
	q.selects = append(q.selects, expr)
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
	// 1. 使用 Session(&gorm.Session{NewDB: true}) 创建干净会话，避免污染传入的 db
	// 2. 通过 Model 注入 T 的表名（子查询场景下外层 db 可能指向其他表）
	// 3. 调用 BuildQuery() 获取闭包并应用条件
	session := db.Session(&gorm.Session{NewDB: true}).Model(getModelInstance[T]())
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

// InSub IN 子查询：col IN (subquery)。
//
// sub 必须为类型安全 *Query[X]；外部冒名实现被 gplusSubquery() guard 阻止。
// sub 应在传入前完成构建（包括 Select/Where/DataRuleBuilder），传入后再修改会
// 反映到最终 SQL（延迟调用语义）。
//
// sub 中需用 Select(&col) 限定单列；否则 GORM 运行时报多列错误。
func (q *Query[T]) InSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(false, col, OpIn, sub)
}

// OrInSub IN 子查询(或)。详见 InSub。
func (q *Query[T]) OrInSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(true, col, OpIn, sub)
}

// NotInSub NOT IN 子查询。详见 InSub。
func (q *Query[T]) NotInSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(false, col, OpNotIn, sub)
}

// OrNotInSub NOT IN 子查询(或)。详见 InSub。
func (q *Query[T]) OrNotInSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(true, col, OpNotIn, sub)
}

// EqSub 子查询：col = (subquery)。详见 InSub 关于 sub 生命周期约束。
func (q *Query[T]) EqSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(false, col, OpEq, sub)
}

// OrEqSub 子查询(或)。详见 EqSub。
func (q *Query[T]) OrEqSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(true, col, OpEq, sub)
}

// NeSub <> 子查询：col <> (subquery)。详见 InSub。
func (q *Query[T]) NeSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(false, col, OpNe, sub)
}

// OrNeSub <> 子查询(或)。详见 NeSub。
func (q *Query[T]) OrNeSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(true, col, OpNe, sub)
}

// GtSub > 子查询：col > (subquery)。详见 InSub。
func (q *Query[T]) GtSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(false, col, OpGt, sub)
}

// OrGtSub > 子查询(或)。详见 GtSub。
func (q *Query[T]) OrGtSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(true, col, OpGt, sub)
}

// GteSub >= 子查询：col >= (subquery)。详见 InSub。
func (q *Query[T]) GteSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(false, col, OpGe, sub)
}

// OrGteSub >= 子查询(或)。详见 GteSub。
func (q *Query[T]) OrGteSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(true, col, OpGe, sub)
}

// LtSub < 子查询：col < (subquery)。详见 InSub。
func (q *Query[T]) LtSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(false, col, OpLt, sub)
}

// OrLtSub < 子查询(或)。详见 LtSub。
func (q *Query[T]) OrLtSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(true, col, OpLt, sub)
}

// LteSub <= 子查询：col <= (subquery)。详见 InSub。
func (q *Query[T]) LteSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(false, col, OpLe, sub)
}

// OrLteSub <= 子查询(或)。详见 LteSub。
func (q *Query[T]) OrLteSub(col any, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	return q.addCond(true, col, OpLe, sub)
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
	name, err := q.resolveColumnNameAny(col)
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
		name, err := q.resolveColumnNameAny(c)
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
		name, err := q.resolveColumnNameAny(c)
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
		name, err := q.resolveColumnNameAny(c)
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
//
// Deprecated: use LeftJoinAs for type-safe column references.
// Will be removed in v1.0. Still useful for joining subquery tables /
// function-returning tables / USING clauses where alias instances
// cannot represent the join target.
func (q *Query[T]) LeftJoin(table string, on string, args ...any) *Query[T] {
	return q.join(table, JoinLeft, on, args...)
}

// RightJoin 右连接：返回右表所有记录，即使左表无匹配
//
// Deprecated: use RightJoinAs for type-safe column references.
// Will be removed in v1.0. Still useful for joining subquery tables /
// function-returning tables / USING clauses where alias instances
// cannot represent the join target.
func (q *Query[T]) RightJoin(table string, on string, args ...any) *Query[T] {
	return q.join(table, JoinRight, on, args...)
}

// InnerJoin 内连接：仅返回两个表中匹配的记录（交集）
//
// Deprecated: use InnerJoinAs for type-safe column references.
// Will be removed in v1.0. Still useful for joining subquery tables /
// function-returning tables / USING clauses where alias instances
// cannot represent the join target.
func (q *Query[T]) InnerJoin(table string, on string, args ...any) *Query[T] {
	return q.join(table, JoinInner, on, args...)
}

// OuterJoin 注意：裸 "OUTER JOIN" 不是标准 SQL，MySQL/PostgreSQL/SQLite 均不支持，
// 调用此方法将导致数据库语法错误。如需外连接，请使用 FullJoin ("FULL OUTER JOIN")。
//
// Deprecated: use OuterJoinAs for type-safe column references.
// Will be removed in v1.0. Still useful for joining subquery tables /
// function-returning tables / USING clauses where alias instances
// cannot represent the join target.
func (q *Query[T]) OuterJoin(table string, on string, args ...any) *Query[T] {
	return q.join(table, JoinOuter, on, args...)
}

// FullJoin 全外连接：返回左右表中所有的记录
//
// Deprecated: use FullJoinAs for type-safe column references.
// Will be removed in v1.0. Still useful for joining subquery tables /
// function-returning tables / USING clauses where alias instances
// cannot represent the join target.
func (q *Query[T]) FullJoin(table string, on string, args ...any) *Query[T] {
	return q.join(table, JoinFull, on, args...)
}

// CrossJoin 交叉连接：返回笛卡尔积
// 注意：交叉连接通常不需要 ON 条件
//
// Deprecated: use CrossJoinAs for type-safe column references.
// Will be removed in v1.0. Still useful for joining subquery tables /
// function-returning tables / USING clauses where alias instances
// cannot represent the join target.
func (q *Query[T]) CrossJoin(table string) *Query[T] {
	return q.join(table, JoinCross, "")
}

// NaturalJoin 自然连接：基于相同列名自动匹配
//
// Deprecated: use NaturalJoinAs for type-safe column references.
// Will be removed in v1.0. Still useful for joining subquery tables /
// function-returning tables / USING clauses where alias instances
// cannot represent the join target.
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

// LeftJoinAs 类型安全的 LEFT JOIN（v0.8.0 alias 体系）。
//
// alias 必须由 As[X](q, ...) 创建的实例，且属于当前 q 链；
// leftCol / rightCol 为任意一侧的字段指针（主表或副表均可）；
// extraSQL 为额外的 ON 条件 SQL 片段，仅含 ? 占位符（如 "AND o.status = ?"）——
// 绝不直接拼接用户输入，extraSQL 本身不经 fmt.Sprintf 处理；
// extraArgs 对应占位符参数，走 GORM 参数化预编译，与 db.Joins(sql, args...) 同语义。
//
// 错误处理：alias 不属于 q 链时累积 ErrAliasNotInChain 并跳过该 JOIN（保持链式）。
func (q *Query[T]) LeftJoinAs(alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Query[T] {
	q.appendJoinAs("LEFT JOIN", alias, leftCol, rightCol, extraSQL, extraArgs)
	return q
}

// RightJoinAs 类型安全的 RIGHT JOIN（参数语义同 LeftJoinAs）
func (q *Query[T]) RightJoinAs(alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Query[T] {
	q.appendJoinAs("RIGHT JOIN", alias, leftCol, rightCol, extraSQL, extraArgs)
	return q
}

// InnerJoinAs 类型安全的 INNER JOIN
func (q *Query[T]) InnerJoinAs(alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Query[T] {
	q.appendJoinAs("INNER JOIN", alias, leftCol, rightCol, extraSQL, extraArgs)
	return q
}

// OuterJoinAs 类型安全的 OUTER JOIN（部分方言别名为 FULL OUTER JOIN）
func (q *Query[T]) OuterJoinAs(alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Query[T] {
	q.appendJoinAs("OUTER JOIN", alias, leftCol, rightCol, extraSQL, extraArgs)
	return q
}

// FullJoinAs 类型安全的 FULL JOIN
func (q *Query[T]) FullJoinAs(alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Query[T] {
	q.appendJoinAs("FULL JOIN", alias, leftCol, rightCol, extraSQL, extraArgs)
	return q
}

// CrossJoinAs 类型安全的 CROSS JOIN（无 ON 条件）
func (q *Query[T]) CrossJoinAs(alias any) *Query[T] {
	q.appendJoinAsNoOn("CROSS JOIN", alias)
	return q
}

// NaturalJoinAs 类型安全的 NATURAL JOIN（无 ON 条件）
func (q *Query[T]) NaturalJoinAs(alias any) *Query[T] {
	q.appendJoinAsNoOn("NATURAL JOIN", alias)
	return q
}

// appendJoinAs 内部辅助：构建 alias JOIN 条目并追加到 ScopeBuilder.joins。
// joinType 为 "LEFT JOIN" / "INNER JOIN" 等字面量。
// C1 保障：joinSQL 仅拼接结构化字面量（table 名 / alias 名 / 列名），
// extraArgs 全程不进入字符串，由 GORM 以参数化方式注入。
func (q *Query[T]) appendJoinAs(joinType string, alias any, leftCol any, rightCol any, extraSQL string, extraArgs []any) {
	if q.core == nil {
		return
	}

	// 1. 校验 alias 实例属于 q 链，取其 alias name 和 reflect.Type
	aliasName, aliasTyp, ok := q.lookupAliasFromChain(alias)
	if !ok {
		q.core.appendErr(ErrAliasNotInChain)
		return
	}

	// 2. 解析 leftCol / rightCol（调用 v0.8.0 resolveColumnName，addr 路径）
	leftStr, lerr := q.resolveColumnName(reflectPointerAddr(leftCol))
	if lerr != nil {
		// resolveColumnName 已 appendErr，直接返回
		return
	}
	rightStr, rerr := q.resolveColumnName(reflectPointerAddr(rightCol))
	if rerr != nil {
		return
	}

	// 3. 构造 JOIN SQL 字面量（C1：仅拼接类型安全的标识符，不拼 extraArgs）
	// 注意：此处不带方言引号（qL/qR 未知）；方言感知转义由 applyJoins 调用 db.Joins 时由 GORM 处理。
	tableName := aliasSchemaTableName(aliasTyp)
	joinSQL := fmt.Sprintf("%s %s AS %s ON %s = %s",
		joinType,
		tableName,
		aliasName,
		leftStr,
		rightStr,
	)
	if extraSQL != "" {
		// extraSQL 含 ? 占位符，参数由 extraArgs 提供，绝不内联
		joinSQL += " " + extraSQL
	}

	// 4. 追加到 joins（rawSQL=true 表示 table 字段存储完整 SQL）
	q.joins = append(q.joins, joinInfo{
		table:     joinSQL,  // rawSQL 路径：table 存储完整 JOIN 片段
		args:      extraArgs, // 走 GORM 参数化
		aliasName: aliasName,
		rawSQL:    true,
	})
}

// appendJoinAsNoOn 内部辅助：无 ON 的 JOIN 构建（CrossJoinAs / NaturalJoinAs 共用）。
// 与 appendJoinAs 区别：仅接收 joinType 和 alias，省去 leftCol/rightCol 解析。
func (q *Query[T]) appendJoinAsNoOn(joinType string, alias any) {
	if q.core == nil {
		return
	}

	// 1. 校验 alias 实例属于 q 链，取其 alias name 和 reflect.Type
	aliasName, aliasTyp, ok := q.lookupAliasFromChain(alias)
	if !ok {
		q.core.appendErr(ErrAliasNotInChain)
		return
	}

	// 2. 构造 JOIN SQL 字面量（仅表名 + 别名，无 ON 条件）
	tableName := aliasSchemaTableName(aliasTyp)
	joinSQL := fmt.Sprintf("%s %s AS %s",
		joinType,
		tableName,
		aliasName,
	)

	// 3. 追加到 joins（rawSQL=true，同 appendJoinAs 路径）
	q.joins = append(q.joins, joinInfo{
		table:     joinSQL,
		aliasName: aliasName,
		rawSQL:    true,
	})
}

// lookupAliasFromChain 沿 q 链（outerQueryRef 方向）查找某个实例对应的 alias 注册项。
// 返回 alias name、reflect.Type 和是否找到。
func (q *Query[T]) lookupAliasFromChain(alias any) (name string, typ reflect.Type, ok bool) {
	if alias == nil {
		return "", nil, false
	}
	addr := reflectPointerAddr(alias)

	var current AnyQuery = q
	for current != nil {
		core := current.gplusCore()
		for _, entry := range core.aliases {
			if entry.addrLow == addr {
				// 精确匹配实例基地址（alias 是整个实例，非字段）
				if entry.revoked {
					return "", nil, false
				}
				return entry.name, entry.typ, true
			}
		}
		current = core.outerQueryRef
	}
	return "", nil, false
}

// reflectPointerAddr 取指针类型 any 的底层地址（uintptr）。
// alias 和 col 参数均为指针（*X 或 *field），此函数统一提取。
func reflectPointerAddr(v any) uintptr {
	return reflect.ValueOf(v).Pointer()
}

// aliasSchemaTableName 根据 reflect.Type 推导 GORM 默认表名（复用 nsColumnName 转蛇形 + 复数）。
// 例：Order → "orders"，UserProfile → "user_profiles"。
// 若 X 实现了 TableName() string，则使用自定义表名；否则走默认规则。
func aliasSchemaTableName(typ reflect.Type) string {
	// 检查是否实现 TableName 接口（GORM 约定）
	// 通过 reflect.New 构造实例，调用 TableName() 方法
	ptrTyp := reflect.PointerTo(typ)
	tableNameMethod, has := ptrTyp.MethodByName("TableName")
	if has && tableNameMethod.Type.NumIn() == 1 && tableNameMethod.Type.NumOut() == 1 &&
		tableNameMethod.Type.Out(0) == reflect.TypeOf("") {
		result := tableNameMethod.Func.Call([]reflect.Value{reflect.New(typ)})
		if len(result) == 1 {
			return result[0].String()
		}
	}
	// 默认：蛇形 + 复数（GORM 默认命名规范）
	return nsColumnName(typ.Name()) + "s"
}

// mainTableName 返回 T 的主表 table 名（用于 resolveColumnName 顶层 fallback 加前缀）。
// 复用 aliasSchemaTableName 逻辑（处理 TableName() interface + 命名规则）。
func (q *Query[T]) mainTableName() string {
	var zero T
	typ := reflect.TypeOf(zero)
	if typ == nil {
		return ""
	}
	return aliasSchemaTableName(typ)
}

// Exists 添加 EXISTS 子查询条件（AND）。
//
// sub 通常通过 SubQuery[X](q) 派生，能引用外层 q 的字段（相关子查询）。
// sub == nil 时累积 ErrSubqueryNil；sub.GetError() 非空时在 BuildQuery 执行时透传到外层 GORM 错误链。
func (q *Query[T]) Exists(sub Subquerier) *Query[T] {
	return q.appendExists("EXISTS", false, sub)
}

// NotExists 添加 NOT EXISTS 子查询条件（AND）。详见 Exists。
func (q *Query[T]) NotExists(sub Subquerier) *Query[T] {
	return q.appendExists("NOT EXISTS", false, sub)
}

// OrExists 添加 OR EXISTS 子查询条件。与 Exists 相同，但使用 OR 逻辑。
func (q *Query[T]) OrExists(sub Subquerier) *Query[T] {
	return q.appendExists("EXISTS", true, sub)
}

// OrNotExists 添加 OR NOT EXISTS 子查询条件。与 NotExists 相同，但使用 OR 逻辑。
func (q *Query[T]) OrNotExists(sub Subquerier) *Query[T] {
	return q.appendExists("NOT EXISTS", true, sub)
}

// appendExists 内部辅助：构建 EXISTS / NOT EXISTS 条件并追加到 conditions。
// 若 sub.GetError() 非空，立即透传到 q.errs（与 InSub 等一致的错误累积策略）。
func (q *Query[T]) appendExists(op string, isOr bool, sub Subquerier) *Query[T] {
	if sub == nil {
		q.errs = append(q.errs, ErrSubqueryNil)
		return q
	}
	// 子查询已有错误，立即透传到外层 q.errs，使调用方 GetError() 可感知
	if subErr := sub.GetError(); subErr != nil {
		q.errs = append(q.errs, subErr)
	}
	q.conditions = append(q.conditions, condition{
		subExpr:  sub,
		existsOp: op,
		isOr:     isOr,
	})
	return q
}

// gplusSubquery 私有 guard 方法，阻止外部包冒名实现 Subquerier 接口。
func (q *Query[T]) gplusSubquery() {}

// gplusCore 返回 Query[T] 的 queryCore。
// 实现 AnyQuery 接口，供 As 包级函数使用。
func (q *Query[T]) gplusCore() *queryCore {
	return q.core
}

// 编译期断言：*Query[T] 实现 AnyQuery 接口
var _ AnyQuery = (*Query[struct{}])(nil)

// resolveColumnName 解析字段地址到列名（v0.8.0 alias 体系）
//
// 路径：
//  1. 当前 q.core.aliases 命中（含 H2 offset 校验）→ "alias.col"
//  2. 沿 outerQueryRef 链向上重复（含 N4 revoked 检查）
//  3. 链遍历完毕后未命中：回退全局 columnNameCache（v0.7.x 既有行为）
//     - sub 模式（outerQueryRef != nil）同样允许全局 fallback（Task 14 关联子查询语义）
//     - 全局 cache 也未命中 → ErrFieldAddrUnregistered
//
// 错误累积到 q.core.errs；调用方可通过 q.GetError() 感知
func (q *Query[T]) resolveColumnName(addr uintptr) (string, error) {
	if q.core == nil {
		// Task 4 强制初始化后此分支不应触发；防御性处理
		return "", fmt.Errorf("gplus: query core not initialized")
	}

	var current AnyQuery = q
	for current != nil {
		core := current.gplusCore()
		if alias, col, ok := core.lookupAddr(addr); ok {
			return alias + "." + col, nil
		}
		// N4：lookupAddr 命中 revoked 区间但被拒，立即累积 ErrAliasRevoked 不再继续
		if core.hadRevokedHit(addr) {
			q.core.appendErr(ErrAliasRevoked)
			return "", ErrAliasRevoked
		}
		current = core.outerQueryRef
	}

	// 顶层 fallback：用 schema.go 的全局 columnNameCache 查找
	// v0.8.0 Task 14：sub 允许 fallback 全局 cache，支持关联子查询引用外层表规范单例字段。
	// 原 H5 严格闭合设计（isSub → 直接报错）在 SubQuery 实装后放宽：
	// sub 通过 outerQueryRef 链已与外层 query 合法关联，访问外层 T 的规范单例是预期用法。
	// TD-7（v0.9 加 StrictColumnResolution 选项）仍适用，此处保持 v0.7.x fallback 语义。
	// 注意：此路径未做 alias 链反向校验（H5 反向风险）——若用户在不同 q 间复用规范单例字段地址，
	// 全局 cache 会静默命中产生跨 Query SQL（FROM 表与列引用不匹配）。
	// v0.7.x 既有行为，不破坏兼容；spec §11.2 TD-7 已记录，v0.9 加 StrictColumnResolution 选项。
	if name, ok := columnNameCache.Load(addr); ok {
		// v0.8.0 fix: 若 q 已注册 alias（说明在 JOIN/alias 场景），给主表列名加表前缀，
		// 避免多表 JOIN 时 SQL 报 ambiguous（如 ON o.user_id = id 缺主表前缀）。
		// 仅在 alias 场景下触发，单表查询保持 v0.7.x 裸列名行为（向后兼容）。
		if len(q.core.aliases) > 0 {
			mainTable := q.mainTableName()
			if mainTable != "" {
				return mainTable + "." + name.(string), nil
			}
		}
		return name.(string), nil
	}

	q.core.appendErr(ErrFieldAddrUnregistered)
	return "", ErrFieldAddrUnregistered
}
