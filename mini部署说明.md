# mini 独立部署

访问地址：`http://100.93.224.87:6688`，需要连接可访问 mini 的 Tailscale 网络。此实例使用独立数据库，不依赖 yilia，也没有迁移 yilia 数据。

## 服务和目录

- 部署根目录：`/Users/yzw/silieco`
- 当前版本：`/Users/yzw/silieco/current`
- PostgreSQL：本机 `127.0.0.1:15432`，数据位于 `data/postgres`
- 后端：`18080`；Web：Tailscale 地址的 `6688`
- 上传文件：`data/uploads`；建议项目目录：`workspaces`
- 日志：`logs/backend.log`、`logs/web.log`、`logs/postgres.log`、`logs/daemon.log`
- 配置密钥：`config.json`，权限 0600，不提交仓库
- launchd：`me.silieco.mini.postgres`、`me.silieco.mini.backend`、`me.silieco.mini.web`、`me.silieco.mini.backup`、`me.silieco.mini.daemon`、`me.silieco.mini.magpie`

服务以 `yzw` 的 launchd 后台会话运行，已配置 KeepAlive。没有修改其他用户的服务；尚未通过整机重启验证自动恢复。

## 注册与连接执行环境

网页使用邮箱和密码注册/登录，无需邮件验证码。用户自行创建账号和工作空间。

账号创建后，在 mini 的 SSH 会话运行：

```sh
/Users/yzw/silieco/bin/connect-mini.sh
```

在当前电脑浏览器打开脚本打印的登录 URL，授权后脚本启动 mini daemon。其独立 profile 为 `mini`，设备名为 `mini`，并发上限为 2，自更新关闭，避免独立部署版本漂移。

Claude Code 和 Codex 安装在 `/Users/yzw/silieco/tools/bin`。安装 CLI 后仍需登录 Silieco 并启动 daemon，运行时才会注册到工作空间。模型凭证另行配置在 mini 上。

安装页和连接远程环境弹窗使用当前实例的地址。mini 安装命令：

```sh
curl -fsSL http://100.93.224.87:6688/cli/install.sh | bash
silieco setup self-host --server-url http://100.93.224.87:6688 --app-url http://100.93.224.87:6688 --callback-host 100.93.224.87
```

安装器校验下载文件并安装到 `~/.local/bin`，无需 sudo。通用部署需要给安装器传入 `SILIECO_DOWNLOAD_BASE_URL=<实例地址>/cli`；mini 发布包已写入本实例默认地址。浏览器支持 Tailscale 的 `100.64.0.0/10` 登录回调。

创建项目后，在项目「资源」中选择 mini 并填写绝对目录路径，验证并绑定，即可浏览文件。Markdown 和 HTML 沿用现有预览组件。

## 数据库备份

备份在 mini 本机执行，每天按 mini 系统时间 03:20 运行，仅保留最新一份，目录为 `/Users/yzw/silieco/backups`。

先生成 `.partial` 文件，完成 `pg_dump` 并通过 `pg_restore --list` 校验后替换为正式备份，再删除旧备份。失败时保留旧副本，不会累积每日历史文件。备份体积会随数据库数据增长。

此备份只包含数据库，不包含项目目录、上传文件或模型凭证；本机副本也不等同于异机灾备。

手动备份：

```sh
python3 /Users/yzw/silieco/bin/backup.py
```

## 运维

```sh
launchctl print user/504/me.silieco.mini.backend
launchctl kickstart -k user/504/me.silieco.mini.backend
launchctl kickstart -k user/504/me.silieco.mini.web
curl http://127.0.0.1:18080/health
```

打包产物位于本仓库 `dist/mini-20261115-final.tar.gz`。发布时保留当前版本作为回退目录，校验新包后切换 `current` 链接并重启 Web/后端。数据库迁移需单独评估，不能自动还原数据库覆盖用户数据。

## 本次验证与临时访问

最终包 SHA-256：`b5c33adceeadff803cf2f514b9874b306120265bf6b22d254a69c11e661ec16b`。

已验证后端健康、登录页面、鉴权拦截；浏览器通过 SSH 转发确认了邮箱密码注册页面。已确认此前浏览器直连超时由系统代理绕过规则缺失导致。现已将 `100.93.224.87` 加入 Wi-Fi、Thunderbolt Bridge 和 Tailscale 的代理绕过列表，浏览器直连登录页已验证成功。临时 16688 SSH 隧道已按用户要求停止。仅在需要临时排查时重新建立转发：

```sh
ssh -N -L 127.0.0.1:16688:100.93.224.87:6688 -o IdentitiesOnly=yes -i /Users/ryan/.ssh/id_rsa yzw@100.93.224.87
```

目录预览的实现和测试范围见 [项目目录预览说明](docs/plans/2026-11-15-project-directory-preview.md)。mini daemon 已通过 yzw 账号连接 MachiMini，Claude 和 Codex 运行时均在线。分别执行只回复 `MINI_OK` 的真实模型请求，两者均成功。

## Magpie 与模型

Magpie 0.1.4148 位于 `/Users/yzw/silieco/tools/magpie.app`，CLI 入口为 `/Users/yzw/silieco/tools/bin/magpie`。使用 launchd 后台运行 `magpie serve`，只监听 `127.0.0.1:3425`。

已通过 Magpie 加密备份/恢复同步本机的 7 个服务商配置，不包含订阅登录会话或本机 Agent 历史；传输用的备份和口令已删除。Claude Code 和 Codex 的配置在 mini 重新生成，使用 mini 本机网关。目前默认模型为 `zhipu/glm-5.3-flash`（machi 服务商）。原 `ryan/glm-5.3-flash` 实测返回额度超限，故切换到有额度的已配置服务商。两个 CLI 都已通过简短真实请求验证。

项目 Customers 的专用目录已创建：`/Users/yzw/silieco/workspaces/customers`。用户可在项目资源中选择 mini 并绑定该路径。

最新 Web/CLI 更新包：`dist/mini-cli-web-v2.tar.gz`，部署目录 `releases/cli-web-v2`。打包必须保留 Next standalone 的依赖软链接。安装器已在 mini 实测成功安装 Silieco v0.2.2。

### Claude 双账号路由（后续更新）

Claude Code 当前使用 `group/claude-ryan-machi`，成员仅包含 `ryan/glm-5.3-flash` 与 `zhipu/glm-5.3-flash`（machi）。`routing=usage` 优先选择剩余额度更多的账号，`stays=auto` 尽量保留可用账号的缓存并允许故障切换。Haiku/Sonnet/Opus 的默认映射都指向该组。Codex 保持原配置。

配置后实测：路由已选择 Ryan，但供应商 Coding Plan 接口对 Ryan 的 `glm-5.3-flash` 和 `glm-5.3` 均返回 HTTP 429 / 1113（余额不足或无可用资源包）；与此同时额度查询显示 Ryan 的 5 小时额度剩余 99%。machi 的 5 小时额度为 0%。不能仅凭额度展示判断请求可用，需确认 Ryan 订阅/资源包与 API Key 的实际状态后重试。此前两个 CLI 调用成功的记录仅代表当时状态。

### yzw 账号同步与恢复验证

已从本地 Magpie 单独同步 `yzw` 服务商（API Key 与 Ryan 不同），并加入 Claude 的同一路由组。组 ID 仍为 `group/claude-ryan-machi`，显示名称改为「Claude自动路由」，现有成员为 Ryan、machi、yzw，继续使用 `routing=usage` / `stays=auto`。未复制其他本地设置。

同步后通过 Claude Code 实测返回 `YZW_OK`；Magpie 用量日志确认请求使用该路由组、实际服务商为 `yzw`、HTTP 200。Ryan/machi 的先前失败不再阻塞该组通过 yzw 执行。

## 文件浏览与版本标记更新

项目资源中的单目录绑定直接显示路径，多目录才显示选择器。点击「在新标签页打开」进入 `/machimini/projects/<project-id>/files` 独立页面，文件树和预览可调整宽度，继续按展开和选中文件加载。Web 使用浏览器标签页，桌面端使用应用 Tab。

CLI 内部发布版本改为 `v0.4.11-mini.1`，包含 commit 和构建时间，避免旧的 `v0.2.2` 标记错误触发项目上下文版本门槛。使用显式版本参数构建，不能再沿用临时脚本中的旧版本号：

```sh
python3 scripts/build-mini-cli.py --version v0.4.11-mini.1 --app-url http://100.93.224.87:6688
```

构建产物位于 `dist/mini-cli-release`，包含六个平台归档及校验和。

本次已部署到 `/Users/yzw/silieco/releases/mini-files-0411`，上一版 `cli-web-v2` 保留回退。发布前逐文件校验 manifest、独立端口预启动检查通过。发布后浏览器确认单目录无选择器、新 Tab 路由正常、Markdown 渲染与 HTML sandbox 预览正常；守护进程在线且上报 `v0.4.11-mini.1`，项目描述版本提醒已消失。相关 UI/路径测试 99 项通过，项目上下文 Go 定向测试、views/core/desktop 类型检查、Web 生产构建通过。桌面端完成类型与路由测试，本次未打包或运行 Electron。

## 可跳过的新用户引导

Web 发布目录为 `/Users/yzw/silieco/releases/mini-onboarding-0411b`。引导的每一步都可「全部跳过，稍后设置」，完成标记保存到账号；没有工作空间时进入 `/workspaces` 等待邀请，也可主动创建。该页面显示当前邮箱、退出登录、邀请入口与刷新按钮，每 15 秒检查邀请与成员资格。引导页和无空间页均使用统一退出流程清理账号缓存。创建页可返回等待页。

使用临时新账号验证了注册、跳过、刷新、创建页返回、退出及重新登录后的空空间状态。相关 UI 和路径测试 21 项通过，views/desktop 类型检查与 Web 生产构建通过。后端和 CLI 保持原版本，CLI 仍为 `v0.4.11-mini.1`；桌面端已同步页面入口，本次未重新打包 Electron。

## 群聊项目与头像更新

Web 发布目录：`/Users/yzw/silieco/releases/mini-chat-0411`，上一版 `mini-onboarding-0411b` 保留。群聊通过「＋」设置项目时，原地更新项目上下文，保留会话 ID、成员与消息；不再误入 Agent 单聊。聊天列表、标题与悬浮窗使用最多 9 位人／Agent 的头像拼图，消息行显示实际发送者头像，历史单聊消息由会话身份补全头像。

65 项相关 UI/控制器测试通过，views/desktop 类型检查和 Web 生产构建通过。浏览器使用临时群聊验证选择 Customers 后会话与成员不变，数据库确认项目绑定；临时群聊已通过 API 删除。已有群聊的成员头像与拼图显示正常。本次仅发布 Web，共享桌面组件已更新但未重新打包 Electron。

## 用量汇总初始化与修复

新数据库的 `agent_run_usage_hourly_rollup_state.watermark_at` 从 1970 年开始，而定时汇总每 5 分钟最多推进一天。只启动服务器会长时间得到空的用量页，即使 `agent_run_usage` 已记录 token。新实例完成迁移后必须执行一次现有回填工具（即使明细为空也会初始化进度）：

```sh
# 在开发机本地构建，再上传到 mini 的 tools 目录。
cd core
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o ../dist/mini-usage-backfill ./cmd/backfill_agent_run_usage_hourly
# 在 mini 使用现有配置提供 DATABASE_URL，先 --dry-run，再执行正式回填。
# DATABASE_URL 不得打印或写入版本库。
```

2026-11-15 已使用该工具完整回填 mini（未限制月份），并将进度校正为当前时间减 5 分钟。5 条小时汇总与 8 条原始明细的合计一致：输入 211164、输出 7139、缓存读取 300288、缓存写入 0。随后调用定时汇总入口成功，进度正常推进；无需升级或重启 Claude Code、Codex 或 daemon。统计通常存在约 5–10 分钟延迟。

当时全部 9 次 Silieco 执行来自 Claude（6 次完成、3 次失败），Codex 运行时在线但没有 Silieco 执行记录，不能把暂无 Codex 统计判断为采集失败。Claude result/modelUsage 与 Codex 会话用量、续跑差值相关定向测试通过；未额外发起模型请求。
