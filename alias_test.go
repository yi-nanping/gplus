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
