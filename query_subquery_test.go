package gplus

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"strings"
	"testing"
)

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

func (e *errSubquerier) gplusSubquery() {}

// TestQuery_InSub_SubError 验证 sub.GetError() 经 GORM 链传播。
func TestQuery_InSub_SubError(t *testing.T) {
	repo := NewRepository[int64, UserWithDelete](newDryRunDB(t))
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

// TestQuery_InSub_OuterErrPriority 验证外层 q.GetError() 已有错误时 Repository 提前 return。
func TestQuery_InSub_OuterErrPriority(t *testing.T) {
	repo := NewRepository[int64, UserWithDelete](newDryRunDB(t))
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

// TestQuery_SubDataRule_ReverseRegression 反向回归：构造带 DataRule 的 ctx，调 sub.ToDB(db)，
// 断言 ToDB 未将 dataRuleApplied 置为 true，即 ToDB 本身不触发 DataRuleBuilder。
// 防止未来 contributor 给 ToDB 加隐式 DataRuleBuilder 调用而破坏既有安全契约。
func TestQuery_SubDataRule_ReverseRegression(t *testing.T) {
	db := newDryRunDB(t)

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
