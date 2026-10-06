package gplus

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

func TestPagination_OffsetOverflow(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	maxInt := int(^uint(0) >> 1)
	q, _ := NewQuery[TestUser](context.Background())
	q.Page(maxInt, 2)
	if q.GetError() == nil {
		t.Fatal("overflow must be rejected before execution")
	}
	var rows []TestUser
	result := db.Session(&gorm.Session{DryRun: true}).Model(new(TestUser)).Scopes(q.BuildQuery()).Find(&rows)
	if result.Error == nil || result.Statement.SQL.Len() != 0 {
		t.Fatalf("error=%v, SQL=%s", result.Error, result.Statement.SQL.String())
	}
}

func TestPagination_OffsetBoundaryAndDefaults(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, tc := range []struct{ page, size, limit, offset int }{
		{0, 0, 10, 0},
		{3, 2, 2, 4},
		{maxInt, 1, 1, maxInt - 1},
		{maxInt/2 + 1, 2, 2, maxInt - 1},
	} {
		q, _ := NewQuery[TestUser](context.Background())
		q.Page(tc.page, tc.size)
		if err := q.GetError(); err != nil || q.limit != tc.limit || q.offset != tc.offset {
			t.Errorf("page=%d size=%d: limit=%d offset=%d error=%v", tc.page, tc.size, q.limit, q.offset, err)
		}
	}
}
