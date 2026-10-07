package gplus_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	. "github.com/yi-nanping/gplus"
	"gorm.io/gorm"
)

type projectionDeptKey struct{}

func projectionRequestContext() context.Context {
	ctx := context.WithValue(context.Background(), projectionDeptKey{}, uint(1))
	return context.WithValue(ctx, DataRuleKey, []DataRule{
		{Table: "page_users", Column: "age", Condition: ">=", Value: "20"},
	})
}

func setupProjectionContextDB(t *testing.T) (*gorm.DB, *Repository[uint, pageUser]) {
	t.Helper()
	db, _ := setupPageDB(t)
	applyDBPoolLimits(t, db)
	// dave 满足 callback 的机构条件，但不满足动态 DataRule，用于区分两种机制。
	if err := db.Create(&pageUser{Name: "dave", Age: 10, DeptID: 1}).Error; err != nil {
		t.Fatal(err)
	}
	dbCtx := context.WithValue(context.Background(), projectionDeptKey{}, uint(2))
	return db, NewRepository[uint, pageUser](db.WithContext(dbCtx))
}

// callback 同时验证请求上下文、主模型和真实隔离结果，不能只检查触发次数。
func observeProjectionContext(t *testing.T, db *gorm.DB, want context.Context) *int {
	t.Helper()
	var calls int
	if err := db.Callback().Query().Before("gorm:query").Register("test:projection_context", func(d *gorm.DB) {
		calls++
		if d.Statement.Context != want {
			t.Errorf("callback Context 未保留预期上下文")
		}
		if _, ok := d.Statement.Model.(*pageUser); !ok {
			t.Errorf("callback Model=%T，期望 *pageUser", d.Statement.Model)
		}
		if d.Statement.Schema == nil || d.Statement.Schema.Table != "page_users" {
			t.Error("callback Schema 未使用主模型 pageUser")
		}
		dept, ok := d.Statement.Context.Value(projectionDeptKey{}).(uint)
		if !ok {
			t.Error("callback 未收到机构上下文")
			return
		}
		d.Where("page_users.dept_id = ?", dept)
	}); err != nil {
		t.Fatal(err)
	}
	return &calls
}

func newProjectionContextQuery(ctx context.Context) *Query[pageUser] {
	q, m := NewQuery[pageUser](ctx)
	dept := As[pageDept](q, "dept")
	return q.LeftJoinAs(dept, &m.DeptID, &dept.ID, "").
		Select(&m.Name, "dept.name AS dept_name").Order(&m.ID, true)
}

func TestProjection_ContextModelAndRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		page bool
		tx   bool
	}{
		{name: "FindAs"},
		{name: "FindAsTx", tx: true},
		{name: "PageAs", page: true},
		{name: "PageAsTx", page: true, tx: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, repo := setupProjectionContextDB(t)
			ctx := projectionRequestContext()
			calls := observeProjectionContext(t, db, ctx)
			q := newProjectionContextQuery(ctx)
			var rows []pageVO
			var total int64
			run := func(tx *gorm.DB) (err error) {
				if tc.page {
					q.Page(1, 1)
					if tc.tx {
						total, err = PageAsTx(repo, q, &rows, false, tx)
					} else {
						total, err = PageAs(repo, q, &rows, false)
					}
					return err
				}
				if tc.tx {
					return FindAsTx(repo, q, &rows, tx)
				}
				return FindAs(repo, q, &rows)
			}
			var err error
			if tc.tx {
				// 事务原有 Context 与 Query 不同，执行入口应使用 Query Context。
				err = repo.GetDB().Transaction(run)
			} else {
				err = run(nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			want := []pageVO{{Name: "alice", DeptName: "Eng"}, {Name: "bob", DeptName: "Eng"}}
			wantCalls := 1
			if tc.page {
				want = want[:1]
				wantCalls = 2
				if total != 2 {
					t.Errorf("total=%d，期望 callback 与 DataRule 共同筛选后的 2", total)
				}
			}
			if !reflect.DeepEqual(rows, want) {
				t.Errorf("rows=%+v，期望 %+v", rows, want)
			}
			if *calls != wantCalls {
				t.Errorf("callback 次数=%d，期望 %d", *calls, wantCalls)
			}
		})
	}
}

func TestProjection_CanceledContext(t *testing.T) {
	for _, name := range []string{"FindAs", "PageAs_Count", "PageAs_List", "ToDB_WithContext"} {
		t.Run(name, func(t *testing.T) {
			db, repo := setupProjectionContextDB(t)
			ctx, cancel := context.WithCancel(projectionRequestContext())
			cancel()
			observeProjectionContext(t, db, ctx)
			q := newProjectionContextQuery(ctx)
			var rows []pageVO
			var err error
			switch name {
			case "FindAs":
				err = FindAs(repo, q, &rows)
			case "PageAs_Count", "PageAs_List":
				_, err = PageAs(repo, q.Page(1, 1), &rows, name == "PageAs_List")
			case "ToDB_WithContext":
				err = q.ToDB(repo.GetDB()).WithContext(q.Context()).Find(&rows).Error
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err=%v，期望 context.Canceled", err)
			}
			if len(rows) != 0 {
				t.Fatalf("取消后仍返回 rows=%+v", rows)
			}
		})
	}
}

func TestPageAs_CancelAfterCount(t *testing.T) {
	db, repo := setupProjectionContextDB(t)
	ctx, cancel := context.WithCancel(projectionRequestContext())
	defer cancel()
	calls := observeProjectionContext(t, db, ctx)
	// 在 COUNT 完成后取消，确定性验证列表继续使用同一个请求 Context。
	if err := db.Callback().Query().After("gorm:query").Register("test:cancel_after_count", func(*gorm.DB) {
		if *calls == 1 {
			cancel()
		}
	}); err != nil {
		t.Fatal(err)
	}
	q := newProjectionContextQuery(ctx).Page(1, 1)
	var rows []pageVO
	total, err := PageAs(repo, q, &rows, false)
	if !errors.Is(err, context.Canceled) || total != 2 || len(rows) != 0 {
		t.Fatalf("total=%d rows=%+v err=%v，期望 COUNT=2、列表取消且无结果", total, rows, err)
	}
	if *calls != 2 {
		t.Fatalf("callback 次数=%d，期望 COUNT 和列表共 2 次", *calls)
	}
}

func TestToDB_ContextAndRuleContract(t *testing.T) {
	for _, tc := range []struct {
		name        string
		bindQuery   bool
		applyRules  bool
		cancelQuery bool
		cancelDB    bool
		wantCancel  bool
		want        []pageVO
	}{
		{name: "InheritsDBContext", want: []pageVO{{Name: "carol", DeptName: "Sales"}}},
		{name: "ExplicitContextWithoutDataRule", bindQuery: true, want: []pageVO{
			{Name: "alice", DeptName: "Eng"}, {Name: "bob", DeptName: "Eng"}, {Name: "dave", DeptName: "Eng"},
		}},
		{name: "ExplicitDataRule", bindQuery: true, applyRules: true, want: []pageVO{
			{Name: "alice", DeptName: "Eng"}, {Name: "bob", DeptName: "Eng"},
		}},
		{name: "CanceledQueryDoesNotOverrideDB", cancelQuery: true, want: []pageVO{{Name: "carol", DeptName: "Sales"}}},
		{name: "CanceledDBContext", cancelDB: true, wantCancel: true},
		{name: "ExplicitContextOverridesCanceledDB", bindQuery: true, cancelDB: true, want: []pageVO{
			{Name: "alice", DeptName: "Eng"}, {Name: "bob", DeptName: "Eng"}, {Name: "dave", DeptName: "Eng"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, repo := setupProjectionContextDB(t)
			queryCtx := projectionRequestContext()
			if tc.cancelQuery {
				ctx, cancel := context.WithCancel(queryCtx)
				cancel()
				queryCtx = ctx
			}
			base := repo.GetDB()
			if tc.cancelDB {
				ctx, cancel := context.WithCancel(base.Statement.Context)
				cancel()
				base = base.WithContext(ctx)
			}
			wantCtx := base.Statement.Context
			if tc.bindQuery {
				wantCtx = queryCtx
			}
			calls := observeProjectionContext(t, db, wantCtx)
			q := newProjectionContextQuery(queryCtx)
			if tc.applyRules {
				q.DataRuleBuilder()
			}
			statement := q.ToDB(base)
			if *calls != 0 {
				t.Fatal("ToDB 构建时不应执行查询 callback")
			}
			if tc.bindQuery {
				statement = statement.WithContext(q.Context())
			}
			var rows []pageVO
			err := statement.Find(&rows).Error
			if tc.wantCancel {
				if !errors.Is(err, context.Canceled) || len(rows) != 0 {
					t.Fatalf("rows=%+v err=%v，期望取消且无结果", rows, err)
				}
			} else if err != nil || !reflect.DeepEqual(rows, tc.want) {
				t.Fatalf("rows=%+v err=%v，期望 %+v", rows, err, tc.want)
			}
			if *calls != 1 {
				t.Fatalf("Find callback 次数=%d，期望 1", *calls)
			}
		})
	}
}
