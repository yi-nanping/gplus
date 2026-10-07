package gplus

import (
	"context"
	"strings"
	"testing"
)

// AC-5：Clear 后 mainAlias/mainAliasTable 重置，FROM 不含别名
func TestMainAlias_clear_resets_alias_fields(t *testing.T) {
	db := newDryRunDB(t)
	repo := NewRepository[int64, Closure](db)
	ctx := context.Background()
	q, _ := repo.NewQueryAs(ctx, "ext")

	q.Clear()

	if q.mainAlias != "" || q.mainAliasTable != "" {
		t.Fatalf("Clear 后 mainAlias/mainAliasTable 应为空，实际 mainAlias=%q mainAliasTable=%q", q.mainAlias, q.mainAliasTable)
	}
	sql, _ := q.ToSQL(db)
	if strings.Contains(stripIdentQuotes(sql), "AS ext") {
		t.Errorf("Clear 后 FROM 不应含 AS ext，实际: %s", sql)
	}
}

// M-1：validTableName 注入守卫表驱动单测
func TestMainAlias_validTableName_guard(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"裸表名", "closure", true},
		{"带数字下划线", "closure_2024", true},
		{"单点 schema.table", "main.closure", true},
		{"引号闭合注入", `x"; DROP TABLE closure; --`, false},
		{"AS 注入", "closure AS evil", false},
		{"空格", "clo sure", false},
		{"空串", "", false},
		{"数字开头", "2closure", false},
		{"多段 a.b.c 不支持", "a.b.c", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validTableName.MatchString(tc.input); got != tc.want {
				t.Errorf("validTableName(%q) = %v，期望 %v", tc.input, got, tc.want)
			}
		})
	}
}
