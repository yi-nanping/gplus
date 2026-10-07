package gplus

import (
	"context"
	"errors"
	"testing"
)

// AC-8：revoked alias。Clear 后 SelectExpr 命中 revoked 区间累积 ErrAliasRevoked。
func TestSelectExpr_revoked_alias累积错误(t *testing.T) {
	repo := NewRepository[int64, Closure](newDryRunDB(t))
	ctx := context.Background()

	q, ext := repo.NewQueryAs(ctx, "ext")
	q.Clear()
	q.SelectExpr(Add(Col(&ext.Depth), Lit(1)))

	if !errors.Is(q.GetError(), ErrAliasRevoked) {
		t.Fatalf("GetError 应为 ErrAliasRevoked，实际: %v", q.GetError())
	}
}

// AC-9：投影计数兼容。SelectExpr +1；与 Select/SelectRaw 混用计数为 3。
func TestSelectExpr_投影计数兼容(t *testing.T) {
	repo := NewRepository[int64, Closure](newDryRunDB(t))
	ctx := context.Background()

	q, m := repo.NewQuery(ctx)
	q.SelectExpr(Add(Col(&m.Depth), Lit(1)))
	if len(q.selects) != 1 {
		t.Fatalf("单次 SelectExpr 后 len(q.selects) 期望 1，实际 %d", len(q.selects))
	}

	q2, m2 := repo.NewQuery(ctx)
	q2.Select(&m2.AncestorID).SelectRaw("depth + ?", 1).SelectExpr(Add(Col(&m2.Depth), Lit(2)))
	if len(q2.selects) != 3 {
		t.Fatalf("混用 3 个投影后 len(q2.selects) 期望 3，实际 %d", len(q2.selects))
	}
	if err := q2.GetError(); err != nil {
		t.Fatalf("混用 GetError 应为 nil，实际: %v", err)
	}
}

// AC-10：空 Add 拒绝。SelectExpr(Add()) 累积 ErrExprEmpty，不追加 selectItem。
func TestSelectExpr_空Add拒绝(t *testing.T) {
	repo := NewRepository[int64, Closure](newDryRunDB(t))
	ctx := context.Background()

	q, _ := repo.NewQuery(ctx)
	q.SelectExpr(Add())

	if !errors.Is(q.GetError(), ErrExprEmpty) {
		t.Fatalf("GetError 应为 ErrExprEmpty，实际: %v", q.GetError())
	}
	if len(q.selects) != 0 {
		t.Errorf("空 Add 不应追加 selectItem，实际 len=%d", len(q.selects))
	}
}
