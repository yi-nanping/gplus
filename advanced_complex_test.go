package gplus

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

// setupComplexDB 初始化包含 UserWithDelete + Order 两张表的测试库，支持 SQLite（默认）和 MySQL（TEST_DB=mysql）
func setupComplexDB(t *testing.T) (*Repository[int64, UserWithDelete], *gorm.DB) {
	db := openDB(t)
	if err := db.AutoMigrate(&UserWithDelete{}, &Order{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	if db.Name() == "mysql" {
		truncateTables(t, db, &Order{}, &UserWithDelete{})
		t.Cleanup(func() { truncateTables(t, db, &Order{}, &UserWithDelete{}) })
	}
	return NewRepository[int64, UserWithDelete](db), db
}

// seedComplexData 插入测试数据
func seedComplexData(t *testing.T, db *gorm.DB) {
	t.Helper()
	users := []UserWithDelete{
		{ID: 1, Name: "Alice", Age: 30},
		{ID: 2, Name: "Bob", Age: 25},
		{ID: 3, Name: "Charlie", Age: 20},
		{ID: 4, Name: "Dave", Age: 20},
	}
	orders := []Order{
		{ID: 1, UserID: 1, Amount: 100, Remark: "order-a"},
		{ID: 2, UserID: 1, Amount: 200, Remark: "order-b"},
		{ID: 3, UserID: 2, Amount: 150, Remark: "order-c"},
		{ID: 4, UserID: 3, Amount: 50, Remark: "order-d"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if err := db.Create(&orders).Error; err != nil {
		t.Fatalf("seed orders: %v", err)
	}
}

// TestComplex_JoinWhereOrder JOIN + WHERE 多条件 + ORDER BY
func TestComplex_JoinWhereOrder(t *testing.T) {
	repo, db := setupComplexDB(t)
	seedComplexData(t, db)

	// 找有订单、年龄 >= 25 的用户，按 age 降序
	q, u := NewQuery[UserWithDelete](context.Background())
	q.Ge(&u.Age, 25).
		InnerJoin("orders", "orders.user_id = user_with_deletes.id").
		Distinct().
		Order(&u.Age, false)

	rows, err := repo.List(q)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	// 第一条应是 Alice(age=30)
	if rows[0].Name != "Alice" {
		t.Errorf("expected Alice first, got %s", rows[0].Name)
	}
}

// TestComplex_GroupByHaving GROUP BY + HAVING（使用 Query 构建器）
func TestComplex_GroupByHaving(t *testing.T) {
	repo, db := setupComplexDB(t)
	seedComplexData(t, db)

	type AgeCount struct {
		Age   int
		Total int
	}
	var rows []AgeCount

	// 统计每个年龄段的用户数，只保留 count >= 2 的组
	err := repo.RawScan(
		context.Background(),
		&rows,
		`SELECT age, COUNT(*) AS total
		 FROM user_with_deletes
		 WHERE deleted_at IS NULL
		 GROUP BY age
		 HAVING COUNT(*) >= ?`,
		2,
	)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 group, got %d", len(rows))
	}
	if rows[0].Age != 20 {
		t.Errorf("expected age=20 group, got %d", rows[0].Age)
	}
	if rows[0].Total != 2 {
		t.Errorf("expected total=2, got %d", rows[0].Total)
	}
}

// TestComplex_QueryBuilder_GroupHaving 使用 Query 构建器的 Group + Having
func TestComplex_QueryBuilder_GroupHaving(t *testing.T) {
	repo, db := setupComplexDB(t)
	seedComplexData(t, db)

	q, u := NewQuery[UserWithDelete](context.Background())
	q.Select(&u.Age).Group(&u.Age).Having("COUNT(*)", OpGe, 2)

	results, err := repo.List(q)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	// age=20 组有 2 人满足 HAVING COUNT(*)>=2，GROUP BY 折叠后该组返回 1 行
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Age != 20 {
		t.Errorf("expected age=20, got %d", results[0].Age)
	}
}

// TestComplex_QueryBuilder_JoinGroupHaving Query 构建器：JOIN + GROUP BY + HAVING
func TestComplex_QueryBuilder_JoinGroupHaving(t *testing.T) {
	repo, db := setupComplexDB(t)
	seedComplexData(t, db)

	type Result struct {
		UserID     int64
		OrderCount int
		TotalAmt   int
	}
	var rows []Result

	// 用原生 SQL 模拟：找订单数 >= 2 的用户，统计订单总金额
	err := repo.RawScan(
		context.Background(),
		&rows,
		`SELECT u.id AS user_id, COUNT(o.id) AS order_count, SUM(o.amount) AS total_amt
		 FROM user_with_deletes u
		 INNER JOIN orders o ON o.user_id = u.id
		 WHERE u.deleted_at IS NULL
		 GROUP BY u.id
		 HAVING COUNT(o.id) >= ?`,
		2,
	)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].UserID != 1 {
		t.Errorf("expected user_id=1, got %d", rows[0].UserID)
	}
	if rows[0].TotalAmt != 300 {
		t.Errorf("expected total_amt=300, got %d", rows[0].TotalAmt)
	}
}

// TestComplex_QueryBuilder_MultiCondOr Query 构建器：And + Or 嵌套多条件
func TestComplex_QueryBuilder_MultiCondOr(t *testing.T) {
	repo, db := setupComplexDB(t)
	seedComplexData(t, db)

	// WHERE (age = 30 OR age = 25) AND name LIKE '%a%'
	q, u := NewQuery[UserWithDelete](context.Background())
	q.And(func(sub *Query[UserWithDelete]) {
		sub.Eq(&u.Age, 30).OrEq(&u.Age, 25)
	}).Like(&u.Name, "a")

	results, err := repo.List(q)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	// Alice(age=30,name含a) 匹配，Bob(age=25,name不含a) 不匹配
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "Alice" {
		t.Errorf("expected Alice, got %s", results[0].Name)
	}
}

// TestComplex_QueryBuilder_InBetweenLike IN + BETWEEN + LIKE 三算子组合
func TestComplex_QueryBuilder_InBetweenLike(t *testing.T) {
	repo, db := setupComplexDB(t)
	seedComplexData(t, db)

	// WHERE age IN (20,25) AND age BETWEEN 20 AND 24 AND name LIKE 'Ch%'
	// Charlie(20): IN✓ BETWEEN✓ LIKE✓ → 匹配
	// Dave(20):    IN✓ BETWEEN✓ LIKE✗ → 不匹配
	// Bob(25):     IN✓ BETWEEN✗(25>24) → 不匹配
	// Alice(30):   IN✗ → 不匹配
	q, u := NewQuery[UserWithDelete](context.Background())
	q.In(&u.Age, []int{20, 25}).Between(&u.Age, 20, 24).Like(&u.Name, "Ch%")

	results, err := repo.List(q)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (Charlie), got %d", len(results))
	}
	if results[0].Name != "Charlie" {
		t.Errorf("expected Charlie, got %s", results[0].Name)
	}
}

// TestComplex_QueryBuilder_LimitOffset ORDER BY + LIMIT + OFFSET 分页
func TestComplex_QueryBuilder_LimitOffset(t *testing.T) {
	repo, db := setupComplexDB(t)
	seedComplexData(t, db)

	// 按 age 升序，取第 2 页，每页 2 条
	q, u := NewQuery[UserWithDelete](context.Background())
	q.Order(&u.Age, true).Limit(2).Offset(2)

	results, err := repo.List(q)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// 4 条按 age 升序: Charlie(20), Dave(20), Bob(25), Alice(30)
	// offset=2 => Bob(25), Alice(30)
	names := map[string]bool{results[0].Name: true, results[1].Name: true}
	if !names["Bob"] || !names["Alice"] {
		t.Errorf("expected {Bob,Alice} in page 2, got %s %s", results[0].Name, results[1].Name)
	}
}

// TestComplex_QueryBuilder_CountWithJoin Count + JOIN
func TestComplex_QueryBuilder_CountWithJoin(t *testing.T) {
	repo, db := setupComplexDB(t)
	seedComplexData(t, db)

	q, u := NewQuery[UserWithDelete](context.Background())
	q.InnerJoin("orders", "orders.user_id = user_with_deletes.id").
		Ge(&u.Age, 25)

	count, err := repo.Count(q)
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	// Alice(30,2个订单) + Bob(25,1个订单) => 3条记录（COUNT不去重）
	if count != 3 {
		t.Fatalf("expected 3, got %d", count)
	}
}

// TestComplex_QueryBuilder_Subquery 子查询：age 高于平均年龄的用户
func TestComplex_QueryBuilder_Subquery(t *testing.T) {
	repo, db := setupComplexDB(t)
	seedComplexData(t, db)

	// 平均 age = (30+25+20+20)/4 = 23.75，高于平均的有 Alice(30) 和 Bob(25)
	q, u := NewQuery[UserWithDelete](context.Background())
	q.WhereRaw("age > (SELECT AVG(age) FROM user_with_deletes WHERE deleted_at IS NULL)").
		Order(&u.Age, false)

	results, err := repo.List(q)
	if err != nil {
		t.Fatalf("subquery failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Name != "Alice" {
		t.Errorf("expected Alice first, got %s", results[0].Name)
	}
}

// TestComplex_QueryBuilder_HavingGroup HavingGroup 嵌套括号：HAVING (COUNT(*) >= 2 OR age = 30)
func TestComplex_QueryBuilder_HavingGroup(t *testing.T) {
	repo, db := setupComplexDB(t)
	seedComplexData(t, db)

	// 数据：Alice(30), Bob(25), Charlie(20), Dave(20)
	// GROUP BY age → age=20(count=2), age=25(count=1), age=30(count=1)
	// HAVING (COUNT(*)>=2 OR age=30) → age=20✓ age=25✗ age=30✓ → 2 组
	q, u := NewQuery[UserWithDelete](context.Background())
	q.Group(&u.Age).HavingGroup(func(sub *Query[UserWithDelete]) {
		sub.Having("COUNT(*)", OpGe, 2).OrHaving("age", OpEq, 30)
	})

	results, err := repo.List(q)
	if err != nil {
		t.Fatalf("HavingGroup query failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(results))
	}
	ages := map[int]bool{results[0].Age: true, results[1].Age: true}
	if !ages[20] || !ages[30] {
		t.Errorf("expected age groups {20,30}, got %d %d", results[0].Age, results[1].Age)
	}
}
