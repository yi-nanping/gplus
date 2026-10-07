package gplus_test

import (
	"context"
	"errors"
	"testing"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
)

type namedVersionMetadata struct {
	Version int64 `gorm:"column:lock_version" gplus:"version"`
	Note    string
	Score   int
}

type nestedNamedVersionMetadata struct {
	State namedVersionMetadata `gorm:"embedded"`
	Label string
}

type nestedNamedVersionRecord struct {
	ID   int64 `gorm:"primaryKey;autoIncrement"`
	Name string
	Meta nestedNamedVersionMetadata `gorm:"embedded"`
}

func TestOptimisticLock_NamedEmbedNestedTransaction(t *testing.T) {
	repo, db := setupTestDB[nestedNamedVersionRecord](t)
	ctx := context.Background()
	seed := nestedNamedVersionRecord{Name: "initial", Meta: nestedNamedVersionMetadata{
		State: namedVersionMetadata{Version: 1, Note: "initial note", Score: 7}, Label: "initial label",
	}}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatal(err)
	}
	fresh, stale := seed, seed
	fresh.Meta.State.Note, fresh.Meta.Label = "fresh note", "fresh label"
	if err := db.Transaction(func(tx *gorm.DB) error {
		return repo.UpdateByIdTx(ctx, &fresh, tx)
	}); err != nil {
		t.Fatalf("多层命名值嵌入事务更新失败: %v", err)
	}
	if fresh.Meta.State.Version != 2 {
		t.Errorf("事务更新应回写版本 2，实际 %d", fresh.Meta.State.Version)
	}
	stale.Meta.State.Note = "stale note"
	if err := db.Transaction(func(tx *gorm.DB) error {
		return repo.UpdateByIdTx(ctx, &stale, tx)
	}); !errors.Is(err, ErrOptimisticLock) {
		t.Errorf("旧版本事务更新应失败，实际 %v", err)
	}
	got, err := repo.GetById(ctx, seed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta.State.Version != 2 || got.Meta.State.Note != "fresh note" || got.Meta.Label != "fresh label" {
		t.Fatalf("多层嵌入业务字段应更新且不被旧版本覆盖: %+v", got)
	}
}

type namedVersionRecord struct {
	ID   int64 `gorm:"primaryKey;autoIncrement"`
	Name string
	Meta namedVersionMetadata `gorm:"embedded"`
}

func TestOptimisticLock_NamedEmbedRejectsStaleUpdate(t *testing.T) {
	repo, db := setupTestDB[namedVersionRecord](t)
	ctx := context.Background()
	seed := namedVersionRecord{Name: "initial", Meta: namedVersionMetadata{Version: 1, Note: "initial note", Score: 7}}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatal(err)
	}
	fresh, stale := seed, seed
	fresh.Name = ""
	fresh.Meta.Note, fresh.Meta.Score = "fresh note", 0
	if err := repo.UpdateById(ctx, &fresh); err != nil {
		t.Fatalf("命名值嵌入更新失败: %v", err)
	}
	if fresh.Meta.Version != 2 {
		t.Errorf("更新成功应回写版本 2，实际 %d", fresh.Meta.Version)
	}
	stale.Meta.Note = "stale note"
	if err := repo.UpdateById(ctx, &stale); !errors.Is(err, ErrOptimisticLock) {
		t.Errorf("旧版本应被拒绝，实际错误 %v", err)
	}
	got, err := repo.GetById(ctx, seed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta.Version != 2 || got.Meta.Note != "fresh note" || got.Name != "initial" || got.Meta.Score != 7 {
		t.Fatalf("数据库结果应包含新版本和嵌入字段更新，保留零值字段原值: %+v", got)
	}
}
