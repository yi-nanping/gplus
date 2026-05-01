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
