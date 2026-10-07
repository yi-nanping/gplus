package gplus

import (
	"context"
	"testing"
)

func BenchmarkAliasLookup_FiveAliases(b *testing.B) {
	q, _ := NewQuery[TestUser](context.Background())
	_ = As[Order](q, "o1")
	_ = As[Order](q, "o2")
	_ = As[Order](q, "o3")
	_ = As[Order](q, "o4")
	o := As[Order](q, "o5")
	addr := uintptrOf(&o.UserID)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = q.core.lookupAddr(addr)
	}
}
