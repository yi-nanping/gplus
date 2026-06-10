# 错误累积双轨制统一：Build* 窄腰统一短路（审计问题 #3，方案 2）

日期：2026-06-10
来源：2026-06-10 全项目审计确认问题 #3
类型：行为变更 + 一致性修复（含复现 AC，测试先红）

## Goal

`errs`（本体错误，v0.6）与 `core.errs`（alias 链级错误，v0.8）双轨并存有结构必然性：
core 是跨对象共享通道（子查询/alias 实例共用），且懒初始化可为 nil（And/Or 闭包的临时
sub 无 core），两轨不可互相合并。问题是**进出规则从未统一**，五轮迭代后：

**写入漂移**：`Query.appendExists` 写 `q.errs`（query.go:1367/1372），`Updater.appendExists`
写 `core.errs`（update.go:191/198）——同名方法不同桶，且 update.go:187 注释声称"与 InSub
等一致"为不实；`InSub` 双侧写本体 errs。

**消费缺口**：短路保护矩阵 10 格只盖 2 格（仅 Query.BuildQuery/BuildQueryDB × core.errs，
v0.8 决策 1B）。其余路径带错仍生成 SQL——**错误条件被静默丢弃 = fail-open**（少 WHERE
多改多删）。

**机制目的回归**（本设计的判断标准）：errs 机制存在的唯一理由是链式 API 无法逐调用报错，
必须"执行前拦截"。唯一能保证"必拦"的位置是所有 SQL 必经的窄腰 `ScopeBuilder.Build*`。
终端方法 / ToDB / debug 路径的 `GetError()` 前置检查降级定性为**报错体验优化**（错误作为
返回值比塞进 db.Error 更直接），不再承担防线职责——防线和礼貌分离。

**暴露面事实**（已核实）：`ToDB` 入口已查完整 `GetError()`（query.go:388），下游 gvs-server
全部 5 处直接用法均走 ToDB 且无裸 `.Scopes(q.Build*())`，本修复属承诺统一而非补活雷；
子查询错误在 build 期有权威检查（builder.go:469/507），延迟调用语义不受影响。

## Tech Stack

Go 1.24+ / GORM / SQLite 内存库。同包白盒测试（可直接断言 errs/core.errs 桶与注入 core 错误）。

## 设计

1. **字段下沉**：`errs []error` 与 `core *queryCore` 从 `Query[T]`/`Updater[T]` 下沉至
   `ScopeBuilder`（均未导出；Go 字段提升使既有 `q.errs`/`u.core` 点引用原样编译；
   复合字面量构造点不能用提升字段名，需机械改写为 `ScopeBuilder{errs: ..., core: ...}` 嵌套
   ——已知构造点 query.go:30/47/926/979/1017 + update.go NewUpdater/And/Or sub，以编译错误清零为准）
2. **窄腰短路**：`ScopeBuilder.BuildQuery/BuildCount/BuildUpdate/BuildDelete` 返回的闭包
   入口统一检查 `b.errs` 与 `b.core.errs`（core 判 nil），非空则
   `session := db.Session(&gorm.Session{}); session.AddError(errors.Join(全部错误)); return session`，
   不生成 SQL（复制既有 Query.BuildQuery override 的 Session 隔离模式）
3. **删旧 override**：`Query[T].BuildQuery` 的 core.errs 专项短路删除（基类覆盖更大集合）；
   `BuildQueryDB` 保留 API，内部检查改为统一双桶
4. **写入对齐**：`Updater.appendExists` 的 nil 与 subErr 透传改写 `u.errs`（对齐
   Query.appendExists 与双侧 InSub），update.go:187 注释同步修正
5. **规则文档化**（CLAUDE.md 错误处理模式段）：本体错误 → `ScopeBuilder.errs`；链级错误 →
   `core.errs`；唯一强制拦截点 = Build* 闭包；`GetError()` 前置检查 = 报错体验优化

**⚠️ 行为变更（CHANGELOG 发版须标注）**：直接 `.Scopes(q.Build*())` / `.Scopes(u.Build*())`
且不自查 GetError 的调用方，从"执行条件残缺的 SQL"变为"db.Error 返回聚合错误、不执行"
（fail-open → fail-closed）。Repository / ToDB / debug 路径无行为变化（GetError 前置已拦，
短路分支在这些路径不可达）。若存在依赖"带错仍出 SQL"的既有测试，按新语义调整并在
commit message 列明。

## Acceptance Criteria

- **AC-1（复现·Query BuildCount 本体错短路，先红）**：`q.Eq(&orphan.Age, 1)`（orphan 为
  非规范实例，地址未注册 → 错误累积、条件被丢）+ `q.Eq(&m.Age, 10)`（有效）；
  `db.Model(&TestUser{}).Scopes(q.BuildCount()).Count(&n)` → `result.Error != nil`，COUNT
  不执行。现状红：Error == nil，按丢失条件后的 WHERE 正常计数。
- **AC-2（复现·Updater BuildUpdate 本体错短路 = fail-open 实证，先红）**：库中两行
  `{Name:"A", Age:10}` / `{Name:"B", Age:20}`；`u.Set(&m.Score, 99).Eq(&m.Age, 10).Eq(&orphan.Name, "B")`
  （调用方意图 WHERE age=10 AND name='B' → 应命中 0 行；坏指针条件被丢后实际 WHERE age=10）；
  `db.Model(&TestUser{}).Scopes(u.BuildUpdate()).Updates(u.setMap)` → `result.Error != nil`、
  `RowsAffected == 0`、两行 Score 均不变。现状红：A 行（age=10）Score 被改为 99——改了
  调用方意图之外的行。
- **AC-3（复现·Updater 链级错短路，先红）**：同包注入 `u.core.appendErr(ErrAliasRevoked)`
  （本体 errs 保持空，隔离桶）；u 含有效 Set + 有效 Eq 条件；
  `db.Model(...).Scopes(u.BuildUpdate()).Updates(u.setMap)` → `result.Error` 满足
  `errors.Is(err, ErrAliasRevoked)`，DB 无行变更。现状红：正常执行 UPDATE。
- **AC-4（回归·决策 1B 保持）**：As 重名场景 `repo.List(q)` 返回含 `ErrAliasDuplicate` 的
  错误且不执行 SQL——既有 alias_test.go:217 一带测试在重构后保持绿，证明删除旧 override
  后基类短路完整接管。
- **AC-5（回归·无错路径零变化）**：全量既有测试套（覆盖率 95.5%，含大量字节级 SQL 断言）
  通过；另新增一条显式断言：无错 `q.Eq(&m.Age, 10).Order(&m.ID, true)` 经 DryRun 生成的
  SQL 与重构前字节一致（`SELECT * FROM ... WHERE "age" = ... ORDER BY "id"` 形态锁定）。
- **AC-6（写入对齐·桶断言）**：`u.Exists(nil)` 后，同包断言 `len(u.errs) == 1` 且
  `errors.Is(u.errs[0], ErrSubqueryNil)`、`u.core.errs` 为空；`u.GetError()` 非 nil（对外
  可见性不变）。现状红：错误在 `u.core.errs`。
- **AC-7（Clear 后复用）**：`q.Eq(&orphan.Age, 1)`（带错）→ `q.Clear()` →
  `q.Eq(&m.Age, 10)` → `db.Model(...).Scopes(q.BuildQuery()).Find(&rows)` →
  `result.Error == nil` 且正常返回结果（短路状态随 Clear 解除）。

## Task 1：字段下沉 + Build* 窄腰统一短路（RED → GREEN）

**Files:**
- `builder.go`：ScopeBuilder 增加 errs/core 字段；四个 Build* 闭包入口统一短路；
  `ScopeBuilder.Clear()` 清 errs（core 的 revoke/清错保留在 Query/Updater.Clear，提升引用原样编译）
- `query.go`：删 Query 结构体 errs/core 字段声明；构造点（:30/:47/:926/:979/:1017）改
  ScopeBuilder 嵌套初始化；删 BuildQuery override 的 core 专项短路；BuildQueryDB 改统一检查
- `update.go`：删 Updater 结构体 errs/core 字段声明；NewUpdater/And/Or 构造点同改
- 新增 `error_track_test.go`：AC-1/2/3/4(引用既有)/5/7 测试
- 既有依赖"带错出 SQL"的测试（如有）按新语义调整

**AC:** AC-1, AC-2, AC-3, AC-4, AC-5, AC-7

## Task 2：appendExists 写入对齐

**Files:**
- `update.go`：appendExists 的 nil 分支与 subErr 透传改写 `u.errs`；:187 注释修正
- `error_track_test.go`：补 AC-6 测试

**AC:** AC-6

## Task 3：规则文档化（无 AC，文档变更豁免）

**Files:**
- `CLAUDE.md`：错误处理模式段补双轨规则与唯一拦截点定性
- `CHANGELOG.md`：Unreleased 段记录 ⚠️ 行为变更

## Out of Scope

- ToDB / 终端方法 GetError 前置检查的删除（保留，报错体验优化）
- GetError() 前缀文案统一（"query builder" / "updater" 维持各自前缀）
- 死代码清理（ColumnInfo/KeyAnd/KeyOr，审计问题 #6 另行处理）
