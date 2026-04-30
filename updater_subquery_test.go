package gplus

import (
	"context"
	"strings"
	"testing"
)

// TestUpdater_AllSub_DryRun 表驱动覆盖 16 个 Updater 子查询方法 SQL 形态。
func TestUpdater_AllSub_DryRun(t *testing.T) {
	_, db := setupAdvancedDB(t)
	ctx := context.Background()

	tests := []struct {
		name   string
		apply  func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier)
		wantOp string
	}{
		{"InSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.InSub(&m.ID, sub) }, "IN"},
		{"NotInSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.NotInSub(&m.ID, sub) }, "NOT IN"},
		{"EqSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.EqSub(&m.Age, sub) }, "="},
		{"NeSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.NeSub(&m.Age, sub) }, "<>"},
		{"GtSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.GtSub(&m.Age, sub) }, ">"},
		{"GteSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.GteSub(&m.Age, sub) }, ">="},
		{"LtSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.LtSub(&m.Age, sub) }, "<"},
		{"LteSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.LteSub(&m.Age, sub) }, "<="},
		{"OrInSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.Eq(&m.Age, 0).OrInSub(&m.ID, sub) }, "IN"},
		{"OrNotInSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.Eq(&m.Age, 0).OrNotInSub(&m.ID, sub) }, "NOT IN"},
		{"OrEqSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.Eq(&m.Age, 0).OrEqSub(&m.Age, sub) }, "="},
		{"OrNeSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.Eq(&m.Age, 0).OrNeSub(&m.Age, sub) }, "<>"},
		{"OrGtSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.Eq(&m.Age, 0).OrGtSub(&m.Age, sub) }, ">"},
		{"OrGteSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.Eq(&m.Age, 0).OrGteSub(&m.Age, sub) }, ">="},
		{"OrLtSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.Eq(&m.Age, 0).OrLtSub(&m.Age, sub) }, "<"},
		{"OrLteSub", func(u *Updater[UserWithDelete], m *UserWithDelete, sub Subquerier) { u.Eq(&m.Age, 0).OrLteSub(&m.Age, sub) }, "<="},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subQ, order := NewQuery[Order](ctx)
			subQ.Select(&order.UserID)

			u, m := NewUpdater[UserWithDelete](ctx)
			u.Set(&m.Name, "X")
			tt.apply(u, m, subQ)

			sql, err := u.ToSQL(db)
			if err != nil {
				t.Fatalf("ToSQL failed: %v", err)
			}
			if !strings.Contains(strings.ToUpper(sql), tt.wantOp) {
				t.Fatalf("expected SQL to contain %q, got: %s", tt.wantOp, sql)
			}
		})
	}
}

// TestUpdater_InSub_RealUpdate 真实 UPDATE WHERE id IN (subquery)。
func TestUpdater_InSub_RealUpdate(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	users := []UserWithDelete{{Name: "UserA", Age: 20}, {Name: "UserB", Age: 30}, {Name: "UserC", Age: 25}}
	db.Create(&users)
	db.Create(&Order{UserID: 1, Amount: 100})
	db.Create(&Order{UserID: 2, Amount: 200})

	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	u, m := NewUpdater[UserWithDelete](ctx)
	u.Set(&m.Age, 99).InSub(&m.ID, subQ)

	affected, err := repo.UpdateByCond(u)
	if err != nil {
		t.Fatalf("UpdateByCond failed: %v", err)
	}
	if affected != 2 {
		t.Fatalf("expected 2 affected, got %d", affected)
	}

	// 验证：UserA + UserB age=99；UserC 无订单不受影响
	var got []UserWithDelete
	db.Order("id ASC").Find(&got)
	if len(got) != 3 {
		t.Fatalf("expected 3 users, got %d", len(got))
	}
	if got[0].Age != 99 || got[1].Age != 99 || got[2].Age != 25 {
		t.Fatalf("unexpected ages: %+v", got)
	}
}

// TestUpdater_GtSub_RealUpdate 真实 UPDATE WHERE age > (SELECT AVG(age))。
func TestUpdater_GtSub_RealUpdate(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	users := []UserWithDelete{{Name: "Young", Age: 20}, {Name: "Avg", Age: 30}, {Name: "Old", Age: 40}}
	db.Create(&users)

	avgQ, _ := NewQuery[UserWithDelete](ctx)
	avgQ.SelectRaw("AVG(age)")

	u, m := NewUpdater[UserWithDelete](ctx)
	u.Set(&m.Name, "Senior").GtSub(&m.Age, avgQ)

	affected, err := repo.UpdateByCond(u)
	if err != nil {
		t.Fatalf("UpdateByCond failed: %v", err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 affected (Old, age=40>30), got %d", affected)
	}
}

// TestUpdater_NotInSub_RealUpdate 真实 UPDATE WHERE id NOT IN (subquery)。
func TestUpdater_NotInSub_RealUpdate(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	users := []UserWithDelete{{Name: "UserA", Age: 20}, {Name: "UserB", Age: 30}, {Name: "UserC", Age: 25}}
	db.Create(&users)
	db.Create(&Order{UserID: 1, Amount: 100})

	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	u, m := NewUpdater[UserWithDelete](ctx)
	u.Set(&m.Age, 0).NotInSub(&m.ID, subQ)

	affected, err := repo.UpdateByCond(u)
	if err != nil {
		t.Fatalf("UpdateByCond failed: %v", err)
	}
	// UserB(2) + UserC(3) 不在订单中，UserA(1) 有订单
	if affected != 2 {
		t.Fatalf("expected 2 affected, got %d", affected)
	}
}

// TestUpdater_InSub_NilSub 验证 sub == nil 错误。
func TestUpdater_InSub_NilSub(t *testing.T) {
	ctx := context.Background()
	u, m := NewUpdater[UserWithDelete](ctx)
	u.InSub(&m.ID, nil)
	if u.GetError() == nil {
		t.Fatalf("expected error, got nil")
	}
}
