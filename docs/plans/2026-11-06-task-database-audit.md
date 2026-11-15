# Task / AgentRun 数据库基线与关系完整性审计

审计日期：2026-11-06。源版本：`c4b3982c1ef304daf8aa86532a1c95ff94a7b14f`。此文记录本次一次性重构的数据库证据；不代表已经清理或部署服务器。

## 基线来源和边界

在隔离的本地 PostgreSQL 中顺序执行源版本全部 377 个升级文件，得到最终有效模式，再统一命名并生成新基线。没有直接截取最早的建表文件。业务对象为 `task` / `task_id`，执行记录为 `agent_run` / `run_id`；工作流的 `main_task_id` 指业务任务，`main_run_id` 指执行。

新模式保留 107 张业务表、1,191 个字段、310 个非外键约束、371 个索引、13 个应用 SQL 函数和 8 个触发器，以及默认值、检查约束、pgcrypto / pg_trgm 扩展和用量汇总状态种子。源模式经过同一命名映射后逐项对比这些目录对象，数量和定义一致；PostgreSQL 16 与 17 的这五类目录也逐项对比，全部零差异。被去除的只有 107 条外键：79 条删除依赖、18 条置空、7 条 NO ACTION、3 条 RESTRICT。历史兼容数据回填和升级钩子已删除，运行中仍使用的汇总互斥锁保留。

`0001_task_domain_baseline` 为空库建立表/函数/触发器；`0002` 至 `0372` 每个文件只创建一个 CONCURRENTLY 索引；`0373_attach_key_constraints` 以已有唯一索引附加主键/唯一约束。反向顺序先拆键约束，再拆索引，最后删除业务对象。迁移器持有迁移互斥锁后检查代际：旧迁移记录或未跟踪旧表都会使迁移失败，绝不自动清空数据库。

## 应用层替代规则

- SQL 写入使用父项存在校验和 `FOR KEY SHARE`。43 个 VALUES 插入改为带校验的 INSERT SELECT，19 个关联改绑 UPDATE 增加校验；Agent、Squad、RuntimeProfile、Comment、重试和标签绑定等已有 SELECT/专用路径也逐项补齐。`:one` 在父项缺失时返回 `pgx.ErrNoRows`；`:exec` / `:execrows` 不写入或返回零行。可空引用仍可为 NULL，NOT NULL 约束照常保留。
- 16 类聚合删除的 SQL 显式删除依赖并清空幸存记录引用，复用原来租户条件和返回值，全部在调用方事务中执行。多级 Comment 使用递归目标集合；同一张表的置空合并在一次 UPDATE，避免一个语句内重复改写同一行。
- 单条清理语句的快照不足以处理“等待父项锁期间，有新后代提交”。`core/pkg/db/relational/cleanup.go` 因此先锁根对象，再以独立 SELECT 按父到子、同表 UUID 顺序锁定所有可能接收依赖的后代表（包括限制删除关系的子项）；反复获取新快照，直到没有新增父行，之后才允许执行清理。辅助器要求同一个 READ COMMITTED 事务；它只加锁，不删除数据。运行中 21 个生产删除调用点已接入。
- 批量 Runtime/Profile 删除先锁所在 Runtime/Profile，再锁该范围全部 Agent/Squad，包括当前未归档的候选；最终删除 SQL 仍只删除归档行。这防止并发 Archive/Restore 在检查后扩大未冻结的删除目标集。加锁顺序保留 ChatSession → AgentRun 等发送/结束执行路径约定。工作流等本来就没有外键的表沿用现有事务清理，不将本次审计扩展成新的数据库约束。
- 新增关系或新硬删除入口必须同时更新应用关系元数据、写入校验、删除事务和对应并发回归。删除谓词若依赖可变列，必须冻结完整候选范围及其归属父项，不能仅锁当前匹配的子集。直接 SQL 运维写入不经过应用规则，不具备数据库外键保护。

限制删除的十条关系：User 的引用没有可达用户硬删除入口，新增/归档写入校验用户存在；AgentRuntime 删除在锁定后拒绝/解除剩余 Agent 绑定，Profile 删除拒绝活动 Agent，再清理归档 Squad/Agent；Squad 负责人只能通过 `GetAgentInWorkspace` 选择 `kind='user'` 的 Agent，因此系统 Builder 删除没有合法 Squad 负责人依赖。删除归档 Agent 前会锁定、校验仍存活的 Squad，避免把原 RESTRICT 悄悄改成无条件删除。

旧 Lark 表、`daemon_connection`、`task_dependency`、`github_pull_request_check_suite` 等保留了模式及显式清理，但当前生产源码没有新增/改绑入口；不能把这些历史表误写为当前可调用功能。

## 验证证据

- PostgreSQL 17：真实迁移器空库 up、重复 up、down 均通过；旧模式拒绝并保留原数据；迁移并发/代际防护/索引规范和归因 CHECK 窄测通过。
- SQL：全部 811 条 sqlc 生成查询在真实 PostgreSQL PREPARE 成功，sqlc 生成通过。不是仅靠 Go 编译判断 SQL 正确。
- 关系回归：Task 删除递归清理评论/回复/反应及 Run 消息/令牌/用量；子 Task 保留并清空父引用；幸存 Run 清空父 Run 和触发评论；错误 workspace 不删除；Workspace 删除不影响其他 workspace/User。
- 并发回归：未提交的 RunMessage 写入持有父 Run 共享锁，Task 删除真实等待；写入提交后清理使用新快照，消息和 Run 同时删除。删除后新消息拒绝写入。全部 16 类辅助器目标在真实数据库执行加锁检查。
- 写入反例：缺失 Agent、缺失 Task 父项、缺失回复父评论、跨 workspace Runtime 绑定均拒绝；保留合法 NULL 关联。
- 动态目标集回归：Runtime 删除加锁后，活动 Agent/Squad 的并发归档真实等待（以 PostgreSQL lock_timeout 验证），覆盖独立审查发现的归档状态竞态。
- 测试命令：`DATABASE_URL=<隔离本地库> go -C core test ./pkg/db/relational ./pkg/db ./cmd/migrate ./internal/migrations -count=1`。只执行相关包，未为本次数据库变更运行全量业务测试。

PostgreSQL 16（与当前部署大版本一致）：本地独立容器空库安装、重复安装通过；`pkg/db/relational`、`pkg/db`、`cmd/migrate` 和 `internal/migrations` 窄测均通过。首次组合测试中最后一个包发生进程超时，单包使用明确 30 秒上限重跑全部 4 个测试在 1.163 秒通过；未将首次超时隐去。另新增 Workspace 删除与 Comment 创建的反向锁序回归，确认写评论先等 Workspace，不先占 Task；删除完成后写入返回 ErrNoRows，最新关系包在 PG16 通过。缺失父评论时 `updated_at` 不发生旁路变更也加入了断言；最后一次重跑遇到本机 Docker 中断的拒连，恢复本次测试容器后通过。

后端调用方真实数据库回归同时通过：Task CRUD/按标识删除、评论删除后重排存活批次、管理员 Project 删除、ChatSession 草稿清理、RuntimeProfile 归档 Agent 级联和活动 Agent 拒绝、并发 finalizer 的 Workspace 清理，以及 ChatSession→Run 锁顺序和新建 ChatSession 等待 Workspace 删除锁。所有数据库验证只使用本机隔离容器，未修改远端数据。

## 107 条原关系逐项清单

以下名称已映射为最终 Task / AgentRun 语义；复合列按对应顺序列出。“当前无生产入口”表示只保留历史模式和清理，不是跳过一个仍可调用的写入校验。

| 子表及字段 | 父表及字段 | 原删除语义 | 写入保护 | 显式删除覆盖 |
| --- | --- | --- | --- | --- |
| `activity_log.task_id` | `task.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteTask`、`DeleteWorkspace` |
| `activity_log.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `agent.archived_by` | `user.id` | 拒绝删除父项 | 存在校验 + KEY SHARE | 无用户硬删除入口 |
| `agent.owner_id` | `user.id` | 拒绝删除父项 | 存在校验 + KEY SHARE | 无用户硬删除入口 |
| `agent.runtime_id` | `agent_runtime.id` | 拒绝删除父项 | 存在校验 + KEY SHARE | 业务删除检查；见下方限制说明 |
| `agent.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `agent_run.agent_id` | `agent.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteArchivedAgentsByRuntime`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteWorkspace` |
| `agent_run.parent_run_id` | `agent_run.id` | 置空引用 | 重试 SELECT 锁父 Run，并验证继承引用 | `DeleteAgentRuntime`、`DeleteAgentRuntimesByProfile`、`DeleteArchivedAgentsByRuntime`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteTask`、`DeleteWorkspace` |
| `agent_run.runtime_id` | `agent_runtime.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteAgentRuntime`、`DeleteAgentRuntimesByProfile`、`DeleteWorkspace` |
| `agent_run.autopilot_run_id` | `autopilot_run.id` | 置空引用 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `agent_run.chat_session_id` | `chat_session.id` | 置空引用 | 存在校验 + KEY SHARE | `DeleteArchivedAgentsByRuntime`、`DeleteChatSession`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteWorkspace` |
| `agent_run.trigger_comment_id` | `comment.id` | 置空引用 | 存在校验 + KEY SHARE | `DeleteComment`、`DeleteTask`、`DeleteWorkspace` |
| `agent_run.task_id` | `task.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteTask`、`DeleteWorkspace` |
| `agent_run_message.run_id` | `agent_run.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteAgentRuntime`、`DeleteAgentRuntimesByProfile`、`DeleteArchivedAgentsByRuntime`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteTask`、`DeleteWorkspace` |
| `agent_run_token.user_id` | `user.id` | 删除依赖 | 存在校验 + KEY SHARE | 无用户硬删除入口 |
| `agent_run_token.agent_id` | `agent.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteArchivedAgentsByRuntime`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteWorkspace` |
| `agent_run_token.run_id` | `agent_run.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteAgentRuntime`、`DeleteAgentRuntimesByProfile`、`DeleteArchivedAgentsByRuntime`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteTask`、`DeleteWorkspace` |
| `agent_run_token.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `agent_run_usage.run_id` | `agent_run.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteAgentRuntime`、`DeleteAgentRuntimesByProfile`、`DeleteArchivedAgentsByRuntime`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteTask`、`DeleteWorkspace` |
| `agent_runtime.owner_id` | `user.id` | 拒绝删除父项 | 存在校验 + KEY SHARE | 无用户硬删除入口 |
| `agent_runtime.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `agent_skill.agent_id` | `agent.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteArchivedAgentsByRuntime`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteWorkspace` |
| `agent_skill.skill_id` | `skill.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteSkill`、`DeleteWorkspace` |
| `attachment.chat_message_id` | `chat_message.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteArchivedAgentsByRuntime`、`DeleteChatSession`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteUserChatMessageByRun`、`DeleteWorkspace` |
| `attachment.chat_session_id` | `chat_session.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteArchivedAgentsByRuntime`、`DeleteChatSession`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteWorkspace` |
| `attachment.comment_id` | `comment.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteComment`、`DeleteTask`、`DeleteWorkspace` |
| `attachment.task_id` | `task.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteTask`、`DeleteWorkspace` |
| `attachment.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `autopilot.project_id` | `project.id` | 置空引用 | 存在校验 + KEY SHARE | `DeleteProject`、`DeleteWorkspace` |
| `autopilot.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `autopilot_run.run_id` | `agent_run.id` | 置空引用 | 存在校验 + KEY SHARE | `DeleteAgentRuntime`、`DeleteAgentRuntimesByProfile`、`DeleteArchivedAgentsByRuntime`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteTask`、`DeleteWorkspace` |
| `autopilot_run.autopilot_id` | `autopilot.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `autopilot_run.trigger_id` | `autopilot_trigger.id` | 置空引用 | 存在校验 + KEY SHARE | `DeleteAutopilotTrigger`、`DeleteWorkspace` |
| `autopilot_run.squad_id` | `squad.id` | 置空引用 | 存在校验 + KEY SHARE | `DeleteSquadsByArchivedAgentsOnRuntime`、`DeleteWorkspace` |
| `autopilot_run.task_id` | `task.id` | 置空引用 | 存在校验 + KEY SHARE | `DeleteTask`、`DeleteWorkspace` |
| `autopilot_trigger.autopilot_id` | `autopilot.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `chat_message.chat_session_id` | `chat_session.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteArchivedAgentsByRuntime`、`DeleteChatSession`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteWorkspace` |
| `chat_session.creator_id` | `user.id` | 删除依赖 | 存在校验 + KEY SHARE | 无用户硬删除入口 |
| `chat_session.agent_id` | `agent.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteArchivedAgentsByRuntime`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteWorkspace` |
| `chat_session.runtime_id` | `agent_runtime.id` | 置空引用 | 存在校验 + KEY SHARE | `DeleteAgentRuntime`、`DeleteAgentRuntimesByProfile`、`DeleteWorkspace` |
| `chat_session.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `comment.parent_id` | `comment.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteComment`、`DeleteTask`、`DeleteWorkspace` |
| `comment.task_id` | `task.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteTask`、`DeleteWorkspace` |
| `comment.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `comment_reaction.comment_id` | `comment.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteComment`、`DeleteTask`、`DeleteWorkspace` |
| `comment_reaction.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `daemon_connection.agent_id` | `agent.id` | 删除依赖 | 当前无生产新增/改绑入口；保留清理 | `DeleteArchivedAgentsByRuntime`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteWorkspace` |
| `daemon_token.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `feedback.user_id` | `user.id` | 删除依赖 | 存在校验 + KEY SHARE | 无用户硬删除入口 |
| `feedback.workspace_id` | `workspace.id` | 置空引用 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `github_installation.connected_by_id` | `user.id` | 置空引用 | 存在校验 + KEY SHARE | 无用户硬删除入口 |
| `github_installation.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `github_pull_request.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `github_pull_request_check_suite.pr_id` | `github_pull_request.id` | 删除依赖 | 当前无生产新增/改绑入口；保留清理 | `DeleteWorkspace` |
| `inbox_item.task_id` | `task.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteTask`、`DeleteWorkspace` |
| `inbox_item.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `lark_binding_token.installation_id` | `lark_installation.id` | 删除依赖 | 当前无生产新增/改绑入口；保留清理 | `DeleteArchivedAgentsByRuntime`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteWorkspace` |
| `lark_binding_token.workspace_id` | `workspace.id` | 删除依赖 | 当前无生产新增/改绑入口；保留清理 | `DeleteWorkspace` |
| `lark_chat_session_binding.chat_session_id` | `chat_session.id` | 删除依赖 | 当前无生产新增/改绑入口；保留清理 | `DeleteArchivedAgentsByRuntime`、`DeleteChatSession`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteWorkspace` |
| `lark_chat_session_binding.installation_id` | `lark_installation.id` | 删除依赖 | 当前无生产新增/改绑入口；保留清理 | `DeleteArchivedAgentsByRuntime`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteWorkspace` |
| `lark_inbound_audit.installation_id` | `lark_installation.id` | 置空引用 | 当前无生产新增/改绑入口；保留清理 | `DeleteArchivedAgentsByRuntime`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteWorkspace` |
| `lark_inbound_message_dedup.installation_id` | `lark_installation.id` | 删除依赖 | 当前无生产新增/改绑入口；保留清理 | `DeleteArchivedAgentsByRuntime`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteWorkspace` |
| `lark_installation.installer_user_id` | `user.id` | 拒绝删除父项 | 当前无生产新增/改绑入口；保留清理 | 无用户硬删除入口 |
| `lark_installation.agent_id` | `agent.id` | 删除依赖 | 当前无生产新增/改绑入口；保留清理 | `DeleteArchivedAgentsByRuntime`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteWorkspace` |
| `lark_installation.workspace_id` | `workspace.id` | 删除依赖 | 当前无生产新增/改绑入口；保留清理 | `DeleteWorkspace` |
| `lark_outbound_card_message.run_id` | `agent_run.id` | 置空引用 | 当前无生产新增/改绑入口；保留清理 | `DeleteAgentRuntime`、`DeleteAgentRuntimesByProfile`、`DeleteArchivedAgentsByRuntime`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteTask`、`DeleteWorkspace` |
| `lark_outbound_card_message.chat_session_id` | `chat_session.id` | 删除依赖 | 当前无生产新增/改绑入口；保留清理 | `DeleteArchivedAgentsByRuntime`、`DeleteChatSession`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteWorkspace` |
| `lark_user_binding.installation_id,workspace_id` | `lark_installation.id,workspace_id` | 删除依赖 | 当前无生产新增/改绑入口；保留清理 | `DeleteArchivedAgentsByRuntime`、`DeleteSystemAgentByID`、`DeleteSystemAgentsByRuntime`、`DeleteWorkspace` |
| `lark_user_binding.workspace_id,silieco_user_id` | `member.workspace_id,user_id` | 删除依赖 | 当前无生产新增/改绑入口；保留清理 | `DeleteMember`、`DeleteWorkspace` |
| `member.user_id` | `user.id` | 删除依赖 | 存在校验 + KEY SHARE | 无用户硬删除入口 |
| `member.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `notification_preference.user_id` | `user.id` | 删除依赖 | 存在校验 + KEY SHARE | 无用户硬删除入口 |
| `notification_preference.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `personal_access_token.user_id` | `user.id` | 删除依赖 | 存在校验 + KEY SHARE | 无用户硬删除入口 |
| `pinned_item.user_id` | `user.id` | 删除依赖 | 存在校验 + KEY SHARE | 无用户硬删除入口 |
| `pinned_item.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `project.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `project_resource.project_id` | `project.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteProject`、`DeleteWorkspace` |
| `project_resource.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `skill.created_by` | `user.id` | 拒绝删除父项 | 存在校验 + KEY SHARE | 无用户硬删除入口 |
| `skill.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `skill_file.skill_id` | `skill.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteSkill`、`DeleteWorkspace` |
| `squad.leader_id` | `agent.id` | 拒绝删除父项 | 存在校验 + KEY SHARE | 业务删除检查；见下方限制说明 |
| `squad.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `squad_member.squad_id` | `squad.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteSquadsByArchivedAgentsOnRuntime`、`DeleteWorkspace` |
| `task.project_id` | `project.id` | 置空引用 | 存在校验 + KEY SHARE | `DeleteProject`、`DeleteWorkspace` |
| `task.parent_task_id` | `task.id` | 置空引用 | 存在校验 + KEY SHARE | `DeleteTask`、`DeleteWorkspace` |
| `task.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `task_dependency.depends_on_task_id` | `task.id` | 删除依赖 | 当前无生产新增/改绑入口；保留清理 | `DeleteTask`、`DeleteWorkspace` |
| `task_dependency.task_id` | `task.id` | 删除依赖 | 当前无生产新增/改绑入口；保留清理 | `DeleteTask`、`DeleteWorkspace` |
| `task_label.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `task_pull_request.pull_request_id` | `github_pull_request.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `task_pull_request.task_id` | `task.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteTask`、`DeleteWorkspace` |
| `task_reaction.task_id` | `task.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteTask`、`DeleteWorkspace` |
| `task_reaction.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `task_subscriber.task_id` | `task.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteTask`、`DeleteWorkspace` |
| `task_to_label.task_id` | `task.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteTask`、`DeleteWorkspace` |
| `task_to_label.label_id` | `task_label.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteLabel`、`DeleteWorkspace` |
| `webhook_delivery.autopilot_id` | `autopilot.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `webhook_delivery.autopilot_run_id` | `autopilot_run.id` | 置空引用 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `webhook_delivery.trigger_id` | `autopilot_trigger.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteAutopilotTrigger`、`DeleteWorkspace` |
| `webhook_delivery.replayed_from_delivery_id` | `webhook_delivery.id` | 置空引用 | 存在校验 + KEY SHARE | `DeleteAutopilotTrigger`、`DeleteWorkspace` |
| `webhook_delivery.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
| `workflow_instance.archived_by` | `user.id` | 拒绝删除父项 | 存在校验 + KEY SHARE | 无用户硬删除入口 |
| `workspace_invitation.invitee_user_id` | `user.id` | 拒绝删除父项 | 存在校验 + KEY SHARE | 无用户硬删除入口 |
| `workspace_invitation.inviter_id` | `user.id` | 拒绝删除父项 | 存在校验 + KEY SHARE | 无用户硬删除入口 |
| `workspace_invitation.workspace_id` | `workspace.id` | 删除依赖 | 存在校验 + KEY SHARE | `DeleteWorkspace` |
