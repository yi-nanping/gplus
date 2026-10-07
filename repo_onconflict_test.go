package gplus

import (
	"errors"
	"testing"
)

// TestOnConflict_Validation 校验互斥规则
func TestOnConflict_Validation(t *testing.T) {
	cases := []struct {
		name string
		oc   OnConflict
	}{
		{
			name: "DoNothing+DoUpdateAll 互斥",
			oc:   OnConflict{DoNothing: true, DoUpdateAll: true},
		},
		{
			name: "DoNothing+DoUpdates 互斥",
			oc:   OnConflict{DoNothing: true, DoUpdates: []any{"name"}},
		},
		{
			name: "DoNothing+UpdateExprs 互斥",
			oc:   OnConflict{DoNothing: true, UpdateExprs: map[string]any{"score": 1}},
		},
		{
			name: "DoUpdateAll+DoUpdates 互斥",
			oc:   OnConflict{DoUpdateAll: true, DoUpdates: []any{"name"}},
		},
		{
			name: "DoUpdateAll+UpdateExprs 互斥",
			oc:   OnConflict{DoUpdateAll: true, UpdateExprs: map[string]any{"score": 1}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.oc.buildClause()
			if !errors.Is(err, ErrOnConflictInvalid) {
				t.Errorf("期望 ErrOnConflictInvalid，得到 %v", err)
			}
		})
	}
}

// TestOnConflict_InvalidColumn 无效列指针返回错误
func TestOnConflict_InvalidColumn(t *testing.T) {
	bad := 42 // 不是字段指针
	oc := OnConflict{Columns: []any{&bad}}
	_, err := oc.buildClause()
	if err == nil {
		t.Error("期望列指针解析失败，得到 nil")
	}
}
