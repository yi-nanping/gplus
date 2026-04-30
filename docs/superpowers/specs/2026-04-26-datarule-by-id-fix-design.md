# Spec: DataRule by-ID 安全修复（v0.6.0 minor）

**日期**：2026-04-26（2026-04-30 修订）
**版本**：v0.6.0（minor，行为变更）
**类型**：安全 bug 修复 + 行为变更

## 修订记录

- 2026-04-30 第一轮：经 4 名专家审计后修订。范围由 5 个写方法扩展到 7 个（补 2 个读路径）；实施路线由"新增 applyDataRuleFromCtx 辅助函数"改为"复用 Query[T].DataRuleBuilder"（避免重复实现 DataRule → SQL 映射逻辑）；版本号由 v0.5.1 patch 升至 v0.6.0 minor（行为变更）；ToUpdateSQL nil 错误改用 `fmt.Errorf("%w: %w", ErrUpdateEmpty, ErrQueryNil)` 双 %w wrap 保持 errors.Is 双向兼容（Go 1.20+ 多 wrap 语法，本项目 go 1.24 可用）。
- 2026-04-30 第二轮：3 位专家复审后补强。明确 `debug.go` 须新增 `fmt` import；测试 setup 须自建 `setupTenantDB`（不复用 `setupTestDB[T]`，因主键类型硬编码 int64 与 tenantUser.ID uint 不兼容）；测试增加 2 个子测试场景（非法 DataRule column 触发 error、无 DataRule ctx 零影响回归）；godoc 须显式警示"启用 DataRule 时 affected=0 不应无条件重试"。

## 背景与问题

v0.2.0 修复了 `UpdateByCondTx` / `DeleteByCondTx` 未应用 `DataRule` 的安全问题。但**所有"按主键读 / 写"的方法系统性遗漏了相同修复**，导致 `ctx` 中设置的数据权限规则对这些路径完全无效，存在跨租户读 / 改 / 删 / 恢复风险。

读路径 List/GetOne/Page/Count/Exists/Last 已经在 `q.DataRuleBuilder().GetError()` 处应用 DataRule（参考 `repository.go:230` `ExistsTx`）；但 `GetByIdTx` / `GetByIdsTx` 路径直接走 `db.First(&data, id)`，未经过 `Query[T]`，DataRule 完全未应用。这造成读路径自身的不对称（"按条件读"安全 vs "按主键读"不安全），加上写路径的 5 个 by-ID 方法，构成完整的安全裂缝。

### 受影响的方法（共 7 个）

| 文件:行号 | 方法 | 攻击模型 |
|---|---|---|
| `repository.go:170` | `GetByIdTx` | **读**：攻击者猜 ID 跨租户读取记录 |
| `repository.go:870`（实际行号实施时核对） | `GetByIdsTx` | **读**：批量猜 ID 跨租户读 |
| `repository.go:421-447` | `UpdateByIdTx` | 攻击者构造 entity 含其他租户 ID，UPDATE 跨越租户 |
| `repository.go:728-742` | `UpdateByIdsTx` | 批量传入跨租户 ID，全部命中 |
| `repository.go:518-521` | `DeleteByIdTx` | 单 ID 跨租户 DELETE |
| `repository.go:790-796` | `DeleteByIdsTx` | 批量 ID 跨租户 DELETE |
| `repository.go:887-905` | `RestoreTx` | 跨租户恢复软删记录 |

### 附带 LOW 修复

`debug.go:85` `r.ToUpdateSQL(nil)` 返回 `ErrQueryNil`（语义属于 Query），与 `Updater[T].ToSQL` 和其他所有 Updater 方法约定的 `ErrUpdateEmpty` 不一致。`errors.Is(err, ErrUpdateEmpty)` 会漏掉这条路径。

## 设计

### 实施路线：复用 `Query[T].DataRuleBuilder`（方案 D）

不新增独立辅助函数。每个方法内部构造一个临时 `Query[T]`，复用现有 `DataRuleBuilder` 链路：

```go
// 模板：所有 7 个方法的统一改造模式
q, _ := NewQuery[T](ctx)
if err := q.DataRuleBuilder().GetError(); err != nil {
    return /* zero value */, err
}
// 把 q.BuildXxx() 作为 scope 套到原 db 上：
db := r.dbResolver(ctx, tx).Scopes(q.BuildDelete()) // 或 BuildUpdate / BuildQuery
result := db.Delete(new(T), id) // 或 First / Updates 等
```

**为什么选方案 D**（4 位专家中 architect 强烈推荐）：

- **单一真相源**：DataRule → SQL 映射逻辑只存在于 `query.go:applyDataRule` 一处，by-Cond 路径与 by-ID 路径共享，避免重复维护
- **未来不遗漏**：新增 by-ID 写方法只需"构造 Query[T] 即免费获得 DataRule"，不会重蹈本次系统性遗漏的覆辙
- **零反射额外开销**：原 spec 提议 `Statement.Parse(new(T))` 拿表名做 `tableName.column = ?` 限定，但现有 `applyDataRule` 不做此限定（直接传裸列名），by-ID 写路径无 join、不会有列名冲突，反射纯属过度设计
- **复用已审计的白名单 / 错误模型**：`validDataRuleColumn` 正则、`SQL`/`USE_SQL_RULES` 黑名单、错误聚合到 `q.errs` 已经过 7 轮审计稳定

### Updater 路径无需改造

`UpdateByIdsTx` 接受 `*Updater[T]` 参数，但 DataRule 走的是临时 Query，不依赖 Updater 是否实现 `DataRuleBuilder`。两个 scope 用 `db.Scopes(q.BuildUpdate(), u.BuildUpdate())` 串联即可。

### 7 处改动（具体代码模板）

#### 1. GetByIdTx
```go
func (r *Repository[D, T]) GetByIdTx(ctx context.Context, id D, tx *gorm.DB) (data T, err error) {
    q, _ := NewQuery[T](ctx)
    if err = q.DataRuleBuilder().GetError(); err != nil {
        return
    }
    err = r.dbResolver(ctx, tx).Scopes(q.BuildQuery()).First(&data, id).Error
    return
}
```

#### 2. GetByIdsTx
（结构同上，`Find(&data, ids)` 替换 `First`）

#### 3. DeleteByIdTx
```go
func (r *Repository[D, T]) DeleteByIdTx(ctx context.Context, id D, tx *gorm.DB) (int64, error) {
    q, _ := NewQuery[T](ctx)
    if err := q.DataRuleBuilder().GetError(); err != nil {
        return 0, err
    }
    result := r.dbResolver(ctx, tx).Scopes(q.BuildDelete()).Delete(new(T), id)
    return result.RowsAffected, result.Error
}
```

#### 4. DeleteByIdsTx
（结构同 3，`Delete(new(T), ids)`）

#### 5. UpdateByIdTx（乐观锁路径 + 普通路径都套）
```go
func (r *Repository[D, T]) UpdateByIdTx(ctx context.Context, entity *T, tx *gorm.DB) error {
    q, _ := NewQuery[T](ctx)
    if err := q.DataRuleBuilder().GetError(); err != nil {
        return err
    }
    baseDB := r.dbResolver(ctx, tx).Scopes(q.BuildUpdate())

    vInfo := getVersionField[T]()
    if vInfo == nil {
        return baseDB.Model(entity).Updates(entity).Error
    }
    // 乐观锁路径：DataRule WHERE 与 version WHERE 用 AND 串联
    // ...保持原有 buildUpdateMap / Where(version=?) 逻辑，把 db 替换为 baseDB
}
```

#### 6. UpdateByIdsTx
```go
func (r *Repository[D, T]) UpdateByIdsTx(ctx context.Context, ids []D, u *Updater[T], tx *gorm.DB) (int64, error) {
    if len(ids) == 0 {
        return 0, nil
    }
    if u == nil || u.IsEmpty() {
        return 0, ErrUpdateEmpty
    }
    if err := u.GetError(); err != nil {
        return 0, err
    }
    q, _ := NewQuery[T](ctx)
    if err := q.DataRuleBuilder().GetError(); err != nil {
        return 0, err
    }
    var model T
    db := r.dbResolver(ctx, tx).Model(&model).Where(ids).Scopes(q.BuildUpdate(), u.BuildUpdate())
    result := db.Updates(u.setMap)
    return result.RowsAffected, result.Error
}
```

#### 7. RestoreTx（保留 Unscoped）
```go
func (r *Repository[D, T]) RestoreTx(ctx context.Context, id D, tx *gorm.DB) (int64, error) {
    q, _ := NewQuery[T](ctx)
    if err := q.DataRuleBuilder().GetError(); err != nil {
        return 0, err
    }
    baseDB := r.dbResolver(ctx, tx).Scopes(q.BuildUpdate())
    // ...保持原有 stmt.Parse / DeleteClausesInterface 查找软删除字段逻辑，把 db 替换为 baseDB
}
```

### debug.go 改动（保持双向 errors.Is 兼容）

```go
import (
    "fmt"  // 需新增此 import，当前 debug.go 仅 import "gorm.io/gorm"
    "gorm.io/gorm"
)

func (r *Repository[D, T]) ToUpdateSQL(u *Updater[T]) (string, error) {
    if u == nil {
        return "", fmt.Errorf("%w: %w", ErrUpdateEmpty, ErrQueryNil)
    }
    return u.ToSQL(r.db)
}
```

`fmt.Errorf` 的 `%w` 双 wrap（Go 1.20+）让 `errors.Is(err, ErrUpdateEmpty)` 和 `errors.Is(err, ErrQueryNil)` 同时返回 true，兼顾"错误类型一致"和"不破坏依赖 ErrQueryNil 的旧调用方"。错误信息文案为 `"gplus: update content is empty: gplus: query cannot be nil"`，前缀冗余但语义清晰，不再优化以避免破坏 `errors.Is` 行为。

## 测试策略

新建 `repo_datarule_byid_test.go`，专用测试模型 `tenantUser`（不污染 `UserWithDelete`）：

```go
type tenantUser struct {
    ID        int64          `gorm:"primaryKey;autoIncrement"` // int64 与现有 setupTestDB[T] 的 D=int64 兼容；但仍自建 setupTenantDB 避免依赖
    Name      string         `gorm:"size:64"`
    TenantID  int            `gorm:"index;column:tenant_id"`
    DeletedAt gorm.DeletedAt
}

func setupTenantDB(t *testing.T) (*Repository[int64, tenantUser], *gorm.DB) {
    t.Helper()
    db := openDB(t) // 复用 testdb_test.go:17 的 openDB
    if err := db.AutoMigrate(&tenantUser{}); err != nil {
        t.Fatalf("migrate failed: %v", err)
    }
    return NewRepository[int64, tenantUser](db), db
}

func ctxWithTenantRule(tenantID int) context.Context {
    return context.WithValue(context.Background(), DataRuleKey, []DataRule{
        {Column: "tenant_id", Condition: "=", Value: fmt.Sprintf("%d", tenantID)},
    })
}
```

10 个表驱动子测试（在原 8 个基础上补 2 个）：

1. **TestDataRule_GetById_Blocked** — 跨租户 GetById 应返回 `gorm.ErrRecordNotFound`
2. **TestDataRule_GetByIds_Blocked** — 混合 ID 列表，断言只读到同租户记录
3. **TestDataRule_UpdateById_Blocked** — 跨租户 UpdateById，断言字段未变更（无 version 字段，affected==0 直接返回 nil；本期不引入 version 测试，因 tenantUser 无 `gplus:"version"` 字段，与乐观锁路径解耦）
4. **TestDataRule_UpdateByIds_Blocked** — 混合 ID 列表，断言只有同租户被改
5. **TestDataRule_DeleteById_Blocked** — 跨租户 DeleteById，断言记录仍存在
6. **TestDataRule_DeleteByIds_Blocked** — 混合 ID 列表，断言只删了同租户
7. **TestDataRule_Restore_Blocked** — 跨租户 Restore 软删记录，断言 `deleted_at` 未恢复
8. **TestToUpdateSQL_NilDoubleWrap** — `errors.Is(err, ErrUpdateEmpty)` 和 `errors.Is(err, ErrQueryNil)` 同时为 true
9. **TestDataRule_ByID_InvalidColumnError** — 注入非法 column（如 `"id;DROP TABLE"`）的 DataRule，对 7 个方法每个都断言返回非 nil error 且匹配 `"data rule: invalid column"` 子串（覆盖 white-list 校验路径）
10. **TestDataRule_ByID_NoRuleNoEffect** — ctx 中无 DataRuleKey 时，对所有 7 个方法做"行为不变"回归（GetById 能查到所有记录、DeleteById 能删除任意记录），确保改造不引入"必须有 DataRule 才能用"的回归

每个测试预插两个租户的数据（tenant=1 和 tenant=2），断言行级隔离。

### Godoc 验收项

实施阶段须在 `UpdateByIdTx` 和 `UpdateByIdsTx` 的 doc comment 中加入：

```
// 注意：启用 DataRule 时，记录存在但跨租户会返回 affected==0（UpdateByIdTx 返回 ErrOptimisticLock），
// 此时不应无条件重试（重试无法绕过权限）。乐观锁版本冲突与 DataRule 拦截当前共用同一错误码，
// 调用方需通过其他途径区分（如先 GetById 检查记录是否在权限范围内）。
```

`RestoreTx` 的 doc 也须补充："启用 DataRule 时跨租户记录返回 affected==0"。

## 兼容性

- **行为变更**：依赖"by-ID 路径不受 DataRule 约束"的下游代码升级后会静默改变行为（affected/查回的记录可能变成 0）。这是依赖未文档化 bug 的代码，本质是安全修复，但 SemVer 上属 minor 行为变更，故升 v0.6.0
- `applyDataRule` 检测 ctx 为 nil 或无 `DataRuleKey` 时直接返回，零额外开销
- `ToUpdateSQL(nil)` 改 wrap 保持 `errors.Is(err, ErrQueryNil)` 仍为 true，旧调用方零影响

## 风险

| 风险 | 缓解 |
|---|---|
| 表无 DataRule.Column 指定的列 | 与 v0.2.0 by-Cond 修复行为一致：SQL 执行期报错，文档已说明 |
| 乐观锁路径与 DataRule 叠加产生意外 affected=0 | DataRule 拦截（记录存在但租户不匹配）和版本冲突（命中但 version 不匹配）当前都返回 `ErrOptimisticLock`，调用方无法区分。本期 godoc 中明确说明"启用 DataRule 时 affected=0 不应无条件重试"。`ErrDataRuleBlocked` 区分留待未来版本（需要额外查询，开销不值） |
| 临时 `Query[T]` 的零分配 | 每次 by-ID 调用多构造一个 `Query[T]{conditions: make([]condition, 0, 8), errs: make([]error, 0, 8)}`，约 200 字节栈/堆混合分配。基准测试在 verification 阶段验证开销可接受 |

## 发布计划

- v0.6.0 minor 版本
- CHANGELOG 单独条目说明 7 处方法的安全修复 + 行为变更警示
- README 安装命令版本号同步到 v0.6.0
- 单独 git tag

## 后续不在本期范围

- INSERT 系列（Save / SaveBatch / Upsert / UpsertBatch / InsertOnConflict / InsertBatchOnConflict）的 DataRule 处理。DataRule 是 WHERE 过滤器，对 INSERT 无意义；INSERT 路径的"跨租户写入"风险需要的是**字段 auto-fill**（mybatis-plus `MetaObjectHandler` 风格），机制不同，单独立项
- `ErrDataRuleBlocked` 错误类型区分（避免与 `ErrOptimisticLock` 混淆），需要在 affected==0 时额外查询，开销不值得本期引入
- IncrByTx / DecrByTx / FirstOrCreate / FirstOrUpdate 已经走 Updater.DataRuleBuilder 或 Query.DataRuleBuilder，本次范围外但已安全
