# 数据库测试驱动隔离实施计划

## 目标与边界

用户自行选择并安装当前项目需要的 GORM 驱动，创建 `*gorm.DB` 后传入 gplus。gplus 核心模块不声明本项目的数据库测试驱动；驱动继续用于完整的数据库回归测试。

- 保持公开 API、生产代码和现有查询、事务、数据规则语义。
- 不升级 GORM 或数据库驱动，不新增驱动注册机制，不为测试暴露生产私有接口。
- 保留白盒测试及数据库可观察行为验证；不得以删除用例或普遍 Skip 实现依赖清理。
- GORM v1.31.1 自身声明 SQLite 间接依赖，因此不承诺整个模块构建列表中不存在驱动。验收区分核心清单、模块图和实际编译依赖。

## 依据

- 当前 `repository.go:126` 的 `NewRepository` 已接收用户的 `*gorm.DB`；生产代码无驱动导入。
- 当前根 `go.mod` 的五类驱动由测试使用，根测试大量访问私有符号，不能仅移动目录。
- GORM 使用独立 [tests/go.mod](https://github.com/go-gorm/gorm/blob/b3d3bf219f0283f8e2e985bac509cb643170f729/tests/go.mod)，本地 `replace` 指向核心源码。
- Bun 将驱动放入独立 [internal/dbtest/go.mod](https://github.com/uptrace/bun/blob/0b7aa4629a4a5965c9d710e3b74c39ea3ccdcb2a/internal/dbtest/go.mod)。
- [Go module 规则](https://go.dev/ref/mod#go-mod-tidy)：tidy 会考虑测试导入和自定义 build tag；仅删除 require 或添加 build tag 不能隔离依赖。

## 实施阶段与验证

1. **基线与测试分类**：记录 SQLite 基线、测试入口和私有符号依赖；区分无驱动白盒、公开接口数据库测试及混合测试。验证迁移前用例清单。
2. **模块与测试迁移**：创建 `tests/go.mod`，使用本地 replace；保留根无驱动白盒测试，将数据库测试迁入独立模块。混合用例按语义拆分，数据库断言尽量通过公开 SQL、错误和数据库结果表达。验证两个模块编译与 SQLite 回归，核对用例是否遗漏。
3. **依赖、入口与文档**：整理两份 go.mod/go.sum；CI 分别执行核心和数据库测试；更新 README、贡献指南、开发命令。验证 tidy 幂等、构建、vet、race 和可选数据库编译。
4. **下游验收与交付**：在关闭 workspace 的独立临时消费者中验证无驱动、仅 MySQL、仅 PostgreSQL 的引用和编译依赖。记录实际执行与未执行数据库验证，不将 SQL DryRun 或编译提升为真实数据库通过。

## 验收标准

- AC-1：根 `go.mod` 不声明 SQLite/MySQL/PostgreSQL/Oracle/DM 测试驱动；根 tidy 不回添。
- AC-2：用户继续使用 `NewRepository(db)`，公开 API 与生产行为不变。
- AC-3：迁移前的测试函数与关键子测试有去向；白盒断言和数据库行为覆盖不丢失。
- AC-4：核心模块与测试模块分别通过适用的 build、vet、SQLite race；CI 显式覆盖两个模块。
- AC-5：独立消费者的实际编译依赖仅包含用户所选驱动；测试模块的 replace 不进入发布核心清单。
- AC-6：MySQL/PostgreSQL/Oracle/DM 的执行、编译及 Skip 分别记录；不读取或使用生产凭据。

## 当前基线

- 2026-10-07：工作区干净；`TEST_DB=sqlite go test ./...` 通过。
- 当前根目录共 619 个 Test/Benchmark/Example 顶层声明（包含带 build tag 用例）；迁移时保留清单用于核对。

执行状态与证据见 [任务清单](../tasks/2026-10-07-test-driver-isolation.md)。
