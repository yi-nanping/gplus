// Package gplus — Query-chain-safe 投影查询 API
//
// 本文件提供 FindAs / FindOneAs / FindAsTx / FindOneAsTx 四个包级泛型函数，
// 让用户能写"投影查询走 GORM Query callback chain"，避开 db.Scan / db.Row / db.Rows
// 绕过 Query chain 的隐患（导致下游 isolation/审计 callback 不触发）。
//
// 详见 docs/superpowers/specs/2026-05-01-scan-callback-fix-design.md
package gplus

import "errors"

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
