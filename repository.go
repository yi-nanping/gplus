package gplus

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

var (
	ErrQueryNil          = errors.New("gplus: query cannot be nil")
	ErrRawSQLEmpty       = errors.New("gplus: raw sql cannot be empty")
	ErrDeleteEmpty       = errors.New("gplus: delete content is empty")
	ErrUpdateEmpty       = errors.New("gplus: update content is empty")
	ErrUpdateNoCondition = errors.New("gplus: update requires at least one condition to prevent full-table update")
	ErrTransactionReq    = errors.New("gplus: locking query must be executed within a transaction")
)

// IsNotFound 判断错误是否为「记录不存在」
func IsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

// Repository 泛型仓储，提供标准 CRUD
// D: ID类型 (int, string, etc.), T: 实体类型
type Repository[D comparable, T any] struct {
	db *gorm.DB
}


// NewRepository 构造函数
func NewRepository[D comparable, T any](db *gorm.DB) *Repository[D, T] {
	return &Repository[D, T]{db: db}
}

// dbResolver 现在的逻辑变得很简单：如果有 tx 用 tx，否则用结构体里的 db
func (r *Repository[D, T]) dbResolver(ctx context.Context, tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx.WithContext(ctx)
	}
	return r.db.WithContext(ctx)
}

// WithTx 返回一个新的 Repository 实例，该实例绑定了传入的事务对象
// 这是一个轻量级的浅拷贝，性能消耗极小
func (r *Repository[D, T]) WithTx(tx *gorm.DB) *Repository[D, T] {
	return &Repository[D, T]{
		db: tx, // 新实例使用事务连接
	}
}

// Transaction 封装 GORM 的事务闭包模式
// fn: 事务内的业务逻辑，如果返回 error，事务会自动回滚
func (r *Repository[D, T]) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(fn)
}

// GetDB 获取当前 Repository 绑定的 DB 实例
// 注意：请勿通过此方法修改 db.Config 或关闭连接
func (r *Repository[D, T]) GetDB() *gorm.DB {
	return r.db
}

// GetById 根据主键查询
func (r *Repository[D, T]) GetById(ctx context.Context, id D) (T, error) {
	return r.GetByIdTx(ctx, id, nil)
}

// GetByIdTx 支持事务的查询
func (r *Repository[D, T]) GetByIdTx(ctx context.Context, id D, tx *gorm.DB) (data T, err error) {
	err = r.dbResolver(ctx, tx).First(&data, id).Error
	return
}

// GetOne 根据条件查询单条
func (r *Repository[D, T]) GetOne(q *Query[T]) (data T, err error) {
	return r.GetOneTx(q, nil)
}

// GetOneTx 支持事务的单条查询
func (r *Repository[D, T]) GetOneTx(q *Query[T], tx *gorm.DB) (data T, err error) {
	if q == nil {
		return data, ErrQueryNil
	}
	if err = q.GetError(); err != nil {
		return data, err
	}
	if err = q.DataRuleBuilder().GetError(); err != nil {
		return data, err
	}
	db := r.dbResolver(q.Context(), tx)
	err = db.Scopes(q.BuildQuery()).First(&data).Error
	return
}

// List 根据条件查询列表
func (r *Repository[D, T]) List(q *Query[T]) (data []T, err error) {
	return r.ListTx(q, nil)
}

// ListTx 支持事务的列表查询
func (r *Repository[D, T]) ListTx(q *Query[T], tx *gorm.DB) (data []T, err error) {
	if q == nil {
		return nil, ErrQueryNil
	}
	if err = q.GetError(); err != nil {
		return data, err
	}
	if err = q.DataRuleBuilder().GetError(); err != nil {
		return data, err
	}
	db := r.dbResolver(q.Context(), tx)
	err = db.Scopes(q.BuildQuery()).Find(&data).Error
	return
}

// Pluck 提取单列值到泛型切片中
func Pluck[T any, R any, D comparable](r *Repository[D, T], q *Query[T], col any) ([]R, error) {
	return PluckTx[T, R, D](r, q, col, nil)
}

// PluckTx 在指定事务中提取单列值到泛型切片中
func PluckTx[T any, R any, D comparable](r *Repository[D, T], q *Query[T], col any, tx *gorm.DB) ([]R, error) {
	if q == nil {
		return nil, ErrQueryNil
	}
	if err := q.GetError(); err != nil {
		return nil, err
	}
	var result []R
	colName, err := resolveColumnName(col)
	if err != nil {
		return nil, err
	}
	if err = q.DataRuleBuilder().GetError(); err != nil {
		return nil, err
	}
	err = r.dbResolver(q.Context(), tx).Model(new(T)).Scopes(q.BuildQuery()).Pluck(colName, &result).Error
	return result, err
}

// Page 分页查询
func (r *Repository[D, T]) Page(q *Query[T], skipCount bool) (data []T, total int64, err error) {
	return r.PageTx(q, skipCount, nil)
}

// PageTx 支持事务的分页查询
func (r *Repository[D, T]) PageTx(q *Query[T], skipCount bool, tx *gorm.DB) (data []T, total int64, err error) {
	if q == nil {
		return nil, 0, ErrQueryNil
	}
	if err = q.GetError(); err != nil {
		return nil, 0, err
	}
	if err = q.DataRuleBuilder().GetError(); err != nil {
		return nil, 0, err
	}
	baseDb := r.dbResolver(q.Context(), tx).Model(new(T))

	if !skipCount {
		err = baseDb.Session(&gorm.Session{}).
			Scopes(q.BuildCount()).
			Count(&total).Error
		if err != nil || total == 0 {
			return nil, total, err
		}
	}

	err = baseDb.Session(&gorm.Session{}).
		Scopes(q.BuildQuery()).
		Find(&data).Error

	return data, total, err
}

// Count 统计数量
func (r *Repository[D, T]) Count(q *Query[T]) (int64, error) {
	return r.CountTx(q, nil)
}

// CountTx 支持事务的统计查询
func (r *Repository[D, T]) CountTx(q *Query[T], tx *gorm.DB) (int64, error) {
	if q == nil {
		return 0, ErrQueryNil
	}
	if err := q.GetError(); err != nil {
		return 0, err
	}
	if err := q.DataRuleBuilder().GetError(); err != nil {
		return 0, err
	}
	var count int64
	err := r.dbResolver(q.Context(), tx).Model(new(T)).Scopes(q.BuildCount()).Count(&count).Error
	return count, err
}

// Save 纯 INSERT（非 upsert）。
// 警告：无论 entity 是否携带主键，均执行 INSERT，不会更新已有记录。
// 若需 insert-or-update 语义，请使用 Upsert。
func (r *Repository[D, T]) Save(ctx context.Context, entity *T) error {
	return r.SaveTx(ctx, entity, nil)
}

// SaveTx 事务纯 INSERT（非 upsert）。
func (r *Repository[D, T]) SaveTx(ctx context.Context, entity *T, tx *gorm.DB) error {
	return r.dbResolver(ctx, tx).Create(entity).Error
}

// SaveBatch 批量纯 INSERT（一次性，适合小批量数据）。
// 警告：底层调用 GORM Create，执行纯插入而非 upsert。
// 大批量数据请使用 CreateBatch 以控制每批插入数量。
func (r *Repository[D, T]) SaveBatch(ctx context.Context, entities []T) error {
	return r.SaveBatchTx(ctx, entities, nil)
}

// SaveBatchTx 事务批量纯 INSERT（一次性，适合小批量数据）。
func (r *Repository[D, T]) SaveBatchTx(ctx context.Context, entities []T, tx *gorm.DB) error {
	return r.dbResolver(ctx, tx).Create(&entities).Error
}

// Upsert 保存或更新单条记录（insert-or-update）。
// 底层调用 GORM db.Save()：有主键时执行 UPDATE 全字段，无主键时执行 INSERT。
// 注意：UPDATE 会覆盖所有字段（包括零值），如需只更新部分字段请使用 UpdateById/UpdateByCond。
func (r *Repository[D, T]) Upsert(ctx context.Context, entity *T) error {
	return r.UpsertTx(ctx, entity, nil)
}

// UpsertTx 事务保存或更新单条记录（insert-or-update）。
func (r *Repository[D, T]) UpsertTx(ctx context.Context, entity *T, tx *gorm.DB) error {
	return r.dbResolver(ctx, tx).Save(entity).Error
}

// UpsertBatch 批量保存或更新（insert-or-update，一次性执行）。
// 底层调用 GORM db.Save()，每条记录按主键决定 INSERT 或 UPDATE。
func (r *Repository[D, T]) UpsertBatch(ctx context.Context, entities []T) error {
	return r.UpsertBatchTx(ctx, entities, nil)
}

// UpsertBatchTx 事务批量保存或更新（insert-or-update）。
func (r *Repository[D, T]) UpsertBatchTx(ctx context.Context, entities []T, tx *gorm.DB) error {
	return r.dbResolver(ctx, tx).Save(&entities).Error
}

// CreateBatch 批量插入（分批执行，适合大批量数据）。
// batchSize 控制每批插入的记录数，防止超出数据库单次 SQL 参数限制。
func (r *Repository[D, T]) CreateBatch(ctx context.Context, entities []*T, batchSize int) error {
	return r.CreateBatchTx(ctx, entities, batchSize, nil)
}

// CreateBatchTx 事务批量插入（分批执行，适合大批量数据）。
func (r *Repository[D, T]) CreateBatchTx(ctx context.Context, entities []*T, batchSize int, tx *gorm.DB) error {
	if batchSize <= 0 {
		return fmt.Errorf("gplus: batchSize must be greater than 0, got %d", batchSize)
	}
	return r.dbResolver(ctx, tx).CreateInBatches(entities, batchSize).Error
}

// UpdateById 根据 ID 更新
func (r *Repository[D, T]) UpdateById(ctx context.Context, entity *T) error {
	return r.UpdateByIdTx(ctx, entity, nil)
}

// UpdateByIdTx 事务更新
func (r *Repository[D, T]) UpdateByIdTx(ctx context.Context, entity *T, tx *gorm.DB) error {
	return r.dbResolver(ctx, tx).Model(entity).Updates(entity).Error
}

// GetByLock 专门的带锁查询方法
// 强制要求传入 tx，因为不在事务里的锁是没有意义的
func (r *Repository[D, T]) GetByLock(q *Query[T], tx *gorm.DB) (*T, error) {
	if tx == nil {
		// 也可以选择自动开启事务，但最好强制要求外部控制事务范围
		return nil, ErrTransactionReq
	}
	if q == nil {
		return nil, ErrQueryNil
	}
	if err := q.GetError(); err != nil {
		return nil, err
	}
	// 确保 Query 开启了锁
	if q.lockStrength == "" {
		q.LockWrite() // 默认给加上写锁
	}

	if err := q.DataRuleBuilder().GetError(); err != nil {
		return nil, err
	}
	var entity T
	// 这里的 q.BuildQuery() 已经包含了 FOR UPDATE
	err := tx.WithContext(q.Context()).Scopes(q.BuildQuery()).First(&entity).Error
	if err != nil {
		return nil, err
	}
	return &entity, nil
}

// UpdateByCond 执行条件更新（不带事务）
func (r *Repository[D, T]) UpdateByCond(u *Updater[T]) (int64, error) {
	return r.UpdateByCondTx(u, nil)
}

// UpdateByCondTx 执行条件更新（支持事务）
func (r *Repository[D, T]) UpdateByCondTx(u *Updater[T], tx *gorm.DB) (int64, error) {
	if u == nil || u.IsEmpty() {
		return 0, ErrUpdateEmpty
	}
	// GetError() 须先于条件数量检查：若 addCond 因 resolveColumnName 失败，
	// 错误写入 u.errs 而非 u.conditions，此时应返回实际 builder 错误而非 ErrUpdateNoCondition
	if err := u.GetError(); err != nil {
		return 0, err
	}
	if err := u.DataRuleBuilder().GetError(); err != nil {
		return 0, err
	}
	if len(u.conditions) == 0 {
		return 0, ErrUpdateNoCondition
	}
	var model T
	// 1. 初始化 DB 并绑定上下文
	db := r.dbResolver(u.Context(), tx).Model(&model)

	// 2. 应用 ScopeBuilder (包含 Unscoped, Where 条件, Joins等)
	db = db.Scopes(u.BuildUpdate())

	// 3. 执行最终更新
	result := db.Updates(u.setMap)
	return result.RowsAffected, result.Error
}

// DeleteById 根据 ID 删除
func (r *Repository[D, T]) DeleteById(ctx context.Context, id D) (int64, error) {
	return r.DeleteByIdTx(ctx, id, nil)
}

// DeleteByIdTx 事务删除
func (r *Repository[D, T]) DeleteByIdTx(ctx context.Context, id D, tx *gorm.DB) (int64, error) {
	db := r.dbResolver(ctx, tx).Delete(new(T), id)
	return db.RowsAffected, db.Error
}

// DeleteByCond 根据条件删除
func (r *Repository[D, T]) DeleteByCond(q *Query[T]) (int64, error) {
	return r.DeleteByCondTx(q, nil)
}

// DeleteByCondTx 事务根据条件删除
func (r *Repository[D, T]) DeleteByCondTx(q *Query[T], tx *gorm.DB) (int64, error) {
	var model T
	// 无论是否设置 Unscoped，空条件一律拒绝执行，防止物理全表删除。
	// 如需全表物理删除，请使用 RawExec 显式执行 DELETE FROM table。
	if q == nil || q.IsEmpty() {
		return 0, ErrDeleteEmpty
	}
	if err := q.GetError(); err != nil {
		return 0, err
	}
	if err := q.DataRuleBuilder().GetError(); err != nil {
		return 0, err
	}
	db := r.dbResolver(q.Context(), tx).Model(&model).
		Scopes(q.BuildDelete()).
		Delete(&model)
	return db.RowsAffected, db.Error
}

// --- 原生 SQL 封装部分 ---

// RawQuery 执行原生查询 SQL，并将结果映射到当前 Repository 的实体切片中
// 适用场景：复杂的 JOIN 查询或存储过程
func (r *Repository[D, T]) RawQuery(ctx context.Context, sql string, args ...any) ([]T, error) {
	return r.RawQueryTx(ctx, nil, sql, args...)
}

// RawExec 执行原生 SQL（如 INSERT, UPDATE, DELETE 或 DDL 语句）
// 返回受影响的行数
func (r *Repository[D, T]) RawExec(ctx context.Context, sql string, args ...any) (int64, error) {
	return r.RawExecTx(ctx, nil, sql, args...)
}

// RawScan 执行原生 SQL 并将结果映射到【任意】指定的结构体或变量中
// 适用场景：聚合查询（如 SUM/COUNT）或统计类报表
func (r *Repository[D, T]) RawScan(ctx context.Context, dest any, sql string, args ...any) error {
	return r.RawScanTx(ctx, nil, dest, sql, args...)
}

// RawQueryTx 在事务中执行原生查询 SQL，并将结果映射到当前 Repository 的实体切片中
func (r *Repository[D, T]) RawQueryTx(ctx context.Context, tx *gorm.DB, sql string, args ...any) ([]T, error) {
	var results []T
	if sql == "" {
		return results, ErrRawSQLEmpty
	}
	err := r.dbResolver(ctx, tx).Raw(sql, args...).Scan(&results).Error
	return results, err
}

// RawExecTx 在事务中执行原生 SQL（如 INSERT, UPDATE, DELETE 或 DDL 语句）
// 返回受影响的行数
func (r *Repository[D, T]) RawExecTx(ctx context.Context, tx *gorm.DB, sql string, args ...any) (int64, error) {
	if sql == "" {
		return 0, ErrRawSQLEmpty
	}
	result := r.dbResolver(ctx, tx).Exec(sql, args...)
	return result.RowsAffected, result.Error
}

// RawScanTx 在事务中执行原生 SQL 并将结果映射到【任意】指定的结构体或变量中
func (r *Repository[D, T]) RawScanTx(ctx context.Context, tx *gorm.DB, dest any, sql string, args ...any) error {
	if sql == "" {
		return ErrRawSQLEmpty
	}
	return r.dbResolver(ctx, tx).Raw(sql, args...).Scan(dest).Error
}
