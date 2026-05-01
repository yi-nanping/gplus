// Package gplus — Query-chain-safe 投影查询 API
//
// 本文件提供 FindAs / FindOneAs / FindAsTx / FindOneAsTx 四个包级泛型函数，
// 让用户能写"投影查询走 GORM Query callback chain"，避开 db.Scan / db.Row / db.Rows
// 绕过 Query chain 的隐患（导致下游 isolation/审计 callback 不触发）。
//
// 详见 docs/superpowers/specs/2026-05-01-scan-callback-fix-design.md
package gplus

import (
	"errors"

	"gorm.io/gorm"
)

// aggregateAlias 是 aggregate 函数生成 SQL 时的列 alias 名。
// 加 gplus_ 前缀以避免与业务表真实列名冲突（实测过 v / __agg__ 等短命名易撞）。
const aggregateAlias = "gplus_agg_v"

// aggregateWrap 包裹聚合结果列；用 Find 路径走 Query callback chain；
// V *R 保证空表 SUM 返回 NULL 时为 nil（不报 "converting NULL to int64" 错误）。
//
// 故意 unexported：内部实现细节，不入公开 API 表面。未来若加用户层
// 聚合表达式 API，新增独立的公开类型（如 AggregateValue[R]）。
type aggregateWrap[R any] struct {
	V *R `gorm:"column:gplus_agg_v"`
}

// ErrFindOneAsConflict 表示 FindOneAs 与 q.Limit() / q.Page() 组合调用。
// 内部 First 会追加 LIMIT 1，与已有 LIMIT 叠加，部分 DB 行为未定义。
var ErrFindOneAsConflict = errors.New("gplus: FindOneAs 不可与 q.Limit() / q.Page() 组合调用")

// FindAs 投影查询（多行）。dest 必须是 *[]Element 切片指针。
//
// 走 GORM Query callback chain，下游挂在 Query chain 上的隔离/审计 callback 会触发。
//
// 【迁移提示】若现有代码用 q.ToDB(db).Model(&T{}).Scan(&rows) / .Rows() / .Row()，
// 必须改用 gplus.FindAs。前者绕过 Query callback chain，会导致下游隔离/审计 callback
// 不触发，可能引发跨租户数据泄露 / 审计日志缺失（详见 README "已知陷阱"）。
//
// 【副作用】调用 FindAs 后 q.conditions 会被永久追加 DataRule 条件
// （dataRuleApplied 保护幂等），q 不应再跨不同 ctx 复用。与 List/Sum 等行为一致。
//
// 【调用形态】Go 1.18+ 类型推导后无需写类型参数：
//
//	var rows []UserVO
//	err := gplus.FindAs(repo, q, &rows)
func FindAs[T any, Dest any, D comparable](
	r *Repository[D, T], q *Query[T], dest *[]Dest,
) error {
	return FindAsTx[T, Dest, D](r, q, dest, nil)
}

// FindAsTx 支持事务的 FindAs。tx 为 nil 时与 FindAs 等价。
func FindAsTx[T any, Dest any, D comparable](
	r *Repository[D, T], q *Query[T], dest *[]Dest, tx *gorm.DB,
) error {
	if q == nil {
		return ErrQueryNil
	}
	if err := q.GetError(); err != nil {
		return err
	}
	if err := q.DataRuleBuilder().GetError(); err != nil {
		return err
	}
	return r.dbResolver(q.Context(), tx).
		Model(new(T)).Scopes(q.BuildQuery()).Find(dest).Error
}

// FindOneAs 投影查询（单行）。dest 是 *Element。
//
// 无匹配时返回 gorm.ErrRecordNotFound（与 GetById 既有语义一致）。
//
// 走 GORM Query callback chain，下游挂在 Query chain 上的隔离/审计 callback 会触发。
//
// 【迁移提示】若现有代码用 q.ToDB(db).Model(&T{}).Limit(1).Scan(&one)，必须改用
// gplus.FindOneAs。前者绕过 Query chain，可能引发跨租户数据泄露 / 审计日志缺失。
//
// 【约束】FindOneAs 不可与 q.Limit() / q.Page() 组合 —— 内部 First 会追加 LIMIT 1，
// 与已有 LIMIT 叠加部分 DB 行为未定义。组合调用会立即返回 ErrFindOneAsConflict。
//
// 【实测确认】GORM v1.31.x First(dest) 不会用 dest 的 schema 覆盖已设置的
// Model(new(T))；下游 isolation callback 拿到的 Schema.Table 仍为 T 表名。
// （由 TestGORMCallbackBehaviorProbe 永久守护）
func FindOneAs[T any, Dest any, D comparable](
	r *Repository[D, T], q *Query[T], dest *Dest,
) error {
	return FindOneAsTx[T, Dest, D](r, q, dest, nil)
}

// FindOneAsTx 支持事务的 FindOneAs。
func FindOneAsTx[T any, Dest any, D comparable](
	r *Repository[D, T], q *Query[T], dest *Dest, tx *gorm.DB,
) error {
	if q == nil {
		return ErrQueryNil
	}
	// 编程式防御：同 q 已设 limit/offset 时拒绝
	if q.ScopeBuilder.limit > 0 || q.ScopeBuilder.offset > 0 {
		return ErrFindOneAsConflict
	}
	if err := q.GetError(); err != nil {
		return err
	}
	if err := q.DataRuleBuilder().GetError(); err != nil {
		return err
	}
	return r.dbResolver(q.Context(), tx).
		Model(new(T)).Scopes(q.BuildQuery()).First(dest).Error
}
