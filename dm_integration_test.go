//go:build dm

package gplus

import (
	"context"
	"os"
	"testing"

	dameng "github.com/godoes/gorm-dameng"
	"gorm.io/gorm"
)

// TestDM_BasicCRUD 验证 DM 方言下基本 CRUD（镜像 TestOracle_BasicCRUD）
func TestDM_BasicCRUD(t *testing.T) {
	repo, _ := setupDMDB(t)
	ctx := context.Background()

	alice := MySQLUser{Name: "Alice", Age: 20, Email: "alice@example.com"}
	bob := MySQLUser{Name: "Bob", Age: 25, Email: "bob@example.com"}
	assertError(t, repo.Save(ctx, &alice), false, "Save Alice 应成功")
	assertError(t, repo.Save(ctx, &bob), false, "Save Bob 应成功")

	t.Run("GetById", func(t *testing.T) {
		user, err := repo.GetById(ctx, alice.ID)
		assertError(t, err, false, "GetById 应成功")
		if user.Name != "Alice" {
			t.Errorf("GetById 返回错误记录，Name=%q", user.Name)
		}
	})

	t.Run("List", func(t *testing.T) {
		q, u := NewQuery[MySQLUser](ctx)
		q.Eq(&u.Name, "Bob")
		result, err := repo.List(q)
		assertError(t, err, false, "List 应成功")
		assertEqual(t, 1, len(result), "应找到 1 条记录")
		assertEqual(t, "Bob", result[0].Name, "Name 应为 Bob")
	})

	t.Run("Count", func(t *testing.T) {
		q, _ := NewQuery[MySQLUser](ctx)
		count, err := repo.Count(q)
		assertError(t, err, false, "Count 应成功")
		assertEqual(t, int64(2), count, "Count 应为 2")
	})

	t.Run("UpdateById", func(t *testing.T) {
		alice.Email = "alice_new@example.com"
		assertError(t, repo.UpdateById(ctx, &alice), false, "UpdateById 应成功")
		user, err := repo.GetById(ctx, alice.ID)
		assertError(t, err, false, "更新后 GetById 应成功")
		assertEqual(t, "alice_new@example.com", user.Email, "Email 应已更新")
	})

	t.Run("DeleteById", func(t *testing.T) {
		_, err := repo.DeleteById(ctx, bob.ID)
		assertError(t, err, false, "DeleteById 应成功")
		_, err = repo.GetById(ctx, bob.ID)
		if !IsNotFound(err) {
			t.Error("删除后 GetById 应返回 ErrRecordNotFound")
		}
	})
}

// TestDM_WhereConditions 验证各类 WHERE 条件在 DM 方言下正确（镜像 TestOracle_WhereConditions）
//
// 不含 IsNull——沿用 Oracle 实测决策：DM Oracle 兼容模式 ”=NULL 语义下 IsNull 测试不可靠。
func TestDM_WhereConditions(t *testing.T) {
	repo, _ := setupDMDB(t)
	ctx := context.Background()

	seeds := []MySQLUser{
		{Name: "Alpha", Age: 10, Email: "a@test.com"},
		{Name: "Beta", Age: 20, Email: "b@test.com"},
		{Name: "Gamma", Age: 30, Email: "c@test.com"},
		{Name: "Delta", Age: 40, Email: "d@test.com"}, // DM ''=NULL，empty email 改占位避免歧义
	}
	for i := range seeds {
		assertError(t, repo.Save(ctx, &seeds[i]), false, "Save seed 应成功")
	}

	t.Run("Ne", func(t *testing.T) {
		q, u := NewQuery[MySQLUser](ctx)
		q.Ne(&u.Name, "Alpha")
		result, err := repo.List(q)
		assertError(t, err, false, "Ne 应成功")
		if len(result) != 3 {
			t.Errorf("Ne: 期望 3 条，实际 %d 条", len(result))
		}
	})

	t.Run("LikeRight_Prefix", func(t *testing.T) {
		// 用前缀匹配避开 DM/Oracle case-sensitive LIKE
		q, u := NewQuery[MySQLUser](ctx)
		q.LikeRight(&u.Name, "Alp")
		result, err := repo.List(q)
		assertError(t, err, false, "LikeRight 应成功")
		assertEqual(t, 1, len(result), "LikeRight Alp%: 应找到 1 条 (Alpha)")
	})

	t.Run("In", func(t *testing.T) {
		q, u := NewQuery[MySQLUser](ctx)
		q.In(&u.Age, []int{10, 30})
		result, err := repo.List(q)
		assertError(t, err, false, "In 应成功")
		assertEqual(t, 2, len(result), "In: 应找到 2 条")
	})

	t.Run("NotIn", func(t *testing.T) {
		q, u := NewQuery[MySQLUser](ctx)
		q.NotIn(&u.Age, []int{10, 30})
		result, err := repo.List(q)
		assertError(t, err, false, "NotIn 应成功")
		assertEqual(t, 2, len(result), "NotIn: 应找到 2 条")
	})

	t.Run("Between", func(t *testing.T) {
		q, u := NewQuery[MySQLUser](ctx)
		q.Between(&u.Age, 15, 35)
		result, err := repo.List(q)
		assertError(t, err, false, "Between 应成功")
		assertEqual(t, 2, len(result), "Between: 应找到 2 条")
	})

	t.Run("GetOne", func(t *testing.T) {
		q, u := NewQuery[MySQLUser](ctx)
		q.Eq(&u.Name, "Gamma")
		user, err := repo.GetOne(q)
		assertError(t, err, false, "GetOne 应成功")
		assertEqual(t, 30, user.Age, "GetOne age 应为 30")
	})
}

// TestDM_OrderGroupHaving 验证 ORDER BY / GROUP BY / HAVING 在 DM 方言下正确
func TestDM_OrderGroupHaving(t *testing.T) {
	repo, _ := setupDMDB(t)
	ctx := context.Background()

	seeds := []MySQLUser{
		{Name: "A", Age: 20},
		{Name: "B", Age: 20},
		{Name: "C", Age: 30},
	}
	for i := range seeds {
		assertError(t, repo.Save(ctx, &seeds[i]), false, "Save seed 应成功")
	}

	t.Run("OrderBy_DESC", func(t *testing.T) {
		q, u := NewQuery[MySQLUser](ctx)
		q.Order(&u.Age, false)
		result, err := repo.List(q)
		assertError(t, err, false, "OrderBy 应成功")
		if len(result) > 0 && result[0].Age != 30 {
			t.Errorf("OrderBy DESC: 期望第一条 age=30，实际 %d", result[0].Age)
		}
	})

	t.Run("Page", func(t *testing.T) {
		// DM 8 Oracle 兼容模式用 FETCH FIRST N ROWS ONLY，GORM Limit/Offset 自动适配
		q, u := NewQuery[MySQLUser](ctx)
		q.Order(&u.Age, true).Limit(2).Offset(0)
		result, err := repo.List(q)
		assertError(t, err, false, "Page 应成功")
		assertEqual(t, 2, len(result), "Limit(2) 应返回 2 条")
	})

	t.Run("GroupBy_Having_RawScan", func(t *testing.T) {
		// HAVING 用 COUNT(*) 而非别名（DM/Oracle 严格 SQL 不支持别名引用）
		// FROM 子句的 my_sql_users 用引号锁定小写（dameng migrator 引号 lowercase 建表）
		// alias 用 "age"/"cnt" 锁定 lowercase 输出列名，与 struct tag 对齐
		// age 列引用也加引号锁定小写匹配 dameng 建表的 case-sensitive lowercase 列
		type row struct {
			Age int `gorm:"column:age"`
			Cnt int `gorm:"column:cnt"`
		}
		var results []row
		err := repo.RawScan(ctx, &results,
			`SELECT "age" AS "age", count(*) AS "cnt" FROM "my_sql_users" GROUP BY "age" HAVING count(*) > ?`, 1)
		assertError(t, err, false, "RawScan Group+Having 应成功")
		assertEqual(t, 1, len(results), "Having count>1 应只有 age=20 的组")
		if len(results) > 0 {
			assertEqual(t, 20, results[0].Age, "分组结果 age 应为 20")
		}
	})

	t.Run("UpdateByCond", func(t *testing.T) {
		u, m := NewUpdater[MySQLUser](ctx)
		u.Set(&m.Name, "A_updated").Eq(&m.Name, "A")
		rows, err := repo.UpdateByCond(u)
		assertError(t, err, false, "UpdateByCond 应成功")
		if rows != 1 {
			t.Errorf("UpdateByCond 应更新 1 行，实际 %d 行", rows)
		}
	})

	t.Run("DeleteByCond", func(t *testing.T) {
		q, m := NewQuery[MySQLUser](ctx)
		q.Eq(&m.Name, "C")
		rows, err := repo.DeleteByCond(q)
		assertError(t, err, false, "DeleteByCond 应成功")
		if rows != 1 {
			t.Errorf("DeleteByCond 应删除 1 行，实际 %d 行", rows)
		}
	})
}

// TestDM_JoinQuery 验证 LEFT JOIN ON 条件在 DM 方言下正确（镜像 TestOracle_JoinQuery）
func TestDM_JoinQuery(t *testing.T) {
	repo, _ := setupDMDB(t)
	ctx := context.Background()

	seeds := []MySQLUser{
		{Name: "JoinUser1", Age: 10},
		{Name: "JoinUser2", Age: 20},
	}
	for i := range seeds {
		assertError(t, repo.Save(ctx, &seeds[i]), false, "Save seed 应成功")
	}

	t.Run("LeftJoin_Self", func(t *testing.T) {
		// 自连接验证 JOIN 语句中列名转义不报错（DM 走双引号 quoter，gplus 自动加引号）
		// 用户传入裸 column 表达式时 quoteColumn 会把 my_sql_users.age 转义为
		// "my_sql_users"."age" 锁定小写匹配 dameng migrator 引号 lowercase 建表。
		q, _ := NewQuery[MySQLUser](ctx)
		q.Eq("my_sql_users.age", 10)
		q.LeftJoin("\"my_sql_users\" m2", "\"my_sql_users\".\"id\" = m2.\"id\"")
		result, err := repo.List(q)
		assertError(t, err, false, "LeftJoin 应成功")
		assertEqual(t, 1, len(result), "LeftJoin 结果应为 1 条")
	})
}

// TestDM_QuoteColumn 直接验证 DM 方言下转义符和 quoteColumn 输出
func TestDM_QuoteColumn(t *testing.T) {
	dsn := os.Getenv("TEST_DM_DSN")
	if dsn == "" {
		t.Skip("TEST_DM_DSN 未设置，跳过")
	}

	db, err := gorm.Open(dameng.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("DM 不可用，跳过: %v", err)
	}
	applyDBPoolLimits(t, db)

	t.Run("getQuoteChar_返回双引号", func(t *testing.T) {
		qL, qR := getQuoteChar(db)
		assertEqual(t, "\"", qL, "DM qL 应为双引号（锁定 lowercase 匹配 dameng 引号建表）")
		assertEqual(t, "\"", qR, "DM qR 应为双引号")
	})

	// 双引号 quoter 下 quoteColumn 自动加引号（与 postgres 行为一致）
	// 复杂表达式（含 ()/,/* 等）跳过转义保持原样
	cases := []struct {
		input string
		want  string
	}{
		{"name", "\"name\""},
		{"users.name", "\"users\".\"name\""},
		{"users.name AS u_name", "\"users\".\"name\" AS \"u_name\""},
		{"count(id)", "count(id)"},
		{"users.*", "\"users\".*"},
		{"", ""},
	}

	t.Run("quoteColumn_DM方言", func(t *testing.T) {
		qL, qR := getQuoteChar(db)
		for _, c := range cases {
			got := quoteColumn(c.input, qL, qR)
			if got != c.want {
				t.Errorf("quoteColumn(%q) = %q, want %q", c.input, got, c.want)
			}
		}
	})
}
