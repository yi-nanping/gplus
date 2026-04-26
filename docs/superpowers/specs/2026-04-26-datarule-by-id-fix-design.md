# Spec: DataRule by-ID 安全修复（v0.5.1 patch）

**日期**：2026-04-26
**版本**：v0.5.1（patch）
**类型**：安全 bug 修复 + 错误类型一致性

## 背景与问题

v0.2.0 修复了 `UpdateByCondTx` / `DeleteByCondTx` 未应用 `DataRule` 的安全问题。但**所有"按主键写"的方法系统性遗漏了相同修复**，导致 `ctx` 中设置的数据权限规则对这条路径完全无效，存在跨租户改/删/恢复风险。

读路径已应用 DataRule（`GetById` 在 `repository.go:230` 调用 `DataRuleBuilder`），写路径不应用，造成读写权限不对称。

### 受影响的方法

| 文件:行号 | 方法 | 攻击模型 |
|---|---|---|
| `repository.go:421-447` | `UpdateByIdTx` | 攻击者构造 entity 含其他租户 ID，UPDATE 跨越租户 |
| `repository.go:728-742` | `UpdateByIdsTx` | 批量传入跨租户 ID，全部命中 |
| `repository.go:518-521` | `DeleteByIdTx` | 单 ID 跨租户 DELETE |
| `repository.go:790-796` | `DeleteByIdsTx` | 批量 ID 跨租户 DELETE |
| `repository.go:887-905` | `RestoreTx` | 跨租户恢复软删记录 |

### 附带 LOW 修复

`debug.go:85` `r.ToUpdateSQL(nil)` 返回 `ErrQueryNil`（语义属于 Query），与 `Updater[T].ToSQL` 和其他所有 Updater 方法约定的 `ErrUpdateEmpty` 不一致。`errors.Is(err, ErrUpdateEmpty)` 会漏掉这条路径。

## 设计

### 共享辅助函数

新增 `applyDataRuleFromCtx`，把 ctx 中 `DataRuleKey` 提取的 `[]DataRule` 追加为 `db.Where`：

```go
// 位置：repository.go（私有方法）
func (r *Repository[D, T]) applyDataRuleFromCtx(ctx context.Context, db *gorm.DB) (*gorm.DB, error)
```

实现要点：
- 从 `ctx.Value(DataRuleKey)` 读取 `[]DataRule`，无规则时直接返回原 `db`
- 复用 `builder.go` 现有 `validDataRuleColumn` 白名单正则做列名校验
- 校验失败返回错误（与 `Query.DataRuleBuilder.GetError()` 行为一致）
- 通过反射 `Statement.Parse(new(T))` 拿到表名做 `tableName.column = ?` 限定，避免与 join 列重名冲突

可考虑在 `builder.go` 中提取一个共享内部函数 `extractDataRules(ctx) ([]DataRule, error)`，被 `Query.DataRuleBuilder` 与 `applyDataRuleFromCtx` 共用，避免逻辑重复。

### 5 处改动模板

每个方法改造模式一致：

```go
func (r *Repository[D, T]) DeleteByIdTx(ctx context.Context, id D, tx *gorm.DB) (int64, error) {
    db, err := r.applyDataRuleFromCtx(ctx, r.dbResolver(ctx, tx))
    if err != nil {
        return 0, err
    }
    result := db.Delete(new(T), id)
    return result.RowsAffected, result.Error
}
```

对 `UpdateByIdTx`（含乐观锁路径）：在 `db := r.dbResolver(ctx, tx)` 后立即套 `applyDataRuleFromCtx`，再分支进入乐观锁或普通 Updates，确保两条路径都受 DataRule 约束。

### debug.go 改动

```go
func (r *Repository[D, T]) ToUpdateSQL(u *Updater[T]) (string, error) {
    if u == nil {
        return "", ErrUpdateEmpty  // 由 ErrQueryNil 改为 ErrUpdateEmpty
    }
    return u.ToSQL(r.db)
}
```

## 测试策略

新建 `repo_datarule_byid_test.go`，6 个表驱动子测试：

1. **TestDataRule_UpdateById_Blocked** — 设置 `DataRule{Column:"tenant_id", Value:1}`，对 `tenant_id=2` 的记录调用 `UpdateById`，断言 `affected == 0` 且记录未变更
2. **TestDataRule_UpdateByIds_Blocked** — `[]uint{ownTenantID, otherTenantID}`，断言只有 `ownTenantID` 被改
3. **TestDataRule_DeleteById_Blocked** — 跨租户 `DeleteById`，断言记录仍存在
4. **TestDataRule_DeleteByIds_Blocked** — 混合 ID 列表，断言只删除了同租户记录
5. **TestDataRule_Restore_Blocked** — 跨租户 `Restore` 软删除记录，断言 `deleted_at` 未恢复
6. **TestToUpdateSQL_NilReturnsErrUpdateEmpty** — `errors.Is(err, ErrUpdateEmpty)` 通过

每个测试用 `setupAdvancedDB(t)` 创建带 `tenant_id` 的临时模型，预插两租户数据，断言行级隔离。

## 兼容性

- **纯增强**：现有测试无需改动，所有未使用 DataRule 的调用路径行为不变
- `applyDataRuleFromCtx` 检测 `ctx` 无 `DataRuleKey` 或规则切片为空时立即返回原 `db`，零额外开销
- 错误返回类型改动（`ToUpdateSQL` nil 路径）属于"修正未文档化的内部不一致"，调用方若误依赖 `ErrQueryNil` 极少见，CHANGELOG 标注 fix

## 风险

| 风险 | 缓解 |
|---|---|
| 表无 DataRule.Column 指定的列 | 与 v0.2.0 by-Cond 修复行为一致：SQL 执行期报错，文档已说明 |
| 乐观锁路径与 DataRule 叠加产生意外 affected=0 | 测试覆盖：`affected == 0` 时区分 `ErrOptimisticLock`（命中但版本不匹配）与 DataRule 拦截（根本未命中）。当前两者都返回 `ErrOptimisticLock`，文档需补一条说明 |
| 行为变更打破依赖"by-ID 不应用 DataRule"的下游代码 | 这是**安全修复**，非破坏性兼容性变更——下游若依赖此行为是依赖未文档化的 bug |

## 发布计划

- v0.5.1 patch 版本
- CHANGELOG 单独条目说明 5 处方法的安全修复
- README 安装命令版本号同步到 v0.5.1
- 单独 git tag

## 后续不在本期范围

- INSERT 系列（Save / SaveBatch / Upsert / UpsertBatch / InsertOnConflict / InsertBatchOnConflict）的 DataRule 处理。需独立设计 auto-fill 机制（mybatis-plus 拦截器风格）。该项作为未来候选 feature，不在 v0.5.1 范围
