package gplus

import (
	"context"
	"reflect"

	"gorm.io/gorm"
)

// Subquerier 子查询契约。任意 *Query[X] 自动满足（X 可与外层 T 不同）。
// gplusSubquery() 私有方法限制接口只能由本包实现。
type Subquerier interface {
	// ToDB 返回可作为 GORM 子查询绑定参数的 *gorm.DB 对象
	ToDB(db *gorm.DB) *gorm.DB

	// GetError 返回构建过程累积的错误
	GetError() error

	gplusSubquery() // unexported guard: 阻止外部包冒名实现
}

// 编译期断言：*Query[T] 满足 Subquerier。
// 选 struct{} 作为占位 T 仅为验证方法集，gplusSubquery 不依赖 T，对任意 T 均成立。
var _ Subquerier = (*Query[struct{}])(nil)

// SubQuery 派生子查询。sub.outerQueryRef = outer，sub 主表 alias 自动设为表名（如 Order → "orders"）。
//
// 错误处理：
//   - outer == nil：返回带 ErrSubqueryOuterNil 累积错误的 dud sub（H4：与 errs 哲学一致，不 panic）
//
// sub.ctx 来自 outer.ctx（透传）。sub 不自动应用 outer 的 DataRule（保持 v0.6.0 既有语义）。
func SubQuery[X any](outer AnyQuery) (*Query[X], *X) {
	if outer == nil {
		// H4：返回带预置错误的 dud sub
		sub, x := NewQuery[X](context.Background())
		sub.gplusCore().appendErr(ErrSubqueryOuterNil)
		return sub, x
	}
	core := outer.gplusCore()
	ctx := core.context()
	// 自动以表名注册主表 alias（sub 默认主表 alias = 表名）
	tableName := aliasSchemaTableName(reflect.TypeOf((*X)(nil)).Elem())
	sub, x := NewQueryAs[X](ctx, tableName)
	sub.gplusCore().outerQueryRef = outer
	return sub, x
}

// SubQueryAs 派生子查询并指定主表 alias。
// 等价于 SubQuery + As(sub, alias)，但合并为单步以避免双初始化。
//
// 错误处理：
//   - outer == nil：返回带 ErrSubqueryOuterNil 累积错误的 dud sub（H4）
//   - alias 不合法：由 NewQueryAs 内部 As 校验逻辑累积 ErrAliasInvalidName
func SubQueryAs[X any](outer AnyQuery, alias string) (*Query[X], *X) {
	if outer == nil {
		// H4：返回带预置错误的 dud sub
		sub, x := NewQuery[X](context.Background())
		sub.gplusCore().appendErr(ErrSubqueryOuterNil)
		return sub, x
	}
	core := outer.gplusCore()
	ctx := core.context()
	// NewQueryAs 内部调用 As(q, alias)，完成主表 alias 注册和 addrLow/addrHigh 计算
	sub, x := NewQueryAs[X](ctx, alias)
	sub.gplusCore().outerQueryRef = outer
	return sub, x
}
