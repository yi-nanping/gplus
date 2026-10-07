package gplus_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"gorm.io/gorm"
)

type firstOrUpdateContextRecord struct {
	ID   int64 `gorm:"primaryKey;autoIncrement"`
	Name string
}

func TestFirstOrUpdate_Context_CanceledUpdaterRollsBack(t *testing.T) {
	repo, db := setupTestDB[firstOrUpdateContextRecord](t)
	row := firstOrUpdateContextRecord{Name: "original"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	q, qm := repo.NewQuery(context.Background())
	q.Eq(&qm.ID, row.ID)
	u, um := repo.NewUpdater(ctx)
	u.Eq(&um.ID, row.ID).Set(&um.Name, "updated")
	if affected, err := repo.UpdateByCond(u); !errors.Is(err, context.Canceled) || affected != 0 {
		t.Fatalf("UpdateByCond cancellation: affected=%d err=%v", affected, err)
	}
	_, created, err := repo.FirstOrUpdate(q, u, &firstOrUpdateContextRecord{Name: "fallback"})
	if !errors.Is(err, context.Canceled) || created {
		t.Errorf("FirstOrUpdate cancellation: created=%v err=%v", created, err)
	}
	got, err := repo.GetById(context.Background(), row.ID)
	if err != nil || got.Name != "original" {
		t.Errorf("canceled update changed stored row: got=%+v err=%v", got, err)
	}
}

func TestFirstOrUpdate_Context_CanceledQueryHasNoWrite(t *testing.T) {
	for _, scenario := range []string{"cancel", "deadline"} {
		for _, existing := range []bool{false, true} {
			name := "create"
			if existing {
				name = "update"
			}
			t.Run(scenario+"/"+name, func(t *testing.T) {
				repo, db := setupTestDB[firstOrUpdateContextRecord](t)
				if existing {
					if err := db.Create(&firstOrUpdateContextRecord{Name: "original"}).Error; err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithCancel(context.Background())
				want := context.Canceled
				if scenario == "deadline" {
					cancel()
					ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
					want = context.DeadlineExceeded
				}
				cancel()
				q, qm := repo.NewQuery(ctx)
				q.Eq(&qm.Name, "original")
				u, um := repo.NewUpdater(context.Background())
				u.Set(&um.Name, "updated")
				_, created, err := repo.FirstOrUpdate(q, u, &firstOrUpdateContextRecord{Name: "fallback"})
				if !errors.Is(err, want) || created {
					t.Errorf("query cancellation: created=%v err=%v", created, err)
				}
				var rows []firstOrUpdateContextRecord
				if err := db.Find(&rows).Error; err != nil {
					t.Fatal(err)
				}
				if (!existing && len(rows) != 0) || (existing && (len(rows) != 1 || rows[0].Name != "original")) {
					t.Errorf("canceled query wrote rows: %+v", rows)
				}
			})
		}
	}
}

func TestFirstOrUpdate_Context_UpdateCallbackErrorRollsBack(t *testing.T) {
	repo, db := setupTestDB[firstOrUpdateContextRecord](t)
	row := firstOrUpdateContextRecord{Name: "original"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("update callback failed")
	var updated int64
	callbackName := "test:first_or_update_failed_update"
	if err := db.Callback().Update().After("gorm:update").Before("gorm:commit_or_rollback_transaction").Register(callbackName, func(d *gorm.DB) {
		updated = d.RowsAffected
		d.AddError(sentinel)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })
	q, qm := repo.NewQuery(context.Background())
	q.Eq(&qm.ID, row.ID)
	u, um := repo.NewUpdater(context.Background())
	u.Set(&um.Name, "updated")
	_, created, err := repo.FirstOrUpdate(q, u, &firstOrUpdateContextRecord{Name: "fallback"})
	if !errors.Is(err, sentinel) || created || updated != 1 {
		t.Errorf("update callback error: created=%v affected=%d err=%v", created, updated, err)
	}
	got, err := repo.GetById(context.Background(), row.ID)
	if err != nil || got.Name != "original" {
		t.Errorf("failed update was not rolled back: got=%+v err=%v", got, err)
	}
}

type firstOrUpdateContextKey struct{}

func TestFirstOrUpdate_Context_StageMarkersAndIsolation(t *testing.T) {
	repo, db := setupTestDB[firstOrUpdateContextRecord](t)
	row := firstOrUpdateContextRecord{Name: "original"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	var queryMarkers, updateMarkers, createMarkers []string
	observe := func(markers *[]string) func(*gorm.DB) {
		return func(d *gorm.DB) {
			marker, _ := d.Statement.Context.Value(firstOrUpdateContextKey{}).(string)
			*markers = append(*markers, marker)
		}
	}
	queryName, updateName, createName := "test:first_or_update_query_context", "test:first_or_update_update_context", "test:first_or_update_create_context"
	if err := db.Callback().Query().Before("gorm:query").Register(queryName, observe(&queryMarkers)); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register(updateName, observe(&updateMarkers)); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register(createName, observe(&createMarkers)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(queryName)
		_ = db.Callback().Update().Remove(updateName)
		_ = db.Callback().Create().Remove(createName)
	})
	qCtx := context.WithValue(context.Background(), firstOrUpdateContextKey{}, "query")
	uCtx := context.WithValue(context.Background(), firstOrUpdateContextKey{}, "update")
	q, qm := repo.NewQuery(qCtx)
	q.Eq(&qm.ID, row.ID)
	u, um := repo.NewUpdater(uCtx)
	u.Set(&um.Name, "updated")
	got, created, err := repo.FirstOrUpdate(q, u, &firstOrUpdateContextRecord{Name: "fallback"})
	if err != nil || created || got.Name != "updated" {
		t.Fatalf("FirstOrUpdate failed: got=%+v created=%v err=%v", got, created, err)
	}
	lookupCtx := context.WithValue(context.Background(), firstOrUpdateContextKey{}, "lookup")
	got, err = repo.GetById(lookupCtx, row.ID)
	if err != nil || got.Name != "updated" {
		t.Fatalf("GetById failed: got=%+v err=%v", got, err)
	}
	standaloneCtx := context.WithValue(context.Background(), firstOrUpdateContextKey{}, "standalone")
	u, um = repo.NewUpdater(standaloneCtx)
	u.Eq(&um.ID, row.ID).Set(&um.Name, "standalone updated")
	if affected, err := repo.UpdateByCond(u); err != nil || affected != 1 {
		t.Fatalf("UpdateByCond failed: affected=%d err=%v", affected, err)
	}
	q, qm = repo.NewQuery(qCtx)
	q.Eq(&qm.Name, "missing")
	canceledCtx, cancel := context.WithCancel(uCtx)
	cancel()
	u, um = repo.NewUpdater(canceledCtx)
	u.Set(&um.Name, "unused update")
	got, created, err = repo.FirstOrUpdate(q, u, &firstOrUpdateContextRecord{Name: "created"})
	if err != nil || !created || got.Name != "created" {
		t.Fatalf("create should use query context: got=%+v created=%v err=%v", got, created, err)
	}
	if !slices.Equal(queryMarkers, []string{"query", "query", "lookup", "query"}) ||
		!slices.Equal(updateMarkers, []string{"update", "standalone"}) || !slices.Equal(createMarkers, []string{"query"}) {
		t.Errorf("unexpected context markers: query=%v update=%v create=%v", queryMarkers, updateMarkers, createMarkers)
	}
}

func TestFirstOrUpdate_UpdaterConditionMissReturnsUnchangedRow(t *testing.T) {
	repo, db := setupTestDB[firstOrUpdateContextRecord](t)
	row := firstOrUpdateContextRecord{Name: "original"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	q, qm := repo.NewQuery(context.Background())
	q.Eq(&qm.ID, row.ID)
	u, um := repo.NewUpdater(context.Background())
	u.Eq(&um.Name, "unmatched").Set(&um.Name, "updated")
	got, created, err := repo.FirstOrUpdate(q, u, &firstOrUpdateContextRecord{Name: "fallback"})
	if err != nil || created || got != row {
		t.Errorf("condition miss must return found row unchanged: got=%+v created=%v err=%v", got, created, err)
	}
	var rows []firstOrUpdateContextRecord
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0] != row {
		t.Errorf("condition miss changed stored rows: %+v", rows)
	}
}
