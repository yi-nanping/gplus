package gplus

import (
	"context"
	"errors"
	"fmt"
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
	// 回查验证数据库中 age 已真实更新为 99
	var updated TestUser
	if err := db.Where("username = ?", "RawTarget").First(&updated).Error; err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if updated.Age != 99 {
		t.Errorf("expected age=99 after RawExec, got %d", updated.Age)
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

// TestRepository_DeleteByCondTx 测试事务删除
func TestRepository_DeleteByCondTx(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "ToDelete", Age: 1})

	var affected int64
	err := repo.Transaction(ctx, func(tx *gorm.DB) error {
		q, m := NewQuery[TestUser](ctx)
		q.Eq(&m.Name, "ToDelete")
		var txErr error
		affected, txErr = repo.DeleteByCondTx(q, tx)
		return txErr
	})
	if err != nil {
		t.Fatalf("DeleteByCondTx failed: %v", err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 affected, got %d", affected)
	}
}

// TestRepository_UpdateByCondTx 测试事务条件更新
func TestRepository_UpdateByCondTx(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "CondUpdate", Age: 1})

	var affected int64
	err := repo.Transaction(ctx, func(tx *gorm.DB) error {
		u, m := NewUpdater[TestUser](ctx)
		u.Set(&m.Age, 99).Eq(&m.Name, "CondUpdate")
		var txErr error
		affected, txErr = repo.UpdateByCondTx(u, tx)
		return txErr
	})
	if err != nil {
		t.Fatalf("UpdateByCondTx failed: %v", err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 affected, got %d", affected)
	}

	_ = ctx
}

// ---- 复杂 SQL 场景测试辅助结构体 ----

type AgeGroup struct {
	Age int `gorm:"column:age"`
	Cnt int `gorm:"column:cnt"`
}

type AggregateStat struct {
	TotalAge float64 `gorm:"column:total_age"`
	AvgScore float64 `gorm:"column:avg_score"`
}

type UserOrderResult struct {
	Username  string `gorm:"column:username"`
	OrderName string `gorm:"column:order_name"`
}

// TestRepository_RawScan_GroupBy GROUP BY + COUNT
func TestRepository_RawScan_GroupBy(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "A", Age: 20})
	db.Create(&TestUser{Name: "B", Age: 20})
	db.Create(&TestUser{Name: "C", Age: 30})

	var groups []AgeGroup
	err := repo.RawScan(ctx, &groups,
		"SELECT age, COUNT(*) as cnt FROM test_users GROUP BY age ORDER BY age")
	if err != nil {
		t.Fatalf("RawScan GroupBy failed: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0].Age != 20 || groups[0].Cnt != 2 {
		t.Fatalf("unexpected group[0]: %+v", groups[0])
	}
	if groups[1].Age != 30 || groups[1].Cnt != 1 {
		t.Fatalf("unexpected group[1]: %+v", groups[1])
	}
}

// TestRepository_RawScan_Having GROUP BY + HAVING 过滤分组
func TestRepository_RawScan_Having(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "A", Age: 20})
	db.Create(&TestUser{Name: "B", Age: 20})
	db.Create(&TestUser{Name: "C", Age: 30})

	var groups []AgeGroup
	err := repo.RawScan(ctx, &groups,
		"SELECT age, COUNT(*) as cnt FROM test_users GROUP BY age HAVING cnt > ?", 1)
	if err != nil {
		t.Fatalf("RawScan Having failed: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group with cnt>1, got %d", len(groups))
	}
	if groups[0].Age != 20 {
		t.Fatalf("expected age=20, got %d", groups[0].Age)
	}
}

// TestRepository_RawQuery_Subquery 子查询：age 大于全表平均值
func TestRepository_RawQuery_Subquery(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "Young", Age: 10})
	db.Create(&TestUser{Name: "Mid", Age: 20})
	db.Create(&TestUser{Name: "Old", Age: 30})

	// avg=20，仅 age=30 满足
	results, err := repo.RawQuery(ctx,
		"SELECT * FROM test_users WHERE age > (SELECT AVG(age) FROM test_users) ORDER BY age")
	if err != nil {
		t.Fatalf("RawQuery subquery failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "Old" {
		t.Fatalf("expected Old, got %s", results[0].Name)
	}
}

// TestRepository_RawQuery_MultiCondition AND + OR 多条件组合
func TestRepository_RawQuery_MultiCondition(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "Alice", Age: 25})
	db.Create(&TestUser{Name: "Bob", Age: 30})
	db.Create(&TestUser{Name: "Charlie", Age: 35})

	results, err := repo.RawQuery(ctx,
		"SELECT * FROM test_users WHERE age > ? AND (username = ? OR username = ?) ORDER BY username",
		24, "Alice", "Bob")
	if err != nil {
		t.Fatalf("RawQuery multi-condition failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Name != "Alice" || results[1].Name != "Bob" {
		t.Fatalf("unexpected names: %s, %s", results[0].Name, results[1].Name)
	}
}

// TestRepository_RawScan_Aggregate 聚合函数：SUM + AVG
func TestRepository_RawScan_Aggregate(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.Create(&TestUser{Name: "A", Age: 10, Score: 1.5})
	db.Create(&TestUser{Name: "B", Age: 20, Score: 2.5})
	db.Create(&TestUser{Name: "C", Age: 30, Score: 3.0})

	var stat AggregateStat
	err := repo.RawScan(ctx, &stat,
		"SELECT SUM(age) as total_age, AVG(score) as avg_score FROM test_users")
	if err != nil {
		t.Fatalf("RawScan aggregate failed: %v", err)
	}
	if stat.TotalAge != 60 {
		t.Fatalf("expected total_age=60, got %v", stat.TotalAge)
	}
	// avg_score = (1.5+2.5+3.0)/3 ≈ 2.333
	if stat.AvgScore < 2.3 || stat.AvgScore > 2.4 {
		t.Fatalf("unexpected avg_score: %v", stat.AvgScore)
	}
}

// TestRepository_RawQuery_LimitOffset 分页：LIMIT + OFFSET
func TestRepository_RawQuery_LimitOffset(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		db.Create(&TestUser{Name: fmt.Sprintf("User%d", i), Age: i * 10})
	}

	// offset=1 跳过 age=10，取 age=20,30
	results, err := repo.RawQuery(ctx,
		"SELECT * FROM test_users ORDER BY age LIMIT ? OFFSET ?", 2, 1)
	if err != nil {
		t.Fatalf("RawQuery LimitOffset failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Age != 20 {
		t.Fatalf("expected age=20, got %d", results[0].Age)
	}
	if results[1].Age != 30 {
		t.Fatalf("expected age=30, got %d", results[1].Age)
	}
}

// TestRepository_RawScan_Join 多表 INNER JOIN
func TestRepository_RawScan_Join(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.AutoMigrate(&testOrder{})
	truncateTables(t, db, &testOrder{})
	t.Cleanup(func() { truncateTables(t, db, &testOrder{}) })

	user := &TestUser{Name: "Alice", Age: 25}
	db.Create(user)
	db.Create(&testOrder{UserID: user.ID, OrderName: "OrderA"})
	db.Create(&testOrder{UserID: user.ID, OrderName: "OrderB"})

	var results []UserOrderResult
	err := repo.RawScan(ctx, &results,
		`SELECT u.username, o.order_name
		 FROM test_users u
		 INNER JOIN test_orders o ON o.user_id = u.id
		 ORDER BY o.order_name`)
	if err != nil {
		t.Fatalf("RawScan join failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 join results, got %d", len(results))
	}
	if results[0].Username != "Alice" || results[0].OrderName != "OrderA" {
		t.Fatalf("unexpected result[0]: %+v", results[0])
	}
	if results[1].OrderName != "OrderB" {
		t.Fatalf("unexpected result[1]: %+v", results[1])
	}
}

// TestRepository_RawScan_JoinGroupBy 多表 JOIN + GROUP BY + HAVING
func TestRepository_RawScan_JoinGroupBy(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	db.AutoMigrate(&testOrder{})
	truncateTables(t, db, &testOrder{})
	t.Cleanup(func() { truncateTables(t, db, &testOrder{}) })

	u1 := &TestUser{Name: "Alice", Age: 25}
	u2 := &TestUser{Name: "Bob", Age: 30}
	db.Create(u1)
	db.Create(u2)
	db.Create(&testOrder{UserID: u1.ID, OrderName: "O1"})
	db.Create(&testOrder{UserID: u1.ID, OrderName: "O2"})
	db.Create(&testOrder{UserID: u2.ID, OrderName: "O3"})

	// 只返回订单数 > 1 的用户
	type UserOrderCount struct {
		Username   string `gorm:"column:username"`
		OrderCount int    `gorm:"column:order_count"`
	}
	var results []UserOrderCount
	err := repo.RawScan(ctx, &results,
		`SELECT u.username, COUNT(o.id) as order_count
		 FROM test_users u
		 INNER JOIN test_orders o ON o.user_id = u.id
		 GROUP BY u.id, u.username
		 HAVING order_count > ?
		 ORDER BY u.username`, 1)
	if err != nil {
		t.Fatalf("RawScan JoinGroupBy failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Username != "Alice" || results[0].OrderCount != 2 {
		t.Fatalf("unexpected result: %+v", results[0])
	}
}
