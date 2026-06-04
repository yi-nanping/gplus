package gplus

import (
	"context"
	"strings"
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

// AC-2：软删除模型自连接（不 Unscoped）报裸表名错误，零副作用（限制锁）。
// GORM 软删除条件用裸表名 closure_sd.deleted_at，FROM 只有 ext/sub 别名 → 报错（与 Round 3a AC-11 同源）。
func TestInsertSelectJoin_softdelete_bare_column_fails(t *testing.T) {
	repo, db := setupTestDB[ClosureSD](t)
	ctx := context.Background()
	seeds := []ClosureSD{{AncestorID: 1, DescendantID: 5, Depth: 0}, {AncestorID: 5, DescendantID: 7, Depth: 0}}
	for i := range seeds {
		if err := repo.Save(ctx, &seeds[i]); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	q, ext := repo.NewQueryAs(ctx, "ext")
	sub := As[ClosureSD](q, "sub")
	q.CrossJoinAs(sub).WhereRaw("sub.ancestor_id = ?", 5).Eq(&ext.DescendantID, 5)
	q.SelectRaw("ext.ancestor_id").
		SelectRaw("sub.descendant_id").
		SelectRaw("ext.depth + sub.depth + 1")

	_, err := InsertSelect(repo, ctx, []any{"ancestor_id", "descendant_id", "depth"}, q)
	if err == nil || !strings.Contains(err.Error(), "no such column") || !strings.Contains(err.Error(), "closure_sd.deleted_at") {
		t.Fatalf("期望报 no such column: closure_sd.deleted_at（GORM 软删除裸表名被别名遮蔽），实际: %v", err)
	}
	// 零副作用：closure_sd 未删行数仍 2（GORM Count 自动排除软删，本例无软删）。
	var n int64
	db.Model(&ClosureSD{}).Count(&n)
	if n != 2 {
		t.Fatalf("行数期望 2（无插入副作用），实际 %d", n)
	}
}

// AC-3：软删除模型 Unscoped + 手动两侧别名前缀正路，已删行不被复制（出路锁）。
// 两侧各种 1 条已删干扰行：删 sub 前缀会多 {1,88,1}、删 ext 前缀会多 {2,7,1} → 两侧前缀均非死代码。
func TestInsertSelectJoin_unscoped_with_alias_prefix_excludes_deleted(t *testing.T) {
	repo, db := setupTestDB[ClosureSD](t)
	ctx := context.Background()
	// 正常候选（未删）
	normal := []ClosureSD{{AncestorID: 1, DescendantID: 5, Depth: 0}, {AncestorID: 5, DescendantID: 7, Depth: 0}}
	for i := range normal {
		if err := repo.Save(ctx, &normal[i]); err != nil {
			t.Fatalf("seed normal: %v", err)
		}
	}
	// 干扰行（Save 后软删）：sub 侧 5->88，ext 侧 2->5
	deleted := []ClosureSD{{AncestorID: 5, DescendantID: 88, Depth: 0}, {AncestorID: 2, DescendantID: 5, Depth: 0}}
	for i := range deleted {
		if err := repo.Save(ctx, &deleted[i]); err != nil {
			t.Fatalf("seed deleted: %v", err)
		}
		if err := db.Delete(&deleted[i]).Error; err != nil { // 软删除：deleted_at 置当前时间
			t.Fatalf("soft delete: %v", err)
		}
	}

	q, ext := repo.NewQueryAs(ctx, "ext")
	sub := As[ClosureSD](q, "sub")
	q.Unscoped().CrossJoinAs(sub).
		WhereRaw("sub.ancestor_id = ?", 5).
		Eq(&ext.DescendantID, 5).
		WhereRaw("ext.deleted_at IS NULL").
		WhereRaw("sub.deleted_at IS NULL")
	q.SelectRaw("ext.ancestor_id").
		SelectRaw("sub.descendant_id").
		SelectRaw("ext.depth + sub.depth + 1")

	affected, err := InsertSelect(repo, ctx, []any{"ancestor_id", "descendant_id", "depth"}, q)
	if err != nil {
		t.Fatalf("InsertSelect 应成功，实际: %v", err)
	}
	if affected != 1 {
		t.Fatalf("affected 期望 1（两侧前缀排除已删干扰），实际 %d", affected)
	}
	// 新增未删行恰为 {1,7,1}（按 id 升序，新插入 id 最大）。
	var live []ClosureSD
	db.Order("id").Find(&live) // GORM 自动排除软删行：原 2 正常 + 1 新增 = 3
	if len(live) != 3 {
		t.Fatalf("未删行期望 3，实际 %d", len(live))
	}
	nw := live[2]
	if nw.AncestorID != 1 || nw.DescendantID != 7 || nw.Depth != 1 {
		t.Fatalf("新增行期望 {1,7,1}，实际 {%d,%d,%d}", nw.AncestorID, nw.DescendantID, nw.Depth)
	}
	// 已删干扰行未被复制成新未删行：未删行中查无 desc=88 也查无 anc=2（GORM Count 自动排除软删）。
	var leaked int64
	db.Model(&ClosureSD{}).Where("descendant_id = 88 OR ancestor_id = 2").Count(&leaked)
	if leaked != 0 {
		t.Fatalf("已删干扰行不应被复制成未删行，泄漏 %d 条", leaked)
	}
}
