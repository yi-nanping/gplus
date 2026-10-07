package gplus_test

import (
	"context"
	"errors"
	"testing"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
)

func TestOptimisticLock_OuterRollbackRequiresReloadBeforeRetry(t *testing.T) {
	repo, db := setupTestDB[UserWithVersion](t)
	ctx := context.Background()
	row := UserWithVersion{Name: "original", Version: 1}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("rollback outer transaction")
	err := repo.Transaction(ctx, func(tx *gorm.DB) error {
		row.Name = "rolled back update"
		if err := repo.UpdateByIdTx(ctx, &row, tx); err != nil {
			return err
		}
		inside, err := repo.GetByIdTx(ctx, row.ID, tx)
		if err != nil || inside.Name != "rolled back update" || inside.Version != 2 {
			t.Errorf("transaction update was not observable: inside=%+v err=%v", inside, err)
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) || row.Version != 2 {
		t.Fatalf("rollback must retain incremented entity version: row=%+v err=%v", row, err)
	}
	stored, err := repo.GetById(ctx, row.ID)
	if err != nil || stored.Name != "original" || stored.Version != 1 {
		t.Fatalf("outer rollback did not restore database row: stored=%+v err=%v", stored, err)
	}
	if err := repo.UpdateById(ctx, &row); !errors.Is(err, ErrOptimisticLock) {
		t.Errorf("retry with incremented stale entity must conflict, got %v", err)
	}
	refreshed, err := repo.GetById(ctx, row.ID)
	if err != nil || refreshed != stored {
		t.Fatalf("failed stale retry changed database row: row=%+v err=%v", refreshed, err)
	}
	refreshed.Name = "retry after reload"
	if err := repo.UpdateById(ctx, &refreshed); err != nil {
		t.Fatal(err)
	}
	stored, err = repo.GetById(ctx, row.ID)
	if err != nil || stored.Name != "retry after reload" || stored.Version != 2 || refreshed.Version != 2 {
		t.Errorf("retry after reload failed: entity=%+v stored=%+v err=%v", refreshed, stored, err)
	}
}
