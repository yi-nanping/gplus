package gplus

import (
	"context"
	"testing"
)

// TestSubquerier_Interface_Implementation 验证 *Query[T] 满足 Subquerier 接口。
func TestSubquerier_Interface_Implementation(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	var sub Subquerier = q
	if sub.GetError() != nil {
		t.Fatalf("expected nil error from fresh sub, got %v", sub.GetError())
	}
}
