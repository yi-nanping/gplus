package gplus

import (
	"context"
	"testing"
)

// invalidDataRuleCtx 返回含非法列名 DataRule 的 context，
// 使 DataRuleBuilder().GetError() 返回错误（而 q.GetError() 不受影响）。
func invalidDataRuleCtx() context.Context {
	rules := []DataRule{{Column: "bad(col)", Condition: "=", Value: "1"}}
	return context.WithValue(context.Background(), DataRuleKey, rules)
}

// builderErrQuery 返回一个已累积 builder 错误的 Query（Eq(nil,...)）。
func builderErrQuery[T any]() *Query[T] {
	q, _ := NewQuery[T](context.Background())
	q.Eq(nil, "x")
	return q
}

func TestRepository_NewQuery_NewUpdater(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	ctx := context.Background()

	t.Run("NewQuery 与 gplus.NewQuery 等价", func(t *testing.T) {
		q, m := repo.NewQuery(ctx)
		if q == nil || m == nil {
			t.Fatal("NewQuery 应返回非 nil 的 Query 和模型指针")
		}
		q.Eq(&m.Name, "Alice")
		if q.GetError() != nil {
			t.Errorf("NewQuery 链式调用不应报错: %v", q.GetError())
		}
	})

	t.Run("NewUpdater 与 gplus.NewUpdater 等价", func(t *testing.T) {
		u, m := repo.NewUpdater(ctx)
		if u == nil || m == nil {
			t.Fatal("NewUpdater 应返回非 nil 的 Updater 和模型指针")
		}
		u.Set(&m.Name, "Bob")
		if u.GetError() != nil {
			t.Errorf("NewUpdater 链式调用不应报错: %v", u.GetError())
		}
	})
}

func TestGetOneTx_BuilderError(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	q := builderErrQuery[TestUser]()
	_, err := repo.GetOne(q)
	if err == nil {
		t.Error("GetOne 有 builder 错误时应返回错误")
	}
}

func TestLastTx_BuilderError(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	q := builderErrQuery[TestUser]()
	_, err := repo.Last(q)
	if err == nil {
		t.Error("Last 有 builder 错误时应返回错误")
	}
}

func TestCountTx_BuilderError(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	q := builderErrQuery[TestUser]()
	_, err := repo.Count(q)
	if err == nil {
		t.Error("Count 有 builder 错误时应返回错误")
	}
}

func TestExistsTx_BuilderError(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	q := builderErrQuery[TestUser]()
	_, err := repo.Exists(q)
	if err == nil {
		t.Error("Exists 有 builder 错误时应返回错误")
	}
}

func TestExistsTx_DataRuleError(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	ctx := invalidDataRuleCtx()
	q, _ := NewQuery[TestUser](ctx)
	_, err := repo.Exists(q)
	if err == nil {
		t.Error("Exists 非法 DataRule 应返回错误")
	}
}

func TestPluckTx_BuilderError(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	q := builderErrQuery[TestUser]()
	m := getModelInstance[TestUser]()
	_, err := Pluck[TestUser, string, int64](repo, q, &m.Name)
	if err == nil {
		t.Error("Pluck 有 builder 错误时应返回错误")
	}
}

func TestPluckTx_DataRuleError(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	ctx := invalidDataRuleCtx()
	q, _ := NewQuery[TestUser](ctx)
	m := getModelInstance[TestUser]()
	_, err := Pluck[TestUser, string, int64](repo, q, &m.Name)
	if err == nil {
		t.Error("Pluck 非法 DataRule 应返回错误")
	}
}

func TestPageTx_BuilderError(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	q := builderErrQuery[TestUser]()
	_, _, err := repo.Page(q, false)
	if err == nil {
		t.Error("Page 有 builder 错误时应返回错误")
	}
}

func TestChunkTx_DataRuleError(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	ctx := invalidDataRuleCtx()
	q, _ := NewQuery[TestUser](ctx)
	err := repo.Chunk(q, 10, func([]TestUser) error { return nil })
	if err == nil {
		t.Error("Chunk 非法 DataRule 应返回错误")
	}
}

func TestIncrByTx_DataRuleError(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	ctx := invalidDataRuleCtx()
	u, m := NewUpdater[TestUser](ctx)
	u.Eq(&m.ID, 1)
	_, err := repo.IncrBy(u, &m.Score, 1)
	if err == nil {
		t.Error("IncrBy 非法 DataRule 应返回错误")
	}
}

func TestRestoreByCondTx_DataRuleError(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	ctx := invalidDataRuleCtx()
	q, m := NewQuery[TestUser](ctx)
	q.Eq(&m.ID, 1)
	_, err := repo.RestoreByCond(q)
	if err == nil {
		t.Error("RestoreByCond 非法 DataRule 应返回错误")
	}
}

func TestAggregate_BuilderError(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	q := builderErrQuery[TestUser]()
	_, err := Sum[TestUser, int64, int64](repo, q, "score")
	if err == nil {
		t.Error("Sum 有 builder 错误时应返回错误")
	}
}

func TestAggregate_InvalidColumn(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	q, _ := NewQuery[TestUser](context.Background())
	// 传入空字符串触发 resolveColumnName 的 ErrColumnEmpty 路径
	_, err := Sum[TestUser, int64, int64](repo, q, "")
	if err == nil {
		t.Error("Sum 传入空列名应返回错误")
	}
}

func TestUpdateByCondTx_DataRuleError(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	ctx := invalidDataRuleCtx()
	u, m := NewUpdater[TestUser](ctx)
	u.Set(&m.Name, "x").Eq(&m.ID, 1)
	_, err := repo.UpdateByCond(u)
	if err == nil {
		t.Error("UpdateByCond 非法 DataRule 应返回错误")
	}
}

func TestToDBName_ConsecutiveUppercase(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// 3 个连续非缩写大写字母：触发中间字母的 lastCase && nextCase 路径
		{"XYZField", "xyz_field"},
		// 大写+大写+数字：触发 lastCase && nextNumber 路径
		{"AB2Field", "ab2_field"},
	}
	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			got := toDBName(c.input)
			if got != c.want {
				t.Errorf("toDBName(%q) = %q, 期望 %q", c.input, got, c.want)
			}
		})
	}
}

func TestApplyPreloads_EmptyQuery(t *testing.T) {
	db := newDryRunDB(t)
	q, _ := NewQuery[TestUser](context.Background())
	// 直接注入空 preload，保留公共接口无法构造的防御分支。
	q.preloads = append(q.preloads, preloadInfo{query: ""})
	result := db.Model(new(TestUser)).Scopes(q.BuildQuery()).Find(&[]TestUser{})
	if result.Error != nil || result.Statement.SQL.Len() == 0 || len(result.Statement.Preloads) != 0 {
		t.Fatalf("empty preload should be skipped: error=%v SQL=%s preloads=%v", result.Error, result.Statement.SQL.String(), result.Statement.Preloads)
	}
	plain, _ := NewQuery[TestUser](context.Background())
	baseline := newDryRunDB(t).Model(new(TestUser)).Scopes(plain.BuildQuery()).Find(&[]TestUser{})
	if baseline.Error != nil || baseline.Statement.SQL.String() != result.Statement.SQL.String() {
		t.Fatalf("空 preload 应与未配置 preload 构建相同 SQL: baseline=%s actual=%s err=%v", baseline.Statement.SQL.String(), result.Statement.SQL.String(), baseline.Error)
	}
}

func TestQuoteColumns_EmptySlice(t *testing.T) {
	result := quoteColumns([]string{}, "`", "`")
	if len(result) != 0 {
		t.Errorf("空切片应直接返回空，实际 %v", result)
	}
	// nil 切片同样触发早返回
	result2 := quoteColumns(nil, "`", "`")
	if len(result2) != 0 {
		t.Errorf("nil 切片应直接返回 nil，实际 %v", result2)
	}
}

func TestLastTx_DataRuleError(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	ctx := invalidDataRuleCtx()
	q, _ := NewQuery[TestUser](ctx)
	_, err := repo.Last(q)
	if err == nil {
		t.Error("Last 非法 DataRule 应返回错误")
	}
}

func TestDeleteByCondTx_BuilderError(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	ctx := context.Background()
	// 先添加有效条件（不触发 IsEmpty），再添加无效 Select 写入 errs
	q, m := NewQuery[TestUser](ctx)
	q.Eq(&m.ID, 1) // 合法条件，conditions 非空
	q.Select(nil)  // 写入 errs 但不清除 conditions
	_, err := repo.DeleteByCond(q)
	if err == nil {
		t.Error("DeleteByCond 有 builder 错误时应返回错误")
	}
}

func TestUpdateByCondTx_BuilderError(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	ctx := context.Background()
	u, m := NewUpdater[TestUser](ctx)
	u.Set(&m.Name, "x") // setMap 非空
	u.Select(nil)       // 写入 errs 但不影响 setMap
	u.Eq(&m.ID, 1)
	_, err := repo.UpdateByCond(u)
	if err == nil {
		t.Error("UpdateByCond 有 builder 错误时应返回错误")
	}
}

func TestFirstOrCreate_DataRuleError(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	ctx := invalidDataRuleCtx()
	q, _ := NewQuery[TestUser](ctx)
	_, _, err := repo.FirstOrCreate(q, &TestUser{Name: "x"})
	if err == nil {
		t.Error("FirstOrCreate 非法 DataRule 应返回错误")
	}
}

func TestFirstOrUpdate_DataRuleError(t *testing.T) {
	repo := NewRepository[int64, TestUser](newDryRunDB(t))
	ctx := invalidDataRuleCtx()
	q, _ := NewQuery[TestUser](ctx)
	u, um := NewUpdater[TestUser](context.Background())
	u.Set(&um.Name, "x")
	_, _, err := repo.FirstOrUpdate(q, u, &TestUser{Name: "x"})
	if err == nil {
		t.Error("FirstOrUpdate 非法 DataRule 应返回错误")
	}
}
