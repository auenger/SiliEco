# Silieco 产品全景

> 更新日期：2026-11-06
> 适用范围：当前仓库中的 Core、Daemon、Web、Desktop、Mobile 与共享 packages。

这份文档描述已经落到代码、数据库迁移或 App 界面中的产品模型。它用于产品、设计、
开发和测试对齐，不把规划中的能力写成已经上线的事实。

## 1. 产品定位

Silieco 是 Human + Agent Work OS。它不是单纯的聊天工具，也不是只面向 Agent 的任务
队列，而是把人、Agent、流程、Task、讨论、决策和执行结果放进同一个 Space。

核心目标是：

1. 用户可以共享自己的 Agent，让 Agent 以明确身份进入工作组织；
2. Task 可以交给人，也可以交给 Agent 或蜂群；
3. 简单工作不强制配置流程，复杂工作可以通过 SOP 与 Stage 管控；
4. 人与人、人和 Agent 在同一份上下文里持续协作；
5. Agent 的本地执行仍由用户掌控，Core 负责协调而不代替本地 Runtime。

## 2. 核心层级

```text
Space
├── Member / Agent / Swarm / Skill / Runtime / Chat
├── Direct Task
└── Project
    ├── Project Task（可不进入 SOP）
    ├── Space SOP / Workflow Definition
    └── Project SOP Variant or Workflow Run
        └── Stage
                └── Task
```

这些关系的产品约束如下：

- 一个用户可以进入多个 Space；一个 Space 可以包含多个 Project。
- SOP 是 Space 级可复用资产；一个 Project 可以直接运行多个 Space SOP。
- Project 可以继承 Space SOP 的已发布版本，并形成独立调整的 Project 变体。
- 一个已发布 SOP 可以启动多个 Workflow Run，每次运行有独立名称。
- 一个 Workflow Run 按发布版本包含多个有序 Stage。
- 一个 Stage 可以关联多个 Task。
- Task 可以完全不分类，也可以只属于 Project，还可以同时属于 Project、Workflow Run
  与 Stage。
- Task 进入 Stage 后仍保留自己的生命周期状态。Stage 表示流程位置，Task status
  表示工作状态，两者不能互相替代。

## 3. 概念词典

| 产品概念 | 定义 | 当前主要实现 |
| --- | --- | --- |
| Space | 团队协作和权限隔离的顶层容器 | `workspace` |
| Member | 人类用户在某个 Space 中的成员身份 | `member` |
| Project | Space 下组织目标、Task、SOP 和协作者的容器 | `project` |
| Task | 最小业务工作单元，可交给人、Agent 或蜂群 | `task` |
| AgentRun | 一次智能体执行，包含运行状态、消息、结果、用量和重试关系 | `agent_run` |
| Agent | 有身份、运行时、模型、说明、Skills 和权限的 AI 工作者 | `agent` |
| Swarm 蜂群 | 多个人与 Agent 的稳定路由和协作单元 | `squad`（内部兼容表名） |
| Runtime | 实际执行 Agent CLI 的计算环境 | `agent_runtime` |
| Daemon | 连接本地 Runtime 与 Core 的常驻进程 | Go daemon 进程 |
| Skill | 可复用、可挂载给 Agent 的工作知识和指令 | `skill` / `agent_skill` |
| SOP | 可复用的 Workflow 定义 | `workflow` |
| Workflow Version | SOP 的不可变发布版本 | `workflow_version` |
| Workflow Run | 某个发布版本的一次具名执行 | `workflow_instance` |
| Stage | Workflow Run 中有顺序和门禁的流程阶段 | `workflow_stage` |
| Gate | 从当前 Stage 流转前的决策规则 | `workflow_gate_decision` |
| Chat | 人与 Agent 的持久协作对话 | `chat_session` / `chat_message` |
| Autopilot | 定时、Webhook 或手动触发的自动化 | `autopilot*` |

### Task 与 AgentRun

Task 是唯一业务任务概念，代码、数据库、API 和 CLI 分别使用 `Task`、`task`、
`/api/tasks` 与 `silieco task`。AgentRun 是一次执行，执行接口使用 `/api/runs`。

一个 Task 可以关联零到多个 AgentRun；聊天和自动化发起的 AgentRun 可以没有 Task。
业务任务用 `task_id` 标识，执行记录用 `run_id` 标识。任务的 `done` 与执行的
`completed` 表示不同层次的状态，不因名称统一而合并生命周期。

当前开发环境采用一次性协议切换和测试数据重置，旧 Issue 接口与别名不再提供。
Core、Web、Desktop、Mobile、CLI 与 Daemon 应作为同一协议代际发布。

### 蜂群与内部 `squad` 命名

App 对用户统一展示“蜂群”。部分内部类型仍使用 `squad`，其含义是任务路由单元，
不是另一种用户可见对象。

## 4. Space 与 Project

### Space

Space 是一切资源的权限边界。每个 Space 独立拥有：

- 成员和邀请；
- Project 与 Task；
- Agent、蜂群、Skill 和 Runtime；
- 单聊、群聊、通知和搜索；
- 仓库、集成与 Space 设置。

Desktop 登录后的左侧导航直接显示 Space 列表。Space 可以手风琴式展开，每个 Space
下展示自己的菜单，不需要先通过额外的“打开”菜单切换。

### Project

Project 是 Space 内面向一个目标或交付物的组织层。一个 Space 可以创建多个
Project；每个 Project 本身也可折叠，并提供 Project Task、SOP 以及与该 Project
相关的自动化、Agent、成员和上下文入口。

Project Task 有三种有效形态：

1. 只属于 Space；
2. 属于 Project，但不进入 SOP；
3. 属于 Project，并关联某个 Workflow Run 的 Stage。

## 5. Task

Task 是 Silieco 的核心工作对象。

### 生命周期

当前通用状态为：

- `backlog`：待规划；
- `todo`：待处理；
- `in_progress`：进行中；
- `in_review`：评审中；
- `done`：已完成；
- `blocked`：已阻塞；
- `cancelled`：已取消。

Task 可以分配给 Member、Agent 或蜂群。Agent 接手 Task 后，Core 创建 AgentRun 执行记录，
Daemon 认领并启动对应 CLI。执行中的事件、回复、用量、错误与完成状态持续回传 Core，
App 通过 WebSocket 和查询缓存更新视图。

### Task 信息

当前 Task 支持：

- 标题、富文本描述、编号、状态和优先级；
- 创建者、负责人、Project、父 Task 与子 Task；
- 标签、开始日期、截止日期、依赖与验收标准；
- 评论、嵌套回复、表情、订阅者和附件；
- 活动时间线、Agent 执行记录与 token/cost 用量；
- Workflow Run 和 Stage 归属。

### Task 视图

- 普通生命周期看板按 Task status 分列；
- Project Task 页面展示当前 Project 的全部 Task；
- SOP Stage 看板按 Workflow Run 的 Stage 分列；
- Stage 看板保留 Task 标题、编号、优先级、负责人、Project、SOP/Stage 和生命周期；
- Stage 看板可以按生命周期状态筛选，并默认让待处理工作优先出现；
- Stage 列采用独立纵向滚动，单列 Task 增多时不会撑高整个页面；
- “我的 Task”用于查看分配给我、由我创建或由我的 Agent 负责的工作。

## 6. SOP、Workflow Run 与 Stage

### 为什么 SOP 是 Task 之上的可选层

并非所有 Task 都需要流程。Silieco 允许用户在创建 Space 或 Project 后直接继续原有
Task 生命周期；只有交付过程需要复用、审计或门禁时，才创建 SOP。

### SOP 定义

SOP 默认属于 Space，是多个 Project 可以复用的流程资产。Project 可以直接启动 Space
SOP，也可以从已发布版本继承并创建 Project 专属变体。创建与详情页面只负责流程设计，
不把 Task 管理重复塞进 SOP 编辑器。用户可以从模板开始，也可以创建空白 SOP。

内置模板包括：

- 投标模板；
- 软件开发模板；
- Bug 修复模板；
- 通用文档协作模板；
- 空白 SOP。

每个 Stage 可以配置：

- 名称与顺序；
- 默认输入与输出；
- 所需 Skills；
- Stage 内可用的 Task 状态集合；
- 出阶段门禁；
- 决策者类型：人、当前 Agent 或指定 Agent；
- 是否必须人工最终确认；
- 失败时的回退 Stage。

### 发布和运行

SOP 需要发布后才能启动。发布形成不可变 Workflow Version。启动时必须给这次运行
指定独立名称，例如“移动端 2.0 发布”，因此同一 SOP 同时存在多个运行时不会混淆。

Workflow Run 状态包括 draft、active、waiting、completed 和 cancelled。Task 创建器会
显示 Project、SOP、运行和 Stage 的级联选择；当已发布 SOP 尚无运行时，可以直接
创建默认具名运行。

active 或 waiting 状态的运行允许管理员和 Project Lead 在不修改已发布 SOP Version
的前提下微调本次运行计划，包括运行名称、说明、Stage 名称、产出物、Skills、Gate
和回退目标。已完成 Stage 被锁定；当前 Stage 不可重排；已有 Task 或 Gate 决策的未来
Stage 不可随意删除或重排；没有任务和决策的未来 Stage 可以新增、删除和排序。保存使用
乐观修订号避免并发覆盖，并记录变更说明及前后计划快照。

### 看板切换

Project Task 页面整合普通 Task 与 SOP：

- “生命周期”模式按 Task status 查看；
- “SOP Stage”模式选择一次 Workflow Run 后按 Stage 查看；
- 拖动 Task 只改变 Stage 归属，不会隐式伪造 Task 的业务完成状态；
- Gate 决策用于后续 Stage 流转管控，并保留决策人、结果与备注。
- 当前 Stage 的审核区汇总 Task 完成情况，并提供批准进入下一 Stage、完成运行或回退操作。

### Project 工作目录与 Git 映射

Project 本地工作目录是一等运行资源，不依赖 Git。每台 Runtime 可以为同一个 Project
配置自己的私有目录，Agent 在该 Runtime 执行 Project Task 时直接使用这个目录；路径和
文件不会同步给 Space 中的其他成员，任务上下文也只包含当前 Runtime 自己的路径。

远程 Git 仓库是可选的 Project 资源。关联仓库后，每台 Runtime 可以在该仓库下面配置
一个已准备好的本地工作副本。Desktop 在保存仓库级映射前验证所选路径是 Git 根目录，
并确认 `origin` 与远程仓库一致。仓库专属映射优先于 Project 默认工作目录；如果当前
Runtime 没有仓库级映射，则回退到独立的 Project 工作目录，再没有时才使用临时 checkout。

## 7. Agent、Runtime 与 Daemon

### Agent

Agent 是组织中的 AI 工作者，而不是单纯的模型别名。它可以配置：

- 名称、头像、描述、说明和可见范围；
- Runtime、Provider、模型、思考等级与服务等级；
- 自定义环境变量和 CLI 参数；
- MCP Server、Connected Apps 与 Skills；
- 最大并发 AgentRun 数量。

默认助手名称为 **Sili Agent**，品牌头像使用 Silieco Logo。

### Runtime

Runtime 表示可以执行 Agent 的机器或环境。本地 Runtime 由 Daemon 注册，并上报机器
信息、可用 Provider、心跳和工作状态。

### Daemon

Daemon 负责：

1. 发现本机可用的 Agent CLI；
2. 通过稳定协议连接 Core；
3. 认领排队 AgentRun；
4. 准备工作目录、仓库、Skill、MCP 和运行环境；
5. 启动 Claude Code、Codex 等 Agent CLI；
6. 流式上报消息、工具调用、状态、用量和错误；
7. 处理取消、超时、恢复、重试与本地垃圾回收。

当前可运行实现位于 Go module 内。`daemon/` 是独立产品边界和未来 Rust 工程位置，
不能误认为当前已有第二套可运行 Daemon。

## 8. 聊天与共享上下文

### 单聊

用户可以与单个 Agent 建立持久化对话。会话支持流式回复、附件、项目上下文、停止
运行、失败原因、未读状态、置顶、重命名和历史恢复。

### Space 群聊

一个群聊只能邀请当前 Space 内的 Member 与 Agent。群创建者默认加入，已加入成员
不会被重复插入。

群聊触发规则是：

- 人与人正常发送消息时，不触发 Agent；
- 只有消息中明确 `@` 某个 Agent，Core 才为该 Agent 创建 AgentRun；
- 一条消息可以提及多个 Agent，并分别创建 AgentRun；
- Agent 执行时读取带说话人标识的群聊历史作为上下文；
- Agent 回复仍写回同一个群聊，所有参与者共享结果；
- 创建弹窗将成员搜索和结果放在同一个 Panel 中，并支持后续继续邀请成员。

群聊成员由 `chat_participant` 数据模型保存，初始化与索引定义随当前数据库基线维护。

## 9. 蜂群、Skill 与 Autopilot

### 蜂群

蜂群把多个 Agent 和人组合成稳定的任务入口。Leader Agent 根据蜂群说明、成员能力
与当前状态决定路由。App 对外称“蜂群”，内部兼容类型仍可能出现 `squad`。

### Skill

Skill 是可复用的工作说明和文件集合。Skill 可以关联 Agent，Daemon 在执行前把启用
的 Skill 准备到工作目录中。它用于沉淀组织做事方式，但不替代 Workflow：Workflow
定义“工作经过哪些 Stage”，Skill 定义“Agent 如何完成某类动作”。

### Autopilot

Autopilot 可以由 Cron、Webhook 或人工触发，按规则创建 Task 或直接启动 Agent 工作。
它适合日报、巡检、同步、周期性分析等重复工作。

## 10. App 体验

### Onboarding

登录后的欢迎页围绕 Space、Workflow、Stage、Task、SOP 和人机协作展开。Desktop
首次启动的三个步骤为：

1. 识别 Runtime；
2. 创建 Space；
3. 用一句话描述这个 Space 要完成什么。

### Desktop 导航

- 左上角只展示 Silieco Logo 与 “Human + Agent Work OS”；
- 上方提供创建 Space；
- Space 与 Project 都是可折叠层级；
- 左下角显示当前用户姓名、邮箱、设置入口与登出；
- SOP 配置属于 Project；Task 管理与 SOP Stage 看板在 Project Task 中融合。

### 平台边界

- Web：完整浏览器体验与公开网站；
- Desktop：Electron 宿主、内置 CLI、Daemon 管理、本地 Runtime 识别；
- Mobile：Expo 客户端，复用 API 与部分业务模型；
- Docs：独立文档站。

## 11. Core、Daemon、App 部署关系

典型测试或自部署拓扑为：

```text
Server
└── Silieco Core + PostgreSQL (+ Redis / object storage as configured)

Client machine
├── Silieco App
└── Silieco Daemon
    └── Claude Code / Codex / other Agent CLI
```

Core 负责信息中转、权限、状态与调度；App 提供协作界面；Daemon 在用户掌控的机器上
执行实际 Agent 工作。客户端只启动 App 而没有 Daemon 时仍可浏览和管理数据，但不能
在本机运行 AgentRun。

## 12. 技术实现边界

| 模块 | 当前技术 |
| --- | --- |
| Core | Go、Chi、sqlc、pgx、gorilla/websocket |
| CLI / Daemon | Go、Cobra、本地进程监督 |
| Web | Next.js、React、TypeScript |
| Desktop | Electron、electron-vite、React |
| Mobile | Expo、React Native |
| Database | PostgreSQL 17、pgvector |
| Realtime | WebSocket |
| Monorepo | pnpm、Turborepo |

Go Core 与 Daemon 目前是可运行基线。Rust 迁移建议先固化协议和 fixtures，再优先替换
Daemon，最后依据收益决定是否迁移 Core。详见
[`docs/architecture/RUST-MIGRATION.md`](architecture/RUST-MIGRATION.md)。

## 13. 关键代码位置

| 能力 | 位置 |
| --- | --- |
| Core 服务入口 | `core/cmd/server` |
| 数据库迁移 | `core/migrations` |
| CLI / Daemon 入口 | `core/cmd/silieco` |
| Daemon 实现 | `core/internal/daemon` |
| Core ↔ Daemon 协议 | `core/pkg/protocol` |
| Workflow API 与服务 | `core/internal/handler`、`core/internal/service` |
| Workflow App 视图 | `packages/views/workflows` |
| 群聊视图 | `packages/views/chat` |
| 共享 TypeScript API | `packages/core` |
| Desktop | `apps/desktop` |
| Web | `apps/web` |

## 14. 当前限制与后续边界

- 当前仍是开发预览版，需要在生产使用前完成安全、迁移、备份和升级验证。
- Workflow Gate 的数据模型已经存在，后续仍需继续完善更严格的自动流转和权限策略。
- 群聊已完成成员模型、`@Agent` 触发与共享历史，后续可以继续优化上下文裁剪、摘要、
  权限和多 Agent 协作策略。
- Task / AgentRun 使用同一协议代际；旧测试数据和旧客户端不在本次切换后的支持范围内。蜂群的内部 `squad` 名称不属于本次重构。
- Rust 迁移尚未开始形成生产替代实现；现阶段不要同时重写 Core 与 Daemon。
- 仓库尚未声明许可证。
