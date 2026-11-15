# Task / AgentRun 重构验证记录

日期：2026-11-06。当前工作区尚未提交；内测服务器已切换，以下记录区分源码验证、实际部署和客户端验证边界。

## 已完成

| 范围 | 结果 |
| --- | --- |
| 命名静态守卫 | `pnpm check:task-domain` 通过；覆盖 4,000 余个活跃源码文件并检查 JSON 对象重复键。保留第三方 GitHub issue、Zod `error.issues` 和明确的旧协议拒绝用例 |
| TypeScript | core、views、Web、Desktop、Mobile 分别通过类型检查；源码变动后仅复查受影响包 |
| 静态检查 | core 228、views 437、Web 26、Desktop 43 个改动文件 ESLint 无错误；Mobile 独立检查无错误 |
| API 与实时契约 | HTTP / WS 代际、Task / AgentRun ID、消息列表、执行响应、用量、错误响应解析的定向测试通过 |
| 共享状态 | Task 缓存更新/删除、执行缓存独立 workspace 标识、聊天缓存、重连、草稿代际和 revision 冲突保护的定向测试通过 |
| 执行展示 | 执行日志、执行计数、Agent header chip、活动页、transcript 的相关测试通过 |
| 浏览器 | 本地隔离库下的看板加载、创建 Task、详情导航、添加评论、父子任务跨状态分组共 5 个代表流程通过 |
| Desktop | Electron main / preload / renderer 构建和 CLI 打包通过；运行中版本判断的 9 个定向测试通过 |
| Web | 本地生产构建与 Linux AMD64 Docker 首轮构建通过；容器登录页、30 个静态资源、后端代理健康均为 200。最终镜像已导出、校验并部署 |
| Mobile | 类型检查、27 个定向测试、最终 Hermes 导出通过；原生 iOS arm64 Release 编译通过；无签名/真机联网边界见独立记录 |
| Go 与数据库 | Go 编译及定向契约测试通过；数据库结构、生成 SQL、删除行为与真实并发验证见数据库审计。最终删除入口接入后的复查、独立并发审查及 PG16 验证通过 |

登录包装层 10 项、共享登录页 34 项，以及下载入口实际文件可用性 2 项定向测试通过。发布核对恢复缺失的 `/download` 页面，只展示当前挂载目录实际存在的安装包。

执行记录主要保存在本机 `/tmp/silieco-*.log`，源码测试和守卫已保留在仓库中。没有运行根目录全量 `pnpm test`、全量 Go 业务测试或完整 Playwright 套件，也没有调用真实模型账号。

## 验证中发现并修正的内容

- 业务状态与执行状态、数量和 ID 分开；移除将 `Task` 缓存当执行缓存的调用。
- 不再因旧服务端秒精度 revision 而省略冲突保护；分页接口缺失时直接返回错误，不回退旧接口。
- Task 响应和 Run 写入响应解析失败不制造成功对象；用量响应校验 `run_count`。
- Mobile 的 `waiting_local_directory` 保持执行活动状态，不退化成 queued。
- 跨端审查发现执行消息缺少 `chat_session_id`、进度缺少业务/聊天上下文；已由后端补齐，对应普通任务与聊天消息/进度路由、catchup 测试通过。
- 浏览器首次开发编译超时后改用生产构建验证。表格用例原本错误地要求分组行接口返回全局总数，并可能捕获前一个未分组子行请求；核对重构前实现后修正断言及请求匹配，保留真实行关系与精确分组数量断言。

## 实际发布与公网验收

- 地址：`http://yilia.me:6688`；测试空间：`/task-run-test/tasks`。
- 重建指定数据库 `silieco_internal`，373 条迁移与本地基线逐项一致；旧 `issue` / `agent_task_queue` 不存在，public 外键数为 0。
- 清理指定上传卷 `silieco-internal_backend_uploads`，没有删除 PostgreSQL 卷；当前未配置 Redis/S3。旧业务数据、应用 token 已清除；保留原 Demo 管理员邮箱、名称和密码登录凭据，并创建新账号身份和 PAT。
- Demo systemd 服务 active，CLI `v0.0.0-32-gc4b3982-dirty`，Codex runtime online；未发送 Agent 任务。`agent_run` 最终仍为 0。
- 公网 HTTP / WS 人工验收通过：Task CRUD、普通评论、`auth_ack` / `subscribe_ack` / `task:created`、旧代际拒绝、业务 ID 与执行 ID 路由隔离。验收临时任务已删除，另保留一个手工任务；它已完成一次 done → todo 状态切换，全程无 AgentRun。
- 公网浏览器通过：1 个真实 Task 卡片、31 个 API/XHR、108 个静态资源、cookie-auth WS 升级、下载页和新 macOS x64 ZIP。无页面 JS 错误；验收脚本阻止了 2 个客户端用量写上报。
- 包括 PostgreSQL 在内的 8 个原有容器 ID 与运行状态均未改变；用户代码目录和模型凭据不在清理范围。

## 发布时发现的配置问题

- `/download` 的旧重定向遮挡了已恢复的下载页；源码已删除这条重定向。Web 已完成生产编译与类型检查后，仅对三个对应的生成配置及两个缺失静态图标做本地、可复现的镜像覆盖；业务 JS bundle 未修改。脚本、基础镜像摘要和最终摘要记录在发布目录中。
- 本地 Docker 28 使用 config digest，服务器 Docker 29/containerd 使用 OCI manifest digest。初次预检查因此在停服务/清库前中止。已逐字节核验归档 manifest → config 摘要链、精确 Config、RootFS 和平台，保持严格校验后才执行切换；没有忽略配置差异或放宽应用协议。
- 本地浏览器首次使用了不在允许 Origin 中的回环地址，已改用配置中的 `localhost`。cookie-auth 模式通过升级前鉴权，不发送 token-auth 的 `auth_ack`；只读查询 POST 仍需真实 CSRF cookie。修正验收环境与凭据构造后，本地和公网同一浏览器脚本均通过，未修改产品鉴权规则。

## 客户端交付边界

- Desktop：macOS Intel `.app` ZIP，未签名、未公证；已验证 Electron/renderer/内置 CLI 打包，没有启动用户本机桌面应用。其他平台旧包已退出公开下载。
- Mobile：iOS arm64 Release 编译成功，Hermes bundle 内 API/Web 均指向 `http://yilia.me:6688`，包含 `task-run-v1`。仅开发环境显式允许 `yilia.me` 的 HTTP，默认/staging/production 会清除例外。产物未签名、无 provisioning profile，不能直接安装；未做真机联网验收。
- 没有运行真实模型账号 smoke test、根全量测试或完整 E2E 套件。模拟执行、关系并发和各端契约使用已列出的定向测试覆盖。

## 可复现产物与回滚范围

本机：`dist/task-run-2026-11-06/`；服务器：`/opt/silieco-internal/releases/20261106-task-run/`。

保留 `manifest.json`、源码快照、后端/Web 镜像、CLI、未签名桌面与 iOS 产物、构建元数据、三个脱敏部署验收报告和部署脚本。源码来自未提交工作区，不能只凭 Git commit 重建。

旧程序镜像和配置已保留；新基线与旧程序不兼容。程序回退需配套旧基线重建测试库，不能直接 retag 旧镜像连接新库，也不承诺恢复已丢弃的旧测试数据。

相关文档：

- [实施方案](2026-11-06-task-domain-unification.md)
- [数据库关系审计](2026-11-06-task-database-audit.md)
- [Mobile 构建与验证边界](../../apps/mobile/docs/task-run-validation.md)
