package gplus

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

// ClosureSD 带软删除的闭包模型（scenario 2 软删除分支测试用，AC-2/AC-3）。
type ClosureSD struct {
	ID           int64          `gorm:"column:id;primaryKey;autoIncrement"`
	AncestorID   uint           `gorm:"column:ancestor_id"`
	DescendantID uint           `gorm:"column:descendant_id"`
	Depth        uint           `gorm:"column:depth"`
	DeletedAt    gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (ClosureSD) TableName() string { return "closure_sd" }

// AC-1：无软删除 closure 自连接 InsertSelect 真插入正确（P1 正路防回归）。
func TestInsertSelectJoin_inserts_row_when_no_softdelete(t *testing.T) {
	repo, db := setupTestDB[Closure](t)
	ctx := context.Background()
	seeds := []Closure{{AncestorID: 1, DescendantID: 5, Depth: 0}, {AncestorID: 5, DescendantID: 7, Depth: 0}}
	for i := range seeds {
		if err := repo.Save(ctx, &seeds[i]); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	q, ext := repo.NewQueryAs(ctx, "ext")
	sub := As[Closure](q, "sub")
	q.CrossJoinAs(sub).WhereRaw("sub.ancestor_id = ?", 5).Eq(&ext.DescendantID, 5)
	q.SelectRaw("ext.ancestor_id").
		SelectRaw("sub.descendant_id").
		SelectRaw("ext.depth + sub.depth + 1")

	affected, err := InsertSelect(repo, ctx, []any{"ancestor_id", "descendant_id", "depth"}, q)
	if err != nil {
		t.Fatalf("InsertSelect 应成功，实际: %v", err)
	}
	if affected != 1 {
		t.Fatalf("affected 期望 1，实际 %d", affected)
	}
	// 逐字段断言新增行 {1,7,1}——禁止只数行数：列错位/错误投影仍满足行数=3（红队实测 {7,1,1} 假绿）。
	var got []Closure
	db.Order("id").Find(&got)
	if len(got) != 3 {
		t.Fatalf("总行数期望 3，实际 %d", len(got))
	}
	nw := got[2]
	if nw.AncestorID != 1 || nw.DescendantID != 7 || nw.Depth != 1 {
		t.Fatalf("新增行期望 {1,7,1}，实际 {%d,%d,%d}", nw.AncestorID, nw.DescendantID, nw.Depth)
	}
}
