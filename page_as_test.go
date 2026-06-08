package gplus

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// PageAs 投影分页 —— 验收标准（AC），每条 1:1 对应一个子测试
//
// 种子：dept 1=Eng / 2=Sales；user alice(dept1) bob(dept1) carol(dept2)，按 id 升序
//
// AC-1: LEFT JOIN 投影 + Page(1,2) + skipCount=false
//       → total=3，len(rows)=2，rows[0]={alice,Eng}，rows[1]={bob,Eng}
// AC-2: 同 AC-1 但 skipCount=true
//       → total=0（跳过 COUNT），len(rows)=2（仍取第一页投影）
// AC-3: q.Eq(name,"nobody") + skipCount=false（COUNT=0）
//       → total=0，len(rows)=0，且不执行投影 Find（Query callback 仅 1 次=COUNT）
// AC-4: ctx 注入 DataRule dept_id=1 + skipCount=false
//       → total=2，投影数据只含授权行 {alice,bob}
// AC-5: q==nil → 返回 (0, ErrQueryNil)
// AC-6: q 累积错误（非法字段指针）→ 透传该 error，total=0
// AC-7: 有数据 + skipCount=false → 走 Query callback chain（query=2: COUNT+Find，row=0），
//       不绕到 Row chain（Scan 会使 row≥1）

type pageUser struct {
	ID     uint `gorm:"primarykey"`
	Name   string
	Age    int
	DeptID uint
}

type pageDept struct {
	ID   uint `gorm:"primarykey"`
	Name string
}

type pageVO struct {
	Name     string
	DeptName string
}

// setupPageDB 建库 + 种子，返回 db 与 repo。
func setupPageDB(t *testing.T) (*gorm.DB, *Repository[uint, pageUser]) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&pageUser{}, &pageDept{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&pageDept{ID: 1, Name: "Eng"})
	db.Create(&pageDept{ID: 2, Name: "Sales"})
	db.Create(&pageUser{Name: "alice", Age: 20, DeptID: 1})
	db.Create(&pageUser{Name: "bob", Age: 30, DeptID: 1})
	db.Create(&pageUser{Name: "carol", Age: 25, DeptID: 2})
	return db, NewRepository[uint, pageUser](db)
}

// AC-1: JOIN 投影 + 第一页 + 带 COUNT，返回 total=3 且当前页 2 行投影正确
func TestPageAs_returns_total3_and_first_page_2_rows_with_count(t *testing.T) {
	_, repo := setupPageDB(t)
	q, _ := NewQuery[pageUser](context.Background())
	q.LeftJoin("page_depts", "page_users.dept_id = page_depts.id").
		Select("page_users.name AS name", "page_depts.name AS dept_name").
		OrderRaw("page_users.id ASC").
		Page(1, 2)
	var rows []pageVO
	total, err := PageAs(repo, q, &rows, false)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("total=%d, 期望 3", total)
	}
	if len(rows) != 2 {
		t.Fatalf("len=%d, 期望 2", len(rows))
	}
	if rows[0].Name != "alice" || rows[0].DeptName != "Eng" {
		t.Fatalf("rows[0]=%+v, 期望 {alice,Eng}", rows[0])
	}
	if rows[1].Name != "bob" || rows[1].DeptName != "Eng" {
		t.Fatalf("rows[1]=%+v, 期望 {bob,Eng}", rows[1])
	}
}

// AC-2: skipCount=true 跳过 COUNT，total=0 但仍返回第一页投影
func TestPageAs_skipCount_true_returns_total0_but_still_projects_page(t *testing.T) {
	_, repo := setupPageDB(t)
	q, _ := NewQuery[pageUser](context.Background())
	q.LeftJoin("page_depts", "page_users.dept_id = page_depts.id").
		Select("page_users.name AS name", "page_depts.name AS dept_name").
		OrderRaw("page_users.id ASC").
		Page(1, 2)
	var rows []pageVO
	total, err := PageAs(repo, q, &rows, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatalf("total=%d, 期望 0（skipCount 跳过 COUNT）", total)
	}
	if len(rows) != 2 {
		t.Fatalf("len=%d, 期望 2", len(rows))
	}
}

// pageProbe 注册 Query/Row callback 计数器，返回计数指针与清理函数。
func pageProbe(t *testing.T, db *gorm.DB) (query, row *int, cleanup func()) {
	t.Helper()
	var qc, rc int
	if err := db.Callback().Query().Before("gorm:query").
		Register("test:page_q", func(*gorm.DB) { qc++ }); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().Before("gorm:row").
		Register("test:page_r", func(*gorm.DB) { rc++ }); err != nil {
		t.Fatal(err)
	}
	return &qc, &rc, func() {
		_ = db.Callback().Query().Remove("test:page_q")
		_ = db.Callback().Row().Remove("test:page_r")
	}
}

// AC-3: COUNT=0（无匹配）+ skipCount=false → total=0、空结果，且不执行投影 Find
func TestPageAs_count_zero_returns_empty_and_skips_find(t *testing.T) {
	db, repo := setupPageDB(t)
	qc, rc, cleanup := pageProbe(t, db)
	defer cleanup()
	q, mu := NewQuery[pageUser](context.Background())
	q.Eq(&mu.Name, "nobody").Page(1, 10)
	var rows []pageVO
	total, err := PageAs(repo, q, &rows, false)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatalf("total=%d, 期望 0", total)
	}
	if len(rows) != 0 {
		t.Fatalf("len=%d, 期望 0", len(rows))
	}
	// COUNT=0 时必须提前返回、不执行投影 Find：Query callback 恰好 1 次（仅 COUNT）
	if *qc != 1 || *rc != 0 {
		t.Fatalf("query=%d row=%d, 期望 1/0（Find 未被跳过则 query=2）", *qc, *rc)
	}
}

// AC-4: DataRule dept_id=1 → total=2，投影数据只含授权行
func TestPageAs_datarule_filters_total_and_rows(t *testing.T) {
	ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{
		{Column: "dept_id", Condition: "=", Value: "1"},
	})
	_, repo := setupPageDB(t)
	q, _ := NewQuery[pageUser](ctx)
	q.Select("name AS name").OrderRaw("id ASC").Page(1, 10)
	var rows []pageVO
	total, err := PageAs(repo, q, &rows, false)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("total=%d, 期望 2（DataRule 限定 dept_id=1）", total)
	}
	if len(rows) != 2 || rows[0].Name != "alice" || rows[1].Name != "bob" {
		t.Fatalf("rows=%+v, 期望 [alice bob]", rows)
	}
}

// AC-5: q==nil → 返回 (0, ErrQueryNil)
func TestPageAs_nil_query_returns_ErrQueryNil(t *testing.T) {
	_, repo := setupPageDB(t)
	var rows []pageVO
	total, err := PageAs[pageUser, pageVO, uint](repo, nil, &rows, false)
	if err != ErrQueryNil {
		t.Fatalf("err=%v, 期望 ErrQueryNil", err)
	}
	if total != 0 {
		t.Fatalf("total=%d, 期望 0", total)
	}
}

// AC-6: q 累积错误（非法字段指针）→ 透传 error，total=0
func TestPageAs_propagates_query_build_error(t *testing.T) {
	_, repo := setupPageDB(t)
	q, _ := NewQuery[pageUser](context.Background())
	// 传入不属于注册实例的字段地址，resolveColumnName 失败 → 累积进 q.errs
	other := struct{ X int }{}
	q.Eq(&other.X, 1)
	var rows []pageVO
	total, err := PageAs(repo, q, &rows, false)
	if err == nil {
		t.Fatal("期望透传 q 累积错误，实际 nil")
	}
	if total != 0 {
		t.Fatalf("total=%d, 期望 0", total)
	}
}

// AC-7: 有数据 + skipCount=false 走 Query callback chain（COUNT+Find=2），不绕到 Row chain
func TestPageAs_uses_query_callback_chain_not_row(t *testing.T) {
	db, repo := setupPageDB(t)
	qc, rc, cleanup := pageProbe(t, db)
	defer cleanup()
	q, _ := NewQuery[pageUser](context.Background())
	q.Select("name AS name").OrderRaw("id ASC").Page(1, 2)
	var rows []pageVO
	total, err := PageAs(repo, q, &rows, false)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("total=%d, 期望 3", total)
	}
	// COUNT 走 Query chain 1 次 + 投影 Find 走 Query chain 1 次 = 2；Row chain 0 次
	if *qc != 2 || *rc != 0 {
		t.Fatalf("query=%d row=%d, 期望 2/0（绕到 Scan/Row 则 row≥1）", *qc, *rc)
	}
}
