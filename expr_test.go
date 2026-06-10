package gplus

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

// AC-4：单表表达式 + Lit 绑定。closure 1 行 {1,5,0}，
// SelectExpr(Add(Col(&m.Depth), Lit(1))) 标量结果=1；DryRun 断言字面量走绑定不拼文本。
func TestSelectExpr_单表加法字面量走绑定(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	q, m := repo.NewQuery(ctx)
	q.SelectExpr(Add(Col(&m.Depth), Lit(1))).Eq(&m.DescendantID, 5)
	if err := q.GetError(); err != nil {
		t.Fatalf("GetError 应为 nil，实际: %v", err)
	}

	var got uint
	if err := q.ToDB(db).Scan(&got).Error; err != nil {
		t.Fatalf("Scan 应成功，实际 err: %v", err)
	}
	if got != 1 {
		t.Fatalf("depth+1 期望 1，实际 %d", got)
	}

	// DryRun 断言：Vars 含字面量 1，SQL 文本中 1 只以 ? 占位
	var dummy uint
	stmt := q.ToDB(db).Session(&gorm.Session{DryRun: true}).Scan(&dummy).Statement
	foundLit := false
	for _, v := range stmt.Vars {
		if iv, ok := v.(int); ok && iv == 1 {
			foundLit = true
		}
	}
	if !foundLit {
		t.Errorf("Vars 应含字面量 1，实际 Vars=%v", stmt.Vars)
	}
	// 仅断言 Vars 含字面量即为绑定的充分证据（值进 Vars ⟹ 走绑定、未拼进 SQL 文本）。
	// 不断言 SQL 占位符字符——占位符方言相关（SQLite/MySQL 为 ?，PG 为 $1，Oracle 为 :n），
	// 硬断言 "?" 会在 PG CI 上误报（实际渲染 "depth" + $1）。
}

// AC-5：alias 列解析。ext/sub 两侧 Col 分别解析为 ext.depth / sub.depth。
func TestSelectExpr_alias两侧列分别解析(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	for _, c := range []Closure{{AncestorID: 1, DescendantID: 5, Depth: 0}, {AncestorID: 5, DescendantID: 7, Depth: 0}} {
		if err := repo.Save(ctx, &c); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	q, ext := repo.NewQueryAs(ctx, "ext")
	sub := As[Closure](q, "sub")
	q.CrossJoinAs(sub).Eq(&sub.AncestorID, 5).Eq(&ext.DescendantID, 5)
	q.SelectExpr(Add(Col(&ext.Depth), Col(&sub.Depth), Lit(1)))
	if err := q.GetError(); err != nil {
		t.Fatalf("GetError 应为 nil，实际: %v", err)
	}

	var got uint
	if err := q.ToDB(db).Scan(&got).Error; err != nil {
		t.Fatalf("Scan 应成功，实际 err: %v", err)
	}
	if got != 1 {
		t.Fatalf("ext.depth+sub.depth+1 期望 1，实际 %d", got)
	}
}

// AC-6：Lit 注入防御。恶意字符串作为字面量走绑定，表不被破坏，返回值=原字符串。
func TestSelectExpr_Lit注入防御走绑定(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	if err := repo.Save(ctx, &Closure{AncestorID: 1, DescendantID: 5, Depth: 0}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const payload = "1; DROP TABLE closure;--"
	q, m := repo.NewQuery(ctx)
	q.SelectExpr(Lit(payload)).Eq(&m.DescendantID, 5)
	if err := q.GetError(); err != nil {
		t.Fatalf("GetError 应为 nil，实际: %v", err)
	}

	var got string
	if err := q.ToDB(db).Scan(&got).Error; err != nil {
		t.Fatalf("Scan 应成功，实际 err: %v", err)
	}
	if got != payload {
		t.Errorf("返回值应为原字符串，实际 %q", got)
	}
	// 表仍存在且行数不变
	assertClosureCount(t, db, 1)
}

// AC-7：未注册地址。Col 指向外部本地 struct，SelectExpr 调用期累积
// ErrFieldAddrUnregistered；repo.List 拦截不发 SQL。
func TestSelectExpr_未注册地址调用期累积错误(t *testing.T) {
	repo, _ := setupTestDB[Closure](t)
	ctx := context.Background()

	var foreign struct{ X uint }
	q, _ := repo.NewQuery(ctx)
	q.SelectExpr(Col(&foreign.X))

	if !errors.Is(q.GetError(), ErrFieldAddrUnregistered) {
		t.Fatalf("GetError 应为 ErrFieldAddrUnregistered，实际: %v", q.GetError())
	}
	_, err := repo.List(q)
	if !errors.Is(err, ErrFieldAddrUnregistered) {
		t.Errorf("List 应返回 ErrFieldAddrUnregistered，实际: %v", err)
	}
}

// AC-8：revoked alias。Clear 后 SelectExpr 命中 revoked 区间累积 ErrAliasRevoked。
func TestSelectExpr_revoked_alias累积错误(t *testing.T) {
	repo, _ := setupTestDB[Closure](t)
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
	repo, _ := setupTestDB[Closure](t)
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
	repo, _ := setupTestDB[Closure](t)
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
