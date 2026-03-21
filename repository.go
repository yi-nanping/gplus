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

// Exists 检查是否存在满足条件的记录。
// 若 q 不含任何条件，等价于检查表中是否存在任何记录（全表 LIMIT 1）。
func (r *Repository[D, T]) Exists(q *Query[T]) (bool, error) {
	return r.ExistsTx(q, nil)
}

// ExistsTx 支持事务的存在性检查
func (r *Repository[D, T]) ExistsTx(q *Query[T], tx *gorm.DB) (bool, error) {
	if q == nil {
		return false, ErrQueryNil
	}
	if err := q.GetError(); err != nil {
		return false, err
	}
	if err := q.DataRuleBuilder().GetError(); err != nil {
		return false, err
	}
	var tmp []T
	err := r.dbResolver(q.Context(), tx).Scopes(q.BuildQuery()).Limit(1).Find(&tmp).Error
	if err != nil {
		return false, err
	}
	return len(tmp) > 0, nil
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

// Page 分页查询。
// skipCount=true 时跳过 COUNT 查询，total 恒为 0，适合已知总数或不需要总数的场景（性能更优）。
// skipCount=false 时先执行 COUNT，若总数为 0 则直接返回，不再执行 Find。
func (r *Repository[D, T]) Page(q *Query[T], skipCount bool) (data []T, total int64, err error) {
	return r.PageTx(q, skipCount, nil)
}

// PageTx 支持事务的分页查询。skipCount 语义同 Page。
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

// FirstOrCreate 按条件查找记录，不存在时用 defaults 创建。
// 返回值：(record, created, error)，created=true 表示本次新建。
// defaults 为 nil 时创建零值记录。
// 内部使用事务保证查询与创建的原子性。
func (r *Repository[D, T]) FirstOrCreate(q *Query[T], defaults *T) (data T, created bool, err error) {
	if q == nil {
		return data, false, ErrQueryNil
	}
	if err = q.GetError(); err != nil {
		return data, false, err
	}
	if err = q.DataRuleBuilder().GetError(); err != nil {
		return data, false, err
	}
	err = r.db.WithContext(q.Context()).Transaction(func(tx *gorm.DB) error {
		// 使用 BuildCount 路径（WHERE/JOIN，不含 SELECT/ORDER/LIMIT/Preload），确保 First 返回完整记录
		if e := tx.Scopes(q.BuildCount()).First(&data).Error; e == nil {
			// 找到记录
			return nil
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			// 查询失败（非「不存在」错误）
			return e
		}
		// 未找到，用 defaults 创建
		if defaults != nil {
			data = *defaults
		}
		created = true
		return tx.Create(&data).Error
	})
	if err != nil {
		created = false
	}
	return data, created, err
}

// Chunk 分批处理查询结果，每批调用 fn 一次。fn 返回非 nil 错误时立即终止并返回该错误。
// batchSize 建议在 100-1000 之间，过小会增加 DB 往返次数，过大会占用大量内存。
//
// 内部基于 GORM FindInBatches，使用主键游标分页（WHERE id > lastID），性能优于 OFFSET 分页。
// 主键类型说明：
//   - 自增 int：最优，单调递增，索引连续扫描。
//   - UUID v7 / 时间有序字符串：接近 int，近似有序，性能良好。
//   - UUID v4 / 随机字符串：功能正确，但随机分布导致索引跳跃，性能弱于 int。
//   - 复合主键：GORM 仅用第一个主键字段做游标，可能漏行，不建议使用 Chunk，请改用 Page()。
func (r *Repository[D, T]) Chunk(q *Query[T], batchSize int, fn func([]T) error) error {
	return r.ChunkTx(q, batchSize, nil, fn)
}

// ChunkTx 支持事务的分批处理。tx=nil 时降级为普通连接，行为等同 Chunk。
func (r *Repository[D, T]) ChunkTx(q *Query[T], batchSize int, tx *gorm.DB, fn func([]T) error) error {
	if q == nil {
		return ErrQueryNil
	}
	if err := q.GetError(); err != nil {
		return err
	}
	if err := q.DataRuleBuilder().GetError(); err != nil {
		return err
	}
	var batch []T
	result := r.dbResolver(q.Context(), tx).Model(new(T)).Scopes(q.BuildQuery()).FindInBatches(&batch, batchSize, func(_ *gorm.DB, _ int) error {
		return fn(batch)
	})
	return result.Error
}

// UpdateByIds 批量按主键更新，ids 为空时直接返回 0，不发 SQL
func (r *Repository[D, T]) UpdateByIds(ctx context.Context, ids []D, u *Updater[T]) (int64, error) {
	return r.UpdateByIdsTx(ctx, ids, u, nil)
}

// UpdateByIdsTx 支持事务的批量主键更新
func (r *Repository[D, T]) UpdateByIdsTx(ctx context.Context, ids []D, u *Updater[T], tx *gorm.DB) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if u == nil || u.IsEmpty() {
		return 0, ErrUpdateEmpty
	}
	if err := u.GetError(); err != nil {
		return 0, err
	}
	var model T
	db := r.dbResolver(ctx, tx).Model(&model).Where(ids).Scopes(u.BuildUpdate())
	result := db.Updates(u.setMap)
	return result.RowsAffected, result.Error
}

// DeleteByIds 批量按主键删除，ids 为空时直接返回 0，不发 SQL
func (r *Repository[D, T]) DeleteByIds(ctx context.Context, ids []D) (int64, error) {
	return r.DeleteByIdsTx(ctx, ids, nil)
}

// DeleteByIdsTx 支持事务的批量主键删除，ids 为空时直接返回 0，不发 SQL
func (r *Repository[D, T]) DeleteByIdsTx(ctx context.Context, ids []D, tx *gorm.DB) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	db := r.dbResolver(ctx, tx).Delete(new(T), ids)
	return db.RowsAffected, db.Error
}

// aggregate 内部通用聚合执行函数
// 使用指针中间变量接收 NULL（空表或无匹配行时聚合结果为 NULL），nil 时返回零值
func aggregate[T any, R any, D comparable](r *Repository[D, T], q *Query[T], fn string, col any, tx *gorm.DB) (result R, err error) {
	if q == nil {
		return result, ErrQueryNil
	}
	if err = q.GetError(); err != nil {
		return result, err
	}
	colName, err := resolveColumnName(col)
	if err != nil {
		return result, err
	}
	if err = q.DataRuleBuilder().GetError(); err != nil {
		return result, err
	}
	expr := fmt.Sprintf("%s(%s)", fn, colName)
	var ptr *R
	// 聚合查询只需要 WHERE/JOIN/GROUP BY，与 BuildCount 路径一致，无需 ORDER/LIMIT/Preload
	err = r.dbResolver(q.Context(), tx).Model(new(T)).Scopes(q.BuildCount()).Select(expr).Scan(&ptr).Error
	if err == nil && ptr != nil {
		result = *ptr
	}
	return result, err
}

// Sum 对指定列求和，R 为数值类型（int64、float64 等）
func Sum[T any, R any, D comparable](r *Repository[D, T], q *Query[T], col any) (R, error) {
	return SumTx[T, R, D](r, q, col, nil)
}

// SumTx 支持事务的列求和
func SumTx[T any, R any, D comparable](r *Repository[D, T], q *Query[T], col any, tx *gorm.DB) (R, error) {
	return aggregate[T, R, D](r, q, "SUM", col, tx)
}

// Max 对指定列求最大值
func Max[T any, R any, D comparable](r *Repository[D, T], q *Query[T], col any) (R, error) {
	return MaxTx[T, R, D](r, q, col, nil)
}

// MaxTx 支持事务的列最大值
func MaxTx[T any, R any, D comparable](r *Repository[D, T], q *Query[T], col any, tx *gorm.DB) (R, error) {
	return aggregate[T, R, D](r, q, "MAX", col, tx)
}

// Min 对指定列求最小值
func Min[T any, R any, D comparable](r *Repository[D, T], q *Query[T], col any) (R, error) {
	return MinTx[T, R, D](r, q, col, nil)
}

// MinTx 支持事务的列最小值
func MinTx[T any, R any, D comparable](r *Repository[D, T], q *Query[T], col any, tx *gorm.DB) (R, error) {
	return aggregate[T, R, D](r, q, "MIN", col, tx)
}

// Avg 对指定列求平均值，R 建议使用 float64
func Avg[T any, R any, D comparable](r *Repository[D, T], q *Query[T], col any) (R, error) {
	return AvgTx[T, R, D](r, q, col, nil)
}

// AvgTx 支持事务的列平均值
func AvgTx[T any, R any, D comparable](r *Repository[D, T], q *Query[T], col any, tx *gorm.DB) (R, error) {
	return aggregate[T, R, D](r, q, "AVG", col, tx)
}

// GetByIds 批量按主键查询，ids 为空时直接返回空切片
func (r *Repository[D, T]) GetByIds(ctx context.Context, ids []D) ([]T, error) {
	return r.GetByIdsTx(ctx, ids, nil)
}

// GetByIdsTx 支持事务的批量主键查询，ids 为空时直接返回空切片
func (r *Repository[D, T]) GetByIdsTx(ctx context.Context, ids []D, tx *gorm.DB) ([]T, error) {
	var result []T
	if len(ids) == 0 {
		return result, nil
	}
	err := r.dbResolver(ctx, tx).Find(&result, ids).Error
	return result, err
}
