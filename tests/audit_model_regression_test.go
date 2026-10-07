package gplus_test

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
)

type AuditModelAddress struct {
	City string `gorm:"column:city_name"`
}

type AuditModelLockRow struct {
	ID      int64              `gorm:"primaryKey"`
	Version int64              `gplus:"version"`
	Address *AuditModelAddress `gorm:"embedded;embeddedPrefix:addr_"`
}

func openAuditModelDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAuditModel_OptimisticPointerBusinessFields(t *testing.T) {
	db := openAuditModelDB(t, &AuditModelLockRow{})
	row := AuditModelLockRow{Version: 1, Address: &AuditModelAddress{City: "old"}}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewRepository[int64, AuditModelLockRow](db)
	row.Address.City = "new"
	if err := repo.UpdateById(context.Background(), &row); err != nil {
		t.Fatal(err)
	}
	var stored AuditModelLockRow
	if err := db.First(&stored, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Address.City != "new" || stored.Version != 2 || row.Version != 2 {
		t.Fatalf("stored=%+v city=%q entityVersion=%d", stored, stored.Address.City, row.Version)
	}
	// 普通零值和 nil 指针仍按既有非零更新契约忽略。
	for _, address := range []*AuditModelAddress{{City: ""}, nil} {
		row.Address = address
		if err := repo.UpdateById(context.Background(), &row); err != nil {
			t.Fatal(err)
		}
		if err := db.First(&stored, row.ID).Error; err != nil {
			t.Fatal(err)
		}
		if stored.Address.City != "new" || stored.Version != row.Version {
			t.Fatalf("zero/nil business update changed stored row: %+v", stored)
		}
	}
}

type AuditModelEnvelope struct {
	Address *AuditModelAddress `gorm:"embedded;embeddedPrefix:inner_"`
}

type AuditModelNestedRow struct {
	ID      int64               `gorm:"primaryKey"`
	Version int64               `gplus:"version"`
	Value   AuditModelEnvelope  `gorm:"embedded;embeddedPrefix:value_"`
	Pointer *AuditModelEnvelope `gorm:"embedded;embeddedPrefix:pointer_"`
}

func TestAuditModel_OptimisticNestedPointerPrefixes(t *testing.T) {
	db := openAuditModelDB(t, &AuditModelNestedRow{})
	row := AuditModelNestedRow{
		Version: 1,
		Value:   AuditModelEnvelope{Address: &AuditModelAddress{City: "value-old"}},
		Pointer: &AuditModelEnvelope{Address: &AuditModelAddress{City: "pointer-old"}},
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	row.Value.Address.City = "value-new"
	row.Pointer.Address.City = "pointer-new"
	repo := NewRepository[int64, AuditModelNestedRow](db)
	if err := repo.UpdateById(context.Background(), &row); err != nil {
		t.Fatal(err)
	}
	var stored AuditModelNestedRow
	if err := db.First(&stored, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Value.Address.City != "value-new" || stored.Pointer.Address.City != "pointer-new" || stored.Version != 2 || row.Version != 2 {
		t.Fatalf("nested prefixes lost: value=%q pointer=%q storedVersion=%d entityVersion=%d", stored.Value.Address.City, stored.Pointer.Address.City, stored.Version, row.Version)
	}
	q, m := NewQuery[AuditModelNestedRow](context.Background())
	p := As[AuditModelJoinPeer](q, "p")
	if err := db.AutoMigrate(&AuditModelJoinPeer{}); err != nil {
		t.Fatal(err)
	}
	q.LeftJoinAsOn(p, &m.ID, &p.ID, func(on *OnBuilder) {
		on.Eq(&m.Value.Address.City, "value-new").Eq(&m.Pointer.Address.City, "pointer-new")
	})
	rows, err := repo.List(q)
	if err != nil || len(rows) != 1 {
		t.Fatalf("nested pointer ON ownership rejected: rows=%v err=%v", rows, err)
	}
}

type AuditModelCategory struct {
	ID   int64
	Name string
}

func TestAuditModel_AliasGORMPlural(t *testing.T) {
	db := openAuditModelDB(t, &AuditModelCategory{})
	row := AuditModelCategory{Name: "one"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewRepository[int64, AuditModelCategory](db)
	q, m := NewQueryAs[AuditModelCategory](context.Background(), "c")
	q.Eq(&m.Name, "one")
	rows, err := repo.List(q)
	if err != nil || len(rows) != 1 || rows[0].ID != row.ID {
		t.Fatalf("alias query must match migrated GORM table: rows=%v err=%v", rows, err)
	}
}

type AuditModelJoinOwner struct {
	ID      int64
	Address *AuditModelAddress `gorm:"embedded;embeddedPrefix:addr_"`
}

type AuditModelJoinPeer struct {
	ID   int64
	City string
}

func TestAuditModel_NamedPointerJoinOwnership(t *testing.T) {
	db := openAuditModelDB(t, &AuditModelJoinOwner{}, &AuditModelJoinPeer{})
	owners := []AuditModelJoinOwner{
		{Address: &AuditModelAddress{City: "one"}},
		{Address: &AuditModelAddress{City: "two"}},
	}
	if err := db.Create(&owners).Error; err != nil {
		t.Fatal(err)
	}
	peer := AuditModelJoinPeer{City: "one"}
	if err := db.Create(&peer).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewRepository[int64, AuditModelJoinOwner](db)
	q, m := NewQuery[AuditModelJoinOwner](context.Background())
	p := As[AuditModelJoinPeer](q, "p")
	q.LeftJoinAsOn(p, &m.ID, &p.ID, func(on *OnBuilder) { on.Eq(&m.Address.City, "one") })
	q.Select(&m.ID, "p.city AS matched_city").Order(&m.ID, true)
	var joined []struct {
		ID          int64
		MatchedCity *string
	}
	if err := q.ToDB(db).Scan(&joined).Error; err != nil {
		t.Fatal(err)
	}
	if len(joined) != 2 || joined[0].MatchedCity == nil || *joined[0].MatchedCity != "one" || joined[1].MatchedCity != nil {
		t.Fatalf("ON must match first row and preserve unmatched owner: %+v", joined)
	}
	bad, bm := NewQuery[AuditModelJoinOwner](context.Background())
	bp := As[AuditModelJoinPeer](bad, "p")
	foreign := AuditModelJoinOwner{Address: &AuditModelAddress{City: "one"}}
	bad.LeftJoinAsOn(bp, &bm.ID, &bp.ID, func(on *OnBuilder) { on.Eq(&foreign.Address.City, "one") })
	if _, err := repo.List(bad); err == nil {
		t.Fatal("ordinary entity field must remain rejected")
	}
}
