package gplus

import (
	"context"
	"strings"
	"testing"
)

// TestQuery_InSub_Basic 验证 InSub 生成 SQL 形态 + 真实数据命中。
func TestQuery_InSub_Basic(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	// 准备数据：UserA(id=1) Amount=100/200, UserB(id=2) Amount=300, UserC 无订单
	users := []UserWithDelete{{Name: "UserA", Age: 20}, {Name: "UserB", Age: 30}, {Name: "UserC", Age: 25}}
	db.Create(&users)
	db.Create(&Order{UserID: 1, Amount: 100})
	db.Create(&Order{UserID: 1, Amount: 200})
	db.Create(&Order{UserID: 2, Amount: 300})

	// 子查询：所有有订单的 user_id
	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	q, u := NewQuery[UserWithDelete](ctx)
	q.InSub(&u.ID, subQ).Order(&u.ID, true)

	result, err := repo.List(q)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(result) != 2 || result[0].Name != "UserA" || result[1].Name != "UserB" {
		t.Fatalf("expected [UserA, UserB], got %+v", result)
	}
}

// TestQuery_NotInSub_Basic 验证 NotInSub。
func TestQuery_NotInSub_Basic(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	users := []UserWithDelete{{Name: "UserA", Age: 20}, {Name: "UserB", Age: 30}, {Name: "UserC", Age: 25}}
	db.Create(&users)
	db.Create(&Order{UserID: 1, Amount: 100})
	db.Create(&Order{UserID: 2, Amount: 300})

	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	q, u := NewQuery[UserWithDelete](ctx)
	q.NotInSub(&u.ID, subQ)

	result, err := repo.List(q)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(result) != 1 || result[0].Name != "UserC" {
		t.Fatalf("expected [UserC], got %+v", result)
	}
}

// TestQuery_OrInSub 验证 OrInSub 与 AND 条件混用。
func TestQuery_OrInSub(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	users := []UserWithDelete{{Name: "UserA", Age: 20}, {Name: "UserB", Age: 30}, {Name: "UserC", Age: 99}}
	db.Create(&users)
	db.Create(&Order{UserID: 1, Amount: 100})

	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	q, u := NewQuery[UserWithDelete](ctx)
	// age=99 OR id IN (subQ) → UserC（age=99）+ UserA（id IN subQ）
	q.Eq(&u.Age, 99).OrInSub(&u.ID, subQ).Order(&u.ID, true)

	result, err := repo.List(q)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 users, got %d: %+v", len(result), result)
	}
}

// TestQuery_OrNotInSub_DryRun 验证 OrNotInSub 通过 SQL 形态 DryRun。
func TestQuery_OrNotInSub_DryRun(t *testing.T) {
	_, db := setupAdvancedDB(t)
	ctx := context.Background()

	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	q, u := NewQuery[UserWithDelete](ctx)
	q.Eq(&u.Age, 20).OrNotInSub(&u.ID, subQ)

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	if !strings.Contains(sql, "NOT IN") {
		t.Fatalf("expected SQL to contain NOT IN, got: %s", sql)
	}
}

// TestQuery_GtSub_Basic 验证 GtSub: WHERE age > (SELECT AVG(age) FROM users)。
func TestQuery_GtSub_Basic(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	users := []UserWithDelete{{Name: "Young", Age: 20}, {Name: "Avg", Age: 30}, {Name: "Old", Age: 40}}
	db.Create(&users)

	avgQ, _ := NewQuery[UserWithDelete](ctx)
	avgQ.SelectRaw("AVG(age)")

	q, u := NewQuery[UserWithDelete](ctx)
	q.GtSub(&u.Age, avgQ).Order(&u.ID, true)

	result, err := repo.List(q)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	// 平均 age=30，> 30 的只有 Old(40)
	if len(result) != 1 || result[0].Name != "Old" {
		t.Fatalf("expected [Old], got %+v", result)
	}
}

// TestQuery_ScalarSub_DryRun 表驱动覆盖 6 个标量子查询的 SQL 形态。
func TestQuery_ScalarSub_DryRun(t *testing.T) {
	_, db := setupAdvancedDB(t)
	ctx := context.Background()

	tests := []struct {
		name     string
		apply    func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier)
		wantOp   string
	}{
		{"EqSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.EqSub(&u.Age, sub) }, "="},
		{"NeSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.NeSub(&u.Age, sub) }, "<>"},
		{"GtSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.GtSub(&u.Age, sub) }, ">"},
		{"GteSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.GteSub(&u.Age, sub) }, ">="},
		{"LtSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.LtSub(&u.Age, sub) }, "<"},
		{"LteSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.LteSub(&u.Age, sub) }, "<="},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub, _ := NewQuery[UserWithDelete](ctx)
			sub.SelectRaw("AVG(age)")

			q, u := NewQuery[UserWithDelete](ctx)
			tt.apply(q, u, sub)

			sql, err := q.ToSQL(db)
			if err != nil {
				t.Fatalf("ToSQL failed: %v", err)
			}
			if !strings.Contains(sql, tt.wantOp+" (SELECT") {
				t.Fatalf("expected SQL to contain '%s (SELECT', got: %s", tt.wantOp, sql)
			}
		})
	}
}

// TestQuery_OrScalarSub_DryRun 验证 6 个 Or 标量变体 SQL 形态。
func TestQuery_OrScalarSub_DryRun(t *testing.T) {
	_, db := setupAdvancedDB(t)
	ctx := context.Background()

	tests := []struct {
		name  string
		apply func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier)
	}{
		{"OrEqSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.Eq(&u.Age, 0).OrEqSub(&u.Age, sub) }},
		{"OrNeSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.Eq(&u.Age, 0).OrNeSub(&u.Age, sub) }},
		{"OrGtSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.Eq(&u.Age, 0).OrGtSub(&u.Age, sub) }},
		{"OrGteSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.Eq(&u.Age, 0).OrGteSub(&u.Age, sub) }},
		{"OrLtSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.Eq(&u.Age, 0).OrLtSub(&u.Age, sub) }},
		{"OrLteSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.Eq(&u.Age, 0).OrLteSub(&u.Age, sub) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub, _ := NewQuery[UserWithDelete](ctx)
			sub.SelectRaw("AVG(age)")

			q, u := NewQuery[UserWithDelete](ctx)
			tt.apply(q, u, sub)

			sql, err := q.ToSQL(db)
			if err != nil {
				t.Fatalf("ToSQL failed: %v", err)
			}
			if !strings.Contains(strings.ToUpper(sql), "OR ") {
				t.Fatalf("expected SQL to contain OR, got: %s", sql)
			}
		})
	}
}
