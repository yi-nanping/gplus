# 数据库测试驱动隔离任务清单

计划：[实施计划](../plans/2026-10-07-test-driver-isolation.md)。

## T1 基线与分类（AC-2、AC-3）

- [x] 确认工作区、生产驱动导入、当前模块与 CI 配置。
- [x] 执行 SQLite 基线：`TEST_DB=sqlite go test ./...` 通过。
- [x] 用语法与类型信息识别生产私有符号依赖，记录用例迁移前后清单。
- [x] 确定并落实白盒/数据库混合测试的拆分方式。

## T2 测试模块与迁移（AC-2、AC-3）

- [x] 创建 `tests/go.mod`，保留当前驱动版本，replace 指向根源码。
- [x] 无驱动白盒测试保留根模块；数据库测试迁入独立模块。
- [x] 保留混合用例的私有断言和公开行为断言，不暴露生产接口。
- [x] 修正迁移后的 testdata、类型约束编译测试和辅助模型路径。
- [x] 核对原有 Test/Benchmark/Example 清单无遗漏，两个模块编译及 SQLite 测试通过。

## T3 依赖与开发入口（AC-1、AC-4、AC-6）

- [x] 两个模块分别 tidy，根清单不再包含本项目测试驱动。
- [x] CI 分别执行核心和数据库测试，缓存包含两份依赖清单。
- [x] README、AGENTS.md、CLAUDE.md 说明驱动选择与新的测试入口。
- [x] 执行 build、vet、race；核对 Oracle/DM 带 tag 的编译和未配置 Skip。

## T4 下游隔离验收（AC-5、AC-6）

- [x] `GOWORK=off` 独立消费者：无驱动引用时无驱动编译依赖。
- [x] 仅 MySQL 消费者：公开 API 可编译，无其他数据库驱动编译依赖。
- [x] 仅 PostgreSQL 消费者：公开 API 可编译，无其他数据库驱动编译依赖。
- [x] 区分 GORM 上游 SQLite 模块声明与实际编译依赖。
- [x] 记录真实数据库、SQL DryRun、编译与 Skip 各自的验证边界。

## 执行证据

- 状态：T1–T4 已完成；以下为隔离实施阶段的本地验收记录，后续按用户授权发布 v0.14.0。
- 基线提交：`629ace1`。初始 SQLite 基线通过，核心包耗时约 1.5 秒。
- 类型分析纳入 Oracle/DM 文件，94 个测试文件中 39 个访问生产私有符号，分析类型检查无错误。按声明拆分，私有断言保留根模块，真实驱动与数据库断言保留测试模块。
- 原有 619 个入口全部保留：597 个 Test、16 个 Benchmark、6 个 Example。迁移后共 638 个入口、620 个唯一名称；18 个名称分置两个模块互补验证，新增 1 个真实 MySQL/PostgreSQL 驱动离线投影用例。
- 生产 Go 源码与公开 API 无修改。测试和驱动版本未升级；根 go.mod 只直接依赖 GORM v1.31.1，另有 3 项必要间接依赖；测试驱动全部由 tests/go.mod 声明。
- 空 preload 防御分支保留根私有注入及 SQL 等价验证，tests 中对应无 preload 查询保留真实行数、ID、Name 结果验证；未使用反射或新增生产接口注入私有状态。

| 验证层 | 实际命令/结果 | 边界 |
| --- | --- | --- |
| 核心构建/静态检查 | 根 `go build ./...`、`go vet ./...` 通过 | 未修改生产逻辑 |
| 核心单元测试 | 根 `go test -race ./...` 通过 | 无驱动白盒及 SQL 构建 |
| 测试模块静态检查 | tests 中 `go vet ./...` 通过 | 包含实际驱动导入 |
| SQLite 数据库测试 | tests 中 `TEST_DB=sqlite go test -race ./...` 通过 | 真实内存 SQLite；MySQL/PG 离线驱动 SQL 也通过 |
| 依赖稳定性 | 两个模块 `go mod tidy -diff` 均通过 | 根不会回添测试驱动 |
| 下游隔离 | 根 `go run ./scripts/check-driver-deps.go` 三消费者通过 | GOWORK=off；验证编译依赖及版本，只编译、不连接数据库 |
| 可选驱动编译 | tests 中 `go test -tags=oracle,dm -run '^$' ./...` 通过 | 不代表 Oracle/DM 运行时通过 |
| 可选套件选择 | tests 中 `TEST_DB=sqlite go test -tags=oracle,dm -json ./...` 通过，32 个 Skip 事件 | 本机未配置 MySQL/PG/Oracle/DM DSN，对应真实数据库用例 Skip |
| 空 preload 补充 | 两个模块 `go test -race -run '^TestApplyPreloads_EmptyQuery$'` 通过 | 分别验证私有分支和真实查询结果 |
| 完整性复审 | 入口清单及重点写入、数据隔离、事务回滚断言复核完成 | 无未解决阻断项；未将 DryRun 当作真实数据库证据 |

## 上游边界与后续使用

GORM v1.31.1 的 go.mod 自身仍声明 SQLite 间接依赖。独立消费者的模块构建列表可能包含它，但无驱动消费者的实际编译依赖没有驱动包；MySQL/PG 消费者也没有未选择的其他驱动包。核心测试依赖隔离不承诺改变 GORM 上游模块图。

CI 已改为分别运行根模块和 tests 模块的检查，并保留 MySQL/PG 服务与三库 race 入口；隔离实施阶段未在 GitHub 执行 CI，也未执行这四类真实数据库验证。v0.14.0 发布阶段将核对该提交的 CI 结果，再创建标签；当前历史标签不受变更影响。

## 本地分段提交

- 用户先授权本任务逐步本地提交，随后明确授权 push 和发布标签；发布版本为 v0.14.0。
- `77db263`：制定隔离计划与验收标准。
- `879016b`：测试迁移、核心依赖清理、CI 与消费者依赖检查。
- 使用文档和本验收记录由本文件所在的文档提交归档。
