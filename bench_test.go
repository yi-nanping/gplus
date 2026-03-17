package gplus

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupBenchDB 初始化内存数据库，日志静默
func setupBenchDB(b *testing.B) (*Repository[int64, TestUser], *gorm.DB) {
	b.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		b.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(new(TestUser)); err != nil {
		b.Fatalf("migrate: %v", err)
	}
	return NewRepository[int64, TestUser](db), db
}

// --- Query 构建 ---

func BenchmarkNewQuery(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewQuery[TestUser](ctx)
	}
}

func BenchmarkQuery_Eq(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q, m := NewQuery[TestUser](ctx)
		q.Eq(&m.Name, "Alice")
	}
}

func BenchmarkQuery_Chain5(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q, m := NewQuery[TestUser](ctx)
		q.Eq(&m.Name, "Alice").
			Ge(&m.Age, 18).
			Le(&m.Age, 60).
			Eq(&m.IsActive, true).
			Like(&m.Email, "example.com")
	}
}

// --- Updater 构建 ---

func BenchmarkNewUpdater(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewUpdater[TestUser](ctx)
	}
}

func BenchmarkUpdater_Set(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u, m := NewUpdater[TestUser](ctx)
		u.Set(&m.Name, "Bob").Set(&m.Age, 30)
	}
}

// --- Repository DB 操作 ---

func BenchmarkRepository_Save(b *testing.B) {
	repo, _ := setupBenchDB(b)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u := &TestUser{Name: "bench", Age: 20, Email: "bench@test.com"}
		_ = repo.Save(ctx, u)
	}
}

func BenchmarkRepository_GetById(b *testing.B) {
	repo, db := setupBenchDB(b)
	ctx := context.Background()
	seed := &TestUser{Name: "Alice", Age: 25, Email: "alice@test.com"}
	db.Create(seed)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = repo.GetById(ctx, seed.ID)
	}
}

func BenchmarkRepository_List(b *testing.B) {
	repo, db := setupBenchDB(b)
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		db.Create(&TestUser{Name: "user", Age: 20 + i, Email: "u@test.com"})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q, m := NewQuery[TestUser](ctx)
		q.Ge(&m.Age, 18)
		_, _ = repo.List(q)
	}
}

func BenchmarkRepository_UpdateByCond(b *testing.B) {
	repo, db := setupBenchDB(b)
	ctx := context.Background()
	db.Create(&TestUser{Name: "target", Age: 30, Email: "t@test.com"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u, m := NewUpdater[TestUser](ctx)
		u.Set(&m.Age, i%100).Eq(&m.Name, "target")
		_, _ = repo.UpdateByCond(u)
	}
}
