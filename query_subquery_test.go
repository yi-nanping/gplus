package gplus

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// TestQuery_InSub_Basic 验证 InSub 生成 SQL 形态 + 真实数据命中。
func TestQuery_InSub_Basic(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	// 准备数据：UserA(id=1) Amount=100/200, UserB(id=2) Amount=300, UserC 无订单
	users := []UserWithDelete{{Name: "UserA", Age: 20}, {Name: "UserB", Age: 30}, {Name: "UserC", Age: 25}}
	db.Create(&users)
	db.Create(&Order{UserID: 1, Amount: 100})
	db.Create(&Order{UserID: 1, Amount: 200})
	db.Create(&Order{UserID: 2, Amount: 300})

	// 子查询：所有有订单的 user_id
	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	q, u := NewQuery[UserWithDelete](ctx)
	q.InSub(&u.ID, subQ).Order(&u.ID, true)

	result, err := repo.List(q)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(result) != 2 || result[0].Name != "UserA" || result[1].Name != "UserB" {
		t.Fatalf("expected [UserA, UserB], got %+v", result)
	}
}

// TestQuery_NotInSub_Basic 验证 NotInSub。
func TestQuery_NotInSub_Basic(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	users := []UserWithDelete{{Name: "UserA", Age: 20}, {Name: "UserB", Age: 30}, {Name: "UserC", Age: 25}}
	db.Create(&users)
	db.Create(&Order{UserID: 1, Amount: 100})
	db.Create(&Order{UserID: 2, Amount: 300})

	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	q, u := NewQuery[UserWithDelete](ctx)
	q.NotInSub(&u.ID, subQ)

	result, err := repo.List(q)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(result) != 1 || result[0].Name != "UserC" {
		t.Fatalf("expected [UserC], got %+v", result)
	}
}

// TestQuery_OrInSub 验证 OrInSub 与 AND 条件混用。
func TestQuery_OrInSub(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	users := []UserWithDelete{{Name: "UserA", Age: 20}, {Name: "UserB", Age: 30}, {Name: "UserC", Age: 99}}
	db.Create(&users)
	db.Create(&Order{UserID: 1, Amount: 100})

	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	q, u := NewQuery[UserWithDelete](ctx)
	// age=99 OR id IN (subQ) → UserC（age=99）+ UserA（id IN subQ）
	q.Eq(&u.Age, 99).OrInSub(&u.ID, subQ).Order(&u.ID, true)

	result, err := repo.List(q)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 users, got %d: %+v", len(result), result)
	}
}

// TestQuery_OrNotInSub_DryRun 验证 OrNotInSub 通过 SQL 形态 DryRun。
func TestQuery_OrNotInSub_DryRun(t *testing.T) {
	_, db := setupAdvancedDB(t)
	ctx := context.Background()

	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	q, u := NewQuery[UserWithDelete](ctx)
	q.Eq(&u.Age, 20).OrNotInSub(&u.ID, subQ)

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	if !strings.Contains(sql, "NOT IN") {
		t.Fatalf("expected SQL to contain NOT IN, got: %s", sql)
	}
}

// TestQuery_GtSub_Basic 验证 GtSub: WHERE age > (SELECT AVG(age) FROM users)。
func TestQuery_GtSub_Basic(t *testing.T) {
	repo, db := setupAdvancedDB(t)
	ctx := context.Background()

	users := []UserWithDelete{{Name: "Young", Age: 20}, {Name: "Avg", Age: 30}, {Name: "Old", Age: 40}}
	db.Create(&users)

	avgQ, _ := NewQuery[UserWithDelete](ctx)
	avgQ.SelectRaw("AVG(age)")

	q, u := NewQuery[UserWithDelete](ctx)
	q.GtSub(&u.Age, avgQ).Order(&u.ID, true)

	result, err := repo.List(q)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	// 平均 age=30，> 30 的只有 Old(40)
	if len(result) != 1 || result[0].Name != "Old" {
		t.Fatalf("expected [Old], got %+v", result)
	}
}

// TestQuery_ScalarSub_DryRun 表驱动覆盖 6 个标量子查询的 SQL 形态。
func TestQuery_ScalarSub_DryRun(t *testing.T) {
	_, db := setupAdvancedDB(t)
	ctx := context.Background()

	tests := []struct {
		name     string
		apply    func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier)
		wantOp   string
	}{
		{"EqSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.EqSub(&u.Age, sub) }, "="},
		{"NeSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.NeSub(&u.Age, sub) }, "<>"},
		{"GtSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.GtSub(&u.Age, sub) }, ">"},
		{"GteSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.GteSub(&u.Age, sub) }, ">="},
		{"LtSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.LtSub(&u.Age, sub) }, "<"},
		{"LteSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.LteSub(&u.Age, sub) }, "<="},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub, _ := NewQuery[UserWithDelete](ctx)
			sub.SelectRaw("AVG(age)")

			q, u := NewQuery[UserWithDelete](ctx)
			tt.apply(q, u, sub)

			sql, err := q.ToSQL(db)
			if err != nil {
				t.Fatalf("ToSQL failed: %v", err)
			}
			if !strings.Contains(sql, tt.wantOp+" (SELECT") {
				t.Fatalf("expected SQL to contain '%s (SELECT', got: %s", tt.wantOp, sql)
			}
		})
	}
}

// TestQuery_OrScalarSub_DryRun 验证 6 个 Or 标量变体 SQL 形态。
func TestQuery_OrScalarSub_DryRun(t *testing.T) {
	_, db := setupAdvancedDB(t)
	ctx := context.Background()

	tests := []struct {
		name  string
		apply func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier)
	}{
		{"OrEqSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.Eq(&u.Age, 0).OrEqSub(&u.Age, sub) }},
		{"OrNeSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.Eq(&u.Age, 0).OrNeSub(&u.Age, sub) }},
		{"OrGtSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.Eq(&u.Age, 0).OrGtSub(&u.Age, sub) }},
		{"OrGteSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.Eq(&u.Age, 0).OrGteSub(&u.Age, sub) }},
		{"OrLtSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.Eq(&u.Age, 0).OrLtSub(&u.Age, sub) }},
		{"OrLteSub", func(q *Query[UserWithDelete], u *UserWithDelete, sub Subquerier) { q.Eq(&u.Age, 0).OrLteSub(&u.Age, sub) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub, _ := NewQuery[UserWithDelete](ctx)
			sub.SelectRaw("AVG(age)")

			q, u := NewQuery[UserWithDelete](ctx)
			tt.apply(q, u, sub)

			sql, err := q.ToSQL(db)
			if err != nil {
				t.Fatalf("ToSQL failed: %v", err)
			}
			if !strings.Contains(strings.ToUpper(sql), "OR ") {
				t.Fatalf("expected SQL to contain OR, got: %s", sql)
			}
		})
	}
}

// errSubquerier 测试辅助：模拟一个返回预设错误的 Subquerier。
// 因 gplusSubquery() 是 unexported，外部包无法实现 Subquerier；
// 此辅助同包可用，正是 guard 设计目的（测试可模拟，外部不可冒名）。
type errSubquerier struct {
	err error
}

// ToDB 故意不在 Session 上调用 AddError —— Session{NewDB:true} 切断了
// session.AddError 到外层 d 的回流路径（这正是 builder.go 必须显式
// d.AddError(sub.GetError()) 的原因）。errSubquerier 的语义就是"ToDB 不
// 传播错误，只有 GetError 才返回错误"，构成 builder.go 错误聚合分支的
// 最小复现场景。
func (e *errSubquerier) ToDB(db *gorm.DB) *gorm.DB {
	return db.Session(&gorm.Session{NewDB: true})
}

func (e *errSubquerier) GetError() error { return e.err }
func (e *errSubquerier) gplusSubquery()  {}

// TestQuery_AllSub_NilSub 表驱动覆盖 16 个 Query 子查询方法的 nil sub 错误路径。
// 解决 Task 3 遗留：12 标量方法 + 4 集合方法的 nil 分支覆盖率不足问题。
func TestQuery_AllSub_NilSub(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name  string
		apply func(q *Query[UserWithDelete], u *UserWithDelete)
	}{
		{"InSub", func(q *Query[UserWithDelete], u *UserWithDelete) { q.InSub(&u.ID, nil) }},
		{"OrInSub", func(q *Query[UserWithDelete], u *UserWithDelete) { q.OrInSub(&u.ID, nil) }},
		{"NotInSub", func(q *Query[UserWithDelete], u *UserWithDelete) { q.NotInSub(&u.ID, nil) }},
		{"OrNotInSub", func(q *Query[UserWithDelete], u *UserWithDelete) { q.OrNotInSub(&u.ID, nil) }},
		{"EqSub", func(q *Query[UserWithDelete], u *UserWithDelete) { q.EqSub(&u.Age, nil) }},
		{"OrEqSub", func(q *Query[UserWithDelete], u *UserWithDelete) { q.OrEqSub(&u.Age, nil) }},
		{"NeSub", func(q *Query[UserWithDelete], u *UserWithDelete) { q.NeSub(&u.Age, nil) }},
		{"OrNeSub", func(q *Query[UserWithDelete], u *UserWithDelete) { q.OrNeSub(&u.Age, nil) }},
		{"GtSub", func(q *Query[UserWithDelete], u *UserWithDelete) { q.GtSub(&u.Age, nil) }},
		{"OrGtSub", func(q *Query[UserWithDelete], u *UserWithDelete) { q.OrGtSub(&u.Age, nil) }},
		{"GteSub", func(q *Query[UserWithDelete], u *UserWithDelete) { q.GteSub(&u.Age, nil) }},
		{"OrGteSub", func(q *Query[UserWithDelete], u *UserWithDelete) { q.OrGteSub(&u.Age, nil) }},
		{"LtSub", func(q *Query[UserWithDelete], u *UserWithDelete) { q.LtSub(&u.Age, nil) }},
		{"OrLtSub", func(q *Query[UserWithDelete], u *UserWithDelete) { q.OrLtSub(&u.Age, nil) }},
		{"LteSub", func(q *Query[UserWithDelete], u *UserWithDelete) { q.LteSub(&u.Age, nil) }},
		{"OrLteSub", func(q *Query[UserWithDelete], u *UserWithDelete) { q.OrLteSub(&u.Age, nil) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, u := NewQuery[UserWithDelete](ctx)
			tt.apply(q, u)
			if !errors.Is(q.GetError(), ErrSubqueryNil) {
				t.Fatalf("expected ErrSubqueryNil, got %v", q.GetError())
			}
		})
	}
}

// TestQuery_SelectRaw_EmptyString 验证 SelectRaw 空字符串错误路径。
func TestQuery_SelectRaw_EmptyString(t *testing.T) {
	ctx := context.Background()
	q, _ := NewQuery[UserWithDelete](ctx)
	q.SelectRaw("")
	if q.GetError() == nil {
		t.Fatalf("expected error for empty SelectRaw expr, got nil")
	}
}

// TestQuery_InSub_SubError 验证 sub.GetError() 经 GORM 链传播。
func TestQuery_InSub_SubError(t *testing.T) {
	repo, _ := setupAdvancedDB(t)
	ctx := context.Background()

	subErr := errors.New("test sub error")
	sub := &errSubquerier{err: subErr}

	q, u := NewQuery[UserWithDelete](ctx)
	q.InSub(&u.ID, sub)

	_, err := repo.List(q)
	if err == nil {
		t.Fatalf("expected error from sub propagation, got nil")
	}
	if !strings.Contains(err.Error(), "test sub error") {
		t.Fatalf("expected sub error in chain, got: %v", err)
	}
}

// TestQuery_InSub_ColInvalid 验证非法 col 指针走 addCond 列名解析错误路径。
func TestQuery_InSub_ColInvalid(t *testing.T) {
	ctx := context.Background()

	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)

	q, _ := NewQuery[UserWithDelete](ctx)
	notRegistered := &struct{ X int }{}
	q.InSub(&notRegistered.X, subQ)

	if q.GetError() == nil {
		t.Fatalf("expected col resolution error, got nil")
	}
}

// TestQuery_InSub_OuterErrPriority 验证外层 q.GetError() 已有错误时 Repository 提前 return。
func TestQuery_InSub_OuterErrPriority(t *testing.T) {
	repo, _ := setupAdvancedDB(t)
	ctx := context.Background()

	q, u := NewQuery[UserWithDelete](ctx)
	q.errs = append(q.errs, errors.New("outer pre-existing error"))
	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID)
	q.InSub(&u.ID, subQ)

	_, err := repo.List(q)
	if err == nil {
		t.Fatalf("expected error from outer errs, got nil")
	}
	if !strings.Contains(err.Error(), "outer pre-existing error") {
		t.Fatalf("expected outer error first, got: %v", err)
	}
}

// TestQuery_InSub_DeferredSemantics 锁定延迟调用语义。
// sub 在 InSub 后追加条件 → 最终 SQL 包含追加条件
// （防止未来"贴心"改为立即快照而不更新文档）。
func TestQuery_InSub_DeferredSemantics(t *testing.T) {
	_, db := setupAdvancedDB(t)
	ctx := context.Background()

	subQ, order := NewQuery[Order](ctx)
	subQ.Select(&order.UserID).Eq(&order.UserID, 1) // 初始条件

	q, u := NewQuery[UserWithDelete](ctx)
	q.InSub(&u.ID, subQ)

	// 传入后追加条件
	subQ.Eq(&order.Amount, 999)

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	// 延迟调用：追加的 amount=999 必须出现在最终 SQL 中
	if !strings.Contains(sql, "999") {
		t.Fatalf("expected SQL to contain 999 (deferred semantics), got: %s", sql)
	}
}

// ─── DataRule × 子查询交互 ──────────────────────────────────────────────────

// TestQuery_SubDataRule_Default_NotApplied 验证 sub.ToDB() 默认不应用 DataRule。
// 锁定 query.go ToDB 不调 DataRuleBuilder 的既有语义。
func TestQuery_SubDataRule_Default_NotApplied(t *testing.T) {
	_, db := setupAdvancedDB(t)

	ctxWithRule := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Column: "user_id", Condition: "=", Value: "999"},
	})

	subQ, order := NewQuery[Order](ctxWithRule)
	subQ.Select(&order.UserID)

	q, u := NewQuery[UserWithDelete](context.Background())
	q.InSub(&u.ID, subQ)

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	// 默认 sub 不应用 DataRule，SQL 中不应出现 999
	if strings.Contains(sql, "999") {
		t.Fatalf("default sub.ToDB should NOT apply DataRule, got SQL: %s", sql)
	}
}

// TestQuery_SubDataRule_Explicit_Applied 验证显式 sub.DataRuleBuilder().ToDB() 应用 DataRule。
func TestQuery_SubDataRule_Explicit_Applied(t *testing.T) {
	_, db := setupAdvancedDB(t)

	ctxWithRule := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Column: "user_id", Condition: "=", Value: "999"},
	})

	subQ, order := NewQuery[Order](ctxWithRule)
	subQ.Select(&order.UserID).DataRuleBuilder() // 显式应用

	q, u := NewQuery[UserWithDelete](context.Background())
	q.InSub(&u.ID, subQ)

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	if !strings.Contains(sql, "999") {
		t.Fatalf("explicit DataRuleBuilder should apply, got SQL: %s", sql)
	}
}

// TestQuery_SubDataRule_OuterOnly 外层有 DataRule、子查询无 DataRule。
func TestQuery_SubDataRule_OuterOnly(t *testing.T) {
	_, db := setupAdvancedDB(t)

	ctxOuter := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Column: "age", Condition: "=", Value: "18"},
	})

	subQ, order := NewQuery[Order](context.Background()) // 子查询无 DataRule
	subQ.Select(&order.UserID)

	q, u := NewQuery[UserWithDelete](ctxOuter)
	q.InSub(&u.ID, subQ).DataRuleBuilder() // 外层应用

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	if !strings.Contains(sql, "18") {
		t.Fatalf("outer DataRule should apply to outer SQL, got: %s", sql)
	}
}

// TestQuery_SubDataRule_ColumnNotInSubTable 子查询表无 DataRule 列时与现状一致行为。
// DataRule 列 "age" 在 Order 表上不存在（Order 只有 id/user_id/amount/remark）。
// 显式应用 DataRuleBuilder 后，SQLite 宽容模式下不报错但子查询返回空集，
// 导致外层结果也为空（0 行）。测试锁定这一"无错误、空结果"的现状行为。
func TestQuery_SubDataRule_ColumnNotInSubTable(t *testing.T) {
	repo, db := setupAdvancedDB(t)

	// 写入用户和订单
	db.Create(&UserWithDelete{Name: "Alice", Age: 20})
	db.Create(&Order{UserID: 1, Amount: 100})

	ctxWithRule := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Column: "age", Condition: "=", Value: "18"},
	})

	subQ, order := NewQuery[Order](ctxWithRule)
	subQ.Select(&order.UserID).DataRuleBuilder() // 显式应用 → SQL 引用 Order 表上不存在的列

	q, u := NewQuery[UserWithDelete](context.Background())
	q.InSub(&u.ID, subQ)

	results, err := repo.List(q)
	// SQLite 宽容模式下不报 "no such column"，子查询返回空集，外层也返回 0 行
	// 锁定现状：无错误 + 结果为空
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results (sub DataRule on non-existent column returns empty set), got %d", len(results))
	}
}

// TestQuery_SubDataRule_ReuseIdempotent 同一 sub 多次调 DataRuleBuilder 应幂等。
func TestQuery_SubDataRule_ReuseIdempotent(t *testing.T) {
	_, db := setupAdvancedDB(t)

	ctxWithRule := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Column: "user_id", Condition: "=", Value: "1"},
	})

	subQ, order := NewQuery[Order](ctxWithRule)
	subQ.Select(&order.UserID).DataRuleBuilder().DataRuleBuilder() // 调两次

	q, u := NewQuery[UserWithDelete](context.Background())
	q.InSub(&u.ID, subQ)

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	// 幂等：user_id=1 只出现一次（不是 user_id=1 AND user_id=1）
	count := strings.Count(sql, `"user_id" = 1`)
	if count == 0 {
		count = strings.Count(sql, "`user_id` = 1") // MySQL 转义
	}
	if count > 1 {
		t.Fatalf("DataRuleBuilder should be idempotent, got %d occurrences in SQL: %s", count, sql)
	}
}

// TestQuery_SubDataRule_ReverseRegression 反向回归：构造带 DataRule 的 ctx，调 sub.ToDB(db)，
// 断言 ToDB 未将 dataRuleApplied 置为 true，即 ToDB 本身不触发 DataRuleBuilder。
// 防止未来 contributor 给 ToDB 加隐式 DataRuleBuilder 调用而破坏既有安全契约。
func TestQuery_SubDataRule_ReverseRegression(t *testing.T) {
	_, db := setupAdvancedDB(t)

	ctxWithRule := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Column: "user_id", Condition: "=", Value: "999"},
	})

	subQ, order := NewQuery[Order](ctxWithRule)
	subQ.Select(&order.UserID)

	// 调 ToDB — 如果它内部调了 DataRuleBuilder，dataRuleApplied 会变为 true
	subQ.ToDB(db)

	// 反向锁定：ToDB 不应将 dataRuleApplied 置为 true
	if subQ.dataRuleApplied {
		t.Fatalf("ToDB must NOT call DataRuleBuilder internally; dataRuleApplied should remain false after ToDB")
	}

	// 进一步验证：通过外层 SQL 观察子查询中不含 DataRule 条件（与 Default_NotApplied 互补）
	q, u := NewQuery[UserWithDelete](context.Background())
	q.InSub(&u.ID, subQ)
	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("outer ToSQL failed: %v", err)
	}
	if strings.Contains(sql, "999") {
		t.Fatalf("ToDB without explicit DataRuleBuilder must NOT apply DataRule, got outer SQL: %s", sql)
	}
}
