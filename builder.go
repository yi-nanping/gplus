package gplus

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// condition 内部结构，存储单个查询条件
type condition struct {
	column   string
	operator string // =, >, <, >=, <=, IN, LIKE, BETWEEN, EXISTS, NOT EXISTS, IS NULL, IS NOT NULL
	value    any
	isOr     bool // true 为 OR，false 为 AND
	isRaw    bool // 是否为原生 SQL
	// 用于存储嵌套的子条件块
	group []condition
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
	Column    string // 规则字段 (例如: "dept_id")
	Condition string // 规则条件 (例如: "=", "IN", "LIKE")
	Value     string // 规则值   (例如: "1001" 或 "1,2,3")
}

// preloadInfo 存储预加载信息
type preloadInfo struct {
	query string // 关联属性名（如 "Orders"）
	args  []any  // 过滤关联数据的额外条件
}

// 定义 Context 中使用的 Key 类型，防止命名冲突
type dataRuleKey struct{}

var DataRuleKey = dataRuleKey{}

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
	// orders 用于构建 Order 字段
	orders []string
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
}

// --- 内部私有组件，用于复用代码 ---

// getQuoteChar 自动探测数据库方言并设置转义符
func getQuoteChar(db *gorm.DB) (string, string) {
	if db.Dialector == nil {
		return "`", "`" // 默认 fallback 到 MySQL
	}
	switch db.Dialector.Name() {
	case "postgres", "sqlite":
		return "\"", "\""
	case "sqlserver":
		return "[", "]"
	default: // mysql, tidb, etc.
		return "`", "`"
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

// applyWhere where
func (b *ScopeBuilder) applyWhere(db *gorm.DB, qL, qR string) *gorm.DB {
	// 抽离一个递归内部函数
	var buildCond func(d *gorm.DB, conds []condition) *gorm.DB
	buildCond = func(d *gorm.DB, conds []condition) *gorm.DB {
		for _, cond := range conds {
			// 如果是嵌套组
			if len(cond.group) > 0 {
				// 显式捕获循环变量，防止 Go < 1.22 下闭包捕获到最后一次迭代值
				cond := cond
				// 递归闭包
				subQueryFunc := func(subDb *gorm.DB) *gorm.DB {
					return buildCond(subDb.Session(&gorm.Session{NewDB: true}), cond.group)
				}
				if cond.isOr {
					d = d.Or(subQueryFunc)
				} else {
					d = d.Where(subQueryFunc)
				}
				continue
			}

			clauseStr := cond.column
			if clauseStr == "" {
				continue
			}

			// ---子查询核心逻辑 ---
			// 检查 cond.value 是否为 *gorm.DB 类型 (即子查询对象)
			if subQuery, ok := cond.value.(*gorm.DB); ok {
				quotedCol := quoteColumn(cond.column, qL, qR)
				// 生成类似于: `dept_id` IN (?) 的 SQL，GORM 会自动把 ? 替换为子查询 SQL
				sqlStr := fmt.Sprintf("%s %s (?)", quotedCol, cond.operator)

				if cond.isOr {
					db = db.Or(sqlStr, subQuery)
				} else {
					db = db.Where(sqlStr, subQuery)
				}
				continue
			}

			// ------------- 原生SQL -------------
			// 安全检查：如果是 Raw SQL，必须标记为 isRaw
			if cond.isRaw {
				if cond.isOr {
					db = db.Or(cond.column, cond.value)
				} else {
					db = db.Where(cond.column, cond.value)
				}
				continue
			}

			// 特殊处理 BETWEEN 和 NOT BETWEEN
			// 必须生成 "col BETWEEN ? AND ?" 格式，并将参数切片展开
			if cond.operator == OpBetween || cond.operator == OpNotBetween {
				sqlStr := fmt.Sprintf("%s %s ? AND ?", quoteColumn(cond.column, qL, qR), cond.operator)

				// 断言 value 为切片 (Query.Between 传入的就是 []any)
				if args, ok := cond.value.([]any); ok && len(args) == 2 {
					if cond.isOr {
						db = db.Or(sqlStr, args[0], args[1])
					} else {
						db = db.Where(sqlStr, args[0], args[1])
					}
					continue // 处理完毕，跳过通用逻辑
				}
				// 如果参数不对，回退到通用逻辑或者报错，这里选择继续走通用逻辑防止Panic，但在Query层应该保证正确
			}

			// 特殊处理 IsNull / IsNotNull (不需要占位符 ?)
			if cond.operator == OpIsNull || cond.operator == OpIsNotNull {
				clauseStr = fmt.Sprintf("%s %s", quoteColumn(cond.column, qL, qR), cond.operator)
				if cond.isOr {
					db = db.Or(clauseStr)
				} else {
					db = db.Where(clauseStr)
				}
				continue
			}

			// 智能转义
			clauseStr = fmt.Sprintf("%s %s ?", quoteColumn(cond.column, qL, qR), cond.operator)
			if cond.isOr {
				db = db.Or(clauseStr, cond.value)
			} else {
				db = db.Where(clauseStr, cond.value)
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
		// Group 字段转义 (quoteColumn 已处理过函数识别，如 COUNT(id) 不会被错误转义)
		db = db.Group(quoteColumn(group, qL, qR))
	}

	// 2. 处理 Having (逻辑与 applyWhere 高度一致)
	// 定义递归函数以支持嵌套 Having
	var buildHaving func(d *gorm.DB, conds []condition) *gorm.DB
	buildHaving = func(d *gorm.DB, conds []condition) *gorm.DB {
		for _, cond := range conds {
			// --- A. 处理嵌套括号 (Group) ---
			if len(cond.group) > 0 {
				// 显式捕获循环变量，防止 Go < 1.22 下闭包捕获到最后一次迭代值
				cond := cond
				subQueryFunc := func(subDb *gorm.DB) *gorm.DB {
					// 注意：Having 的嵌套闭包同样需要 NewDB Session 隔离
					return buildHaving(subDb.Session(&gorm.Session{NewDB: true}), cond.group)
				}
				if cond.isOr {
					d = d.Or(subQueryFunc)
				} else {
					d = d.Having(subQueryFunc)
				}
				continue
			}

			// 如果没有 column 字段，则跳过
			if cond.column == "" {
				continue
			}

			// --- B. 处理原生 SQL ---
			if cond.isRaw {
				if cond.isOr {
					d = d.Or(cond.column, cond.value)
				} else {
					d = d.Having(cond.column, cond.value)
				}
				continue
			}

			// --- C. 处理特殊操作符 ---

			// C1. Between (双参数)
			if cond.operator == OpBetween || cond.operator == OpNotBetween {
				quotedCol := quoteColumn(cond.column, qL, qR)
				clause := fmt.Sprintf("%s %s ? AND ?", quotedCol, cond.operator)

				// 尝试解构 slice 参数
				if args, ok := cond.value.([]any); ok && len(args) == 2 {
					if cond.isOr {
						d = d.Or(clause, args[0], args[1])
					} else {
						d = d.Having(clause, args[0], args[1])
					}
					continue
				}
			}

			// C2. IsNull / IsNotNull (无参数)
			if cond.operator == OpIsNull || cond.operator == OpIsNotNull {
				quotedCol := quoteColumn(cond.column, qL, qR)
				clause := fmt.Sprintf("%s %s", quotedCol, cond.operator)
				if cond.isOr {
					d = d.Or(clause)
				} else {
					d = d.Having(clause)
				}
				continue
			}

			// --- D. 标准操作 (Eq, Gt, Lt, Like, In 等) ---
			quotedCol := quoteColumn(cond.column, qL, qR)
			clause := fmt.Sprintf("%s %s ?", quotedCol, cond.operator)

			if cond.isOr {
				d = d.Or(clause, cond.value)
			} else {
				d = d.Having(clause, cond.value)
			}
		}
		return d
	}

	// 执行构建
	if len(b.havings) > 0 {
		db = buildHaving(db, b.havings)
	}

	return db
}

// applyOrderLimit order limit
func (b *ScopeBuilder) applyOrderLimit(db *gorm.DB, qL, qR string) *gorm.DB {
	for _, order := range b.orders {
		// Order 字段转义
		parts := strings.Split(strings.TrimSpace(order), " ")
		if len(parts) >= 1 {
			col := quoteColumn(parts[0], qL, qR)
			// 重新拼装，例如 `created_at` DESC
			newOrder := col
			if len(parts) > 1 {
				newOrder += " " + parts[1] // DESC/ASC 保持原样
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

	// 1. 如果已经转义过，或者包含函数调用 (、算术运算符等，视为复杂表达式，直接返回
	if strings.HasPrefix(col, qL) {
		return col
	}

	// 增加对 + - * / 等符号的判断，防止转义 `a + b` 为 ``a + b``
	if strings.ContainsAny(col, "()+-*/, ") {
		return col
	}

	// 2. 处理带有别名的情况 (如 "name AS user_name" 或 "name user_name")
	// 统一处理大小写不敏感的 " AS "
	upperCol := strings.ToUpper(col)
	if idx := strings.Index(upperCol, " AS "); idx != -1 {
		left := quoteColumn(col[:idx], qL, qR)
		right := quoteColumn(col[idx+4:], qL, qR)
		return left + " AS " + right
	}

	// 3. 处理带表名限定符的情况 (如 "users.name")
	if strings.Contains(col, ".") {
		parts := strings.Split(col, ".")
		for i, part := range parts {
			parts[i] = quoteColumn(part, qL, qR)
		}
		return strings.Join(parts, ".")
	}

	// 4. 标准字段转义
	return qL + col + qR
}
