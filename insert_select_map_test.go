package gplus

import (
	"context"
	"errors"
	"testing"
)

func TestInsertSelectMap_target_resolve_failure_leaves_src_untouched(t *testing.T) {
	repo := NewRepository[uint, Closure](newDryRunDB(t))
	ctx := context.Background()
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
}

func TestInsertSelectMap_rejects_invalid_string_target(t *testing.T) {
	repo := NewRepository[uint, Closure](newDryRunDB(t))
	ctx := context.Background()
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
}

func TestInsertSelectMap_src_expr_resolve_failure_leaves_src_untouched(t *testing.T) {
	repo := NewRepository[uint, Closure](newDryRunDB(t))
	ctx := context.Background()
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
	// Src 的 Col(&foreign.X) 地址未注册到 src alias 链/全局 cache → ErrFieldAddrUnregistered。
	if !errors.Is(err, ErrFieldAddrUnregistered) {
		t.Errorf("err 期望 ErrFieldAddrUnregistered，实际 %v", err)
	}
	if len(src.selects) != 0 {
		t.Errorf("src.selects 期望 0（零副作用），实际 %d", len(src.selects))
	}
}
