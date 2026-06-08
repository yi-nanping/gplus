package gplus

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// drUser DataRule.Table 测试专用模型（自引用，用于自连接验证）。
type drUser struct {
	ID     int64  `gorm:"column:id;primaryKey;autoIncrement"`
	DeptID uint   `gorm:"column:dept_id"`
	Age    uint   `gorm:"column:age"`
	Name   string `gorm:"column:name"`
}

func (drUser) TableName() string { return "dr_users" }

// drDataRuleSQL 注入 rules 后用 DryRun 生成 SQL（去引号）并返回 q（供取 GetError）。
// 单表 drUser，不执行；用于操作符穿透与负例断言。
func drDataRuleSQL(t *testing.T, rules []DataRule) (*Query[drUser], string) {
	t.Helper()
	db := newDryRunDB(t)
	ctx := context.WithValue(context.Background(), DataRuleKey, rules)
	q, _ := NewQuery[drUser](ctx)
	stmt := db.Session(&gorm.Session{DryRun: true}).
		Model(&drUser{}).
		Scopes(q.DataRuleBuilder().BuildQuery()).
		Find(&[]drUser{}).Statement
	return q, stripIdentQuotes(stmt.SQL.String())
}

// AC-3: Table 非空 + Column 含点 → fail-fast，GetError 含原始 "dept.id"，DryRun WHERE 不含 dept
func TestDataRuleTable_failfast_when_table_set_and_column_has_dot(t *testing.T) {
	rules := []DataRule{{Table: "ext", Column: "dept.id", Condition: "=", Value: "1"}}
	q, sql := drDataRuleSQL(t, rules)
	err := q.GetError()
	if err == nil || !strings.Contains(err.Error(), "dept.id") {
		t.Fatalf("期望 fail-fast 错误含原始 dept.id，实际: %v", err)
	}
	if strings.Contains(sql, "dept") {
		t.Fatalf("fail-fast 后 WHERE 不应含 dept，实际 SQL: %s", sql)
	}
}

// resolveDataRuleColumn 新路径正路：Table:"ext" + 裸列 dept_id → 拼出 ext.dept_id 并进入 WHERE
// 覆盖 2b（Table 单段校验通过）→ 2c（拼接）→ 2d（拼接结果过白名单）三个分支。
func TestDataRuleTable_table_prefix_produces_qualified_column(t *testing.T) {
	q, sql := drDataRuleSQL(t, []DataRule{{Table: "ext", Column: "dept_id", Condition: "=", Value: "1"}})
	if err := q.GetError(); err != nil {
		t.Fatalf("正路不应有错误，实际: %v", err)
	}
	if !strings.Contains(sql, "ext.dept_id") {
		t.Fatalf("WHERE 期望含 ext.dept_id（helper 拼前缀，覆盖 2b/2c/2d），实际 SQL: %s", sql)
	}
}

// AC-4: 注入 payload（table-driven）→ 每个 GetError 非 nil 且条件未拼进 SQL
func TestDataRuleTable_rejects_injection_payloads(t *testing.T) {
	payloads := []struct {
		name  string
		table string
	}{
		{"双引号分号", `ext";DROP--`},
		{"反引号", "ext`alias"},
		{"尾点", "ext."},
		{"首点", ".ext"},
		{"尾空格", "ext "}, // 与 AC-11 呼应，AC-11 单列强调"不 TrimSpace"决策
		{"Tab", "ext\t"},
		{"换行", "ext\n"},
		{"NUL字节", "ext\x00alias"}, // 部分驱动 C 层视 NUL 为字符串终止符，validTableName 正则拒
		{"西里尔同形", "еxt"}, // 首字符 U+0435，非 ASCII [a-zA-Z_]
	}
	for _, p := range payloads {
		t.Run(p.name, func(t *testing.T) {
			q, sql := drDataRuleSQL(t, []DataRule{{Table: p.table, Column: "dept_id", Condition: "=", Value: "1"}})
			if q.GetError() == nil {
				t.Fatalf("payload %q 期望 GetError 非 nil", p.table)
			}
			// 负例断言：非法输入被拒后该 rule 不生成任何条件，SQL 不含列名
			if strings.Contains(sql, "dept_id") {
				t.Fatalf("payload %q 非法但条件被拼进 SQL: %s", p.table, sql)
			}
		})
	}
}

// AC-10: Table 含点（多段 public.users）违反单段约束 → GetError 非 nil
func TestDataRuleTable_rejects_multi_segment_table(t *testing.T) {
	q, sql := drDataRuleSQL(t, []DataRule{{Table: "public.users", Column: "id", Condition: "=", Value: "1"}})
	if q.GetError() == nil {
		t.Fatal("Table=public.users 含点违反单段约束，期望 GetError 非 nil")
	}
	if strings.Contains(sql, "public") {
		t.Fatalf("多段 Table 被拒后不应拼进 SQL: %s", sql)
	}
}

// AC-11: Table 首尾空格 → GetError 非 nil（不做 TrimSpace，validTableName 拒）
// 独立锁定"不 TrimSpace"这一设计决策——与 AC-4 尾空格 case 输入重叠是有意为之，勿当冗余删除。
func TestDataRuleTable_rejects_table_with_trailing_space(t *testing.T) {
	q, _ := drDataRuleSQL(t, []DataRule{{Table: "ext ", Column: "dept_id", Condition: "=", Value: "1"}})
	if q.GetError() == nil {
		t.Fatal(`Table="ext "（含尾空格，不 TrimSpace）期望 GetError 非 nil`)
	}
}

// setupDRDB 建库 + AutoMigrate drUser + 自连接对照种子。
// 种子让"裸 dept_id"与"ext.dept_id"在错位自连接（ext.id=m.id+1）下结果必然不同。
func setupDRDB(t *testing.T) (*Repository[int64, drUser], *gorm.DB) {
	t.Helper()
	repo, db := setupTestDB[drUser](t)
	seeds := []drUser{
		{ID: 1, DeptID: 2, Age: 9, Name: "a"},
		{ID: 2, DeptID: 1, Age: 5, Name: "b"},
		{ID: 3, DeptID: 1, Age: 9, Name: "c"},
	}
	for i := range seeds {
		if err := db.Create(&seeds[i]).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return repo, db
}

// AC-1: 跨表正路（裸列 dept_id + Table:"ext"），ext 侧过滤，且与裸列对照行为不同（防死代码）
func TestDataRuleTable_crosstable_filters_ext_side_and_differs_from_bare(t *testing.T) {
	rule := []DataRule{{Table: "ext", Column: "dept_id", Condition: "=", Value: "1"}}

	// (1) 结构：DryRun WHERE 含 ext.dept_id（裸列经 helper 拼前缀，非旧点前缀 workaround）
	_, sql := drDataRuleSQL(t, rule)
	if !strings.Contains(sql, "ext.dept_id") {
		t.Fatalf("WHERE 期望含 ext.dept_id（helper 拼前缀），实际 SQL: %s", sql)
	}

	// (2) 行为：错位自连接真实执行，ext.dept_id=1 过滤 ext 侧
	// 配对 ext.id=m.id+1 → (m1,ext2)(m2,ext3)；ext2/ext3 dept 均=1 → 2 行
	repo, _ := setupDRDB(t)
	ctxExt := context.WithValue(context.Background(), DataRuleKey, rule)
	qExt, _ := repo.NewQueryAs(ctxExt, "m")
	qExt.CrossJoinAs(As[drUser](qExt, "ext")).WhereRaw("ext.id = m.id + 1")
	extCount, err := repo.Count(qExt)
	if err != nil {
		t.Fatalf("ext 前缀版 Count 失败: %v", err)
	}
	if extCount != 2 {
		t.Fatalf("ext.dept_id=1 期望 2 行（配对 ext2/ext3 均 dept1），实际 %d", extCount)
	}

	// (3) 强制对照（防假绿，不可省）：裸 dept_id 自连接两表同名列 → 行为必不同
	// （SQLite 报 ambiguous，或解析到 m.dept_id 致行数不同）。仅"无错且行数相同"才判死代码。
	repo2, _ := setupDRDB(t)
	ctxBare := context.WithValue(context.Background(), DataRuleKey,
		[]DataRule{{Column: "dept_id", Condition: "=", Value: "1"}})
	qBare, _ := repo2.NewQueryAs(ctxBare, "m")
	qBare.CrossJoinAs(As[drUser](qBare, "ext")).WhereRaw("ext.id = m.id + 1")
	bareCount, bareErr := repo2.Count(qBare)
	if bareErr == nil && bareCount == extCount {
		t.Fatalf("Table:\"ext\" 与裸 dept_id 结果相同(count=%d)，Table 前缀未改变行为=死代码", bareCount)
	}
}

// AC-2: 旧点前缀写法 Table:"" Column:"ext.dept_id" 真实执行，与 AC-1 的 Table:"ext" 等价（零回归）
func TestDataRuleTable_legacy_dotprefix_equivalent_to_table_field(t *testing.T) {
	repo, _ := setupDRDB(t)
	ctx := context.WithValue(context.Background(), DataRuleKey,
		[]DataRule{{Table: "", Column: "ext.dept_id", Condition: "=", Value: "1"}})
	q, _ := repo.NewQueryAs(ctx, "m")
	q.CrossJoinAs(As[drUser](q, "ext")).WhereRaw("ext.id = m.id + 1")
	count, err := repo.Count(q)
	if err != nil {
		t.Fatalf("旧点前缀版 Count 失败: %v", err)
	}
	if count != 2 {
		t.Fatalf("旧 workaround ext.dept_id=1 期望 2 行（与 AC-1 等价），实际 %d", count)
	}
}

// AC-6: 单表裸列 Table:"" Column:"dept_id" 真实执行，WHERE 裸 dept_id 无前缀，结果不变
func TestDataRuleTable_single_table_bare_column_no_regression(t *testing.T) {
	repo, _ := setupDRDB(t)
	ctx := context.WithValue(context.Background(), DataRuleKey,
		[]DataRule{{Table: "", Column: "dept_id", Condition: "=", Value: "1"}})
	q, _ := NewQuery[drUser](ctx)
	count, err := repo.Count(q)
	if err != nil {
		t.Fatalf("单表裸列 Count 失败: %v", err)
	}
	if count != 2 {
		t.Fatalf("单表 dept_id=1 期望 2 行，实际 %d", count)
	}
	_, sql := drDataRuleSQL(t, []DataRule{{Column: "dept_id", Condition: "=", Value: "1"}})
	if !strings.Contains(sql, "dept_id") || strings.Contains(sql, ".dept_id") {
		t.Fatalf("单表应裸 dept_id 无前缀，实际 SQL: %s", sql)
	}
}

// AC-7: 两条 rule（新路径 ext.dept_id IN + 旧点前缀 m.age=）AND 共存，参数绑定不错位
func TestDataRuleTable_multi_rule_new_old_mixed_AND_no_param_corruption(t *testing.T) {
	repo, db := setupTestDB[drUser](t)
	// 种子让两条件唯一同时满足 1 行：仅 (m1 age9, ext2 dept1) 满足 ext.dept∈{1,2} ∧ m.age=9
	seeds := []drUser{
		{ID: 1, DeptID: 2, Age: 9, Name: "a"},
		{ID: 2, DeptID: 1, Age: 5, Name: "b"},
		{ID: 3, DeptID: 1, Age: 9, Name: "c"},
	}
	for i := range seeds {
		if err := db.Create(&seeds[i]).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	rules := []DataRule{
		{Table: "ext", Column: "dept_id", Condition: "IN", Values: []string{"1", "2"}},
		{Table: "", Column: "m.age", Condition: "=", Value: "9"},
	}
	ctx := context.WithValue(context.Background(), DataRuleKey, rules)
	q, _ := repo.NewQueryAs(ctx, "m")
	q.CrossJoinAs(As[drUser](q, "ext")).WhereRaw("ext.id = m.id + 1")
	count, err := repo.Count(q)
	if err != nil {
		t.Fatalf("多 rule Count 失败: %v", err)
	}
	// AND 语义 + 参数不错位：配对 (m1 age9,ext2 dept1) 唯一同时满足 → 1
	if count != 1 {
		t.Fatalf("ext.dept_id IN(1,2) ∧ m.age=9 期望 1 行，实际 %d", count)
	}
	_, sql := drDataRuleSQL(t, rules)
	if !strings.Contains(sql, "ext.dept_id") || !strings.Contains(sql, "m.age") {
		t.Fatalf("WHERE 期望同时含 ext.dept_id 与 m.age，实际: %s", sql)
	}
}

// AC-8: IS NULL + Table → ext.dept_id IS NULL（证明空值 early-return 前已解析 Table 前缀）
func TestDataRuleTable_is_null_carries_table_prefix(t *testing.T) {
	_, sql := drDataRuleSQL(t, []DataRule{{Table: "ext", Column: "dept_id", Condition: "IS NULL"}})
	if !strings.Contains(sql, "ext.dept_id IS NULL") {
		t.Fatalf("IS NULL 应带 ext 前缀（INV-2），实际: %s", sql)
	}
}

// AC-9: BETWEEN + Table + 多值 → ext.age BETWEEN（证明 Table 穿透到非 = 多值分支）
func TestDataRuleTable_between_carries_table_prefix(t *testing.T) {
	_, sql := drDataRuleSQL(t, []DataRule{{Table: "ext", Column: "age", Condition: "BETWEEN", Values: []string{"10", "30"}}})
	if !strings.Contains(sql, "ext.age BETWEEN") {
		t.Fatalf("BETWEEN 应带 ext 前缀，实际: %s", sql)
	}
}
