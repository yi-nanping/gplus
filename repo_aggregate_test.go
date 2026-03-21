package gplus

import (
	"context"
	"testing"
)

// ---- 共用测试数据初始化 ----

func setupAggregateDB(t *testing.T) *Repository[int64, TestUser] {
	repo, db := setupTestDB[TestUser](t)
	db.Create(&TestUser{Name: "Alice", Age: 20})
	db.Create(&TestUser{Name: "Bob", Age: 30})
	db.Create(&TestUser{Name: "Charlie", Age: 10})
	return repo
}

// ---- Sum ----

func TestSum_NilQuery(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	_, err := Sum[TestUser, int64, int64](repo, nil, "age")
	assertError(t, err, true, "nil query 应返回 ErrQueryNil")
}

func TestSum_QueryBuilderError(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()
	q, _ := NewQuery[TestUser](ctx)
	q.Eq(nil, "x") // 累积错误
	_, err := Sum[TestUser, int64, int64](repo, q, "age")
	assertError(t, err, true, "构建器错误应透传")
}

func TestSum_Normal(t *testing.T) {
	repo := setupAggregateDB(t)
	ctx := context.Background()
	q, _ := NewQuery[TestUser](ctx)
	total, err := Sum[TestUser, int64, int64](repo, q, "age")
	assertError(t, err, false, "Sum 不应报错")
	assertEqual(t, int64(60), total, "20+30+10=60")
}

func TestSum_WithCondition(t *testing.T) {
	repo := setupAggregateDB(t)
	ctx := context.Background()
	q, u := NewQuery[TestUser](ctx)
	q.Gt(&u.Age, 15)
	total, err := Sum[TestUser, int64, int64](repo, q, "age")
	assertError(t, err, false, "条件 Sum 不应报错")
	assertEqual(t, int64(50), total, "20+30=50")
}

func TestSum_EmptyTable(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()
	q, _ := NewQuery[TestUser](ctx)
	total, err := Sum[TestUser, int64, int64](repo, q, "age")
	assertError(t, err, false, "空表 Sum 不应报错")
	assertEqual(t, int64(0), total, "空表 Sum 应为 0")
}

// ---- Max ----

func TestMax_Normal(t *testing.T) {
	repo := setupAggregateDB(t)
	ctx := context.Background()
	q, _ := NewQuery[TestUser](ctx)
	max, err := Max[TestUser, int64, int64](repo, q, "age")
	assertError(t, err, false, "Max 不应报错")
	assertEqual(t, int64(30), max, "最大值应为 30")
}

func TestMax_WithCondition(t *testing.T) {
	repo := setupAggregateDB(t)
	ctx := context.Background()
	q, u := NewQuery[TestUser](ctx)
	q.Lt(&u.Age, 30)
	max, err := Max[TestUser, int64, int64](repo, q, "age")
	assertError(t, err, false, "条件 Max 不应报错")
	assertEqual(t, int64(20), max, "age<30 最大值为 20")
}

// ---- Min ----

func TestMin_Normal(t *testing.T) {
	repo := setupAggregateDB(t)
	ctx := context.Background()
	q, _ := NewQuery[TestUser](ctx)
	min, err := Min[TestUser, int64, int64](repo, q, "age")
	assertError(t, err, false, "Min 不应报错")
	assertEqual(t, int64(10), min, "最小值应为 10")
}

func TestMin_WithCondition(t *testing.T) {
	repo := setupAggregateDB(t)
	ctx := context.Background()
	q, u := NewQuery[TestUser](ctx)
	q.Gt(&u.Age, 15)
	min, err := Min[TestUser, int64, int64](repo, q, "age")
	assertError(t, err, false, "条件 Min 不应报错")
	assertEqual(t, int64(20), min, "age>15 最小值为 20")
}

// ---- Avg ----

func TestAvg_Normal(t *testing.T) {
	repo := setupAggregateDB(t)
	ctx := context.Background()
	q, _ := NewQuery[TestUser](ctx)
	avg, err := Avg[TestUser, float64, int64](repo, q, "age")
	assertError(t, err, false, "Avg 不应报错")
	assertEqual(t, float64(20), avg, "(20+30+10)/3=20")
}

func TestAvg_WithCondition(t *testing.T) {
	repo := setupAggregateDB(t)
	ctx := context.Background()
	q, u := NewQuery[TestUser](ctx)
	q.Gt(&u.Age, 15)
	avg, err := Avg[TestUser, float64, int64](repo, q, "age")
	assertError(t, err, false, "条件 Avg 不应报错")
	assertEqual(t, float64(25), avg, "(20+30)/2=25")
}

// ---- Tx 变体（以 SumTx 为代表） ----

func TestSumTx_WithTx(t *testing.T) {
	repo := setupAggregateDB(t)
	ctx := context.Background()

	q, _ := NewQuery[TestUser](ctx)
	total, err := SumTx[TestUser, int64, int64](repo, q, "age", nil)
	assertError(t, err, false, "SumTx nil tx 不应报错")
	assertEqual(t, int64(60), total, "SumTx 结果应与 Sum 相同")
}
