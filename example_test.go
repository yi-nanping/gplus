package gplus_test

import (
	"context"
	"fmt"
	"log"

	"github.com/glebarez/sqlite"
	"github.com/yi-nanping/gplus"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Article 示例模型
type Article struct {
	ID      uint   `gorm:"primaryKey;autoIncrement"`
	Title   string `gorm:"column:title"`
	Author  string `gorm:"column:author"`
	Views   int    `gorm:"column:views"`
	Deleted gorm.DeletedAt
}

func openExampleDB() *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&Article{}); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	return db
}

func ExampleNewRepository() {
	db := openExampleDB()
	repo := gplus.NewRepository[uint, Article](db)

	ctx := context.Background()

	// 插入记录
	a := &Article{Title: "Hello gplus", Author: "Alice", Views: 100}
	if err := repo.Save(ctx, a); err != nil {
		log.Fatal(err)
	}

	// 按主键查询
	got, err := repo.GetById(ctx, a.ID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(got.Title)
	// Output:
	// Hello gplus
}

func ExampleRepository_List() {
	db := openExampleDB()
	repo := gplus.NewRepository[uint, Article](db)
	ctx := context.Background()

	db.Create(&Article{Title: "Go generics", Author: "Alice", Views: 200})
	db.Create(&Article{Title: "GORM tips", Author: "Bob", Views: 50})
	db.Create(&Article{Title: "gplus guide", Author: "Alice", Views: 300})

	q, m := gplus.NewQuery[Article](ctx)
	q.Eq(&m.Author, "Alice").Ge(&m.Views, 100).Order(&m.Views, false)

	articles, err := repo.List(q)
	if err != nil {
		log.Fatal(err)
	}
	for _, a := range articles {
		fmt.Printf("%s (%d)\n", a.Title, a.Views)
	}
	// Output:
	// gplus guide (300)
	// Go generics (200)
}

func ExampleRepository_Page() {
	db := openExampleDB()
	repo := gplus.NewRepository[uint, Article](db)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		db.Create(&Article{Title: fmt.Sprintf("Article %d", i), Author: "Alice", Views: i * 10})
	}

	q, _ := gplus.NewQuery[Article](ctx)
	q.Limit(3).Offset(0)

	list, total, err := repo.Page(q, false)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("total=%d page=%d\n", total, len(list))
	// Output:
	// total=5 page=3
}

func ExampleRepository_ToSQL() {
	db := openExampleDB()
	repo := gplus.NewRepository[uint, Article](db)
	ctx := context.Background()

	q, m := gplus.NewQuery[Article](ctx)
	q.Eq(&m.Author, "Alice").Gt(&m.Views, 100)

	sql, err := repo.ToSQL(q)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(sql != "")
	// Output:
	// true
}

func ExamplePluck() {
	db := openExampleDB()
	repo := gplus.NewRepository[uint, Article](db)
	ctx := context.Background()

	db.Create(&Article{Title: "A", Author: "Alice", Views: 10})
	db.Create(&Article{Title: "B", Author: "Alice", Views: 20})

	q, m := gplus.NewQuery[Article](ctx)
	q.Eq(&m.Author, "Alice").Order(&m.Views, true)

	titles, err := gplus.Pluck[Article, string, uint](repo, q, &m.Title)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(titles)
	// Output:
	// [A B]
}
