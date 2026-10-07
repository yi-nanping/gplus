package gplus

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// OnBuilder 构造 JOIN 的额外 ON 条件；字段指针必须属于当前查询链。
// 条件只作用于 ON，不会自动为副表追加 DataRule 或软删除过滤。
type OnBuilder struct {
	resolve    func(any) (string, error)
	conditions []condition
	errs       []error
}

// Eq 添加等值条件，值通过参数绑定传入。
func (b *OnBuilder) Eq(field, value any) *OnBuilder { return b.add(field, OpEq, value) }

// In 添加集合条件，values 必须是切片或数组。
func (b *OnBuilder) In(field, values any) *OnBuilder {
	v := reflect.ValueOf(values)
	if !v.IsValid() || (v.Kind() != reflect.Slice && v.Kind() != reflect.Array) {
		b.errs = append(b.errs, errors.New("gplus: ON In requires a slice or array"))
		return b
	}
	return b.add(field, OpIn, values)
}

// IsNull 添加 NULL 条件。
func (b *OnBuilder) IsNull(field any) *OnBuilder { return b.add(field, OpIsNull, nil) }

// IsNotNull 添加非 NULL 条件。
func (b *OnBuilder) IsNotNull(field any) *OnBuilder { return b.add(field, OpIsNotNull, nil) }

func (b *OnBuilder) add(field any, op string, value any) *OnBuilder {
	if !isOnFieldPointer(field) {
		b.errs = append(b.errs, ErrInvalidPointer)
		return b
	}
	col, err := b.resolve(field)
	if err != nil {
		b.errs = append(b.errs, err)
		return b
	}
	b.conditions = append(b.conditions, condition{expr: col, operator: op, value: value})
	return b
}

func isOnFieldPointer(field any) bool {
	v := reflect.ValueOf(field)
	return v.IsValid() && v.Kind() == reflect.Pointer && !v.IsNil()
}

// And 添加带括号的 AND 条件组。
func (b *OnBuilder) And(fn func(*OnBuilder)) *OnBuilder { return b.group(false, fn) }

// Or 添加带括号的 OR 条件组。
func (b *OnBuilder) Or(fn func(*OnBuilder)) *OnBuilder { return b.group(true, fn) }

func (b *OnBuilder) group(isOr bool, fn func(*OnBuilder)) *OnBuilder {
	if fn == nil {
		b.errs = append(b.errs, errors.New("gplus: ON group callback cannot be nil"))
		return b
	}
	sub := &OnBuilder{resolve: b.resolve}
	fn(sub)
	b.errs = append(b.errs, sub.errs...)
	if len(sub.conditions) > 0 {
		b.conditions = append(b.conditions, condition{group: sub.conditions, isOr: isOr})
	}
	return b
}

// LeftJoinAsOn 添加 LEFT JOIN，fn 构造额外 ON 条件并保留未匹配的主表行。
// fn 不可为 nil；需要纯列等值连接时使用 LeftJoinAs。
func (q *Query[T]) LeftJoinAsOn(alias, leftCol, rightCol any, fn func(*OnBuilder)) *Query[T] {
	return q.appendJoinOn("LEFT JOIN", alias, leftCol, rightCol, fn)
}

// InnerJoinAsOn 添加 INNER JOIN，fn 构造额外 ON 条件。
func (q *Query[T]) InnerJoinAsOn(alias, leftCol, rightCol any, fn func(*OnBuilder)) *Query[T] {
	return q.appendJoinOn("INNER JOIN", alias, leftCol, rightCol, fn)
}

func (q *Query[T]) appendJoinOn(method string, alias, leftCol, rightCol any, fn func(*OnBuilder)) *Query[T] {
	if fn == nil {
		q.errs = append(q.errs, errors.New("gplus: JOIN ON callback cannot be nil"))
		return q
	}
	v := reflect.ValueOf(alias)
	if !isOnFieldPointer(alias) || v.Elem().Kind() != reflect.Struct {
		q.errs = append(q.errs, ErrAliasNotInChain)
		return q
	}
	name, typ, ok := q.lookupAliasFromChain(alias)
	if !ok {
		q.errs = append(q.errs, ErrAliasNotInChain)
		return q
	}
	if !isOnFieldPointer(leftCol) || !isOnFieldPointer(rightCol) {
		q.errs = append(q.errs, ErrInvalidPointer)
		return q
	}
	left, err := q.resolveJoinField(leftCol)
	if err != nil {
		q.errs = append(q.errs, err)
		return q
	}
	right, err := q.resolveJoinField(rightCol)
	if err != nil {
		q.errs = append(q.errs, err)
		return q
	}
	table := aliasSchemaTableName(typ)
	if !validTableName.MatchString(table) {
		q.errs = append(q.errs, fmt.Errorf("gplus: invalid JOIN table %q", table))
		return q
	}
	on := &OnBuilder{resolve: q.resolveJoinField}
	fn(on)
	if len(on.errs) > 0 {
		q.errs = append(q.errs, on.errs...)
		return q
	}
	q.joins = append(q.joins, joinInfo{method: method, table: table, aliasName: name,
		leftColumn: left, rightColumn: right, onConditions: on.conditions})
	return q
}

// 新 ON 入口只接受当前链中的 alias 或主模型字段，避免全局缓存重绑其他模型。
func (q *Query[T]) resolveJoinField(field any) (string, error) {
	if !isOnFieldPointer(field) {
		return "", ErrInvalidPointer
	}
	addr := reflect.ValueOf(field).Pointer()
	for current := AnyQuery(q); current != nil; current = current.gplusCore().outerQueryRef {
		core := current.gplusCore()
		if alias, col, ok := core.lookupAddr(addr); ok {
			return alias + "." + col, nil
		}
		if core.hadRevokedHit(addr) {
			return "", ErrAliasRevoked
		}
		if owner, ok := current.(interface{ joinModelColumn(uintptr) (string, bool) }); ok {
			if col, found := owner.joinModelColumn(addr); found {
				return col, nil
			}
		}
	}
	return "", ErrFieldAddrUnregistered
}

func (q *Query[T]) joinModelColumn(addr uintptr) (string, bool) {
	if q.mainAlias != "" {
		return "", false
	}
	table := q.tableName
	if table == "" {
		table = q.mainTableName()
	}
	return canonicalJoinColumn[T](addr, table)
}

func (u *Updater[T]) joinModelColumn(addr uintptr) (string, bool) {
	table := u.tableName
	if table == "" {
		table = u.mainTableName()
	}
	return canonicalJoinColumn[T](addr, table)
}

func canonicalJoinColumn[T any](addr uintptr, table string) (string, bool) {
	if !ownsJoinField(reflect.ValueOf(getModelInstance[T]()).Elem(), addr) {
		return "", false
	}
	col, ok := columnNameCache.Load(addr)
	if !ok {
		return "", false
	}
	return table + "." + col.(string), true
}

func ownsJoinField(model reflect.Value, addr uintptr) bool {
	base := model.Addr().Pointer()
	for offset := range reflectStructSchema(model.Interface(), "gorm", "COLUMN") {
		if base+offset == addr {
			return true
		}
	}
	// 命名、匿名及值嵌入内的指针字段均复用注册时的遍历路径。
	owned := false
	walkPtrEmbedFields(model, "gorm", "", func(inner reflect.Value, _ string) {
		base := inner.Addr().Pointer()
		for offset := range reflectStructSchema(inner.Interface(), "gorm", "COLUMN") {
			if base+offset == addr {
				owned = true
				return
			}
		}
	})
	return owned
}

func renderJoinOn(conditions []condition, qL, qR string) (string, []any) {
	var parts []string
	var args []any
	for _, cond := range conditions {
		var sql string
		var values []any
		if len(cond.group) > 0 {
			sql, values = renderJoinOn(cond.group, qL, qR)
			sql = "(" + sql + ")"
		} else {
			sql, values, _ = buildLeafSQL(cond, qL, qR)
		}
		if len(parts) > 0 {
			if cond.isOr {
				parts = append(parts, "OR")
			} else {
				parts = append(parts, "AND")
			}
		}
		parts = append(parts, sql)
		args = append(args, values...)
	}
	return strings.Join(parts, " "), args
}
