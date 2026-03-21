package gplus

import (
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// condition 内部结构，存储单个查询条件
type condition struct {
	// expr 双重语义：isRaw=false 时为列名；isRaw=true 时为完整 SQL 片段
	expr     string
	operator string // =, <>, >, >=, <, <=, IN, NOT IN, LIKE, NOT LIKE, BETWEEN, NOT BETWEEN, IS NULL, IS NOT NULL
	value    any
	isOr     bool // true 为 OR，false 为 AND
	isRaw    bool // 是否为原生 SQL
	// 用于存储嵌套的子条件块
	group []condition
}

// orderItem 存储单个排序项，isRaw=true 时 expr 为原生表达式，不经转义
type orderItem struct {
	expr  string
	isRaw bool
}

// joinInfo 结构化 Join 存储，优化性能
// joinInfo 结构化存储 Join 信息，避免闭包带来的额外开销
type joinInfo struct {
	method string // LEFT JOIN, RIGHT JOIN, INNER JOIN
	table  string // 表名
	on     string // 关联条件
	args   []any  // 条件参数
}

// DataRule 对外开放的核心规则字段
type DataRule struct {
	Column    string   // 规则字段 (例如: "dept_id")；仅允许字母/数字/下划线及单个点分隔的表名前缀（如 "table.col"），含括号或运算符的表达式会被拒绝
	Condition string   // 规则条件 (例如: "=", "IN", "LIKE")
	Value     string   // 规则值   (例如: "1001"）；IN/NOT IN/BETWEEN 建议使用 Values
	Values    []string // IN/NOT IN/BETWEEN 的多值列表，优先于 Value 的逗号分隔解析
}

// preloadInfo 存储预加载信息
type preloadInfo struct {
	query string // 关联属性名（如 "Orders"）
	args  []any  // 过滤关联数据的额外条件
}

// 定义 Context 中使用的 Key 类型，防止命名冲突
type dataRuleKey struct{}

// DataRuleKey 是用于在 context.Context 中存储 []DataRule 的键。
// 使用示例：ctx = context.WithValue(ctx, gplus.DataRuleKey, rules)
var DataRuleKey = dataRuleKey{}

// validDataRuleColumn 白名单校验 DataRule.Column，防止含括号/运算符的恶意表达式绕过 quoteColumn 转义。
// 允许: 字母/数字/下划线开头，可含单个点（table.col 形式）
var validDataRuleColumn = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)?$`)

// ScopeBuilder 负责将条件转换为 GORM Scope
// 这是 QueryCond 和 UpdateCond 的基类
type ScopeBuilder struct {
	// tableName 动态表名
	tableName string
	// conditions 是核心字段，用于构建 Where 条件
	conditions []condition
	// selects 用于构建 Select 字段
	selects []string
	// omits 用于构建 Omit 字段
	omits []string
	// orders 统一存储所有排序项，保留调用顺序；isRaw=true 时不经转义直接传给 GORM
	orders []orderItem
	// groups 用于构建 Group 字段
	groups []string
	// havings 用于构建 Having 字段
	havings []condition
	// joins 结构化 Join
	joins  []joinInfo
	limit  int
	offset int
	// 是否使用软删除
	unscoped bool
	// 是否去重
	distinct bool
	// 存储预加载列表
	preloads []preloadInfo
	// 悲观锁配置
	// lockStrength 锁强度: "UPDATE" (排他锁) 或 "SHARE" (共享锁)
	lockStrength string
	// lockOptions 锁选项: "NOWAIT" (不等待报错), "SKIP LOCKED" (跳过被锁行)
	lockOptions string
	// scopes 存储用户注入的自定义 GORM scope 函数，按顺序执行
	scopes []func(*gorm.DB) *gorm.DB
}

// applyScopes 将用户注入的自定义 scope 函数依次应用到 db
func (b *ScopeBuilder) applyScopes(db *gorm.DB) *gorm.DB {
	if len(b.scopes) == 0 {
		return db
	}
	return db.Scopes(b.scopes...)
}

// BuildCount 计数构建
func (b *ScopeBuilder) BuildCount() func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		// 获取当前数据库的转义符
		qL, qR := getQuoteChar(db)
		//  基础条件
		db = b.applyBaseTable(db)
		// where
		db = b.applyWhere(db, qL, qR)
		//  join
		db = b.applyJoins(db)
		// 分组与聚合过滤
		db = b.applyGroupHaving(db, qL, qR)
		db = b.applyScopes(db)
		return db
	}
}

// BuildQuery 专门用于查询 (Find/First/List)
func (b *ScopeBuilder) BuildQuery() func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		// 获取当前数据库的转义符
		qL, qR := getQuoteChar(db)
		// 基础条件
		db = b.applyBaseTable(db)
		// 查询字段
		db = b.applySelects(db, qL, qR)
		// 去重
		db = b.applyDistinct(db)
		// 查询条件
		db = b.applyWhere(db, qL, qR)
		// 关联查询
		db = b.applyJoins(db)
		// 分组与聚合过滤
		db = b.applyGroupHaving(db, qL, qR)
		// 分页
		db = b.applyOrderLimit(db, qL, qR)
		// 悲观锁
		db = b.applyStrength(db)
		// 预加载
		db = b.applyPreloads(db)
		db = b.applyScopes(db)
		return db
	}
}

// BuildUpdate 专门用于更新 (Updates/Update/Save)
// 更新逻辑不应包含 Distinct, Limit, Offset, Order
func (b *ScopeBuilder) BuildUpdate() func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		qL, qR := getQuoteChar(db)
		db = b.applyBaseTable(db)
		// 更新字段
		db = b.applySelects(db, qL, qR)

		// 更新通常只依赖 Where 条件
		db = b.applyWhere(db, qL, qR)

		// 某些复杂场景下更新可能需要 Join (取决于数据库支持情况，按需保留)
		db = b.applyJoins(db)
		db = b.applyScopes(db)
		return db
	}
}

// BuildDelete 专门用于删除 (Delete)
// 删除操作最核心的是 Where 和 Unscoped
func (b *ScopeBuilder) BuildDelete() func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		qL, qR := getQuoteChar(db)
		// 基础条件
		db = b.applyBaseTable(db)
		// 删除逻辑只应用 Where 条件，防止误带 Limit/Order 导致部分数据库报错
		db = b.applyWhere(db, qL, qR)
		db = b.applyScopes(db)
		return db
	}
}

// Clear 清空所有查询/构建条件
func (b *ScopeBuilder) Clear() {
	// 1. 基础字段复位
	b.tableName = "" //必须清除表名
	b.limit = 0
	b.offset = 0
	b.unscoped = false
	b.distinct = false

	// 2. 切片复位
	// 含嵌套引用的切片（condition.group、joinInfo.args、preloadInfo.args）
	// 置 nil 以释放内部引用，避免 backing array 持续持有内存
	b.conditions = nil
	b.havings = nil
	b.joins = nil
	b.preloads = nil
	// 纯 string 切片无嵌套引用，[:0] 保留容量可安全复用
	b.selects = b.selects[:0]
	b.omits = b.omits[:0]
	b.orders = b.orders[:0]
	b.groups = b.groups[:0]

	// 清理锁状态
	b.lockStrength = ""
	b.lockOptions = ""
	b.scopes = nil
}

// --- 内部私有组件，用于复用代码 ---

// getQuoteChar 自动探测数据库方言并设置转义符
func getQuoteChar(db *gorm.DB) (string, string) {
	if db.Dialector == nil {
		return "", "" // 无法探测方言，交由 GORM 自行处理
	}
	switch db.Name() {
	case "postgres", "sqlite":
		return "\"", "\""
	case "sqlserver":
		return "[", "]"
	case "mysql", "tidb":
		return "`", "`"
	default: // 未知方言，不强制转义，交由 GORM 自行处理
		return "", ""
	}
}

// applyBaseTable 统一处理表名和 Unscoped
func (b *ScopeBuilder) applyBaseTable(db *gorm.DB) *gorm.DB {
	// 如果手动指定了表名，则优先使用 Table 方法，否则 GORM 会根据 Model 反射
	if b.tableName != "" {
		db = db.Table(b.tableName) // 动态指定表名
	}
	if b.unscoped {
		db = db.Unscoped()
	}
	return db
}

// applySelects select
func (b *ScopeBuilder) applySelects(db *gorm.DB, qL, qR string) *gorm.DB {
	// 对 Select 字段进行深度转义
	// GORM 的 Select 接收 string 或 []string
	// 为了防止 GORM 再次错误转义，我们传入处理好的字符串
	if len(b.selects) > 0 {
		db = db.Select(quoteColumns(b.selects, qL, qR))
	}
	if len(b.omits) > 0 {
		db = db.Omit(quoteColumns(b.omits, qL, qR)...)
	}
	return db
}

// applyDistinct 去重 如果调用了 Distinct 方法 将对select 字段进行去重
func (b *ScopeBuilder) applyDistinct(db *gorm.DB) *gorm.DB {
	// 去重 distinctArgs 不为空 且 distinct 为 true
	if b.distinct {
		db = db.Distinct()
	}
	return db
}

// buildLeafSQL 为叶子条件（非嵌套组、非子查询）生成 SQL 片段和参数列表。
// ok=false 表示应跳过（BETWEEN 参数校验失败的防御性情况）。
func buildLeafSQL(cond condition, qL, qR string) (sqlStr string, args []any, ok bool) {
	if cond.isRaw {
		if cond.value == nil {
			return cond.expr, nil, true
		}
		// 多参数时 value 存储为 []any，直接展开传给 GORM
		if args, ok := cond.value.([]any); ok {
			return cond.expr, args, true
		}
		return cond.expr, []any{cond.value}, true
	}
	quotedCol := quoteColumn(cond.expr, qL, qR)
	switch cond.operator {
	case OpBetween, OpNotBetween:
		a, isSlice := cond.value.([]any)
		if !isSlice || len(a) != 2 {
			return "", nil, false
		}
		return fmt.Sprintf("%s %s ? AND ?", quotedCol, cond.operator), a, true
	case OpIsNull, OpIsNotNull:
		return fmt.Sprintf("%s %s", quotedCol, cond.operator), nil, true
	default:
		return fmt.Sprintf("%s %s ?", quotedCol, cond.operator), []any{cond.value}, true
	}
}

// applyWhere where
func (b *ScopeBuilder) applyWhere(db *gorm.DB, qL, qR string) *gorm.DB {
	// 抽离一个递归内部函数
	var buildCond func(d *gorm.DB, conds []condition) *gorm.DB
	buildCond = func(d *gorm.DB, conds []condition) *gorm.DB {
		for _, cond := range conds {
			// 如果是嵌套组
			if len(cond.group) > 0 {
				// GORM v2 分组条件需传入 *gorm.DB，不支持 func(*gorm.DB)*gorm.DB 签名
				subDb := d.Session(&gorm.Session{NewDB: true})
				subDb = buildCond(subDb, cond.group)
				if cond.isOr {
					d = d.Or(subDb)
				} else {
					d = d.Where(subDb)
				}
				continue
			}

			clauseStr := cond.expr
			if clauseStr == "" {
				continue
			}

			// ---子查询核心逻辑 ---
			// 检查 cond.value 是否为 *gorm.DB 类型 (即子查询对象)
			if subQuery, ok := cond.value.(*gorm.DB); ok {
				quotedCol := quoteColumn(cond.expr, qL, qR)
				// 生成类似于: `dept_id` IN (?) 的 SQL，GORM 会自动把 ? 替换为子查询 SQL
				sqlStr := fmt.Sprintf("%s %s (?)", quotedCol, cond.operator)

				if cond.isOr {
					d = d.Or(sqlStr, subQuery)
				} else {
					d = d.Where(sqlStr, subQuery)
				}
				continue
			}

			// 叶子条件：isRaw / BETWEEN / IS NULL / 标准操作
			sqlStr, leafArgs, leafOK := buildLeafSQL(cond, qL, qR)
			if !leafOK {
				continue
			}
			if cond.isOr {
				d = d.Or(sqlStr, leafArgs...)
			} else {
				d = d.Where(sqlStr, leafArgs...)
			}
		}
		return d
	}
	return buildCond(db, b.conditions)
}

// applyJoins join
func (b *ScopeBuilder) applyJoins(db *gorm.DB) *gorm.DB {
	for _, j := range b.joins {
		var query string
		if j.on != "" {
			// 标准连接：METHOD TABLE ON CONDITION
			query = fmt.Sprintf("%s %s ON %s", j.method, j.table, j.on)
		} else {
			// 无条件连接（如 Cross Join / Natural Join）
			query = fmt.Sprintf("%s %s", j.method, j.table)
		}
		if len(j.args) > 0 {
			db = db.Joins(query, j.args...)
		} else {
			db = db.Joins(query)
		}
	}
	return db
}

// applyGroupHaving 应用 Group By 和 Having 条件
func (b *ScopeBuilder) applyGroupHaving(db *gorm.DB, qL, qR string) *gorm.DB {
	// 1. 处理 Group By
	for _, group := range b.groups {
		// db.Group() 内部会用方言引号转义，直接传原始列名即可
		db = db.Group(group)
	}

	// 2. 处理 Having
	// GORM 的 Having() 不接受 scope 函数，Or() 会追加到 WHERE 而非 HAVING。
	// 用 clause 表达式树构建：isOr=true 与前一项合并为 clause.Or，isOr=false 追加为新 AND 项。
	var buildHavingExprs func(conds []condition) []clause.Expression
	buildHavingExprs = func(conds []condition) []clause.Expression {
		var result []clause.Expression
		for _, cond := range conds {
			var expr clause.Expression

			if len(cond.group) > 0 {
				// 嵌套括号：递归构建子表达式，多项用 clause.And 合并
				subExprs := buildHavingExprs(cond.group)
				if len(subExprs) == 0 {
					continue
				}
				if len(subExprs) == 1 {
					expr = subExprs[0]
				} else {
					expr = clause.And(subExprs...)
				}
			} else {
				if cond.expr == "" {
					continue
				}
				sqlStr, leafArgs, leafOK := buildLeafSQL(cond, qL, qR)
				if !leafOK {
					continue
				}
				expr = clause.Expr{SQL: sqlStr, Vars: leafArgs}
			}

			// isOr=true：与前一项合并为 OR；否则追加为独立 AND 项
			if cond.isOr && len(result) > 0 {
				prev := result[len(result)-1]
				result[len(result)-1] = clause.Or(prev, expr)
			} else {
				result = append(result, expr)
			}
		}
		return result
	}

	if len(b.havings) > 0 {
		for _, expr := range buildHavingExprs(b.havings) {
			db = db.Having(expr)
		}
	}

	return db
}

// applyOrderLimit order limit
func (b *ScopeBuilder) applyOrderLimit(db *gorm.DB, qL, qR string) *gorm.DB {
	for _, item := range b.orders {
		if item.isRaw {
			// 原生表达式：不经转义直接传入
			db = db.Order(item.expr)
			continue
		}
		// 普通字段：转义列名，保留 ASC/DESC
		parts := strings.Split(strings.TrimSpace(item.expr), " ")
		if len(parts) >= 1 {
			col := quoteColumn(parts[0], qL, qR)
			newOrder := col
			if len(parts) > 1 {
				newOrder += " " + parts[1]
			}
			db = db.Order(newOrder)
		}
	}
	if b.limit > 0 {
		db = db.Limit(b.limit)
	}
	if b.offset > 0 {
		db = db.Offset(b.offset)
	}
	return db
}

// applyStrength apply strength 悲观锁
func (b *ScopeBuilder) applyStrength(db *gorm.DB) *gorm.DB {
	// 应用悲观锁
	if b.lockStrength != "" {
		db = db.Clauses(clause.Locking{
			Strength: b.lockStrength,
			Options:  b.lockOptions,
		})
	}
	return db
}

// applyPreloads apply preloads 预加载
func (b *ScopeBuilder) applyPreloads(db *gorm.DB) *gorm.DB {
	// 应用预加载
	for _, p := range b.preloads {
		if p.query == "" {
			continue
		}
		db = db.Preload(p.query, p.args...)
	}
	return db
}

// 批量转义辅助函数
func quoteColumns(cols []string, qL, qR string) []string {
	if len(cols) == 0 {
		return cols
	}
	newCols := make([]string, len(cols))
	for i, col := range cols {
		newCols[i] = quoteColumn(col, qL, qR)
	}
	return newCols
}

// quoteColumn 深度处理列名转义，适配表名、别名及复杂表达式
func quoteColumn(col string, qL, qR string) string {
	col = strings.TrimSpace(col)
	if col == "" {
		return ""
	}

	// 1. 已经转义过，直接返回（qL 为空时跳过，避免 HasPrefix("") 永远匹配）
	if qL != "" && strings.HasPrefix(col, qL) {
		return col
	}

	// 2. 处理带有别名的情况（须先于复杂表达式检查，否则空格会被误判为复杂表达式）
	upperCol := strings.ToUpper(col)
	if idx := strings.Index(upperCol, " AS "); idx != -1 {
		left := quoteColumn(col[:idx], qL, qR)
		right := quoteColumn(col[idx+4:], qL, qR)
		return left + " AS " + right
	}

	// 3. 特殊处理 table.* 形式（* 在 ContainsAny 中会被误判为复杂表达式）
	if strings.HasSuffix(col, ".*") {
		tablePart := col[:len(col)-2]
		if !strings.ContainsAny(tablePart, "()+-*/, ") {
			return quoteColumn(tablePart, qL, qR) + ".*"
		}
		return col
	}

	// 4. 包含函数调用、算术运算符等，视为复杂表达式，直接返回
	if strings.ContainsAny(col, "()+-*/, ") {
		return col
	}

	// 5. 处理带表名限定符的情况 (如 "users.name")
	if strings.Contains(col, ".") {
		parts := strings.Split(col, ".")
		for i, part := range parts {
			parts[i] = quoteColumn(part, qL, qR)
		}
		return strings.Join(parts, ".")
	}

	// 6. 标准字段转义
	return qL + col + qR
}
