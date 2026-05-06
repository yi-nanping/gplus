// alias_test.go - v0.8.0 alias 体系 Task 3 测试
package gplus

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestQueryCore_AddAlias_HappyPath(t *testing.T) {
	c := newQueryCore(context.Background())
	o := &TestUser{}
	if err := c.addAlias("o", reflect.TypeOf(o).Elem(), o); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if _, ok := c.aliases["o"]; !ok {
		t.Fatalf("alias not stored")
	}
}

func TestQueryCore_AddAlias_DuplicateAccumulates(t *testing.T) {
	c := newQueryCore(context.Background())
	o1 := &TestUser{}
	o2 := &TestUser{}
	_ = c.addAlias("o", reflect.TypeOf(o1).Elem(), o1)
	err := c.addAlias("o", reflect.TypeOf(o2).Elem(), o2)
	if !errors.Is(err, ErrAliasDuplicate) {
		t.Fatalf("expected ErrAliasDuplicate, got %v", err)
	}
}

func TestQueryCore_LookupAddr_OffsetMustBeInSchema_H2(t *testing.T) {
	c := newQueryCore(context.Background())
	o := &TestUser{}
	_ = c.addAlias("o", reflect.TypeOf(o).Elem(), o)
	// 取实例区间内但非任何字段起始的 padding 偏移
	base := uintptr(reflect.ValueOf(o).Pointer())
	// 找一个一定不在 schema 中的 offset：取最大已知 offset + 1
	typ := reflect.TypeOf(o).Elem()
	schema := reflectStructSchema(o, "gorm", "COLUMN")
	var maxOff uintptr
	for off := range schema {
		if off > maxOff {
			maxOff = off
		}
	}
	paddingOffset := maxOff + 1
	if paddingOffset >= typ.Size() {
		t.Skip("TestUser has no padding bytes after last field; H2 padding test not applicable")
	}
	paddingAddr := base + paddingOffset
	if _, _, ok := c.lookupAddr(paddingAddr); ok {
		t.Fatalf("padding offset should not be considered a hit (H2)")
	}
}

func TestQueryCore_LookupAddr_RevokedReturnsFalse_N4(t *testing.T) {
	c := newQueryCore(context.Background())
	o := &TestUser{}
	_ = c.addAlias("o", reflect.TypeOf(o).Elem(), o)
	c.aliases["o"].revoked = true
	base := uintptr(reflect.ValueOf(o).Pointer())
	if _, _, ok := c.lookupAddr(base); ok {
		t.Fatalf("revoked entry should not be considered a hit (N4)")
	}
	if !c.hadRevokedHit(base) {
		t.Fatalf("hadRevokedHit should return true for revoked entry")
	}
}

func TestAs_HappyPath(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	o := As[TestUser](q, "o")
	if o == nil {
		t.Fatalf("expected non-nil alias instance")
	}
	if err := q.GetError(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAs_InvalidNameAccumulates(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	_ = As[TestUser](q, "1bad-name")
	err := q.GetError()
	if !errors.Is(err, ErrAliasInvalidName) {
		t.Fatalf("expected ErrAliasInvalidName, got %v", err)
	}
}

func TestAs_DuplicateAccumulates_DecisionB(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	o1 := As[TestUser](q, "o")
	o2 := As[TestUser](q, "o")
	if o1 != o2 {
		t.Fatalf("expected first instance returned on duplicate")
	}
	if !errors.Is(q.GetError(), ErrAliasDuplicate) {
		t.Fatalf("expected ErrAliasDuplicate accumulated")
	}
}

func TestAs_NilQueryPanics_N5(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic")
		}
		err, _ := r.(error)
		if !errors.Is(err, ErrAliasQueryNil) {
			t.Fatalf("expected ErrAliasQueryNil panic, got %v", r)
		}
	}()
	_ = As[TestUser](nil, "o")
}

// TestAs_DuplicateAcrossOuterChain 验证 As 沿 outerQueryRef 链向上查重名
// 子查询里若注册的 alias name 已在外层 q 上存在，应累积 ErrAliasDuplicate
func TestAs_DuplicateAcrossOuterChain_DecisionB(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	o1 := As[TestUser](q, "o")

	// TODO Task 14: SubQuery 实现后，替换为 sub, _ := SubQuery[TestUser](q)
	sub, _ := NewQuery[TestUser](context.Background())
	sub.gplusCore().outerQueryRef = q

	// 在 sub 上注册同名 alias，应命中外层 q 的 "o" 并返回 o1
	o2 := As[TestUser](sub, "o")
	if o1 != o2 {
		t.Fatalf("expected outer chain duplicate to return first instance")
	}
	if !errors.Is(sub.GetError(), ErrAliasDuplicate) {
		t.Fatalf("expected ErrAliasDuplicate accumulated on sub, got %v", sub.GetError())
	}
}

// TestAs_TypeConflictPanics CRITICAL：同名 alias 不同类型应立即 panic
// 而非返回 fallback 静默继续，避免数据类型安全破坏
func TestAs_TypeConflictPanics(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	_ = As[TestUser](q, "x")
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic on type conflict")
		}
	}()
	// 同名 "x" 但类型不同（Order 而非 TestUser）
	_ = As[Order](q, "x")
}

// uintptrOf 测试辅助：取指针变量的 uintptr 地址
func uintptrOf(v any) uintptr {
	return uintptr(reflect.ValueOf(v).Pointer())
}

func TestResolveColumnName_AliasField_Resolves(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	o := As[TestUser](q, "o")
	col, err := q.resolveColumnName(uintptrOf(&o.Name))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	// TestUser.Name 的 gorm tag 为 column:username
	if col != "o.username" {
		t.Errorf("expected o.username, got %s", col)
	}
}

func TestResolveColumnName_SubStrictClosure_H5(t *testing.T) {
	q2, _ := NewQuery[TestUser](context.Background())
	// TODO Task 14: SubQuery 实现后，替换为 sub, _ := SubQuery[TestUser](q2)
	sub, _ := NewQuery[TestUser](context.Background())
	sub.gplusCore().outerQueryRef = q2
	// 错误地引用一个完全无关的全局规范单例字段地址
	u := getModelInstance[TestUser]()
	col, err := sub.resolveColumnName(uintptrOf(&u.Name))
	if !errors.Is(err, ErrFieldAddrUnregistered) {
		t.Errorf("H5 sub 必须严格闭合，不回退全局 cache，got col=%q err=%v", col, err)
	}
}

func TestResolveColumnName_AliasRevoked_AccumulatesError_N4(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	o := As[TestUser](q, "o")
	// 翻转 revoked（模拟 Clear 效果，不调真正的 Clear 因 Task 7 才实现）
	q.gplusCore().aliases["o"].revoked = true
	_, err := q.resolveColumnName(uintptrOf(&o.Name))
	if !errors.Is(err, ErrAliasRevoked) {
		t.Errorf("expected ErrAliasRevoked, got %v", err)
	}
}

func TestBuildQuery_ErrsShortCircuit_DecisionB(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	_ = As[TestUser](q, "o")
	_ = As[TestUser](q, "o") // 重名累积 ErrAliasDuplicate
	_, db := setupTestDB[TestUser](t)
	finalDB := q.DataRuleBuilder().BuildQueryDB(db)
	if finalDB.Error == nil {
		t.Fatalf("expected BuildQuery to short-circuit on accumulated errors")
	}
	if !errors.Is(finalDB.Error, ErrAliasDuplicate) {
		t.Errorf("expected ErrAliasDuplicate in db.Error, got %v", finalDB.Error)
	}
}

func TestQuery_Clear_AliasUseAfterClear_N4(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	o := As[TestUser](q, "o")
	q.Clear()
	// 业务代码仍持有 o，使用 &o.Name 应被 revoked 拦截
	_, err := q.resolveColumnName(uintptrOf(&o.Name))
	if !errors.Is(err, ErrAliasRevoked) {
		t.Errorf("expected ErrAliasRevoked after Clear, got %v", err)
	}
}
