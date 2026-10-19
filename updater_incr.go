package gplus

import (
	"fmt"
	"reflect"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Incr 为主模型字段构建原子自增赋值，由 UpdateByCond/UpdateByCondTx 执行。
// field 必须是 NewUpdater 返回的模型或 Model[T]() 上的字段指针；不接受字符串、
// 独立模型实例、其他模型或 alias 字段。重复设置同一列时，最后一次赋值生效。
func (u *Updater[T]) Incr(field any, delta int64) *Updater[T] {
	fieldValue := reflect.ValueOf(field)
	if !fieldValue.IsValid() || fieldValue.Kind() != reflect.Pointer || fieldValue.IsNil() {
		u.errs = append(u.errs, fmt.Errorf("gplus: Incr invalid main model field pointer: %w", ErrFieldAddrUnregistered))
		return u
	}
	addr := fieldValue.Pointer()
	model := reflect.ValueOf(getModelInstance[T]()).Elem()
	if !ownsJoinField(model, addr) {
		u.errs = append(u.errs, fmt.Errorf("gplus: Incr invalid main model field pointer: %w", ErrFieldAddrUnregistered))
		return u
	}

	column, ok := columnNameCache.Load(addr)
	if !ok {
		u.errs = append(u.errs, fmt.Errorf("gplus: Incr invalid main model field pointer: %w", ErrFieldAddrUnregistered))
		return u
	}
	columnName := column.(string)
	u.setMap[columnName] = gorm.Expr("? + ?", clause.Column{Name: columnName}, delta)
	return u
}
