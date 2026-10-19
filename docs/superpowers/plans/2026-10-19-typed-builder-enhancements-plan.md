# 类型化更新、投影别名与列比较计划

## 目标与边界

实施从 v0.14.0 工作树开始，补充下游 v0.14.1 反馈的三个公共 API。提交前发现远端 main 已到 v0.14.1（b90d575），已快进同步并保留上游审计修复；后续分段提交基于该版本。不修改既有 IncrBy 契约，不修改 gvs-server，不发布版本。

- `Updater.Incr(field any, delta int64)`：构建主模型字段的自增赋值，与 Set/SetExpr 共用赋值集合，由 UpdateByCond/Tx 执行。
- `Query.SelectAs(field any, alias string)`：按字段指针解析来源列，结果别名独立保存、校验并按方言转义。
- `Query.EqColumn(left, right any)`：WHERE 两侧均为字段引用，支持当前链及外层查询；右侧不得作为普通值绑定。

新入口只接受有效字段指针；不接受原始列名字符串。Incr 限定主模型字段；SelectAs/EqColumn 限定当前查询链及可见外层作用域。保留错误累积和 Build 阶段拒绝执行的机制，不另建 Repository 执行路径。

## 实施任务

- [x] T1 原子自增：新增 Incr 及无驱动测试。列引用通过 GORM clause.Column 渲染，delta 参数化；同字段沿用 Set 的最后一次赋值覆盖语义。
  - 验证：SQL 中 Set、自增和 CAS 共处一条 UPDATE；column tag、方言引用、非法字段、Select/Omit；真实 SQLite 的成功/冲突、DataRule、Context、事务回滚；既有 IncrBy 语义回归。
- [x] T2 投影别名：新增 SelectAs，扩展内部投影项及统一渲染，不改变已有 Select/SelectRaw/SelectExpr 行为。
  - 验证：JOIN alias、column tag、混合投影、保留字别名、非法别名/指针、Count/FindAs 投影映射及错误短路。
- [x] T3 列比较：新增 EqColumn，内部条件保存独立右列引用，复用既有条件分组与子查询构建链。
  - 验证：同表/JOIN、带别名和无别名相关子查询、多层作用域、自关联使用不同 alias、column tag、跨链/撤销/非法字段拒绝；真实 SQLite 的 NOT EXISTS 和软删除行为。
- [x] T4 文档与整体验收：更新 README 用法；复查 API 边界、错误机制和兼容性。
  - 验证：两模块 go test、go vet、go mod tidy -diff；可用时 race；根模块 go build 与 check-driver-deps。SQLite 显式选择；MySQL/PG/Oracle/DM 无可用专用测试环境则标记未执行/跳过，SQL 构建断言不算真实数据库验收。

## 分工与落盘

主线程负责 SelectAs、EqColumn、共享 builder、文档与最终整合；一个实现任务负责独立的 updater_incr.go 及单元测试，另一个任务负责独立的 tests/typed_builder_test.go 数据库验收。每个任务不得覆盖其他人的修改。

完成每项后更新本清单，记录实际验证结果。用户后续已授权分段 commit 后 push；按以下四段提交，每段使用暂存区导出的独立快照验证：

1. 修复主别名、子查询 FROM 及软删除引用，附独立回归测试。
2. 新增 Updater.Incr 及无驱动测试。
3. 新增 SelectAs / EqColumn 及查询、数据库验收测试。
4. 更新 README 和本计划的验收记录。

## 执行结果

### 已完成验证

- 根模块 `go test ./... -count=1`、`go test -race ./... -count=1`：PASS。
- tests 模块 `TEST_DB=sqlite go test -json -race ./... -count=1`：同步 v0.14.1 后，第三段暂存区独立快照 PASS，859 个测试项（含子测试及示例）通过，14 个可选 MySQL/PG 测试项跳过，失败 0。新增用例覆盖单条 UPDATE、影响行数、CAS、DataRule/OR、取消、回滚、投影映射、软删除孤儿查询及同模型不同 alias 的真实自关联。
- 两模块 `go vet ./...`、`go mod tidy -diff`：PASS，依赖文件无变化。tests tidy 所需已锁定依赖首次缺少本地缓存，取得沙箱扩展后下载并通过。
- 根模块 `go build ./...`、`go run ./scripts/check-driver-deps.go`：PASS；独立消费者 none/mysql/postgres 构建与驱动依赖隔离通过，未连接数据库。
- 实际数据库验收范围是 SQLite；MySQL/PostgreSQL/Oracle/DM 未做真实数据库验收。
- 第一、二、三段均分别导出暂存区完整快照，快照内根模块和 tests 模块全量 race 均通过；原子更新与查询数据库测试已拆分，前一段提交不依赖后续 API。

### 必要接入修复

定向测试先复现了 SubQueryAs 清除 FROM alias 以及命名/多层指针嵌入被所有权检查拒绝的问题。前者由本轮保留并物化主别名；后者在同步 v0.14.1 后直接保留上游使用 schema 注册遍历路径的修复，没有重复实现。

初始基线的默认表名推导简单追加 s，会将 Company 错解为 companys，导致类型化列前缀与 GORM 的 companies 表不一致。失败测试确认后采用 GORM 默认 NamingStrategy；同步 v0.14.1 时保留其已有的同项修复，本轮增加无驱动 SQL 及 SQLite 实际投影/子查询验证。

物化带引号的主别名时，GORM 的 Table 正则不能提取该别名，需要同步 Statement.Table，确保自动软删除及主键排序引用同一 alias。修复后两个既有“已知限制”反向测试不再成立，已改为验证正确返回行及不复制软删除主记录，并同步 README。SubQueryAs 同时拒绝遮蔽外层 alias 的重名主别名。

所有实现任务完成；`git diff --check` 通过。本轮未自行调整依赖版本，测试模块版本变化来自同步上游 v0.14.1。分段提交与 push 按后续授权执行，远端 CI 结果以交付报告为准。
