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
	// 找一个一定不在 schema 中的 offset：取 typ.Size()-1（通常是 padding）
	typ := reflect.TypeOf(o).Elem()
	paddingOffset := typ.Size() - 1
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
}
