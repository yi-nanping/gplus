package gplus

import (
	"context"
	"errors"
	"testing"
)

func TestSubQueryAs_RejectsShadowedAlias(t *testing.T) {
	q, _ := NewQueryAs[TestUser](context.Background(), "same")
	sub, _ := SubQueryAs[Order](q, "same")
	q.NotExists(sub)
	if !errors.Is(q.GetError(), ErrAliasDuplicate) {
		t.Fatalf("shadowed alias must fail: %v", q.GetError())
	}
	result := q.ToDB(newDryRunDB(t)).Find(&[]TestUser{})
	if result.Error == nil || result.Statement.SQL.Len() != 0 {
		t.Fatalf("shadowed alias executed SQL=%s err=%v", result.Statement.SQL.String(), result.Error)
	}
}

func TestSubQueryAs_UsesAliasForSoftDelete(t *testing.T) {
	q, _ := NewQueryAs[UserWithDelete](context.Background(), "parent")
	sub, _ := SubQueryAs[UserWithDelete](q, "child")
	sub.WhereRaw("child.id = parent.id")
	q.NotExists(sub)
	result := q.ToDB(newDryRunDB(t)).Find(&[]UserWithDelete{})
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	sql := stripIdentQuotes(result.Statement.SQL.String())
	assertSQL(t, sql, "FROM user_with_deletes AS child", "child.deleted_at IS NULL", "parent.deleted_at IS NULL")
}
