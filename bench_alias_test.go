// bench_alias_test.go - v0.8.0 alias 体系性能基线 benchmark
package gplus

import (
	"context"
	"testing"
)

// BenchmarkResolveColumnName_NoAlias 基线：无 alias，直查全局 cache
func BenchmarkResolveColumnName_NoAlias(b *testing.B) {
	q, u := NewQuery[TestUser](context.Background())
	addr := uintptrOf(&u.Name)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = q.resolveColumnName(addr)
	}
}

// BenchmarkResolveColumnName_OneAlias 1 alias，期望 ≤ 50 ns/op
func BenchmarkResolveColumnName_OneAlias(b *testing.B) {
	q, _ := NewQuery[TestUser](context.Background())
	o := As[Order](q, "o")
	addr := uintptrOf(&o.UserID)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = q.resolveColumnName(addr)
	}
}

// BenchmarkResolveColumnName_FiveAliases 5 alias，期望 ≤ 100 ns/op
func BenchmarkResolveColumnName_FiveAliases(b *testing.B) {
	q, _ := NewQuery[TestUser](context.Background())
	_ = As[Order](q, "o1")
	_ = As[Order](q, "o2")
	_ = As[Order](q, "o3")
	_ = As[Order](q, "o4")
	o5 := As[Order](q, "o5")
	addr := uintptrOf(&o5.UserID)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = q.resolveColumnName(addr)
	}
}

// BenchmarkResolveColumnName_OuterChain3 跨 3 层 outerQuery，期望 ≤ 300 ns/op
func BenchmarkResolveColumnName_OuterChain3(b *testing.B) {
	q, u := NewQuery[TestUser](context.Background())
	sub1, _ := SubQuery[Order](q)
	sub2, _ := SubQuery[Product](sub1)
	addr := uintptrOf(&u.Name)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = sub2.resolveColumnName(addr)
	}
}
