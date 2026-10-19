package gplus_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
)

type typedBuilderAccount struct {
	ID       int64  `gorm:"column:account_key;primaryKey;autoIncrement"`
	TenantID int    `gorm:"column:tenant_key;index"`
	TeamID   int64  `gorm:"column:team_ref"`
	Name     string `gorm:"column:display_name"`
	Score    int64  `gorm:"column:score_points"`
	Attempts int64  `gorm:"column:retry_total"`
	Version  int64  `gorm:"column:lock_version"`
}

func (typedBuilderAccount) TableName() string { return "typed_builder_accounts" }

type typedBuilderTeam struct {
	ID    int64  `gorm:"column:team_key;primaryKey;autoIncrement"`
	Label string `gorm:"column:team_label"`
}

func (typedBuilderTeam) TableName() string { return "typed_builder_teams" }

type typedBuilderEvent struct {
	ID        int64          `gorm:"column:event_key;primaryKey;autoIncrement"`
	AccountID int64          `gorm:"column:owner_ref;index"`
	DeletedAt gorm.DeletedAt `gorm:"column:removed_at;index"`
}

func (typedBuilderEvent) TableName() string { return "typed_builder_events" }

func setupTypedBuilderDB(t *testing.T) (*Repository[int64, typedBuilderAccount], *gorm.DB) {
	t.Helper()
	db := openDB(t)
	if err := db.AutoMigrate(&typedBuilderTeam{}, &typedBuilderAccount{}, &typedBuilderEvent{}); err != nil {
		t.Fatalf("迁移 typed builder 验收表失败: %v", err)
	}
	if db.Name() == "mysql" || db.Name() == "postgres" {
		truncateTables(t, db, &typedBuilderEvent{}, &typedBuilderAccount{}, &typedBuilderTeam{})
		t.Cleanup(func() {
			truncateTables(t, db, &typedBuilderEvent{}, &typedBuilderAccount{}, &typedBuilderTeam{})
		})
	}
	return NewRepository[int64, typedBuilderAccount](db), db
}

func seedTypedBuilderAccounts(t *testing.T, db *gorm.DB) (typedBuilderAccount, typedBuilderAccount) {
	t.Helper()
	rows := []typedBuilderAccount{
		{TenantID: 1, Name: "inside", Score: 10, Attempts: 1, Version: 1},
		{TenantID: 2, Name: "outside", Score: 20, Attempts: 2, Version: 1},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("写入 typed builder 账号失败: %v", err)
	}
	return rows[0], rows[1]
}

func getTypedBuilderAccount(t *testing.T, repo *Repository[int64, typedBuilderAccount], id int64) typedBuilderAccount {
	t.Helper()
	row, err := repo.GetById(context.Background(), id)
	if err != nil {
		t.Fatalf("读取 typed builder 账号 %d 失败: %v", id, err)
	}
	return row
}

func TestTypedUpdaterIncr_AtomicUpdateGuardsAndCompatibility(t *testing.T) {
	t.Run("SetTwoIncrAndCASUseOneUpdate", func(t *testing.T) {
		repo, db := setupTypedBuilderDB(t)
		inside, _ := seedTypedBuilderAccounts(t, db)

		var updateCalls atomic.Int32
		callbackName := fmt.Sprintf("typed_builder:update:%p", &updateCalls)
		if err := db.Callback().Update().After("gorm:update").Register(callbackName, func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Table == inside.TableName() {
				updateCalls.Add(1)
			}
		}); err != nil {
			t.Fatalf("注册 UPDATE callback 失败: %v", err)
		}
		t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })

		u, m := NewUpdater[typedBuilderAccount](context.Background())
		u.Set(&m.Name, "committed").
			Incr(&m.Score, 5).
			Incr(&m.Attempts, 2).
			Set(&m.Version, int64(2)).
			Eq(&m.ID, inside.ID).
			Eq(&m.Version, int64(1))
		affected, err := repo.UpdateByCond(u)
		if err != nil || affected != 1 {
			t.Fatalf("原子更新 affected=%d err=%v，期望 1/nil", affected, err)
		}
		if calls := updateCalls.Load(); calls != 1 {
			t.Fatalf("原子更新 callback 次数=%d，期望只执行一条 UPDATE", calls)
		}
		got := getTypedBuilderAccount(t, repo, inside.ID)
		if got.Name != "committed" || got.Score != 15 || got.Attempts != 3 || got.Version != 2 {
			t.Fatalf("原子更新结果=%+v，期望名称、两个计数和版本同时更新", got)
		}

		stale, sm := NewUpdater[typedBuilderAccount](context.Background())
		stale.Incr(&sm.Score, 100).Eq(&sm.ID, inside.ID).Eq(&sm.Version, int64(1))
		affected, err = repo.UpdateByCond(stale)
		if err != nil || affected != 0 {
			t.Fatalf("过期 CAS affected=%d err=%v，期望 0/nil", affected, err)
		}
		got = getTypedBuilderAccount(t, repo, inside.ID)
		if got.Score != 15 || got.Version != 2 {
			t.Fatalf("过期 CAS 不应修改数据，实际=%+v", got)
		}
	})

	t.Run("SelectAndOmitFilterIncrementAssignments", func(t *testing.T) {
		repo, db := setupTypedBuilderDB(t)
		inside, _ := seedTypedBuilderAccounts(t, db)
		u, m := NewUpdater[typedBuilderAccount](context.Background())
		u.Set(&m.Name, "must-stay").
			Incr(&m.Score, 4).
			Incr(&m.Attempts, 9).
			Select(&m.Name, &m.Score, &m.Attempts).
			Omit(&m.Name, &m.Attempts).
			Eq(&m.ID, inside.ID)
		affected, err := repo.UpdateByCond(u)
		if err != nil || affected != 1 {
			t.Fatalf("Select/Omit 自增 affected=%d err=%v", affected, err)
		}
		got := getTypedBuilderAccount(t, repo, inside.ID)
		if got.Name != "inside" || got.Score != 14 || got.Attempts != 1 {
			t.Fatalf("Select/Omit 过滤结果=%+v，期望仅 score_points 自增", got)
		}
	})

	t.Run("DataRuleGroupsBusinessOR", func(t *testing.T) {
		repo, db := setupTypedBuilderDB(t)
		inside, outside := seedTypedBuilderAccounts(t, db)
		ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{
			{Column: "tenant_key", Condition: "=", Value: "1"},
		})
		u, m := NewUpdater[typedBuilderAccount](ctx)
		u.Incr(&m.Score, 1).Eq(&m.Name, "inside").OrEq(&m.Name, "outside")
		affected, err := repo.UpdateByCond(u)
		if err != nil || affected != 1 {
			t.Fatalf("DataRule + OR affected=%d err=%v，期望只更新租户 1", affected, err)
		}
		if got := getTypedBuilderAccount(t, repo, inside.ID); got.Score != 11 {
			t.Fatalf("授权行 score=%d，期望 11", got.Score)
		}
		if got := getTypedBuilderAccount(t, repo, outside.ID); got.Score != 20 {
			t.Fatalf("OR 不得绕过 DataRule，越权行 score=%d", got.Score)
		}
	})

	t.Run("CanceledContextDoesNotModify", func(t *testing.T) {
		repo, db := setupTypedBuilderDB(t)
		inside, _ := seedTypedBuilderAccounts(t, db)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		u, m := NewUpdater[typedBuilderAccount](ctx)
		u.Incr(&m.Score, 7).Eq(&m.ID, inside.ID)
		affected, err := repo.UpdateByCond(u)
		if !errors.Is(err, context.Canceled) || affected != 0 {
			t.Fatalf("取消上下文 affected=%d err=%v，期望 0/context.Canceled", affected, err)
		}
		if got := getTypedBuilderAccount(t, repo, inside.ID); got.Score != 10 {
			t.Fatalf("取消上下文后 score=%d，期望保持 10", got.Score)
		}
	})

	t.Run("TransactionRollbackRestoresAllAssignments", func(t *testing.T) {
		repo, db := setupTypedBuilderDB(t)
		inside, _ := seedTypedBuilderAccounts(t, db)
		rollback := errors.New("typed builder rollback")
		err := db.Transaction(func(tx *gorm.DB) error {
			u, m := NewUpdater[typedBuilderAccount](context.Background())
			u.Set(&m.Name, "rolled-back").Incr(&m.Score, 8).Eq(&m.ID, inside.ID)
			affected, err := repo.UpdateByCondTx(u, tx)
			if err != nil || affected != 1 {
				t.Fatalf("事务内更新 affected=%d err=%v", affected, err)
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatalf("事务错误=%v，期望 rollback sentinel", err)
		}
		got := getTypedBuilderAccount(t, repo, inside.ID)
		if got.Name != "inside" || got.Score != 10 {
			t.Fatalf("事务回滚后数据=%+v，期望恢复初值", got)
		}
	})

	t.Run("IncrByKeepsIgnoringSetMap", func(t *testing.T) {
		repo, db := setupTypedBuilderDB(t)
		inside, _ := seedTypedBuilderAccounts(t, db)
		u, m := NewUpdater[typedBuilderAccount](context.Background())
		u.Set(&m.Name, "must-be-ignored").Eq(&m.ID, inside.ID)
		affected, err := repo.IncrBy(u, &m.Score, 3)
		if err != nil || affected != 1 {
			t.Fatalf("IncrBy affected=%d err=%v", affected, err)
		}
		got := getTypedBuilderAccount(t, repo, inside.ID)
		if got.Name != "inside" || got.Score != 13 {
			t.Fatalf("IncrBy 兼容结果=%+v，期望忽略 Set 且只自增 score", got)
		}
	})
}
