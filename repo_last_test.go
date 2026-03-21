package gplus

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

// TestLast_NilQuery 验证 nil query 返回 ErrQueryNil
func TestLast_NilQuery(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	_, err := repo.Last(nil)
	assertError(t, err, true, "nil query 应返回错误")
	if !errors.Is(err, ErrQueryNil) {
		t.Errorf("期望 ErrQueryNil，得到 %v", err)
	}
}

// TestLast_QueryBuilderError 验证构建器错误被提前返回
func TestLast_QueryBuilderError(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	q, _ := NewQuery[TestUser](context.Background())
	q.Eq(nil, "bad") // 累积错误
	_, err := repo.Last(q)
	assertError(t, err, true, "构建器错误应被返回")
}

// TestLast_NotFound 验证无匹配记录时返回 ErrRecordNotFound
func TestLast_NotFound(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	q, u := NewQuery[TestUser](context.Background())
	q.Eq(&u.Name, "notexist")
	_, err := repo.Last(q)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("期望 ErrRecordNotFound，得到 %v", err)
	}
}

// TestLast_Normal 验证返回主键最大的记录
func TestLast_Normal(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 20})
	db.Create(&TestUser{Name: "Bob", Age: 25})
	db.Create(&TestUser{Name: "Charlie", Age: 30})

	q, _ := NewQuery[TestUser](ctx)
	user, err := repo.Last(q)
	assertError(t, err, false, "Last 应成功")
	assertEqual(t, "Charlie", user.Name, "Last 应返回最后插入的记录")
}

// TestLast_WithCondition 验证带条件时返回符合条件的最后一条
func TestLast_WithCondition(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 20})
	db.Create(&TestUser{Name: "Bob", Age: 25})
	db.Create(&TestUser{Name: "Charlie", Age: 30})

	q, u := NewQuery[TestUser](ctx)
	q.Lt(&u.Age, 30)
	user, err := repo.Last(q)
	assertError(t, err, false, "带条件 Last 应成功")
	assertEqual(t, "Bob", user.Name, "age<30 的最后一条应是 Bob")
}

// TestLastTx_WithTx 验证事务版本正常工作
func TestLastTx_WithTx(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 20})
	db.Create(&TestUser{Name: "Bob", Age: 25})

	q, _ := NewQuery[TestUser](ctx)
	user, err := repo.LastTx(q, nil)
	assertError(t, err, false, "LastTx nil tx 应成功")
	assertEqual(t, "Bob", user.Name, "LastTx 应返回最后一条")
}
