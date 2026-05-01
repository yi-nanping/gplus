package gplus

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// probeUser 是仅用于 GORM 行为锁定测试的最小模型
type probeUser struct {
	ID   uint `gorm:"primarykey"`
	Name string
	Age  int
}

// TestGORMCallbackBehaviorProbe 锁定 GORM v1.31.x 的 callback chain 行为。
// 任意一条断言失败 → GORM 行为已变 → 必须重审 spec §1.1 + 附录 B。
func TestGORMCallbackBehaviorProbe(t *testing.T) {
	openDB := func(t *testing.T) *gorm.DB {
		t.Helper()
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.AutoMigrate(&probeUser{}); err != nil {
			t.Fatal(err)
		}
		db.Create(&probeUser{Name: "alice", Age: 20})
		return db
	}

	// probe：返回 (queryCount*int, rowCount*int, observedTable*string, cleanup)
	setupProbe := func(t *testing.T, db *gorm.DB) (q, r *int, tbl *string, cleanup func()) {
		t.Helper()
		var qc, rc int
		var ot string
		err := db.Callback().Query().Before("gorm:query").
			Register("test:probe_query", func(d *gorm.DB) {
				qc++
				if d.Statement.Schema != nil {
					ot = d.Statement.Schema.Table
				}
			})
		if err != nil {
			t.Fatal(err)
		}
		err = db.Callback().Row().Before("gorm:row").
			Register("test:probe_row", func(d *gorm.DB) {
				rc++
			})
		if err != nil {
			t.Fatal(err)
		}
		return &qc, &rc, &ot, func() {
			_ = db.Callback().Query().Remove("test:probe_query")
			_ = db.Callback().Row().Remove("test:probe_row")
		}
	}

	t.Run("Find_走_Query_chain", func(t *testing.T) {
		db := openDB(t)
		qc, rc, _, cleanup := setupProbe(t, db)
		defer cleanup()
		var rows []probeUser
		_ = db.Model(&probeUser{}).Find(&rows).Error
		if *qc != 1 || *rc != 0 {
			t.Fatalf("Find: queryCount=%d rowCount=%d, 期望 1/0", *qc, *rc)
		}
	})

	t.Run("Scan_走_Row_chain（基线）", func(t *testing.T) {
		db := openDB(t)
		qc, rc, _, cleanup := setupProbe(t, db)
		defer cleanup()
		var rows []probeUser
		_ = db.Model(&probeUser{}).Scan(&rows).Error
		if *qc != 0 || *rc != 1 {
			t.Fatalf("Scan: queryCount=%d rowCount=%d, 期望 0/1（GORM 行为已变？）", *qc, *rc)
		}
	})

	t.Run("Rows_走_Row_chain（基线）", func(t *testing.T) {
		db := openDB(t)
		qc, rc, _, cleanup := setupProbe(t, db)
		defer cleanup()
		rows, _ := db.Model(&probeUser{}).Rows()
		if rows != nil {
			_ = rows.Close()
		}
		if *qc != 0 || *rc != 1 {
			t.Fatalf("Rows: queryCount=%d rowCount=%d, 期望 0/1（GORM 行为已变？）", *qc, *rc)
		}
	})

	t.Run("First_dest_VO_不覆盖_Schema", func(t *testing.T) {
		db := openDB(t)
		_, _, tbl, cleanup := setupProbe(t, db)
		defer cleanup()
		type VO struct {
			Name string
		}
		var vo VO
		_ = db.Model(&probeUser{}).First(&vo).Error
		if *tbl != "probe_users" {
			t.Fatalf("First(&VO) 后 Schema.Table=%q, 期望 'probe_users'（GORM 行为已变 → spec C1 复活）", *tbl)
		}
	})

	t.Run("Find_aggregateWrap_不覆盖_Schema", func(t *testing.T) {
		db := openDB(t)
		_, _, tbl, cleanup := setupProbe(t, db)
		defer cleanup()
		var rows []aggregateWrap[int64]
		_ = db.Model(&probeUser{}).Select("SUM(age) AS " + aggregateAlias).Find(&rows).Error
		if *tbl != "probe_users" {
			t.Fatalf("Find(&[]aggregateWrap[int64]) 后 Schema.Table=%q, 期望 'probe_users'（aggregate isolation 失效）", *tbl)
		}
	})

	t.Run("空表_SUM_aggregateWrap_NULL_为_nil", func(t *testing.T) {
		emptyDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		_ = emptyDB.AutoMigrate(&probeUser{})
		var rows []aggregateWrap[int64]
		err = emptyDB.Model(&probeUser{}).Select("SUM(age) AS " + aggregateAlias).Find(&rows).Error
		if err != nil {
			t.Fatalf("空表 SUM Find: 报错 %v（NULL 处理失败 → 方案 G 失效）", err)
		}
		if len(rows) != 1 || rows[0].V != nil {
			t.Fatalf("空表 SUM: rows=%v, 期望 1 行 V=nil", rows)
		}
	})

	_ = context.Background() // import 占位
}

// TestFindAs_ProjectionCorrectness 验证 FindAs/FindOneAs 在各种 Query 设置下投影结果正确。
func TestFindAs_ProjectionCorrectness(t *testing.T) {
	type projUser struct {
		ID     uint `gorm:"primarykey"`
		Name   string
		Age    int
		DeptID uint
	}
	type projDept struct {
		ID   uint `gorm:"primarykey"`
		Name string
	}
	type userVO struct {
		Name     string
		DeptName string
	}

	openDB := func(t *testing.T) (*gorm.DB, *Repository[uint, projUser]) {
		t.Helper()
		db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		_ = db.AutoMigrate(&projUser{}, &projDept{})
		db.Create(&projDept{ID: 1, Name: "Eng"})
		db.Create(&projDept{ID: 2, Name: "Sales"})
		db.Create(&projUser{Name: "alice", Age: 20, DeptID: 1})
		db.Create(&projUser{Name: "bob", Age: 30, DeptID: 1})
		db.Create(&projUser{Name: "carol", Age: 25, DeptID: 2})
		return db, NewRepository[uint, projUser](db)
	}

	t.Run("LEFT_JOIN_+_alias_映射", func(t *testing.T) {
		db, repo := openDB(t)
		_ = db
		q, _ := NewQuery[projUser](context.Background())
		q.LeftJoin("proj_depts", "proj_users.dept_id = proj_depts.id").
			Select("proj_users.name AS name", "proj_depts.name AS dept_name").
			OrderRaw("proj_users.id ASC")
		var rows []userVO
		if err := FindAs(repo, q, &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 3 {
			t.Fatalf("len=%d, 期望 3", len(rows))
		}
		if rows[0].Name != "alice" || rows[0].DeptName != "Eng" {
			t.Fatalf("rows[0]=%+v", rows[0])
		}
		if rows[2].Name != "carol" || rows[2].DeptName != "Sales" {
			t.Fatalf("rows[2]=%+v", rows[2])
		}
	})

	t.Run("WHERE_+_LIMIT_透传", func(t *testing.T) {
		_, repo := openDB(t)
		q, mu := NewQuery[projUser](context.Background())
		q.Gt(&mu.Age, 20).OrderRaw("id ASC").Limit(1)
		type nameVO struct{ Name string }
		var rows []nameVO
		if err := FindAs(repo, q, &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Name != "bob" {
			t.Fatalf("rows=%+v, 期望 [bob]", rows)
		}
	})

	t.Run("Distinct_+_FindAs", func(t *testing.T) {
		_, repo := openDB(t)
		q, mu := NewQuery[projUser](context.Background())
		q.Distinct(&mu.DeptID).OrderRaw("dept_id ASC")
		type deptIDVO struct {
			DeptID uint
		}
		var rows []deptIDVO
		if err := FindAs(repo, q, &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("Distinct 后 len=%d, 期望 2", len(rows))
		}
	})

	t.Run("DataRule_透传_builder_加_WHERE", func(t *testing.T) {
		_, repo := openDB(t)
		ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{
			{Column: "dept_id", Condition: "=", Value: "1"},
		})
		q, _ := NewQuery[projUser](ctx)
		type nameVO struct{ Name string }
		var rows []nameVO
		if err := FindAs(repo, q, &rows); err != nil {
			t.Fatal(err)
		}
		// DataRule 限定 dept_id=1，应只返回 alice / bob
		if len(rows) != 2 {
			t.Fatalf("DataRule 后 len=%d, 期望 2", len(rows))
		}
	})

	t.Run("FindOneAs_单行_alias", func(t *testing.T) {
		db, repo := openDB(t)
		_ = db
		q, _ := NewQuery[projUser](context.Background())
		q.LeftJoin("proj_depts", "proj_users.dept_id = proj_depts.id").
			Select("proj_users.name AS name", "proj_depts.name AS dept_name").
			WhereRaw("proj_users.name = ?", "carol")
		var one userVO
		if err := FindOneAs(repo, q, &one); err != nil {
			t.Fatal(err)
		}
		if one.Name != "carol" || one.DeptName != "Sales" {
			t.Fatalf("one=%+v", one)
		}
	})
}

// TestFindAs_CallbackChainMatrix 验证 FindAs/FindOneAs/aggregate 走 Query callback chain。
// 这是漏洞修复的核心证明 — 任意一条失败说明回归到了 Row chain。
func TestFindAs_CallbackChainMatrix(t *testing.T) {
	type matrixUser struct {
		ID   uint `gorm:"primarykey"`
		Name string
		Age  int
	}

	openDB := func(t *testing.T) *gorm.DB {
		t.Helper()
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.AutoMigrate(&matrixUser{}); err != nil {
			t.Fatal(err)
		}
		db.Create(&matrixUser{Name: "alice", Age: 20})
		db.Create(&matrixUser{Name: "bob", Age: 30})
		return db
	}

	type counts struct {
		query int
		row   int
	}
	probe := func(t *testing.T, db *gorm.DB) (*counts, func()) {
		t.Helper()
		c := &counts{}
		if err := db.Callback().Query().Before("gorm:query").
			Register("test:matrix_q", func(*gorm.DB) { c.query++ }); err != nil {
			t.Fatal(err)
		}
		if err := db.Callback().Row().Before("gorm:row").
			Register("test:matrix_r", func(*gorm.DB) { c.row++ }); err != nil {
			t.Fatal(err)
		}
		return c, func() {
			_ = db.Callback().Query().Remove("test:matrix_q")
			_ = db.Callback().Row().Remove("test:matrix_r")
		}
	}

	type matrixVO struct {
		Name string
	}

	t.Run("FindAs_有数据", func(t *testing.T) {
		db := openDB(t)
		c, cleanup := probe(t, db)
		defer cleanup()
		repo := NewRepository[uint, matrixUser](db)
		q, _ := NewQuery[matrixUser](context.Background())
		var rows []matrixVO
		err := FindAs[matrixUser, matrixVO, uint](repo, q, &rows)
		if err != nil {
			t.Fatalf("FindAs err=%v", err)
		}
		if c.query != 1 || c.row != 0 {
			t.Fatalf("FindAs: query=%d row=%d, 期望 1/0", c.query, c.row)
		}
		if len(rows) != 2 {
			t.Fatalf("FindAs len=%d, 期望 2", len(rows))
		}
	})

	t.Run("FindOneAs_有匹配", func(t *testing.T) {
		db := openDB(t)
		c, cleanup := probe(t, db)
		defer cleanup()
		repo := NewRepository[uint, matrixUser](db)
		q, mu := NewQuery[matrixUser](context.Background())
		q.Eq(&mu.Name, "alice")
		var one matrixVO
		err := FindOneAs[matrixUser, matrixVO, uint](repo, q, &one)
		if err != nil {
			t.Fatalf("FindOneAs err=%v", err)
		}
		if c.query != 1 || c.row != 0 {
			t.Fatalf("FindOneAs: query=%d row=%d, 期望 1/0", c.query, c.row)
		}
		if one.Name != "alice" {
			t.Fatalf("FindOneAs Name=%q, 期望 alice", one.Name)
		}
	})

	t.Run("FindOneAs_无匹配_返回_ErrRecordNotFound", func(t *testing.T) {
		db := openDB(t)
		c, cleanup := probe(t, db)
		defer cleanup()
		repo := NewRepository[uint, matrixUser](db)
		q, mu := NewQuery[matrixUser](context.Background())
		q.Eq(&mu.Name, "nobody")
		var one matrixVO
		err := FindOneAs[matrixUser, matrixVO, uint](repo, q, &one)
		if err == nil {
			t.Fatal("FindOneAs 期望 ErrRecordNotFound，实际 nil")
		}
		if c.query != 1 || c.row != 0 {
			t.Fatalf("FindOneAs(无匹配): query=%d row=%d, 期望 1/0", c.query, c.row)
		}
	})

	t.Run("Sum_有数据", func(t *testing.T) {
		db := openDB(t)
		c, cleanup := probe(t, db)
		defer cleanup()
		repo := NewRepository[uint, matrixUser](db)
		q, mu := NewQuery[matrixUser](context.Background())
		sum, err := Sum[matrixUser, int64, uint](repo, q, &mu.Age)
		if err != nil {
			t.Fatalf("Sum err=%v", err)
		}
		if c.query != 1 || c.row != 0 {
			t.Fatalf("Sum: query=%d row=%d, 期望 1/0（aggregate 修复）", c.query, c.row)
		}
		if sum != 50 {
			t.Fatalf("Sum=%d, 期望 50", sum)
		}
	})

	t.Run("Sum_空表_NULL_零值", func(t *testing.T) {
		emptyDB, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		_ = emptyDB.AutoMigrate(&matrixUser{})
		c, cleanup := probe(t, emptyDB)
		defer cleanup()
		repo := NewRepository[uint, matrixUser](emptyDB)
		q, mu := NewQuery[matrixUser](context.Background())
		sum, err := Sum[matrixUser, int64, uint](repo, q, &mu.Age)
		if err != nil {
			t.Fatalf("Sum 空表: err=%v（NULL 处理失败）", err)
		}
		if c.query != 1 || c.row != 0 {
			t.Fatalf("Sum 空表: query=%d row=%d, 期望 1/0", c.query, c.row)
		}
		if sum != 0 {
			t.Fatalf("Sum 空表=%d, 期望零值 0", sum)
		}
	})
}

// TestFindAs_Boundary 验证错误路径与防御逻辑。
func TestFindAs_Boundary(t *testing.T) {
	type bUser struct {
		ID   uint `gorm:"primarykey"`
		Name string
		Age  int
	}
	type bVO struct {
		Name string
	}

	openDB := func(t *testing.T) *Repository[uint, bUser] {
		t.Helper()
		db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		_ = db.AutoMigrate(&bUser{})
		db.Create(&bUser{Name: "alice", Age: 20})
		return NewRepository[uint, bUser](db)
	}

	t.Run("q_为_nil_返回_ErrQueryNil", func(t *testing.T) {
		repo := openDB(t)
		var rows []bVO
		if err := FindAs[bUser, bVO, uint](repo, nil, &rows); err != ErrQueryNil {
			t.Fatalf("err=%v, 期望 ErrQueryNil", err)
		}
		var one bVO
		if err := FindOneAs[bUser, bVO, uint](repo, nil, &one); err != ErrQueryNil {
			t.Fatalf("FindOneAs err=%v, 期望 ErrQueryNil", err)
		}
	})

	t.Run("FindOneAs_+_Limit_返回_ErrFindOneAsConflict", func(t *testing.T) {
		repo := openDB(t)
		q, _ := NewQuery[bUser](context.Background())
		q.Limit(5)
		var one bVO
		if err := FindOneAs(repo, q, &one); err != ErrFindOneAsConflict {
			t.Fatalf("err=%v, 期望 ErrFindOneAsConflict", err)
		}
	})

	t.Run("FindOneAs_+_Page_返回_ErrFindOneAsConflict", func(t *testing.T) {
		repo := openDB(t)
		q, _ := NewQuery[bUser](context.Background())
		q.Page(2, 10) // 内部设 limit + offset
		var one bVO
		if err := FindOneAs(repo, q, &one); err != ErrFindOneAsConflict {
			t.Fatalf("err=%v, 期望 ErrFindOneAsConflict", err)
		}
	})

	t.Run("FindOneAs_无匹配_返回_ErrRecordNotFound", func(t *testing.T) {
		repo := openDB(t)
		q, mu := NewQuery[bUser](context.Background())
		q.Eq(&mu.Name, "nobody")
		var one bVO
		err := FindOneAs(repo, q, &one)
		if err == nil {
			t.Fatal("期望 ErrRecordNotFound, 实际 nil")
		}
	})

	t.Run("dest_nil_切片_覆盖写入", func(t *testing.T) {
		repo := openDB(t)
		q, _ := NewQuery[bUser](context.Background())
		var rows []bVO // nil 切片
		if err := FindAs(repo, q, &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("len=%d, 期望 1", len(rows))
		}
	})

	t.Run("同一_q_两次_FindAs_DataRule_幂等", func(t *testing.T) {
		// 验证 dataRuleApplied 幂等保护
		ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{
			{Column: "age", Condition: ">=", Value: "18"},
		})
		repo := openDB(t)
		q, _ := NewQuery[bUser](ctx)
		var rows1, rows2 []bVO
		_ = FindAs(repo, q, &rows1)
		_ = FindAs(repo, q, &rows2)
		if len(rows1) != len(rows2) {
			t.Fatalf("两次结果不一致: %d vs %d", len(rows1), len(rows2))
		}
	})

	t.Run("FindAs_+_DataRule_+_LEFT_JOIN_复合", func(t *testing.T) {
		// 复合场景：DataRule WHERE + JOIN ON 不互染
		type joinUser struct {
			ID     uint `gorm:"primarykey"`
			Name   string
			DeptID uint
		}
		type joinDept struct {
			ID   uint `gorm:"primarykey"`
			Name string
		}
		db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		_ = db.AutoMigrate(&joinUser{}, &joinDept{})
		db.Create(&joinDept{ID: 1, Name: "Eng"})
		db.Create(&joinDept{ID: 2, Name: "Sales"})
		db.Create(&joinUser{Name: "alice", DeptID: 1})
		db.Create(&joinUser{Name: "bob", DeptID: 2})

		ctx := context.WithValue(context.Background(), DataRuleKey, []DataRule{
			// 用 table.col 形式避免 JOIN 后二义性
			{Column: "join_users.dept_id", Condition: "=", Value: "1"},
		})
		repo := NewRepository[uint, joinUser](db)
		q, _ := NewQuery[joinUser](ctx)
		q.LeftJoin("join_depts", "join_users.dept_id = join_depts.id").
			Select("join_users.name AS name", "join_depts.name AS dept_name")

		type vo struct {
			Name     string
			DeptName string
		}
		var rows []vo
		if err := FindAs(repo, q, &rows); err != nil {
			t.Fatal(err)
		}
		// DataRule 限定 dept_id=1 → 只 alice；JOIN 拿 dept name "Eng"
		if len(rows) != 1 || rows[0].Name != "alice" || rows[0].DeptName != "Eng" {
			t.Fatalf("rows=%+v", rows)
		}
	})
}
