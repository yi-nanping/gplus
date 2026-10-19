package gplus

import "fmt"

// SelectAs 追加带结果别名的字段投影，来源列按字段指针及 column tag 解析。
// field 必须属于当前查询链或可见外层查询；不接受字符串列名。
// alias 为 1-32 个字母、数字或下划线，且不能以数字开头；构建时按方言转义。
func (q *Query[T]) SelectAs(field any, alias string) *Query[T] {
	if !aliasNameRegexp.MatchString(alias) {
		q.errs = append(q.errs, fmt.Errorf("gplus: SelectAs invalid result alias: %w", ErrAliasInvalidName))
		return q
	}
	column, err := q.resolveJoinField(field)
	if err != nil {
		q.errs = append(q.errs, fmt.Errorf("gplus: SelectAs invalid column pointer: %w", err))
		return q
	}
	q.selects = append(q.selects, selectItem{expr: column, alias: alias})
	return q
}

// EqColumn 添加两个字段之间的等值条件（AND），右侧生成列引用而非绑定值。
// 两个字段指针必须属于当前查询链或可见外层查询；不接受字符串列名。
// 同模型的相关子查询应使用不同 alias 实例区分内外字段。
func (q *Query[T]) EqColumn(left, right any) *Query[T] {
	leftColumn, err := q.resolveJoinField(left)
	if err != nil {
		q.errs = append(q.errs, fmt.Errorf("gplus: EqColumn invalid left column pointer: %w", err))
		return q
	}
	rightColumn, err := q.resolveJoinField(right)
	if err != nil {
		q.errs = append(q.errs, fmt.Errorf("gplus: EqColumn invalid right column pointer: %w", err))
		return q
	}
	q.conditions = append(q.conditions, condition{expr: leftColumn, operator: OpEq, rightColumn: rightColumn})
	return q
}
