package gplus

import (
	"context"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestQuery_CompareOps 测试各类比较运算符（Ne/Or 系列）
func TestQuery_CompareOps(t *testing.T) {
	ctx := context.Background()

	type opCase struct {
		name     string
		fn       func(q *Query[TestUser], u *TestUser)
		wantIsOr bool
		wantOp   string
	}

	cases := []opCase{
		{"Ne", func(q *Query[TestUser], u *TestUser) { q.Ne(&u.Age, 18) }, false, OpNe},
		{"OrEq", func(q *Query[TestUser], u *TestUser) { q.OrEq(&u.Age, 18) }, true, OpEq},
		{"OrNe", func(q *Query[TestUser], u *TestUser) { q.OrNe(&u.Age, 18) }, true, OpNe},
		{"Ge", func(q *Query[TestUser], u *TestUser) { q.Ge(&u.Age, 18) }, false, OpGe},
		{"OrGe", func(q *Query[TestUser], u *TestUser) { q.OrGe(&u.Age, 18) }, true, OpGe},
		{"Le", func(q *Query[TestUser], u *TestUser) { q.Le(&u.Age, 18) }, false, OpLe},
		{"OrLe", func(q *Query[TestUser], u *TestUser) { q.OrLe(&u.Age, 18) }, true, OpLe},
		{"OrGt", func(q *Query[TestUser], u *TestUser) { q.OrGt(&u.Age, 18) }, true, OpGt},
		{"OrLt", func(q *Query[TestUser], u *TestUser) { q.OrLt(&u.Age, 18) }, true, OpLt},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q, u := NewQuery[TestUser](ctx)
			c.fn(q, u)
			assertError(t, q.GetError(), false, c.name+" 不应有错误")
			if len(q.conditions) != 1 {
				t.Fatalf("期望 1 个条件，实际 %d", len(q.conditions))
			}
			cond := q.conditions[0]
			if cond.isOr != c.wantIsOr {
				t.Errorf("isOr: 期望 %v，实际 %v", c.wantIsOr, cond.isOr)
			}
			if cond.operator != c.wantOp {
				t.Errorf("operator: 期望 %q，实际 %q", c.wantOp, cond.operator)
			}
		})
	}
}

// TestQuery_InOps 测试 IN / NOT IN 系列
func TestQuery_InOps(t *testing.T) {
	ctx := context.Background()

	t.Run("In 正常添加", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.In(&u.Age, []int{18, 19, 20})
		if len(q.conditions) != 1 {
			t.Fatalf("期望 1 个条件，实际 %d", len(q.conditions))
		}
		if q.conditions[0].operator != OpIn || q.conditions[0].isOr {
			t.Error("In 条件不正确")
		}
	})

	t.Run("OrIn isOr=true", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.OrIn(&u.Age, []int{18, 19})
		if len(q.conditions) != 1 || !q.conditions[0].isOr || q.conditions[0].operator != OpIn {
			t.Error("OrIn 条件不正确")
		}
	})

	t.Run("NotIn operator=NOT IN", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.NotIn(&u.Age, []int{1, 2})
		if len(q.conditions) != 1 || q.conditions[0].operator != OpNotIn || q.conditions[0].isOr {
			t.Error("NotIn 条件不正确")
		}
	})

	t.Run("OrNotIn isOr=true 且 operator=NOT IN", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.OrNotIn(&u.Age, []int{1, 2})
		if len(q.conditions) != 1 || q.conditions[0].operator != OpNotIn || !q.conditions[0].isOr {
			t.Error("OrNotIn 条件不正确")
		}
	})
}

// TestQuery_NullOps 测试 IS NULL / IS NOT NULL 系列
func TestQuery_NullOps(t *testing.T) {
	ctx := context.Background()

	t.Run("IsNull 正常添加", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.IsNull(&u.Name)
		if len(q.conditions) != 1 || q.conditions[0].operator != OpIsNull || q.conditions[0].isOr {
			t.Error("IsNull 条件不正确")
		}
	})

	t.Run("OrIsNull isOr=true", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.OrIsNull(&u.Name)
		if len(q.conditions) != 1 || q.conditions[0].operator != OpIsNull || !q.conditions[0].isOr {
			t.Error("OrIsNull 条件不正确")
		}
	})

	t.Run("IsNotNull 正常添加", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.IsNotNull(&u.Name)
		if len(q.conditions) != 1 || q.conditions[0].operator != OpIsNotNull || q.conditions[0].isOr {
			t.Error("IsNotNull 条件不正确")
		}
	})

	t.Run("OrIsNotNull isOr=true", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.OrIsNotNull(&u.Name)
		if len(q.conditions) != 1 || q.conditions[0].operator != OpIsNotNull || !q.conditions[0].isOr {
			t.Error("OrIsNotNull 条件不正确")
		}
	})
}

// TestQuery_LikeOps 测试 LIKE 系列通配符拼接与 isOr 标志
func TestQuery_LikeOps(t *testing.T) {
	ctx := context.Background()

	t.Run("Like 两端通配符", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Like(&u.Name, "alice")
		if len(q.conditions) != 1 {
			t.Fatal("期望 1 个条件")
		}
		if q.conditions[0].value != "%alice%" {
			t.Errorf("Like 值应为 %%alice%%，实际 %v", q.conditions[0].value)
		}
		if q.conditions[0].isOr {
			t.Error("Like 的 isOr 应为 false")
		}
	})

	t.Run("OrLike 两端通配符且 isOr=true", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.OrLike(&u.Name, "alice")
		if !q.conditions[0].isOr || q.conditions[0].value != "%alice%" {
			t.Error("OrLike 不正确")
		}
	})

	t.Run("LikeLeft 左通配符", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.LikeLeft(&u.Name, "alice")
		if q.conditions[0].value != "%alice" {
			t.Errorf("LikeLeft 值应为 %%alice，实际 %v", q.conditions[0].value)
		}
		if q.conditions[0].isOr {
			t.Error("LikeLeft 的 isOr 应为 false")
		}
	})

	t.Run("OrLikeLeft 左通配符且 isOr=true", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.OrLikeLeft(&u.Name, "alice")
		if !q.conditions[0].isOr || q.conditions[0].value != "%alice" {
			t.Error("OrLikeLeft 不正确")
		}
	})

	t.Run("LikeRight 右通配符", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.LikeRight(&u.Name, "alice")
		if q.conditions[0].value != "alice%" {
			t.Errorf("LikeRight 值应为 alice%%，实际 %v", q.conditions[0].value)
		}
		if q.conditions[0].isOr {
			t.Error("LikeRight 的 isOr 应为 false")
		}
	})

	t.Run("OrLikeRight 右通配符且 isOr=true", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.OrLikeRight(&u.Name, "alice")
		if !q.conditions[0].isOr || q.conditions[0].value != "alice%" {
			t.Error("OrLikeRight 不正确")
		}
	})

	t.Run("NotLike 两端通配符且 operator=NOT LIKE", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.NotLike(&u.Name, "alice")
		if q.conditions[0].operator != OpNotLike {
			t.Errorf("NotLike operator 应为 NOT LIKE，实际 %q", q.conditions[0].operator)
		}
		if q.conditions[0].value != "%alice%" {
			t.Errorf("NotLike 值应为 %%alice%%，实际 %v", q.conditions[0].value)
		}
		if q.conditions[0].isOr {
			t.Error("NotLike 的 isOr 应为 false")
		}
	})

	t.Run("OrNotLike 两端通配符且 isOr=true", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.OrNotLike(&u.Name, "alice")
		if !q.conditions[0].isOr || q.conditions[0].operator != OpNotLike || q.conditions[0].value != "%alice%" {
			t.Error("OrNotLike 不正确")
		}
	})
}

// TestQuery_BetweenOps 测试 BETWEEN 剩余变体
func TestQuery_BetweenOps(t *testing.T) {
	ctx := context.Background()

	t.Run("OrBetween isOr=true", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.OrBetween(&u.Age, 18, 25)
		if len(q.conditions) != 1 || !q.conditions[0].isOr || q.conditions[0].operator != OpBetween {
			t.Error("OrBetween 不正确")
		}
	})

	t.Run("NotBetween operator=NOT BETWEEN", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.NotBetween(&u.Age, 18, 25)
		if len(q.conditions) != 1 || q.conditions[0].isOr || q.conditions[0].operator != OpNotBetween {
			t.Error("NotBetween 不正确")
		}
	})

	t.Run("OrNotBetween isOr=true 且 operator=NOT BETWEEN", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.OrNotBetween(&u.Age, 18, 25)
		if len(q.conditions) != 1 || !q.conditions[0].isOr || q.conditions[0].operator != OpNotBetween {
			t.Error("OrNotBetween 不正确")
		}
	})
}

// TestQuery_QueryModifiers 测试查询修饰符
func TestQuery_QueryModifiers(t *testing.T) {
	ctx := context.Background()

	t.Run("Order ASC", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Order(&u.Age, true)
		if len(q.orders) != 1 {
			t.Fatalf("期望 1 个排序条件，实际 %d", len(q.orders))
		}
		if !strings.Contains(q.orders[0].expr, KeyAsc) {
			t.Errorf("ASC 排序应包含 %q，实际 %q", KeyAsc, q.orders[0].expr)
		}
	})

	t.Run("Order DESC", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Order(&u.Age, false)
		if len(q.orders) != 1 {
			t.Fatalf("期望 1 个排序条件，实际 %d", len(q.orders))
		}
		if !strings.Contains(q.orders[0].expr, KeyDesc) {
			t.Errorf("DESC 排序应包含 %q，实际 %q", KeyDesc, q.orders[0].expr)
		}
	})

	t.Run("多字段排序独立追加", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Order(&u.Age, true).Order(&u.Name, false)
		if len(q.orders) != 2 {
			t.Errorf("期望 2 个排序条件，实际 %d", len(q.orders))
		}
	})

	t.Run("OrderRaw 追加原生表达式", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.OrderRaw("FIELD(age, 18, 25, 30)")
		if len(q.orders) != 1 {
			t.Fatalf("期望 1 个 order 项，实际 %d", len(q.orders))
		}
		if !q.orders[0].isRaw {
			t.Error("OrderRaw 项 isRaw 应为 true")
		}
		if q.orders[0].expr != "FIELD(age, 18, 25, 30)" {
			t.Errorf("expr 不符，实际 %q", q.orders[0].expr)
		}
	})

	t.Run("OrderRaw 空表达式返回错误", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.OrderRaw("")
		if q.GetError() == nil {
			t.Error("空 expr 应累积错误")
		}
	})

	t.Run("OrderRaw 与 Order 共存保留调用顺序", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Order(&u.Age, false).OrderRaw("FIELD(age, 18, 25)")
		if len(q.orders) != 2 {
			t.Fatalf("期望 2 个 order 项，实际 %d", len(q.orders))
		}
		if q.orders[0].isRaw {
			t.Error("第一项应为普通 Order")
		}
		if !q.orders[1].isRaw {
			t.Error("第二项应为 OrderRaw")
		}
	})

	t.Run("Limit 设置", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Limit(50)
		if q.limit != 50 {
			t.Errorf("limit 期望 50，实际 %d", q.limit)
		}
	})

	t.Run("Offset 设置", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Offset(100)
		if q.offset != 100 {
			t.Errorf("offset 期望 100，实际 %d", q.offset)
		}
	})

	t.Run("Select 多字段", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Select(&u.Name, &u.Age)
		if len(q.selects) != 2 {
			t.Errorf("期望 2 个 select 字段，实际 %d", len(q.selects))
		}
	})

	t.Run("Omit 排除多字段", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Omit(&u.Name, &u.Score)
		if len(q.omits) != 2 {
			t.Errorf("期望 2 个 omit 字段，实际 %d", len(q.omits))
		}
	})

	t.Run("Distinct 无参 distinct=true 且不追加 selects", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Distinct()
		if !q.distinct {
			t.Error("Distinct() 应将 distinct 设为 true")
		}
		if len(q.selects) != 0 {
			t.Errorf("无参 Distinct 不应追加 selects，实际 %d", len(q.selects))
		}
	})

	t.Run("Distinct 带参追加 selects", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Distinct(&u.Name, &u.Age)
		if !q.distinct {
			t.Error("Distinct 应将 distinct 设为 true")
		}
		if len(q.selects) != 2 {
			t.Errorf("Distinct 带参应追加 2 个 selects，实际 %d", len(q.selects))
		}
	})

	t.Run("Group 多字段", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Group(&u.Name, &u.Age)
		if len(q.groups) != 2 {
			t.Errorf("期望 2 个分组字段，实际 %d", len(q.groups))
		}
	})

	t.Run("Table 动态表名", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Table("custom_table")
		if q.tableName != "custom_table" {
			t.Errorf("tableName 期望 custom_table，实际 %q", q.tableName)
		}
	})

	t.Run("Unscoped 设置 unscoped=true", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Unscoped()
		if !q.unscoped {
			t.Error("Unscoped() 应将 unscoped 设为 true")
		}
	})
}

// TestQuery_Locks 测试悲观锁字段设置
func TestQuery_Locks(t *testing.T) {
	ctx := context.Background()

	t.Run("LockWrite 设置 lockStrength=UPDATE", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.LockWrite()
		if q.lockStrength != "UPDATE" {
			t.Errorf("LockWrite 应设置 lockStrength=UPDATE，实际 %q", q.lockStrength)
		}
	})

	t.Run("LockRead 设置 lockStrength=SHARE", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.LockRead()
		if q.lockStrength != "SHARE" {
			t.Errorf("LockRead 应设置 lockStrength=SHARE，实际 %q", q.lockStrength)
		}
	})
}

// TestQuery_Joins 测试各类 JOIN 方法的结构设置
func TestQuery_Joins(t *testing.T) {
	ctx := context.Background()

	type joinCase struct {
		name       string
		fn         func(*Query[TestUser])
		wantMethod string
		wantTable  string
	}

	cases := []joinCase{
		{"InnerJoin", func(q *Query[TestUser]) { q.InnerJoin("orders", "orders.user_id = users.id") }, JoinInner, "orders"},
		{"OuterJoin", func(q *Query[TestUser]) { q.OuterJoin("orders", "orders.user_id = users.id") }, JoinOuter, "orders"},
		{"FullJoin", func(q *Query[TestUser]) { q.FullJoin("orders", "orders.user_id = users.id") }, JoinFull, "orders"},
		{"CrossJoin", func(q *Query[TestUser]) { q.CrossJoin("orders") }, JoinCross, "orders"},
		{"NaturalJoin", func(q *Query[TestUser]) { q.NaturalJoin("orders") }, JoinNatural, "orders"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q, _ := NewQuery[TestUser](ctx)
			c.fn(q)
			if len(q.joins) != 1 {
				t.Fatalf("期望 1 个 join，实际 %d", len(q.joins))
			}
			if q.joins[0].method != c.wantMethod {
				t.Errorf("method: 期望 %q，实际 %q", c.wantMethod, q.joins[0].method)
			}
			if q.joins[0].table != c.wantTable {
				t.Errorf("table: 期望 %q，实际 %q", c.wantTable, q.joins[0].table)
			}
		})
	}
}

// TestQuery_HavingGroup 测试 HavingGroup 嵌套 Having
func TestQuery_HavingGroup(t *testing.T) {
	ctx := context.Background()

	t.Run("HavingGroup 正常路径", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Having("age", OpGt, 10).HavingGroup(func(sub *Query[TestUser]) {
			sub.Having("score", OpGt, 90)
		})
		if len(q.havings) != 2 {
			t.Errorf("期望 2 个 having，实际 %d", len(q.havings))
		}
		if len(q.havings[1].group) != 1 {
			t.Errorf("HavingGroup 应有 1 个子条件，实际 %d", len(q.havings[1].group))
		}
	})

	t.Run("HavingGroup 空 fn 不追加", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.HavingGroup(func(sub *Query[TestUser]) {})
		if len(q.havings) != 0 {
			t.Errorf("空 HavingGroup 不应追加条件，实际 %d", len(q.havings))
		}
	})

	t.Run("HavingGroup nil fn 写入 errs", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.HavingGroup(nil)
		assertError(t, q.GetError(), true, "HavingGroup(nil) 应写入 errs")
		if len(q.havings) != 0 {
			t.Errorf("HavingGroup(nil) 不应追加条件，实际 %d", len(q.havings))
		}
	})

	t.Run("HavingGroup 内部错误应传播到外层", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.HavingGroup(func(sub *Query[TestUser]) {
			sub.applyDataRule(DataRule{Column: "age+(bad)", Condition: "=", Value: "1"})
		})
		assertError(t, q.GetError(), true, "HavingGroup 内部错误应传播到外层")
	})
}

// TestQuery_Group 测试 Group 分组
func TestQuery_Group(t *testing.T) {
	ctx := context.Background()

	t.Run("Group 单字段", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Group(&u.Name)
		if len(q.groups) != 1 {
			t.Errorf("期望 1 个分组字段，实际 %d", len(q.groups))
		}
	})

	t.Run("Group 多字段", func(t *testing.T) {
		q, u := NewQuery[TestUser](ctx)
		q.Group(&u.Name, &u.Age)
		if len(q.groups) != 2 {
			t.Errorf("期望 2 个分组字段，实际 %d", len(q.groups))
		}
	})
}

// TestQuery_Table 测试动态表名
func TestQuery_Table(t *testing.T) {
	ctx := context.Background()

	q, _ := NewQuery[TestUser](ctx)
	q.Table("custom_users")
	if q.tableName != "custom_users" {
		t.Errorf("Table: 期望 'custom_users'，实际 %q", q.tableName)
	}
}

// TestQuery_Unscoped 测试 Unscoped
func TestQuery_Unscoped(t *testing.T) {
	ctx := context.Background()

	q, _ := NewQuery[TestUser](ctx)
	if q.IsUnscoped() {
		t.Error("初始状态 unscoped 应为 false")
	}
	q.Unscoped()
	if !q.IsUnscoped() {
		t.Error("Unscoped() 后 IsUnscoped 应为 true")
	}
}

// TestQuery_Preload 测试 Preload 预加载
func TestQuery_Preload(t *testing.T) {
	ctx := context.Background()

	t.Run("Preload 正常追加", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Preload("Orders")
		if len(q.preloads) != 1 {
			t.Errorf("期望 1 个 preload，实际 %d", len(q.preloads))
		}
		if q.preloads[0].query != "Orders" {
			t.Errorf("preload query: 期望 'Orders'，实际 %q", q.preloads[0].query)
		}
	})

	t.Run("Preload 多次链式调用", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Preload("Orders").Preload("Profile")
		if len(q.preloads) != 2 {
			t.Errorf("期望 2 个 preload，实际 %d", len(q.preloads))
		}
	})

	t.Run("Preload 空 column 写入 errs", func(t *testing.T) {
		q, _ := NewQuery[TestUser](ctx)
		q.Preload("")
		assertError(t, q.GetError(), true, "Preload 空 column 应写入 errs")
		if len(q.preloads) != 0 {
			t.Errorf("Preload 空 column 不应追加条件，实际 %d", len(q.preloads))
		}
	})
}

// setupTestDB 初始化内存 SQLite 用于 ToDB 测试
func setupToDBTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	return db
}

// TestQuery_ToDB 测试 ToDB 将 Query 条件转换为 GORM DB 对象
func TestQuery_ToDB(t *testing.T) {
	ctx := context.Background()

	t.Run("ToDB 返回非 nil DB", func(t *testing.T) {
		db := setupToDBTestDB(t)
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "alice").Gt(&u.Age, 18)
		result := q.ToDB(db)
		if result == nil {
			t.Fatal("ToDB 返回了 nil")
		}
	})

	t.Run("ToDB 不污染原始 DB", func(t *testing.T) {
		db := setupToDBTestDB(t)
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Name, "bob")
		_ = q.ToDB(db)
		// 原始 db 的 Statement 不应被修改（ToDB 使用了 Session）
		if db.Statement != nil && db.Statement.SQL.Len() > 0 {
			t.Error("ToDB 不应污染原始 DB 的 Statement")
		}
	})

	t.Run("空 Query ToDB 返回干净 DB", func(t *testing.T) {
		db := setupToDBTestDB(t)
		q, _ := NewQuery[TestUser](ctx)
		result := q.ToDB(db)
		if result == nil {
			t.Fatal("空 Query 的 ToDB 不应返回 nil")
		}
	})

	t.Run("ToDB 有错误时将错误注入 DB", func(t *testing.T) {
		db := setupToDBTestDB(t)
		q, _ := NewQuery[TestUser](ctx)
		q.Eq(nil, "bad") // 触发 builder 错误
		result := q.ToDB(db)
		if result.Error == nil {
			t.Error("ToDB 有 builder 错误时，返回的 DB 应携带错误")
		}
	})

	t.Run("ToDB 不继承 dirty db 的已有条件", func(t *testing.T) {
		db := setupToDBTestDB(t)
		// 模拟已有条件的 dirty db（如 db.Where("deleted_at IS NULL")）
		dirty := db.Where("1 = 1")
		q, u := NewQuery[TestUser](ctx)
		q.Eq(&u.Age, 18)
		result := q.ToDB(dirty)
		if result == nil {
			t.Fatal("ToDB 返回了 nil")
		}
		// result 不应携带错误
		if result.Error != nil {
			t.Errorf("ToDB 不应携带错误: %v", result.Error)
		}
	})
}
