package gplus

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type chunkMutationRecord struct {
	ID       int `gorm:"primaryKey;column:cursor_key"`
	TenantID int
}

func setupChunkMutationDB[T any](t *testing.T, rows []T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(new(T)); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func TestChunk_CallbackMutation(t *testing.T) {
	for _, mutation := range []string{"reverse", "last_id", "replace_last"} {
		t.Run(mutation, func(t *testing.T) {
			rows := make([]chunkMutationRecord, 6)
			for i := range rows {
				rows[i].ID = i + 1
			}
			db := setupChunkMutationDB(t, rows)
			repo := NewRepository[int, chunkMutationRecord](db)
			q, _ := repo.NewQuery(context.Background())
			var seen []int
			var retained [][]chunkMutationRecord
			var expected [][]chunkMutationRecord
			err := repo.Chunk(q, 2, func(batch []chunkMutationRecord) error {
				if len(retained) > 6 {
					return errors.New("unexpected repeated batches")
				}
				for _, row := range batch {
					seen = append(seen, row.ID)
				}
				switch mutation {
				case "reverse":
					slices.Reverse(batch)
				case "last_id":
					batch[len(batch)-1].ID = 1000
				case "replace_last":
					batch[len(batch)-1] = chunkMutationRecord{ID: 1000, TenantID: 99}
				}
				retained = append(retained, batch)
				expected = append(expected, slices.Clone(batch))
				return nil
			})
			if err != nil {
				t.Fatalf("Chunk failed: %v", err)
			}
			if !slices.Equal(seen, []int{1, 2, 3, 4, 5, 6}) {
				t.Errorf("original IDs must be visited once: %v", seen)
			}
			if !reflect.DeepEqual(retained, expected) {
				t.Errorf("retained callback batches changed: got %v want %v", retained, expected)
			}
		})
	}
}

func TestChunkTx_CallbackMutation_LimitOffsetDataRule(t *testing.T) {
	rows := make([]chunkMutationRecord, 8)
	for i := range rows {
		rows[i] = chunkMutationRecord{ID: i + 1, TenantID: 1}
	}
	rows[3].TenantID = 2
	db := setupChunkMutationDB(t, rows)
	repo := NewRepository[int, chunkMutationRecord](db)
	q, _ := repo.NewQuery(ctxWithTenantRule(1))
	q.Limit(4).Offset(1)
	var seen []int
	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.ChunkTx(q, 2, tx, func(batch []chunkMutationRecord) error {
			for _, row := range batch {
				seen = append(seen, row.ID)
			}
			batch[len(batch)-1].ID = 1000
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(seen, []int{2, 3, 5, 6}) {
		t.Errorf("unexpected scoped IDs: %v", seen)
	}
	if q.limit != 4 || q.offset != 1 {
		t.Errorf("query pagination changed: limit=%d offset=%d", q.limit, q.offset)
	}
}

func TestChunk_CallbackMutation_Error(t *testing.T) {
	db := setupChunkMutationDB(t, []chunkMutationRecord{{ID: 1}, {ID: 2}, {ID: 3}})
	repo := NewRepository[int, chunkMutationRecord](db)
	q, _ := repo.NewQuery(context.Background())
	sentinel := errors.New("callback failed")
	calls := 0
	err := repo.Chunk(q, 2, func(batch []chunkMutationRecord) error {
		calls++
		batch[len(batch)-1].ID = 1000
		return sentinel
	})
	if !errors.Is(err, sentinel) || calls != 1 {
		t.Errorf("callback error must stop iteration: calls=%d err=%v", calls, err)
	}
}

type ChunkMutationKey struct {
	ID *int `gorm:"primaryKey;autoIncrement:false;column:cursor_key"`
}

type chunkPointerMutationRecord struct {
	*ChunkMutationKey
	Name string
}

func chunkPointerIDs(batch []chunkPointerMutationRecord) []int {
	ids := make([]int, len(batch))
	for i, row := range batch {
		switch {
		case row.ChunkMutationKey == nil:
			ids[i] = -1
		case row.ID == nil:
			ids[i] = -2
		default:
			ids[i] = *row.ID
		}
	}
	return ids
}

func TestChunk_CallbackMutation_PointerEmbeddedPrimaryKey(t *testing.T) {
	for _, mutation := range []string{"reverse", "id_value", "replace_last", "nil_embed", "nil_id"} {
		t.Run(mutation, func(t *testing.T) {
			rows := make([]chunkPointerMutationRecord, 6)
			for i := range rows {
				id := i + 1
				rows[i] = chunkPointerMutationRecord{ChunkMutationKey: &ChunkMutationKey{ID: &id}}
			}
			db := setupChunkMutationDB(t, rows)
			repo := NewRepository[int, chunkPointerMutationRecord](db)
			q, _ := repo.NewQuery(context.Background())
			var seen []int
			var retained [][]chunkPointerMutationRecord
			var expected [][]int
			err := repo.Chunk(q, 2, func(batch []chunkPointerMutationRecord) error {
				if len(retained) > 6 {
					return errors.New("unexpected repeated batches")
				}
				seen = append(seen, chunkPointerIDs(batch)...)
				last := &batch[len(batch)-1]
				switch mutation {
				case "reverse":
					slices.Reverse(batch)
				case "id_value":
					*last.ID = 1000
				case "replace_last":
					id := 1000
					*last = chunkPointerMutationRecord{ChunkMutationKey: &ChunkMutationKey{ID: &id}, Name: "changed"}
				case "nil_embed":
					last.ChunkMutationKey = nil
				case "nil_id":
					last.ID = nil
				}
				retained = append(retained, batch)
				expected = append(expected, chunkPointerIDs(batch))
				return nil
			})
			if err != nil {
				t.Fatalf("Chunk failed: %v", err)
			}
			if !slices.Equal(seen, []int{1, 2, 3, 4, 5, 6}) {
				t.Errorf("original IDs must be visited once: %v", seen)
			}
			for i, batch := range retained {
				if ids := chunkPointerIDs(batch); !slices.Equal(ids, expected[i]) {
					t.Errorf("retained batch %d changed: got %v want %v", i, ids, expected[i])
				}
			}
		})
	}
}

type chunkNoPrimaryRecord struct {
	Name string
}

func TestChunk_CallbackMutation_NoPrimaryKeyErrorTiming(t *testing.T) {
	for _, tc := range []struct {
		name      string
		batchSize int
		limit     int
		wantError bool
	}{
		{name: "partial_batch", batchSize: 4},
		{name: "limit_ends_batch", batchSize: 3, limit: 3},
		{name: "needs_cursor", batchSize: 2, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupChunkMutationDB(t, []chunkNoPrimaryRecord{{Name: "a"}, {Name: "b"}, {Name: "c"}})
			repo := NewRepository[string, chunkNoPrimaryRecord](db)
			q, _ := repo.NewQuery(context.Background())
			if tc.limit > 0 {
				q.Limit(tc.limit)
			}
			calls := 0
			err := repo.Chunk(q, tc.batchSize, func(batch []chunkNoPrimaryRecord) error {
				calls++
				slices.Reverse(batch)
				return nil
			})
			if (err != nil) != tc.wantError || (tc.wantError && !errors.Is(err, gorm.ErrPrimaryKeyRequired)) || calls != 1 {
				t.Errorf("unexpected no-primary-key behavior: calls=%d err=%v", calls, err)
			}
		})
	}
}

type chunkZeroPrimaryRecord struct {
	ID int `gorm:"primaryKey;autoIncrement:false"`
}

func TestChunk_CallbackMutation_ZeroPrimaryKeyErrorTiming(t *testing.T) {
	for _, tc := range []struct {
		name      string
		batchSize int
		limit     int
		wantError bool
	}{
		{name: "partial_batch", batchSize: 2},
		{name: "limit_ends_batch", batchSize: 1, limit: 1},
		{name: "needs_cursor", batchSize: 1, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupChunkMutationDB(t, []chunkZeroPrimaryRecord{{ID: 0}})
			repo := NewRepository[int, chunkZeroPrimaryRecord](db)
			q, _ := repo.NewQuery(context.Background())
			if tc.limit > 0 {
				q.Limit(tc.limit)
			}
			calls := 0
			err := repo.Chunk(q, tc.batchSize, func(batch []chunkZeroPrimaryRecord) error {
				calls++
				if len(batch) != 1 || batch[0].ID != 0 {
					t.Errorf("unexpected zero-primary-key batch: %v", batch)
				}
				return nil
			})
			if (err != nil) != tc.wantError || (tc.wantError && !errors.Is(err, gorm.ErrPrimaryKeyRequired)) || calls != 1 {
				t.Errorf("unexpected zero-primary-key behavior: calls=%d err=%v", calls, err)
			}
		})
	}
}

type chunkBytesPrimaryRecord struct {
	ID []byte `gorm:"primaryKey"`
}

func TestChunk_CallbackMutation_BytesPrimaryKey(t *testing.T) {
	rows := make([]chunkBytesPrimaryRecord, 6)
	for i := range rows {
		rows[i].ID = []byte{byte(i + 1)}
	}
	db := setupChunkMutationDB(t, rows)
	repo := NewRepository[string, chunkBytesPrimaryRecord](db)
	q, _ := repo.NewQuery(context.Background())
	var seen []byte
	var retained [][]chunkBytesPrimaryRecord
	var expected [][]chunkBytesPrimaryRecord
	err := repo.Chunk(q, 2, func(batch []chunkBytesPrimaryRecord) error {
		for _, row := range batch {
			seen = append(seen, row.ID...)
		}
		batch[len(batch)-1].ID[0] = 255
		retained = append(retained, batch)
		snapshot := make([]chunkBytesPrimaryRecord, len(batch))
		for i, row := range batch {
			snapshot[i].ID = slices.Clone(row.ID)
		}
		expected = append(expected, snapshot)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(seen, []byte{1, 2, 3, 4, 5, 6}) {
		t.Errorf("original blob IDs must be visited once: %v", seen)
	}
	if !reflect.DeepEqual(retained, expected) {
		t.Errorf("retained blob batches changed: got %v want %v", retained, expected)
	}
}
