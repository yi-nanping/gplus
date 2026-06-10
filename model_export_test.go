package gplus

import (
	"context"
	"sync"
	"testing"
)

// round1FreshModel 全新模型：此前从未被任何测试经 NewQuery/RegisterModel 触达，
// 用于验证 Model[T]() 首次调用触发自动注册（getModelInstance 慢路径）。
type round1FreshModel struct {
	ID   int64  `gorm:"column:id;primaryKey"`
	Name string `gorm:"column:name"`
}

func (round1FreshModel) TableName() string { return "round1_fresh_model" }

// AC-1：Model[T]() 返回与 NewQuery 同一规范单例指针，字段地址可解析为列名；并发安全。
func TestModel_返回规范单例且与NewQuery指针一致并发安全(t *testing.T) {
	// Arrange
	repo, _ := setupTestDB[Closure](t)
	ctx := context.Background()

	// Act
	m := Model[Closure]()
	_, m2 := repo.NewQuery(ctx)

	// Assert：非 nil + 指针相等 + 字段地址可解析
	if m == nil {
		t.Fatal("Model[Closure]() 返回 nil")
	}
	if m != m2 {
		t.Fatalf("Model[Closure]() 与 NewQuery 返回的单例指针不一致：%p != %p", m, m2)
	}
	col, err := resolveColumnName(&m.Depth)
	if err != nil {
		t.Fatalf("resolveColumnName(&m.Depth) 返回错误：%v", err)
	}
	if col != "depth" {
		t.Fatalf("resolveColumnName(&m.Depth) 期望 \"depth\"，实际 %q", col)
	}

	// 并发段：100 goroutine 并发调用 Model[Closure]() 全部返回同一指针
	const n = 100
	results := make([]*Closure, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = Model[Closure]()
		}(i)
	}
	wg.Wait()
	for i, p := range results {
		if p != m {
			t.Fatalf("并发调用第 %d 个返回的指针与单例不一致：%p != %p", i, p, m)
		}
	}
}

// AC-3：Model[T]() 首次调用一个全新模型时自动注册，字段地址随后可解析为列名。
func TestModel_首次调用全新模型自动注册字段地址(t *testing.T) {
	// Arrange + Act
	fresh := Model[round1FreshModel]()

	// Assert：返回可用单例 + 字段地址解析成功
	if fresh == nil {
		t.Fatal("Model[round1FreshModel]() 返回 nil")
	}
	col, err := resolveColumnName(&fresh.Name)
	if err != nil {
		t.Fatalf("resolveColumnName(&fresh.Name) 返回错误：%v", err)
	}
	if col != "name" {
		t.Fatalf("resolveColumnName(&fresh.Name) 期望 \"name\"，实际 %q", col)
	}
}
