package gplus

import (
	"context"
	"testing"
)

// TestListMap_NilQuery 验证 nil query 返回 ErrQueryNil
func TestListMap_NilQuery(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	_, err := repo.ListMap(nil, func(u TestUser) int64 { return u.ID })
	assertError(t, err, true, "nil query 应返回 ErrQueryNil")
}

// TestListMap_NilKeyFn 验证 nil keyFn 返回错误
func TestListMap_NilKeyFn(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()
	q, _ := NewQuery[TestUser](ctx)
	_, err := repo.ListMap(q, nil)
	assertError(t, err, true, "nil keyFn 应返回错误")
}

// TestListMap_BuilderError 验证构建器错误透传
func TestListMap_BuilderError(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()
	q, _ := NewQuery[TestUser](ctx)
	q.Eq(nil, "bad")
	_, err := repo.ListMap(q, func(u TestUser) int64 { return u.ID })
	assertError(t, err, true, "构建器错误应透传")
}

// TestListMap_Empty 验证空表返回空 map（非 nil）
func TestListMap_Empty(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()
	q, _ := NewQuery[TestUser](ctx)
	m, err := repo.ListMap(q, func(u TestUser) int64 { return u.ID })
	assertError(t, err, false, "空表不应报错")
	if m == nil {
		t.Error("空结果应返回空 map，而非 nil")
	}
	assertEqual(t, 0, len(m), "空表 map 长度应为 0")
}

// TestListMap_Normal 验证正常查询转 map，key 为主键
func TestListMap_Normal(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 18})
	db.Create(&TestUser{Name: "Bob", Age: 20})
	db.Create(&TestUser{Name: "Charlie", Age: 22})

	q, _ := NewQuery[TestUser](ctx)
	m, err := repo.ListMap(q, func(u TestUser) int64 { return u.ID })
	assertError(t, err, false, "正常查询不应报错")
	assertEqual(t, 3, len(m), "应有 3 条记录")
	for _, u := range m {
		if u.Name == "" {
			t.Error("map 中的实体应有有效数据")
		}
	}
}

// TestListMap_WithCondition 验证带条件查询的 map 结果
func TestListMap_WithCondition(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 18})
	db.Create(&TestUser{Name: "Bob", Age: 20})
	db.Create(&TestUser{Name: "Charlie", Age: 22})

	q, u := NewQuery[TestUser](ctx)
	q.Gt(&u.Age, 19)
	m, err := repo.ListMap(q, func(u TestUser) int64 { return u.ID })
	assertError(t, err, false, "条件查询不应报错")
	assertEqual(t, 2, len(m), "age>19 应有 2 条记录")
}

// TestListMap_DuplicateKey 验证重复 key 时后者覆盖前者
func TestListMap_DuplicateKey(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 18})
	db.Create(&TestUser{Name: "Bob", Age: 18})

	q, _ := NewQuery[TestUser](ctx)
	m, err := repo.ListMap(q, func(u TestUser) int64 { return int64(u.Age) })
	assertError(t, err, false, "重复 key 不应报错")
	assertEqual(t, 1, len(m), "重复 key 后者覆盖前者，应只有 1 条")
}

// TestListMapTx_WithTx 验证在事务中执行 ListMapTx
func TestListMapTx_WithTx(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 18})
	db.Create(&TestUser{Name: "Bob", Age: 20})

	q, _ := NewQuery[TestUser](ctx)
	tx := db.WithContext(ctx).Begin()
	defer tx.Rollback()
	m, err := repo.ListMapTx(q, func(u TestUser) int64 { return u.ID }, tx)
	assertError(t, err, false, "事务查询不应报错")
	assertEqual(t, 2, len(m), "应有 2 条记录")
}
