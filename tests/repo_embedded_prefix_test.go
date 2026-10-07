package gplus_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
)

func TestEmbeddedPrefix_QueryUpdateStoredRows(t *testing.T) {
	repo, db := setupTestDB[prefixModel](t)
	row := prefixModel{PrefixAnonymousValue: PrefixAnonymousValue{Street: "street"}, PrefixAnonymousPointer: &PrefixAnonymousPointer{Zone: "zone"}, Billing: PrefixAddress{City: "billing", Code: "B1"}, Shipping: &PrefixAddress{City: "shipping"}, Nested: PrefixNested{Details: &PrefixAddress{City: "details"}, Home: PrefixAddress{City: "home"}}}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	q, m := NewQuery[prefixModel](context.Background())
	q.Eq(&m.Billing.City, "billing")
	rows, err := repo.List(q)
	if err != nil || len(rows) != 1 || rows[0].ID != row.ID {
		t.Fatalf("prefixed query rows=%+v err=%v", rows, err)
	}
	assertPrefixPointersInitialized(t, m)
	u, um := NewUpdater[prefixModel](context.Background())
	u.Set(&um.Billing.Code, "B2").Set(&um.Shipping.City, "new shipping").Set(&um.Nested.Details.City, "new details").Eq(&um.ID, row.ID)
	var updateSQL string
	if err := db.Callback().Update().After("gorm:update").Register("test:prefix_update_sql", func(tx *gorm.DB) {
		updateSQL = tx.Statement.SQL.String()
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove("test:prefix_update_sql") })
	if affected, err := repo.UpdateByCond(u); err != nil || affected != 1 {
		t.Fatalf("affected=%d err=%v", affected, err)
	}
	for _, name := range []string{"bill_postal", "ship_city", "outer_detail_city"} {
		if !strings.Contains(updateSQL, name) {
			t.Fatalf("SET SQL missing %s: %s", name, updateSQL)
		}
	}
	var stored prefixModel
	if err := db.First(&stored, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Billing.Code != "B2" || stored.Shipping.City != "new shipping" || stored.Nested.Details.City != "new details" || stored.Billing.City != "billing" || stored.Nested.Home.City != "home" {
		t.Fatalf("unexpected stored fields: %+v", stored)
	}
	aliased, am := NewQueryAs[prefixModel](context.Background(), "p")
	aliased.Eq(&am.Shipping.City, "new shipping").Eq(&am.Nested.Details.City, "new details")
	rows, err = repo.List(aliased)
	if err != nil || len(rows) != 1 || rows[0].ID != row.ID {
		t.Fatalf("alias query rows=%+v err=%v", rows, err)
	}
}

type PrefixVersionFields struct {
	Version int64 `gorm:"column:revision" gplus:"version"`
	Name    string
}

type prefixVersionRow struct {
	ID      int64               `gorm:"primaryKey"`
	Lock    PrefixVersionFields `gorm:"embedded;embeddedPrefix:lock_"`
	Address PrefixAddress       `gorm:"embedded;embeddedPrefix:addr_"`
}

func TestEmbeddedPrefix_OptimisticLockAndBusinessUpdate(t *testing.T) {
	repo, db := setupTestDB[prefixVersionRow](t)
	row := prefixVersionRow{Lock: PrefixVersionFields{Version: 3, Name: "old"}, Address: PrefixAddress{City: "old city", Code: "old code"}}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	stale := row
	row.Lock.Name, row.Address.City, row.Address.Code = "updated", "new city", "new code"
	if err := repo.UpdateById(context.Background(), &row); err != nil {
		t.Fatal(err)
	}
	if row.Lock.Version != 4 {
		t.Fatalf("version=%d want=4", row.Lock.Version)
	}
	stale.Lock.Name, stale.Address.City = "stale", "stale city"
	if err := repo.UpdateById(context.Background(), &stale); !errors.Is(err, ErrOptimisticLock) {
		t.Fatalf("stale update err=%v", err)
	}
	var stored prefixVersionRow
	if err := db.First(&stored, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Lock.Version != 4 || stored.Lock.Name != "updated" || stored.Address.City != "new city" || stored.Address.Code != "new code" {
		t.Fatalf("unexpected stored row: %+v", stored)
	}
}
