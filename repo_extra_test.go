package gplus

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

// TestRepository_WithTx 验证 WithTx 返回绑定事务的新实例
func TestRepository_WithTx(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	txRepo := repo.WithTx(db)
	if txRepo == nil {
		t.Fatal("WithTx should return non-nil repo")
	}
	if txRepo.db != db {
		t.Fatal("WithTx should bind the provided db")
	}
}

// TestRepository_GetDB 验证 GetDB 返回内部 db
func TestRepository_GetDB(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	if repo.GetDB() == nil {
		t.Fatal("GetDB should return non-nil db")
	}
}

// TestRepository_Transaction_Commit 验证事务提交
func TestRepository_Transaction_Commit(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	err := repo.Transaction(ctx, func(tx *gorm.DB) error {
		return tx.Create(&TestUser{Name: "TxUser", Age: 30}).Error
	})
	if err != nil {
		t.Fatalf("Transaction commit failed: %v", err)
	}

	q, _ := NewQuery[TestUser](ctx)
	users, _ := repo.List(q)
	if len(users) == 0 {
		t.Fatal("committed user should be persisted")
	}
}

// TestRepository_Transaction_Rollback 验证事务回滚
func TestRepository_Transaction_Rollback(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	_ = repo.Transaction(ctx, func(tx *gorm.DB) error {
		tx.Create(&TestUser{Name: "RollbackUser", Age: 99})
		return errors.New("force rollback")
	})

	q2, _ := NewQuery[TestUser](ctx)
	users, _ := repo.List(q2)
	for _, u := range users {
		if u.Name == "RollbackUser" {
			t.Fatal("rolled back user should not be persisted")
		}
	}
}

// TestRepository_RawExec 测试 RawExec 正常执行
func TestRepository_RawExec(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "RawTarget", Age: 10})

	affected, err := repo.RawExec(ctx, "UPDATE test_users SET age = ? WHERE username = ?", 99, "RawTarget")
	if err != nil {
		t.Fatalf("RawExec failed: %v", err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 affected row, got %d", affected)
	}
}

// TestRepository_RawExec_EmptySQL 空 SQL 返回 ErrRawSQLEmpty
func TestRepository_RawExec_EmptySQL(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	_, err := repo.RawExec(context.Background(), "")
	if !errors.Is(err, ErrRawSQLEmpty) {
		t.Fatalf("expected ErrRawSQLEmpty, got %v", err)
	}
}

// TestRepository_RawQuery 测试 RawQuery 映射到实体切片
func TestRepository_RawQuery(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "RawQ", Age: 5})

	results, err := repo.RawQuery(ctx, "SELECT * FROM test_users WHERE username = ?", "RawQ")
	if err != nil {
		t.Fatalf("RawQuery failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "RawQ" {
		t.Fatalf("unexpected results: %v", results)
	}
}

// TestRepository_RawQuery_EmptySQL 空 SQL 返回 ErrRawSQLEmpty
func TestRepository_RawQuery_EmptySQL(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	_, err := repo.RawQuery(context.Background(), "")
	if !errors.Is(err, ErrRawSQLEmpty) {
		t.Fatalf("expected ErrRawSQLEmpty, got %v", err)
	}
}

// TestRepository_RawScan 测试 RawScan 映射到任意结构体
func TestRepository_RawScan(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "ScanUser", Age: 7})

	var count int64
	err := repo.RawScan(ctx, &count, "SELECT COUNT(*) FROM test_users")
	if err != nil {
		t.Fatalf("RawScan failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count=1, got %d", count)
	}
}

// TestRepository_RawScan_EmptySQL 空 SQL 返回 ErrRawSQLEmpty
func TestRepository_RawScan_EmptySQL(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	var dest int64
	err := repo.RawScan(context.Background(), &dest, "")
	if !errors.Is(err, ErrRawSQLEmpty) {
		t.Fatalf("expected ErrRawSQLEmpty, got %v", err)
	}
}

// TestRepository_RawExecTx 测试 RawExecTx 在事务中执行
func TestRepository_RawExecTx(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "TxExec", Age: 1})

	var affected int64
	err := repo.Transaction(ctx, func(tx *gorm.DB) error {
		var txErr error
		affected, txErr = repo.RawExecTx(ctx, tx, "UPDATE test_users SET age = ? WHERE username = ?", 50, "TxExec")
		return txErr
	})
	if err != nil {
		t.Fatalf("RawExecTx failed: %v", err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 affected, got %d", affected)
	}
}

// TestRepository_RawExecTx_EmptySQL 空 SQL 返回错误
func TestRepository_RawExecTx_EmptySQL(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	_, err := repo.RawExecTx(context.Background(), nil, "")
	if !errors.Is(err, ErrRawSQLEmpty) {
		t.Fatalf("expected ErrRawSQLEmpty, got %v", err)
	}
}

// TestRepository_RawScanTx 测试 RawScanTx 在事务中映射结果
func TestRepository_RawScanTx(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "TxScan", Age: 3})

	var count int64
	err := repo.Transaction(ctx, func(tx *gorm.DB) error {
		return repo.RawScanTx(ctx, tx, &count, "SELECT COUNT(*) FROM test_users")
	})
	if err != nil {
		t.Fatalf("RawScanTx failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count=1, got %d", count)
	}
}

// TestRepository_RawScanTx_EmptySQL 空 SQL 返回错误
func TestRepository_RawScanTx_EmptySQL(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	var dest int64
	err := repo.RawScanTx(context.Background(), nil, &dest, "")
	if !errors.Is(err, ErrRawSQLEmpty) {
		t.Fatalf("expected ErrRawSQLEmpty, got %v", err)
	}
}

// TestRepository_RawQueryTx_EmptySQL 空 SQL 返回错误
func TestRepository_RawQueryTx_EmptySQL(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	_, err := repo.RawQueryTx(context.Background(), nil, "")
	if !errors.Is(err, ErrRawSQLEmpty) {
		t.Fatalf("expected ErrRawSQLEmpty, got %v", err)
	}
}

// TestRepository_RawQueryTx 测试 RawQueryTx 在事务中查询
func TestRepository_RawQueryTx(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "TxQueryUser", Age: 8})

	var results []TestUser
	err := repo.Transaction(ctx, func(tx *gorm.DB) error {
		var txErr error
		results, txErr = repo.RawQueryTx(ctx, tx, "SELECT * FROM test_users WHERE username = ?", "TxQueryUser")
		return txErr
	})
	if err != nil {
		t.Fatalf("RawQueryTx failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

// TestRepository_DeleteByCondTX 测试事务删除
func TestRepository_DeleteByCondTX(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "ToDelete", Age: 1})

	var affected int64
	err := repo.Transaction(ctx, func(tx *gorm.DB) error {
		q, m := NewQuery[TestUser](ctx)
		q.Eq(&m.Name, "ToDelete")
		var txErr error
		affected, txErr = repo.DeleteByCondTX(q, tx)
		return txErr
	})
	if err != nil {
		t.Fatalf("DeleteByCondTX failed: %v", err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 affected, got %d", affected)
	}
}

// TestRepository_UpdateByCondTX 测试事务条件更新
func TestRepository_UpdateByCondTX(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "CondUpdate", Age: 1})

	var affected int64
	err := repo.Transaction(ctx, func(tx *gorm.DB) error {
		u, m := NewUpdater[TestUser](ctx)
		u.Set(&m.Age, 99).Eq(&m.Name, "CondUpdate")
		var txErr error
		affected, txErr = repo.UpdateByCondTX(u, tx)
		return txErr
	})
	if err != nil {
		t.Fatalf("UpdateByCondTX failed: %v", err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 affected, got %d", affected)
	}

	_ = ctx
}
