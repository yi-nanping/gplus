package gplus_test

import (
	"context"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
	"testing"
)

// invalidDataRuleCtx 返回含非法列名 DataRule 的 context，
// 使 DataRuleBuilder().GetError() 返回错误（而 q.GetError() 不受影响）。
func invalidDataRuleCtx() context.Context {
	rules := []DataRule{{Column: "bad(col)", Condition: "=", Value: "1"}}
	return context.WithValue(context.Background(), DataRuleKey, rules)
}

func TestApplySelects_OmitPath(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "OmitUser", Age: 30})

	q, m := NewQuery[TestUser](ctx)
	q.Omit(&m.Email) // 触发 applySelects 的 len(b.omits)>0 分支
	list, err := repo.List(q)
	if err != nil {
		t.Errorf("Omit 不应报错: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("期望 1 条，实际 %d", len(list))
	}
}

// 根白盒测试确认空 preload 项被丢弃；这里验证等价的无 preload 查询实际返回记录。
func TestApplyPreloads_EmptyQuery(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	seed := TestUser{Name: "PreloadUser", Age: 20}
	if err := db.Create(&seed).Error; err != nil {
		t.Fatal(err)
	}
	q, _ := NewQuery[TestUser](context.Background())
	list, err := repo.List(q)
	if err != nil || len(list) != 1 || list[0].ID != seed.ID || list[0].Name != seed.Name {
		t.Fatalf("无 preload 查询应返回原记录: rows=%v err=%v", list, err)
	}
}

func TestGetByLock_DataRuleError(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := invalidDataRuleCtx()

	var err error
	_ = db.Transaction(func(tx *gorm.DB) error {
		q, m := NewQuery[TestUser](ctx)
		q.Eq(&m.ID, 1)
		_, err = repo.GetByLock(q, tx)
		return err
	})
	if err == nil {
		t.Error("GetByLock 非法 DataRule 应返回错误")
	}
}
