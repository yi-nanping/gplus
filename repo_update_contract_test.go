package gplus

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

type updateContractRow struct {
	ID       int64 `gorm:"primaryKey;autoIncrement"`
	TenantID int
	Weight   int
	Enabled  bool
	Label    string
	Note     string
}

type updateContractVersionRow struct {
	ID       int64 `gorm:"primaryKey;autoIncrement"`
	TenantID int
	Weight   int
	Enabled  bool
	Label    string
	Note     string
	Version  int64 `gplus:"version"`
}

// 直接读取完整测试表，验证零值落库、未选字段保留及没有 INSERT 副作用。
func assertUpdateContractRows[T comparable](t *testing.T, db *gorm.DB, want ...T) {
	t.Helper()
	var rows []T
	if err := db.Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(want) {
		t.Fatalf("rows=%+v，期望 %+v", rows, want)
	}
	for i := range rows {
		if rows[i] != want[i] {
			t.Fatalf("rows[%d]=%+v，期望 %+v", i, rows[i], want[i])
		}
	}
}

func TestUpdateById_ZeroValueContract(t *testing.T) {
	t.Run("WithoutVersion", func(t *testing.T) {
		repo, db := setupTestDB[updateContractRow](t)
		initial := updateContractRow{TenantID: 1, Weight: 9, Enabled: true, Label: "keep", Note: "old"}
		if err := db.Create(&initial).Error; err != nil {
			t.Fatal(err)
		}
		entity := updateContractRow{ID: initial.ID, Note: "changed"}
		if err := repo.UpdateById(context.Background(), &entity); err != nil {
			t.Fatal(err)
		}
		want := initial
		want.Note = "changed"
		assertUpdateContractRows(t, db, want)
	})
	t.Run("WithVersion", func(t *testing.T) {
		repo, db := setupTestDB[updateContractVersionRow](t)
		initial := updateContractVersionRow{TenantID: 1, Weight: 9, Enabled: true, Label: "keep", Note: "old", Version: 4}
		if err := db.Create(&initial).Error; err != nil {
			t.Fatal(err)
		}
		entity := updateContractVersionRow{ID: initial.ID, Note: "changed", Version: initial.Version}
		if err := repo.UpdateById(context.Background(), &entity); err != nil {
			t.Fatal(err)
		}
		want := initial
		want.Note, want.Version = "changed", 5
		assertUpdateContractRows(t, db, want)
		if entity.Version != want.Version {
			t.Fatalf("entity.Version=%d，期望回写 %d", entity.Version, want.Version)
		}
	})
}

func TestUpdateByCond_ZeroValuesPreserveOtherFields(t *testing.T) {
	repo, db := setupTestDB[updateContractRow](t)
	initial := updateContractRow{TenantID: 1, Weight: 9, Enabled: true, Label: "clear", Note: "keep"}
	if err := db.Create(&initial).Error; err != nil {
		t.Fatal(err)
	}
	u, m := NewUpdater[updateContractRow](ctxWithTenantRule(1))
	u.Eq(&m.ID, initial.ID).Set(&m.Weight, 0).Set(&m.Enabled, false).Set(&m.Label, "")
	affected, err := repo.UpdateByCond(u)
	if err != nil || affected != 1 {
		t.Fatalf("affected=%d err=%v，期望更新 1 行", affected, err)
	}
	want := initial
	want.Weight, want.Enabled, want.Label = 0, false, ""
	assertUpdateContractRows(t, db, want)
}

func TestUpdateByCond_OnlyUpdate(t *testing.T) {
	for _, name := range []string{"Invisible", "Missing"} {
		t.Run(name, func(t *testing.T) {
			repo, db := setupTestDB[updateContractRow](t)
			initial := updateContractRow{TenantID: 1, Weight: 9, Enabled: true, Label: "keep", Note: "keep"}
			if err := db.Create(&initial).Error; err != nil {
				t.Fatal(err)
			}
			id, tenant := initial.ID, 2
			if name == "Missing" {
				id, tenant = initial.ID+1000, 1
			}
			u, m := NewUpdater[updateContractRow](ctxWithTenantRule(tenant))
			u.Eq(&m.ID, id).Set(&m.Weight, 0).Set(&m.Enabled, false).Set(&m.Label, "")
			affected, err := repo.UpdateByCond(u)
			if err != nil || affected != 0 {
				t.Fatalf("affected=%d err=%v，期望不匹配目标时影响 0 行", affected, err)
			}
			assertUpdateContractRows(t, db, initial)
		})
	}
}

func TestUpdateByCond_ExplicitVersionContract(t *testing.T) {
	repo, db := setupTestDB[updateContractVersionRow](t)
	initial := updateContractVersionRow{TenantID: 1, Weight: 9, Enabled: true, Label: "clear", Note: "keep", Version: 4}
	if err := db.Create(&initial).Error; err != nil {
		t.Fatal(err)
	}
	u, m := NewUpdater[updateContractVersionRow](ctxWithTenantRule(1))
	u.Eq(&m.ID, initial.ID).Set(&m.Weight, 0).Set(&m.Enabled, false).Set(&m.Label, "")
	affected, err := repo.UpdateByCond(u)
	if err != nil || affected != 1 {
		t.Fatalf("affected=%d err=%v，期望更新 1 行", affected, err)
	}
	want := initial
	want.Weight, want.Enabled, want.Label = 0, false, ""
	// version 标签不会让 Updater 自动比较或递增版本。
	assertUpdateContractRows(t, db, want)

	u, m = NewUpdater[updateContractVersionRow](ctxWithTenantRule(1))
	u.Eq(&m.ID, initial.ID).Eq(&m.Version, initial.Version).
		Set(&m.Weight, 7).Set(&m.Version, initial.Version+1)
	affected, err = repo.UpdateByCond(u)
	if err != nil || affected != 1 {
		t.Fatalf("显式版本更新 affected=%d err=%v，期望 1 行", affected, err)
	}
	want.Weight, want.Version = 7, initial.Version+1
	assertUpdateContractRows(t, db, want)

	for _, tc := range []struct {
		name    string
		id      int64
		tenant  int
		version int64
	}{
		{name: "StaleVersion", id: initial.ID, tenant: 1, version: initial.Version},
		{name: "Invisible", id: initial.ID, tenant: 2, version: want.Version},
		{name: "Missing", id: initial.ID + 1000, tenant: 1, version: want.Version},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, m := NewUpdater[updateContractVersionRow](ctxWithTenantRule(tc.tenant))
			u.Eq(&m.ID, tc.id).Eq(&m.Version, tc.version).
				Set(&m.Weight, 99).Set(&m.Enabled, true).Set(&m.Version, tc.version+1)
			affected, err := repo.UpdateByCond(u)
			if err != nil || affected != 0 {
				t.Fatalf("affected=%d err=%v，期望 0 行且无自动乐观锁错误", affected, err)
			}
			assertUpdateContractRows(t, db, want)
		})
	}
}

func TestUpsert_FullFieldsWithoutAutomaticRules(t *testing.T) {
	repo, db := setupTestDB[updateContractVersionRow](t)
	initial := updateContractVersionRow{TenantID: 1, Weight: 9, Enabled: true, Label: "clear", Note: "clear", Version: 4}
	if err := db.Create(&initial).Error; err != nil {
		t.Fatal(err)
	}
	// Upsert 是 GORM Save：覆盖零值，不注入 gplus DataRule，也不自动比较版本。
	entity := updateContractVersionRow{ID: initial.ID, TenantID: 1, Version: 3}
	if err := repo.Upsert(ctxWithTenantRule(2), &entity); err != nil {
		t.Fatal(err)
	}
	assertUpdateContractRows(t, db, entity)
	if entity.Version != 3 {
		t.Fatalf("Upsert 不应自动回写版本，实际 Version=%d", entity.Version)
	}
}

func TestUpsert_MissingPrimaryKeyInserts(t *testing.T) {
	repo, db := setupTestDB[updateContractRow](t)
	entity := updateContractRow{ID: 1000, TenantID: 1}
	if err := repo.Upsert(context.Background(), &entity); err != nil {
		t.Fatal(err)
	}
	assertUpdateContractRows(t, db, entity)
}
