package gplus

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type stringPrimaryRecord struct {
	Key       string `gorm:"column:record_key;primaryKey"`
	Name      string
	TenantID  int
	DeletedAt gorm.DeletedAt
}

func setupStringPrimaryDB(t *testing.T, id string) (*Repository[string, stringPrimaryRecord], *gorm.DB) {
	t.Helper()
	db := openDB(t).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	if db.Name() == "sqlite" {
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		sqlDB.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := db.AutoMigrate(&stringPrimaryRecord{}); err != nil {
		t.Fatal(err)
	}
	if db.Name() == "mysql" || db.Name() == "postgres" {
		truncateTables(t, db, &stringPrimaryRecord{})
		t.Cleanup(func() { truncateTables(t, db, &stringPrimaryRecord{}) })
	}
	rows := []stringPrimaryRecord{
		{Key: "000-other", Name: "other", TenantID: 2},
		{Key: id, Name: "target", TenantID: 1},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return NewRepository[string, stringPrimaryRecord](db), db
}

func readStringPrimaryRows(t *testing.T, db *gorm.DB) map[string]stringPrimaryRecord {
	t.Helper()
	var rows []stringPrimaryRecord
	if err := db.Unscoped().Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	result := make(map[string]stringPrimaryRecord, len(rows))
	for _, row := range rows {
		result[row.Key] = row
	}
	return result
}

func TestRepository_StringPrimaryKey_ReadDeleteRestore(t *testing.T) {
	for _, id := range []string{"550e8400-e29b-41d4-a716-446655440000", "user-a", "1=1"} {
		for _, useTx := range []bool{false, true} {
			for _, operation := range []string{"read", "delete", "restore"} {
				mode := "direct"
				if useTx {
					mode = "tx"
				}
				t.Run(id+"/"+mode+"/"+operation, func(t *testing.T) {
					repo, db := setupStringPrimaryDB(t, id)
					if operation == "restore" {
						if err := db.Where("record_key IN ?", []string{id, "000-other"}).Delete(&stringPrimaryRecord{}).Error; err != nil {
							t.Fatal(err)
						}
					}
					run := func(tx *gorm.DB) error {
						ctx := context.Background()
						var affected int64
						var err error
						switch operation {
						case "read":
							var got stringPrimaryRecord
							if useTx {
								got, err = repo.GetByIdTx(ctx, id, tx)
							} else {
								got, err = repo.GetById(ctx, id)
							}
							if err == nil && (got.Key != id || got.Name != "target") {
								t.Errorf("read returned wrong row: %+v", got)
							}
							return err
						case "delete":
							if useTx {
								affected, err = repo.DeleteByIdTx(ctx, id, tx)
							} else {
								affected, err = repo.DeleteById(ctx, id)
							}
						case "restore":
							if useTx {
								affected, err = repo.RestoreTx(ctx, id, tx)
							} else {
								affected, err = repo.Restore(ctx, id)
							}
						}
						if err == nil && affected != 1 {
							t.Errorf("expected 1 affected row, got %d", affected)
						}
						return err
					}
					var err error
					if useTx {
						err = db.Transaction(run)
					} else {
						err = run(nil)
					}
					if err != nil {
						t.Fatalf("%s failed: %v", operation, err)
					}
					rows := readStringPrimaryRows(t, db)
					if rows[id].Name != "target" || rows[id].DeletedAt.Valid != (operation == "delete") {
						t.Errorf("unexpected target state: %+v", rows[id])
					}
					if rows["000-other"].Name != "other" || rows["000-other"].DeletedAt.Valid != (operation == "restore") {
						t.Errorf("other row changed: %+v", rows["000-other"])
					}
				})
			}
		}
	}
}

func TestRepository_StringPrimaryKey_FirstOrUpdate(t *testing.T) {
	for _, id := range []string{"550e8400-e29b-41d4-a716-446655440000", "1=1"} {
		t.Run(id, func(t *testing.T) {
			repo, db := setupStringPrimaryDB(t, id)
			q, qm := repo.NewQuery(context.Background())
			q.Eq(&qm.Key, id)
			u, um := repo.NewUpdater(context.Background())
			u.Set(&um.Name, "updated")
			got, created, err := repo.FirstOrUpdate(q, u, &stringPrimaryRecord{Key: "fallback"})
			if err != nil {
				t.Fatalf("FirstOrUpdate failed: %v", err)
			}
			if created || got.Key != id || got.Name != "updated" {
				t.Errorf("unexpected result: created=%v row=%+v", created, got)
			}
			rows := readStringPrimaryRows(t, db)
			if rows[id].Name != "updated" {
				t.Errorf("update was not committed: %+v", rows[id])
			}
			if rows["000-other"].Name != "other" {
				t.Errorf("other row changed: %+v", rows["000-other"])
			}
		})
	}
}

func TestRepository_StringPrimaryKey_MissingSQLStyleID(t *testing.T) {
	repo, db := setupStringPrimaryDB(t, "user-a")
	ctx := context.Background()
	if _, err := repo.GetById(ctx, "1=1"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("missing ID should return ErrRecordNotFound, got %v", err)
	}
	if affected, err := repo.DeleteById(ctx, "1=1"); err != nil || affected != 0 {
		t.Errorf("missing ID delete: affected=%d err=%v", affected, err)
	}
	if err := db.Where("record_key IN ?", []string{"user-a", "000-other"}).Delete(&stringPrimaryRecord{}).Error; err != nil {
		t.Fatal(err)
	}
	if affected, err := repo.Restore(ctx, "1=1"); err != nil || affected != 0 {
		t.Errorf("missing ID restore: affected=%d err=%v", affected, err)
	}
	for _, row := range readStringPrimaryRows(t, db) {
		if !row.DeletedAt.Valid {
			t.Errorf("missing ID restored existing row: %+v", row)
		}
	}
}

func TestRepository_StringPrimaryKey_DataRule(t *testing.T) {
	repo, db := setupStringPrimaryDB(t, "1=1")
	ctx := ctxWithTenantRule(2)
	if _, err := repo.GetById(ctx, "1=1"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("cross-tenant ID should be invisible, got %v", err)
	}
	if affected, err := repo.DeleteById(ctx, "1=1"); err != nil || affected != 0 {
		t.Errorf("cross-tenant delete: affected=%d err=%v", affected, err)
	}
	rows := readStringPrimaryRows(t, db)
	if rows["1=1"].DeletedAt.Valid || rows["000-other"].DeletedAt.Valid {
		t.Error("cross-tenant delete changed existing rows")
	}
	if err := db.Where("record_key IN ?", []string{"1=1", "000-other"}).Delete(&stringPrimaryRecord{}).Error; err != nil {
		t.Fatal(err)
	}
	if affected, err := repo.Restore(ctx, "1=1"); err != nil || affected != 0 {
		t.Errorf("cross-tenant restore: affected=%d err=%v", affected, err)
	}
	for _, row := range readStringPrimaryRows(t, db) {
		if !row.DeletedAt.Valid {
			t.Errorf("cross-tenant restore changed row: %+v", row)
		}
	}
}
