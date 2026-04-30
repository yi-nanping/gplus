package gplus

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"unsafe"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

var (
	ErrQueryNil          = errors.New("gplus: query cannot be nil")
	ErrRawSQLEmpty       = errors.New("gplus: raw sql cannot be empty")
	ErrDeleteEmpty       = errors.New("gplus: delete content is empty")
	ErrUpdateEmpty       = errors.New("gplus: update content is empty")
	ErrUpdateNoCondition = errors.New("gplus: update requires at least one condition to prevent full-table update")
	ErrTransactionReq    = errors.New("gplus: locking query must be executed within a transaction")
	ErrDefaultsNil       = errors.New("gplus: defaults cannot be nil, use &T{} to create a zero-value record explicitly")
	ErrRestoreEmpty      = errors.New("gplus: restore condition is empty")
	ErrOnConflictInvalid = errors.New("gplus: OnConflict config invalid: DoNothing is mutually exclusive with DoUpdates/DoUpdateAll/UpdateExprs; DoUpdateAll is mutually exclusive with DoUpdates/UpdateExprs")
	ErrOptimisticLock    = errors.New("gplus: optimistic lock conflict (version mismatch or row not found)")
)

// OnConflict 定义 INSERT ... ON CONFLICT 的冲突处理策略。
// Columns 为空时：MySQL 按表内唯一索引自动判定；Postgres/SQLite 须显式指定冲突列。
type OnConflict struct {
	// Columns 冲突检测列（唯一索引/主键），支持字段指针或字符串列名。
	Columns []any
	// DoNothing 冲突时跳过，不执行任何更新（INSERT IGNORE / DO NOTHING）。
	// 与 DoUpdates/DoUpdateAll/UpdateExprs 互斥。
	DoNothing bool
	// DoUpdates 冲突时按 EXCLUDED 覆盖指定列，支持字段指针或字符串列名。
	// 与 DoNothing/DoUpdateAll 互斥，可与 UpdateExprs 组合。
	DoUpdates []any
	// DoUpdateAll 冲突时按 EXCLUDED 覆盖除主键外的所有列。
	// 与 DoNothing/DoUpdates/UpdateExprs 互斥。
	DoUpdateAll bool
	// UpdateExprs 冲突时按自定义表达式更新，key 为列名，value 为 gorm.Expr 或普通值。
	// 示例：{"count": gorm.Expr("count + VALUES(count)")}
	// 可与 DoUpdates 组合，不可与 DoNothing/DoUpdateAll 共用。
	UpdateExprs map[string]any
}

// buildClause 将 OnConflict 转换为 GORM clause.OnConflict。
func (oc OnConflict) buildClause() (clause.OnConflict, error) {
	hasDoUpdates := len(oc.DoUpdates) > 0
	hasExprs := len(oc.UpdateExprs) > 0

	// 互斥校验
	if oc.DoNothing && (oc.DoUpdateAll || hasDoUpdates || hasExprs) {
		return clause.OnConflict{}, ErrOnConflictInvalid
	}
	if oc.DoUpdateAll && (hasDoUpdates || hasExprs) {
		return clause.OnConflict{}, ErrOnConflictInvalid
	}

	// 解析冲突检测列
	cols := make([]clause.Column, 0, len(oc.Columns))
	for _, c := range oc.Columns {
		name, err := resolveColumnName(c)
		if err != nil {
			return clause.OnConflict{}, fmt.Errorf("gplus: OnConflict.Columns invalid: %w", err)
		}
		cols = append(cols, clause.Column{Name: name})
	}

	if oc.DoNothing {
		return clause.OnConflict{Columns: cols, DoNothing: true}, nil
	}
	if oc.DoUpdateAll {
		return clause.OnConflict{Columns: cols, UpdateAll: true}, nil
	}

	// 构建 DoUpdates + UpdateExprs 合并的 Set
	var assignments clause.Set
	if hasDoUpdates {
		colNames := make([]string, 0, len(oc.DoUpdates))
		for _, c := range oc.DoUpdates {
			name, err := resolveColumnName(c)
			if err != nil {
				return clause.OnConflict{}, fmt.Errorf("gplus: OnConflict.DoUpdates invalid: %w", err)
			}
			colNames = append(colNames, name)
		}
		assignments = append(assignments, clause.AssignmentColumns(colNames)...)
	}
	if hasExprs {
		for col, val := range oc.UpdateExprs {
			assignments = append(assignments, clause.Assignment{
				Column: clause.Column{Name: col},
				Value:  val,
			})
		}
	}

	// 无任何更新策略 → 默认 UpdateAll
	if len(assignments) == 0 {
		return clause.OnConflict{Columns: cols, UpdateAll: true}, nil
	}
	return clause.OnConflict{Columns: cols, DoUpdates: assignments}, nil
}

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

// NewQuery 创建与当前 Repository 同类型的查询构建器，无需重复指定泛型参数。
// 等价于 gplus.NewQuery[T](ctx)，但类型由 Repository 自动推导。
func (r *Repository[D, T]) NewQuery(ctx context.Context) (*Query[T], *T) {
	return NewQuery[T](ctx)
}

// NewUpdater 创建与当前 Repository 同类型的更新构建器，无需重复指定泛型参数。
// 等价于 gplus.NewUpdater[T](ctx)，但类型由 Repository 自动推导。
func (r *Repository[D, T]) NewUpdater(ctx context.Context) (*Updater[T], *T) {
	return NewUpdater[T](ctx)
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

// GetByIdTx 支持事务的查询。
// 若 ctx 中携带 DataRule，将在 WHERE 中追加对应条件；跨租户 ID 返回 gorm.ErrRecordNotFound。
func (r *Repository[D, T]) GetByIdTx(ctx context.Context, id D, tx *gorm.DB) (data T, err error) {
	q, _ := NewQuery[T](ctx)
	if err = q.DataRuleBuilder().GetError(); err != nil {
		return
	}
	err = r.dbResolver(ctx, tx).Scopes(q.BuildQuery()).First(&data, id).Error
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

// Last 按主键倒序取第一条记录，语义与 GetOne 对称（GetOne 用 First）。
func (r *Repository[D, T]) Last(q *Query[T]) (data T, err error) {
	return r.LastTx(q, nil)
}

// LastTx 支持事务的 Last。
func (r *Repository[D, T]) LastTx(q *Query[T], tx *gorm.DB) (data T, err error) {
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
	err = db.Scopes(q.BuildQuery()).Last(&data).Error
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
	db := r.dbResolver(q.Context(), tx).Model(new(T))
	// 提前应用 Distinct 标志：GORM Pluck 在 callbacks.Execute 之前就构建 clause.Select，
	// 此时 Statement.Distinct 还未被 scope 内的 applyDistinct 设置；
	// 所以必须在调用 Pluck 之前先在 statement 上置位，才能让 Pluck 生成 SELECT DISTINCT。
	if q.distinct {
		db = db.Distinct()
	}
	err = db.Scopes(q.BuildQuery()).Pluck(colName, &result).Error
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

// UpdateByIdTx 事务更新。若模型含 `gplus:"version"` 字段则启用乐观锁：
// WHERE id=? AND version=oldVer，SET ..., version=version+1；
// affected==0 时返回 ErrOptimisticLock（版本冲突或记录不存在）。
// 成功后 entity.Version 自动递增，可直接再次调用。
//
// 注意：启用 DataRule 时，记录存在但跨租户会返回 affected==0（返回 ErrOptimisticLock），
// 此时不应无条件重试（重试无法绕过权限）。乐观锁版本冲突与 DataRule 拦截当前共用同一错误码，
// 调用方需通过其他途径区分（如先 GetById 检查记录是否在权限范围内）。
func (r *Repository[D, T]) UpdateByIdTx(ctx context.Context, entity *T, tx *gorm.DB) error {
	q, _ := NewQuery[T](ctx)
	if err := q.DataRuleBuilder().GetError(); err != nil {
		return err
	}
	baseDB := r.dbResolver(ctx, tx).Scopes(q.BuildUpdate())
	vInfo := getVersionField[T]()
	if vInfo == nil {
		return baseDB.Model(entity).Updates(entity).Error
	}

	oldVer := readVersionValue(unsafe.Pointer(entity), vInfo)
	qL, qR := getQuoteChar(baseDB)
	quotedCol := quoteColumn(vInfo.columnName, qL, qR)

	setMap := buildUpdateMap(entity, vInfo)
	setMap[vInfo.columnName] = gorm.Expr(quotedCol + " + 1")

	result := baseDB.Model(entity).
		Where(quotedCol+" = ?", oldVer).
		Updates(setMap)

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrOptimisticLock
	}
	writeVersionValue(unsafe.Pointer(entity), vInfo, oldVer+1)
	return nil
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

// DeleteByIdTx 事务删除。
// 若 ctx 中携带 DataRule，仅当记录满足权限条件时才删除；跨租户 ID 返回 affected=0。
func (r *Repository[D, T]) DeleteByIdTx(ctx context.Context, id D, tx *gorm.DB) (int64, error) {
	q, _ := NewQuery[T](ctx)
	if err := q.DataRuleBuilder().GetError(); err != nil {
		return 0, err
	}
	result := r.dbResolver(ctx, tx).Scopes(q.BuildDelete()).Delete(new(T), id)
	return result.RowsAffected, result.Error
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
// defaults 不可为 nil，否则返回 ErrDefaultsNil。
// 内部使用事务保证查询与创建的原子性。
func (r *Repository[D, T]) FirstOrCreate(q *Query[T], defaults *T) (data T, created bool, err error) {
	if q == nil {
		return data, false, ErrQueryNil
	}
	if defaults == nil {
		return data, false, ErrDefaultsNil
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
		// 未找到，用 defaults 创建新记录
		data = *defaults
		created = true
		return tx.Create(&data).Error
	})
	if err != nil {
		created = false
	}
	return data, created, err
}

// FirstOrUpdate 按条件查找记录，找到则执行 Updater 更新，未找到则用 defaults 创建。
// 返回值：(record, created, error)，created=true 表示本次新建。
// defaults 不可为 nil，否则返回 ErrDefaultsNil。
// 内部使用事务保证查找与更新/创建的原子性。
func (r *Repository[D, T]) FirstOrUpdate(q *Query[T], u *Updater[T], defaults *T) (data T, created bool, err error) {
	if q == nil {
		return data, false, ErrQueryNil
	}
	if defaults == nil {
		return data, false, ErrDefaultsNil
	}
	if u == nil || u.IsEmpty() {
		return data, false, ErrUpdateEmpty
	}
	if err = q.GetError(); err != nil {
		return data, false, err
	}
	if err = u.GetError(); err != nil {
		return data, false, err
	}
	if err = q.DataRuleBuilder().GetError(); err != nil {
		return data, false, err
	}
	err = r.db.WithContext(q.Context()).Transaction(func(tx *gorm.DB) error {
		// BuildCount 路径：WHERE/JOIN，不含 SELECT/ORDER/LIMIT，确保 First 返回完整记录
		if e := tx.Scopes(q.BuildCount()).First(&data).Error; e == nil {
			// 找到记录，执行更新
			if ue := tx.Model(&data).Scopes(u.BuildUpdate()).Updates(u.setMap).Error; ue != nil {
				return ue
			}
			// 按主键重读：避免 data 中的旧字段值（含被更新的字段）被当作 WHERE 条件导致查不到
			reloadStmt := &gorm.Statement{DB: tx}
			if pe := reloadStmt.Parse(new(T)); pe == nil && reloadStmt.Schema.PrioritizedPrimaryField != nil {
				if pkVal, isZero := reloadStmt.Schema.PrioritizedPrimaryField.ValueOf(q.Context(), reflect.ValueOf(data)); !isZero {
					var fresh T
					if re := tx.First(&fresh, pkVal).Error; re != nil {
						return re
					}
					data = fresh
					return nil
				}
			}
			// 无主键时降级：按原始查询条件重读
			return tx.Scopes(q.BuildCount()).First(&data).Error
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		// 未找到，用 defaults 创建新记录
		data = *defaults
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

// UpdateByIdsTx 支持事务的批量主键更新。
//
// 注意：启用 DataRule 时，记录存在但跨租户会返回 affected==0，此时不应无条件重试
// （重试无法绕过权限）。调用方需通过其他途径区分权限拦截与实际无匹配行。
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
	q, _ := NewQuery[T](ctx)
	if err := q.DataRuleBuilder().GetError(); err != nil {
		return 0, err
	}
	var model T
	db := r.dbResolver(ctx, tx).Model(&model).Where(ids).Scopes(q.BuildUpdate(), u.BuildUpdate())
	result := db.Updates(u.setMap)
	return result.RowsAffected, result.Error
}

// IncrBy 原子自增指定列。col 须为 NewUpdater 返回的 *T 实例的字段指针。
// u 用于指定 WHERE 条件；未设置条件时返回 ErrUpdateNoCondition 以防全表更新。
func (r *Repository[D, T]) IncrBy(u *Updater[T], col any, delta int64) (int64, error) {
	return r.IncrByTx(u, col, delta, nil)
}

// IncrByTx 支持事务的原子自增。
func (r *Repository[D, T]) IncrByTx(u *Updater[T], col any, delta int64, tx *gorm.DB) (int64, error) {
	if u == nil {
		return 0, ErrQueryNil
	}
	if err := u.GetError(); err != nil {
		return 0, err
	}
	if err := u.DataRuleBuilder().GetError(); err != nil {
		return 0, err
	}
	if len(u.conditions) == 0 {
		return 0, ErrUpdateNoCondition
	}
	colName, err := resolveColumnName(col)
	if err != nil {
		return 0, fmt.Errorf("gplus: IncrBy invalid column pointer: %w", err)
	}
	var model T
	db := r.dbResolver(u.Context(), tx).Model(&model).Scopes(u.BuildUpdate())
	result := db.Update(colName, gorm.Expr(colName+" + ?", delta))
	return result.RowsAffected, result.Error
}

// DecrBy 原子自减指定列。等价于 IncrBy(u, col, -delta)。
func (r *Repository[D, T]) DecrBy(u *Updater[T], col any, delta int64) (int64, error) {
	return r.IncrByTx(u, col, -delta, nil)
}

// DecrByTx 支持事务的原子自减。
func (r *Repository[D, T]) DecrByTx(u *Updater[T], col any, delta int64, tx *gorm.DB) (int64, error) {
	return r.IncrByTx(u, col, -delta, tx)
}

// DeleteByIds 批量按主键删除，ids 为空时直接返回 0，不发 SQL
func (r *Repository[D, T]) DeleteByIds(ctx context.Context, ids []D) (int64, error) {
	return r.DeleteByIdsTx(ctx, ids, nil)
}

// DeleteByIdsTx 支持事务的批量主键删除，ids 为空时直接返回 0，不发 SQL。
// 若 ctx 中携带 DataRule，仅删除满足权限条件的记录；跨租户 ID 被静默跳过。
func (r *Repository[D, T]) DeleteByIdsTx(ctx context.Context, ids []D, tx *gorm.DB) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	q, _ := NewQuery[T](ctx)
	if err := q.DataRuleBuilder().GetError(); err != nil {
		return 0, err
	}
	result := r.dbResolver(ctx, tx).Scopes(q.BuildDelete()).Delete(new(T), ids)
	return result.RowsAffected, result.Error
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

// GetByIdsTx 支持事务的批量主键查询，ids 为空时直接返回空切片。
// 若 ctx 中携带 DataRule，结果集仅包含满足权限条件的记录；跨租户 ID 被静默过滤。
func (r *Repository[D, T]) GetByIdsTx(ctx context.Context, ids []D, tx *gorm.DB) ([]T, error) {
	var result []T
	if len(ids) == 0 {
		return result, nil
	}
	q, _ := NewQuery[T](ctx)
	if err := q.DataRuleBuilder().GetError(); err != nil {
		return nil, err
	}
	err := r.dbResolver(ctx, tx).Scopes(q.BuildQuery()).Find(&result, ids).Error
	return result, err
}

// Restore 恢复软删除记录（将 deleted_at 置 NULL）。
// 返回受影响的行数：1 表示成功恢复，0 表示记录不存在或未被软删除。
// 注意：模型须包含 gorm.DeletedAt 字段，否则行为未定义。
func (r *Repository[D, T]) Restore(ctx context.Context, id D) (int64, error) {
	return r.RestoreTx(ctx, id, nil)
}

// RestoreTx 支持事务的软删除恢复。
// 注意：启用 DataRule 时，跨租户记录返回 affected==0（不会恢复）。
func (r *Repository[D, T]) RestoreTx(ctx context.Context, id D, tx *gorm.DB) (int64, error) {
	q, _ := NewQuery[T](ctx)
	if err := q.DataRuleBuilder().GetError(); err != nil {
		return 0, err
	}
	baseDB := r.dbResolver(ctx, tx).Scopes(q.BuildUpdate())
	// 动态查找软删除字段：遍历所有字段，找实现了 DeleteClausesInterface 的字段（即 gorm.DeletedAt 类型），
	// 不依赖 Go 字段名，支持自定义字段名（如 RemovedAt gorm.DeletedAt）
	col := "deleted_at"
	stmt := &gorm.Statement{DB: baseDB}
	if err := stmt.Parse(new(T)); err == nil {
		for _, f := range stmt.Schema.Fields {
			if _, ok := reflect.New(f.FieldType).Interface().(schema.DeleteClausesInterface); ok {
				col = f.DBName
				break
			}
		}
	}
	result := baseDB.Unscoped().Model(new(T)).Where(id).
		Where(col + " IS NOT NULL").
		Update(col, nil)
	return result.RowsAffected, result.Error
}

// RestoreByCond 按条件批量恢复软删除记录（将 deleted_at 置 NULL）。
// 空条件会返回 ErrRestoreEmpty，防止意外全表恢复。
// 返回受影响的行数。
func (r *Repository[D, T]) RestoreByCond(q *Query[T]) (int64, error) {
	return r.RestoreByCondTx(q, nil)
}

// RestoreByCondTx 支持事务的按条件批量恢复软删除。
func (r *Repository[D, T]) RestoreByCondTx(q *Query[T], tx *gorm.DB) (int64, error) {
	if q == nil {
		return 0, ErrQueryNil
	}
	if q.IsEmpty() {
		return 0, ErrRestoreEmpty
	}
	if err := q.GetError(); err != nil {
		return 0, err
	}
	if err := q.DataRuleBuilder().GetError(); err != nil {
		return 0, err
	}
	db := r.dbResolver(q.Context(), tx)
	// 动态查找软删除字段列名
	col := "deleted_at"
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(new(T)); err == nil {
		for _, f := range stmt.Schema.Fields {
			if _, ok := reflect.New(f.FieldType).Interface().(schema.DeleteClausesInterface); ok {
				col = f.DBName
				break
			}
		}
	}
	result := db.Unscoped().Model(new(T)).
		Scopes(q.BuildUpdate()).
		Where(col + " IS NOT NULL").
		Update(col, nil)
	return result.RowsAffected, result.Error
}

// ListMap 查询列表并按 keyFn 转换为 map。重复 key 时后者覆盖前者。
func (r *Repository[D, T]) ListMap(q *Query[T], keyFn func(T) D) (map[D]T, error) {
	return r.ListMapTx(q, keyFn, nil)
}

// ListMapTx 支持事务的 ListMap。
func (r *Repository[D, T]) ListMapTx(q *Query[T], keyFn func(T) D, tx *gorm.DB) (map[D]T, error) {
	if q == nil {
		return nil, ErrQueryNil
	}
	if keyFn == nil {
		return nil, errors.New("gplus: keyFn cannot be nil")
	}
	list, err := r.ListTx(q, tx)
	if err != nil {
		return nil, err
	}
	result := make(map[D]T, len(list))
	for _, item := range list {
		result[keyFn(item)] = item
	}
	return result, nil
}

// InsertOnConflict 执行带冲突处理的单条插入。
// 根据 OnConflict 策略决定：冲突时跳过(DoNothing)、覆盖指定列(DoUpdates)、覆盖所有列(DoUpdateAll)、原子表达式更新(UpdateExprs)。
func (r *Repository[D, T]) InsertOnConflict(ctx context.Context, entity *T, oc OnConflict) error {
	return r.InsertOnConflictTx(ctx, entity, oc, nil)
}

// InsertOnConflictTx 支持事务的单条冲突插入。
func (r *Repository[D, T]) InsertOnConflictTx(ctx context.Context, entity *T, oc OnConflict, tx *gorm.DB) error {
	c, err := oc.buildClause()
	if err != nil {
		return err
	}
	return r.dbResolver(ctx, tx).Clauses(c).Create(entity).Error
}

// InsertBatchOnConflict 执行带冲突处理的批量插入。entities 为空时直接返回，不发 SQL。
func (r *Repository[D, T]) InsertBatchOnConflict(ctx context.Context, entities []T, oc OnConflict) error {
	return r.InsertBatchOnConflictTx(ctx, entities, oc, nil)
}

// InsertBatchOnConflictTx 支持事务的批量冲突插入。
func (r *Repository[D, T]) InsertBatchOnConflictTx(ctx context.Context, entities []T, oc OnConflict, tx *gorm.DB) error {
	if len(entities) == 0 {
		return nil
	}
	c, err := oc.buildClause()
	if err != nil {
		return err
	}
	return r.dbResolver(ctx, tx).Clauses(c).Create(&entities).Error
}
