# gplus 路线图

> 项目演进方向、方言支持矩阵、已知技术债与下一步规划。
>
> **信息时效以 git history 为准** — 本文档为人工汇总，可能滞后。当条目与代码冲突时，**信代码**。

最后更新：2026-05-18（v0.8.3 已发布，v0.8.4 KingbaseES PG 兼容 plan 已写、实施未启动）

---

## 1. 方言支持矩阵

| 方言 | 状态 | 引入版本 | quoter | CI 跑 | 关键说明 |
|---|---|---|---|---|---|
| **SQLite** | ✅ 默认 | v0.1.0 | 双引号 | ✓ 默认 | 测试基础，内存 DB |
| **MySQL** | ✅ 默认 | v0.1.0 | 反引号 | ✓ 默认 | 生产主力 |
| **PostgreSQL** | ✅ 默认 | v0.1.0 | 双引号 | ✓ 默认 | 生产主力 |
| **Oracle 12c+** | ✅ build tag | v0.8.2 | 空 | ✗ tag=oracle | 信创关联，镜像启动慢 |
| **DM 8** (Oracle 兼容) | ✅ build tag | v0.8.3 | 双引号 | ✗ tag=dm | 信创主力 |
| **KingbaseES V9R1C10** (PG 兼容) | 🚧 进行中 | v0.8.4 (plan) | 双引号（postgres dialect 复用） | TBD | 信创扩展 |
| DM MySQL/PG/TD 兼容模式 | 🔮 候选 | v0.9+ | TBD | TBD | TD-18 真实下游需求 |
| DM 7 (老版本) | 🔮 候选 | 未计划 | TBD | TBD | TD-17，需求未明 |
| OceanBase | 🔮 候选 | 未计划 | TBD | TBD | 多模兼容（Oracle/MySQL）|
| GaussDB / openGauss | 🔮 候选 | 未计划 | TBD | TBD | openGauss 基于 PG，可能 postgres 复用 |

**图例**：✅ 已发布 / 🚧 进行中 / 🔮 候选未启动

**信创判断流程**（新增信创数据库时务必先做）：
1. 跑 driver migrator 实测建表 SQL
2. 看 `CREATE TABLE name (col TYPE)` 是 UPPERCASE 不带引号 → 走 Oracle 路径（空 quoter）
3. 看 `CREATE TABLE "name" ("col" TYPE)` 引号 lowercase → 走 PG 路径（双引号 quoter）
4. **不要**默认推广 Oracle 空 quoter 决策（v0.8.3 DM 实测推翻早期假设）

---

## 2. 已支持方言细节

### 2.1 SQLite / MySQL / PostgreSQL (v0.1.0+)

GORM 内置 dialect，gplus 仅做条件转义层（`getQuoteChar` 返回各方言标准 quoter）。默认 CI 用 SQLite 内存 DB 跑 899 test，MySQL/PG 走集成测试。

### 2.2 Oracle 12c+ (v0.8.2)

- **build tag**：`oracle`
- **quoter**：空（migrator UPPERCASE 不带引号建表，加双引号触发 `ORA-00904`）
- **已知限制**：CLOB / ROWNUM / NULLS LAST / RETURNING 单行 / 标识符长度 30 / 无 ON CONFLICT
- **测试基础设施**：`TEST_ORACLE_DSN` + `TEST_ORACLE_REQUIRED` 守卫（v0.8.4 对称化补全）
- **spec**：`docs/superpowers/specs/2026-05-07-oracle-support-design.md`
- **plan**：`docs/superpowers/plans/2026-05-07-oracle-support-plan.md`

### 2.3 DM 8 Oracle 兼容 (v0.8.3)

- **build tag**：`dm`
- **quoter**：**双引号**（v0.8.3 实测推翻 spec 早期假设；godoes/gorm-dameng migrator 用引号 lowercase 建表）
- **继承 Oracle 全部限制** + DM 特有 `COMPATIBLE_MODE=2` / 镜像默认密码版本差异
- **测试基础设施**：`TEST_DM_DSN` + `TEST_DM_REQUIRED` 守卫
- **部署陷阱**：WSL2 distro idle stop 拖死容器（详见 README "启动 DM 8 容器" 段，**适用所有 WSL2 Docker 方言**）
- **spec**：`docs/superpowers/specs/2026-05-08-dm-support-design.md`
- **plan**：`docs/superpowers/plans/2026-05-08-dm-support-plan.md`

---

## 3. 进行中

### 3.1 KingbaseES V9R1C10 PG 兼容 (v0.8.4)

- **状态**：spec/plan 4 commit 已完成，**实施未启动**
- **技术路径**：先试 GORM `postgres` dialect 直接复用（最低成本，验证 postgres dialect 是否够用）
- **范围**：仅做 PG 兼容模式（其他 3 模 Oracle/MySQL/Sybase 延后到 v0.9 多模架构期）
- **驱动**：官网 Gokb 2025-08-12 版（**不用** gitea 5 年旧版）
- **镜像**：官方 docker tar
- **spec**：`docs/superpowers/specs/2026-05-09-kingbase-support-design.md`
- **plan**：`docs/superpowers/plans/2026-05-09-kingbase-support-plan.md`

---

## 4. 候选未启动

### 4.1 DM MySQL/PG/TD 兼容模式 (TD-18)

- **真实下游需求**：DM MySQL 是常见生产配置
- **升级到 v0.9 大版本**：与 KingbaseES 4 模兼容统一架构合并设计
- **风险**：可能需要重构 dialect 子方言体系（dm → dm_oracle / dm_mysql / dm_pg）
- **触发条件**：真实 PR 需求 + KingbaseES v0.8.4 实施收尾

### 4.2 DM 7 (TD-17)

- 需求未明，搁置
- 涉及 sequence + trigger 自增、ROWNUM 重写未实现
- 主流装机量已迁 DM 8

### 4.3 OceanBase

- 信创扩展方向，蚂蚁系生态
- 多模 (Oracle / MySQL) 兼容，与 DM 多模架构同构
- 待真实需求触发立项

### 4.4 GaussDB / openGauss

- 华为信创方向
- **openGauss 基于 PostgreSQL**，可能 GORM `postgres` dialect 直接复用（类似 KingbaseES 路径，最低成本探针）
- GaussDB 商业版可能需要专用 driver
- 待真实需求触发立项

---

## 5. v0.9 大版本规划方向

**主题**：信创多模 dialect 体系架构

### 5.1 潜在改动

- 引入**子方言**概念：
  - `dm` → `dm_oracle` / `dm_mysql` / `dm_pg` / `dm_td`
  - `kingbase` → `kingbase_pg` / `kingbase_oracle` / `kingbase_mysql` / `kingbase_sybase`
- 抽象 dialect 行为为 interface：
  - quoter 各模式独立配置
  - 保留字集合独立
  - migrator 大小写策略独立
  - 错误码导航独立
- 测试基础设施统一：所有方言 `TEST_XXX_DSN` + `TEST_XXX_REQUIRED` 守卫模式
- 错误码导航 generalize 为通用 helper

### 5.2 触发条件

- TD-18 真实 PR 需求出现
- 或 KingbaseES v0.8.4 实施后用户要求 4 模都支持
- 或第 3 个多模信创数据库引入（OceanBase 等）

### 5.3 当前策略

保留 v0.8.x 单一方言模式（每方言一个 build tag），按需求触发再启动 v0.9 设计。**避免提前架构化**。

---

## 6. v1.0 候选方向

- **保留字自动检测** (TD-14)：检测 struct 字段名是 Oracle/DM 保留字时自动给 RawSQL 加引号
- **子查询 N+1 优化**：dataloader 模式
- **DataRule 表达式扩展**：含 OR / NESTED / 函数 / 子查询
- **多 schema 路由**：tenant ID → schema 自动切换

---

## 7. 贡献：如何添加新方言

参考 v0.8.2 Oracle / v0.8.3 DM / v0.8.4 KingbaseES 三轮经验，标准流程：

### 7.1 立项前（issue）

- 描述真实下游场景（避免理论需求）
- 确认目标方言的 driver 存在（GORM ecosystem 内）
- **关键**：跑 driver 的 migrator 实测建表 SQL 决定 quoter 策略（参考第 1 节"信创判断流程"）

### 7.2 设计阶段（spec）

- 用 `superpowers:brainstorming` skill 6 节探索
- 写 `docs/superpowers/specs/YYYY-MM-DD-<dialect>-support-design.md`
- 经 5+ 专家审计修订（参考 v0.8.3 DM 2 轮 6 专家流程；v0.8.4 KingbaseES 升级到 11 专家两轮）

### 7.3 实施计划（plan）

- 用 `superpowers:writing-plans` skill 转 spec 为 plan
- 写 `docs/superpowers/plans/YYYY-MM-DD-<dialect>-support-plan.md`
- **Task 0 环境前置检查**：把 spec §11 "plan 阶段待定项" 集中探测后写回（v0.8.3 留下的模式）

### 7.4 实施

- 用 `superpowers:executing-plans` 执行 plan
- **引入 build tag** `<dialect>`（避免污染默认 CI）
- `<dialect>_setup_test.go` 加 `TEST_<DIALECT>_REQUIRED` 守卫（对称 DM/Oracle 模式）
- `<dialect>_contract_test.go` 锁定 dialect 关键契约（quoter / Dialector.Name）
- `<dialect>_integration_test.go` 镜像 v0.8.2 Oracle 5 个 CRUD 路径

### 7.5 文档

- README 加方言支持章节（结构对齐 DM/Oracle 6 节：Quickstart / DSN / 下游集成 / quoter / 错误码 / 容器启动）
- CHANGELOG 记登记技术债 (TD-N)
- **本路线图**（第 1 节方言矩阵）更新

---

## 8. 关键参考

- 设计 specs：`docs/superpowers/specs/`
- 实施 plans：`docs/superpowers/plans/`
- 项目个人记忆（auto-loaded）：MEMORY.md（关键决策链接）
- CHANGELOG：每版本技术债 (TD-N) 登记
- 已知部署陷阱：README "DM 数据库支持" 章节（WSL2 workaround 对所有 Docker 方言适用）
- 质量基线：gosec + golangci-lint + gofmt 全 0 issue（v0.8.4 quality 三连 commit 7ac57f8 / b694795 / ad109e5 建立）
