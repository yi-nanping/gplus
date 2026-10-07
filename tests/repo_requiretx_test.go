package gplus_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
)

type requireTxRow struct {
	ID       int64 `gorm:"primaryKey;autoIncrement"`
	TenantID int
	Name     string
}

type requireTxContextKey struct{}

// 使用值类型包装真实连接，验证能力识别不局限于 *sql.Tx 或指针类型。
type requireTxValuePool struct {
	gorm.ConnPool
	gorm.TxCommitter
}

type requireTxInspectionPool struct{}

func (requireTxInspectionPool) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	panic("RequireTx must not prepare SQL")
}

func (requireTxInspectionPool) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	panic("RequireTx must not execute SQL")
}

func (requireTxInspectionPool) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	panic("RequireTx must not query SQL")
}

func (requireTxInspectionPool) QueryRowContext(context.Context, string, ...any) *sql.Row {
	panic("RequireTx must not query SQL")
}

func (requireTxInspectionPool) BeginTx(context.Context, *sql.TxOptions) (gorm.ConnPool, error) {
	panic("RequireTx must not begin a transaction")
}

func (requireTxInspectionPool) Commit() error {
	panic("RequireTx must not commit a transaction")
}

func (requireTxInspectionPool) Rollback() error {
	panic("RequireTx must not roll back a transaction")
}

func requireTxWithPool(db *gorm.DB, pool gorm.ConnPool) *gorm.DB {
	copyDB := db.Session(&gorm.Session{Context: db.Statement.Context})
	copyDB.Statement.ConnPool = pool
	return copyDB
}

func TestRequireTx_RejectInvalidHandles(t *testing.T) {
	repo, db := setupTestDB[requireTxRow](t)
	for _, tc := range []struct {
		name string
		tx   *gorm.DB
	}{
		{name: "Nil"},
		{name: "OrdinaryDB", tx: db},
		{name: "PreparedDB", tx: db.Session(&gorm.Session{PrepareStmt: true})},
		{name: "DryRunDB", tx: db.Session(&gorm.Session{DryRun: true})},
		{name: "MissingStatement", tx: &gorm.DB{}},
		{name: "MissingPool", tx: requireTxWithPool(db, nil)},
		{name: "TypedNilPool", tx: requireTxWithPool(db, (*sql.Tx)(nil))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bound, err := repo.RequireTx(tc.tx)
			if bound != nil || !errors.Is(err, gorm.ErrInvalidTransaction) {
				t.Fatalf("repo=%v err=%v，期望 nil 和 ErrInvalidTransaction", bound, err)
			}
			if repo.GetDB() != db {
				t.Fatal("RequireTx 不应修改原仓库")
			}
		})
	}
}

func TestRequireTx_PropagateHandleError(t *testing.T) {
	repo, db := setupTestDB[requireTxRow](t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	failed := db.WithContext(ctx).Begin()
	if !errors.Is(failed.Error, context.Canceled) {
		t.Fatalf("Begin err=%v，期望 context.Canceled", failed.Error)
	}
	bound, err := repo.RequireTx(failed)
	if bound != nil || err != failed.Error {
		t.Fatalf("repo=%v err=%v，期望原样返回事务句柄错误 %v", bound, err, failed.Error)
	}
}

func TestRequireTx_BindingHasNoConnectionSideEffects(t *testing.T) {
	repo, db := setupTestDB[requireTxRow](t)
	handle := requireTxWithPool(db, requireTxInspectionPool{})
	bound, err := repo.RequireTx(handle)
	if err != nil || bound == nil || bound.GetDB() != handle {
		t.Fatalf("绑定失败: %v", err)
	}
}

func TestRequireTx_AcceptTransactionPools(t *testing.T) {
	for _, name := range []string{"Begin", "Prepared", "ValueWrapper"} {
		t.Run(name, func(t *testing.T) {
			repo, db := setupTestDB[requireTxRow](t)
			base := db
			if name == "Prepared" {
				base = db.Session(&gorm.Session{PrepareStmt: true})
			}
			tx := base.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			candidate := tx
			if name == "ValueWrapper" {
				pool := requireTxValuePool{ConnPool: tx.Statement.ConnPool, TxCommitter: tx.Statement.ConnPool.(gorm.TxCommitter)}
				candidate = requireTxWithPool(tx, pool)
			}
			bound, err := repo.RequireTx(candidate)
			if err != nil || bound == nil {
				t.Fatalf("绑定失败: %v", err)
			}
			if bound == repo || bound.GetDB() != candidate || repo.GetDB() != db {
				t.Fatal("应返回绑定原事务句柄的新仓库，保留原仓库")
			}
			if name == "Prepared" {
				if _, ok := bound.GetDB().Statement.ConnPool.(*gorm.PreparedStmtTX); !ok {
					t.Fatal("应保留 GORM 预处理事务包装")
				}
			}
		})
	}
}

func TestRequireTx_CommitAndRollback(t *testing.T) {
	for _, tc := range []struct {
		name     string
		prepare  bool
		rollback bool
	}{
		{name: "Commit"},
		{name: "Rollback", rollback: true},
		{name: "PreparedCommit", prepare: true},
		{name: "PreparedRollback", prepare: true, rollback: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, db := setupTestDB[requireTxRow](t)
			base := db
			if tc.prepare {
				base = db.Session(&gorm.Session{PrepareStmt: true})
			}
			ctx := ctxWithTenantRule(1)
			rollbackErr := errors.New("rollback bound repository")
			err := base.Transaction(func(tx *gorm.DB) error {
				bound, err := repo.RequireTx(tx)
				if err != nil {
					return err
				}
				first := requireTxRow{TenantID: 1, Name: "first"}
				if err := bound.Save(ctx, &first); err != nil {
					return err
				}
				second := requireTxRow{TenantID: 1, Name: "second"}
				// nil 使用新仓库的绑定连接，不能回退到原仓库 DB。
				if err := bound.SaveTx(ctx, &second, nil); err != nil {
					return err
				}
				u, m := bound.NewUpdater(ctx)
				u.Eq(&m.ID, first.ID).Set(&m.Name, "updated")
				affected, err := bound.UpdateByCond(u)
				if err != nil {
					return err
				}
				if affected != 1 {
					t.Fatalf("affected=%d，期望 1", affected)
				}
				q, m := bound.NewQuery(ctx)
				q.Select(&m.Name).Order(&m.ID, true)
				var rows []struct{ Name string }
				if err := FindAsTx(bound, q, &rows, nil); err != nil {
					return err
				}
				if len(rows) != 2 || rows[0].Name != "updated" || rows[1].Name != "second" {
					t.Fatalf("事务内 rows=%+v，期望 [updated second]", rows)
				}
				if tc.rollback {
					return rollbackErr
				}
				return nil
			})
			if tc.rollback {
				if !errors.Is(err, rollbackErr) {
					t.Fatalf("err=%v，期望回滚原因", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			var rows []requireTxRow
			if err := db.Order("id ASC").Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if tc.rollback {
				if len(rows) != 0 {
					t.Fatalf("回滚后仍有记录: %+v", rows)
				}
			} else if len(rows) != 2 || rows[0].Name != "updated" || rows[1].Name != "second" {
				t.Fatalf("提交后 rows=%+v，期望 [updated second]", rows)
			}
		})
	}
}

func TestRequireTx_ContextAndDataRule(t *testing.T) {
	repo, db := setupTestDB[requireTxRow](t)
	visible := requireTxRow{TenantID: 1, Name: "visible"}
	invisible := requireTxRow{TenantID: 2, Name: "hidden"}
	if err := db.Create(&visible).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&invisible).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(ctxWithTenantRule(1), requireTxContextKey{}, "request")
	var queryCalls, updateCalls int
	observe := func(d *gorm.DB) {
		if d.Statement.Context.Value(requireTxContextKey{}) != "request" {
			t.Error("callback 未收到请求 Context")
		}
	}
	if err := db.Callback().Query().Before("gorm:query").Register("test:require_tx_context_query", func(d *gorm.DB) {
		queryCalls++
		observe(d)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register("test:require_tx_context_update", func(d *gorm.DB) {
		updateCalls++
		observe(d)
	}); err != nil {
		t.Fatal(err)
	}
	txCtx := context.WithValue(context.Background(), requireTxContextKey{}, "transaction")
	err := db.WithContext(txCtx).Transaction(func(tx *gorm.DB) error {
		bound, err := repo.RequireTx(tx)
		if err != nil {
			return err
		}
		q, _ := bound.NewQuery(ctx)
		rows, err := bound.List(q)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0] != visible {
			t.Fatalf("rows=%+v，期望仅返回可见租户记录", rows)
		}
		for _, tc := range []struct {
			id       int64
			affected int64
		}{
			{id: visible.ID, affected: 1},
			{id: invisible.ID, affected: 0},
		} {
			u, m := bound.NewUpdater(ctx)
			u.Eq(&m.ID, tc.id).Set(&m.Name, "changed")
			affected, err := bound.UpdateByCond(u)
			if err != nil {
				return err
			}
			if affected != tc.affected {
				t.Fatalf("id=%d affected=%d，期望 %d", tc.id, affected, tc.affected)
			}
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		q, _ = bound.NewQuery(canceled)
		if _, err := bound.List(q); !errors.Is(err, context.Canceled) {
			t.Fatalf("取消请求的 err=%v，期望 context.Canceled", err)
		}
		if err := bound.Save(canceled, &requireTxRow{TenantID: 1, Name: "canceled"}); !errors.Is(err, context.Canceled) {
			t.Fatalf("取消写入的 err=%v，期望 context.Canceled", err)
		}
		if bound.GetDB().Statement.Context != txCtx {
			t.Fatal("单次操作不应修改绑定事务原有 Context")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	visible.Name = "changed"
	var rows []requireTxRow
	if err := db.WithContext(ctx).Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0] != visible || rows[1] != invisible {
		t.Fatalf("rows=%+v，期望仅可见行被更新且取消写入未落库", rows)
	}
	if queryCalls == 0 || updateCalls != 2 {
		t.Fatalf("callback 次数 query=%d update=%d，期望读写均触发", queryCalls, updateCalls)
	}
}

func TestRequireTx_EndedTransactionDoesNotFallback(t *testing.T) {
	for _, tc := range []struct {
		name     string
		prepare  bool
		rollback bool
	}{
		{name: "Committed"},
		{name: "RolledBack", rollback: true},
		{name: "PreparedCommitted", prepare: true},
		{name: "PreparedRolledBack", prepare: true, rollback: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, db := setupTestDB[requireTxRow](t)
			base := db
			if tc.prepare {
				base = db.Session(&gorm.Session{PrepareStmt: true})
			}
			tx := base.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			var err error
			if tc.rollback {
				err = tx.Rollback().Error
			} else {
				err = tx.Commit().Error
			}
			if err != nil {
				t.Fatal(err)
			}
			// 接口能力仍存在，入口不能通过该检查断言事务活跃。
			bound, err := repo.RequireTx(tx)
			if err != nil {
				t.Fatal(err)
			}
			if err := bound.Save(context.Background(), &requireTxRow{TenantID: 1, Name: "after end"}); !errors.Is(err, sql.ErrTxDone) {
				t.Fatalf("err=%v，期望底层 sql.ErrTxDone，不能回退默认 DB", err)
			}
			var count int64
			if err := db.Model(&requireTxRow{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("事务结束后不应插入记录，count=%d", count)
			}
		})
	}
}
