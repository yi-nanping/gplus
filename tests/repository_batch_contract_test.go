package gplus_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestChunk_InvalidArgumentsNoQuery(t *testing.T) {
	for _, tc := range []struct {
		name      string
		batchSize int
		fn        func([]TestUser) error
		wantError string
	}{
		{"ZeroBatchSize", 0, func([]TestUser) error { return nil }, "batchSize"},
		{"NegativeBatchSize", -1, func([]TestUser) error { return nil }, "batchSize"},
		{"NilCallback", 2, nil, "fn"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, db := setupTestDB[TestUser](t)
			queries := 0
			if err := db.Callback().Query().Before("gorm:query").Register("contract:invalid_chunk", func(tx *gorm.DB) {
				queries++
				tx.AddError(errors.New("unexpected query"))
			}); err != nil {
				t.Fatal(err)
			}
			q, _ := NewQuery[TestUser](context.Background())
			err := repo.Chunk(q, tc.batchSize, tc.fn)
			if err == nil || !strings.Contains(err.Error(), tc.wantError) || queries != 0 {
				t.Fatalf("err=%v 查询次数=%d，期望参数错误且不发查询", err, queries)
			}
		})
	}
}

func TestChunk_QueryPaginationLimits(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		limit, offset, batchSize int
		wantSizes, wantAges      []int
	}{
		{"TotalLimit", 5, 0, 2, []int{2, 2, 1}, []int{1, 2, 3, 4, 5}},
		{"LimitAndOffset", 5, 2, 2, []int{2, 2, 1}, []int{3, 4, 5, 6, 7}},
		{"LimitBelowBatchSize", 1, 0, 2, []int{1}, []int{1}},
		{"OffsetWithoutLimit", 0, 2, 2, []int{2, 2, 1}, []int{3, 4, 5, 6, 7}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, db := setupTestDB[TestUser](t)
			for i := 1; i <= 7; i++ {
				if err := db.Create(&TestUser{Age: i}).Error; err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var limits, offsets []int
			if err := db.Callback().Query().After("gorm:query").Register("contract:chunk", func(tx *gorm.DB) {
				if tx.Statement.Context != ctx {
					t.Error("Chunk 丢失 Query Context")
				}
				if tx.Statement.Schema == nil || tx.Statement.Schema.ModelType != reflect.TypeOf(TestUser{}) {
					t.Error("Chunk 丢失模型类型")
				}
				if tx.RowsAffected == 0 {
					return
				}
				limit := tx.Statement.Clauses["LIMIT"].Expression.(clause.Limit)
				limits = append(limits, *limit.Limit)
				offsets = append(offsets, limit.Offset)
				if !strings.Contains(strings.ToUpper(tx.Statement.SQL.String()), "LIMIT") {
					t.Error("查询 SQL 缺少 LIMIT")
				}
			}); err != nil {
				t.Fatal(err)
			}
			q, _ := NewQuery[TestUser](ctx)
			q.Limit(tc.limit).Offset(tc.offset)
			var sizes, ages []int
			err := repo.Chunk(q, tc.batchSize, func(batch []TestUser) error {
				sizes = append(sizes, len(batch))
				for _, row := range batch {
					ages = append(ages, row.Age)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(sizes, tc.wantSizes) || !reflect.DeepEqual(ages, tc.wantAges) {
				t.Errorf("批次=%v 行=%v，期望批次=%v 行=%v", sizes, ages, tc.wantSizes, tc.wantAges)
			}
			wantLimits := append([]int(nil), tc.wantSizes...)
			if tc.limit == 0 {
				for i := range wantLimits {
					wantLimits[i] = tc.batchSize
				}
			}
			if !reflect.DeepEqual(limits, wantLimits) {
				t.Errorf("SQL LIMIT=%v，期望 %v", limits, wantLimits)
			}
			for i, offset := range offsets {
				want := 0
				if i == 0 {
					want = tc.offset
				}
				if offset > 0 && offset != want || want > 0 && offset != want {
					t.Errorf("批次 %d OFFSET=%d，期望 %d", i, offset, want)
				}
			}
			var previewRows []TestUser
			preview := q.ToDB(db.Session(&gorm.Session{DryRun: true})).WithContext(ctx).Find(&previewRows)
			pagination, _ := preview.Statement.Clauses["LIMIT"].Expression.(clause.Limit)
			gotLimit := 0
			if pagination.Limit != nil {
				gotLimit = *pagination.Limit
			}
			if preview.Error != nil || gotLimit != tc.limit || pagination.Offset != tc.offset {
				t.Fatalf("Chunk 修改了 Query 分页参数: limit=%d offset=%d err=%v", gotLimit, pagination.Offset, preview.Error)
			}
		})
	}
}

func TestChunkTx_QueryLimitWithDataRuleAndRollback(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{{Column: "age", Condition: OpGe, Value: "3"}})
	rollback := errors.New("rollback")
	err := db.Transaction(func(tx *gorm.DB) error {
		for i := 1; i <= 9; i++ {
			if err := tx.Create(&TestUser{Age: i}).Error; err != nil {
				return err
			}
		}
		q, _ := NewQuery[TestUser](ctx)
		q.Limit(5)
		var sizes, ages []int
		if err := repo.ChunkTx(q, 2, tx, func(batch []TestUser) error {
			sizes = append(sizes, len(batch))
			for _, row := range batch {
				ages = append(ages, row.Age)
			}
			return nil
		}); err != nil {
			return err
		}
		if !reflect.DeepEqual(sizes, []int{2, 2, 1}) || !reflect.DeepEqual(ages, []int{3, 4, 5, 6, 7}) {
			t.Errorf("批次=%v 行=%v，期望只读当前事务中权限可见的前5条", sizes, ages)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("事务返回 %v，期望回滚标记", err)
	}
	var count int64
	if err := db.Model(&TestUser{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("回滚后 count=%d err=%v，期望没有数据", count, err)
	}
}
