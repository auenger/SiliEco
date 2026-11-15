# Silieco

**Human + Agent Work OS**

共享你的 Agent，让你的 Agent 代表你接入工作组织。

![CI](https://github.com/auenger/SiliEco/actions/workflows/ci.yml/badge.svg)

[官网](https://silieco.ai) · [English](README.md) · [自部署](SELF_HOSTING.md) · [参与贡献](CONTRIBUTING.md)

> Silieco 当前处于开发预览阶段，请勿直接承载不可恢复的生产数据。仓库尚未声明\
> 开源许可证，在正式添加许可证前默认保留所有权利。

## Silieco 是什么

Silieco 是一个面向人与 Agent 共同工作的协作操作系统。团队可以从一句目标描述\
开始，把工作拆成 Task，将 Task 分配给人、Agent 或蜂群，并让执行过程、讨论、\
决策、评审和结果持续沉淀在同一份协作记录中。

产品同时兼容两种工作方式：

* 在 Space 中直接创建 Task，沿用简单的直线生命周期；

* 在 Project 中发布 SOP，启动一次有独立名称的 Workflow Run，再按 Stage 管理 Task。

当前层级模型为：

```text
Space
├── 直线生命周期 Task
├── 成员、Agent、蜂群、Skill、聊天与设置
└── Project
    ├── Project Task
    └── SOP / Workflow 定义
        └── Workflow Run（一次具体运行）
            └── Stage
                └── Task
```

SOP 是可复用的流程定义，Workflow Run 是某个已发布 SOP 的一次具名执行。Task\
始终保留自己的生命周期状态，同时可以归属于一个 Stage，所以看板可以在“Task\
状态”和“Workflow Stage”之间切换，而不会丢失任何一个维度。

## 当前已经实现

### Space 与 Project

* 多 Space 隔离，每个 Space 独立管理成员、权限、仓库、Agent、Skill、Task、 聊天与设置。

* Desktop 左侧采用 Space 与 Project 的手风琴式层级导航。

* 一个 Space 可以创建多个 Project；每个 Project 拥有自己的 Task、SOP、自动化、 Agent、人员与上下文。

* Project 可以只使用普通 Task；SOP 是可选的增强层，不会阻断原有直线生命周期。

### 以 Task 为核心的工作模型

* Task 可以分配给人、Agent 或蜂群。

* 生命周期包含：待规划、待处理、进行中、评审中、已完成、已阻塞和已取消。

* 提供列表、我的 Task、Project Task、状态看板和 Workflow Stage 看板。

* 支持优先级、负责人、创建者、Project、标签、日期、父子 Task、依赖、验收条件、 附件、评论、表情回应、订阅、活动时间线与运行用量。

* Agent 从排队、认领、执行到评审、完成或失败的状态变化实时回传。

> **Task** 是业务任务，**AgentRun** 是一次智能体执行。代码、数据库、API 与 CLI
> 直接使用这套名称；一个 Task 可关联多次执行，聊天或自动化执行也可不关联 Task。

### Space 与 Project 文件资源

* 上传到 Task 的文件会作为 Space 内的持久资源管理；仅在对话中上传的临时文件不会进入资源库。

* Space 资源页按文件来源 Project 分组，并展示每个文件具体被哪些 Project 或 Task 引用。

* 每个 Project 都有独立文件视图，统一展示其 Task 文件以及 Project 直接引用的共享文件。

* Task 可以直接引用 Space 中已有文件，不产生重复副本；评论输入框提供可搜索、多选的快速引用入口，并将所选文件作为文件卡片插入草稿。

* 图片、PDF、音视频、Markdown、HTML 和文本资源支持独立预览页，其他文件可以直接下载。

* 文件引用的创建和移除遵循 Space 成员及 Project 管理权限；仍被引用的源文件不会被误删除。

### Project 运行目录与 Git 仓库

* Project 可以直接为每台 Runtime 配置私有的本地工作目录，不要求 Project 必须关联 Git 仓库。

* 关联远程 Git 仓库后，可以在仓库下面为 Ryan 的 Mac、Alice 的 Mac 等不同 Runtime 分别配置本地工作副本；仓库专属映射优先于 Project 默认工作目录。

* Desktop 会验证仓库级本地目录是 Git 根目录，并确认 `origin` 与远程仓库一致；独立 Project 工作目录只要求路径可读写。

* 本地路径和文件不会同步给 Space 中的其他成员。其他成员只看到机器、所有者、在线状态以及是否已配置，Agent 也只接收当前 Runtime 自己的路径。

### SOP、Workflow 与 Stage

* Project 级 SOP，可处于草稿、已发布或已归档状态。

* 已发布版本不可变；每次 Workflow Run 可以单独命名，便于同时识别多个运行实例。

* active 或 waiting 状态的 Workflow Run 可以安全微调运行名称、说明、Stage 产出物、Skills 与 Gate；已完成 Stage 保持锁定，含 Task 或 Gate 决策的未来 Stage 不能随意删除或重排。

* 没有 Task 和 Gate 决策的未来 Stage 可以新增、删除或调整顺序，保存时使用修订号防止多人覆盖，并记录变更说明与前后计划快照。

* Stage 可配置顺序、输入、输出、所需 Skills、允许的 Task 状态、回退目标，以及 人工、Agent 或人机联合门禁。

* 内置投标、软件开发、Bug 修复、通用文档协作和空白 SOP 模板。

* 创建 Task 时可以选择 Project → SOP → Workflow Run → Stage；已发布 SOP 尚无 运行时可以自动创建一次运行。

* 现有 Project Task 可以绑定到 Workflow，并在不同 Stage 之间移动。

* Stage 看板对齐普通 Task 看板的信息密度，显示 Task 当前生命周期，并支持状态 筛选和待办优先排序。

* Stage 看板按列独立纵向滚动，Task 数量较多时不会撑高整个页面；当前 Stage 提供人工审核、进入下一 Stage、完成运行或回退操作。

### 人机协作与群聊

* 支持与单个 Agent 的持久化聊天和可恢复执行上下文。

* 每个 Space 可以创建群聊，并邀请当前 Space 的成员与 Agent。

* 人类消息默认只是正常群聊；只有明确 `@ Agent` 时才会触发 Agent 执行。

* 被提及的 Agent 会读取带发言人标识的群聊历史作为共享上下文；同一条消息可以 同时触发多个 Agent。

* Agent 支持档案、可见范围、Runtime、Provider、模型、思考等级、环境变量、 启动参数、MCP、并发数和 Skills 配置。

* 蜂群为一组人和 Agent 提供稳定的任务路由入口。

* Autopilot 支持定时、Webhook 或手动触发重复性 Agent 工作。

### Runtime 与集成

* 本地 Daemon 负责 CLI 发现、Runtime 注册、WebSocket 心跳、任务认领、流式事件、 取消、恢复和本地垃圾回收。

* 支持 Claude Code、Codex、CodeBuddy、GitHub Copilot CLI、OpenCode、OpenClaw、 Hermes、Pi、Cursor Agent、Kimi、Kiro、Qwen、Qoder、Trae 等运行时适配。

* 具备 GitHub/VCS、Slack、飞书、对象存储、邮件、指标与自部署集成基础。

* Web、Electron Desktop、Expo Mobile 和 Docs App 复用 TypeScript SDK、设计系统和 业务视图。

## Core、Daemon 与 App

```text
┌──────────────────────────────────────────────────────────┐
│ App                                                      │
│ Next.js Web · Electron Desktop · Expo Mobile · 共享视图 │
└────────────────────────────┬─────────────────────────────┘
                             │ REST + WebSocket
┌────────────────────────────▼─────────────────────────────┐
│ Core Service                                             │
│ 身份 · Space · Project · Workflow · Task · Chat         │
│ 调度 · 实时协调 · PostgreSQL                             │
└────────────────────────────┬─────────────────────────────┘
                             │ Daemon 协议
┌────────────────────────────▼─────────────────────────────┐
│ Daemon / CLI                                             │
│ 本地运行时识别 · Agent 子进程执行 · 恢复与清理          │
└──────────────────────────────────────────────────────────┘
```

| 边界           | 当前实现                                   | 后续方向                             |
| ------------ | -------------------------------------- | -------------------------------- |
| Core Service | Go、Chi、sqlc、pgx、WebSocket              | 评估渐进迁移到 Rust/Axum                |
| Daemon / CLI | Go、Cobra、本地子进程管理                       | 优先考虑迁移到 Rust                     |
| App          | Next.js、React、Electron、Expo            | 保留 TypeScript 与 `@silieco/*` 共享包 |
| 数据           | PostgreSQL 17 + pgvector、Redis、S3 兼容存储 | 保持 schema 与协议兼容                  |

当前 Go 实现是可执行的行为基线。替换 Core 或 Daemon 前，请先阅读\
Rust 迁移分析。

## 仓库结构

```text
silieco/
├── core/                 # Go Core、CLI、当前 Daemon、数据库迁移
├── daemon/               # 独立 Daemon 边界与未来 Rust 工程位置
├── apps/
│   ├── web/              # Next.js Web 与官网
│   ├── desktop/          # Electron Desktop
│   ├── mobile/           # Expo Mobile
│   └── docs/             # 文档站
├── packages/
│   ├── core/             # 共享 TypeScript SDK 与状态
│   ├── ui/               # 设计系统
│   └── views/            # 共享业务视图
├── deploy/               # Docker 与 Helm 部署资源
├── docs/                 # 产品和架构文档
└── e2e/                  # Playwright 端到端测试
```

## 本地开发

环境要求：

* Node.js 22+

* pnpm 10.28.2+

* Go 1.26+

* Docker 与 Compose 插件

* 可选：至少一个已经登录的 Agent CLI

初始化并启动 Core + Web：

```bash
cp .env.example .env
pnpm install
make dev
```

默认地址：

* Web：<http://localhost:3000>

* Core API：<http://localhost:8080>

* 健康检查：<http://localhost:8080/health>

另开终端启动 Desktop：

```bash
pnpm dev:desktop
```

常用命令：

```bash
make setup             # 安装依赖、启动 PostgreSQL、执行迁移
make start             # 执行迁移并启动 Core + Web
make server            # 只启动 Core
make daemon            # 重启已认证的本地 Daemon
make silieco ARGS=...  # 从 Go 源码运行 CLI
pnpm dev:desktop       # 启动 Electron Desktop
pnpm dev:mobile        # 启动 Expo Mobile
pnpm typecheck         # TypeScript 类型检查
pnpm test              # TypeScript 测试
make test              # Go 测试
make check             # 完整本地校验
```

进一步说明见产品全景、[自部署](SELF_HOSTING.md)、\
[CLI 与 Daemon](CLI_AND_DAEMON.md)和[贡献指南](CONTRIBUTING.md)。

## 当前命名约定

* npm scope：`@silieco/*`

* Go module：`github.com/silieco-ai/silieco/core`

* CLI：`silieco`

* 环境变量：`SILIECO_*`

* Deep Link：`silieco://`

* 本地目录：`~/.silieco`

* 协议请求头：`X-Silieco-*`

Silieco 不提供迁移前产品品牌的兼容层。

## License

许可证尚未确定。在正式添加许可证文件前，本仓库默认保留所有权利。

⠀
