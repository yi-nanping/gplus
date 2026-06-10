package gplus

import "errors"

// ErrExprEmpty 表示构造的表达式至少需要一个操作数（如 Add() 无参数）。
var ErrExprEmpty = errors.New("gplus: expression requires at least one operand")

// Expr 是投影表达式树的封闭接口，仅 colRef / litVal / addExpr 三种实现。
// 构造期不持有 *Query，列地址解析延到 SelectExpr 调用期完成。
type Expr interface {
	exprNode()
}

// colRef 引用一个结构体字段指针，列名在 SelectExpr 调用期经 resolveColumnNameAny 解析。
type colRef struct {
	ptr any
}

func (colRef) exprNode() {}

// litVal 持有一个字面量值，渲染期作为参数化绑定（? 占位），防注入。
type litVal struct {
	val any
}

func (litVal) exprNode() {}

// addExpr 表示变长加法表达式（全加法满足结合律，可安全扁平化）。
type addExpr struct {
	operands []Expr
}

func (addExpr) exprNode() {}

// Col 引用一个结构体字段指针（如 &model.Depth）作为投影列。
// 构造期仅存指针，列名解析在 SelectExpr 调用期完成。
func Col(fieldPtr any) Expr {
	return colRef{ptr: fieldPtr}
}

// Lit 持有一个字面量值，渲染期走参数化绑定（? 占位符），防 SQL 注入。
func Lit(val any) Expr {
	return litVal{val: val}
}

// Add 构造变长加法表达式（如 Add(Col(&a), Col(&b), Lit(1))）。
// 构造期仅存 operands；空操作数（Add()）在 SelectExpr 调用期检出 ErrExprEmpty。
func Add(operands ...Expr) Expr {
	return addExpr{operands: operands}
}
