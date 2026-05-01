# grep 嵌套括号漏检修复（v0.7.1 文档 patch）

**日期**：2026-05-01
**版本**：v0.7.1（纯文档版，不动代码）
**性质**：文档修复 — 修正 v0.7.0 「行为约束」段排查命令在嵌套括号场景下漏检的 bug
**来源**：下游 gvs-server 项目落地 v0.7.0 时实测发现并已在自己 CI 中验证修复方案

---

## 1. Bug 描述

### 1.1 v0.7.0 已发布的排查命令

CHANGELOG v0.7.0 「行为约束」段第 26 行提供：

```bash
grep -rEn 'ToDB\([^)]*\)\.(Scan|Row|Rows)\(' . --include='*.go'
```

意图：扫描下游代码中 `q.ToDB(...).Scan/Row/Rows(...)` 的违规调用，提示迁移到 `FindAs`/`FindOneAs`，避免绕过 GORM Query callback chain 导致的隔离/审计漏洞。

### 1.2 缺陷

`[^)]*` 是「非右括号字符任意次」。当 `ToDB(...)` 实参本身是个方法调用、含括号时，匹配在第一个 `)` 处停止，导致漏检。

匹配过程（反例 `q.ToDB(r.GetDB()).Scan(&x)`）：

| 步骤 | regex 片段 | 匹配的子串 |
|------|-----------|-----------|
| 1 | `ToDB\(` | `ToDB(` |
| 2 | `[^)]*` | `r.GetDB`（遇 `(` 后第一个 `)` 前停） |
| 3 | `\)` | `r.GetDB(` 之后的 `)` |
| 4 | `\.` | 期待 `.`，实际是 `)` |
| 5 | **失败** | 漏检 |

### 1.3 实测影响

下游 gvs-server 项目跑 v0.7.0 文档命令对其 2 个真实 `q.ToDB(r.GetDB()).Find(...)` 调用全部漏检。如果某天有人写错成 `.Scan(...)`，文档命令同样漏检。下游用户照搬命令时，违规调用看似 0 命中，实际隔离漏洞仍在。

---

## 2. 修复方案对比

### 2.1 方案 A：贪婪 `.*` + 后续锚点回溯（推荐）

```bash
grep -rEn 'ToDB\(.*\)\.(Scan|Row|Rows)\(' . --include='*.go'
```

匹配过程（同样反例）：
- `ToDB\(` → `ToDB(`
- `.*` 贪婪到行末
- `\)` 失败 → 回溯
- 最终 `.*` 缩到 `r.GetDB()`，`\)` 匹配第二个 `)`，`\.(Scan|Row|Rows)\(` 匹配 `.Scan(`

**优点**：单字符替换、5 分钟工作量、跨平台 grep 都支持、嵌套深度无限制
**缺点**：仍是 regex 启发式；同一行多调用的极端 case 可能误匹配

### 2.2 方案 B：PCRE 递归子模式

```bash
grep -rPn 'ToDB\((?:[^()]|\([^()]*\))*\)\.(Scan|Row|Rows)\(' . --include='*.go'
```

**优点**：能 cover 单层嵌套，理论更准
**缺点**：要求 `grep -P`（macOS 自带 grep 不支持，需 ggrep），跨平台兼容性差

### 2.3 方案 C：放弃 grep，提供 Go AST 工具

gplus 仓库提供 `cmd/check-todb-scan/main.go` 真正解析 AST，精确无假阳/假阴。

**优点**：最精确
**缺点**：gplus 是库不是工具集，加 `cmd/` 改变项目定位；下游已自行写 shell 脚本，AST 工具用户群可能太小

### 2.4 方案 D：方案 A + 文档警告

承认 regex 的启发式本质，在文档中明确边界。

### 2.5 决策：A + D

- 单字符替换覆盖 90% 场景
- 文档加警告承认局限，引导关键代码人工 review 或用 AST 工具
- 不引入 PCRE 强依赖（B 跨平台代价不值）
- 不改变 gplus 项目定位（C 范围过宽）

---

## 3. 变更清单

### 3.1 `CHANGELOG.md`

**位置 1**：v0.7.0 段（第 26 行）

```diff
-grep -rEn 'ToDB\([^)]*\)\.(Scan|Row|Rows)\(' . --include='*.go'
+grep -rEn 'ToDB\(.*\)\.(Scan|Row|Rows)\(' . --include='*.go'
```

**位置 2**：v0.7.0 段 grep 命令后追加局限警告段（见 §4）

**位置 3**：v0.7.0 段追加 regex 命中对照表（见 §5）

**位置 4**：CHANGELOG 文件中按 Keep a Changelog 倒序惯例，在 v0.7.0 段**之上、文件标题段之下**新增 v0.7.1 条目

```markdown
## [0.7.1] - 2026-05-01

### 修复 (文档)

- **v0.7.0「行为约束」段排查命令在嵌套括号场景漏检**：原命令 `grep -rEn 'ToDB\([^)]*\)\.(Scan|Row|Rows)\(' . --include='*.go'` 中 `[^)]*` 在 `ToDB(r.GetDB()).Scan(&x)` 这类实参含括号的调用上匹配失败。修正为 `grep -rEn 'ToDB\(.*\)\.(Scan|Row|Rows)\(' . --include='*.go'`，并在文档中追加「regex 启发式局限 + AST 工具兜底」警告与命中对照表
- 来源：下游 gvs-server 项目落地 v0.7.0 时实测发现并验证修复
- 仅文档变更，不涉及代码、API、行为
```

### 3.2 `docs/superpowers/specs/2026-05-01-scan-callback-fix-design.md`（镜像同步）

5.1 节内嵌的 CHANGELOG 示例（第 274 行）同步三项：grep 命令字面替换、追加局限警告段、追加对照表。**不复制 v0.7.1 条目**（spec 是历史设计稿，不重复 CHANGELOG 演进；只让 spec 的 5.1 节示例与实际 CHANGELOG v0.7.0 段一致即可）。

### 3.3 本 spec 文档

`docs/superpowers/specs/2026-05-01-grep-nested-paren-fix-design.md`（即本文）。

---

## 4. 局限警告段文案

```markdown
**regex 启发式的本质局限**：上述 grep 命令是行内启发式扫描，无法理解 Go AST。
真正的深嵌套（同一行多次调用 ToDB / 跨行 builder pattern）仍可能漏检或误检。
关键代码请人工 review，或使用 AST 工具作为兜底：

- [ast-grep](https://ast-grep.github.io/)：结构化模式匹配，例如 `ast-grep --pattern '$Q.ToDB($$$).Scan($$$)' --lang go`
- `golang.org/x/tools/go/analysis` 写自定义 lint analyzer，精确识别方法链
- `go/parser` + `go/ast` 手写小工具（参考下游 gvs-server `scripts/check-no-todb-scan.sh` 的 grep 版本）
```

---

## 5. 命中对照表文案

```markdown
| 反例代码                                              | 旧 `[^)]*` | 新 `.*`  |
|-------------------------------------------------------|-----------|----------|
| `q.ToDB(db).Scan(&x)` (基础违规)                       | ✓ 命中    | ✓ 命中    |
| `q.ToDB(r.GetDB()).Scan(&x)` (实参嵌套括号)            | ✗ 漏检    | ✓ 命中    |
| `q.ToDB(r.GetDB()).WithContext(ctx).Scan(&x)` (中间链) | ✗ 漏检    | ✓ 命中    |
| `q.ToDB(db).Model(&T{}).Find(&rows)` (Find 非违规)      | ✗ 不命中  | ✗ 不命中  |
```

最后一行展示「合规 Find 不会被误伤」。

---

## 6. 不在范围

- 不动 `.go` 代码、测试、API 行为
- 不动 README.md（已核实不含 `[^)]*` 字面量，仅引用 CHANGELOG）
- 不动 godoc 注释（已核实 `*.go` 文件 0 命中字面量）
- 不动互补 grep #2 命令 `'\.ToDB\('`（不含 `[^)]*`）
- 不并 v0.7.1 候选段中的 aggregate `Distinct + Select` bug（性质不同，留待未来）
- 不引入 PCRE / shell 测试脚本 / CI 步骤
- 不发 GitHub release / 不 push tag — 由用户审完后手动发版

---

## 7. 验收清单（与用户原始任务一一对应）

- [ ] brainstorming 产出本 spec，用户已批准
- [ ] writing-plans 产出实施计划，用户已批准
- [ ] CHANGELOG 中 `[^)]*` 已替换为 `.*`
- [ ] CHANGELOG 中追加局限警告段
- [ ] CHANGELOG 中追加命中对照表
- [ ] 镜像 spec（2026-05-01-scan-callback-fix-design.md）已同步
- [ ] CHANGELOG 顶部新增 v0.7.1 条目
- [ ] git commit 中文 message，禁用 Co-Authored-By trailer
- [ ] 留 commit 不 push（用户手动审完发版）

---

## 8. commit 规划

| # | 内容 | message |
|---|------|---------|
| 1 | 新增本 spec 文档 | `docs(spec): grep [^)]* → .* 嵌套括号修复设计 (v0.7.1)` |
| 2 | writing-plans 产出的 plan 文档 | `docs(plan): grep regex 修复实施计划` |
| 3 | CHANGELOG.md + 镜像 spec 实际变更 | `docs: v0.7.1 修正 v0.7.0 grep 命令嵌套括号漏检 + 局限警告` |

3 commit 不 push，等用户审完手动发版（或 squash）。
