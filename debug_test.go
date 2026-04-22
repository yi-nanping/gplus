package gplus

import (
	"context"
	"strings"
	"testing"
)

// ---- Query.ToSQL ----

func TestQuery_ToSQL_Select(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	q, m := repo.NewQuery(ctx)
	q.Eq(&m.Name, "Alice").Gt(&m.Age, 18).Order(&m.ID, false).Limit(10)

	sql, err := repo.ToSQL(q)
	assertError(t, err, false, "ToSQL 不应报错")

	up := strings.ToUpper(sql)
	if !strings.Contains(up, "SELECT") {
		t.Errorf("ToSQL 应包含 SELECT，实际: %s", sql)
	}
	if !strings.Contains(sql, "Alice") {
		t.Errorf("ToSQL 应包含内联参数值 Alice，实际: %s", sql)
	}
	if !strings.Contains(sql, "18") {
		t.Errorf("ToSQL 应包含内联参数值 18，实际: %s", sql)
	}
	if !strings.Contains(up, "ORDER BY") {
		t.Errorf("ToSQL 应包含 ORDER BY，实际: %s", sql)
	}
	if !strings.Contains(sql, "10") {
		t.Errorf("ToSQL 应包含 LIMIT 10，实际: %s", sql)
	}
}

func TestQuery_ToSQL_Distinct(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	q, m := NewQuery[TestUser](ctx)
	q.Distinct(&m.Age).Gt(&m.Age, 0)

	sql, err := q.ToSQL(db)
	assertError(t, err, false, "Distinct ToSQL 不应报错")
	if !strings.Contains(strings.ToUpper(sql), "DISTINCT") {
		t.Errorf("ToSQL 应包含 DISTINCT，实际: %s", sql)
	}
}

func TestQuery_ToSQL_Join(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	q, m := NewQuery[TestUser](ctx)
	q.Eq(&m.Name, "Alice").LeftJoin("orders", "orders.user_id = test_users.id")

	sql, err := q.ToSQL(db)
	assertError(t, err, false, "Join ToSQL 不应报错")
	up := strings.ToUpper(sql)
	if !strings.Contains(up, "LEFT JOIN") {
		t.Errorf("ToSQL 应包含 LEFT JOIN，实际: %s", sql)
	}
}

func TestQuery_ToSQL_BuilderError(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	q, _ := NewQuery[TestUser](ctx)
	q.Eq(nil, "bad") // 触发列名解析错误

	_, err := q.ToSQL(db)
	assertError(t, err, true, "builder 错误应透传到 ToSQL")
}

func TestQuery_ToSQL_NilQuery(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	_, err := repo.ToSQL(nil)
	assertError(t, err, true, "nil query 应返回错误")
}

// ---- Query.ToCountSQL ----

func TestQuery_ToCountSQL_Basic(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	q, m := repo.NewQuery(ctx)
	q.Ge(&m.Age, 18).Order(&m.ID, true) // Order 不应出现在 COUNT SQL

	sql, err := repo.ToCountSQL(q)
	assertError(t, err, false, "ToCountSQL 不应报错")

	up := strings.ToUpper(sql)
	if !strings.Contains(up, "COUNT") {
		t.Errorf("ToCountSQL 应包含 COUNT，实际: %s", sql)
	}
	if strings.Contains(up, "ORDER BY") {
		t.Errorf("ToCountSQL 不应包含 ORDER BY，实际: %s", sql)
	}
	if !strings.Contains(sql, "18") {
		t.Errorf("ToCountSQL 应包含 WHERE 条件参数 18，实际: %s", sql)
	}
}

func TestQuery_ToCountSQL_Distinct(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	ctx := context.Background()

	q, m := NewQuery[TestUser](ctx)
	q.Distinct(&m.Age)

	sql, err := q.ToCountSQL(db)
	assertError(t, err, false, "Distinct ToCountSQL 不应报错")
	up := strings.ToUpper(sql)
	if !strings.Contains(up, "DISTINCT") {
		t.Errorf("Distinct ToCountSQL 应包含 DISTINCT，实际: %s", sql)
	}
}

// ---- Updater.ToSQL ----

func TestUpdater_ToSQL_Basic(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	ctx := context.Background()

	u, um := repo.NewUpdater(ctx)
	u.Set(&um.Age, 30).Set(&um.Name, "Bob").Eq(&um.ID, int64(1))

	sql, err := repo.ToUpdateSQL(u)
	assertError(t, err, false, "ToUpdateSQL 不应报错")

	up := strings.ToUpper(sql)
	if !strings.Contains(up, "UPDATE") {
		t.Errorf("ToUpdateSQL 应包含 UPDATE，实际: %s", sql)
	}
	if !strings.Contains(sql, "30") {
		t.Errorf("ToUpdateSQL 应包含 Age 值 30，实际: %s", sql)
	}
	if !strings.Contains(up, "WHERE") {
		t.Errorf("ToUpdateSQL 应包含 WHERE 条件，实际: %s", sql)
	}
}

func TestUpdater_ToSQL_EmptyUpdater(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	u, _ := NewUpdater[TestUser](ctx)

	_, err := u.ToSQL(db)
	assertError(t, err, true, "空 setMap 应返回 ErrUpdateEmpty")
}

func TestUpdater_ToSQL_NoCondition(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	u, um := NewUpdater[TestUser](ctx)
	u.Set(&um.Age, 30) // 有 set 但无 WHERE 条件

	_, err := u.ToSQL(db)
	assertError(t, err, true, "无条件应返回 ErrUpdateNoCondition")
}

func TestUpdater_ToSQL_BuilderError(t *testing.T) {
	_, db := setupTestDB[TestUser](t)
	ctx := context.Background()
	u, um := NewUpdater[TestUser](ctx)
	u.Set(&um.Age, 30).Eq(nil, "bad") // 触发列名解析错误

	_, err := u.ToSQL(db)
	assertError(t, err, true, "builder 错误应透传到 Updater.ToSQL")
}

func TestUpdater_ToSQL_NilUpdater(t *testing.T) {
	repo, _ := setupTestDB[TestUser](t)
	_, err := repo.ToUpdateSQL(nil)
	assertError(t, err, true, "nil updater 应返回错误")
}

// ---- DataRule 集成 ----

func TestQuery_ToSQL_DataRule(t *testing.T) {
	_, db := setupTestDB[TestUser](t)

	rules := []DataRule{{Column: "age", Condition: ">=", Value: "18"}}
	ctx := context.WithValue(context.Background(), DataRuleKey, rules)

	q, m := NewQuery[TestUser](ctx)
	q.Eq(&m.Name, "Alice")

	sql, err := q.ToSQL(db)
	assertError(t, err, false, "DataRule ToSQL 不应报错")
	if !strings.Contains(sql, "18") {
		t.Errorf("ToSQL 应包含 DataRule 注入的条件值 18，实际: %s", sql)
	}
	if !strings.Contains(sql, "Alice") {
		t.Errorf("ToSQL 应包含原有条件值 Alice，实际: %s", sql)
	}
}
