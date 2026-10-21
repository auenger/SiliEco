# SOP 验收记录与人工回归清单

当前状态：核心实施后的人工验收中。已提供 [功能回归验收手册](2026-09-09-sop-manual-regression-guide.md)，包含 57 个用例及结果记录格式，人工结果尚未填写，不代表最终验收通过。下表区分服务集成、组件测试和真实浏览器结果，不将其中一种替代另一种。

## 环境

- 本地 Web：<http://localhost:65315>
- 本地 API：<http://localhost:18094>；健康检查 `/health`。
- 独立数据库：`silieco_sop_acceptance`，PostgreSQL 端口 65313。仅复制测试库结构，未复制用户数据。
- 浏览器使用 `TestApiClient` 登录、真实 API 发布 SOP，并归档测试创建的 SOP。
- 当前测试不调用用户安装的智能体 CLI，不产生真实模型执行。

## 当前核心范围与差距（2026-09-09）

按用户最新指示，评测相关暂缓；评测、发布、观察与回滚不计入本次核心交付缺口。已有代码保留。下方历史轮次中的“仍待完成”是当时状态，本节优先于历史记录。

| 核心能力 | 已实现与已有证据 | 当前缺口／边界 |
| --- | --- | --- |
| 阶段执行与推进 | 固定执行快照、持久派工意图、幂等恢复、任务与结果门禁；服务／HTTP 集成覆盖暂停、取消、迟到回调和未来阶段不得提前执行 | 还需将输入补正、产物提交与下一阶段推进串成完整浏览器验收 |
| 审核专用重试与恢复 | 自动技术重试、退避与上限；失败／完成无结论均可人工恢复；新调用与旧决定隔离；两类恢复已有真实浏览器通过记录 | 受控 provider 验证协议；未声称真实模型完成审核 |
| 返工与下游失效 | 人工与智能体来源、人工与智能体目标；旧任务／结果／决定失效、新轮次和审计；混合下游并发已有集成证据，人工再完成及 Agent 退回人工再执行已有浏览器证据 | 本轮组合浏览器回归已通过；真实模型执行未包含在此证据中 |
| 配置表单 | 执行者、契约、绑定、审核、回退目标、返工次数，以及重试次数／超时／退避／倍数／上限；真实发布请求已有浏览器校验 | Run 初始输入与阻塞补正的浏览器串联尚需补齐 |
| 页面与弹窗 | 配置统一 4xl、审核／返工／恢复操作统一 2xl；已有桌面及 390px 窄屏截图检查 | 深色主题、长内容及全部操作弹窗还需统一巡检 |
| 执行中的计划编辑 | 未产生智能体执行记录时可调整符合约束的阶段；已有执行后服务端禁止修改计划 | 已知功能限制：不能只编辑尚未执行的未来阶段；需要兼容冻结版本的独立设计，不属于已完成能力 |

主体功能已实现，当前不能仅凭历史专项测试声明最终交付。当前代码组合回归已通过。剩余收尾分为两块：完整输入到交付推进的浏览器链，以及视觉巡检与最终人工回归清单。运行中编辑未来阶段另列为功能限制，不与“未来阶段不可提前执行”的可靠性保护混淆。

### 本轮组合复核

核心 Workflow／Builtin 服务测试通过（84.755s），HTTP 测试通过（13.421s）；契约包另行不加名称过滤全测通过（2.177s）。使用独立 PostgreSQL 测试库。日志：`/tmp/silieco-core-scope-regression.log`。6 个 SOP 浏览器文件、共 7 个场景全部通过（5.3m）：跨类型返工再执行、契约发布、重试／返工策略配置发布、旧审核状态冲突、人工返工再完成、失败审核恢复、完成无结论审核恢复。日志：`/tmp/silieco-core-scope-browser.log`。使用真实本地 API 和受控 provider，没有调用真实模型或用户安装的 Agent CLI。本轮测试均已结束，无挂起检查。

截图复查：契约窄屏表单内容和操作按钮可达；智能体退回人工的窄屏返工弹窗取消按钮贴近底部滚动边缘，需在视觉收尾中检查完整按钮可见性。不能将功能断言通过等同于所有界面细节完成。

## 人工回归步骤（待最终验收后使用）

1. 新建空白 SOP，切换智能体执行，明确选择执行者；审核者应单独配置。
2. 配置输入输出：增加字符串／数字字段、必填项、文件要求；确认高级 JSON 中的嵌套约束保存后仍存在。
3. 添加 Run 输入绑定及前序结果绑定。删除或重排依赖阶段时，应提示修复，不得自动改为另一个来源。
4. 编辑契约后取消，重新打开确认未应用的草稿没有进入父表单。应用后仍需发布 SOP 或保存 Run 计划。
5. 将智能体执行切回手动再发布；请求不能残留执行智能体，界面只显示适用的完成规则。
6. 继承含自定义稳定标识的 SOP，确认回退目标保留；失效目标需要明确选择合法目标或取消回退。
7. 启动 Run 时输入参数。非法 JSON 不可提交；连续点击启动或按 Enter 不能重复创建 Run。
8. 在桌面和窄屏打开配置弹窗，滚动到底部，确认内容不穿透按钮栏，所有文件字段和操作可达。
9. 审核通过、技术失败重试、返工和下游失效按最终提供的预置场景逐项回归；人工退回已有下述浏览器证据，其余场景继续补齐，本清单尚非整体完成声明。

## 可重复运行的配置浏览器测试

```bash
PLAYWRIGHT_BASE_URL=http://localhost:65315 \
NEXT_PUBLIC_API_URL=http://localhost:18094 \
DATABASE_URL='postgres://silieco:silieco@localhost:65313/silieco_sop_acceptance?sslmode=disable' \
SILIECO_DEV_VERIFICATION_CODE=918273 \
pnpm exec playwright test e2e/sop-configuration.spec.ts --project=chromium
```

执行前确认 Web 和 API 已启动，并等待 `/health` 返回 200。首次 Web 冷编译曾超过测试时限，后续配置路径通过；详细结果及滚动修复复测记录见 [实施记录](2026-09-09-sop-contract-foundation.md)。截图输出位于 `test-results/sop-configuration-*/`。

最终配置浏览器复测通过（58.6s；整次 1.1m），包含窄屏滚动范围及底部文件操作可见性断言。桌面／窄屏截图已人工检查，MCP 列表在补齐验收环境加密配置后返回 200。最终 views 类型检查通过。

返工下游隔离新增验证：旧人工阶段 issue 与决定保留原状态／结果并标记失效；活动调用和并发生成的重试调用被取消；失效父 issue 不接受新子任务；列表、分组和搜索均保留标记。失败触发重试的锁顺序已用 PostgreSQL 实际阻塞测试验证。相关 HTTP 测试通过，WebSocket／返工客户端 16 项测试通过；这些结果不代表完整人工退回链已完成。

人工审核状态保护：Run 响应提供 `state_token`，弹窗固定打开时的值；Run 变化或提交结果不确定后，需要关闭并重新审核。该凭据保护 Run 状态，不能替代交付结果的证据版本审核。正式 Run 入口位于项目页的 `?run=<id>`；SOP 详情页是定义编辑器。

人工审核浏览器最终复测通过（20.0s；整次 29.1s）：项目 Run 页面打开审核，第二客户端将同阶段 Run 改为 waiting，旧凭据提交返回 409，实时刷新禁用旧弹窗；关闭重开后用新凭据成功完成 Run。桌面 2xl 宽度及 390px 窄屏边界、按钮可见性通过，截图已查看，无横向溢出或按钮遮挡。测试创建的 SOP 已归档，项目及其 Run 已删除。测试命令同上，将文件替换为 `e2e/sop-manual-review.spec.ts`。


人工退回专项已完成真实浏览器复测（41.8s；整次 59.4s）：原因与影响确认缺一不可；退回后旧 task 和审批保留历史但标记失效、旧 task 从当前看板排除；新增交付后重新推进并完成 Run。桌面和 390px 窄屏截图已查看，确认区与操作按钮均可达，无横向溢出。命令同配置测试，将文件替换为 `e2e/sop-manual-rework.spec.ts`。首轮在页面重载等待中超时，最终复测使用已启动的相同服务通过，没有以部分通过代替全链结果。

人工审核退回智能体目标的 PostgreSQL 集成验证已通过：原已验收执行失效，新执行 attempt=2，旧结果／接受信息未复用，恰有一个新派工意图。纯人工路径不创建虚假的智能体执行。Run 详情提供返工审计记录；对应 issue／决定字段为 `workflow_invalidated_by_rework_id`／`invalidated_by_rework_id`。迁移 311–315 已应用到验收环境。若已有纯人工来源的返工记录，311 的降级迁移会拒绝执行，避免旧 schema 无法保存这些历史。

人工退回本轮最终类型检查、内置 skill 校验和 diff 检查均已通过。当前真实浏览器证据覆盖纯人工退回再完成；退回智能体目标已有服务集成证据，不能将二者合并声称完整智能体审核恢复链已在浏览器通过。


## 第五十七轮补验：审核失败恢复

- 已通过真实浏览器：失败审核显示恢复入口，原因／确认缺一不可；提交真实恢复接口返回 200，新调用与旧调用不同、授权预算 3 次、总 ceiling 6；页面显示排队并移除恢复入口，Run 保持 active。
- 桌面／390px 窄屏截图：`test-results/sop-review-recovery-failed-bf2fa-ed-without-reusing-its-call-chromium/review-recovery-{desktop,mobile}.png`。均已人工查看，操作弹窗沿用 2xl，文字和按钮可达、无横向溢出。
- 后端并发恢复、旧结论不得放行新调用、旧 token 撤销、环境迁移不复用 workdir、自动重试待执行时拒绝人工加派、权限及旧版本拒绝，均已验证。
- 测试使用受控 provider，没有 daemon，没有调用真实 Agent CLI。此证据说明实际页面与本地后端恢复协议通过，不代表模型实际完成了新审核。
- 已完成但没有结论的审核仍仅有诊断；智能体来源返工到人工目标、配置策略与未来阶段约束及 P3 skill 评测发布闭环仍待完成。本文仍为验收记录，尚非最终人工回归清单。


## 第五十八轮补验：Agent 退回人工输入再执行

`e2e/sop-agent-manual-rework.spec.ts` 已首次跑通真实页面／本地后端：选择配置的人工回退目标、填写原因并确认，返工返回明确的空 Agent 执行身份；原人工输入与下游旧工作失效。创建新人工输入，重新审核后进入 Agent 第 2 次执行、Result version 0。受控 provider 无 daemon，不调用真实 Agent CLI。

后端并发重复请求只产生一个返工记录，人工目标没有伪造轮次；旧执行继续提交被拒绝。HTTP、race、响应 schema、mutation 缓存失效和目标选择组件已覆盖。两种结果都通过响应 schema；人工结果缺失返工身份、包含伪造执行 ID／attempt 或错误状态时拒绝。

首轮截图已检查桌面和窄屏内容／按钮可达。最后的提示文案及主题色调整正在浏览器复测。仍未到整份计划的最终人工回归交付。

第五十八轮浏览器最终复测通过（2.3m／整次 2.5m）。已重新检查窄屏提示文案和内容边界；单选使用主题强调色，复选框随后补齐相同样式类。组件最终 10 项通过。完整 views 类型复测尚在运行，当前不宣称该项通过。


## 第五十九轮补验：完成但无结论的审核恢复

真实浏览器的失败调用与 completed／missing_decision 两场景均通过（整次 2.8m）。无结论调用恢复后生成独立审核调用、预算 3 次，页面更新为排队，Run 保持 active；旧完成状态保留。已有通过／拒绝结论时禁止恢复，恢复与迟到决定只允许一方生效，已通过服务和 race 验证。

missing 场景桌面／390px 截图位于 `test-results/sop-review-recovery-missin-506be-ed-without-reusing-its-call-chromium/`，已查看，无横向溢出，操作可达，沿用统一 2xl。受控 provider 无 daemon，未运行真实 Agent CLI。首轮浏览器在 API 重编译尚未启动时连接失败，确认健康检查 200 后复测通过；不将首轮算作功能通过。

完整 service／builtin、HTTP、race、13 项组件、E2E 类型和 vet 通过；共享 views 增量类型检查仍在执行。完整配置策略、未来阶段约束、最终组合验收和 P3 skill 评测发布仍需继续，尚非最终人工回归交付。


## 第六十轮补验：真实表单配置人工回退目标

SOP 设计器和 Run 计划配置已取消 Agent-only 回退目标过滤。设计器／Run 表单 13 项测试、共享 views／E2E 类型检查及内置 skill 通过。`e2e/sop-cross-mode-configuration.spec.ts` 从空白 SOP 表单配置 Manual input → Agent Build、选择 Manual input 作为回退目标并提交真实发布 API；后端返回 201，配置发布通过（41.3s／整次 51.7s）。受控 Agent 仅用于配置，无 daemon 或真实 CLI 调用。

桌面／窄屏截图已查看，无横向溢出，顶栏发布按钮可达。首轮文本断言误包含下拉箭头，修正后通过；随后修复截图发现的旧 Agent-only 提示文案和执行模式主题色，最后显示复测仍在运行。

未来阶段编辑仍受整体执行版本约束；完整重试／返工策略配置、最终组合验收、P3 skill 评测发布仍未完成。此记录不是最终人工回归交付。

第六十轮最终复测通过（浏览器 1.5m／整次 1.6m）。最终窄屏截图已重新查看：帮助文字明确支持人工或 Agent 目标、主题单选色正确、发布按钮可达。类型、13 项组件、内置 skill 与 diff --check 均已通过，本轮无挂起检查。完整 goal 继续进行。


## Round 61: bounded rework policy

Stage configuration now persists `rework_policy.max_reworks` (integer 0–100, omitted means no configured cap). The shared rework transaction counts initiations by Run and source stage, uses frozen policy for agent executions, and rejects exhaustion before invalidating any work. Repeated agent requests reuse the original audit. Both configuration forms expose the field; review/rework controls report exhaustion.

Verified: workflow HTTP suite (7.618s); focused manual HTTP regression (5.206s) proves template-to-Run copying, one successful rework, rejection of the second, unchanged state and one audit record. Service/builtin policy checks pass (3.731s). API malformed-response tests: 14 passed. Execution configuration components: 7 passed after separating the accessible input name from its help text; the other two controls passed 22 cases in the earlier run. Shared views typecheck completed successfully. Migration 320 up/down/up was checked in an isolated transactional schema, including default object values and rejection of arrays.

The real browser configuration test passed (53.8s; total 1.1m): it fills a limit of 2 and asserts it in the successful publication request. The 390px screenshot was inspected: aligned field widths, no horizontal overflow, and reachable publish action. E2E typecheck also passed. No checks from this round remain running. This is not final acceptance of retry policy, future-stage editing, or P3 skill evaluation/publication.

### Skill 候选修订界面验收（第 76 轮）

`e2e/skill-revisions.spec.ts` 使用真实本地 API、独立测试工作区与浏览器完成：打开版本页、复制已保存的内容和配套文件、填写契约版本和工具依赖、创建候选（201）、读取详情、刷新后重新读取；另以 API 确认当前 skill 内容仍是原始基线。没有调用真实智能体。

- 最终浏览器用例通过：26.5 秒，总计 34.8 秒。日志：`/tmp/silieco-revision-browser-verified.log`。
- 检查 1440px 桌面及 390px 窄屏；创建和详情弹窗桌面宽度一致，小屏保留左右边距且没有页面水平溢出。已查看桌面创建、窄屏创建和桌面详情截图。
- 首轮尺寸断言发现边距约束需要显式声明；改为 `w-[calc(100vw_-_2rem)] max-w-none sm:max-w-4xl`。随后修正屏幕尺寸切换时的中间布局读数（等待尺寸稳定）及只读文本框定位方式；最终端到端流程通过。
- 测试仅清理本次创建 skill 的修订和 skill，不清理其他数据。
- 人工回归入口：skill 详情 → 版本 → 创建候选修订。可检查文件编辑、契约/工具输入、关闭和失败后的草稿表现，以及不同屏幕宽度。此项验收不涵盖尚未实现的评测、发布、回滚及改进提案闭环。

### 改进提案界面（第 79 轮）

人工回归入口：skill 详情 → 改进提案。先在“版本”创建两个不同修订，再创建提案，检查基线/候选选择、内容比较、分类、理由、假设、适用范围和可选执行证据。创建提案不会改变当前 skill。

- 首轮真实浏览器通过：在独立测试工作区创建两个修订，经页面提交提案（201），读取详情，刷新后重新读取，并通过 API 确认 live skill 仍为原始内容。
- 1440px 桌面表单与详情均为 896px 宽；390px 窄屏弹窗不超过 358px，页面无水平溢出。已检查桌面表单、窄屏表单和桌面详情截图。
- 交互测试覆盖无编辑权限、修订不足、提交后详情、未知结果锁定字段并重试相同请求，以及明确 400 拒绝后允许修改。未知结果时请先重试同一请求或关闭检查列表；不要立即另建相同提案。
- 当前 Run 来源通过可选执行 ID/结果版本输入，服务端校验身份与归属；从 Run 页面直接选择证据的入口仍待完成。本轮不代表评测、发布及回滚验收通过。

选择器补齐共享组件要求的 `items` 映射后，最终真实浏览器复验通过：用例 47.9 秒、总计 57.9 秒；日志 `/tmp/silieco-proposal-browser-final.log`。5 项提案交互测试与 8 项原详情页回归测试通过，E2E 类型检查和内置 skill 检查通过。

最终 views 类型检查通过；日志 `/tmp/silieco-proposal-ui-types-verified.log`。本轮要求的检查均已结束，没有遗留运行中的测试。
