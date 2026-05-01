# grep 嵌套括号漏检修复实施计划 (v0.7.1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修正 v0.7.0 CHANGELOG 「行为约束」段排查命令在嵌套括号场景下漏检的 bug，发 v0.7.1 纯文档版本。

**Architecture:** 把 `grep -rEn 'ToDB\([^)]*\)\.(Scan|Row|Rows)\(' . --include='*.go'` 中的 `[^)]*` 改为 `.*`，靠后续锚点 `\)\.(Scan|Row|Rows)\(` 强制回溯支持任意嵌套。同步修改 v0.7.0 已发布段、镜像 spec 内嵌示例，并在 CHANGELOG 顶部新增 v0.7.1 条目。所有变更纯文档，不动 .go 代码。

**Tech Stack:** Markdown、bash grep（无新增依赖）。

**Spec：** `docs/superpowers/specs/2026-05-01-grep-nested-paren-fix-design.md`

**前置状态：**
- 分支：`docs/fix-grep-nested-paren`
- 已有 commit：`22dd584 docs(spec): grep [^)]* → .* 嵌套括号修复设计 (v0.7.1)`
- 本 plan 文档将作为下一个 commit
- 工作目录：`D:\projects\gplus`

---

## 文件结构

| 类别 | 路径 | 责任 |
|------|------|------|
| 修改 | `CHANGELOG.md` | v0.7.0 段 grep 修复 + 警告 + 对照表；顶部新增 v0.7.1 条目 |
| 修改 | `docs/superpowers/specs/2026-05-01-scan-callback-fix-design.md` | 5.1 节内嵌 CHANGELOG 示例与实际 CHANGELOG 字面同步 |
| 临时（不入仓） | `/tmp/grep_probe/probe.go` | 反例验证文件，task 完成后删除 |

> **不动**：README.md（不含字面量）、`*.go` 源码（不含字面量）、互补 grep #2 命令（不含字面量）、v0.7.1 候选段中的 aggregate Distinct+Select bug。

---

## Task 0: 锁定修复方案基线（可选但推荐）

**Files:**
- Create: `/tmp/grep_probe/probe.go`（临时，task 末尾删除）

**Why:** 在改文档前用临时反例文件锁定「旧 regex 漏检 / 新 regex 命中」对照基线，给 Task 1/2 验证提供事实依据。本 task 也证明 spec 设计不是空想。

- [ ] **Step 0.1: 创建临时反例文件**

```bash
mkdir -p /tmp/grep_probe
cat > /tmp/grep_probe/probe.go <<'EOF'
package probe

// 反例 1（基础违规）：单层括号，旧/新 regex 都应命中
func case1() { q.ToDB(db).Scan(&x) }

// 反例 2（嵌套括号）：旧 regex 漏检，新 regex 命中
func case2() { q.ToDB(r.GetDB()).Scan(&x) }

// 反例 3（中间链方法）：旧 regex 漏检，新 regex 命中
func case3() { q.ToDB(r.GetDB()).WithContext(ctx).Scan(&x) }

// 反例 4（合规 Find）：旧/新 regex 都不应命中
func case4() { q.ToDB(db).Model(&T{}).Find(&rows) }
EOF
```

- [ ] **Step 0.2: 跑旧 regex 锁基线**

Run（bash）:
```bash
grep -rEn 'ToDB\([^)]*\)\.(Scan|Row|Rows)\(' /tmp/grep_probe --include='*.go'
```
Expected: 仅 1 行命中（case1），case2/case3 漏检，case4 不命中。

- [ ] **Step 0.3: 跑新 regex 验证修复**

Run（bash）:
```bash
grep -rEn 'ToDB\(.*\)\.(Scan|Row|Rows)\(' /tmp/grep_probe --include='*.go'
```
Expected: 3 行命中（case1 + case2 + case3），case4 不命中。

如果 Step 0.2/0.3 结果与预期不符，**停止**，回看 spec §1.2 匹配过程。

---

## Task 1: 修 CHANGELOG.md

**Files:**
- Modify: `CHANGELOG.md:5`（顶部新增 v0.7.1 条目）
- Modify: `CHANGELOG.md:26`（grep 命令字符替换）
- Modify: `CHANGELOG.md:30-31`（grep 代码块后追加警告段 + 对照表）

- [ ] **Step 1.1: 在 v0.7.0 段之上插入 v0.7.1 条目**

定位：找到 `## [0.7.0] - 2026-05-01`（当前 line 5），在它**之上**插入。

使用 Edit tool：
- old_string（需要包含足够 context 区分唯一）：
```
所有版本变更记录遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/) 格式，版本号遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)。

## [0.7.0] - 2026-05-01
```
- new_string：
```
所有版本变更记录遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/) 格式，版本号遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)。

## [0.7.1] - 2026-05-01

### 修复 (文档)

- **v0.7.0「行为约束」段排查命令在嵌套括号场景漏检**：原命令中 `[^)]*` 在 `q.ToDB(r.GetDB()).Scan(&x)` 这类实参含括号的调用上匹配失败，整条 regex 在第一个 `)` 处断裂导致漏检。修正为 `.*` + 后续锚点强制回溯，支持任意嵌套深度
- 同步在 v0.7.0「行为约束」段追加「regex 启发式局限 + AST 工具兜底」警告与「regex 命中对照表」（4 行反例覆盖单层 / 嵌套 / 中间链 / 合规 Find）
- 来源：下游 gvs-server 项目落地 v0.7.0 时实测发现并验证修复
- 仅文档变更，不涉及代码、API、行为；GORM 版本锁定保持 v1.31.x

---

## [0.7.0] - 2026-05-01
```

- [ ] **Step 1.2: 修 v0.7.0 段 grep 命令字面量**

使用 Edit tool：
- old_string：
```
    # 1. 单行直链（高置信度）
    grep -rEn 'ToDB\([^)]*\)\.(Scan|Row|Rows)\(' . --include='*.go'
```
- new_string：
```
    # 1. 单行直链（高置信度）
    grep -rEn 'ToDB\(.*\)\.(Scan|Row|Rows)\(' . --include='*.go'
```

- [ ] **Step 1.3: 在 grep 代码块后追加警告段 + 对照表**

使用 Edit tool：
- old_string（grep 代码块结束 + 紧跟的 RawScan 段，作为唯一定位锚点）：
```
    # 2. 跨行场景（变量赋值后调用 / 中间链方法）— 需人工复查
    grep -rEn '\.ToDB\(' . --include='*.go'
    # 在结果文件中再 grep 是否有 .Scan/.Row/.Rows
    ```
- **新 API 不取代 `RawScan`**：
```
- new_string：
```
    # 2. 跨行场景（变量赋值后调用 / 中间链方法）— 需人工复查
    grep -rEn '\.ToDB\(' . --include='*.go'
    # 在结果文件中再 grep 是否有 .Scan/.Row/.Rows
    ```

    **regex 启发式的本质局限**：上述 grep 命令是行内启发式扫描，无法理解 Go AST。真正的深嵌套（同一行多次调用 ToDB / 跨行 builder pattern）仍可能漏检或误检。关键代码请人工 review，或使用 AST 工具作为兜底：
    - [ast-grep](https://ast-grep.github.io/)：结构化模式匹配，例如 `ast-grep --pattern '$Q.ToDB($$$).Scan($$$)' --lang go`
    - `golang.org/x/tools/go/analysis` 写自定义 lint analyzer，精确识别方法链
    - `go/parser` + `go/ast` 手写小工具

    **regex 命中对照表**（验证修正后的命令）：

    | 反例代码                                              | 旧 `[^)]*` | 新 `.*`  |
    |-------------------------------------------------------|-----------|----------|
    | `q.ToDB(db).Scan(&x)` (基础违规)                       | ✓ 命中    | ✓ 命中    |
    | `q.ToDB(r.GetDB()).Scan(&x)` (实参嵌套括号)            | ✗ 漏检    | ✓ 命中    |
    | `q.ToDB(r.GetDB()).WithContext(ctx).Scan(&x)` (中间链) | ✗ 漏检    | ✓ 命中    |
    | `q.ToDB(db).Model(&T{}).Find(&rows)` (Find 非违规)      | ✗ 不命中  | ✗ 不命中  |
- **新 API 不取代 `RawScan`**：
```

> 注意：警告段 + 对照表用 4-space 缩进保持在「行为约束」第一个 bullet 内（与 grep 代码块同级），不破坏外层列表结构。

- [ ] **Step 1.4: 验证 CHANGELOG.md 中 `[^)]*` 字面量残留**

Run（PowerShell 或 bash 都可）：
```bash
grep -n '\[\^)\]\*' CHANGELOG.md
```
Expected: 命中 **2 处**，且都是「字面引用」语境，不在可执行 grep 命令的 regex 位置：
1. v0.7.1 条目内描述「原命令中 `[^)]*` 在 ...」（Step 1.1 插入）
2. v0.7.0 段对照表表头「旧 `[^)]*`」单元格（Step 1.3 插入）

不允许出现：v0.7.0 段第 26 行 `grep -rEn 'ToDB\([^)]*\)...` 这种**仍未替换的可执行命令**。

如果命中数 > 2，复查是否漏改 Step 1.2；如果命中数 < 2，复查是否 Step 1.1/1.3 内容缺失。

- [ ] **Step 1.5: 跑修正后命令对反例文件做端到端验证**

Run（bash，从 CHANGELOG 第 26 行复制实际命令）：
```bash
grep -rEn 'ToDB\(.*\)\.(Scan|Row|Rows)\(' /tmp/grep_probe --include='*.go'
```
Expected: 与 Task 0 Step 0.3 相同 — 命中 case1/case2/case3 共 3 行。

如果命中数与 Task 0 Step 0.3 不一致，说明 Step 1.2 改错了字符，回看 Edit 结果。

---

## Task 2: 修镜像 spec（5.1 节内嵌 CHANGELOG 示例）

**Files:**
- Modify: `docs/superpowers/specs/2026-05-01-scan-callback-fix-design.md:274`（grep 命令字面量）
- Modify: `docs/superpowers/specs/2026-05-01-scan-callback-fix-design.md:278-279`（追加警告段 + 对照表）

> **关键**：spec 5.1 节是「外层 ```markdown ... ``` code fence 内嵌一段 CHANGELOG 示例，CHANGELOG 示例中的 ```bash 内层 code fence 必须 escape 成 `\`\`\`bash`」。修改时**保留** `\`\`\`` escape 形式，不要改成普通 `` ``` ``，否则外层 code fence 会被提前关闭。

- [ ] **Step 2.1: 修 spec 5.1 节内嵌示例第 274 行 grep 命令**

使用 Edit tool：
- old_string：
```
    # 1. 单行直链（高置信度）
    grep -rEn 'ToDB\([^)]*\)\.(Scan|Row|Rows)\(' . --include='*.go'
```
- new_string：
```
    # 1. 单行直链（高置信度）
    grep -rEn 'ToDB\(.*\)\.(Scan|Row|Rows)\(' . --include='*.go'
```

> 此 old_string 与 CHANGELOG.md Step 1.2 字面相同，但 Edit 是文件级别操作，对镜像 spec 文件不冲突。

- [ ] **Step 2.2: 在 spec 5.1 节内嵌示例的 grep 代码块后追加警告段 + 对照表**

使用 Edit tool（注意保留 `\`\`\`` escape）：
- old_string：
```
    # 2. 跨行场景（变量赋值后调用 / 中间链方法）— 需人工复查
    grep -rEn '\.ToDB\(' . --include='*.go'
    # 在结果文件中再 grep 是否有 .Scan/.Row/.Rows
    \`\`\`
- **新 API 不取代 `RawScan`**：
```
- new_string：
```
    # 2. 跨行场景（变量赋值后调用 / 中间链方法）— 需人工复查
    grep -rEn '\.ToDB\(' . --include='*.go'
    # 在结果文件中再 grep 是否有 .Scan/.Row/.Rows
    \`\`\`

    **regex 启发式的本质局限**：上述 grep 命令是行内启发式扫描，无法理解 Go AST。真正的深嵌套（同一行多次调用 ToDB / 跨行 builder pattern）仍可能漏检或误检。关键代码请人工 review，或使用 AST 工具作为兜底：
    - [ast-grep](https://ast-grep.github.io/)：结构化模式匹配，例如 `ast-grep --pattern '$Q.ToDB($$$).Scan($$$)' --lang go`
    - `golang.org/x/tools/go/analysis` 写自定义 lint analyzer，精确识别方法链
    - `go/parser` + `go/ast` 手写小工具

    **regex 命中对照表**（验证修正后的命令）：

    | 反例代码                                              | 旧 `[^)]*` | 新 `.*`  |
    |-------------------------------------------------------|-----------|----------|
    | `q.ToDB(db).Scan(&x)` (基础违规)                       | ✓ 命中    | ✓ 命中    |
    | `q.ToDB(r.GetDB()).Scan(&x)` (实参嵌套括号)            | ✗ 漏检    | ✓ 命中    |
    | `q.ToDB(r.GetDB()).WithContext(ctx).Scan(&x)` (中间链) | ✗ 漏检    | ✓ 命中    |
    | `q.ToDB(db).Model(&T{}).Find(&rows)` (Find 非违规)      | ✗ 不命中  | ✗ 不命中  |
- **新 API 不取代 `RawScan`**：
```

> 警告段 + 对照表内容与 CHANGELOG.md Step 1.3 字面相同（保持镜像一致性）。

- [ ] **Step 2.3: 验证镜像 spec 中 `[^)]*` 字面量残留**

Run：
```bash
grep -n '\[\^)\]\*' docs/superpowers/specs/2026-05-01-scan-callback-fix-design.md
```
Expected: 仅命中本次新增对照表中的 `` `[^)]*` `` 字面量（表头单元格），不应在 grep 命令位置出现。

---

## Task 3: 全局收尾验证 + 清理 + commit

**Files:**
- Modify: `CHANGELOG.md`（已在 Task 1 完成）
- Modify: `docs/superpowers/specs/2026-05-01-scan-callback-fix-design.md`（已在 Task 2 完成）
- Delete: `/tmp/grep_probe/`（临时验证目录）

- [ ] **Step 3.1: 全局 grep `[^)]*` 残留扫描**

Run：
```bash
grep -rn '\[\^)\]\*' --include='*.md' --include='*.go' .
```
Expected: 所有命中都在「描述历史 bug」或「对照表表头」语境（即作为字面引用，不在可执行 grep 命令的 regex 位置）。具体允许命中位置：

1. CHANGELOG.md v0.7.1 条目内描述（Step 1.1 插入）
2. CHANGELOG.md v0.7.0 段对照表表头（Step 1.3 插入）
3. 镜像 spec v0.7.0 段对照表表头（Step 2.2 插入）
4. 设计 spec `2026-05-01-grep-nested-paren-fix-design.md`（已存在，描述 bug）

不允许出现：任何 `grep -rEn 'ToDB\([^)]*\)...` 形式的可执行命令。

- [ ] **Step 3.2: 清理临时反例文件**

Run（bash）：
```bash
rm -rf /tmp/grep_probe
```

- [ ] **Step 3.3: git status 确认改动范围**

Run：
```bash
git status
```
Expected:
```
modified:   CHANGELOG.md
modified:   docs/superpowers/specs/2026-05-01-scan-callback-fix-design.md
```
**只有这 2 个文件**。如果有其它文件，停止并复查。

- [ ] **Step 3.4: git diff 自检**

Run：
```bash
git diff CHANGELOG.md docs/superpowers/specs/2026-05-01-scan-callback-fix-design.md
```
检查项：
- v0.7.1 条目位置正确（v0.7.0 之上）
- v0.7.0 段第 26 行 grep 命令 `[^)]*` 已变 `.*`
- v0.7.0 段警告段 + 对照表已追加在 grep 代码块后
- 镜像 spec 同 4 处变更（除 v0.7.1 条目外）
- 镜像 spec 中 `\`\`\`` escape 形式保留
- 无误删行

- [ ] **Step 3.5: 提交单 commit**

Run（bash，HEREDOC 写法保证中文 + 多行不被 shell 转义；**禁用 Co-Authored-By trailer**）：

```bash
git add CHANGELOG.md docs/superpowers/specs/2026-05-01-scan-callback-fix-design.md
git commit -m "$(cat <<'EOF'
docs: v0.7.1 修正 v0.7.0 grep 命令嵌套括号漏检 + 局限警告

v0.7.0 CHANGELOG「行为约束」段提供的排查命令使用 [^)]* 在 ToDB 实参
含括号场景下漏检，下游 gvs-server 项目落地时实测发现。本次修复：

- v0.7.0 段 grep 命令 [^)]* → .*，靠后续锚点回溯支持任意嵌套
- v0.7.0 段追加 regex 启发式局限警告 + AST 工具兜底建议
- v0.7.0 段追加 regex 命中对照表（4 行反例覆盖单层/嵌套/中间链/合规 Find）
- 镜像 spec 5.1 节同步上述变更，保持字面一致
- CHANGELOG 顶部新增 v0.7.1 条目记录本次文档修复

仅文档变更，不涉及代码、API、行为；GORM 版本锁定保持 v1.31.x。
EOF
)"
```

- [ ] **Step 3.6: 验证 commit 历史**

Run：
```bash
git log --oneline -5
```
Expected: 最新 commit message 以 `docs: v0.7.1 修正 v0.7.0 grep` 开头，无 `Co-Authored-By` 行。

```bash
git log -1 --pretty=%B
```
Expected: 完整 commit body 输出，**不含** `Co-Authored-By:` 字样。

如果出现 Co-Authored-By：用 `git commit --amend` 重写 message 删除该 trailer（amend 在「同 commit 范围内修 message」是允许的）。

- [ ] **Step 3.7: 不 push，等用户审完手动发版**

明确**不**执行：
```bash
# git push origin docs/fix-grep-nested-paren  ← 禁止
# git tag v0.7.1                              ← 禁止
```

汇报给用户：
- 分支 `docs/fix-grep-nested-paren` 已就绪
- 共 3 个 commit：spec（已存在 22dd584）、plan（writing-plans 阶段产物）、CHANGELOG+spec 实际变更
- `git diff main...HEAD` 可看完整本次 PR 候选 diff

---

## 验收清单（一一对应 spec §7）

- [x] brainstorming 产出 spec，用户已批准
- [ ] writing-plans 产出实施计划（即本文件），用户审完批准后才能执行
- [ ] CHANGELOG 中 `[^)]*` 已替换为 `.*`（Task 1 Step 1.2）
- [ ] CHANGELOG 中追加局限警告段（Task 1 Step 1.3）
- [ ] CHANGELOG 中追加命中对照表（Task 1 Step 1.3）
- [ ] 镜像 spec 已同步（Task 2 全部）
- [ ] CHANGELOG 顶部新增 v0.7.1 条目（Task 1 Step 1.1）
- [ ] git commit 中文 message，禁用 Co-Authored-By trailer（Task 3 Step 3.5/3.6）
- [ ] 留 commit 不 push（Task 3 Step 3.7）

---

## 风险与回滚

- **风险 1**：Edit 工具 old_string 在文件中不唯一 → Edit 报错。处理：扩大 context 直到唯一。
- **风险 2**：镜像 spec 内层 `\`\`\`` escape 被误改成普通 `` ``` `` → 外层 code fence 提前关闭 → markdown 渲染破坏。处理：Step 2.2 严格保留原 escape 形式；Step 3.4 diff 自检时 grep `\\\``确认。
- **风险 3**：commit message 误带 Co-Authored-By → Step 3.6 检测后 `git commit --amend` 重写。
- **回滚**：本次 3 commit 均未 push，可 `git reset --hard 22dd584`（保留 spec）或 `git checkout main && git branch -D docs/fix-grep-nested-paren`（全废）。
