package gplus

import (
	"context"
	"errors"
	"testing"
)

// AC-11：端到端闭包搬移，成对映射插入正确（逐字段断言 {1,7,1}）。
func TestInsertSelectMap_inserts_paired_columns_end_to_end(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	seeds := []Closure{{AncestorID: 1, DescendantID: 5, Depth: 0}, {AncestorID: 5, DescendantID: 7, Depth: 0}}
	for i := range seeds {
		if err := repo.Save(ctx, &seeds[i]); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	q, ext := repo.NewQueryAs(ctx, "ext")
	sub := As[Closure](q, "sub")
	q.CrossJoinAs(sub).Eq(&sub.AncestorID, 5).Eq(&ext.DescendantID, 5)
	m := Model[Closure]()
	affected, err := InsertSelectMap(repo, ctx, []InsertCol{
		{Target: &m.AncestorID, Src: Col(&ext.AncestorID)},
		{Target: &m.DescendantID, Src: Col(&sub.DescendantID)},
		{Target: &m.Depth, Src: Add(Col(&ext.Depth), Col(&sub.Depth), Lit(1))},
	}, q)
	if err != nil {
		t.Fatalf("InsertSelectMap 应成功，实际: %v", err)
	}
	if affected != 1 {
		t.Fatalf("affected 期望 1，实际 %d", affected)
	}
	// 逐字段断言新增行 {1,7,1}——禁止只数行数：列错位仍满足行数=3。
	var got []Closure
	db.Order("id").Find(&got)
	if len(got) != 3 {
		t.Fatalf("总行数期望 3，实际 %d", len(got))
	}
	nw := got[2]
	if nw.AncestorID != 1 || nw.DescendantID != 7 || nw.Depth != 1 {
		t.Fatalf("新增行期望 {1,7,1}，实际 {%d,%d,%d}", nw.AncestorID, nw.DescendantID, nw.Depth)
	}
}

// AC-12：与手动投影互斥（q 已有 Select/SelectRaw/SelectExpr）→ ErrInsertSelectMapConflict，不发 SQL。
func TestInsertSelectMap_rejects_manual_projection(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src, m := repo.NewQuery(ctx)
	src.SelectRaw("ancestor_id").Eq(&m.DescendantID, 5) // 手动投影：len(selects)>0
	affected, err := InsertSelectMap(repo, ctx, []InsertCol{
		{Target: &m.AncestorID, Src: Col(&m.AncestorID)},
	}, src)
	if affected != 0 || !errors.Is(err, ErrInsertSelectMapConflict) {
		t.Errorf("期望 (0, ErrInsertSelectMapConflict)，实际 (%d, %v)", affected, err)
	}
	assertClosureCount(t, db, 1)
}

// AC-13：第 2 对 Target 未注册地址 → ErrFieldAddrUnregistered，不发 SQL，且 src.selects 未被污染（len==0）。
func TestInsertSelectMap_target_resolve_failure_leaves_src_untouched(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src, m := repo.NewQuery(ctx)
	src.Eq(&m.DescendantID, 5)
	var foreign struct{ X uint } // 全新本地 struct，字段地址未注册
	affected, err := InsertSelectMap(repo, ctx, []InsertCol{
		{Target: &m.AncestorID, Src: Col(&m.AncestorID)},
		{Target: &foreign.X, Src: Col(&m.DescendantID)},
		{Target: &m.Depth, Src: Col(&m.Depth)},
	}, src)
	// Target 走包级 resolveColumnName（全局 cache），未注册地址返回 ErrColumnNotFound
	// （非 ErrFieldAddrUnregistered——后者是 src 的 alias 链解析错误，Target 不经 src）。
	if affected != 0 || !errors.Is(err, ErrColumnNotFound) {
		t.Errorf("期望 (0, ErrColumnNotFound)，实际 (%d, %v)", affected, err)
	}
	if len(src.selects) != 0 {
		t.Errorf("src.selects 期望 0（零副作用，未被污染），实际 %d", len(src.selects))
	}
	assertClosureCount(t, db, 1)
}

// AC-14：空映射（cols==nil 与 len==0 两种）→ ErrInsertSelectColMismatch，不发 SQL。
func TestInsertSelectMap_rejects_empty_cols(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cases := []struct {
		name string
		cols []InsertCol
	}{
		{"nil 映射", nil},
		{"空 slice 映射", []InsertCol{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, m := repo.NewQuery(ctx)
			src.Eq(&m.DescendantID, 5)
			affected, err := InsertSelectMap(repo, ctx, tc.cols, src)
			if affected != 0 || !errors.Is(err, ErrInsertSelectColMismatch) {
				t.Errorf("期望 (0, ErrInsertSelectColMismatch)，实际 (%d, %v)", affected, err)
			}
		})
	}
	assertClosureCount(t, db, 1)
}

// 补充：Target 为合法标识符字符串（走 validDataRuleColumn 白名单分支），端到端插入成功。
func TestInsertSelectMap_accepts_string_target_cols(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src, m := repo.NewQuery(ctx)
	src.Eq(&m.DescendantID, 5)
	affected, err := InsertSelectMap(repo, ctx, []InsertCol{
		{Target: "ancestor_id", Src: Col(&m.AncestorID)},
		{Target: "descendant_id", Src: Lit(9)},
		{Target: "depth", Src: Add(Col(&m.Depth), Lit(1))},
	}, src)
	if err != nil {
		t.Fatalf("InsertSelectMap 应成功，实际: %v", err)
	}
	if affected != 1 {
		t.Fatalf("affected 期望 1，实际 %d", affected)
	}
	var got []Closure
	db.Order("id").Find(&got)
	if len(got) != 2 || got[1].AncestorID != 1 || got[1].DescendantID != 9 || got[1].Depth != 1 {
		t.Fatalf("新增行期望 {1,9,1}，实际 %+v", got)
	}
}

// 补充：Target 为非法标识符字符串 → ErrInsertSelectColInvalid，零副作用。
func TestInsertSelectMap_rejects_invalid_string_target(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src, m := repo.NewQuery(ctx)
	src.Eq(&m.DescendantID, 5)
	payload := "id) ; " + "DROP " + "TABLE closure; --"
	affected, err := InsertSelectMap(repo, ctx, []InsertCol{
		{Target: payload, Src: Col(&m.AncestorID)},
	}, src)
	if affected != 0 || !errors.Is(err, ErrInsertSelectColInvalid) {
		t.Errorf("期望 (0, ErrInsertSelectColInvalid)，实际 (%d, %v)", affected, err)
	}
	if len(src.selects) != 0 {
		t.Errorf("src.selects 期望 0（零副作用），实际 %d", len(src.selects))
	}
	assertClosureCount(t, db, 1)
}

// 补充：Src 表达式含未注册 Col 地址 → src.resolveExprItem 失败，返回 src.GetError()，零副作用。
func TestInsertSelectMap_src_expr_resolve_failure_leaves_src_untouched(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src, m := repo.NewQuery(ctx)
	src.Eq(&m.DescendantID, 5)
	var foreign struct{ X uint } // 全新本地 struct，Src 里的 Col 地址不在 src alias 链/全局 cache
	affected, err := InsertSelectMap(repo, ctx, []InsertCol{
		{Target: &m.AncestorID, Src: Col(&m.AncestorID)},
		{Target: &m.Depth, Src: Col(&foreign.X)},
	}, src)
	if affected != 0 || err == nil {
		t.Errorf("期望 (0, 非nil)，实际 (%d, %v)", affected, err)
	}
	if len(src.selects) != 0 {
		t.Errorf("src.selects 期望 0（零副作用），实际 %d", len(src.selects))
	}
	assertClosureCount(t, db, 1)
}

// AC-15：事务变体回滚后无新增行。
func TestInsertSelectMapTx_rolls_back_on_rollback(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	seeds := []Closure{{AncestorID: 1, DescendantID: 5, Depth: 0}, {AncestorID: 5, DescendantID: 7, Depth: 0}}
	for i := range seeds {
		if err := repo.Save(ctx, &seeds[i]); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	q, ext := repo.NewQueryAs(ctx, "ext")
	sub := As[Closure](q, "sub")
	q.CrossJoinAs(sub).Eq(&sub.AncestorID, 5).Eq(&ext.DescendantID, 5)
	m := Model[Closure]()

	tx := db.Begin()
	affected, err := InsertSelectMapTx(repo, ctx, tx, []InsertCol{
		{Target: &m.AncestorID, Src: Col(&ext.AncestorID)},
		{Target: &m.DescendantID, Src: Col(&sub.DescendantID)},
		{Target: &m.Depth, Src: Add(Col(&ext.Depth), Col(&sub.Depth), Lit(1))},
	}, q)
	if err != nil {
		t.Fatalf("InsertSelectMapTx err: %v", err)
	}
	if affected != 1 {
		t.Errorf("affected 期望 1，实际 %d", affected)
	}
	tx.Rollback()
	assertClosureCount(t, db, 2)
}
