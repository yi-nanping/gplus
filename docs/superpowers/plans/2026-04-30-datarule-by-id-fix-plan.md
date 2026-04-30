# Plan: DataRule by-ID 安全修复实施（v0.5.1）

**Spec**：[`docs/superpowers/specs/2026-04-26-datarule-by-id-fix-design.md`](../specs/2026-04-26-datarule-by-id-fix-design.md)
**日期**：2026-04-30
**目标版本**：v0.5.1
**预估工作量**：~250 行代码 + ~300 行测试

## 目标与成功标准

把 7 个 by-ID 路径接入 `Query[T].DataRuleBuilder`，使 ctx 中的 `DataRule` 对它们生效，同时修复 `ToUpdateSQL(nil)` 的错误类型不一致。

**完成判定**：
- 10 个新测试全部通过（8 个安全场景 + 2 个补强场景）
- `D:/Environment/golang/go1.21.11/bin/go.exe test ./...` 全量 PASS，无回归
- 覆盖率 ≥ 94%（基线，不应下降）
- `go vet ./...` 无新增警告
- 7 个改动方法的 godoc 含乐观锁/DataRule 歧义警示

## 依赖图

```
S0 (前置：核对) ─┬─> S1 (测试 RED) ─> S2 (实施 GREEN) ─┬─> S5 (验证)
                └─> S3 (debug.go 修复) ────────────────┤
                                                       └─> S4 (godoc 补充)
```

S1 和 S3、S4 可并行；S2 必须在 S1 之后；S5 是终点。

## 任务步骤

### S0：前置核对（5 分钟，不写代码）

读 `repository.go` 与 `query.go` 关键行，复核 spec 模板与实际代码的偏差。

**检查点**：
- [ ] `repository.go` 7 个方法的实际行号与 spec 一致（GetByIdTx:170 / GetByIdsTx:870 / DeleteByIdTx:518 / DeleteByIdsTx:790 / UpdateByIdTx:421 / UpdateByIdsTx:728 / RestoreTx:887）
- [ ] `Query[T].DataRuleBuilder` / `BuildQuery` / `BuildDelete` / `BuildUpdate` API 符合 spec 假设
- [ ] `testdb_test.go:openDB` 可在新测试文件中调用

**模型分级**：N/A（人工读，不派 subagent）

---

### S1：写测试文件 RED 阶段（~300 行 + 必失败）

**文件**：新建 `D:/projects/gplus/repo_datarule_byid_test.go`

**内容**：
1. `tenantUser` 模型定义（int64 主键、tenant_id 字段、gorm.DeletedAt 软删除）
2. `setupTenantDB(t)` helper（复用 `openDB` + AutoMigrate）
3. `ctxWithTenantRule(tenantID int)` helper
4. 10 个表驱动子测试（spec 测试策略章节列举的 1-10）

**TDD 顺序**：
- 先写 1-7 跨租户拦截测试 → 跑应该全失败（因为修复未做）
- 再写 8 双 wrap 测试 → 跑应该失败（debug.go 未改）
- 最后写 9-10 补强测试 → 9 应该会失败、10 不依赖修复（先通过）

**RED 验收**：
```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestDataRule_ByID -v ./...
# 期望：1-7 + 9 失败（共 8 个失败），8 失败（debug 未改），10 通过
```

**模型分级**：sonnet（常规 TDD，不机械但有明确测试模板）

**潜在坑**：
- `setupTenantDB` 返回 `*Repository[int64, tenantUser]`，调用 `GetById(ctx, int64(1))` 时 id 必须是 `int64` 字面量
- `gorm.DeletedAt` 软删除字段，Restore 测试需先 `DeleteById` 再断言 deleted_at IS NOT NULL，再 `Restore` 断言 affected==0

---

### S2：实施 7 处 GREEN 修复（~150 行净增）

**文件**：`D:/projects/gplus/repository.go`

**改动模板**：每处方法体头部注入：
```go
q, _ := NewQuery[T](ctx)
if err := q.DataRuleBuilder().GetError(); err != nil {
    return /* zero value */, err
}
```
然后把后续 `r.dbResolver(ctx, tx)` 改为 `r.dbResolver(ctx, tx).Scopes(q.BuildXxx())`。

**逐方法清单**（按改动复杂度从低到高）：

| 方法 | 行号 | scope 类型 | 特殊处理 |
|---|---|---|---|
| GetByIdTx | 169-172 | BuildQuery | 无 |
| GetByIdsTx | 869-877 | BuildQuery | 无 |
| DeleteByIdTx | 517-521 | BuildDelete | 无 |
| DeleteByIdsTx | 789-796 | BuildDelete | 无 |
| UpdateByIdTx | 421-447 | BuildUpdate | 普通路径 + 乐观锁路径都套 baseDB |
| UpdateByIdsTx | 727-742 | BuildUpdate | 双 scope `Scopes(q.BuildUpdate(), u.BuildUpdate())` |
| RestoreTx | 887-905 | BuildUpdate | `baseDB := dbResolver(...).Scopes(q.BuildUpdate())`，后续 `baseDB.Unscoped().Model(...)` 链式 |

**GREEN 验收**：
```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestDataRule_ByID -v ./...
# 期望：10 个子测试全部通过
```

**模型分级**：sonnet（机械改动，但有 7 个点 + 乐观锁分支需要小心，不派 haiku）

**潜在坑**：
- `UpdateByIdTx` 乐观锁路径必须用 `baseDB.Model(entity).Where(...)`，不能用 `db.Model(...)`（会丢 DataRule）
- `UpdateByIdsTx` 双 scope 顺序：`q.BuildUpdate()` 在前，`u.BuildUpdate()` 在后（Where(ids) 已先调）
- `RestoreTx` 的 `stmt := &gorm.Statement{DB: baseDB}` 需更新引用

---

### S3：debug.go 修复（~3 行）

**文件**：`D:/projects/gplus/debug.go`

**改动**：
1. import 块加 `"fmt"`
2. `ToUpdateSQL` 第 84-86 行：`return "", fmt.Errorf("%w: %w", ErrUpdateEmpty, ErrQueryNil)`

**验收**：
```bash
D:/Environment/golang/go1.21.11/bin/go.exe test -run TestToUpdateSQL_NilDoubleWrap -v ./...
```

**模型分级**：haiku（纯机械，3 行改动）

---

### S4：godoc 补充（~30 行注释）

在以下方法的 doc comment 中追加乐观锁+DataRule 歧义警示：

- `UpdateByIdTx`（spec 测试策略章节有完整文案）
- `UpdateByIdsTx`
- `RestoreTx`

**验收**：人工 review，无需测试

**模型分级**：haiku（纯文档）

---

### S5：全量验证 + commit（verification-loop）

**子步骤**：

1. **build / vet**：
   ```bash
   D:/Environment/golang/go1.21.11/bin/go.exe build ./...
   D:/Environment/golang/go1.21.11/bin/go.exe vet ./...
   ```

2. **全量测试**（含覆盖率）：
   ```bash
   D:/Environment/golang/go1.21.11/bin/go.exe test -coverprofile=coverage.out ./...
   D:/Environment/golang/go1.21.11/bin/go.exe tool cover -func=coverage.out | grep total
   # 期望 total ≥ 94.0%
   ```

3. **diff review**：`git diff --stat` 检查改动范围
4. **code-reviewer agent 审查**（superpowers:requesting-code-review）
5. 通过后 commit：feat: + 单条 commit，spec/plan 已先 commit

**模型分级**：人工 + opus（final review）

## 风险与回滚

| 风险 | 缓解 |
|---|---|
| 临时 `Query[T]` 分配开销影响 hot path | S5 加 `BenchmarkGetById` 对比基线，>5% 退化则切回方案 A |
| GORM `Scopes(q.BuildQuery()).First(&data, id)` 行为与预期不符 | S1 测试用例覆盖此具体路径，发现立即调整 |
| 乐观锁 + DataRule 在某些 GORM 版本上叠加产生 SQL 错误 | S1 包含跨租户乐观锁场景（虽然 tenantUser 无 version 字段，可考虑加 versionedTenantUser 子测试） |
| `errors.Is` 双 wrap 在某些边缘场景失效 | S3 用 `errors.Is` 双向断言验证 |
| 全量回归发现旧测试依赖 by-ID 不应用 DataRule 的隐式行为 | 检查 `*_test.go` 中是否有 ctx 携带 DataRule 又调 by-ID 方法的用例，按需调整测试预期 |

**回滚策略**：S2 的 7 个改动各自独立可回滚，可分 7 个 commit；如必要可 `git revert` 单个 commit 而不影响其他方法。

## Commit 计划

按 plan 阶段先后顺序：

1. **chore: 接入 DataRule 到 GetById/GetByIds（读路径修复）**（S2 的 GetByIdTx + GetByIdsTx）
2. **fix: 接入 DataRule 到 DeleteById/DeleteByIds（写路径修复）**（S2 的 DeleteByIdTx + DeleteByIdsTx）
3. **fix: 接入 DataRule 到 UpdateById/UpdateByIds（写路径含乐观锁）**（S2 的 UpdateByIdTx + UpdateByIdsTx）
4. **fix: 接入 DataRule 到 Restore（软删恢复）**（S2 的 RestoreTx）
5. **fix: ToUpdateSQL(nil) 错误类型双 wrap**（S3）
6. **docs: 补 godoc 警示乐观锁+DataRule 歧义**（S4）
7. **test: 新增 repo_datarule_byid_test.go 覆盖 by-ID DataRule**（S1，最后 commit 因 TDD 中间状态有 RED）

如选择"测试先 commit RED → 再 commit GREEN"模式，则 S1 在最前。本项目偏好"绿后才 commit"（README 与历史 commit 都是绿态提交），故 S1 与 S2 合并 commit。

最终预期 4-5 个 feature commit + 1 个 doc commit，可压缩为单个语义连贯 commit `feat: v0.5.1 DataRule by-ID 安全修复（7 处方法 + ToUpdateSQL）`，由 implementer 阶段判断粒度。

## CHANGELOG / README 更新（不在本 plan 范围）

属于发布阶段任务，等代码 GREEN + review 通过后单独执行：
- `CHANGELOG.md` 加 v0.5.1 章节
- `README.md` 安装命令版本号
- `git tag v0.5.1` + push
