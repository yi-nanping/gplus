package gplus

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestExists_QueryLimitReadsOneRow(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	for i := 0; i < 7; i++ {
		if err := db.Create(&TestUser{Age: i}).Error; err != nil {
			t.Fatal(err)
		}
	}
	var queryRows int64
	var sql string
	var sqlLimit int
	if err := db.Callback().Query().After("gorm:query").Register("contract:exists", func(tx *gorm.DB) {
		queryRows += tx.RowsAffected
		sql = tx.Statement.SQL.String()
		limit := tx.Statement.Clauses["LIMIT"].Expression.(clause.Limit)
		sqlLimit = *limit.Limit
	}); err != nil {
		t.Fatal(err)
	}
	q, _ := NewQuery[TestUser](context.Background())
	q.Limit(5)
	exists, err := repo.Exists(q)
	if err != nil || !exists {
		t.Fatalf("Exists=%v err=%v", exists, err)
	}
	if queryRows != 1 || sqlLimit != 1 || !strings.Contains(strings.ToUpper(sql), "LIMIT") {
		t.Fatalf("读取行数=%d SQL LIMIT=%d SQL=%q，期望只读取一行", queryRows, sqlLimit, sql)
	}
	if q.limit != 5 {
		t.Fatal("Exists 修改了 Query Limit")
	}
}

func TestExists_QueryShapeAndDataRulePreserved(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	for _, age := range []int{10, 10, 20, 20, 30, 30} {
		if err := db.Create(&TestUser{Age: age}).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{{Column: "age", Condition: OpLe, Value: "20"}})
	var got []TestUser
	if err := db.Callback().Query().After("gorm:query").Register("contract:exists_shape", func(tx *gorm.DB) {
		if tx.Statement.Context != ctx {
			t.Error("Exists 丢失 Query Context")
		}
		if tx.Statement.Schema == nil || tx.Statement.Schema.ModelType != reflect.TypeOf(TestUser{}) {
			t.Error("Exists 丢失模型类型")
		}
		got = append(got, (*tx.Statement.Dest.(*[]TestUser))...)
	}); err != nil {
		t.Fatal(err)
	}
	q, m := NewQuery[TestUser](ctx)
	q.Select(&m.Age).Group(&m.Age).Having("COUNT(*)", OpGt, 1).Order(&m.Age, true).Offset(1).Limit(5)
	exists, err := repo.Exists(q)
	if err != nil || !exists || len(got) != 1 || got[0].Age != 20 {
		t.Fatalf("Exists=%v err=%v rows=%v，期望分组过滤后第二组 age=20", exists, err, got)
	}
	q.Offset(2)
	exists, err = repo.Exists(q)
	if err != nil || exists {
		t.Fatalf("DataRule 应过滤第三组 age=30，Exists=%v err=%v", exists, err)
	}
}

func TestExists_NilQuery(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ok, err := repo.Exists(nil)
	assertError(t, err, true, "nil query 应返回 ErrQueryNil")
	assertEqual(t, false, ok, "nil query 应返回 false")
}

func TestExists_NoMatch(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 20})

	q, u := NewQuery[TestUser](ctx)
	q.Eq(&u.Name, "NotExist")

	ok, err := repo.Exists(q)
	assertError(t, err, false, "无匹配时不应有错误")
	assertEqual(t, false, ok, "无匹配时应返回 false")
}

func TestExists_Match(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	db.Create(&TestUser{Name: "Alice", Age: 20})

	q, u := NewQuery[TestUser](ctx)
	q.Eq(&u.Name, "Alice")

	ok, err := repo.Exists(q)
	assertError(t, err, false, "有匹配时不应有错误")
	assertEqual(t, true, ok, "有匹配时应返回 true")
}

func TestExists_QueryBuilderError(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	q, _ := NewQuery[TestUser](ctx)
	q.Eq(nil, "value") // 非法字段指针，累积错误

	ok, err := repo.Exists(q)
	assertError(t, err, true, "构建器错误应透传")
	assertEqual(t, false, ok, "构建器错误时应返回 false")
}

func TestExists_WithDataRule(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	db.Create(&TestUser{Name: "Alice", Age: 20})
	db.Create(&TestUser{Name: "Bob", Age: 30})

	// DataRule 过滤 age=20，Alice 存在
	ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Column: "age", Condition: OpEq, Value: "20"},
	})
	q, u := NewQuery[TestUser](ctx)
	q.Eq(&u.Name, "Alice")
	ok, err := repo.Exists(q)
	assertError(t, err, false, "DataRule 过滤后有匹配不应报错")
	assertEqual(t, true, ok, "DataRule 过滤后 Alice 应存在")

	// DataRule 过滤 age=20，Bob 不存在
	q2, u2 := NewQuery[TestUser](ctx)
	q2.Eq(&u2.Name, "Bob")
	ok2, err2 := repo.Exists(q2)
	assertError(t, err2, false, "DataRule 过滤后无匹配不应报错")
	assertEqual(t, false, ok2, "DataRule 过滤后 Bob 不应存在")
}

func TestExistsTx_Commit(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	_ = db.Transaction(func(tx *gorm.DB) error {
		tx.Create(&TestUser{Name: "Charlie", Age: 25})
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "Charlie")
		ok, err := repo.ExistsTx(q, tx)
		assertError(t, err, false, "事务内应无错误")
		assertEqual(t, true, ok, "事务内 Charlie 应存在")
		return nil
	})

	// 事务提交后，外部也能查到
	q, u := NewQuery[TestUser](ctx)
	q.Eq(&u.Name, "Charlie")
	ok, err := repo.Exists(q)
	assertError(t, err, false, "提交后应无错误")
	assertEqual(t, true, ok, "提交后 Charlie 应存在")
}

func TestExistsTx_Rollback(t *testing.T) {
	repo, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	_ = db.Transaction(func(tx *gorm.DB) error {
		tx.Create(&TestUser{Name: "Dave", Age: 40})
		return errors.New("rollback") // 触发回滚
	})

	q, u := NewQuery[TestUser](ctx)
	q.Eq(&u.Name, "Dave")
	ok, err := repo.Exists(q)
	assertError(t, err, false, "回滚后查询不应报错")
	assertEqual(t, false, ok, "回滚后 Dave 不应存在")
}
