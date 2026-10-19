package gplus_test

import (
	"context"
	"testing"

	. "github.com/yi-nanping/gplus"
)

// 不提供 TableName，验证来源列限定名与 GORM 的默认复数表名一致。
type typedBuilderCompany struct {
	ID   int64
	Name string `gorm:"column:company_label"`
}

func TestTypedColumns_DefaultPluralTableName(t *testing.T) {
	repo, _ := setupTestDB[typedBuilderCompany](t)
	ctx := context.Background()
	company := typedBuilderCompany{Name: "example"}
	if err := repo.Save(ctx, &company); err != nil {
		t.Fatal(err)
	}
	q, model := NewQuery[typedBuilderCompany](ctx)
	q.SelectAs(&model.Name, "label").EqColumn(&model.ID, &model.ID)
	var rows []struct{ Label string }
	if err := FindAs(repo, q, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Label != "example" {
		t.Fatalf("unexpected projection: %+v", rows)
	}
	// 默认子查询也须使用相同物理表名，不能退回简单追加 s 的命名。
	outer, m := NewQueryAs[typedBuilderCompany](ctx, "company")
	sub, inner := SubQuery[typedBuilderCompany](outer)
	sub.EqColumn(&inner.ID, &m.ID)
	outer.Exists(sub)
	list, err := repo.List(outer)
	if err != nil || len(list) != 1 || list[0].ID != company.ID {
		t.Fatalf("unexpected correlated result: %+v, err=%v", list, err)
	}
}
