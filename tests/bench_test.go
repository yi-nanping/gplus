package gplus_test

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	. "github.com/yi-nanping/gplus"
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
	sqlDB, err := db.DB()
	if err != nil {
		b.Fatalf("get sql db: %v", err)
	}
	b.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			b.Errorf("close db: %v", err)
		}
	})
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
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u := &TestUser{Name: "bench", Age: 20, Email: "bench@test.com"}
		if err := repo.Save(ctx, u); err != nil {
			b.Fatalf("Save: %v", err)
		}
		if u.ID == 0 {
			b.Fatal("Save did not assign a primary key")
		}
	}
}

func BenchmarkRepository_GetById(b *testing.B) {
	repo, db := setupBenchDB(b)
	ctx := context.Background()
	seed := &TestUser{Name: "Alice", Age: 25, Email: "alice@test.com"}
	if err := db.Create(seed).Error; err != nil {
		b.Fatalf("seed: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u, err := repo.GetById(ctx, seed.ID)
		if err != nil || u.ID != seed.ID || u.Name != seed.Name {
			b.Fatalf("GetById: user=%+v err=%v", u, err)
		}
	}
}

func BenchmarkRepository_List(b *testing.B) {
	repo, db := setupBenchDB(b)
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		if err := db.Create(&TestUser{Name: "user", Age: 20 + i, Email: "u@test.com"}).Error; err != nil {
			b.Fatalf("seed: %v", err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q, m := NewQuery[TestUser](ctx)
		q.Ge(&m.Age, 18)
		rows, err := repo.List(q)
		if err != nil || len(rows) != 50 {
			b.Fatalf("List: rows=%d err=%v", len(rows), err)
		}
	}
}

func BenchmarkRepository_UpdateByCond(b *testing.B) {
	repo, db := setupBenchDB(b)
	ctx := context.Background()
	if err := db.Create(&TestUser{Name: "target", Age: 30, Email: "t@test.com"}).Error; err != nil {
		b.Fatalf("seed: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u, m := NewUpdater[TestUser](ctx)
		u.Set(&m.Age, i%100).Eq(&m.Name, "target")
		affected, err := repo.UpdateByCond(u)
		if err != nil || affected != 1 {
			b.Fatalf("UpdateByCond: affected=%d err=%v", affected, err)
		}
	}
}
