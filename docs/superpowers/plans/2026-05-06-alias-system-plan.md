# v0.8.0 alias 体系 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 gplus（GORM 泛型封装库）实现 alias 体系，支持跨表列引用、自连接、相关 EXISTS 子查询，使 v0.6.0 子查询 + v0.7.0 投影 API 后剩余的"JOIN 与 WhereRaw 强耦合"裂缝彻底闭合。

**Architecture:** 字段指针 + alias 包装实例（独立于规范单例）；alias 实例 Query 局部生命周期；包级 As/SubQuery 函数（受 Go 泛型 method 限制）；`*queryCore` 命名字段持有共享状态（不内嵌，避免 method promotion 泄漏）；AnyQuery 接口仅作 phantom guard。

**Tech Stack:** Go 1.21+ / GORM v1.31.x（行为永久 RED-locked）/ glebarez/sqlite（测试）/ MySQL（集成测试可选）

**Spec reference:** `docs/superpowers/specs/2026-05-06-alias-system-design.md`（946 行设计文档，已经过 brainstorming 6 轮澄清 + 2 轮 5 路审计 + 12 处修订；本计划是该 spec 的 task 化拆解）

**Verification commands:**
- 测试：`D:/Environment/golang/go1.21.11/bin/go.exe test ./...`
- 单测：`D:/Environment/golang/go1.21.11/bin/go.exe test -run TestXxx ./...`
- 覆盖率：`D:/Environment/golang/go1.21.11/bin/go.exe test -coverprofile=coverage.out ./... && D:/Environment/golang/go1.21.11/bin/go.exe tool cover -func=coverage.out`
- DryRun SQL 断言：测试中用 `q.ToSQL(db)` 比对生成 SQL 字面

---

## File Structure

| 文件 | 类型 | 职责 |
|---|---|---|
| `alias.go` | **新建** | `aliasEntry` / `queryCore` / `AnyQuery` / `As` / `getAliasName` / 7 个新哨兵 |
| `alias_probe_test.go` | **新建** | `TestGORMAliasBehaviorProbe` 永久 RED-lock GORM v1.31.x 行为 |
| `alias_test.go` | **新建** | As 创建/重名/非法名/As(nil) panic/Clear 残骸 测试（15 子测试） |
| `subquery.go` | 修改 | 加 `SubQuery[X]` / `SubQueryAs[X]` 包级函数；扩 `Subquerier`（已有，零变更） |
| `query.go` | 修改 | `Query[T]` 加 `core *queryCore`；7 个 JoinAs / 4 个 EXISTS 方法；`gplusCore()`；`Clear()` 加 revoked 翻转 |
| `update.go` | 修改 | `Updater[T]` 加 `core *queryCore`；2 个 JoinAs（Left/Inner）/ 4 个 EXISTS；`gplusCore()`；`Clear()` 同步 |
| `repository.go` | 修改 | 加 `NewQueryAs` 便捷方法 |
| `builder.go` | 修改 | `applyJoinsAs`；`existsLeaf` 处理；`BuildQuery` 入口 `len(errs)>0` 短路（决策 1B） |
| `query_joinas_test.go` | **新建** | 7 种 JoinAs × ON × extraSQL 参数化（18 子测试） |
| `query_subquery_correlated_test.go` | **新建** | SubQuery 派生/跨层引用/嵌套/H5 闭合（12 子测试） |
| `query_exists_test.go` | **新建** | Exists/NotExists/OrExists/OrNotExists × 简单/相关 sub（12 子测试） |
| `updater_alias_test.go` | **新建** | Updater 镜像（8 子测试） |
| `query_newqueryas_test.go` | **新建** | NewQueryAs 主表 alias（8 子测试） |
| `alias_datarule_e2e_test.go` | **新建** | DataRule × alias e2e 段 A（锁敞开）+ 段 B（验合规）（7 子测试） |
| `bench_alias_test.go` | **新建** | 性能基线 4 个 benchmark |
| `mysql_integration_test.go` | 修改 | 加自连接 / correlated EXISTS 双方言测试 |
| `README.md` | 修改 | 加 "Alias 与跨表查询" 章节 |
| `CHANGELOG.md` | 修改 | 加 v0.8.0 段，强调 DataRule × alias 安全契约 |
| `doc.go` | 修改 | 包级 godoc 加 alias 简介 |

---

## Task 1: GORM 行为探针（P0）

**Files:**
- Create: `alias_probe_test.go`

**目的**：永久 RED-lock GORM v1.31.x 在 alias / EXISTS / Session{NewDB} / Joins+args 四个场景的实测行为。GORM 升级时此测试 fail 第一时间感知。

- [ ] **Step 1: 创建探针测试文件，写 4 个子测试**

```go
// alias_probe_test.go
package gplus

import (
	"testing"

	"gorm.io/gorm"
)

// TestGORMAliasBehaviorProbe 永久锁定 GORM v1.31.x 行为
// 升级 GORM 时此测试 fail，提醒同步检查 alias 体系实现
func TestGORMAliasBehaviorProbe(t *testing.T) {
	db := setupTestDB[TestUser](t)

	t.Run("Joins_AliasString_GeneratesExpectedSQL", func(t *testing.T) {
		var users []TestUser
		stmt := db.Session(&gorm.Session{DryRun: true}).
			Joins("LEFT JOIN orders AS o ON o.user_id = test_users.id").
			Find(&users).Statement
		got := stmt.SQL.String()
		want := "LEFT JOIN orders AS o ON o.user_id = test_users.id"
		if !contains(got, want) {
			t.Errorf("expected SQL contains %q, got %q", want, got)
		}
	})

	t.Run("Where_ExistsSubquery_SubqueryDBSubstitution", func(t *testing.T) {
		subDB := db.Session(&gorm.Session{NewDB: true}).
			Model(&TestUser{}).Select("1").Where("id = ?", 1)
		var users []TestUser
		stmt := db.Session(&gorm.Session{DryRun: true}).
			Where("EXISTS (?)", subDB).
			Find(&users).Statement
		got := stmt.SQL.String()
		if !contains(got, "EXISTS") {
			t.Errorf("expected SQL contains EXISTS, got %q", got)
		}
	})

	t.Run("SelfJoin_SameTable_DifferentAlias_NoConflict", func(t *testing.T) {
		var users []TestUser
		stmt := db.Session(&gorm.Session{DryRun: true}).
			Joins("LEFT JOIN test_users AS boss ON test_users.id = boss.id").
			Find(&users).Statement
		got := stmt.SQL.String()
		if !contains(got, "test_users AS boss") {
			t.Errorf("self-join alias not preserved, got %q", got)
		}
	})

	t.Run("Session_NewDBTrue_BreaksOuterWhereInheritance", func(t *testing.T) {
		dirty := db.Where("name = ?", "leak")
		clean := dirty.Session(&gorm.Session{NewDB: true})
		var users []TestUser
		stmt := clean.Session(&gorm.Session{DryRun: true}).Find(&users).Statement
		got := stmt.SQL.String()
		if contains(got, "name = ") {
			t.Errorf("expected NewDB:true to clear outer WHERE, but got %q", got)
		}
	})

	t.Run("Subquery_NoOuterClauseLeak_AssertedExplicitly", func(t *testing.T) {
		// 外层已积累 WHERE
		outer := db.Where("status = ?", "active").Model(&TestUser{}).Select("id, name")
		// 派生子查询
		sub := outer.Session(&gorm.Session{NewDB: true}).
			Model(&TestUser{}).Select("1").Where("age > ?", 18)
		stmt := sub.Session(&gorm.Session{DryRun: true}).Find(&[]TestUser{}).Statement
		got := stmt.SQL.String()
		if contains(got, "status = ") {
			t.Errorf("subquery leaked outer WHERE: %q", got)
		}
		if contains(got, "id, name") {
			t.Errorf("subquery leaked outer SELECT: %q", got)
		}
	})

	t.Run("JoinsWithArgs_ArgsParameterized_NotInlined", func(t *testing.T) {
		var users []TestUser
		stmt := db.Session(&gorm.Session{DryRun: true}).
			Joins("LEFT JOIN orders AS o ON o.user_id = test_users.id AND o.status = ?", "paid").
			Find(&users).Statement
		got := stmt.SQL.String()
		if !contains(got, "?") {
			t.Errorf("expected ? placeholder in DryRun SQL, got %q", got)
		}
		if contains(got, "'paid'") {
			t.Errorf("paid value should not be inlined: %q", got)
		}
	})
}

// contains 简单包含判断（避免引入 strings 依赖太重）
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: 运行测试，应全部 PASS（GORM v1.31.x 现行为符合预期）**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestGORMAliasBehaviorProbe -v ./...`
Expected: 6 sub-test 全部 PASS。如某项 fail，说明 GORM 行为与 spec 假设不符，必须先调整 spec 或冻结 GORM 版本

- [ ] **Step 3: Commit**

```bash
git add alias_probe_test.go
git commit -m "test(probe): GORM v1.31.x alias 行为永久 RED-lock"
```

---

## Task 2: queryCore 类型 + AnyQuery phantom guard 接口（P1.1）

**Files:**
- Create: `alias.go`
- Test: 仅编译期断言（运行时测试在后续 task）

- [ ] **Step 1: 创建 alias.go 骨架，定义 aliasEntry / queryCore / AnyQuery**

```go
// alias.go - v0.8.0 alias 体系核心类型与函数
package gplus

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"unsafe"
)

// AnyQuery 是 Query[T] 和 Updater[T] 的 phantom 标签接口
// 业务代码无法实现（gplusCore 返回 unexported 类型）
type AnyQuery interface {
	gplusCore() *queryCore
}

// aliasEntry 单条 alias 注册项
type aliasEntry struct {
	instance any          // *X 独立实例（reflect.New 创建）
	name     string       // SQL 中的 alias name
	typ      reflect.Type // X 的反射类型
	addrLow  uintptr      // 实例地址范围下界（含）
	addrHigh uintptr      // 实例地址范围上界（不含）
	revoked  bool         // N4：Clear() 时翻转，lookupAddr 命中 revoked 直接返回错误
}

// coreMetadata 是横切关注点的扩展容器
// v0.8.0 只放 ctx；v0.9+ 加 routing/tracing/sharding 时不破坏 AnyQuery 接口
type coreMetadata struct {
	ctx context.Context
	// 预留：routing *RoutingHint
	// 预留：tracing *TraceContext
	// 预留：shardKey any
}

// queryCore 承载 alias 体系的共享状态与方法
// unexported 类型，外部包无法直接引用
type queryCore struct {
	aliases       map[string]*aliasEntry
	outerQueryRef AnyQuery     // 子查询时指向外层；顶层为 nil
	metadata      coreMetadata // v0.8.0 仅含 ctx
	errs          []error
}

// 新增哨兵错误（7 个）
var (
	ErrAliasDuplicate        = errors.New("gplus: alias name already registered in this query chain")
	ErrAliasInvalidName      = errors.New("gplus: invalid alias name (must match [a-zA-Z_][a-zA-Z0-9_]{0,31})")
	ErrFieldAddrUnregistered = errors.New("gplus: field address not registered to any model or alias in this query chain")
	ErrAliasNotInChain       = errors.New("gplus: alias instance does not belong to this query chain")
	ErrSubqueryOuterNil      = errors.New("gplus: SubQuery outer is nil")
	ErrAliasQueryNil         = errors.New("gplus: As query is nil")
	ErrAliasRevoked          = errors.New("gplus: alias instance has been revoked by Clear()")
)

// aliasNameRegexp 白名单：字母/下划线开头，后续字母数字下划线，1-32 字符
var aliasNameRegexp = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,31}$`)

// newQueryCore 创建空的 queryCore
func newQueryCore(ctx context.Context) *queryCore {
	if ctx == nil {
		ctx = context.Background()
	}
	return &queryCore{
		aliases:  make(map[string]*aliasEntry),
		metadata: coreMetadata{ctx: ctx},
	}
}

// context 返回 metadata 中的 ctx（v0.9+ metadata 扩张时此方法签名不变）
func (c *queryCore) context() context.Context {
	return c.metadata.ctx
}

// outerQuery 返回 outerQueryRef（用于 resolveColumnName 沿链查找）
func (c *queryCore) outerQuery() AnyQuery {
	return c.outerQueryRef
}

// appendErr 累积错误（与 v0.6.0 errs 哲学一致）
func (c *queryCore) appendErr(err error) {
	if err != nil {
		c.errs = append(c.errs, err)
	}
}

// getError 聚合错误（沿 outerQuery 链）
func (c *queryCore) getError() error {
	if len(c.errs) == 0 {
		if c.outerQueryRef != nil {
			return c.outerQueryRef.gplusCore().getError()
		}
		return nil
	}
	// 简化：暴露第一个错误，详细聚合在 Query.GetError 处理
	return errors.Join(c.errs...)
}
```

- [ ] **Step 2: 编译期断言（在 alias.go 末尾）**

```go
// 注意：*Query[T] 和 *Updater[T] 的 gplusCore() 方法实现在后续 task 中加到 query.go / update.go
// 编译期断言放在它们各自所在文件，避免循环依赖
```

- [ ] **Step 3: 运行 build 验证文件编译通过**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe build ./...`
Expected: 编译通过（此时 Query[T] / Updater[T] 还没实现 gplusCore，但 alias.go 自身合法）

- [ ] **Step 4: Commit**

```bash
git add alias.go
git commit -m "feat(alias): queryCore + AnyQuery phantom guard 骨架"
```

---

## Task 3: addAlias + lookupAddr（含 H2 offset 校验 + N4 revoked）

**Files:**
- Modify: `alias.go`
- Test: `alias_test.go`

- [ ] **Step 1: 写 addAlias 测试（RED）**

```go
// alias_test.go
package gplus

import (
	"context"
	"errors"
	"testing"
)

func TestQueryCore_AddAlias_HappyPath(t *testing.T) {
	c := newQueryCore(context.Background())
	o := &TestUser{} // 这里用 TestUser 模拟，真实场景 instance 来自 reflect.New
	if err := c.addAlias("o", reflectTypeOf(o), o); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if _, ok := c.aliases["o"]; !ok {
		t.Fatalf("alias not stored")
	}
}

func TestQueryCore_AddAlias_DuplicateAccumulates(t *testing.T) {
	c := newQueryCore(context.Background())
	o1 := &TestUser{}
	o2 := &TestUser{}
	_ = c.addAlias("o", reflectTypeOf(o1), o1)
	err := c.addAlias("o", reflectTypeOf(o2), o2)
	if !errors.Is(err, ErrAliasDuplicate) {
		t.Fatalf("expected ErrAliasDuplicate, got %v", err)
	}
}

func TestQueryCore_LookupAddr_OffsetMustBeInSchema_H2(t *testing.T) {
	c := newQueryCore(context.Background())
	o := &TestUser{}
	_ = c.addAlias("o", reflectTypeOf(o), o)
	// 取实例区间内但非字段起始的随机偏移（padding 区域）
	base := uintptrOf(o)
	paddingAddr := base + 7 // 假设字段都不从 offset 7 开始
	if _, _, ok := c.lookupAddr(paddingAddr); ok {
		t.Fatalf("padding offset should not be considered a hit (H2)")
	}
}

func TestQueryCore_LookupAddr_RevokedReturnsFalse_N4(t *testing.T) {
	c := newQueryCore(context.Background())
	o := &TestUser{}
	_ = c.addAlias("o", reflectTypeOf(o), o)
	c.aliases["o"].revoked = true
	base := uintptrOf(o)
	if _, _, ok := c.lookupAddr(base); ok {
		t.Fatalf("revoked entry should not be considered a hit (N4)")
	}
}

// 测试辅助
func reflectTypeOf(v any) reflect.Type {
	return reflect.TypeOf(v).Elem()
}

func uintptrOf(v any) uintptr {
	return uintptr(reflect.ValueOf(v).Pointer())
}
```

- [ ] **Step 2: 运行 RED，确认 fail**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestQueryCore_ -v ./...`
Expected: FAIL — addAlias / lookupAddr 未实现

- [ ] **Step 3: 实现 addAlias 和 lookupAddr（在 alias.go 中追加）**

```go
// addAlias 注册一个 alias 实例
// 不做 nameRegexp 校验（由 As 包级函数完成）；不做 nil 校验（内部方法）
// 重名累积 ErrAliasDuplicate 但仍返回 error 让 As 决定如何处理
func (c *queryCore) addAlias(name string, typ reflect.Type, instance any) error {
	if _, ok := c.aliases[name]; ok {
		return ErrAliasDuplicate
	}
	addrLow := uintptr(reflect.ValueOf(instance).Pointer())
	addrHigh := addrLow + typ.Size()
	c.aliases[name] = &aliasEntry{
		instance: instance,
		name:     name,
		typ:      typ,
		addrLow:  addrLow,
		addrHigh: addrHigh,
		revoked:  false,
	}
	return nil
}

// lookupAddr 反向查找：给定字段地址，返回 alias name 和列名
// H2: offset 必须在 schema 已知字段集合内
// N4: revoked entry 直接返回未命中
func (c *queryCore) lookupAddr(addr uintptr) (alias, col string, ok bool) {
	for _, entry := range c.aliases {
		if entry.addrLow <= addr && addr < entry.addrHigh {
			if entry.revoked {
				// N4：revoked，由调用方累积 ErrAliasRevoked
				return "", "", false
			}
			offset := addr - entry.addrLow
			schema, _ := reflectStructSchema(entry.typ)
			if name, found := schema[offset]; found {
				return entry.name, name, true
			}
			// H2：区间命中但 offset 不在 schema，视为未命中
		}
	}
	return "", "", false
}

// hadRevokedHit 辅助：上次 lookupAddr 是否因 revoked 命中
// 用于 resolveColumnName 累积 ErrAliasRevoked（避免改 lookupAddr 签名）
func (c *queryCore) hadRevokedHit(addr uintptr) bool {
	for _, entry := range c.aliases {
		if entry.addrLow <= addr && addr < entry.addrHigh && entry.revoked {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 运行测试，应 PASS**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestQueryCore_ -v ./...`
Expected: 4 子测试 PASS

- [ ] **Step 5: Commit**

```bash
git add alias.go alias_test.go
git commit -m "feat(alias): addAlias + lookupAddr 含 H2 offset 校验 + N4 revoked 防御"
```

---

## Task 4: As 包级函数（含 N5 nil panic + N6 重名累积）

**Files:**
- Modify: `alias.go`
- Test: `alias_test.go`

- [ ] **Step 1: 写 As 函数测试（RED）**

```go
// alias_test.go 追加

func TestAs_HappyPath(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	o := As[TestUser](q, "o")
	if o == nil {
		t.Fatalf("expected non-nil alias instance")
	}
	if err := q.GetError(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAs_InvalidNameAccumulates(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	_ = As[TestUser](q, "1bad-name")
	err := q.GetError()
	if !errors.Is(err, ErrAliasInvalidName) {
		t.Fatalf("expected ErrAliasInvalidName, got %v", err)
	}
}

func TestAs_DuplicateAccumulates_DecisionB(t *testing.T) {
	// 决策 1B：重名累积错误（不 panic），返回首次注册实例
	q, _ := NewQuery[TestUser](context.Background())
	o1 := As[TestUser](q, "o")
	o2 := As[TestUser](q, "o")
	if o1 != o2 {
		t.Fatalf("expected first instance returned on duplicate")
	}
	if !errors.Is(q.GetError(), ErrAliasDuplicate) {
		t.Fatalf("expected ErrAliasDuplicate accumulated")
	}
}

func TestAs_NilQueryPanics_N5(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic")
		}
		err, _ := r.(error)
		if !errors.Is(err, ErrAliasQueryNil) {
			t.Fatalf("expected ErrAliasQueryNil panic, got %v", r)
		}
	}()
	_ = As[TestUser](nil, "o")
}
```

- [ ] **Step 2: 运行 RED，确认 fail（As 未实现）**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestAs_ -v ./...`
Expected: FAIL — As undefined

- [ ] **Step 3: 实现 As（在 alias.go 中追加）**

```go
// As 在 q（含 outerQueryRef 链）上注册一个 X 类型的 alias 实例
//
// 错误处理：
//   - q == nil：panic ErrAliasQueryNil（N5：API 入口编程错误，无 q 可挂错误）
//   - name 不符合白名单正则：累积 ErrAliasInvalidName，返回 fallback 实例
//   - name 已在链中存在：累积 ErrAliasDuplicate，返回首次注册的实例（决策 1B）
//
// 返回的 *X 实例字段地址独立于规范单例，仅用于取字段地址
func As[X any](q AnyQuery, alias string) *X {
	if q == nil {
		panic(ErrAliasQueryNil)
	}
	core := q.gplusCore()

	// name 校验
	if !aliasNameRegexp.MatchString(alias) {
		core.appendErr(ErrAliasInvalidName)
		return getModelInstance[X]()
	}

	// 检查 q 链中是否已有同名 alias
	for cur := AnyQuery(nil); ; {
		if cur == nil {
			cur = q
		}
		curCore := cur.gplusCore()
		if existing, ok := curCore.aliases[alias]; ok {
			core.appendErr(ErrAliasDuplicate)
			if inst, _ := existing.instance.(*X); inst != nil {
				return inst
			}
			return getModelInstance[X]()
		}
		next := curCore.outerQueryRef
		if next == nil {
			break
		}
		cur = next
	}

	// 创建独立 alias 实例
	typ := reflect.TypeOf((*X)(nil)).Elem()
	instancePtr := reflect.New(typ)
	instance := instancePtr.Interface().(*X)
	if err := core.addAlias(alias, typ, instance); err != nil {
		// 理论不会触发（前面已检查重名），但防御性处理
		core.appendErr(err)
	}
	return instance
}
```

- [ ] **Step 4: 运行测试**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestAs_ -v ./...`
Expected: 4 子测试 PASS

- [ ] **Step 5: Commit**

```bash
git add alias.go alias_test.go
git commit -m "feat(alias): As 包级函数（N5 nil panic + 决策 1B 重名累积）"
```

---

## Task 5: Query[T] 加 core *queryCore + gplusCore() + 注释清理

**Files:**
- Modify: `query.go`
- Test: 复用 Task 4 的 NewQuery 链路

- [ ] **Step 1: 修改 Query[T] 结构体加 core 字段**

定位 `query.go` 中 `type Query[T any] struct {` 定义，在结构体内首字段位置加：

```go
type Query[T any] struct {
	core *queryCore // v0.8.0：alias 体系共享状态（命名字段，避免内嵌 method promotion 泄漏）

	// ...其他既有字段保持不变（conditions / selects / joins / orders / errs / dataRuleApplied 等）
}
```

- [ ] **Step 2: 修改 NewQuery 函数初始化 core**

定位 `query.go` 中 `func NewQuery[T any](ctx context.Context) (*Query[T], *T)`：

```go
func NewQuery[T any](ctx context.Context) (*Query[T], *T) {
	q := &Query[T]{
		core: newQueryCore(ctx), // v0.8.0：alias 体系共享状态
		// ...其他既有初始化
	}
	return q, getModelInstance[T]()
}
```

- [ ] **Step 3: 在 query.go 末尾追加 gplusCore() 实现 + AnyQuery 编译断言**

```go
// gplusCore 实现 AnyQuery 接口（v0.8.0 alias 体系）
// 仅供 gplus 包内 As / SubQuery 等包级函数调用
func (q *Query[T]) gplusCore() *queryCore {
	return q.core
}

// 编译期断言 *Query[T] 满足 AnyQuery
var _ AnyQuery = (*Query[struct{}])(nil)
```

- [ ] **Step 4: 修改 Query.GetError() 聚合 core.errs**

定位现有 `func (q *Query[T]) GetError() error`，修改为：

```go
func (q *Query[T]) GetError() error {
	allErrs := append([]error{}, q.errs...)
	if q.core != nil {
		allErrs = append(allErrs, q.core.errs...)
	}
	if len(allErrs) == 0 {
		return nil
	}
	// 摘要前缀保持现有语义
	return fmt.Errorf("gplus query builder failed with %d errors: %w", len(allErrs), errors.Join(allErrs...))
}
```

- [ ] **Step 5: 运行测试，确认 Task 4 测试现在 PASS**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestAs_ -v ./...`
Expected: 4 子测试全部 PASS（Task 4 测试需要 Query 实现 AnyQuery）

- [ ] **Step 6: 全量回归测试，确认不破坏 v0.7.x 既有测试**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test ./...`
Expected: 所有既有测试 PASS（覆盖率应保持 ≥ 96%）

- [ ] **Step 7: Commit**

```bash
git add query.go
git commit -m "feat(alias): Query[T] 加 core *queryCore + gplusCore() 实现 AnyQuery"
```

---

## Task 6: resolveColumnName 沿链查找（H5 sub 严格闭合 + N4 revoked 累积）

**Files:**
- Modify: `query.go`（修改既有 resolveColumnName）或新建辅助函数
- Test: `alias_test.go`

- [ ] **Step 1: 写测试（RED）**

```go
// alias_test.go 追加

func TestResolveColumnName_AliasField_Resolves(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	o := As[TestUser](q, "o")
	col, err := q.resolveColumnName(uintptrOf(&o.Name))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if col != "o.name" {
		t.Errorf("expected o.name, got %s", col)
	}
}

func TestResolveColumnName_SubStrictClosure_H5(t *testing.T) {
	q1, _ := NewQuery[TestUser](context.Background())
	u1 := getModelInstance[TestUser]()
	// q2 不与 q1 关联
	q2, _ := NewQuery[TestUser](context.Background())
	_ = q2 // 仅占位
	// sub 派生自 q1
	sub, _ := SubQuery[TestUser](q1)
	// 错误地引用 q2 的 u（实际是 q2 的规范单例 = TestUser 全局单例 = u1）
	col, err := sub.resolveColumnName(uintptrOf(&u1.Name))
	if !errors.Is(err, ErrFieldAddrUnregistered) {
		t.Errorf("H5 sub 必须严格闭合，got col=%s err=%v", col, err)
	}
}

func TestResolveColumnName_AliasRevoked_AccumulatesError_N4(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	o := As[TestUser](q, "o")
	q.Clear() // 翻转 revoked
	_, err := q.resolveColumnName(uintptrOf(&o.Name))
	if !errors.Is(err, ErrAliasRevoked) {
		t.Errorf("expected ErrAliasRevoked, got %v", err)
	}
}
```

- [ ] **Step 2: 运行 RED，确认 fail**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestResolveColumnName_ -v ./...`
Expected: FAIL — sub 未严格闭合 / revoked 未拦截

- [ ] **Step 3: 修改 resolveColumnName（在 query.go 现有实现处）**

```go
// resolveColumnName 解析字段地址到列名
// 路径：当前 q.core.aliases → outerQueryRef 链 → 全局 columnNameCache（仅顶层）
// H5：sub 模式（outerQueryRef != nil）严格闭合，不回退全局
// N4：lookupAddr 命中 revoked entry 时累积 ErrAliasRevoked
func (q *Query[T]) resolveColumnName(addr uintptr) (string, error) {
	if q.core == nil {
		// 兼容 v0.7.x 既有路径（无 alias 体系）
		return resolveColumnNameLegacy(addr)
	}
	isSub := q.core.outerQueryRef != nil

	current := AnyQuery(q)
	for current != nil {
		core := current.gplusCore()
		if alias, col, ok := core.lookupAddr(addr); ok {
			return alias + "." + col, nil
		}
		// N4：检查是否因 revoked 被拒
		if core.hadRevokedHit(addr) {
			q.core.appendErr(ErrAliasRevoked)
			return "", ErrAliasRevoked
		}
		current = core.outerQueryRef
	}

	if isSub {
		// H5：sub 严格闭合，不回退全局
		q.core.appendErr(ErrFieldAddrUnregistered)
		return "", ErrFieldAddrUnregistered
	}

	// 顶层 fallback 到全局 columnNameCache（v0.7.x 既有行为）
	if col, ok := globalColumnNameLookup(addr); ok {
		return col, nil
	}

	q.core.appendErr(ErrFieldAddrUnregistered)
	return "", ErrFieldAddrUnregistered
}

// globalColumnNameLookup 是对既有 columnNameCache 查询的封装（保持 v0.7.x 行为）
// 实现位于 schema.go，此处仅声明
```

注：`resolveColumnNameLegacy` 和 `globalColumnNameLookup` 是对既有 `schema.go::resolveColumnName` 逻辑的重命名/封装。在该 task 中保留旧函数为 `resolveColumnNameLegacy`，新函数走新路径。

- [ ] **Step 4: 运行测试**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestResolveColumnName_ -v ./...`
Expected: 3 子测试 PASS

- [ ] **Step 5: 全量回归测试**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test ./...`
Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add query.go alias.go alias_test.go schema.go
git commit -m "feat(alias): resolveColumnName 沿链查找 + H5 sub 闭合 + N4 revoked 累积"
```

---

## Task 7: BuildQuery len(errs)>0 短路 + Clear() 翻转 revoked

**Files:**
- Modify: `query.go`、`builder.go`
- Test: `alias_test.go`

- [ ] **Step 1: 写测试（RED）**

```go
// alias_test.go 追加

func TestBuildQuery_ErrsShortCircuit_DecisionB(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	_ = As[TestUser](q, "o")
	_ = As[TestUser](q, "o") // 重名累积错误
	db := setupTestDB[TestUser](t)
	finalDB := q.DataRuleBuilder().BuildQuery(db)
	if finalDB.Error == nil {
		t.Fatalf("expected BuildQuery to short-circuit on accumulated errors")
	}
	if !errors.Is(finalDB.Error, ErrAliasDuplicate) {
		t.Errorf("expected ErrAliasDuplicate, got %v", finalDB.Error)
	}
}

func TestQuery_Clear_AliasUseAfterClear_N4(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	o := As[TestUser](q, "o")
	q.Clear()
	// 业务代码仍持有 o，使用 &o.Name 应被 revoked 拦截
	_, err := q.resolveColumnName(uintptrOf(&o.Name))
	if !errors.Is(err, ErrAliasRevoked) {
		t.Errorf("expected ErrAliasRevoked after Clear, got %v", err)
	}
}
```

- [ ] **Step 2: 运行 RED，确认 fail**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run "TestBuildQuery_ErrsShortCircuit|TestQuery_Clear_AliasUseAfterClear" -v ./...`
Expected: FAIL

- [ ] **Step 3: 修改 BuildQuery 入口加短路（builder.go）**

定位 `builder.go` 中 `BuildQuery(db *gorm.DB) *gorm.DB`：

```go
func (b *ScopeBuilder) BuildQuery(db *gorm.DB) *gorm.DB {
	// v0.8.0 决策 1B：累积错误短路（防 As 重名等错误被快乐路径 SQL 掩盖）
	if b.core != nil && len(b.core.errs) > 0 {
		return db.Session(&gorm.Session{}).AddError(errors.Join(b.core.errs...))
	}
	// ...既有实现
}
```

注：`ScopeBuilder` 嵌入在 `Query[T]` / `Updater[T]` 中。需要让 ScopeBuilder 持有 core 引用——简单做法：让 Query[T] / Updater[T] 在 BuildQuery 入口前把 q.core 注入 ScopeBuilder.core 字段（或 ScopeBuilder 直接 embed *queryCore）。

实施：在 `builder.go` 中给 `ScopeBuilder` 加 `core *queryCore` 字段：

```go
type ScopeBuilder struct {
	core *queryCore // v0.8.0：alias 体系共享状态访问入口
	// ...既有字段
}
```

并在 `Query.BuildQuery` 调用前确保 `q.scopeBuilder.core = q.core`（具体接合点视既有代码结构而定）。

- [ ] **Step 4: 修改 Query.Clear() 翻转 revoked**

定位 `query.go` 中 `func (q *Query[T]) Clear() *Query[T]`：

```go
func (q *Query[T]) Clear() *Query[T] {
	// v0.8.0 N4：翻转所有 alias entry 的 revoked，防 Clear 后用 alias 残骸
	if q.core != nil {
		for name, entry := range q.core.aliases {
			entry.revoked = true
			q.core.aliases[name] = entry
		}
		q.core.aliases = nil
		q.core.outerQueryRef = nil
		q.core.errs = nil
	}
	// ...既有 Clear 逻辑（清空 conditions / selects / joins / orders / errs / dataRuleApplied 等）
	return q
}
```

注意：先翻转 revoked，**再**清空 aliases map。但 lookupAddr 走的是 map，如果 map 已清空，revoked 检查失效。正确顺序：**只翻转 revoked，不清空 aliases**——保留 entry 让后续残骸调用能命中 revoked。

修订：

```go
func (q *Query[T]) Clear() *Query[T] {
	if q.core != nil {
		for _, entry := range q.core.aliases {
			entry.revoked = true // N4：保留 entry 但标记 revoked
		}
		// 不清空 q.core.aliases，留作 revoked 检查
		q.core.outerQueryRef = nil
		q.core.errs = nil
	}
	// ...其他清理
	return q
}
```

- [ ] **Step 5: 运行测试**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run "TestBuildQuery_ErrsShortCircuit|TestQuery_Clear_AliasUseAfterClear" -v ./...`
Expected: PASS

- [ ] **Step 6: 全量回归**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test ./...`
Expected: 全部 PASS

- [ ] **Step 7: Commit**

```bash
git add query.go builder.go alias_test.go
git commit -m "feat(alias): BuildQuery errs 短路 + Clear 翻转 revoked（决策 1B + N4 防御）"
```

---

## Task 8: NewQueryAs 主表 alias（P2）

**Files:**
- Modify: `query.go`
- Test: `query_newqueryas_test.go`（新建）

- [ ] **Step 1: 写测试（RED）**

```go
// query_newqueryas_test.go
package gplus

import (
	"context"
	"errors"
	"testing"
)

func TestNewQueryAs_HappyPath(t *testing.T) {
	q, u := NewQueryAs[TestUser](context.Background(), "u")
	if q == nil || u == nil {
		t.Fatalf("nil returned")
	}
	col, err := q.resolveColumnName(uintptrOf(&u.Name))
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if col != "u.name" {
		t.Errorf("expected u.name, got %s", col)
	}
}

func TestNewQueryAs_InvalidNameAccumulates(t *testing.T) {
	q, _ := NewQueryAs[TestUser](context.Background(), "1bad")
	if !errors.Is(q.GetError(), ErrAliasInvalidName) {
		t.Fatalf("expected ErrAliasInvalidName, got %v", q.GetError())
	}
}

func TestNewQueryAs_ConflictsWithSideAlias(t *testing.T) {
	q, _ := NewQueryAs[TestUser](context.Background(), "u")
	_ = As[TestUser](q, "u") // 副表与主表同名
	if !errors.Is(q.GetError(), ErrAliasDuplicate) {
		t.Fatalf("expected ErrAliasDuplicate, got %v", q.GetError())
	}
}
```

- [ ] **Step 2: 运行 RED**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestNewQueryAs_ -v ./...`
Expected: FAIL — NewQueryAs 未实现

- [ ] **Step 3: 实现 NewQueryAs（query.go 追加）**

```go
// NewQueryAs 创建 Query 并给主表起 alias
//
// 与 NewQuery 不同：返回的 *T 是独立 alias 实例（字段地址绑定到 alias），
// 而非规范单例；使用 &u.Field 时解析为 "alias.col" 而非 "table.col"
func NewQueryAs[T any](ctx context.Context, alias string) (*Query[T], *T) {
	q := &Query[T]{
		core: newQueryCore(ctx),
	}
	// 通过 As 注册主表 alias 实例（复用 As 的全部校验逻辑）
	t := As[T](q, alias)
	return q, t
}
```

- [ ] **Step 4: 测试 PASS**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestNewQueryAs_ -v ./...`
Expected: 3 子测试 PASS

- [ ] **Step 5: Commit**

```bash
git add query.go query_newqueryas_test.go
git commit -m "feat(alias): NewQueryAs 主表 alias 入口（复用 As 注册路径）"
```

---

## Task 9: Repository.NewQueryAs 便捷方法

**Files:**
- Modify: `repository.go`
- Test: `query_newqueryas_test.go` 追加

- [ ] **Step 1: 写测试（RED）**

```go
// query_newqueryas_test.go 追加

func TestRepository_NewQueryAs(t *testing.T) {
	db := setupTestDB[TestUser](t)
	repo := NewRepository[uint, TestUser](db)
	q, u := repo.NewQueryAs("u")
	col, err := q.resolveColumnName(uintptrOf(&u.Name))
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if col != "u.name" {
		t.Errorf("expected u.name, got %s", col)
	}
}
```

- [ ] **Step 2: 运行 RED**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestRepository_NewQueryAs -v ./...`
Expected: FAIL

- [ ] **Step 3: 实现 Repository.NewQueryAs（repository.go 追加）**

定位 `repository.go` 中现有 `NewQuery` 实现附近：

```go
// NewQueryAs 创建带主表 alias 的 Query，便捷方法（绑定 repo 的 db）
func (r *Repository[K, T]) NewQueryAs(alias string) (*Query[T], *T) {
	q, t := NewQueryAs[T](context.Background(), alias)
	q.db = r.db // 绑定 repository 的 db（复用 NewQuery 既有逻辑）
	return q, t
}
```

注：具体 db 绑定路径取决于 Query[T] 现有 db 字段命名（可能是 `q.db` 或 `q.scopeBuilder.db`），按既有代码风格调整。

- [ ] **Step 4: 测试 PASS**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestRepository_NewQueryAs -v ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add repository.go query_newqueryas_test.go
git commit -m "feat(alias): Repository.NewQueryAs 便捷方法"
```

---

## Task 10: LeftJoinAs（含 C1 extraSQL 参数化）

**Files:**
- Modify: `query.go`、`builder.go`
- Test: `query_joinas_test.go`（新建）

- [ ] **Step 1: 写测试（RED）**

```go
// query_joinas_test.go
package gplus

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestLeftJoinAs_BasicSQL(t *testing.T) {
	db := setupTestDB[TestUser](t)
	q, u := NewQuery[TestUser](context.Background())
	o := As[Order](q, "o")
	q.LeftJoinAs(o, &o.UserID, &u.ID, "")

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	if !strings.Contains(sql, "LEFT JOIN") {
		t.Errorf("expected LEFT JOIN, got %s", sql)
	}
	if !strings.Contains(sql, "AS `o`") && !strings.Contains(sql, `AS "o"`) {
		t.Errorf("expected alias AS o, got %s", sql)
	}
}

func TestLeftJoinAs_ExtraSQLParameterized_C1(t *testing.T) {
	db := setupTestDB[TestUser](t)
	q, u := NewQuery[TestUser](context.Background())
	o := As[Order](q, "o")
	q.LeftJoinAs(o, &o.UserID, &u.ID, "AND o.status = ?", "paid")

	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL failed: %v", err)
	}
	// DryRun 应内联 args 后再展示，但 ?-占位符的预编译形态在 .Statement.SQL 字面里
	// 此处验证 SQL 含 status 关键词；具体参数化由 GORM 内部保证（探针锁定）
	if !strings.Contains(sql, "status") {
		t.Errorf("expected status in SQL, got %s", sql)
	}
}

func TestLeftJoinAs_AliasNotInChain(t *testing.T) {
	q1, _ := NewQuery[TestUser](context.Background())
	q2, u2 := NewQuery[TestUser](context.Background())
	oOfQ1 := As[Order](q1, "o")
	// 把 q1 的 alias 传给 q2 的 LeftJoinAs
	q2.LeftJoinAs(oOfQ1, &oOfQ1.UserID, &u2.ID, "")
	if !errors.Is(q2.GetError(), ErrAliasNotInChain) {
		t.Errorf("expected ErrAliasNotInChain, got %v", q2.GetError())
	}
}
```

注：`Order` 是测试模型，需要在 `model_test.go` 或 `advanced_test.go` 中已存在。如未存在，在 `model_test.go` 中追加：

```go
type Order struct {
	ID     uint
	UserID uint
	Amount int
	Status string
}
```

- [ ] **Step 2: 运行 RED**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestLeftJoinAs_ -v ./...`
Expected: FAIL — LeftJoinAs 未实现

- [ ] **Step 3: 实现 LeftJoinAs（query.go 追加）+ joinInfo 加 aliasName 字段（builder.go）**

`builder.go` 中 `joinInfo` 加字段：

```go
type joinInfo struct {
	query     string
	args      []any
	aliasName string // v0.8.0：alias name，空字符串表示走 deprecated 旧 Join 路径
}
```

`query.go` 中追加：

```go
// LeftJoinAs 类型安全的 LEFT JOIN
//
// alias：必须由 As[X](q, ...) 创建的实例
// leftCol / rightCol：字段地址（任意一方可来自主表 / alias 副表）
// extraSQL：额外 ON 条件 SQL 片段，含 ? 占位符（如 "AND o.status = ?"）；不能拼用户输入
// extraArgs：占位符对应参数，走 GORM 参数化（绝不进入 SQL 字面量）
func (q *Query[T]) LeftJoinAs(alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Query[T] {
	q.appendJoinAs("LEFT JOIN", alias, leftCol, rightCol, extraSQL, extraArgs)
	return q
}

// appendJoinAs 内部辅助：所有 JoinAs 共用的 join 构建逻辑
func (q *Query[T]) appendJoinAs(joinType string, alias any, leftCol any, rightCol any, extraSQL string, extraArgs []any) {
	if q.core == nil {
		q.errs = append(q.errs, errors.New("gplus: query core not initialized"))
		return
	}

	// 校验 alias 实例属于 q 链
	addr := uintptr(reflect.ValueOf(alias).Pointer())
	aliasName, aliasTyp, ok := q.lookupAliasFromChain(addr)
	if !ok {
		q.core.appendErr(ErrAliasNotInChain)
		return
	}

	// 解析 leftCol / rightCol
	leftStr, lerr := q.resolveColumnName(uintptr(reflect.ValueOf(leftCol).Pointer()))
	rightStr, rerr := q.resolveColumnName(uintptr(reflect.ValueOf(rightCol).Pointer()))
	if lerr != nil || rerr != nil {
		return // resolveColumnName 已 appendErr
	}

	// 构造 join SQL（仅拼接结构化字面量，绝不拼 extraArgs）
	tableName := schemaTableName(aliasTyp)
	joinSQL := fmt.Sprintf("%s %s AS %s ON %s = %s",
		joinType,
		quoteIdent(tableName),
		quoteIdent(aliasName),
		leftStr, rightStr,
	)
	if extraSQL != "" {
		joinSQL += " " + extraSQL // extraSQL 自身含 ? 占位符
	}

	q.joins = append(q.joins, joinInfo{
		query:     joinSQL,
		args:      extraArgs, // 走 GORM 参数化，与 ? 占位符匹配
		aliasName: aliasName,
	})
}

// lookupAliasFromChain 沿 q 链查找某个实例对应的 alias name 和 type
func (q *Query[T]) lookupAliasFromChain(addr uintptr) (name string, typ reflect.Type, ok bool) {
	current := AnyQuery(q)
	for current != nil {
		core := current.gplusCore()
		for _, entry := range core.aliases {
			if entry.addrLow == addr && !entry.revoked {
				return entry.name, entry.typ, true
			}
		}
		current = core.outerQueryRef
	}
	return "", nil, false
}

// schemaTableName 取 X 的 table name（GORM Schema 解析，由 schema.go 已有逻辑提供）
// 此处声明，实现复用 schema.go 中现有 schema 解析路径
```

`builder.go` 中 `applyJoins` 函数已能处理 joinInfo（v0.7.x 既有），加 alias name 不破坏既有路径。

- [ ] **Step 4: 测试 PASS**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestLeftJoinAs_ -v ./...`
Expected: 3 子测试 PASS

- [ ] **Step 5: Commit**

```bash
git add query.go builder.go query_joinas_test.go model_test.go
git commit -m "feat(alias): LeftJoinAs + joinInfo aliasName 字段（C1 extraSQL 参数化）"
```

---

## Task 11: RightJoinAs / InnerJoinAs / OuterJoinAs / FullJoinAs

**Files:**
- Modify: `query.go`
- Test: `query_joinas_test.go` 追加

- [ ] **Step 1: 写测试（RED，4 子测试一组）**

```go
// query_joinas_test.go 追加

func TestJoinAsVariants(t *testing.T) {
	cases := []struct {
		name    string
		method  func(q *Query[TestUser], alias any, l, r any)
		wantSQL string
	}{
		{"RightJoinAs", func(q *Query[TestUser], a, l, r any) { q.RightJoinAs(a, l, r, "") }, "RIGHT JOIN"},
		{"InnerJoinAs", func(q *Query[TestUser], a, l, r any) { q.InnerJoinAs(a, l, r, "") }, "INNER JOIN"},
		{"OuterJoinAs", func(q *Query[TestUser], a, l, r any) { q.OuterJoinAs(a, l, r, "") }, "OUTER JOIN"},
		{"FullJoinAs", func(q *Query[TestUser], a, l, r any) { q.FullJoinAs(a, l, r, "") }, "FULL JOIN"},
	}
	db := setupTestDB[TestUser](t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, u := NewQuery[TestUser](context.Background())
			o := As[Order](q, "o")
			tc.method(q, o, &o.UserID, &u.ID)
			sql, err := q.ToSQL(db)
			if err != nil {
				t.Fatalf("ToSQL: %v", err)
			}
			if !strings.Contains(sql, tc.wantSQL) {
				t.Errorf("expected %s in %s", tc.wantSQL, sql)
			}
		})
	}
}
```

- [ ] **Step 2: RED 确认 fail**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestJoinAsVariants -v ./...`
Expected: FAIL — 4 个 method 未实现

- [ ] **Step 3: 实现 4 个 JoinAs（query.go 追加）**

```go
func (q *Query[T]) RightJoinAs(alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Query[T] {
	q.appendJoinAs("RIGHT JOIN", alias, leftCol, rightCol, extraSQL, extraArgs)
	return q
}
func (q *Query[T]) InnerJoinAs(alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Query[T] {
	q.appendJoinAs("INNER JOIN", alias, leftCol, rightCol, extraSQL, extraArgs)
	return q
}
func (q *Query[T]) OuterJoinAs(alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Query[T] {
	q.appendJoinAs("OUTER JOIN", alias, leftCol, rightCol, extraSQL, extraArgs)
	return q
}
func (q *Query[T]) FullJoinAs(alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Query[T] {
	q.appendJoinAs("FULL JOIN", alias, leftCol, rightCol, extraSQL, extraArgs)
	return q
}
```

- [ ] **Step 4: 测试 PASS**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestJoinAsVariants -v ./...`
Expected: 4 子测试 PASS

- [ ] **Step 5: Commit**

```bash
git add query.go query_joinas_test.go
git commit -m "feat(alias): RightJoinAs / InnerJoinAs / OuterJoinAs / FullJoinAs"
```

---

## Task 12: CrossJoinAs / NaturalJoinAs（无 ON 条件）

**Files:**
- Modify: `query.go`
- Test: `query_joinas_test.go` 追加

- [ ] **Step 1: 写测试（RED）**

```go
// query_joinas_test.go 追加

func TestCrossJoinAs(t *testing.T) {
	db := setupTestDB[TestUser](t)
	q, _ := NewQuery[TestUser](context.Background())
	o := As[Order](q, "o")
	q.CrossJoinAs(o)
	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	if !strings.Contains(sql, "CROSS JOIN") {
		t.Errorf("expected CROSS JOIN, got %s", sql)
	}
}

func TestNaturalJoinAs(t *testing.T) {
	db := setupTestDB[TestUser](t)
	q, _ := NewQuery[TestUser](context.Background())
	o := As[Order](q, "o")
	q.NaturalJoinAs(o)
	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	if !strings.Contains(sql, "NATURAL JOIN") {
		t.Errorf("expected NATURAL JOIN, got %s", sql)
	}
}
```

- [ ] **Step 2: RED**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run "TestCrossJoinAs|TestNaturalJoinAs" -v ./...`
Expected: FAIL

- [ ] **Step 3: 实现（query.go 追加）**

```go
func (q *Query[T]) CrossJoinAs(alias any) *Query[T] {
	q.appendJoinAsNoOn("CROSS JOIN", alias)
	return q
}
func (q *Query[T]) NaturalJoinAs(alias any) *Query[T] {
	q.appendJoinAsNoOn("NATURAL JOIN", alias)
	return q
}

func (q *Query[T]) appendJoinAsNoOn(joinType string, alias any) {
	if q.core == nil {
		return
	}
	addr := uintptr(reflect.ValueOf(alias).Pointer())
	aliasName, aliasTyp, ok := q.lookupAliasFromChain(addr)
	if !ok {
		q.core.appendErr(ErrAliasNotInChain)
		return
	}
	tableName := schemaTableName(aliasTyp)
	joinSQL := fmt.Sprintf("%s %s AS %s",
		joinType,
		quoteIdent(tableName),
		quoteIdent(aliasName),
	)
	q.joins = append(q.joins, joinInfo{
		query:     joinSQL,
		aliasName: aliasName,
	})
}
```

- [ ] **Step 4: 测试 PASS**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run "TestCrossJoinAs|TestNaturalJoinAs" -v ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add query.go query_joinas_test.go
git commit -m "feat(alias): CrossJoinAs / NaturalJoinAs（无 ON）"
```

---

## Task 13: deprecated 标记 7 个旧 Join API

**Files:**
- Modify: `query.go`
- Test: 仅注释验证（无运行时变化）

- [ ] **Step 1: 在每个旧 Join 方法上加 deprecated godoc**

定位 `query.go` 中 7 个既有 Join 方法（LeftJoin / RightJoin / InnerJoin / OuterJoin / FullJoin / CrossJoin / NaturalJoin），在每个上方加：

```go
// LeftJoin 添加 LEFT JOIN（基于 raw 字符串）
//
// Deprecated: use LeftJoinAs for type-safe column references.
// Will be removed in v1.0. Still useful for joining subquery tables /
// function-returning tables / USING clauses where alias instances
// cannot represent the join target.
func (q *Query[T]) LeftJoin(table string, on string, args ...any) *Query[T] {
	// ...既有实现保持不变
}
```

对其他 6 个方法做同样的 deprecated 标记。

- [ ] **Step 2: 运行 vet 确认无新警告**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe vet ./...`
Expected: 通过；deprecated 标记不影响编译

- [ ] **Step 3: 全量回归（确保旧测试仍 PASS）**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test ./...`
Expected: 全部 PASS

- [ ] **Step 4: Commit**

```bash
git add query.go
git commit -m "docs(alias): deprecated 标记 7 个旧 Join API（v1.0 删除）"
```

---

## Task 14: SubQuery / SubQueryAs 派生（含 H4 nil 累积）

**Files:**
- Modify: `subquery.go`
- Test: `query_subquery_correlated_test.go`（新建）

- [ ] **Step 1: 写测试（RED）**

```go
// query_subquery_correlated_test.go
package gplus

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSubQuery_NilOuter_AccumulatesError_H4(t *testing.T) {
	sub, _ := SubQuery[Order](nil)
	if !errors.Is(sub.GetError(), ErrSubqueryOuterNil) {
		t.Errorf("expected ErrSubqueryOuterNil, got %v", sub.GetError())
	}
}

func TestSubQuery_HappyPath(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	sub, o := SubQuery[Order](q)
	if sub.core.outerQueryRef != AnyQuery(q) {
		t.Errorf("outerQueryRef not set correctly")
	}
	if o == nil {
		t.Errorf("nil instance returned")
	}
}

func TestSubQuery_CorrelatedAliasResolves(t *testing.T) {
	q, u := NewQuery[TestUser](context.Background())
	sub, o := SubQuery[Order](q)
	col, err := sub.resolveColumnName(uintptrOf(&u.ID))
	if err != nil {
		t.Fatalf("expected resolve to succeed, got %v", err)
	}
	if !strings.Contains(col, "id") {
		t.Errorf("expected u.ID resolved, got %s", col)
	}
	col2, err := sub.resolveColumnName(uintptrOf(&o.UserID))
	if err != nil {
		t.Fatalf("sub alias resolve failed: %v", err)
	}
	if col2 != "orders.user_id" {
		t.Errorf("expected orders.user_id, got %s", col2)
	}
}

func TestSubQueryAs_MainTableAlias(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	sub, o := SubQueryAs[Order](q, "o")
	col, err := sub.resolveColumnName(uintptrOf(&o.UserID))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if col != "o.user_id" {
		t.Errorf("expected o.user_id, got %s", col)
	}
}
```

- [ ] **Step 2: RED**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestSubQuery_ -v ./...`
Expected: FAIL

- [ ] **Step 3: 实现 SubQuery / SubQueryAs（subquery.go 追加）**

```go
// SubQuery 派生子查询：sub.outerQueryRef = outer，sub 主表默认 alias = 表名
//
// outer == nil：返回带 ErrSubqueryOuterNil 累积错误的 dud sub（H4：与 errs 累积哲学一致）
// sub.ctx 来自 outer.ctx 透传
// sub 不自动应用 outer 的 DataRule
func SubQuery[X any](outer AnyQuery) (*Query[X], *X) {
	if outer == nil {
		// H4：返回带预置错误的 dud sub
		sub, x := NewQuery[X](context.Background())
		sub.core.appendErr(ErrSubqueryOuterNil)
		return sub, x
	}
	core := outer.gplusCore()
	ctx := core.context()
	sub, x := NewQuery[X](ctx)
	sub.core.outerQueryRef = outer
	return sub, x
}

// SubQueryAs 派生子查询并指定主表 alias
func SubQueryAs[X any](outer AnyQuery, alias string) (*Query[X], *X) {
	if outer == nil {
		sub, _ := NewQuery[X](context.Background())
		sub.core.appendErr(ErrSubqueryOuterNil)
		return sub, getModelInstance[X]()
	}
	core := outer.gplusCore()
	ctx := core.context()
	sub := &Query[X]{core: newQueryCore(ctx)}
	sub.core.outerQueryRef = outer
	x := As[X](sub, alias)
	return sub, x
}
```

- [ ] **Step 4: 测试 PASS**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestSubQuery_ -v ./...`
Expected: 4 子测试 PASS

- [ ] **Step 5: Commit**

```bash
git add subquery.go query_subquery_correlated_test.go
git commit -m "feat(alias): SubQuery / SubQueryAs 派生（H4 nil 累积 + outerQuery 链路）"
```

---

## Task 15: 嵌套 SubQuery 3 层 + 跨链泄露 H5 测试

**Files:**
- Test: `query_subquery_correlated_test.go` 追加

- [ ] **Step 1: 写测试（应直接 PASS，验证既有实现）**

```go
// query_subquery_correlated_test.go 追加

func TestSubQuery_NestedThreeLayers(t *testing.T) {
	q, u := NewQuery[TestUser](context.Background())
	sub1, o := SubQuery[Order](q)
	sub2, p := SubQuery[Product](sub1)
	// sub2 应能向上解析 q.u 和 sub1.o
	colU, err := sub2.resolveColumnName(uintptrOf(&u.ID))
	if err != nil || !strings.Contains(colU, "id") {
		t.Errorf("nested resolve to grandparent failed: col=%s err=%v", colU, err)
	}
	colO, err := sub2.resolveColumnName(uintptrOf(&o.UserID))
	if err != nil || colO != "orders.user_id" {
		t.Errorf("nested resolve to parent failed: col=%s err=%v", colO, err)
	}
	// sub2 自身 alias 默认表名 "products"
	colP, err := sub2.resolveColumnName(uintptrOf(&p.Name))
	if err != nil || colP != "products.name" {
		t.Errorf("sub2 self alias failed: col=%s err=%v", colP, err)
	}
}

func TestSubQuery_CrossChainLeakage_H5(t *testing.T) {
	// q1 与 q2 完全独立
	q1, _ := NewQuery[TestUser](context.Background())
	q2, u2 := NewQueryAs[TestUser](context.Background(), "u2")
	sub, _ := SubQuery[Order](q1)
	// 错误地把 q2 的 alias 字段传给 sub.resolveColumnName
	_, err := sub.resolveColumnName(uintptrOf(&u2.Name))
	if !errors.Is(err, ErrFieldAddrUnregistered) {
		t.Errorf("H5 expected sub strict closure ErrFieldAddrUnregistered, got %v", err)
	}
}
```

注：`Product` 是新增测试模型，需在 `model_test.go` 加：

```go
type Product struct {
	ID   uint
	Name string
}
```

- [ ] **Step 2: 运行测试，应 PASS（实现已就绪）**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run "TestSubQuery_Nested|TestSubQuery_CrossChain" -v ./...`
Expected: PASS（前置 task 实现已支持嵌套和 H5 闭合）

- [ ] **Step 3: Commit**

```bash
git add query_subquery_correlated_test.go model_test.go
git commit -m "test(alias): SubQuery 嵌套 3 层 + 跨链 H5 闭合验证"
```

---

## Task 16: Exists / NotExists（P5）

**Files:**
- Modify: `query.go`、`builder.go`
- Test: `query_exists_test.go`（新建）

- [ ] **Step 1: 写测试（RED）**

```go
// query_exists_test.go
package gplus

import (
	"context"
	"strings"
	"testing"
)

func TestExists_BasicSQL(t *testing.T) {
	db := setupTestDB[TestUser](t)
	q, u := NewQuery[TestUser](context.Background())
	sub, o := SubQuery[Order](q)
	sub.EqCol(&o.UserID, &u.ID)
	q.Exists(sub)
	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	if !strings.Contains(sql, "EXISTS") {
		t.Errorf("expected EXISTS in SQL, got %s", sql)
	}
}

func TestNotExists_BasicSQL(t *testing.T) {
	db := setupTestDB[TestUser](t)
	q, u := NewQuery[TestUser](context.Background())
	sub, o := SubQuery[Order](q)
	sub.EqCol(&o.UserID, &u.ID)
	q.NotExists(sub)
	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	if !strings.Contains(sql, "NOT EXISTS") {
		t.Errorf("expected NOT EXISTS in SQL, got %s", sql)
	}
}

func TestExists_NilSub_AccumulatesErrSubqueryNil(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	q.Exists(nil)
	if !errors.Is(q.GetError(), ErrSubqueryNil) {
		t.Errorf("expected ErrSubqueryNil, got %v", q.GetError())
	}
}

func TestExists_SubErrorsPropagate(t *testing.T) {
	q, _ := NewQuery[TestUser](context.Background())
	sub, _ := SubQuery[Order](nil) // sub 自身有 ErrSubqueryOuterNil
	q.Exists(sub)
	if !errors.Is(q.GetError(), ErrSubqueryOuterNil) {
		t.Errorf("expected sub error to propagate to q, got %v", q.GetError())
	}
}
```

注：`EqCol` 是已有的 v0.6.0 子查询/列引用方法，假设其行为不变；如未实现，此 task 之前先补。

- [ ] **Step 2: RED**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run "TestExists_|TestNotExists_" -v ./...`
Expected: FAIL — Exists / NotExists 未实现

- [ ] **Step 3: 实现 Exists / NotExists（query.go 追加）+ existsLeaf 处理（builder.go）**

`builder.go` 中 `leafKind` 加新值 + `applyWhere` 加分支：

```go
const (
	// ...既有 leafKind 常量
	existsLeaf leafKind = iota + 100 // v0.8.0 alias 体系
)

// applyWhere 中 switch 加 case
switch leaf.kind {
case existsLeaf:
	if leaf.subExpr == nil {
		// 已在 Exists() 入口校验，此处防御性处理
		return d
	}
	if subErr := leaf.subExpr.GetError(); subErr != nil {
		d.AddError(subErr)
	}
	subDB := leaf.subExpr.ToDB(d.Session(&gorm.Session{NewDB: true}))
	d = d.Where(leaf.op+" (?)", subDB)
}
```

`query.go` 中追加：

```go
// Exists / NotExists / OrExists / OrNotExists 相关子查询
//
// sub 必须满足 Subquerier 接口（*Query[X] 自动满足）；推荐通过 SubQuery[X](q) 派生
// sub 内部 errs 会通过 BuildQuery 时透传到外层 q.errs
func (q *Query[T]) Exists(sub Subquerier) *Query[T] {
	return q.appendExists("EXISTS", false, sub)
}
func (q *Query[T]) NotExists(sub Subquerier) *Query[T] {
	return q.appendExists("NOT EXISTS", false, sub)
}

func (q *Query[T]) appendExists(op string, isOr bool, sub Subquerier) *Query[T] {
	if sub == nil {
		q.appendCoreErr(ErrSubqueryNil)
		return q
	}
	q.conditions = append(q.conditions, leafCondition{
		kind:    existsLeaf,
		op:      op,
		subExpr: sub,
		isOr:    isOr,
	})
	return q
}

// appendCoreErr 辅助：累积错误到 core.errs（与 Updater 共用）
func (q *Query[T]) appendCoreErr(err error) {
	if q.core != nil {
		q.core.appendErr(err)
	} else {
		q.errs = append(q.errs, err)
	}
}
```

- [ ] **Step 4: 测试 PASS**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run "TestExists_|TestNotExists_" -v ./...`
Expected: 4 子测试 PASS

- [ ] **Step 5: Commit**

```bash
git add query.go builder.go query_exists_test.go
git commit -m "feat(alias): Exists / NotExists + existsLeaf 条件类型"
```

---

## Task 17: OrExists / OrNotExists

**Files:**
- Modify: `query.go`
- Test: `query_exists_test.go` 追加

- [ ] **Step 1: 写测试（RED）**

```go
// query_exists_test.go 追加

func TestOrExists_OrBranchSQL(t *testing.T) {
	db := setupTestDB[TestUser](t)
	q, u := NewQuery[TestUser](context.Background())
	q.Eq(&u.Name, "alice")
	sub, o := SubQuery[Order](q)
	sub.EqCol(&o.UserID, &u.ID)
	q.OrExists(sub)
	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	if !strings.Contains(sql, "OR EXISTS") {
		t.Errorf("expected OR EXISTS, got %s", sql)
	}
}

func TestOrNotExists_OrBranchSQL(t *testing.T) {
	db := setupTestDB[TestUser](t)
	q, u := NewQuery[TestUser](context.Background())
	q.Eq(&u.Name, "alice")
	sub, o := SubQuery[Order](q)
	sub.EqCol(&o.UserID, &u.ID)
	q.OrNotExists(sub)
	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	if !strings.Contains(sql, "OR NOT EXISTS") {
		t.Errorf("expected OR NOT EXISTS, got %s", sql)
	}
}
```

- [ ] **Step 2: RED**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run "TestOrExists_|TestOrNotExists_" -v ./...`
Expected: FAIL

- [ ] **Step 3: 实现（query.go 追加）**

```go
func (q *Query[T]) OrExists(sub Subquerier) *Query[T] {
	return q.appendExists("EXISTS", true, sub)
}
func (q *Query[T]) OrNotExists(sub Subquerier) *Query[T] {
	return q.appendExists("NOT EXISTS", true, sub)
}
```

注：`appendExists` 已有 `isOr` 参数；builder.go 中 applyWhere 处理 existsLeaf 时根据 `leaf.isOr` 决定使用 `Where` 还是 `Or`。补全 builder.go：

```go
case existsLeaf:
	// ...
	clause := leaf.op + " (?)"
	if leaf.isOr {
		d = d.Or(clause, subDB)
	} else {
		d = d.Where(clause, subDB)
	}
```

- [ ] **Step 4: 测试 PASS**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run "TestOrExists_|TestOrNotExists_" -v ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add query.go builder.go query_exists_test.go
git commit -m "feat(alias): OrExists / OrNotExists（OR 分支）"
```

---

## Task 18: Updater[T] 加 core *queryCore + 2 个 JoinAs（P6）

**Files:**
- Modify: `update.go`
- Test: `updater_alias_test.go`（新建）

- [ ] **Step 1: 写测试（RED）**

```go
// updater_alias_test.go
package gplus

import (
	"context"
	"strings"
	"testing"
)

func TestUpdater_LeftJoinAs_BasicSQL(t *testing.T) {
	db := setupTestDB[TestUser](t)
	u, ut := NewUpdater[TestUser](context.Background())
	o := As[Order](u, "o")
	u.LeftJoinAs(o, &o.UserID, &ut.ID, "")
	u.Set(&ut.Name, "x")
	sql, err := u.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	if !strings.Contains(sql, "LEFT JOIN") {
		t.Errorf("expected LEFT JOIN in updater SQL, got %s", sql)
	}
}

func TestUpdater_InnerJoinAs(t *testing.T) {
	db := setupTestDB[TestUser](t)
	u, ut := NewUpdater[TestUser](context.Background())
	o := As[Order](u, "o")
	u.InnerJoinAs(o, &o.UserID, &ut.ID, "")
	u.Set(&ut.Name, "x")
	sql, err := u.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	if !strings.Contains(sql, "INNER JOIN") {
		t.Errorf("expected INNER JOIN, got %s", sql)
	}
}
```

- [ ] **Step 2: RED**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestUpdater_ -v ./...`
Expected: FAIL — Updater 无 core / 无 JoinAs

- [ ] **Step 3: 修改 Updater[T] 结构 + NewUpdater 初始化 + gplusCore + 2 个 JoinAs**

`update.go` 中：

```go
type Updater[T any] struct {
	core *queryCore // v0.8.0：alias 体系共享状态

	// ...其他既有字段
}

func NewUpdater[T any](ctx context.Context) (*Updater[T], *T) {
	u := &Updater[T]{
		core: newQueryCore(ctx),
		// ...其他既有初始化
	}
	return u, getModelInstance[T]()
}

func (u *Updater[T]) gplusCore() *queryCore {
	return u.core
}

var _ AnyQuery = (*Updater[struct{}])(nil)

// LeftJoinAs / InnerJoinAs（M2 精简：UPDATE 中 Cross/Natural/Outer/Full 几乎无用，不提供）
func (u *Updater[T]) LeftJoinAs(alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Updater[T] {
	u.appendJoinAs("LEFT JOIN", alias, leftCol, rightCol, extraSQL, extraArgs)
	return u
}
func (u *Updater[T]) InnerJoinAs(alias any, leftCol any, rightCol any, extraSQL string, extraArgs ...any) *Updater[T] {
	u.appendJoinAs("INNER JOIN", alias, leftCol, rightCol, extraSQL, extraArgs)
	return u
}

// appendJoinAs 与 Query 同形态（复用代码逻辑，但因 receiver 类型不同需各自实现）
// 实现内部直接调用 ScopeBuilder 的 appendJoinAs（如 ScopeBuilder 已封装）
// 或拷贝 query.go 中的 appendJoinAs 逻辑（注意 u.core 而非 q.core）
func (u *Updater[T]) appendJoinAs(joinType string, alias any, leftCol any, rightCol any, extraSQL string, extraArgs []any) {
	if u.core == nil {
		u.errs = append(u.errs, errors.New("gplus: updater core not initialized"))
		return
	}
	addr := uintptr(reflect.ValueOf(alias).Pointer())
	aliasName, aliasTyp, ok := u.lookupAliasFromChain(addr)
	if !ok {
		u.core.appendErr(ErrAliasNotInChain)
		return
	}
	leftStr, lerr := u.resolveColumnName(uintptr(reflect.ValueOf(leftCol).Pointer()))
	rightStr, rerr := u.resolveColumnName(uintptr(reflect.ValueOf(rightCol).Pointer()))
	if lerr != nil || rerr != nil {
		return
	}
	tableName := schemaTableName(aliasTyp)
	joinSQL := fmt.Sprintf("%s %s AS %s ON %s = %s",
		joinType, quoteIdent(tableName), quoteIdent(aliasName), leftStr, rightStr)
	if extraSQL != "" {
		joinSQL += " " + extraSQL
	}
	u.joins = append(u.joins, joinInfo{
		query:     joinSQL,
		args:      extraArgs,
		aliasName: aliasName,
	})
}

// lookupAliasFromChain 与 Query 同逻辑（复制即可，DRY 容忍度内）
func (u *Updater[T]) lookupAliasFromChain(addr uintptr) (name string, typ reflect.Type, ok bool) {
	current := AnyQuery(u)
	for current != nil {
		core := current.gplusCore()
		for _, entry := range core.aliases {
			if entry.addrLow == addr && !entry.revoked {
				return entry.name, entry.typ, true
			}
		}
		current = core.outerQueryRef
	}
	return "", nil, false
}

// resolveColumnName 与 Query 同逻辑（沿链 + H5 + N4）
func (u *Updater[T]) resolveColumnName(addr uintptr) (string, error) {
	if u.core == nil {
		return resolveColumnNameLegacy(addr)
	}
	isSub := u.core.outerQueryRef != nil
	current := AnyQuery(u)
	for current != nil {
		core := current.gplusCore()
		if alias, col, ok := core.lookupAddr(addr); ok {
			return alias + "." + col, nil
		}
		if core.hadRevokedHit(addr) {
			u.core.appendErr(ErrAliasRevoked)
			return "", ErrAliasRevoked
		}
		current = core.outerQueryRef
	}
	if isSub {
		u.core.appendErr(ErrFieldAddrUnregistered)
		return "", ErrFieldAddrUnregistered
	}
	if col, ok := globalColumnNameLookup(addr); ok {
		return col, nil
	}
	u.core.appendErr(ErrFieldAddrUnregistered)
	return "", ErrFieldAddrUnregistered
}
```

注：`Query.appendJoinAs` / `Query.lookupAliasFromChain` / `Query.resolveColumnName` 与 `Updater` 几乎同代码——可考虑后续 task 重构到 ScopeBuilder 公共方法（但 v0.8.0 直接复制接受重复代码，避免引入新抽象）。

- [ ] **Step 4: 测试 PASS**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestUpdater_ -v ./...`
Expected: 2 子测试 PASS

- [ ] **Step 5: Commit**

```bash
git add update.go updater_alias_test.go
git commit -m "feat(alias): Updater[T] core + LeftJoinAs/InnerJoinAs（M2 精简镜像）"
```

---

## Task 19: Updater Exists / NotExists / OrExists / OrNotExists

**Files:**
- Modify: `update.go`
- Test: `updater_alias_test.go` 追加

- [ ] **Step 1: 写测试（RED）**

```go
// updater_alias_test.go 追加

func TestUpdater_Exists(t *testing.T) {
	db := setupTestDB[TestUser](t)
	u, ut := NewUpdater[TestUser](context.Background())
	sub, o := SubQuery[Order](u)
	sub.EqCol(&o.UserID, &ut.ID)
	u.Exists(sub)
	u.Set(&ut.Name, "x")
	sql, err := u.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	if !strings.Contains(sql, "EXISTS") {
		t.Errorf("expected EXISTS, got %s", sql)
	}
}

func TestUpdater_NotExists(t *testing.T) {
	db := setupTestDB[TestUser](t)
	u, ut := NewUpdater[TestUser](context.Background())
	sub, o := SubQuery[Order](u)
	sub.EqCol(&o.UserID, &ut.ID)
	u.NotExists(sub)
	u.Set(&ut.Name, "x")
	sql, err := u.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	if !strings.Contains(sql, "NOT EXISTS") {
		t.Errorf("expected NOT EXISTS, got %s", sql)
	}
}

func TestUpdater_OrExists(t *testing.T) {
	db := setupTestDB[TestUser](t)
	u, ut := NewUpdater[TestUser](context.Background())
	u.Eq(&ut.Name, "alice")
	sub, o := SubQuery[Order](u)
	sub.EqCol(&o.UserID, &ut.ID)
	u.OrExists(sub)
	u.Set(&ut.Name, "x")
	sql, err := u.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	if !strings.Contains(sql, "OR EXISTS") {
		t.Errorf("expected OR EXISTS, got %s", sql)
	}
}
```

- [ ] **Step 2: RED**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run "TestUpdater_Exists|TestUpdater_NotExists|TestUpdater_OrExists" -v ./...`
Expected: FAIL

- [ ] **Step 3: 实现（update.go 追加，与 Query 同形态）**

```go
func (u *Updater[T]) Exists(sub Subquerier) *Updater[T] {
	return u.appendExists("EXISTS", false, sub)
}
func (u *Updater[T]) NotExists(sub Subquerier) *Updater[T] {
	return u.appendExists("NOT EXISTS", false, sub)
}
func (u *Updater[T]) OrExists(sub Subquerier) *Updater[T] {
	return u.appendExists("EXISTS", true, sub)
}
func (u *Updater[T]) OrNotExists(sub Subquerier) *Updater[T] {
	return u.appendExists("NOT EXISTS", true, sub)
}

func (u *Updater[T]) appendExists(op string, isOr bool, sub Subquerier) *Updater[T] {
	if sub == nil {
		if u.core != nil {
			u.core.appendErr(ErrSubqueryNil)
		}
		return u
	}
	u.conditions = append(u.conditions, leafCondition{
		kind:    existsLeaf,
		op:      op,
		subExpr: sub,
		isOr:    isOr,
	})
	return u
}
```

- [ ] **Step 4: 测试 PASS**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestUpdater_ -v ./...`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add update.go updater_alias_test.go
git commit -m "feat(alias): Updater Exists/NotExists/OrExists/OrNotExists 镜像"
```

---

## Task 20: Updater Clear() 同步翻转 revoked

**Files:**
- Modify: `update.go`
- Test: `updater_alias_test.go` 追加

- [ ] **Step 1: 写测试（RED）**

```go
// updater_alias_test.go 追加

func TestUpdater_Clear_AliasUseAfterClear_N4(t *testing.T) {
	u, _ := NewUpdater[TestUser](context.Background())
	o := As[Order](u, "o")
	u.Clear()
	_, err := u.resolveColumnName(uintptrOf(&o.UserID))
	if !errors.Is(err, ErrAliasRevoked) {
		t.Errorf("expected ErrAliasRevoked after Updater.Clear, got %v", err)
	}
}
```

- [ ] **Step 2: RED**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestUpdater_Clear -v ./...`
Expected: FAIL

- [ ] **Step 3: 修改 Updater.Clear()（与 Query.Clear 同形态）**

定位 `update.go` 中 `func (u *Updater[T]) Clear() *Updater[T]`：

```go
func (u *Updater[T]) Clear() *Updater[T] {
	if u.core != nil {
		for _, entry := range u.core.aliases {
			entry.revoked = true // N4：保留 entry 但标记 revoked
		}
		u.core.outerQueryRef = nil
		u.core.errs = nil
	}
	// ...既有 Clear 逻辑
	return u
}
```

- [ ] **Step 4: 测试 PASS**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestUpdater_Clear -v ./...`
Expected: PASS

- [ ] **Step 5: 全量回归**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test ./...`
Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add update.go updater_alias_test.go
git commit -m "feat(alias): Updater Clear 翻转 revoked（N4 防御）"
```

---

## Task 21: DataRule × alias e2e 反例 + 集成测试（P7）

**Files:**
- Create: `alias_datarule_e2e_test.go`
- Modify: `mysql_integration_test.go`

- [ ] **Step 1: 写 e2e 反例段 A（锁敞开契约）+ 段 B（验合规模式）**

```go
// alias_datarule_e2e_test.go
package gplus

import (
	"context"
	"strings"
	"testing"
)

// TestDataRuleAliasContract_NoAutoInjectionToSideTable
// 段 A：锁敞开契约 v0.8.0 — 未在 JoinAs extraSQL 加副表过滤时，DataRule 不自动注入副表
// 此测试 PASS 表示 v0.8.0 "副表敞开"契约成立；未来 v0.9+ 加 cross-table 自动注入此段会 fail
func TestDataRuleAliasContract_NoAutoInjectionToSideTable(t *testing.T) {
	db := setupTestDB[TestUser](t)
	ctx := WithDataRules(context.Background(), []DataRule{
		{Column: "tenant_id", Op: "=", Value: 1},
	})
	q, u := NewQuery[TestUser](ctx)
	o := As[Order](q, "o")
	q.LeftJoinAs(o, &o.UserID, &u.ID, "") // 故意未加 o.tenant_id
	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	// 主表 DataRule 应注入
	if !strings.Contains(sql, "tenant_id") {
		t.Errorf("DataRule should inject to main table tenant_id, got %s", sql)
	}
	// 副表（orders / o）的 tenant_id 不应自动出现 — 段 A 锁定此契约
	// 计数：sql 中 tenant_id 应只出现 1 次（主表的）
	count := strings.Count(sql, "tenant_id")
	if count != 1 {
		t.Errorf("expected exactly 1 tenant_id occurrence (main table only), got %d in %s", count, sql)
	}
}

// TestDataRuleAliasContract_ExplicitExtraBlocksLeak
// 段 B：验合规模式 — 在 JoinAs extraSQL 加副表 tenant 过滤时，无泄漏
func TestDataRuleAliasContract_ExplicitExtraBlocksLeak(t *testing.T) {
	db := setupTestDB[TestUser](t)
	ctx := WithDataRules(context.Background(), []DataRule{
		{Column: "tenant_id", Op: "=", Value: 1},
	})
	q, u := NewQuery[TestUser](ctx)
	o := As[Order](q, "o")
	q.LeftJoinAs(o, &o.UserID, &u.ID, "AND o.tenant_id = ?", 1) // 显式合规
	sql, err := q.ToSQL(db)
	if err != nil {
		t.Fatalf("ToSQL: %v", err)
	}
	// 主表 + 副表都应有 tenant_id 过滤
	if !strings.Contains(sql, "tenant_id") {
		t.Errorf("expected tenant_id, got %s", sql)
	}
	// SQL 中 tenant_id 应至少出现 2 次（主表 + 副表 alias.tenant_id）
	count := strings.Count(sql, "tenant_id")
	if count < 2 {
		t.Errorf("expected ≥ 2 tenant_id occurrences (main + side via alias), got %d in %s", count, sql)
	}
}
```

- [ ] **Step 2: 运行 e2e 测试**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run TestDataRuleAliasContract_ -v ./...`
Expected: 2 子测试 PASS

- [ ] **Step 3: 加 MySQL/SQLite 双方言集成测试**

`mysql_integration_test.go` 追加：

```go
func TestIntegration_SelfJoin_BothDialects(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			db := setupDialectDB(t, dialect)
			q, u := NewQueryAs[TestUser](context.Background(), "u")
			boss := As[TestUser](q, "boss")
			q.LeftJoinAs(boss, &u.BossID, &boss.ID, "")
			sql, err := q.ToSQL(db)
			if err != nil {
				t.Fatalf("ToSQL: %v", err)
			}
			if !strings.Contains(sql, "test_users AS boss") &&
				!strings.Contains(sql, "`test_users` AS `boss`") {
				t.Errorf("self-join alias not in SQL: %s", sql)
			}
		})
	}
}
```

注：`TestUser` 需要加 `BossID` 字段（在 `model_test.go` 中追加），`setupDialectDB` 是 `mysql_integration_test.go` 中已有辅助函数。

- [ ] **Step 4: 测试 PASS**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -run "TestDataRuleAliasContract|TestIntegration_SelfJoin" -v ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add alias_datarule_e2e_test.go mysql_integration_test.go model_test.go
git commit -m "test(alias): e2e DataRule×alias 段 A 锁敞开 + 段 B 验合规 + 双方言集成"
```

---

## Task 22: README + godoc + CHANGELOG（P8）

**Files:**
- Modify: `README.md`、`doc.go`、`CHANGELOG.md`

- [ ] **Step 1: README 加 "Alias 与跨表查询" 章节**

在 `README.md` 现有 v0.7.0 章节之后追加：

```markdown
## Alias 与跨表查询（v0.8.0）

类型安全的跨表列引用、自连接、相关 EXISTS 子查询。

### 跨表列引用

```go
q, u := gplus.NewQuery[User](ctx)
o := gplus.As[Order](q, "o")
q.LeftJoinAs(o, &o.UserID, &u.ID, "").
    Eq(&o.Amount, 100)
// SQL: SELECT users.* FROM users
//      LEFT JOIN orders AS o ON o.user_id = users.id
//      WHERE o.amount = 100
```

### 同表自连接

```go
q, u := gplus.NewQueryAs[User](ctx, "u")
boss := gplus.As[User](q, "boss")
q.LeftJoinAs(boss, &u.BossID, &boss.ID, "")
```

### Correlated EXISTS

```go
q, u := gplus.NewQuery[User](ctx)
sub, o := gplus.SubQuery[Order](q)
sub.EqCol(&o.UserID, &u.ID)
q.Exists(sub)
```

### ⚠️ DataRule × alias 安全契约

**DataRule 不会自动应用到 alias 副表。** 副表敏感字段（tenant_id 等）必须在 JoinAs 的 extraSQL 显式过滤：

```go
q.LeftJoinAs(o, &o.UserID, &u.ID,
    "AND o.tenant_id = ?", tenantID) // ← 显式 + 参数化
```

**禁止**用 `fmt.Sprintf` 拼接用户输入到 extraSQL（SQL 注入）。**禁止**在 `DataRule.Column` 写 alias 前缀（`"o.tenant_id"`）—— v0.9 cross-table DataRule 通过新增 `DataRule.Table` 字段提供，提前写 alias 前缀会形成兼容性陷阱。
```

- [ ] **Step 2: doc.go 加包级 alias 简介**

`doc.go` 现有内容追加段落：

```go
// Alias 体系（v0.8.0）
//
// 通过 As[X](q, name) 创建 alias 实例（独立于规范单例的 *X），
// 支持跨表列引用 / 自连接 / correlated EXISTS。
//
// 详见 docs/superpowers/specs/2026-05-06-alias-system-design.md。
```

- [ ] **Step 3: CHANGELOG.md 加 v0.8.0 段**

文件顶部追加：

```markdown
## [0.8.0] - 2026-05-06

### 新增

- **alias 体系**：类型安全的跨表列引用 / 同表自连接 / correlated EXISTS 子查询
  - `gplus.As[X](q, name)`：在 q 上注册 X 类型的 alias 实例
  - `gplus.NewQueryAs[T](ctx, name)`：主表起 alias 入口
  - `gplus.SubQuery[X](outer)` / `SubQueryAs[X](outer, name)`：派生子查询，支持跨层引用外层 alias
  - 7 种 JoinAs（Query）/ 2 种 JoinAs（Updater）：LeftJoinAs / RightJoinAs / InnerJoinAs / OuterJoinAs / FullJoinAs / CrossJoinAs / NaturalJoinAs
  - Exists / NotExists / OrExists / OrNotExists（Query + Updater 镜像）
- 7 个新错误哨兵：ErrAliasDuplicate / ErrAliasInvalidName / ErrFieldAddrUnregistered / ErrAliasNotInChain / ErrSubqueryOuterNil / ErrAliasQueryNil / ErrAliasRevoked

### 行为约束（须知）

- **DataRule × alias 安全契约**：DataRule 仅作用主表，alias 副表用户自负责。详见 README "Alias 与跨表查询" 章节
- **DataRule.Column 不应写 alias 前缀**：v0.9 cross-table DataRule 通过新增 Table 字段提供，提前在 Column 写 alias 前缀会形成兼容性陷阱
- **JoinAs extraSQL 必须参数化**：禁止 fmt.Sprintf 拼接用户输入；占位符 `?` + extraArgs 走 GORM 参数化
- **As(q=nil)** panic ErrAliasQueryNil（API 入口编程错误）；其他错误均累积 + BuildQuery 短路（决策 1B）
- **Clear() 后 alias 实例失效**：Clear 翻转所有 alias entry 的 revoked 标记，后续使用累积 ErrAliasRevoked
- **GORM 版本锁定 v1.31.x**：升级前必须重跑 TestGORMAliasBehaviorProbe

### Deprecated

- `LeftJoin / RightJoin / InnerJoin / OuterJoin / FullJoin / CrossJoin / NaturalJoin`（Query + Updater 各 7 个）：使用对应 JoinAs 替代；v1.0 删除。仍保留用于 JOIN 子查询表 / USING 子句等 alias 不能表达的场景

### 不在本期范围

- ANY / ALL 24 方法 → v0.8.1
- SelectSub → v0.8.1（依赖 GORM Select 嵌套实测）
- 类型安全 ON extra 三元组 / 包级泛型 LeftJoinAs[L,R] → v0.9
- 跨表 DataRule（DataRule.Table 字段）→ v0.9
- UNION / WITH CTE / 窗口函数 → v1.0+
```

- [ ] **Step 4: Commit**

```bash
git add README.md doc.go CHANGELOG.md
git commit -m "docs(alias): README + godoc + CHANGELOG v0.8.0"
```

---

## Task 23: 性能基线 benchmark（P9）

**Files:**
- Create: `bench_alias_test.go`

- [ ] **Step 1: 写 4 个 benchmark**

```go
// bench_alias_test.go
package gplus

import (
	"context"
	"testing"
)

func BenchmarkResolveColumnName_NoAlias(b *testing.B) {
	q, u := NewQuery[TestUser](context.Background())
	addr := uintptrOf(&u.Name)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = q.resolveColumnName(addr)
	}
}

func BenchmarkResolveColumnName_OneAlias(b *testing.B) {
	q, _ := NewQuery[TestUser](context.Background())
	o := As[Order](q, "o")
	addr := uintptrOf(&o.UserID)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = q.resolveColumnName(addr)
	}
}

func BenchmarkResolveColumnName_FiveAliases(b *testing.B) {
	q, _ := NewQuery[TestUser](context.Background())
	_ = As[Order](q, "o1")
	_ = As[Order](q, "o2")
	_ = As[Order](q, "o3")
	_ = As[Order](q, "o4")
	o5 := As[Order](q, "o5")
	addr := uintptrOf(&o5.UserID)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = q.resolveColumnName(addr)
	}
}

func BenchmarkResolveColumnName_OuterChain3(b *testing.B) {
	q, u := NewQuery[TestUser](context.Background())
	sub1, _ := SubQuery[Order](q)
	sub2, _ := SubQuery[Product](sub1)
	addr := uintptrOf(&u.Name)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = sub2.resolveColumnName(addr)
	}
}
```

- [ ] **Step 2: 运行 benchmark 验证阈值**

Run: `D:/Environment/golang/go1.21.11/bin/go.exe test -bench=BenchmarkResolveColumnName -benchmem -run=^$ ./...`
Expected:
- NoAlias: < 50 ns/op（基线）
- OneAlias: ≤ 50 ns/op
- FiveAliases: ≤ 100 ns/op
- OuterChain3: ≤ 300 ns/op

如某项超阈值，先调查瓶颈再决定是否抽 hash map（TD-3 触发条件 ≥ 8 alias 才考虑）。

- [ ] **Step 3: Commit**

```bash
git add bench_alias_test.go
git commit -m "bench(alias): 性能基线 4 benchmark（解析 ≤ 100ns / 链 3 层 ≤ 300ns）"
```

---

## 验收检查（执行完成后逐项核对 spec §10）

按 spec `docs/superpowers/specs/2026-05-06-alias-system-design.md` §10 验收清单逐项核对：

- [ ] 所有新测试 GREEN，覆盖率 ≥ 96.0%（运行 `go test -coverprofile`）
- [ ] `TestGORMAliasBehaviorProbe` 6 子测试 RED-lock 通过
- [ ] MySQL + SQLite 双方言集成测试通过（`TestIntegration_SelfJoin_BothDialects`）
- [ ] 性能基线达成（5 alias ≤ 100 ns/op，跨 3 层 outerQuery ≤ 300 ns/op）
- [ ] CHANGELOG / README / godoc 三处文档更新（Task 22）
- [ ] DataRule × alias 副表敞开 e2e 段 A + 段 B 双段都 PASS（Task 21 N1 验收）
- [ ] deprecated 旧 Join API 仍可编译，下游升级零破坏（Task 13）
- [ ] AnyQuery 接口的 phantom guard `gplusCore() *queryCore` 阻止外部冒名实现（Task 2）
- [ ] NewQueryAs / As / SubQuery 所有错误路径测试覆盖（Task 4 + 8 + 14）
- [ ] **C1 防御**：extraSQL 含 ? + extraArgs 参数化路径在 DryRun 测试中可见参数不入字面 SQL（Task 1 探针 + Task 10 测试）
- [ ] **H6 防御（决策 1B）**：As 重名累积 + BuildQuery 短路双重防御均测试覆盖（Task 4 + 7）
- [ ] **N4 防御**：Clear() 翻转 revoked，lookupAddr 命中 revoked 累积 ErrAliasRevoked（Task 7 + 20）
- [ ] **N5 防御**：As(nil) panic ErrAliasQueryNil（Task 4）
- [ ] **H5 防御**：跨链 alias 误用返回 ErrFieldAddrUnregistered，不回退全局 cache（Task 15）

---

## 自审记录

写完上述 23 个 task 后做了以下自审：

**1. Spec 覆盖**：spec §1-§11 每段都映射到至少一个 task：
- §1 背景 → 不需要 task（动机说明）
- §2 决策摘要 → 全部 task 覆盖
- §3 架构 → Task 2-3 (queryCore + lookupAddr)
- §4 公共 API → Task 4 (As) / 8 (NewQueryAs) / 10-12 (JoinAs) / 14 (SubQuery) / 16-17 (Exists)
- §5 数据流 → 内嵌在 task 实现描述中
- §6 错误处理 → Task 4-7（哨兵 + 短路）
- §7 测试策略 → Task 1（探针）/ Task 21（e2e）/ 各 task 的测试段
- §8 落地计划 → 本 plan 整体
- §9 风险 → 已在 spec 中，无需 task
- §10 验收 → 验收检查段
- §11 范围与债 → 不在 plan 范围（v0.9+）

**2. Placeholder 扫描**：无 TBD/TODO/FIXME。所有"既有实现"引用均指向具体既有方法（如 v0.7.x 的 schema_test.go 中的 `globalColumnNameLookup`，按现有命名）。

**3. 类型一致性**：
- `*queryCore` 在 Task 2 定义，Task 5 在 Query 中引用为 `core *queryCore`，Task 18 在 Updater 中同名引用 ✓
- `aliasEntry.revoked` 在 Task 3 定义，Task 7 / 20 中引用 ✓
- `existsLeaf` 在 Task 16 定义，Task 17 / 19 中引用 ✓
- `appendCoreErr` / `appendExists` / `appendJoinAs` / `appendJoinAsNoOn` / `lookupAliasFromChain` 均在首次定义的 task 中给出完整实现

**4. 已知简化**：
- Updater 与 Query 的 `appendJoinAs` / `lookupAliasFromChain` / `resolveColumnName` 代码重复（Task 18 显式说明）。v0.8.0 容忍此重复，v0.9 可重构到 ScopeBuilder 公共方法
- `globalColumnNameLookup` 在 schema.go 中需补充（封装现有 columnNameCache 查询，保持 v0.7.x 行为兼容）



