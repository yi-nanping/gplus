// alias.go - v0.8.0 alias 体系核心类型与函数
package gplus

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
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
	// aliases：alias name → entry 指针
	// 用 *aliasEntry 而非 aliasEntry（spec §3.3 原文是值 map）的理由：
	// - 支持 N4 revoked 翻转：Clear() 时原地修改 entry 标志，无需赋值回 map
	// - lookupAddr 命中 revoked alias 可直接返回错误，语义清晰
	// - GC 安全等价：entry 指针持有 instance 强引用，与值 map 生命期同步
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

// appendErr 累积错误（与 v0.6.0 errs 哲学一致）
func (c *queryCore) appendErr(err error) {
	if err != nil {
		c.errs = append(c.errs, err)
	}
}

// addAlias 注册一个 alias 实例。
// 不做 nameRegexp 校验（由 As 包级函数完成）；不做 nil 校验（内部方法）。
// 重名直接返回 ErrAliasDuplicate（不累积），由 As 决定如何处理。
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

// lookupAddr 反向查找：给定字段地址，返回 alias name 和列名。
//
// H2：offset 必须在 schema 已知字段集合内（防 GC 重用 / size 巧合误命中）。
// N4：revoked entry 直接返回未命中（让调用方累积 ErrAliasRevoked）。
func (c *queryCore) lookupAddr(addr uintptr) (alias, col string, ok bool) {
	for _, entry := range c.aliases {
		if entry.addrLow <= addr && addr < entry.addrHigh {
			if entry.revoked {
				// N4：revoked，由调用方 resolveColumnName 累积 ErrAliasRevoked
				return "", "", false
			}
			offset := addr - entry.addrLow
			// 使用 gorm tag + COLUMN label 与 registerModel/resolveColumnName 保持一致
			// 直接使用 entry.instance（已存储的实例），避免每次 lookup 的 reflect.New 堆分配
			schema := reflectStructSchema(entry.instance, "gorm", "COLUMN")
			if name, found := schema[offset]; found {
				return entry.name, name, true
			}
			// H2：区间命中但 offset 不在 schema，视为未命中
		}
	}
	return "", "", false
}

// hadRevokedHit 辅助：检查给定地址是否落在某个 revoked entry 的区间内。
// 用于 resolveColumnName 在 lookupAddr 返回 false 后累积 ErrAliasRevoked。
func (c *queryCore) hadRevokedHit(addr uintptr) bool {
	for _, entry := range c.aliases {
		if entry.addrLow <= addr && addr < entry.addrHigh && entry.revoked {
			return true
		}
	}
	return false
}

// 注意：*Query[T] 和 *Updater[T] 的 gplusCore() 方法实现在 query.go / update.go
// 编译期断言放在它们各自所在文件，避免循环依赖

// As 在 q（含 outerQueryRef 链）上注册一个 X 类型的 alias 实例。
//
// 错误处理：
//   - q == nil：panic ErrAliasQueryNil（N5：API 入口编程错误，无 q 可挂错误）
//   - name 不符合白名单正则：累积 ErrAliasInvalidName，返回规范单例 fallback
//   - name 已在链中存在：累积 ErrAliasDuplicate，返回首次注册实例（决策 1B）
//
// 返回的 *X 实例字段地址独立于规范单例，仅用于取字段地址。
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

	// 沿 outerQueryRef 链检查是否已有同名 alias（决策 1B）
	cur := q
	for cur != nil {
		curCore := cur.gplusCore()
		if existing, ok := curCore.aliases[alias]; ok {
			if inst, ok2 := existing.instance.(*X); ok2 && inst != nil {
				// 同类型重名：累积错误返回首次实例（决策 1B）
				core.appendErr(ErrAliasDuplicate)
				return inst
			}
			// 同名但类型不一致：编程错误，立即 panic
			panic(fmt.Errorf("gplus: alias %q already registered as %T, but As called with different type %T",
				alias, existing.instance, (*X)(nil)))
		}
		next := curCore.outerQueryRef
		if next == nil {
			break
		}
		cur = next
	}

	// 创建独立 alias 实例（reflect.New，地址独立于规范单例）
	typ := reflect.TypeOf((*X)(nil)).Elem()
	instancePtr := reflect.New(typ)
	instance := instancePtr.Interface().(*X)
	if err := core.addAlias(alias, typ, instance); err != nil {
		core.appendErr(err)
	}
	return instance
}
