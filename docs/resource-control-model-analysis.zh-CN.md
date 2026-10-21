# Silieco 资源控制模型：现状与架构分析

> 状态：讨论基线
>
> 记录日期：2026-08-30
>
> 本文记录当前 Silieco 的资源概念、权限缺口、Task 依赖和调度模型分析。本轮仅完成代码现状与架构分析，不包含实现修改或测试结果。

## 一、当前实现现状

当前 Silieco 并不是“资源只有文件”，而是“资源概念已经散落在多个模块里，却没有形成统一的资源控制模型”。现在缺的核心不是再增加几个 `resource_type`，而是把资源身份、权限、Task 依赖、可用性和占用调度拆成清晰的领域对象。

目前至少存在四套事实上的“资源”：

| 现有概念 | 当前定位 | 主要问题 |
| --- | --- | --- |
| File Resource | Resources 页面展示的附件、引用文件 | UI 上几乎等同于“资源” |
| Project Resource | `github_repo`、`local_directory` | 把资源身份与 Project 关联混在一张表 |
| Runtime / Server | Agent 执行所在机器及并发容量 | 属于执行资源，但没有进入 Resource 模型 |
| Skill / MCP / Connected App | Task 运行需要的能力或服务 | 有执行清单，但权限与可用性各自实现 |

当前 Resources 页面确实只读取 FileResource，因此从产品视角看，“资源就是文件”：

- `packages/views/resources/resources-page.tsx:9`
- `core/internal/handler/file_resource.go:132`

但 Project Resource 从设计之初已经允许扩展外部资源，目前实际支持：

- `github_repo`
- `local_directory`

相关定义见：

- `core/migrations/065_project_resources.up.sql:1`
- `core/internal/handler/project_resource.go:67`

此外，MCP Server、Skill 和 Connected App 已经会写入 Task 的 `execution_manifest`，说明系统事实上已经开始建立“本次运行获得了哪些能力”的执行凭据：

- `core/migrations/262_workspace_mcp_catalog.up.sql:1`
- `core/internal/handler/daemon.go:1677`

所以目前最大问题是：这些都是彼此独立的局部模型。

## 二、当前权限缺口

现在 Workspace 基本是权限边界，而不是资源 ACL。

对于普通文件和 Project Resource：

- 只要是 Workspace 成员，通常就可以读取。
- 文件资源列表会返回 Workspace 内所有持久文件。
- Project Resource 的读取同样主要依赖 Workspace 成员身份。
- Project Resource 的修改权限是 Workspace `owner`/`admin` 或 Project lead。
- 本地目录还额外允许对应 Runtime 的 owner 管理。
- 执行中的 Task Token 不能修改 Project Resource。

现有管理权限见：

- `core/internal/handler/project_resource.go:254`

因此当前缺少的是：

- 某个资源是否可以被发现。
- 哪些用户可以读取。
- 哪些用户可以使用或委托 Agent 使用。
- 哪些 Agent 可以使用。
- 谁可以修改、分配、分享资源。
- 跨 Space/Workspace 是否允许共享。
- Task 获得资源后，权限何时失效。

Agent 本身已经有 `private/public_to` + `invocation_targets` 的权限模式，这套思路可以参考，但不能直接等同于资源权限。调用 Agent 的权限和使用服务器、凭据、文件的权限是不同的。

## 三、资源应该怎样重新定义

建议把 Resource 定义为：

> Task、Agent 或用户在工作过程中需要读取、调用、占用或消耗的受管对象。

可以分成三类：

1. **信息资源**

   文件、URL、网页、文档、Git 仓库、知识库、数据集。

2. **执行资源**

   Runtime、服务器、设备、GPU、本地目录、端口、测试环境、部署环境。

3. **能力资源**

   Skill、MCP Server、Connected App、API 服务、数据库连接、CI/CD 服务。

但不建议把所有行为都塞进一张 Resource 表的 JSONB。比较稳妥的是建立统一控制面，同时保留类型适配器。

核心对象应拆成：

- **Resource**：资源是谁、属于哪个 Workspace、是什么类型。
- **ResourceBinding**：资源挂在哪个 Project、Issue、Agent 或 Workflow 上。
- **ResourceGrant**：谁能发现、读取、使用、管理、分享。
- **TaskResourceRequirement**：Task 需要哪些资源，以什么方式使用。
- **ResourceLease**：某个 Task 当前占用了多少资源、何时过期。
- **ResourceHealth**：资源当前在线、离线、降级、维护还是未知。
- **ExecutionManifest**：最终实际授予本次 Task 的资源和能力快照。

这也意味着，当前 `project_resource` 实际混合了前两个概念：既保存资源本身，又表示它属于某个 Project。未来应逐步拆开。

## 四、权限模型建议

建议资源权限至少包含以下动作：

- `discover`：能否在资源列表中看到。
- `read`：能否查看内容或元数据。
- `use`：能否在 Task 中使用。
- `allocate`：能否占用排他性资源。
- `manage`：能否修改资源配置。
- `share`：能否给其他人或 Agent 授权。

授权主体可以是：

- Workspace
- Member
- Team
- Agent
- Automation principal

Task 不应该永久成为 ACL 主体。更安全的方式是：

1. 创建或触发 Task 的用户有权使用该资源。
2. 被分配的 Agent 被允许作为执行者使用。
3. Task 明确声明需要该资源。
4. 系统生成仅限本次 Task、带过期时间的能力凭据。
5. Task 结束、取消或租约过期后立即失效。

这是“用户授权 × Agent 执行资格 × Task 明确依赖”的交集，不应使用三者权限的并集，否则容易产生 Agent 代替低权限用户访问高权限资源的 confused deputy 问题。

对于 Space，建议第一阶段坚持：

- Resource 必须有唯一的 `workspace_id`。
- 默认禁止跨 Workspace 使用。
- Project 只是组织和继承范围，不是安全边界。
- 真正需要跨 Workspace 时，再增加显式分享或导入机制，不要直接绕过租户边界。

## 五、Task 与资源调度

“资源 A 被另一个 Task 占用”其实已经存在一个特例。

`local_directory` 当前会：

- 验证目录和 Git 仓库。
- 尝试获取进程内路径锁。
- 锁冲突时将 Task 改成 `waiting_local_directory`。
- 展示占用该目录的 Task。
- 等待期间续租、监听取消。
- 锁释放后自动进入 `running`。

相关实现：

- `core/internal/daemon/daemon.go:3975`
- `core/internal/daemon/local_directory.go:506`
- `core/pkg/db/queries/agent.sql:650`

这正是通用 Resource Scheduler 的原型，但目前有几个限制：

- 只支持 `local_directory`。
- 状态名和逻辑硬编码到资源类型。
- 锁只存在于单个 Daemon 进程内。
- Task 已经被 Daemon claim 后才开始等待，可能占用执行槽位。
- `wait_reason` 是人类文本，不适合统一调度和本地化。
- 多资源同时依赖时没有原子获取和死锁处理。

建议统一为：

```text
queued
  → resolving_resources
  → waiting_resource
  → dispatched
  → running
  → completed / failed / cancelled
```

其中：

- 无权限、资源不存在、配置永久错误：立即 `blocked`，需要人工处理。
- 临时离线、正在维护、容量不足、被占用：进入 `waiting_resource`。
- UI 立即展示等待状态。
- 资源释放或恢复在线时事件驱动唤醒。
- 定时重试作为兜底，采用指数退避和 jitter。
- 等待资源不应增加 Task 的执行 attempt。
- 多个资源必须“一次性全部获取”，不能拿着 A 等 B。
- Lease 必须有过期时间和 fencing token，防止旧 Daemon 恢复后继续操作。
- Task 完成、失败、取消、Daemon 失联时都必须释放或自然过期。

资源容量至少需要支持：

- `unlimited`：文件、URL、只读 Skill。
- `shared`：允许多个 Task 并发读取。
- `exclusive`：本地目录、独占设备。
- `capacity=N`：服务器槽位、GPU、API 并发额度。

## 六、Task 运行前应检查什么

建议把资源检查分成两个阶段。

### Task 创建或 Assign 时做静态准入

- 用户是否有权委托使用资源。
- Agent 是否允许使用。
- 资源是否存在。
- 资源类型是否满足 Requirement。
- 是否存在永久配置错误。
- Runtime/资源的地域与机器归属是否兼容。

### Task 真正调度时做动态检查

- 当前健康状态。
- 是否在线。
- 是否有空闲容量。
- 是否被其他 Task 占用。
- 凭据是否仍有效。
- 依赖版本是否发生变化。
- 是否可以原子获取全部 Lease。

“资源正忙”不应导致 Assign 失败，而应允许 Task 入队等待；只有权限和永久配置错误才应直接阻断。

## 七、推荐实施顺序

1. 先确定 Resource 术语和边界，形成 ADR，不急着改现有表。
2. 建立统一资源目录和 ResourceBinding，先纳入文件、仓库、本地目录。
3. 增加 ResourceGrant，先支持 workspace/private/restricted。
4. 增加 Task Requirement 与通用 `waiting_resource`，把现有 `local_directory` 特例迁进去。
5. 增加 Lease、容量、健康状态和事件唤醒。
6. 最后接入 Runtime、MCP、Connected App、服务器和外部服务。
7. 扩展现有 `execution_manifest`，让它成为每次运行真实获得资源的审计快照。

## 八、后续讨论的关键决策

- 这里所说的 Space 是现有 Workspace，还是未来 Workspace 内还会有新的 Space 层级？
- Resource 权限默认是 Workspace 可用，还是默认私有？
- Task 的资源需求由 Task 手动指定、Project 继承、Workflow 声明，还是四者组合？
- Agent 使用资源时，是否必须同时满足用户权限与 Agent 权限？
- 资源长期不可用时，是无限等待、超时失败，还是转为人工阻塞？
