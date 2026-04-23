package gplus

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

// conflictUser 用于 OnConflict 测试，email 建唯一索引
type conflictUser struct {
	ID    int64  `gorm:"primaryKey;autoIncrement"`
	Email string `gorm:"uniqueIndex;column:email;size:255"`
	Name  string `gorm:"column:name;size:255"`
	Score int    `gorm:"column:score;default:0"`
}

func setupConflictDB(t *testing.T) (*Repository[int64, conflictUser], *gorm.DB) {
	t.Helper()
	db := openDB(t)
	if err := db.AutoMigrate(&conflictUser{}); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	if db.Name() == "mysql" {
		truncateTables(t, db, &conflictUser{})
		t.Cleanup(func() { truncateTables(t, db, &conflictUser{}) })
	}
	return NewRepository[int64, conflictUser](db), db
}

// TestOnConflict_Validation 校验互斥规则
func TestOnConflict_Validation(t *testing.T) {
	cases := []struct {
		name string
		oc   OnConflict
	}{
		{
			name: "DoNothing+DoUpdateAll 互斥",
			oc:   OnConflict{DoNothing: true, DoUpdateAll: true},
		},
		{
			name: "DoNothing+DoUpdates 互斥",
			oc:   OnConflict{DoNothing: true, DoUpdates: []any{"name"}},
		},
		{
			name: "DoNothing+UpdateExprs 互斥",
			oc:   OnConflict{DoNothing: true, UpdateExprs: map[string]any{"score": 1}},
		},
		{
			name: "DoUpdateAll+DoUpdates 互斥",
			oc:   OnConflict{DoUpdateAll: true, DoUpdates: []any{"name"}},
		},
		{
			name: "DoUpdateAll+UpdateExprs 互斥",
			oc:   OnConflict{DoUpdateAll: true, UpdateExprs: map[string]any{"score": 1}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.oc.buildClause()
			if !errors.Is(err, ErrOnConflictInvalid) {
				t.Errorf("期望 ErrOnConflictInvalid，得到 %v", err)
			}
		})
	}
}

// TestOnConflict_InvalidColumn 无效列指针返回错误
func TestOnConflict_InvalidColumn(t *testing.T) {
	bad := 42 // 不是字段指针
	oc := OnConflict{Columns: []any{&bad}}
	_, err := oc.buildClause()
	if err == nil {
		t.Error("期望列指针解析失败，得到 nil")
	}
}

// TestInsertOnConflict_DoNothing 冲突时跳过
func TestInsertOnConflict_DoNothing(t *testing.T) {
	repo, _ := setupConflictDB(t)
	ctx := context.Background()

	_, m := NewQuery[conflictUser](ctx)
	oc := OnConflict{
		Columns:   []any{&m.Email},
		DoNothing: true,
	}

	u1 := &conflictUser{Email: "a@x.com", Name: "Alice", Score: 10}
	if err := repo.InsertOnConflict(ctx, u1, oc); err != nil {
		t.Fatalf("首次插入失败: %v", err)
	}

	// 再次插入相同 email，应跳过不报错
	u2 := &conflictUser{Email: "a@x.com", Name: "Bob", Score: 99}
	if err := repo.InsertOnConflict(ctx, u2, oc); err != nil {
		t.Fatalf("DoNothing 冲突时不应报错: %v", err)
	}

	// 数据应保持原始值
	q, qm := NewQuery[conflictUser](ctx)
	q.Eq(&qm.Email, "a@x.com")
	got, err := repo.GetOne(q)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	assertEqual(t, "Alice", got.Name, "DoNothing 后 name 应保持原值")
	assertEqual(t, 10, got.Score, "DoNothing 后 score 应保持原值")
}

// TestInsertOnConflict_DoUpdates 冲突时只更新指定列
func TestInsertOnConflict_DoUpdates(t *testing.T) {
	repo, _ := setupConflictDB(t)
	ctx := context.Background()

	_, m := NewQuery[conflictUser](ctx)
	oc := OnConflict{
		Columns:   []any{&m.Email},
		DoUpdates: []any{&m.Name}, // 只更新 name，不更新 score
	}

	u1 := &conflictUser{Email: "b@x.com", Name: "Bob", Score: 5}
	if err := repo.InsertOnConflict(ctx, u1, oc); err != nil {
		t.Fatalf("首次插入失败: %v", err)
	}

	u2 := &conflictUser{Email: "b@x.com", Name: "Bobby", Score: 999}
	if err := repo.InsertOnConflict(ctx, u2, oc); err != nil {
		t.Fatalf("DoUpdates 冲突插入失败: %v", err)
	}

	q, qm := NewQuery[conflictUser](ctx)
	q.Eq(&qm.Email, "b@x.com")
	got, err := repo.GetOne(q)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	assertEqual(t, "Bobby", got.Name, "DoUpdates 后 name 应被更新")
	assertEqual(t, 5, got.Score, "DoUpdates 后 score 应保持原值")
}

// TestInsertOnConflict_DoUpdateAll 冲突时覆盖所有列
func TestInsertOnConflict_DoUpdateAll(t *testing.T) {
	repo, _ := setupConflictDB(t)
	ctx := context.Background()

	_, m := NewQuery[conflictUser](ctx)
	oc := OnConflict{
		Columns:     []any{&m.Email},
		DoUpdateAll: true,
	}

	u1 := &conflictUser{Email: "c@x.com", Name: "Carol", Score: 1}
	if err := repo.InsertOnConflict(ctx, u1, oc); err != nil {
		t.Fatalf("首次插入失败: %v", err)
	}

	u2 := &conflictUser{Email: "c@x.com", Name: "Caroline", Score: 100}
	if err := repo.InsertOnConflict(ctx, u2, oc); err != nil {
		t.Fatalf("DoUpdateAll 冲突插入失败: %v", err)
	}

	q, qm := NewQuery[conflictUser](ctx)
	q.Eq(&qm.Email, "c@x.com")
	got, err := repo.GetOne(q)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	assertEqual(t, "Caroline", got.Name, "DoUpdateAll 后 name 应被更新")
	assertEqual(t, 100, got.Score, "DoUpdateAll 后 score 应被更新")
}

// TestInsertOnConflict_InvalidConfig buildClause 错误通过方法返回
func TestInsertOnConflict_InvalidConfig(t *testing.T) {
	repo, _ := setupConflictDB(t)
	ctx := context.Background()

	oc := OnConflict{DoNothing: true, DoUpdateAll: true} // 互斥配置
	err := repo.InsertOnConflict(ctx, &conflictUser{Email: "x@x.com"}, oc)
	if !errors.Is(err, ErrOnConflictInvalid) {
		t.Errorf("InsertOnConflict 应返回 ErrOnConflictInvalid，得到 %v", err)
	}

	err = repo.InsertBatchOnConflict(ctx, []conflictUser{{Email: "x@x.com"}}, oc)
	if !errors.Is(err, ErrOnConflictInvalid) {
		t.Errorf("InsertBatchOnConflict 应返回 ErrOnConflictInvalid，得到 %v", err)
	}
}

// TestInsertBatchOnConflict_EmptySlice 空切片应无操作
func TestInsertBatchOnConflict_EmptySlice(t *testing.T) {
	repo, _ := setupConflictDB(t)
	ctx := context.Background()

	err := repo.InsertBatchOnConflict(ctx, []conflictUser{}, OnConflict{DoNothing: true})
	assertError(t, err, false, "空切片不应返回错误")
}

// TestInsertBatchOnConflict_DoUpdates 批量冲突插入
func TestInsertBatchOnConflict_DoUpdates(t *testing.T) {
	repo, _ := setupConflictDB(t)
	ctx := context.Background()

	_, m := NewQuery[conflictUser](ctx)
	oc := OnConflict{
		Columns:   []any{&m.Email},
		DoUpdates: []any{&m.Score},
	}

	// 首批：插入 2 条
	batch1 := []conflictUser{
		{Email: "d@x.com", Name: "Dave", Score: 1},
		{Email: "e@x.com", Name: "Eve", Score: 2},
	}
	if err := repo.InsertBatchOnConflict(ctx, batch1, oc); err != nil {
		t.Fatalf("批量首次插入失败: %v", err)
	}

	// 第二批：相同 email，score 更新，name 不变
	batch2 := []conflictUser{
		{Email: "d@x.com", Name: "DAVE", Score: 100},
		{Email: "e@x.com", Name: "EVE", Score: 200},
	}
	if err := repo.InsertBatchOnConflict(ctx, batch2, oc); err != nil {
		t.Fatalf("批量冲突插入失败: %v", err)
	}

	q, qm := NewQuery[conflictUser](ctx)
	q.In(&qm.Email, []string{"d@x.com", "e@x.com"}).Order(&qm.Email, true)
	list, err := repo.List(q)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	assertEqual(t, 2, len(list), "应有 2 条记录")
	assertEqual(t, "Dave", list[0].Name, "d@x.com name 不应被更新")
	assertEqual(t, 100, list[0].Score, "d@x.com score 应被更新")
	assertEqual(t, "Eve", list[1].Name, "e@x.com name 不应被更新")
	assertEqual(t, 200, list[1].Score, "e@x.com score 应被更新")
}

// TestInsertOnConflict_DefaultUpdateAll 零值 OnConflict 默认 UpdateAll
func TestInsertOnConflict_DefaultUpdateAll(t *testing.T) {
	repo, _ := setupConflictDB(t)
	ctx := context.Background()

	_, m := NewQuery[conflictUser](ctx)
	// 仅指定 Columns，无其他策略 → 默认 UpdateAll
	oc := OnConflict{Columns: []any{&m.Email}}

	u1 := &conflictUser{Email: "f@x.com", Name: "Frank", Score: 7}
	if err := repo.InsertOnConflict(ctx, u1, oc); err != nil {
		t.Fatalf("首次插入失败: %v", err)
	}
	u2 := &conflictUser{Email: "f@x.com", Name: "Frankie", Score: 77}
	if err := repo.InsertOnConflict(ctx, u2, oc); err != nil {
		t.Fatalf("默认 UpdateAll 冲突插入失败: %v", err)
	}

	q, qm := NewQuery[conflictUser](ctx)
	q.Eq(&qm.Email, "f@x.com")
	got, err := repo.GetOne(q)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	assertEqual(t, "Frankie", got.Name, "默认 UpdateAll 后 name 应被更新")
	assertEqual(t, 77, got.Score, "默认 UpdateAll 后 score 应被更新")
}

// TestInsertOnConflict_UpdateExprs 冲突时用表达式更新（原子累加）
func TestInsertOnConflict_UpdateExprs(t *testing.T) {
	repo, db := setupConflictDB(t)
	ctx := context.Background()

	// 用方言自适应表达式：SQLite 用 excluded.score，MySQL 用 VALUES(score)
	var expr any
	if db.Name() == "mysql" {
		expr = gorm.Expr("score + VALUES(score)")
	} else {
		expr = gorm.Expr("score + excluded.score")
	}

	_, m := NewQuery[conflictUser](ctx)
	oc := OnConflict{
		Columns:     []any{&m.Email},
		UpdateExprs: map[string]any{"score": expr},
	}

	u1 := &conflictUser{Email: "h@x.com", Name: "Hank", Score: 10}
	if err := repo.InsertOnConflict(ctx, u1, oc); err != nil {
		t.Fatalf("首次插入失败: %v", err)
	}

	// 冲突时：score 累加（10 + 5 = 15）
	u2 := &conflictUser{Email: "h@x.com", Name: "Hank", Score: 5}
	if err := repo.InsertOnConflict(ctx, u2, oc); err != nil {
		t.Fatalf("UpdateExprs 冲突插入失败: %v", err)
	}

	q, qm := NewQuery[conflictUser](ctx)
	q.Eq(&qm.Email, "h@x.com")
	got, err := repo.GetOne(q)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	assertEqual(t, 15, got.Score, "UpdateExprs 后 score 应累加为 15")
}

// TestInsertOnConflict_DoUpdatesAndExprs DoUpdates 与 UpdateExprs 组合使用
func TestInsertOnConflict_DoUpdatesAndExprs(t *testing.T) {
	repo, db := setupConflictDB(t)
	ctx := context.Background()

	var incrExpr any
	if db.Name() == "mysql" {
		incrExpr = gorm.Expr("score + VALUES(score)")
	} else {
		incrExpr = gorm.Expr("score + excluded.score")
	}

	_, m := NewQuery[conflictUser](ctx)
	oc := OnConflict{
		Columns:     []any{&m.Email},
		DoUpdates:   []any{&m.Name},                  // name 用 excluded 覆盖
		UpdateExprs: map[string]any{"score": incrExpr}, // score 原子累加
	}

	u1 := &conflictUser{Email: "i@x.com", Name: "Ivan", Score: 3}
	if err := repo.InsertOnConflict(ctx, u1, oc); err != nil {
		t.Fatalf("首次插入失败: %v", err)
	}
	u2 := &conflictUser{Email: "i@x.com", Name: "Ivo", Score: 7}
	if err := repo.InsertOnConflict(ctx, u2, oc); err != nil {
		t.Fatalf("DoUpdates+UpdateExprs 冲突插入失败: %v", err)
	}

	q, qm := NewQuery[conflictUser](ctx)
	q.Eq(&qm.Email, "i@x.com")
	got, err := repo.GetOne(q)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	assertEqual(t, "Ivo", got.Name, "name 应被 excluded 覆盖")
	assertEqual(t, 10, got.Score, "score 应累加为 10")
}

// TestInsertBatchOnConflict_Tx 事务路径
func TestInsertBatchOnConflict_Tx(t *testing.T) {
	repo, _ := setupConflictDB(t)
	ctx := context.Background()

	_, m := NewQuery[conflictUser](ctx)
	oc := OnConflict{Columns: []any{&m.Email}, DoNothing: true}

	err := repo.Transaction(ctx, func(tx *gorm.DB) error {
		batch := []conflictUser{
			{Email: "j@x.com", Name: "Judy", Score: 1},
		}
		return repo.InsertBatchOnConflictTx(ctx, batch, oc, tx)
	})
	assertError(t, err, false, "事务批量冲突插入不应报错")

	q, qm := NewQuery[conflictUser](ctx)
	q.Eq(&qm.Email, "j@x.com")
	got, err := repo.GetOne(q)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	assertEqual(t, "Judy", got.Name, "事务插入后记录应存在")
}

// TestInsertOnConflict_StringColumnName 字符串列名解析
func TestInsertOnConflict_StringColumnName(t *testing.T) {
	repo, _ := setupConflictDB(t)
	ctx := context.Background()

	// Columns 用字符串而非字段指针
	oc := OnConflict{
		Columns:   []any{"email"},
		DoUpdates: []any{"name"},
	}

	u1 := &conflictUser{Email: "g@x.com", Name: "Grace", Score: 3}
	if err := repo.InsertOnConflict(ctx, u1, oc); err != nil {
		t.Fatalf("首次插入失败: %v", err)
	}
	u2 := &conflictUser{Email: "g@x.com", Name: "Gracie", Score: 300}
	if err := repo.InsertOnConflict(ctx, u2, oc); err != nil {
		t.Fatalf("字符串列名冲突插入失败: %v", err)
	}

	q, qm := NewQuery[conflictUser](ctx)
	q.Eq(&qm.Email, "g@x.com")
	got, err := repo.GetOne(q)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	assertEqual(t, "Gracie", got.Name, "name 应被更新")
	assertEqual(t, 3, got.Score, "score 不应被更新")
}
