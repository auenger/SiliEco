# Task / AgentRun 一次性统一重构方案

日期：2026-11-06。

状态：代码切换、定向验证、三端构建和内测服务器发布已完成。用户已批准按本方案推进，并要求避免非必要的全量测试；当前没有客户，现有测试数据可清理，不承担旧数据和旧客户端迁移成本。桌面包与 iOS 产物未签名，iOS 真机安装和联网尚未验证。

本版替代此前的渐进兼容方案。采用一次性破坏性切换：全栈改为 Task / AgentRun，重建测试数据库，同步升级服务端、客户端、CLI 和 Daemon，不保留 Issue 兼容实现。

## 1. 最终模型

- **Task**：业务任务，可分配给人、Agent 或蜂群，包含标题、描述、父子关系、项目、SOP 阶段、评论和业务状态。
- **AgentRun**：一次 Agent 执行记录，包含排队、运行、消息、结果、错误、用量、运行环境和重试关系。
- 一个 Task 可关联零到多个 AgentRun；一个 AgentRun 可不关联 Task，例如独立聊天或自动化执行。
- WorkflowRun、StageExecution、Runtime、AgentSession 继续保留独立职责。

目标是统一术语和实现，保持业务状态机、调度、权限和删除行为。命名重构本身不承诺运行时性能提升。

## 2. 重构前的迁移状态

已有 Task 文案、`/api/tasks`、`/api/runs`、`silieco task` 和输出 `/tasks` 的路径工具。但内部仍存在：

- `Task = Issue` 类型别名，`packages/core/tasks/*` 转导出 `issues/*`。
- Task API 方法包装 Issue API 方法。
- 业务 `IssueService` 与执行 `TaskService` 并存。
- `issue`、`agent_task_queue` 表，以及含义不同的 `issue_id` / `task_id`。
- `issue:*` 业务事件和 `task:*` 执行事件。
- 三端目录、组件、缓存、schema、持久化键和文档中的旧名称。

本轮将 Task 变成唯一真实实现，将执行层明确改为 AgentRun，删除上述中间层。

## 3. 命名契约

| 当前名称/含义 | 新名称 |
| --- | --- |
| 业务 `Issue` | `Task` |
| `IssueStatus` / `IssuePriority` | `TaskStatus` / `TaskPriority` |
| `IssueService` | `TaskService` |
| 执行 `AgentTask` / `AgentTaskQueue` | `AgentRun` |
| 当前执行 `TaskService` | `AgentRunService` |
| 执行 `TaskMessage` / `TaskToken` | `AgentRunMessage` / `AgentRunToken` |
| 业务 `issue_id` / `issueId` / `IssueID` | `task_id` / `taskId` / `TaskID` |
| 执行 `task_id` / `taskId` / `TaskID` | `run_id` / `runId` / `RunID` |
| 业务 `parent_issue_id` | `parent_task_id` |
| 执行重试 `parent_task_id` | `parent_run_id` |
| `WorkflowExecution.main_issue_id` | `main_task_id` |
| `WorkflowExecution.main_task_id` | `main_run_id` |
| 业务 `issue:*` 事件 | `task:*` |
| 执行 `task:*` 事件 | `run:*` |
| `issue` 表及业务 `issue_*` 表列 | `task` / `task_*` |
| `agent_task_queue` 及执行消息、令牌、用量表 | `agent_run` / `agent_run_*` |

裸 Task 只表示业务任务；执行类型使用 AgentRun，执行 API 路径使用 `/runs`。

**不能全局替换 task。**部分 `WorkflowStageRun.task_id` 已表示业务任务；聊天、评审恢复、失败记录中的字段须逐项查明。当前同时包含 `task_id` 和 `issue_id` 的帧，先将执行字段改为 `run_id`，再把业务字段改为 `task_id`。

## 4. 删除过渡设计，直接切换契约

- 删除内部 Issue 类型、方法、目录、hooks、schema 和缓存别名。
- 删除 `/api/issues`、旧 `/issues` 页面、`silieco issue` alias 等旧入口。
- 删除原 `/api/tasks/{runId}/messages`、`/cancel` 等混用执行 ID 的旧接口，执行操作统一走 `/api/runs`。
- 直接采用新 HTTP / WS / Daemon 协议，不新增 `/api/v2` 或新旧 DTO 适配器。
- 不保留 `issue:*` 或执行语义的 `task:*` 事件，不双发同义事件。
- 不搬迁旧草稿、标签页、筛选、缓存和 token；清理本项目客户端状态后重新登录。
- 不迁移旧评论中的 `mention://issue/...`，新写入与解析统一为 `mention://task/...`。
- 不保留旧数据库升级链作为新安装入口，历史通过 Git 追溯。

新版本只支持同批发布的新契约。协议和 schema 设置明确的代际检查，拒绝不支持的旧进程，避免把旧 `task_id` 当成新语义解释；这不是旧协议适配器。切换前停止旧客户端、Daemon 和服务。

API 的 Zod 校验、错误处理、权限检查和未知枚举防护继续保留。删除兼容代码不等于删除边界校验。

## 5. 全栈改动范围

### 后端与执行链路

- Go handler、service、事件、辅助包、SQL 查询名和 sqlc 生成类型。
- Task CRUD、分配、评论、附件、订阅、父子任务、列表、表格、项目、SOP 和 Inbox。
- AgentRun 认领、启动、进度、消息、完成、失败、取消、重试、用量、归因和 GC。
- HTTP / WS / Daemon RPC 的方法、字段、事件及订阅 scope。
- 执行身份改为 `X-Run-ID` / `SILIECO_RUN_ID`；需要业务关联时明确使用 Task ID。执行 token 仍绑定一次执行，不扩大权限。
- CLI 实现、JSON 输出、帮助文案、内置 Skills、MCP 上下文与 source map。
- 日志、指标、告警查询、seed、回填工具和测试 fixtures。

### Web / Desktop / 共享包

- 真正将 `packages/core/issues`、`packages/views/issues` 迁入 `tasks`；my-issues 改为 my-tasks。
- Task 成为原始定义；AgentRun 有独立执行类型和模块。
- 同步修改 API 方法、schema、Query keys、Mutation、缓存协调器、WS updater 和消费者。
- 业务缓存使用 tasks，执行缓存使用 agent-runs，均保留 workspace 隔离。
- 同步更新 i18n namespace、路由函数、收藏及通知实体类型。
- Electron main、preload、IPC、窗口参数和独立窗口使用 Task，例如 `--silieco-task-window=`。
- 更新持久化 key 和清理注册，采用新存储代际或重置本应用状态，不读取旧草稿和布局。

### Mobile

- 独立修改 API、schema、queries、realtime、stores、页面和导航。
- `issue/[id]`、`more/issues`、`my-issues` 改为 Task 命名。
- 清理本应用旧登录和缓存后重新登录，不导入 Web/Desktop hooks。
- 保持三端数量、权限、状态和实体身份一致。

### 容易漏掉的字段与入口

- 列表 `issues`、行对象 `issue`、`active_issues`。
- Inbox 的 `issue_id`、`issue_status`、`issue_assigned`、`issue_subscribed`。
- `PinnedItemType`、`AttachmentReferenceTargetType` 中的 `issue` 值。
- workspace `issue_prefix`、project `issue_count`、resource `source_issue_*`。
- mention、通知深链接、`/inbox?issue=`、桌面窗口上下文。
- SOP 主 Task / 主 Run、自动化、附件来源和聊天消息的执行关联。

## 6. 数据库：建立新的当前基线

建议直接建立 Task / AgentRun 新建库基线，不追加一串 rename migration 延续旧命名链。此项是明确的工程子任务，不只是重写最初的 init.sql。

1. 在隔离临时库执行现有全部迁移，得到完整当前 schema，清点列、默认值、约束、函数、触发器、扩展、索引和必需种子数据。
2. 按语义映射生成新基线，保持当前功能结构。活跃 migrations 目录只保留新基线及其后续增量，旧链由 Git 保存，不再输入 sqlc 或安装器。
3. 所有显式索引，包括 unique index，使用 `CREATE INDEX CONCURRENTLY` / `CREATE UNIQUE INDEX CONCURRENTLY`，每条独立 migration 文件，并提供 down 文件。
4. 遵守现行不新增外键或级联的约定。旧 schema 仍有历史 FK/cascade，不能直接 dump 后原样作为新迁移提交；必须核查父记录删除、关联校验和依赖清理，由应用层及必要事务承担，并增加对应回归验证。
5. 保留 checks、默认值和唯一性等当前业务保证；必要的清理代码调整以维持原行为为目标，不扩展业务规则。
6. 更新 SQL 源文件并执行 `make sqlc`，提交生成结果，不手改生成文件。
7. 清理旧库专属升级 hooks、失效回填路径及历史升级测试。日常仍有用途的回填工具按新命名保留；业务断言迁到新基线测试。
8. 同步更新 migration lint、runner、readiness、seed、Docker 和自托管初始化脚本。
9. 新基线只接受空库；旧 schema 明确拒绝，不在正常启动或 `migrate up` 中自动删库。
10. 验证空库安装、重复执行、并发迁移、健康检查及业务约束。

现有迁移有针对 103、198 的硬编码 pre-hooks，测试还依赖特定旧版本；readiness 检查全部 migration 版本，sqlc 直接读取 migrations。不能只替换 DDL 而遗漏配套实现。

基线重建保留所有当前功能所需结构，不是仅留下 Task 和 AgentRun 两张表。

## 7. 分步开发，一次发布

| 步骤 | 工作 | 完成条件 |
| --- | --- | --- |
| 1 | 固化词典、ID 映射，更新开发约定 | 字段、事件、状态和表列含义明确 |
| 2 | 执行 Task 改为 AgentRun | 释放业务 Task 的命名空间 |
| 3 | 业务 Issue 全量改为 Task，三端与协议同步 | 无别名、转导出和旧调用方 |
| 4 | 新基线、SQL、生成代码和初始化工具 | 空库可用，约束及删除清理完整 |
| 5 | 测试、构建、文档和静态残留检查 | 全栈关键流程通过 |
| 6 | 停旧版本、重置测试状态、整套部署 | 新契约下重新登录和注册后验收 |

一次性指一次契约切换和发布。开发仍按依赖拆分可审核提交，半成品不发布，新前端不单独上线连接旧后端。

## 8. 服务器与客户端重置

实施时先确定环境、数据库名、缓存前缀和存储路径，再执行针对该项目的清理。

1. 停止调度、新请求、旧 Daemon 与客户端，处理活动 Agent 子进程，防止旧执行继续回报。
2. 停止旧服务，重建指定测试数据库，包括旧 `schema_migrations`。
3. 清理本项目 Redis stream、缓存与执行提示，避免重放旧协议；共享 Redis 只处理本项目前缀。
4. 清理绑定测试数据的本项目上传对象、附件数据和 Runtime 恢复元数据。用户代码仓库、Agent 登录凭据及其他项目目录不属于应用测试数据。
5. 新基线建库，初始化测试账号、空间和数据，部署匹配的后端、Web、Desktop、Mobile、CLI 与 Daemon。
6. 重置本应用客户端存储或切换代际，重新登录；Daemon 重新绑定和注册 Runtime，重新生成应用 token。
7. 验证业务任务、聊天执行、SOP、消息与用量，确认无旧进程重连。

保留可复现的部署包及环境配置。需要退回旧代码时，用对应旧基线重新初始化测试环境；不承诺旧二进制运行在新 schema 上，也不承诺恢复已明确丢弃的测试数据。

## 9. 验收

静态验收：活跃领域实现没有业务 Issue 类型、旧导入、旧 API、旧事件、旧 CLI alias 和双命名包装。Task / AgentRun 的类型、表列、JSON 和日志语义一致。

不要求英文 issue 在全文为零：第三方 GitHub Issue、Zod `error.issues`、普通英文“问题”不属于业务实体。静态检查明确限定范围。

行为验收：

- 人工任务无 AgentRun 可完成；一个 Task 可关联多次执行，重试父记录正确指向 Run。
- 聊天和自动化执行可不关联 Task。
- 评论触发、认领、取消、完成竞争、用量、归因及权限保持正确。
- SOP 阶段、评审、验收、返工及主 Task / 主 Run 关联正确。
- WS 实时发送、Redis 重放、订阅 scope、游标和重连均使用新协议，无重复消费。
- 三端列表、表格、详情、父子任务、Inbox、收藏、附件和 mention 正常。
- 执行 token 按 Run 授权，跨空间访问和错误 ID 不能越权。
- 空库安装、重复迁移、并发迁移、健康检查、父对象删除及关联清理通过。

```bash
make sqlc
pnpm typecheck
pnpm lint
pnpm test
pnpm -C apps/mobile typecheck
pnpm -C apps/mobile lint
pnpm -C apps/mobile test
make test
pnpm exec playwright test
```

补充 Go / Web / Desktop 构建、iOS Release 构建及三端关键流程验收。根脚本排除 mobile，不能省略独立检查。Agent 自动化测试使用 fake executable，真实账号 smoke test 不属于默认验证。

删除旧兼容测试时，将仍有价值的业务断言迁入新契约测试，不通过删测试掩盖回归。

## 10. 实施结果（2026-11-06）

- 后端、sqlc、CLI、Daemon、共享包、Web、Desktop 和 Mobile 已切换为 Task / AgentRun。业务身份使用 `task_id`，执行身份使用 `run_id`；不存在内部 Issue 别名、转导出模块和旧 Issue API / 页面 / CLI 入口。
- HTTP 使用 `X-Silieco-Protocol: task-run-v1`；WS query 与 token-auth 首帧使用 `protocol_generation: task-run-v1`。旧协议明确拒绝。客户端存储切换代际；旧服务端 revision 绕过及聊天分页回退已删除。
- 新基线包含 107 张业务表；373 个迁移文件在 PostgreSQL 16/17 验证。旧 107 条外键由应用层关联校验、事务清理和删除锁闭包替代；全部 21 个聚合删除调用入口、真实并发和独立审查完成，详见[数据库审计](2026-11-06-task-database-audit.md)。
- Go 编译、Task CRUD、父子关系、执行权限、SOP、实时协议、关系删除和并发的定向测试通过；各端类型、缓存、schema、登录与执行展示的定向检查通过。没有运行根全量 TS / Go 测试或完整 Playwright 套件，没有调用真实模型。
- Web 生产编译与类型检查通过；Desktop macOS x64 构建、内置 CLI 与压缩包校验通过；Mobile Hermes 导出和原生 iOS arm64 Release 编译通过。签名、公证、真机安装及移动端运行时联网不在本次已验证范围，详见[移动端记录](../../apps/mobile/docs/task-run-validation.md)。
- 服务器 `silieco-internal` 已停止旧服务并重建 `silieco_internal` 测试库、清理指定附件卷；没有配置 Redis / S3，因此未执行这些服务的清理。旧业务数据和 token 已丢弃，仅保留原 Demo 管理员的邮箱、名称和密码登录凭据。
- 新测试空间为 `task-run-test`，保留 1 个手工 Task。已验证人工任务无 AgentRun 也能完成；最终 AgentRun 数量为 0。Demo Daemon 使用新 PAT 重新注册，CLI 为 `v0.0.0-32-gc4b3982-dirty`。
- 公网 API / WS 验证通过：Task CRUD、评论、事件、旧协议拒绝、Task / Run 身份隔离；浏览器任务页、31 个 API/XHR、108 个静态资源、cookie-auth WS 和下载页通过。
- 下载页只提供本次 macOS Intel ZIP；旧安装包已移出公开目录。其他平台没有同批可安装包时显示不可用。包括 PostgreSQL 在内的 8 个原有容器 ID 与运行状态保持不变；用户代码仓库和模型凭据保留。

发布目录：本机 `dist/task-run-2026-11-06/`，服务器 `/opt/silieco-internal/releases/20261106-task-run/`。保留源码快照、镜像、二进制、校验清单、脱敏验收结果和旧程序/配置。当前改动尚未提交 Git。

旧镜像不能直接连接新基线；若退回旧代码，需要按对应旧基线重新初始化测试库。本次不承诺恢复已明确丢弃的旧测试数据。

完整结果及验证边界见[验证记录](2026-11-06-task-domain-validation.md)。
