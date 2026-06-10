# FirstOrUpdate 数据权限缺口修复（UPDATE 侧 + 重读侧）

日期：2026-06-10
来源：2026-06-10 全项目审计确认问题 #2
类型：bug 修复（先列复现 AC，测试先红）

## Goal

`FirstOrUpdate`（repository.go:676）是唯一一条 UPDATE 语句本身不带 DataRule 条件的更新路径：

1. 查找阶段 `q.DataRuleBuilder()` 已接入 ✅
2. 更新阶段 `tx.Model(&data).Scopes(u.BuildUpdate())` —— `u` 从未调 `u.DataRuleBuilder()`，UPDATE WHERE 不含数据权限条件 ⚠️
3. 重读阶段 `tx.First(&fresh, pkVal)` —— 裸主键重读，不含数据权限条件 ⚠️

与 `UpdateByCondTx`（:528 调 `u.DataRuleBuilder()`）不对称。当前靠"主键来自合法查询"的隐式前提兜底，存在：
- 事务内 SELECT→UPDATE 之间的 TOCTOU 窗口（并发改 tenant_id 时 UPDATE 改到越权行）
- q 与 u 用不同 ctx 构建时，u 侧 DataRule 被静默忽略（确定性复现，见 AC-1）
- UPDATE 把行改出权限范围后，裸主键重读把越权数据回显给调用方（确定性复现，见 AC-3）

修复：UPDATE 侧补 `u.DataRuleBuilder()`；重读侧改为带 DataRule 的主键查询。

## Tech Stack

Go 1.24+ / GORM / SQLite 内存库（复用 `repo_datarule_byid_test.go` 的 `tenantUser` + `setupTenantDB` + `ctxWithTenantRule` + `insertTenantUsers` helper）。

## 行为决策（显式记录）

**D-1 事务回滚语义**：重读防护后，若 UPDATE 把行改出 DataRule 可见范围（如规则 `tenant_id=1` 下 `Set(&m.TenantID, 2)`），重读返回 `gorm.ErrRecordNotFound` → 事务整体回滚，**更新被撤销**。这比 `UpdateByCond`（允许 SET 改 tenant_id，affected=1）更严格，是有意为之：DataRule 生效时禁止经 FirstOrUpdate 将行移出自身权限范围（防权限自提升）。该差异写入方法 doc 注释。

**D-2 重读的 DataRule 取自 q.Context()**：重读是"读"的延续，与查找阶段同源（q 的 ctx）；UPDATE 侧用 u 自身 ctx（与 UpdateByCondTx 对称）。正常用法 q/u 同 ctx，两者一致。

**D-3 创建路径不防护**：未找到记录时 `tx.Create(defaults)` 不校验 defaults 是否满足 DataRule——与 Save/InsertSelect"结构性写入不被隔离过滤"的既有设计一致，不在本次范围。

## Acceptance Criteria

- **AC-1（复现·UPDATE 侧，先红）**：库中行 `{Name:"Bob", TenantID:2}`；`q` 用**无规则** ctx 构建且 `Eq(&m.ID, bobID)`；`u` 用 `ctxWithTenantRule(1)` 构建且 `Set(&m.Name, "HACKED")`；defaults 非 nil。调用 `FirstOrUpdate(q, u, defaults)` → 返回 `err == nil`、`created == false`，**DB 中该行 Name 仍为 "Bob"**（UPDATE WHERE 含 `tenant_id = 1`，affected=0）。现状红：行被改为 "HACKED"。
- **AC-2（回归·正常路径）**：行 `{Name:"Alice", TenantID:1}`；q/u 均用 `ctxWithTenantRule(1)` 构建，`q.Eq(&m.ID, aliceID)`，`u.Set(&m.Name, "Alice2")` → `err == nil`、`created == false`、DB 行 Name == "Alice2"、返回 `data.Name == "Alice2"`。修复前后均绿（防回归）。
- **AC-3（复现·重读侧，先红）**：行 `{Name:"Alice", TenantID:1}`；q/u 均用 `ctxWithTenantRule(1)`，`q.Eq(&m.ID, aliceID)`，`u.Set(&m.TenantID, 2)`（把行改出权限范围）→ 返回 `errors.Is(err, gorm.ErrRecordNotFound)`、`created == false`，且**事务回滚后 DB 行 TenantID 仍为 1**（D-1）。现状红：`err == nil` 且返回 `data.TenantID == 2`（越权回显），DB 行被改为 2。
- **AC-4（复现·u 侧规则错误传播，先红）**：q 用无规则 ctx；u 用携带非法规则 `{Column: "dept(id)", Condition: "=", Value: "1"}` 的 ctx → 返回 `err != nil`（u.DataRuleBuilder 拒绝含括号列名），**不发任何 UPDATE**，DB 无变更。现状红：err == nil，规则被静默忽略，更新照常执行。

## Task 1：复现测试（RED）→ 修复（GREEN）

**Files:**
- 新增 `repo_firstorupdate_datarule_test.go`（4 个测试函数 1:1 对应 AC）
- 修改 `repository.go` `FirstOrUpdate`：
  - 前置检查区补 `u.DataRuleBuilder().GetError()`（紧随 :692 的 q 侧检查，写法与 UpdateByCondTx :528 逐字对称）
  - 重读改为：`rq, _ := NewQuery[T](q.Context())` + `rq.DataRuleBuilder()` 后 `tx.Scopes(rq.BuildCount()).First(&fresh, pkVal)`（rq 的规则与 :692 已校验的 q 同 ctx 同源，必无新错误，注释说明）
  - 方法 doc 注释补 D-1 行为说明
- 无主键降级路径（:715 `q.BuildCount()` 重读）已含 DataRule（q 侧已合并），不动

**AC:** AC-1, AC-2, AC-3, AC-4

## Out of Scope

- FirstOrCreate 创建路径的 defaults 校验（D-3）
- FirstOrCreate/FirstOrUpdate 的 Tx 变体补齐（审计问题 #5，另行排期）
- IncrBy alias 解析（审计问题 #4，决议为不修代码、补 doc 注释，与本 spec 同批提交但无 AC）
