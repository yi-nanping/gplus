package gplus

import (
	"context"
	"os"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// defaultMySQLDSN 本地开发默认 DSN，CI 通过 TEST_MYSQL_DSN 覆盖
const defaultMySQLDSN = "root:root@tcp(127.0.0.1:3306)/test?charset=utf8mb4&parseTime=True&loc=Local"

// MySQLUser 用于 MySQL 集成测试的实体
type MySQLUser struct {
	ID    int64  `gorm:"primaryKey;autoIncrement"`
	Name  string `gorm:"column:username;size:64"`
	Age   int    `gorm:"column:age"`
	Email string `gorm:"column:email;size:128"`
}

// setupMySQLDB 连接 MySQL 并迁移表，返回 Repository
func setupMySQLDB(t *testing.T) (*Repository[int64, MySQLUser], *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		dsn = defaultMySQLDSN
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Skipf("MySQL 不可用，跳过集成测试: %v", err)
	}

	if err := db.AutoMigrate(&MySQLUser{}); err != nil {
		t.Fatalf("迁移 MySQL 表失败: %v", err)
	}

	repo := NewRepository[int64, MySQLUser](db)
	// 清空测试数据，保证幂等
	if _, err := repo.RawExec(context.Background(), "DELETE FROM my_sql_users"); err != nil {
		t.Fatalf("清空表失败: %v", err)
	}

	return repo, db
}

// TestMySQL_BasicCRUD 验证 MySQL 方言下基本 CRUD
func TestMySQL_BasicCRUD(t *testing.T) {
	repo, _ := setupMySQLDB(t)
	ctx := context.Background()

	// 使用封装的 Save 插入数据
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

// TestMySQL_WhereConditions 验证各类 WHERE 条件在 MySQL 方言下反引号转义正确
func TestMySQL_WhereConditions(t *testing.T) {
	repo, _ := setupMySQLDB(t)
	ctx := context.Background()

	seeds := []MySQLUser{
		{Name: "Alpha", Age: 10, Email: "a@test.com"},
		{Name: "Beta", Age: 20, Email: "b@test.com"},
		{Name: "Gamma", Age: 30, Email: "c@test.com"},
		{Name: "Delta", Age: 40, Email: ""},
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

	t.Run("Like", func(t *testing.T) {
		q, u := NewQuery[MySQLUser](ctx)
		q.Like(&u.Name, "lph")
		result, err := repo.List(q)
		assertError(t, err, false, "Like 应成功")
		assertEqual(t, 1, len(result), "Like: 应找到 1 条")
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

	t.Run("IsNull", func(t *testing.T) {
		// MySQL 中空字符串不等于 NULL，验证查询可以正常执行
		q, u := NewQuery[MySQLUser](ctx)
		q.IsNull(&u.Email)
		_, err := repo.List(q)
		assertError(t, err, false, "IsNull 应成功")
	})

	t.Run("GetOne", func(t *testing.T) {
		q, u := NewQuery[MySQLUser](ctx)
		q.Eq(&u.Name, "Gamma")
		user, err := repo.GetOne(q)
		assertError(t, err, false, "GetOne 应成功")
		assertEqual(t, 30, user.Age, "GetOne age 应为 30")
	})
}

// TestMySQL_OrderGroupHaving 验证 ORDER BY / GROUP BY / HAVING 在 MySQL 方言下正确
func TestMySQL_OrderGroupHaving(t *testing.T) {
	repo, _ := setupMySQLDB(t)
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
		q, u := NewQuery[MySQLUser](ctx)
		q.Order(&u.Age, true).Limit(2).Offset(0)
		result, err := repo.List(q)
		assertError(t, err, false, "Page 应成功")
		assertEqual(t, 2, len(result), "Limit(2) 应返回 2 条")
	})

	t.Run("GroupBy_Having_RawScan", func(t *testing.T) {
		// 使用 RawScan 验证 GROUP BY + HAVING 在 MySQL 下执行正确
		type row struct {
			Age int `gorm:"column:age"`
			Cnt int `gorm:"column:cnt"`
		}
		var results []row
		err := repo.RawScan(ctx, &results,
			"SELECT age, count(*) AS cnt FROM my_sql_users GROUP BY age HAVING count(*) > ?", 1)
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

// TestMySQL_JoinQuery 验证 LEFT JOIN ON 条件中反引号转义
func TestMySQL_JoinQuery(t *testing.T) {
	repo, _ := setupMySQLDB(t)
	ctx := context.Background()

	seeds := []MySQLUser{
		{Name: "JoinUser1", Age: 10},
		{Name: "JoinUser2", Age: 20},
	}
	for i := range seeds {
		assertError(t, repo.Save(ctx, &seeds[i]), false, "Save seed 应成功")
	}

	t.Run("LeftJoin_Self", func(t *testing.T) {
		// 自连接验证 JOIN 语句中列名反引号转义不报错
		// 使用表限定列名避免歧义：my_sql_users.age
		q, _ := NewQuery[MySQLUser](ctx)
		q.Eq("my_sql_users.age", 10)
		q.LeftJoin("my_sql_users AS m2", "my_sql_users.id = m2.id")
		result, err := repo.List(q)
		assertError(t, err, false, "LeftJoin 应成功")
		assertEqual(t, 1, len(result), "LeftJoin 结果应为 1 条")
	})
}

// TestMySQL_QuoteColumn 直接验证 MySQL 方言下转义符和 quoteColumn 输出
func TestMySQL_QuoteColumn(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		dsn = defaultMySQLDSN
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("MySQL 不可用，跳过集成测试: %v", err)
	}

	t.Run("getQuoteChar_返回反引号", func(t *testing.T) {
		qL, qR := getQuoteChar(db)
		assertEqual(t, "`", qL, "MySQL qL 应为反引号")
		assertEqual(t, "`", qR, "MySQL qR 应为反引号")
	})

	cases := []struct {
		input string
		want  string
	}{
		{"name", "`name`"},
		{"users.name", "`users`.`name`"},
		{"users.name AS u_name", "`users`.`name` AS `u_name`"},
		{"count(id)", "count(id)"},
		{"users.*", "`users`.*"},
		{"", ""},
	}

	t.Run("quoteColumn_MySQL方言", func(t *testing.T) {
		qL, qR := getQuoteChar(db)
		for _, c := range cases {
			got := quoteColumn(c.input, qL, qR)
			if got != c.want {
				t.Errorf("quoteColumn(%q) = %q, want %q", c.input, got, c.want)
			}
		}
	})
}
