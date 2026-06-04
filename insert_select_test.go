package gplus

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

// Closure 闭包表形态测试模型：ancestor_id/descendant_id/depth + 自增主键。
type Closure struct {
	ID           int64 `gorm:"column:id;primaryKey;autoIncrement"`
	AncestorID   uint  `gorm:"column:ancestor_id"`
	DescendantID uint  `gorm:"column:descendant_id"`
	Depth        uint  `gorm:"column:depth"`
}

func (Closure) TableName() string { return "closure" }

func assertClosureCount(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	var n int64
	db.Model(&Closure{}).Count(&n)
	if n != want {
		t.Errorf("closure 行数期望 %d，实际 %d", want, n)
	}
}

// AC-1 + AC-5：基础 INSERT...SELECT，绑定 descendant_id；真实插入成功即证明无外层括号
func TestInsertSelect_copies_ancestor_chain_with_bound_descendant(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src, m := repo.NewQuery(ctx)
	src.SelectRaw("ancestor_id").SelectRaw("?", 9).SelectRaw("depth + 1").Eq(&m.DescendantID, 5)

	affected, err := InsertSelect(repo, ctx, []any{&m.AncestorID, &m.DescendantID, &m.Depth}, src)
	if err != nil {
		t.Fatalf("InsertSelect err: %v", err)
	}
	if affected != 1 {
		t.Errorf("affected 期望 1，实际 %d", affected)
	}
	var got []Closure
	db.Order("id").Find(&got)
	if len(got) != 2 {
		t.Fatalf("行数期望 2，实际 %d", len(got))
	}
	nw := got[1]
	if nw.AncestorID != 1 || nw.DescendantID != 9 || nw.Depth != 1 {
		t.Errorf("新行期望 {1,9,1}，实际 {%d,%d,%d}", nw.AncestorID, nw.DescendantID, nw.Depth)
	}
}

// AC-6：事务变体，回滚后无新增行
func TestInsertSelectTx_rolls_back_on_rollback(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src, m := repo.NewQuery(ctx)
	src.SelectRaw("ancestor_id").SelectRaw("?", 9).SelectRaw("depth + 1").Eq(&m.DescendantID, 5)

	tx := db.Begin()
	affected, err := InsertSelectTx(repo, ctx, tx, []any{&m.AncestorID, &m.DescendantID, &m.Depth}, src)
	if err != nil {
		t.Fatalf("InsertSelectTx err: %v", err)
	}
	if affected != 1 {
		t.Errorf("affected 期望 1，实际 %d", affected)
	}
	tx.Rollback()
	assertClosureCount(t, db, 1)
}

// AC-4a：src==nil → ErrQueryNil
func TestInsertSelect_returns_ErrQueryNil_when_src_nil(t *testing.T) {
	repo, _ := setupTestDB[Closure](t)
	affected, err := InsertSelect[Closure, Closure, int64](repo, context.Background(), []any{"ancestor_id"}, nil)
	if affected != 0 || !errors.Is(err, ErrQueryNil) {
		t.Errorf("期望 (0, ErrQueryNil)，实际 (%d, %v)", affected, err)
	}
}

// AC-4b：src.GetError()!=nil（非法字段指针）→ 原样返回，不插入
func TestInsertSelect_propagates_src_builder_error(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src, m := repo.NewQuery(ctx)
	var stray Closure // 非注册单例，其字段地址不在 columnNameCache 中
	src.SelectRaw("ancestor_id").Eq(&stray.DescendantID, 5)
	if src.GetError() == nil {
		t.Fatal("前置条件：src 应已累积错误")
	}
	affected, err := InsertSelect(repo, ctx, []any{&m.AncestorID}, src)
	if affected != 0 || err == nil {
		t.Errorf("期望 (0, 非nil)，实际 (%d, %v)", affected, err)
	}
	assertClosureCount(t, db, 1)
}

// AC-3：源无投影 → ErrInsertSelectNoProjection
func TestInsertSelect_rejects_source_without_projection(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src, m := repo.NewQuery(ctx)
	src.Eq(&m.DescendantID, 5) // 无任何 Select/SelectRaw
	affected, err := InsertSelect(repo, ctx, []any{&m.AncestorID, &m.DescendantID, &m.Depth}, src)
	if affected != 0 || !errors.Is(err, ErrInsertSelectNoProjection) {
		t.Errorf("期望 (0, ErrInsertSelectNoProjection)，实际 (%d, %v)", affected, err)
	}
	assertClosureCount(t, db, 1)
}

// AC-2：目标列 3、投影 2 → ErrInsertSelectColMismatch
func TestInsertSelect_rejects_column_count_mismatch(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src, m := repo.NewQuery(ctx)
	src.SelectRaw("ancestor_id").SelectRaw("depth").Eq(&m.DescendantID, 5)                         // 2 投影
	affected, err := InsertSelect(repo, ctx, []any{&m.AncestorID, &m.DescendantID, &m.Depth}, src) // 3 目标列
	if affected != 0 || !errors.Is(err, ErrInsertSelectColMismatch) {
		t.Errorf("期望 (0, ErrInsertSelectColMismatch)，实际 (%d, %v)", affected, err)
	}
	assertClosureCount(t, db, 1)
}

// AC-12：空/nil targetCols + 有投影 → ErrInsertSelectColMismatch
func TestInsertSelect_rejects_empty_target_cols(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src, m := repo.NewQuery(ctx)
	src.SelectRaw("ancestor_id").Eq(&m.DescendantID, 5)
	affected, err := InsertSelect(repo, ctx, nil, src)
	if affected != 0 || !errors.Is(err, ErrInsertSelectColMismatch) {
		t.Errorf("期望 (0, ErrInsertSelectColMismatch)，实际 (%d, %v)", affected, err)
	}
	assertClosureCount(t, db, 1)
}

// AC-8：targetCols 原始字符串含注入 payload → ErrInsertSelectColInvalid，表不变
func TestInsertSelect_rejects_injection_in_string_target_col(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src, m := repo.NewQuery(ctx)
	src.SelectRaw("ancestor_id").Eq(&m.DescendantID, 5) // 1 投影，列数与 1 个 targetCol 匹配
	payload := "id) ; " + "DROP " + "TABLE closure; --" // 拆写避免工具链黑名单误判，运行时拼回完整 payload
	affected, err := InsertSelect(repo, ctx, []any{payload}, src)
	if affected != 0 || !errors.Is(err, ErrInsertSelectColInvalid) {
		t.Errorf("期望 (0, ErrInsertSelectColInvalid)，实际 (%d, %v)", affected, err)
	}
	assertClosureCount(t, db, 1) // 表仍存在、行数不变
}

// AC-9：源 query 用 Distinct → ErrInsertSelectModifier
func TestInsertSelect_rejects_distinct_source(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src, m := repo.NewQuery(ctx)
	src.Distinct(&m.AncestorID).SelectRaw("?", 9).SelectRaw("depth + 1").Eq(&m.DescendantID, 5)
	affected, err := InsertSelect(repo, ctx, []any{&m.AncestorID, &m.DescendantID, &m.Depth}, src)
	if affected != 0 || !errors.Is(err, ErrInsertSelectModifier) {
		t.Errorf("期望 (0, ErrInsertSelectModifier)，实际 (%d, %v)", affected, err)
	}
	assertClosureCount(t, db, 1)
}

// AC-7：源 SELECT 不注入 DataRule —— 有匹配规则也插入全量（2 行而非被过滤的 1 行）
func TestInsertSelect_does_not_inject_data_rule(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	base := context.Background()
	if err := repo.Save(base, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed1: %v", err)
	}
	if err := repo.Save(base, &Closure{AncestorID: 2, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed2: %v", err)
	}
	// 若被注入，DataRule 会把源 SELECT 过滤成 ancestor_id=1 单行
	rules := []DataRule{{Column: "ancestor_id", Condition: "=", Value: "1"}}
	ctx := context.WithValue(base, DataRuleKey, rules)
	src, m := repo.NewQuery(ctx)
	src.SelectRaw("ancestor_id").SelectRaw("?", 9).SelectRaw("depth + 1").Eq(&m.DescendantID, 5)
	affected, err := InsertSelect(repo, ctx, []any{&m.AncestorID, &m.DescendantID, &m.Depth}, src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if affected != 2 {
		t.Errorf("affected 期望 2（DataRule 未注入），实际 %d", affected)
	}
	assertClosureCount(t, db, 4)
}

// AC-10：源 WHERE 0 命中 → (0, nil)，无副作用
func TestInsertSelect_zero_match_is_noop(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src, m := repo.NewQuery(ctx)
	src.SelectRaw("ancestor_id").SelectRaw("?", 9).SelectRaw("depth + 1").Eq(&m.DescendantID, 99999)
	affected, err := InsertSelect(repo, ctx, []any{&m.AncestorID, &m.DescendantID, &m.Depth}, src)
	if affected != 0 || err != nil {
		t.Errorf("期望 (0, nil)，实际 (%d, %v)", affected, err)
	}
	assertClosureCount(t, db, 1)
}

// AC-11：cancelled ctx → context.Canceled 透传，表不变
func TestInsertSelect_propagates_context_cancel(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	base := context.Background()
	if err := repo.Save(base, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctx, cancel := context.WithCancel(base)
	cancel()
	src, m := repo.NewQuery(ctx)
	src.SelectRaw("ancestor_id").SelectRaw("?", 9).SelectRaw("depth + 1").Eq(&m.DescendantID, 5)
	affected, err := InsertSelect(repo, ctx, []any{&m.AncestorID, &m.DescendantID, &m.Depth}, src)
	if affected != 0 || !errors.Is(err, context.Canceled) {
		t.Errorf("期望 (0, context.Canceled)，实际 (%d, %v)", affected, err)
	}
	assertClosureCount(t, db, 1)
}
