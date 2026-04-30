package gplus

import "gorm.io/gorm"

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
