# SOP 契约实施记录

日期：2026-09-09。对应 [总体计划](2026-09-08-sop-stage-automation-and-skill-iteration.md) 的 P0 与 P1 产物预检基础。整体计划仍在实施中。

## 已实现范围

`core/pkg/workflowcontract` 提供无网络、无存储依赖的共享 Go 校验器；CLI 已接入，服务端结果提交尚待接入。新增 `silieco workflow preflight --contract <file> --result <file>`，检查数据和产物清单，返回 JSON 路径、错误码和说明，失败退出码非零。

契约版本为 `silieco.stage/v1`，包含 `data` 和 `artifacts`：

- 数据支持 object、array、string、boolean、number、integer；支持必填属性、标量枚举、数值上下限、数组数量。对象不接受未声明字段。
- 不支持任意表达式、外部引用或完整 JSON Schema。未知关键字、未知版本、重复 JSON 键均拒绝。
- 数字精确比较，不把字符串转为数值；有大小、深度、精度及指数限制。
- 每个产物用唯一名称声明 kind、media_type、schema_version 和 required；结果必须提供附件 UUID 和 SHA-256 摘要。一个附件不能重复满足不同产物。
- 客户端结果只有 data 和 artifacts，不允许自行声明审核结果、轮次身份或已验收版本。

本地预检成功只证明结构符合契约。附件所属工作区、访问权限、实际字节摘要、媒体格式和业务正确性须由服务端验证。没有结果提交 API，也没有新增自动派工或推进能力。现有手动 SOP 的自由描述配置未替换成可执行契约，避免产生“已配置就会强制验收”的误解。

## 样板与验证样例

继续沿用工作区已有的热仿真 POC。当前代码样例固定为模拟结果交付环节；完整八阶段契约和真实业务输入仍须补齐，不把这一环节视为 P0 全部完成。

| 样例 | 内容 | 预期 |
| --- | --- | --- |
| `core/pkg/workflowcontract/testdata/thermal-contract.json` | 模拟结果数据、报告与结果附件要求 | 可解析契约 |
| `thermal-normal.json` | 明确模拟工具，74 °C 对比 80 °C，依赖需求修订 1 | 数据结构通过 |
| `thermal-failed.json` | 缺少目标温度、带单位字符串、空来源 | 缺项、类型和数量错误 |
| `thermal-reworked.json` | 需求修订 2 将目标改为 70 °C，结果明确 fail | 数据结构通过；业务审核仍应判断未达到目标 |

数值均为测试数据，不代表客户输入或真实工程验证。`verdict: fail` 可以是完整、诚实的产物；结构校验通过不代表业务目标达成。来源字段目前仅校验结构，P1 必须通过服务端绑定解析实际已验收修订，不能信任客户端来源声明。

## 存储与版本核查

- 现有 `attachment` 和 `attachment_reference` 可复用为内容存储和关联层。`GetAttachment` 已按工作区查询；附件当前字段没有权威内容摘要或不可变产物修订。新增业务清单必须由服务端读取字节计算摘要，并保证底层内容不会被静默替换或删除。
- 现有 `skillbundle.BuildManifest` 对内容和排序后的文件计算摘要，包含来源、身份、名称和说明。可复用为修订内容身份，不另创摘要算法。
- 当前 skill 和 skill_file 支持更新；Run 阶段已有 required_skill_snapshots。快照和摘要不等于不可变候选修订、发布指针或完整评测生命周期。
- P3 需将发布指针与不可变内容分离，并记录样例权限、保留集隔离、固定环境和评测证据。尚未实现这些表和服务。

## 后续实施顺序

1. 完成八阶段输入输出契约、审核与版本交接样例；将配置与结果契约写入阶段快照。
2. 增加执行轮次、主 issue 关联、状态及派工意图持久化，补齐应用事务和工作区清理。
3. 在 `WorkflowExecutionService` 内实现结果提交、真实附件验证、必需任务屏障、人工验收及已验收版本交接。人工与自动推进共享并发控制。
4. 接入执行队列、重启补偿、暂停恢复与未来阶段控制，再开放配置和运行界面。
5. 按总体计划继续 agent/hybrid 审核、返工失效与 skill 评测发布闭环。

本记录只记录当前成果和下一步依赖，不缩小总体计划的验收范围。

## 本轮验证

2026-09-09 执行并通过：

- `cd core && go test ./pkg/workflowcontract -count=1`
- `cd core && go test ./cmd/silieco ./internal/service -count=1`
- `cd core && go test -race ./pkg/workflowcontract`
- `cd core && go test ./pkg/workflowcontract -run '^$' -fuzz FuzzParseContract -fuzztime=5s`，完成 60,089 次输入测试。
- `cd core && go vet ./pkg/workflowcontract ./cmd/silieco`
- `git diff --check`

本轮没有新增数据库或前端行为，未运行完整 `make check`；上述检查不证明自动推进、服务端验收或完整 SOP 已实现。

## 第二轮：执行轮次与持久化派工意图

2026-09-09 新增：

- `workflow_stage_execution` 保存轮次、计划修订、实际执行者、阶段配置与输入快照、主 issue 指针及执行状态。
- `workflow_execution_action` 保存派工意图、重试时间、领取次数、租约和领取令牌。领取语句使用 `FOR UPDATE SKIP LOCKED`；失效租约可重新领取，旧令牌无法确认新租约。
- `WorkflowExecutionService.EnsureStageExecution` 在同一事务中创建首个轮次及派工意图。重复请求返回原轮次，不因失败或重试请求悄悄新建业务轮次。显式返工仍待实现。
- 创建时锁定工作区、项目和 Run，检查当前阶段、计划修订、活跃状态、归档状态和执行者所属工作区。项目／工作区删除已补充显式清理；共享父资源锁与现有删除锁配合，防止清理期间新增孤立记录。
- 领取只考虑活跃且未归档的 Run，以及仍匹配当前阶段和计划修订的轮次。`waiting`、取消、已完成、归档及失效计划均不领取。
- 迁移 267–274 无外键或级联；7 个索引分别使用单语句 `CONCURRENTLY` 文件，均有回滚迁移。sqlc 使用与仓库一致的 v1.29.0 重新生成。

这些是内部执行层基础，尚未连接启动接口、Worker、TaskService 或界面。领取租约本身不是执行授权：Worker 交付前必须重新锁定 Run，检查当前权限、状态和轮次，并将主 issue／task 的幂等创建与意图确认纳入事务设计。结果提交、产物修订、输入绑定、暂停恢复、审核及自动推进继续按总体计划实现。

本轮在独立 PostgreSQL 17 容器中应用完整迁移；另建空测试数据库，对 267–274 执行 up → down → up，确认 7 个索引全部有效。测试资源没有连接现有项目数据库。

第二轮验证全部通过（`DATABASE_URL` 指向独立测试容器）：

- `go test -race ./internal/service -run '^TestWorkflowExecution' -count=1 -v`：7 项集成测试及工作区／项目删除锁子用例，均实际执行，无数据库跳过。
- `go test ./internal/handler -run 'TestWorkflow|TestProject' -count=1`：现有 SOP 与项目接口回归。
- `go vet ./internal/service ./internal/handler ./pkg/db/generated`。
- `git diff --check`。

下一步优先接入阶段执行配置、启动事务及派工 Worker，然后实现产物修订和服务端验收。当前仍不能据这些内部测试认定点击启动会自动完成 SOP。

## 第三轮：派工事务与队列约束

2026-09-09 新增：

- `DispatchStageAction` 将主 issue、task、执行轮次绑定和派工意图确认纳入一个事务；失败全部回滚，重复投递旧领取令牌不重复创建任务。
- TaskService 将队列写入与提交后通知分离，复用现有归因与运行时配置逻辑；issue 创建事件和守护进程唤醒仅在事务提交后发送。
- Server 已启动 `RunDispatchWorker`，周期扫描持久化派工意图。临时错误保留同一业务轮次并延后重试，进程退出留下的租约可回收。
- 执行者删除／归档、缺少运行时或启动者无调用权限会阻塞轮次并保存原因。管理员不能绕过私有智能体调用权限；暂时离线的运行时保留排队任务。
- task 增加独立 `workflow_stage_execution_id`，执行轮次保存主 issue 与主 task。原 issue 移动不会重写已存储的 task 轮次身份。
- 绑定轮次的 task 领取时，检查 Run 活跃及未归档、当前阶段／计划修订、issue 归属及状态、执行者权限和启动者成员资格，并对 Run 取得共享锁。

迁移 275–276 为可回滚的列与独立并发索引迁移，已应用于独立测试数据库。尚未开放阶段执行配置和启动接口，也未接入结果提交、服务端产物验收、输入版本绑定及下一阶段推进。上述领取约束当前针对带执行轮次绑定的 task；现有手动 SOP 的未来阶段任务约束仍需统一接入，不能视为 P2 已完成。

第三轮验证通过（集成测试使用独立 PostgreSQL 测试容器，未运行真实智能体）：

- `go test ./internal/service -count=1`：TaskService 归因、队列和既有 service 回归。
- `go test -race ./internal/service -run '^TestWorkflow' -count=1 -v`：执行轮次及派工集成测试，包含权限撤销、无管理员调用绕过、事务回滚、重复投递和领取屏障。
- `go test ./internal/service -run '^TestWorkflowDispatch' -count=1`：补充 issue 创建活动记录后的针对性回归。
- `go test ./cmd/server -run '^TestWorkflowExecution' -count=1`：确认 server 包编译接线；该包没有同名用例，不能视作 HTTP 启动链路验收。
- `go vet ./internal/service ./cmd/server ./pkg/db/generated`。

下一步是执行配置与启动事务接入，再补齐服务端结果提交、产物版本和审核推进；本轮未完成整个 P1。

## 第四轮：执行配置与启动接线

2026-09-09 新增：

- 阶段定义／Run 副本保存 `execution_mode` 和 `executor_id`，默认 `manual`，与审核模式分离。`agent` 必须提供工作区内的执行者及 `output_spec.contract`，完成规则使用 `validated_result`。
- Run 保存 `inputs` 对象。自动首阶段启动时在创建 Run 的同一事务内固定输入、创建轮次和派工意图；缺少必需输入会整体回滚。手动阶段进入自动阶段时同样事务派工。
- `input_spec.contract` 可校验结构化输入。产物输入绑定尚未实现，因此包含产物绑定要求的输入契约明确拒绝，不能把缺少绑定当作校验成功。
- 旧阶段切换接口不能绕过自动阶段结果验收。已有轮次的 Run 暂不允许普通计划编辑，响应中的 `can_edit` 同步关闭；显式返工仍需实现。
- packages/core 已增加响应 schema 与请求类型；旧响应缺失执行模式时保持 manual，未知模式保留原值，格式错误显示 unknown，不误降级成手动行为。
- Web/Desktop 共用的 SOP 继承编辑器和 Run 计划表单保存执行者与契约，不因修改文字描述而丢失它们。本轮未增加执行配置选择器和 Run 输入表单，配置／输入目前由 API 提供。

输入契约示例为 `input_spec: {"contract":{"schema_version":"silieco.stage/v1","data":{"type":"object","required":["goal"],"properties":{"goal":{"type":"string"}}}}}`；启动请求传 `inputs: {"goal":"生成报告"}`。输出契约格式与本地预检一致，放在 `output_spec.contract`。这里只说明已实现的配置／启动协议，不代表服务端产物验收已经可用。

本轮还修正了共享 Run 表单测试缺少 MCP 查询 mock 的问题，以及看板刷新按钮中 TypeScript 报告的恒真条件。执行配置与启动仍是 P1 的一部分；产物提交、不可变修订、服务端验收、版本交接和完整审核推进尚未完成。

第四轮验证通过：

- 独立 PostgreSQL 中的 handler/service SOP 测试与竞态检查：`go test -race ./internal/handler ./internal/service -run '^TestWorkflow|^TestNormalizeWorkflow' -count=1`。
- `go test ./pkg/workflowcontract`、`go vet ./internal/handler ./internal/service`。
- packages/core 测试全量实际执行：109 个文件、1,173 个测试通过，包含缺失／异常执行字段解析。
- 共享 Run 计划表单测试通过，覆盖保留未来阶段的执行者、完成规则和输出契约。
- SOP 设计器原有 10 个测试通过，另增“继承自动 SOP 时保留执行者和契约”用例通过。一次并行运行中的 5 秒超时在单文件重跑时未复现。
- `pnpm --filter @silieco/views typecheck` 通过。
- 迁移 277 在独立测试库执行 up → down → up，确认原有手动默认值仍可插入。
- `git diff --check` 通过。

## 第五轮：结果提交与不可变产物修订

2026-09-09 新增：

- 迁移 278–290 保存执行结果、阶段产物身份、产物修订和上传记录。结果通过 `expected_revision` 做并发检查；每个结果和产物修订分别保留，旧版本不覆盖。
- `GET /api/workflow-instances/{id}/executions` 返回轮次身份、当前结果版本及冻结配置／输入；`POST .../executions/{executionId}/results` 接收 `{expected_revision, submission:{data,artifacts}}`；`GET .../results/{revision}` 读取结果数据。
- 服务端从执行快照读取契约，检查提交者是 Run 发起者或绑定的执行者／主任务。提交前后均检查 Run、阶段、计划修订、主 issue、执行者权限及结果版本；上传期间的取消和并发提交不能提交过期结果。
- 附件必须位于同一工作区且属于主 issue、其评论或主 task。服务端仅从托管存储读取，核对真实字节数与 SHA-256，再检查 UTF-8 文本或 JSON 格式。单文件上限 16 MiB，空文件拒绝；目前支持纯文本、Markdown、CSV 和 JSON，JSON 拒绝重复键。文件格式校验不等于领域正确性或领域产物 schema 验证。
- 每个产物修订保存独立存储副本；删除或修改原附件不改变已提交修订。网络上传发生在两段短数据库事务之间；上传前持久化记录，最终提交时再检查身份与状态，并将产物修订、结果、上传状态和轮次状态一起提交。
- 上传失败或业务提交冲突后保留清理记录。后台 Worker 删除过期／放弃的对象，保留两天的删除记录并定期重试，覆盖上传超时后对象延迟落盘的情况。项目／工作区删除在事务中删除结果元数据并保留存储清理记录。
- 已保存结果进入 `awaiting_review`，不自动标记阶段成功，也不绕过现有自动阶段的人工 transition 限制。接口尚未返回产物下载地址，尚未连接前端或提交 CLI。

已验证：独立 PostgreSQL 中迁移 278–290 的 up → down → up；契约内容、结果副本、旧版本冲突、并发提交、附件篡改／越权、上传失败及延迟对象清理、上传期间取消 Run 的测试；已有 SOP handler/service 竞态回归通过。测试只使用隔离数据库和内存存储，没有执行真实智能体。

仍需完成的关键路径是产物读取与领域校验、任务完成屏障、审核决定绑定精确结果版本、输入产物版本交接及自动推进。返工、暂停恢复、技能修订／评测和真实完整八阶段验收仍未完成，不能将本轮视为整个计划或 P1 已完成。

第五轮最终检查通过：

- `go test -race ./internal/service ./internal/handler ./pkg/workflowcontract ./cmd/server -run '^TestWorkflow|^TestNormalizeWorkflow|^TestBuiltin' -count=1`。service 和 handler 的数据库用例实际执行；server 包仅完成编译，没有匹配用例。
- `go test -race ./internal/service -run '^TestWorkflowResultDatabaseFailure' -count=1`：产物修订插入失败时，结果、产物身份、版本指针和状态一并回滚；移除故障后可用原版本号重试。
- 智能体主任务身份与项目删除后的产物清理测试通过。
- `go vet ./internal/service ./internal/handler ./pkg/db/generated ./pkg/workflowcontract ./cmd/server`。
- `git diff --check`。

## 第六轮：精确产物读取与 CLI 提交

2026-09-09 新增：

- 结果 API 返回产物清单，包括产物稳定身份、修订身份、版本号、类型、摘要、字节数和受认证保护的读取路径；不暴露内部存储键。
- 下载路径同时绑定工作区、Run、执行轮次、结果版本和产物修订。返回前重新核对真实大小和摘要，内容错误返回冲突，存储读取失败返回网关错误，不回退到原附件。文件使用附件下载、禁止类型嗅探和私有不缓存响应。
- CLI 新增 `workflow executions`、`workflow submit`、`workflow result` 和 `workflow artifact`。提交必须显式传 `--expected-revision`；读取必须传正整数 `--revision`。下载要求新目标文件，失败清理临时输出，不覆盖已有文件。
- 内置工作技能及来源映射同步更新。审核者现在可读取结果的精确产物版本；审核决策、完成屏障和自动推进仍需继续实现。

验证通过：

- 隔离 PostgreSQL 中 `go test -race ./internal/handler -run '^TestWorkflowStart' -count=1`，包含产物清单、精确版本下载、各层路径错配拒绝、内容篡改拒绝及对象缺失处理。
- `go test ./cmd/silieco -run '^TestWorkflow' -count=1`，包含离线预检、认证 API 请求、提交载荷、显式版本要求、下载内容和覆盖拒绝。
- `go vet ./internal/handler ./cmd/silieco ./cmd/server` 和 `git diff --check` 通过。

## 第七轮：历史调用归属修正

2026-09-09 核对验收屏障的关联基础时，发现阶段记录列表和总数仍通过 issue 当前阶段推导执行历史。现已修正：带执行轮次绑定的调用按冻结轮次归属查询；未绑定的手动调用沿用原有规则；无效轮次引用不降级为手动调用。移动 issue 不再重写自动执行历史。这覆盖总体计划 4.2 中的历史归属要求，尚未实现完成屏障或审核推进。

隔离数据库中的 `go test -race ./internal/service -run '^TestWorkflowTranscript' -count=1` 通过，实际验证移动前后列表／总数一致、手动调用归属及无效绑定拒绝回退。

## 第八轮：完成条件诊断

2026-09-09 新增统一完成条件检查和 `workflow readiness` CLI／HTTP 接口。单条 SQL 提供一致的读取快照，分别检查当前 Run／阶段／计划、已提交结果、主 issue 完成、主调用成功结束、其他阶段 issue 完成、阶段内活动调用。取消不会等价成功；重新打开 issue、新增未完成 issue、计划变化或仍活动的手动调用都会列出缺项。

响应明确标记 `scope: readiness_snapshot`，只提供可审核状态及缺项，不产生通过决定。该诊断还未构成原子验收屏障：正式审核／推进仍需在写事务中锁定并重新验证任务集合及状态。没有将当前诊断快照用于自动推进，也没有完成整个 P1/P2。

第八轮验证通过：

- 隔离数据库中的 `go test -race ./internal/service -run '^TestWorkflowCompletion' -count=1`，覆盖分别完成结果／issue／调用、取消、新增、重开、活动手动调用、计划失效及跨 Run／工作区查询。
- 隔离数据库中的 `go test -race ./internal/handler -run '^TestWorkflowStart' -count=1`，覆盖已提交结果仍返回未完成条件。
- `go test ./cmd/silieco -run '^TestWorkflow' -count=1`，包含 readiness 的认证请求路径。
- `go vet ./internal/service ./internal/handler ./cmd/silieco ./cmd/server` 与 `git diff --check`。

## 第九轮：现有手动阶段的交付检查

2026-09-09 修正手动 transition 将 cancelled 计作已交付的问题。审批通过现在要求阶段现有必需 issue 全部 done；检查移到取得 Run 锁后的事务内，对现有 issue 按稳定顺序加行锁并重新读取状态，锁持续到事务结束。取消、重开或其他未完成状态不通过。

这消除了原有事务外数量检查与已有任务状态写入之间的窗口，但并未封闭新增／移动任务引起的集合变化。任务创建、绑定、移动、批量更新和自动状态写入仍需统一执行事务协议，之后才能将完整屏障用于自动验收。空手动阶段规则与显式豁免协议也尚待实现；本轮没有宣称完成自动验收。

第九轮验证通过：隔离数据库中的 `go test -race ./internal/handler -run '^TestWorkflow' -count=1`（包含取消必需任务的审批拒绝），以及 `go test -race ./internal/service -run '^TestWorkflowStageDelivery' -count=1`（锁持有期间阻止状态写入，释放后可写）。`go vet ./internal/handler ./internal/service ./pkg/db/generated` 和 `git diff --check` 通过。

## 第十轮：专用绑定／解绑接口的事务锁

2026-09-09 新增共享任务变更锁定方法，按工作区 → 有序项目 → 有序 Run → issue 的顺序取得锁，并检查等待期间 issue 归属或 Run 项目是否变化。专用 SOP attach/detach 已调用该事务服务；关闭／归档的来源或目标 Run 不允许继续改动成员关系。对跨 Run 移动，双方按相同排序取得锁。

这使专用成员关系接口与现有阶段 transition 使用相同 Run 锁，但通用 issue 创建、更新、批量更新、删除及自动状态写入尚未全部接入。完整验收屏障与审核推进仍未完成。

第十轮验证通过：隔离数据库中的 `go test -race ./internal/service -run '^TestWorkflowMembership' -count=1`（Run 锁等待、关闭后拒绝、恢复绑定／解绑、相反方向跨 Run 并发移动），以及 `go test -race ./internal/handler -run '^TestWorkflow' -count=1`。`go vet ./internal/service ./internal/handler ./pkg/db/generated` 和 `git diff --check` 通过。

## 第十一轮：普通更新与批量更新接线

2026-09-09 普通 issue 更新和批量更新已使用同一 SOP 任务变更事务。即使客户端没有发送 expected_revision，也会比较处理该请求时读取的任务版本，避免等待期间任务归属或状态被修改后覆盖新数据；持有锁后检查来源／目标 Run 未关闭未归档、阶段属于目标 Run、项目与 Run 一致。单条冲突返回 409；批量更新保留原有部分成功行为，跳过冲突项。

新增 service 竞态测试覆盖等待 Run 锁、关闭后拒绝、项目不匹配、成功更新及陈旧快照拒绝。创建、删除、自动状态更新和调用队列变更仍需继续接入，原子验收和自动推进尚未完成。

第十一轮验证：

- 隔离数据库中的 `go test -race ./internal/service -run '^TestWorkflowGeneric' -count=1` 通过。
- `go test ./internal/handler -run '^TestWorkflow|^TestUpdateIssue|^TestBatchUpdate' -count=1` 通过。
- `go vet ./internal/service ./internal/handler` 和 `git diff --check` 通过。
- 全量 `go test ./internal/handler -count=1` 未通过，共 12 个失败，涉及运行时清理、MCP 配置、评论取消与私有运行时权限。用 Go overlay 仅将本轮两处更新接线还原为直接 Queries.UpdateIssue 后运行完整对照，得到相同 12 个失败，无新增失败。对照未修改工作区源码；不能据此声称全量 handler 测试通过。
- 本轮日志：`/tmp/silieco-workflow-handler-regression.log`；对照日志：`/tmp/silieco-workflow-handler-baseline.log`。

## 第十二轮：创建事务接入

2026-09-09 在现有 IssueService.Create 事务中将 SOP Run 查询改为共享父资源／Run 锁定。所有使用该服务的创建入口在分配任务编号、写入 issue 和依赖记录前，重新检查等待后的 Run 状态和项目／阶段归属。Run 关闭后不留下半成品任务；正常创建行为保留。

隔离数据库中的 `go test -race ./internal/service -run '^TestWorkflowIssueCreation|^TestIssueService' -count=1` 通过，其中新增创建锁用例实际执行，覆盖锁等待、关闭拒绝、无部分写入及重新开放后创建成功。`go vet ./internal/service` 通过。删除与自动状态写入尚待接入，完整原子验收尚未完成。

本轮 handler 定向回归 `go test ./internal/handler -run '^TestWorkflow|^TestCreateIssue' -count=1` 与 `git diff --check` 也通过；全量 handler 的 12 个既有对照失败仍按第十一轮记录，未宣称全量通过。

## 第十三轮：自动状态更新与单任务删除

2026-09-09 将 GitHub 合并 PR 后的完成写入、失败调用后的 todo 重置接入共享 SOP 锁定和陈旧版本检查。自动写入保留任务分配、项目及阶段；读取后任务发生变化时，不覆盖新版本。

单任务删除先锁定 SOP 成员关系，再在同一个事务中取消活动调用、清理关联 Autopilot 运行和删除任务。取消通知改在提交后发送；删除失败时调用状态一起回滚。关闭／归档 SOP 的任务不能通过该接口删除。批量删除和调用队列写入仍需进一步统一，完整验收及审核推进仍未完成。

第十三轮验证通过：

- 隔离数据库中的全量 `go test ./internal/service -count=1`。
- `go test -race ./internal/service -run '^TestWorkflowGeneric' -count=1`，包含自动更新保留归属和拒绝旧快照。
- `go test -race ./internal/handler -run '^TestWorkflowStart|^TestDeleteIssue|^TestWebhook_MergedPR' -count=1`，包含注入删除故障后 issue 与调用状态均保留的检查，以及合并 PR 回归。
- `go vet ./internal/service ./internal/handler` 和 `git diff --check`。

全量 handler 包此前的 12 个对照失败未在本轮重新宣称通过。

## 第十四轮：批量删除统一事务

2026-09-09 提取单条与批量删除共用的事务服务。每项删除在取得 SOP 锁后，一并处理调用取消、Autopilot 清理、附件引用和 issue 删除；只有提交后才返回可通知和存储清理的结果。批量仍逐项提交，失败项不影响成功项，关闭／归档 Run 的保护与单条删除一致。新增故障注入覆盖单条与批量删除回滚、关闭 Run 的批量拒绝，以及恢复后的批量重试。

调用队列写入与领取的完整阶段约束、原子审核验收和自动推进仍未完成。

第十四轮验证通过：隔离数据库中 `go test -race ./internal/handler -run '^TestWorkflowStart|^TestDeleteIssue|^TestBatchDelete' -count=1`；`go vet ./internal/service ./internal/handler`；`git diff --check`。批量故障、关闭保护和重试覆盖位于 WorkflowStart 所调用的删除回滚 helper 内，不以测试名称匹配数量代替实际覆盖。

## 第十五轮：手动阶段的领取屏障

2026-09-09 补充无执行轮次绑定调用的领取条件。普通任务／聊天沿用既有规则；手动 SOP 调用要求 Run 活跃、未归档、所处阶段为当前手动阶段，且 issue 不处于 backlog／done／cancelled。未来阶段的已排队调用等待进入当前阶段后领取。自动阶段的无绑定调用不能绕过执行轮次检查。查询对 Run 取得共享锁，与阶段变更互斥。

本轮解决领取边界，尚未把所有队列创建和重试入口统一绑定到轮次，也未完成审核任务派发、正式验收及自动推进。

第十五轮验证通过：隔离数据库中 `go test -race ./internal/service -run '^TestWorkflowManualQueue|^TestWorkflowDispatch|^TestClaimTask' -count=1`，覆盖新增阶段边界、原有派工／权限和队列并发；`go vet ./internal/service ./pkg/db/generated`；`git diff --check`。

## 第十六轮：技术重试的轮次与结果权限

2026-09-09 修复重试调用丢失 workflow_stage_execution_id 的问题。重试继承原业务轮次，调用 attempt 增加而 execution.attempt 不变；更新当前 main_task_id 与插入重试在同一 SQL 原子完成，历史由 retry_of_task_id 保留。锁定并核对父调用仍为当前主调用、Run／阶段／计划有效后才创建；过期父调用不能再创建第二条链。

提交过结果后失败的调用仍可技术重试，执行状态回到 queued，需再次提交结果才能回到 awaiting_review。被拒绝的重试不会回滚原调用的 failed 记录。针对性测试证明新调用可领取、可提交结果，完成条件以当前调用为准；注入插入错误时主调用指针回滚。

第十六轮验证通过：隔离数据库中的 `go test -race ./internal/service -run '^TestWorkflowRetry' -count=1`、全量 `go test ./internal/service -count=1`、`go vet ./internal/service ./pkg/db/generated` 和 `git diff --check`。完整审核、返工轮次和自动推进仍未完成。

## 第十七轮：普通受理人调用入队事务

2026-09-09 普通 assignee 调用入队已锁定 Run／issue 后验证阶段、关闭状态和受理人归属，并在提交后通知。自动阶段拒绝普通无绑定触发，改由派工与技术重试的绑定路径执行。归因和外部工具配置准备移到事务外，避免持有连接后再等待池中其他连接；插入前使用事务查询重新核对执行者和运行时。

新增测试覆盖 Run 锁等待、关闭拒绝、自动阶段拒绝、手动阶段正常入队及提交后可见通知；单连接池测试证明普通入队事务不依赖第二个连接。专用 mention／委派等入队入口尚待统一，审核和自动推进仍未完成。

第十七轮验证通过：隔离数据库中的全量 `go test ./internal/service -count=1`；`go test -race ./internal/service -run '^TestWorkflowOrdinaryEnqueue|^TestWorkflowDispatch' -count=1`；新增单连接池后的 `go test -race ./internal/service -run '^TestWorkflowOrdinaryEnqueue' -count=1`；`git diff --check`。

## 第十八轮：mention 与委派入队事务

2026-09-09 普通调用与显式 mention、线程父评论回复、小队 leader 调用统一使用入队事务。事务锁定 Run／issue 后验证成员归属、阶段与关闭状态，再核对目标 Agent 和运行时；归因及工具配置仍在事务前准备，通知仍在提交后发送。显式指定的 Agent 不必是 issue 受理人，leader、fresh session 与 handoff 信息继续保留。

新增 mention 测试覆盖成员受理的任务调用另一 Agent、Run 锁等待、关闭和自动阶段拒绝、单连接池以及通知时队列行已提交。验证通过：隔离数据库中的全量 `go test ./internal/service -count=1`（31.080s）、`go test -race ./internal/service -run '^TestWorkflow(Ordinary|Mention)Enqueue' -count=1`、`go vet ./internal/service` 和 `git diff --check`。

延迟 fallback 等专用队列写入尚未统一；正式验收、审核派工、自动推进、返工、技能迭代和完整八阶段验证仍未完成。

## 第十九轮：延迟 fallback 入队事务

2026-09-09 延迟 fallback 入队与普通／mention 调用共用 Run／issue 事务检查，拒绝关闭、归档和自动阶段的无绑定调用，并在插入前核对执行者运行时。保留 fire_at 与 escalation_for_task_id；创建后仍为 deferred，到期提升前不发 queued 通知。就绪检查原已将 deferred 计为活动调用，到期提升不会新增未计入的阻塞项。

新增测试覆盖锁等待、关闭／自动阶段拒绝、单连接池、延迟身份和无提前通知。针对性普通／mention／deferred 测试及 race 检查、`go vet ./internal/service` 通过。全量 service 回归唯一失败为内置 skill 正文超过 500 行；已将 SOP 详细说明移入 `references/sop-execution.md`，正文保留入口与关键约束，继续验证模板规则。

完整验收决策、审核和自动推进仍待实现；本轮未宣称所有调用状态变更已完成并发协议统一。

整理引用文档后，`go test ./internal/service -run '^TestBuiltinSkills' -count=1` 与 `git diff --check` 通过；未重复运行仅受文档长度影响的全量回归。

## 第二十轮：绑定审核快照的人工决策记录

2026-09-09 扩展现有 workflow_gate_decision，增加 execution_id、result_revision、review_step、basis_hash 和 basis_snapshot。新增 291 迁移定义整体一致性约束，292 为独立单语句并发唯一索引；未增加外键或级联。既有 Run／项目／工作区的决策清理继续覆盖这些记录。

WorkflowExecutionService 增加人工审核准备及决策记录服务。读取和提交均锁定 Run、必需 issue 及相关调用，重新检查完成屏障、冻结契约、已提交结果和产物清单，并验证配置中指定的人工审核人、当前成员关系和启动者执行权限。审核快照包含结果与产物版本、任务集合、调用身份／状态、执行配置和输入。提交必须携带相同结果版本与快照摘要；并发相同请求返回同一决策，已有决策不允许原地改写。交付变更后摘要变化，旧决策不能被匹配到新的审核依据。

本轮仅实现服务与持久化基础，尚未新增 API／CLI／UI，也未将批准解释为执行成功。none 自动验收、agent／hybrid 审核、原子推进和明确版本输入交接仍需接入；人工服务明确拒绝 hybrid，不能跳过智能体审核步骤。

验证通过：隔离数据库中 `go test ./internal/service -run '^TestWorkflowHuman' -count=1`、`go test -race ./internal/service -run '^TestWorkflowHuman' -count=1`、全量 `go test ./internal/service -count=1`（33.718s）、`go vet ./internal/service ./internal/handler ./pkg/db/generated`、`git diff --check`。测试覆盖不完整交付／错误审核人拒绝、并发幂等、旧结果版本、任务内容变化后旧摘要失效、插入故障回滚、调用锁等待与取消、关闭 Run 和 hybrid 绕过保护。291／292 在独立测试 schema 中完成 up → down → up。

## 第二十一轮：人工审核 API 与共享客户端

2026-09-09 新增执行轮次的 GET review 与 POST decisions。读取返回结果版本、审核摘要及经过筛选的任务／调用列表，不暴露内部快照、存储键或调用配置；提交严格限制请求字段、大小、结果版本和摘要格式，审核人来自认证上下文，携带 Agent／调用身份的请求不能降级成人工审核。现有决策响应增加轮次、结果版本、审核步骤和摘要，保留历史决策关联信息。

共享 ApiClient 添加读取与记录方法，均使用 parseWithFallback 和 schema。异常任务／调用列表使整份审核响应失效，不能因降级为空列表而保留可用审批凭据。新增类型、导出和 schema／客户端异常响应测试。API 已可调用，CLI 与审核 UI 尚未接线；批准仍不等于执行成功，自动推进和版本交接未完成。

验证通过：隔离数据库中 `go test ./internal/handler -run '^TestWorkflowStart' -count=1`（包含新增人工审核 API helper：未完成交付、伪造身份、非法字段、读取安全、幂等、旧结果和跨 Run 拒绝）、`go test -race ./internal/service -run '^TestWorkflowHuman' -count=1`、`go vet ./internal/handler ./internal/service`；共享 API/schema 149 项测试、core typecheck、`git diff --check`。类型检查发现新增类型未从总入口导出，修复后通过；未重复宣称全量 handler 基线无失败。

## 第二十二轮：CLI 人工审核与共享查询入口

2026-09-09 增加 `workflow review` 和 `workflow decision`。决策要求显式传入 result-revision、basis-hash、outcome，可附 note；本地验证 UUID、正整数版本及小写摘要。服务端冲突直接返回，CLI 不重新获取摘要或重发批准。命令不会覆盖 Agent 身份来冒充人工。

共享 React Query 增加审核查询和决策 mutation，缓存按工作区、Run、执行轮次区分。审批不进行乐观更新，明确禁用自动重试，失败与成功均使当前工作区的 workflow 查询失效；异常 API 响应不被视为成功决策。统一导出请求类型供客户端与 mutation 复用。Web／桌面审核界面、正式验收推进和版本交接仍待实现。

验证通过：`go test ./cmd/silieco -run '^TestWorkflow' -count=1`、`go vet ./cmd/silieco`；审核查询／mutation 与 API 客户端 57 项测试、core typecheck、`git diff --check`。测试验证 CLI 409 仅发出一次决策请求，错误参数不联网；mutation 在全局重试开启时仍不重试冲突或异常响应，且不会使另一工作区缓存失效。

更新操作说明后，内置 skill 模板测试 `go test ./internal/service -run '^TestBuiltinSkills' -count=1` 通过。

## 第二十三轮：共享交付审核界面

2026-09-09 自动阶段在现有 Run 面板中改用执行轮次审核控件，读取执行列表、明确结果版本、产物清单和人工审核依据。Web／桌面共用 packages/views 实现，未引入平台路由依赖。产物通过认证后的版本接口下载，不直接打开服务端返回的 URL。指定审核人勾选已审核本版结果、产物及必需任务后，才能记录通过／驳回；摘要变化时重新挂载表单并清除确认。异常结果不提供决策按钮，提交失败后禁止在原表单连续重试。

共享 API 客户端与 schema 新增执行列表、结果版本读取及产物字节获取；异常产物列表使结果无效，不能降级为空清单。决策 mutation 额外核对响应中的 Run、轮次、结果版本、摘要、审核步骤及 outcome，防止结构合法但身份不匹配的响应显示为保存成功。手动阶段界面也修正为取消的必需任务阻止批准，与服务端一致。

当前批准只记录审核决策，不宣称自动进入下一阶段。自动验收、版本交接、agent／hybrid 审核、返工及技能迭代仍未完成。UI 已通过 DOM 交互测试，尚未做真实浏览器的视觉验收。

本轮已通过：共享界面 10 项测试（包含刷新摘要后重置确认、精确提交、失败保护和 cancelled 阻塞）；API／查询／mutation 61 项测试；内置 skill 模板测试；首轮 core／views typecheck 和 `git diff --check`。新增测试文件及最终响应身份校验后的类型检查继续运行，以下补充最终结果。

补充测试后，共享界面覆盖增至 14 项，包含 none／agent／hybrid 不显示人工决策及精确产物下载调用。最终 core typecheck 已通过；views 补充检查定位并修正了测试夹具的显式转换和审核模式字面量类型，继续复查。未因类型检查耗时重启仍在运行的进程。

最终 views 类型复查通过（`tsc --noEmit --incremental --tsBuildInfoFile /tmp/silieco-sop-review-views.tsbuildinfo`）；缓存仅放在 /tmp。两端接线已核实：Web 的 workflows 列表／详情页及桌面 routes.tsx 均使用同一 WorkflowsPage，该页面内的 StageReviewControl 已接入新控件。最终 `git diff --check` 通过。

## 第二十四轮：显式输入绑定与冻结值解析

2026-09-09 契约包增加 `silieco.input-bindings/v1`，将 Run 输入或前序阶段已验收结果中的明确路径映射到输入契约声明的字段。目标唯一、来源类型受限、路径必须显式给出，支持对象转义键与数组索引；缺失值、非法路径、未知配置和类型不匹配均拒绝。保留 JSON 数字精度。同一前序阶段只调用一次来源解析器，避免多个字段混入不同结果版本。

生成的 `silieco.input-snapshot/v1` 保存投影值、Run／计划版本、输入路径及执行轮次／结果身份／结果版本／来源摘要。解析器不自行判定业务验收；服务器回调必须在 Run／依赖锁下完成权限、前序关系与当前验收有效性检查，不允许直接把客户端声明的 accepted 当成依据。

本轮为纯契约解析基础。阶段配置字段、服务端已验收版本查询、执行轮次输入格式和推进事务尚未接入；产物与项目资源绑定尚不支持。不能将此内部格式写入任意 stage JSON 后宣称已交接。

验证通过：契约包全量 `go test ./pkg/workflowcontract -count=1`、`go test -race ./pkg/workflowcontract -count=1`、`go vet ./pkg/workflowcontract`；区分绑定配置／输入快照版本标签后再次运行契约包全量测试通过。测试覆盖路径转义、数组边界、大整数、重复／未声明目标、重复 JSON 键、缺少显式路径、伪造 accepted、无版本来源、来源拒绝传播以及单次解析固定版本。`git diff --check` 通过。

新增引用说明后的内置 skill 模板测试通过。

## 第二十五轮：阶段配置与 Run 输入绑定接线

2026-09-09 迁移 293 为模板阶段和 Run 阶段增加 input_bindings 对象字段。API 请求、响应、模板创建、Run 复制及计划编辑均保存该字段；非空配置必须有输入契约，来源阶段必须在当前阶段之前。空对象表示未配置投影。设计器／Run 编辑器保留已有绑定，设计器保留既有 stable_key，新阶段生成避开已用标识的键，避免破坏来源引用。

创建执行轮次时，run_input 从锁定 Run 的 inputs 读取，忽略调用方提供的替代值；生成带 data 和来源证据的 silieco.input-snapshot/v1，并冻结绑定配置。再次创建同一执行轮次直接返回原快照，不因当前 Run 输入消失而重新解析。输入缺失或来源不可用时，当前实现回滚执行创建和派工意图；尚未改成持久化 blocked 轮次。

stage_result 的配置和前序依赖校验已保存，但执行端仍拒绝解析，等待正式验收查询；不能将普通提交或审核记录冒充已验收版本。产物／项目资源绑定、正式验收后的版本交接、自动推进和配置表单仍未完成。

已通过：迁移 293 独立 schema 的 up → down → up；契约包全量测试；全量 service 回归（36.532s）；输入绑定／并发恢复的 race 检查；Go vet；core schema 95 项测试与 core typecheck。前端设计器／Run 计划 12 项测试在本次使用 15 秒用例限时复测通过；首次并行检查下热仿真 POC 用例超过默认 5 秒，未改其业务断言。启动集成测试已改为核对版本化快照的 data.goal、版本标签和派工意图，继续复测；views 类型检查仍在运行。

最终启动／配置／计划相关 handler 复测通过（5.780s），views 增量类型检查通过。`git diff --check` 通过。


## 第二十六轮：人工审核后的原子验收与版本交接服务

2026-09-09 迁移 294 增加执行轮次 acceptance。内部 AdvanceHumanReviewedExecution 在 Run 锁下重新校验审核人、结果版本、完整交付依据及既有通过决策，将确切结果身份、版本和依据摘要固定为验收记录；同一事务更新 Run，并为下一自动阶段冻结输入和创建派工意图。末阶段验收完成 Run，相同决策重试幂等。

stage_result 现在由服务端查询前序已验收轮次，并重算交付依据，校验结果身份与版本后解析。普通 issue 创建、修改、移动、删除和共享入队路径拒绝改写 succeeded 阶段。验收本身的状态、时间和 acceptance 字段不进入交付摘要，避免内部状态转换令刚验收的来源失效。

本轮仍是内部服务：人工决策 API 继续只记录审核，尚未调用推进服务；后台自动推进也未接入。输入错误目前回滚整个推进事务，尚未持久化 blocked 轮次。none／agent／hybrid、显式返工与依赖失效、特殊队列写入路径审计、配置界面及 skill 评测仍待完成。

验证通过：两阶段交接及最终完成；末尾派工意图写入失败回滚验收和下一轮次；审核后交付变更拒绝推进；并发重试只验收一次；已验收 issue 重开／移动拒绝。全量 service 回归通过（33.462s）；新增并发用例与人工审核用例的 race 检查通过（6.913s）；Go vet、内置 skill 模板测试、迁移 294 独立 schema 的 up → down → up、git diff --check 通过。未声称 handler 全量或真实浏览器验收通过。


## 第二十七轮：人工决策接口接通原子推进

2026-09-09 人工决策 API 改用 DecideHumanExecution，在同一 Run 事务中记录通过决策、验收当前结果、切换阶段并建立下一轮次输入和派工意图。末阶段完成 Run。失败回滚包含本次新建的批准记录；已验收后的相同请求返回原决策，不创建重复派工，修改意见的重试被拒绝。驳回仍记录决策而不推进，返工尚未接入。提交成功后发布工作流更新事件，共享界面按钮改为“通过并推进”。请求及响应结构未变。

服务测试已验证最终 Run 更新失败时批准记录回滚，以及并发重复批准只保留同一决策。全量 service 回归通过（38.535s）、新增原子决策 race 检查通过（6.093s）、Go vet 通过；共享审核控件 8 项测试通过。接口集成测试补充校验成功响应确实完成 Run 和执行轮次。初次运行暴露共享测试夹具的下载步骤会修改产物契约，已恢复审核先于下载，并在审核断言后恢复夹具供独立删除测试使用，继续复测。

其余计划继续保留：后台 none 模式自动推进、agent／hybrid 审核、blocked 输入诊断与恢复、显式返工和下游失效、特殊队列写入审计、配置表单、真实浏览器验收、P0 样板及 skill 评测迭代。

最终启动／结果／人工审核接口集成复测通过（5.160s），包括验收完成状态与重复请求身份断言。内置 skill 模板测试通过（2.622s）；git diff --check 通过。


## 第二十八轮：none 模式后台自动验收

2026-09-09 新增后台验收 worker，启动时及每两秒扫描当前有效计划中 awaiting_review 且冻结 gate 为 none 的轮次。查询按执行 ID 分页，未就绪的早期轮次不会占满固定首批而饿死后续轮次。每次验收在 Run 锁下重新检查当前阶段、计划版本、状态、成员资格、执行者可用性与调用权限，再复用完整交付依据校验及原子交接逻辑。完成后发布工作流刷新事件，不伪造人工审核记录。

轮次自身的持久化状态用于重启恢复和并发去重；单轮次处理有 30 秒期限。none 之外的 gate 不进入该路径；缺少结果、未完成或取消的必需任务、暂停 Run 均不能通过。下一阶段输入缺失仍回滚推进，持久化阻塞及恢复后续实现。

新增集成测试已通过（3.685s），覆盖上述阻塞条件、两独立 worker 并发扫描、重复扫描与最终完成，以及没有人工决策行。service／server 的 Go vet 已通过；全量 service 与 race 检查继续运行。agent／hybrid 审核、返工、配置界面、P0 样板与 skill 评测等目标保持未完成。

最终全量 service 回归通过（36.958s，包含内置 skill 模板测试），none 并发扫描 race 检查通过（5.224s）。已核实共享实时同步收到 workflow_instance 事件会失效对应工作区的 workflow Query 缓存。git diff --check 通过。


## 第二十九轮：持久化输入阻塞与查询诊断

2026-09-09 输入缺失、绑定来源不可用或值不满足契约时，创建 blocked 轮次，保存 input_unavailable 和原因，输入快照为空对象，不创建派工意图。冻结配置保留，重复创建返回同一阻塞轮次。配置错误及数据库错误仍回滚；来源数据库错误不伪装成业务阻塞。

前序验收后的交接允许下一阶段进入上述阻塞状态，前序验收与 Run 阶段切换保留。启动 Run 缺少必需输入也创建可查询的阻塞轮次。执行列表 API 新增 failure_code／failure_detail；客户端 schema 将缺失或异常诊断字段降级为 null。尚未实现修正输入和恢复同一轮次的入口，也未增加阻塞诊断的专用界面。

已通过：全量 service 回归（39.713s）、输入绑定 race（6.602s）、Go vet、启动集成测试（5.103s）、core schema 96 项测试。测试覆盖阻塞重试保持轮次、零派工意图、前序验收保留与后序阻塞。补充公开诊断接口断言后的最终 handler 复测和 core 类型检查继续运行。agent／hybrid、返工、P0 样板、skill 评测及完整 UI 验收仍未完成。

最终复查修正了新增 Go 测试缺少 strings 导入及 TypeScript 测试数组访问的严格类型错误；handler 启动／公开诊断集成复测和 core typecheck 均通过。内置 skill 模板测试通过（3.186s）。git diff --check 通过。


## 第三十轮：同轮次输入恢复服务

2026-09-09 提取共享输入解析逻辑，初始创建与输入阻塞恢复使用相同契约校验。内部 ResumeInputBlockedExecution 仅允许当前有效 Run 的发起成员恢复 input_unavailable 且尚未产生主 issue／调用／结果的轮次，重新检查执行者权限。恢复读取原轮次冻结的契约、绑定及阶段位置，当前计划修改不能降低原契约要求。成功后同一事务恢复 pending、保存输入快照、清除诊断并创建派工意图；重复及并发请求保持同一轮次和唯一派工。

本轮不改写 Run 输入，也未提供恢复 API／CLI／UI。输入修正需处理对已验收阶段依据及下游来源的影响，不能直接更新 Run.inputs 后宣称完整恢复流程已完成。

输入相关测试通过（4.674s）、全量 service 回归通过（37.862s）、初版并发恢复 race 检查通过（4.740s）、Go vet 通过。已补充派工意图写入失败时恢复事务回滚为 blocked 的断言，最终 race 与内置 skill 模板测试继续运行。配置界面、agent／hybrid、返工、P0 样板及 skill 评测等原计划项继续保留。

补充回滚断言后的恢复 race 检查及内置 skill 模板测试通过，最终 git diff --check 通过。


## 第三十一轮：输入修正与已消费值保护

2026-09-09 内部 CorrectBlockedExecutionInputs 服务使用预期旧输入做 JSONB compare-and-swap，输入更新与同轮次恢复／派工在一笔事务提交。验证失败回滚输入，旧输入版本拒绝覆盖。修正前检查其他轮次冻结输入：显式 run_input 绑定保护其实际投影字段；无绑定的输入快照保护整个输入对象。已消费值变更要求后续显式返工，不能通过输入修正绕过。

审核依据改为 silieco.review/v3，保留冻结执行输入，不再包含无关的当前 Run 输入整包。这样新增后续字段不会使未受影响的前序验收失效；输入写入路径承担已消费值保护。非正式数据库直接改写仍不属于支持的产品操作。

本轮为内部服务，输入修正与恢复 API／CLI／UI 尚未接入。已通过输入／验收／审核相关 service 与契约测试（7.570s／3.958s），全量回归、race 和 vet 继续检查。原计划其余目标保持未完成。

最终全量 service／契约回归通过（39.688s／1.379s），相关 race 检查通过（10.323s／5.833s），Go vet、内置 skill 模板测试（2.583s）及 git diff --check 通过。


## 第三十二轮：输入修正 API 与共享客户端

2026-09-09 新增 POST /api/workflow-instances/{run}/executions/{execution}/inputs，要求发起成员身份，拒绝智能体／调用身份。严格校验完整 expected_inputs 和 inputs 对象，限制请求体，使用现有修正／恢复事务。成功返回轮次、Run、attempt 和 pending 状态，并发布工作流刷新事件。旧输入、非法契约值或重复提交返回冲突，不刷新凭据后自动重试。

共享客户端新增 schema 解析，异常状态、缺字段或 Run／轮次身份不匹配返回 null，不显示为恢复成功。CLI、查询 mutation 和界面控件仍未接入。

接口集成通过（6.261s），覆盖智能体身份拒绝、非法值、旧输入、失败零派工、成功同轮次及重复提交不再派工。客户端 61 项测试通过；handler／server Go vet 通过。类型检查首次读到缺失的新类型导出，已补全并继续复查。内置 skill 文档及源码映射已更新。

最终 core typecheck 通过，内置 skill 模板测试通过（2.953s），git diff --check 通过。


## 第三十三轮：共享输入恢复界面与 mutation

2026-09-09 Web／桌面共用的执行面板显示输入阻塞原因，当前有效轮次的 Run 发起成员可打开“修正输入”，编辑完整 JSON 输入对象并保存恢复。表单保存打开时的旧输入，变化后重新挂载；非法 JSON 禁止提交，失败后禁用再次提交并要求重新加载确认。成员身份、当前阶段、计划版本、Run 状态及阻塞类型共同约束按钮显示。

共享 mutation 验证恢复响应身份，关闭自动重试，不做乐观输入更新，仅失效指定工作区的 workflow Query 缓存。CLI 尚未接入；本轮 UI 为 DOM 验证，尚未完成真实浏览器视觉验收。

已通过 mutation 4 项测试、输入恢复／交付审核 UI 11 项测试。首轮失败来自测试 beforeEach 返回 mock 被当作清理函数调用，修正后通过。core／views 类型检查及内置 skill 模板测试继续运行。agent／hybrid、返工、配置表单、P0 样板和 skill 评测仍待完成。

最终 core／views 类型检查均通过，内置 skill 模板测试通过（2.641s），git diff --check 通过。顺带修正 CLI decision 帮助中残留的“不会推进”描述；命令行为未改，批准按已接入的原子验收推进执行。


## 第三十四轮：CLI 输入修正命令

2026-09-09 增加 workflow correct-inputs <run-id> <execution-id> --expected-inputs <file> --inputs <file>，显式读取完整旧／新输入对象，每文件最多 1 MiB。本地拒绝非法 JSON、重复键、数组与 null，ID 保持 canonical UUID 校验。发送一次 POST 到输入修正接口，409 不重新获取输入、不重试。

workflow 命令组测试及 CLI Go vet 通过。新增测试验证确切旧／新载荷、缺失参数不发请求、非法文件不发请求和冲突不重试。内置 skill 说明与源码映射已同步。输入修正 API、共享界面与 CLI 均已接通，但真实浏览器验收、agent／hybrid 审核、返工、阶段配置表单、P0 样板及 skill 评测仍未完成。

最终 workflow CLI 测试通过（2.694s），内置 skill 模板测试通过（3.079s），git diff --check 通过。


## 第三十五轮：智能体审核身份冻结

2026-09-09 新建执行轮次时解析 gate 中 @self 为该轮次实际执行者，写入冻结 gate.decider 和 decider_type=agent。agent／hybrid 要求审核智能体在本工作区存在、未归档且发起成员可调用；hybrid 还要求显式有效的 human_decider。无效审核身份阻止新轮次创建。重复创建直接返回原快照，不跟随当前计划修改审核人；空 gate 类型归一为 none。

新增服务测试通过（5.361s），覆盖两类 gate 的 @self 解析、计划变化后快照保持、缺失／非法／不存在的审核人和 hybrid 人工审核人校验。Go vet 通过，全量 service 回归及内置 skill 模板测试继续运行。

本轮是智能体审核的身份前置约束，尚未实现版本绑定的审核任务、审核调用派发和结构化决策接收；agent／hybrid 不能视为可用。返工、配置表单、浏览器验收、P0 样板及 skill 评测仍未完成。

最终全量 service 回归通过（41.828s），内置 skill 模板测试通过（3.077s），git diff --check 通过。


## 第三十六轮：版本绑定的智能体审核请求

2026-09-09 迁移 295 增加 workflow_review_request，迁移 296／297 分别以单语句并发索引约束请求 ID 和执行轮次／依据摘要唯一性。无外键。项目／工作区删除 SQL 加入显式清理，测试夹具也清理请求记录。

内部 PrepareAgentReview 在 Run 锁下校验当前轮次、冻结 gate、执行者及审核者权限，hybrid 同时检查人工审核人仍为成员。交付完成后保存明确结果 ID／版本、审核人及完整依据摘要／快照。重复准备返回同一请求；交付变化创建新请求并使旧 pending／queued 请求失效。尚未接入后台派发、队列绑定、结构化审核决策或 agent／hybrid 验收。

相关集成测试通过（5.346s），覆盖 agent／hybrid、未完成交付拒绝、并发去重、结果身份固定、证据变化失效及归档审核者拒绝。迁移 295–297 在独立 schema 中 up → down → up 通过。全量 service 回归、race 与 Go vet 继续检查。原计划其余项保持未完成。

最终全量 service 回归通过（43.180s），智能体审核准备 race 检查通过（6.733s），Go vet、内置 skill 模板测试（2.743s）及 git diff --check 通过。


## 第三十七轮：审核调用角色与队列隔离

2026-09-09 迁移 298 为队列调用增加 workflow_review_request_id，并以 CHECK 禁止同时绑定执行轮次和审核请求；审核请求保存 task_id。迁移 299 的独立并发唯一索引保证一份审核请求只有一个调用。绑定 SQL 在同一语句更新调用与请求状态，要求 reviewer、主 issue 和 queued 状态匹配；调用方必须持有 Run 锁并重查证据。

交付完成条件及审核依据排除审核调用，避免创建审核任务本身改变待审核的交付。普通领取及执行技术重试路径排除审核调用，专用领取和重试逻辑尚未接入，因此当前不会启动审核智能体。

绑定／依据测试通过（4.792s），覆盖审核角色互斥、调用绑定后交付摘要保持。迁移 298–299 独立 schema 的 up → down → up 通过，Go vet 通过。全量回归及 race 已运行，补充“审核调用丢失 issue 关联也不能进入普通领取”断言后继续最终检查。审核派发、决策接收、返工、配置表单、浏览器验收和 skill 评测等仍未完成。

最终全量 service 回归通过（44.194s），审核／领取 race 检查通过（7.485s），补充失去 issue 关联的断言及内置 skill 模板后的最终 race 检查通过（7.448s），git diff --check 通过。


## 第三十八轮：智能体审核事务派发服务

2026-09-09 内部 DispatchAgentReview 在事务外准备调用归属和运行配置，随后复用事务内审核准备逻辑，在 Run 锁下重新核对证据、权限及运行时。创建新会话的审核调用，handoff 明确请求 ID、Run、执行轮次、结果 ID／版本及依据摘要，提示不得重写交付。主 issue 负责人不变。

队列调用创建与审核请求 task_id／queued 状态同一事务提交，重复及并发派发复用绑定调用。请求已失效则拒绝；绑定失败回滚调用，避免孤立队列记录。提交后才通知队列和守护进程。后台调用接线、审核领取和决策入口尚未完成，当前审核调用仍不能被普通领取路径执行。

派发相关测试通过（5.646s），覆盖并发去重、失效拒绝、新会话和精确版本上下文。Go vet 通过。补充绑定失败后零孤立调用断言，全量 service 与 race 继续运行。原计划中的返工、配置表单、浏览器验收、P0 样板与 skill 评测等仍未完成。

最终全量 service 回归通过（49.185s），审核准备／派发 race 检查通过（7.632s），内置 skill 模板测试通过（3.543s），git diff --check 通过。


## 第三十九轮：智能体决策与混合审核屏障

2026-09-09 内部 RecordAgentDecision 验证审核请求、审核者及绑定调用、结果版本与依据摘要，只接受 running／completed 审核调用。记录明确 approved／rejected 决策和 agent 审核步骤，重复记录幂等且不允许改写 outcome／意见。运行中的审核通过只记录，不推进。

AcceptAgentReviewedExecution 要求审核调用 completed、同一证据仍有效且智能体通过，再复用原子验收与交接。重复验收返回已完成 Run。hybrid 不允许走该自动验收入口；人工审核依据必须先取得同一摘要的智能体通过并确认审核调用成功结束，最终仍经人工决策事务推进。

服务测试通过（7.646s），覆盖 queued／执行调用拒绝、错版本拒绝、不可改写决策、未结束审核不推进、agent 完成与重复验收，以及 hybrid 必须人工对同一依据通过。Go vet、全量 service 与 race 继续运行。智能体决策 API、专用领取和后台接线尚未完成，旧已验收阶段上的智能体决策重试仍返回冲突；UI 对 hybrid 的放行也未接线。返工、配置表单、浏览器验收、P0 样板和 skill 评测继续保留。

最终全量 service 回归通过（47.567s），相关 agent／human 决策 race 检查通过（9.097s），Go vet、内置 skill 模板测试（5.565s）和 git diff --check 通过。


## 第四十轮：智能体审核读取与决策 API

2026-09-09 在 Run／执行轮次下新增 reviews/{request} 读取及 decisions 提交接口。复用 resolveActor 验证智能体身份，并要求绑定的 X-Task-ID；校验请求所属工作区、Run 和执行轮次。读取服务在 Run 锁下重查请求及当前证据，仅返回必要身份、结果版本和摘要，不暴露内部 basis_snapshot。

决策接口严格限制请求体，拒绝额外 actor 字段和非法版本／摘要／outcome，调用现有智能体决策服务。成员或执行调用不能替代审核调用，重复当前决策返回原记录；记录通过仍等待审核调用成功完成后验收。

首轮接口集成通过（7.359s），相关 service 测试通过（7.805s），service／handler／server Go vet 通过。新增非法请求体与过期版本断言后最终接口复测、内置 skill 模板测试继续运行。专用领取、后台接线、智能体 CLI 及 hybrid UI 放行仍未完成，原计划其余目标继续保留。

最终接口复测通过（6.670s），包括无效请求体和过期版本拒绝；内置 skill 模板测试通过（3.231s），git diff --check 通过。


## 第四十一轮：审核领取及执行前校验

2026-09-09 TaskService 在普通领取未取得任务后尝试专用审核领取。在 Run／交付证据锁下校验审核请求与调用身份，再取得智能体容量锁，遵守并发数及同 issue 调用互斥。仅领取请求绑定的 queued 调用。迁移 300 以独立并发索引支持按审核者查询待领取请求。

签发调用 token 和 StartTask 前再次校验，核对最新调用状态、领取时间、运行时、issue、审核请求及交付依据，避免暂停或证据变化后继续执行。StartTask 对审核调用走事务内校验，普通调用保留原有状态转换。

相关测试通过（7.903s），覆盖真实 ClaimTask → StartTask → 审核决策路径及暂停后拒绝启动。新增并发只领取一次、暂停后拒绝 token 签发断言后，全量 service 与 race 继续运行。迁移 300 在独立 schema 中 up → down → up 通过。后台审核准备／验收接线和审核技术重试仍未完成；返工、配置表单、浏览器验收、P0 样板和 skill 评测继续保留。

全量 service 回归通过（46.751s），审核／领取 race 检查通过（10.447s），Go vet 和内置 skill 模板测试（3.694s）通过。正在补查现有 handler 的领取凭据签发失败／回滚用例；未重启仍在编译的检查进程。

handler 凭据签发与领取失败回滚相关复查通过，最终 git diff --check 通过。

## 第四十二轮：后台审核编排

2026-09-09 接通 acceptance worker 的 agent／hybrid 扫描：启动及周期扫描按执行 ID 分页，持久化状态恢复审核准备与派发。每步复用 Run 锁下的权限、证据与绑定校验；agent 仅在同一证据的审核通过且调用成功完成后原子验收，hybrid 保留人工步骤。自动验收提交后发布工作流更新事件，幂等读取不重复发布。

集成用例覆盖重复扫描只绑定一个调用、运行中的审核不能推进、完成后 agent 自动验收、hybrid 仍等待人工。首次相关测试通过（7.883s），全量 service 回归通过（48.105s）；补充并发扫描断言后 race 检查进行中。Go vet、内置 skill 模板测试（3.722s）与 git diff --check 通过。

本轮不宣称整体完成：审核 CLI、hybrid 前端入口、审核失败恢复、返工与依赖失效、配置表单、浏览器验收、P0 样板及 skill 评测迭代仍需继续。

并发审核扫描与决策路径 race 检查通过（9.203s），最终 git diff --check 通过。

## 第四十三轮：审核智能体 CLI 与派工指引

2026-09-09 新增 `workflow agent-review <run> <execution> <request>` 和 `workflow agent-decision`，使用现有任务凭据及智能体／调用身份头。命令校验规范 UUID、正结果版本、完整小写证据摘要与明确 outcome；不会把 actor 写进请求体，也不自动刷新或重试过期决定。普通 human review／decision 命令行为保留。

审核派工说明提供当前请求与结果版本对应的读取、决策命令，要求先读取产物再决定，并明确审核调用成功完成前不算阶段验收。同步内置 skill 及源码映射。

CLI 相关测试通过（2.948s），服务派工与内置 skill 测试通过（7.489s），Go vet 与 git diff --check 通过。首轮 CLI 夹具使用普通 token 被已有任务凭据防线拒绝，改为任务 token 后通过，未放松生产校验。全量 CLI 回归进行中。

审核重试／失败恢复、hybrid 人工界面、返工、依赖失效、配置表单、浏览器验收、P0 样板及 skill 评测迭代仍需继续；本轮不宣称计划完成。

全量 CLI 回归通过（3.390s），最终 git diff --check 通过。

## 第四十四轮：hybrid 人工审核界面

2026-09-09 共享交付审核界面支持 hybrid 的明确 human_decider，不能把智能体 decider 当成人工审核人。界面提示先完成智能体审核；仅服务端返回有效的当前审核依据后显示人工决定表单。审核未完成时可主动刷新检查，不能直接绕过服务端前置校验。

补充组件用例覆盖等待智能体 → 主动刷新 → 审核指定依据、缺失／不同 human_decider、不再可审核时清除旧确认。同时修复结果刷新失败但缓存仍在时保留决定表单的问题，刷新错误会撤下表单。交付审核与输入恢复合计 16 项组件测试通过（17.41s），git diff --check 通过；共享 views 类型检查及内置 skill 校验进行中。

本轮仅完成共享组件行为验证，尚未完成真实浏览器验收。审核失败恢复、返工与依赖失效、配置表单、P0 样板、skill 评测及版本迭代继续保留。

共享 views 类型检查通过；内置 skill 模板测试通过（2.970s）。类型检查原进程持续运行后正常完成，未重启。最终 git diff --check 通过。

## 第四十五轮：验收后的审核决定重放

2026-09-09 修复已验收执行上的智能体决定重试返回冲突的问题。RecordAgentDecision 在 Run 锁下识别 succeeded 执行，仅返回原始审核调用对应的同一 approved 决定；严格核对请求、结果 ID／版本、摘要、actor、调用角色及完成状态、说明、验收记录。hybrid 额外核对已验收的人工通过决定。保留当前审核者未归档及可调用校验，不新增决定、不再次推进。

相关集成通过（6.976s），覆盖 agent／hybrid 完成后重放、错误版本／摘要／outcome／说明拒绝、执行调用冒充拒绝及归档审核者拒绝。进一步加入 hybrid 人工验收记录核对后，全量服务与 race 校验进行中；Go vet 与 git diff --check 通过。内置 skill 已更新语义：这是已记录决定的响应恢复，不会绕过过期凭据，也不是失败调用的技术重试。

审核调用失败重试、返工与依赖失效、配置表单、浏览器验收、P0 样板、skill 评测及版本迭代仍未完成。

全量服务回归通过（51.802s），审核路径 race 检查通过（8.206s），内置 skill 模板测试通过（3.374s），最终 git diff --check 通过。

## 第四十六轮：审核失败持久化与证据稳定性

2026-09-09 准备审核技术重试时发现既有普通重试 SQL 拒绝 reviewer，但 FailTask 将该拒绝当事务错误，可能回滚审核调用的失败记录。普通重试资格现明确排除 reviewer，同时保护已进入事务的拒绝分支；审核失败可以提交并由专用恢复机制后续处理。

真实 FailTask 测试还揭示系统失败评论会触碰 issue.updated_at，改变交付摘要并让扫描创建新审核请求。审核失败信息改为保留在调用错误及失败事件中，不新增交付评论；测试核对失败后请求和绑定调用不变，无普通重试子调用，未决策或已记录通过的失败调用都不能验收。

初轮断言过度限定拒绝错误类型，暴露上述评论变化；修复后相关测试通过（5.124s）。此前重试资格修改的全量服务回归通过（52.434s）；最终评论修复的 race 检查与内置 skill 校验进行中。Go vet、git diff --check 通过。

本轮解决失败状态可靠落库及错误再派工问题，尚未实现专用审核重试。返工／下游失效、配置表单、真实浏览器与 P0 样板、skill 评测和版本发布继续保留。

最终审核失败／决定路径 race 检查通过（10.192s），内置 skill 模板测试通过（3.380s），git diff --check 通过。

## 第四十七轮：审核专用重试与逐调用决定

用户明确要求继续完成审核专用重试、返工与下游失效、完整配置表单，并要求页面／弹窗设计和谐、同类弹窗尽量等宽、整体设计语言一致；随后完成真实验收测试，再提供人工回归入口与步骤。该要求保留为后续交付标准，不以组件测试替代真实场景验收。

2026-09-09 新增 review_task_id，审核决定按具体调用分别保存。迁移 301 回填既有请求调用；302–305 用独立并发索引迁移替换原依据唯一索引及单调用唯一限制。请求仍只指向一个当前调用，保留旧调用及旧决定用于追溯。人工决定保持每依据唯一。

后台 review scan 识别失败调用并调用 RetryAgentReview：网络 overlay 准备在事务外；事务内锁 Run，重查依据、权限、运行时、当前调用、技术故障分类、重试上限和退避。创建 fresh-session 调用并原子推进请求指针，保留 parent_task_id／retry_of_task_id，提交后通知。旧调用无法为当前请求决策；新调用必须重新产生自己的决定，旧通过不会被带入验收。

相关集成通过（16.873s），覆盖无决定／已有通过后失败、暂停／关闭重试拒绝、并发扫描恢复、真实领取和开始、重新审核后推进。301–305 在独立 schema 执行 up → down → up 通过。测试数据库旧迁移登记停在 290，与已手工应用到 300 的实际结构不一致；迁移 runner 在 291 明确报已存在列，未重复执行结构修改，301–305 使用独立 SQL 文件应用。全量 service、race 和 vet 继续运行。

后续仍需补齐失败可视化与人工恢复、返工／下游失效、统一配置表单、真实场景验收及 skill 评测版本闭环。

全量服务回归通过（82.057s），增加并发子调用计数及历史决定保留断言后 race 检查通过（18.884s），Go vet 与内置 skill 模板测试（10.190s）通过，最终 git diff --check 通过。

## 第四十八轮：返工所需的任务轮次隔离

2026-09-09 新增 issue.workflow_stage_execution_id 及并发索引（306–307），回填已有阶段任务。创建普通／来源任务自动绑定阶段最新执行；同阶段更新保留绑定，显式移动解析目标轮次，脱离和项目清理清除当前绑定。旧轮次任务在新轮次存在时不能经普通修改／派工路径重新使用。

审核任务集、未完成任务与未绑定调用的完成屏障按当前轮次过滤；legacy 无绑定任务仍保守计入。审核 v3 的 issue JSON 投影显式排除新增绑定字段，避免仅因数据库增加字段而改变已有验收摘要。任务是否属于本轮由查询选择决定。

集成构造两轮任务，确认新建任务绑定最新轮次、旧轮次未完成任务和排队调用不影响当前依据、旧任务更新不改当前摘要、无绑定任务仍阻塞、历史任务修改拒绝。初次夹具漏传 runtime 导致数据库约束失败，补齐后相关测试通过（6.939s）。306–307 独立 schema up → down → up 通过，Go vet 与 git diff --check 通过。全量 service 最初仅该夹具失败，修正后最终回归与 race 正在运行。

本轮是返工隔离基础，不宣称返工入口已完成。下一步仍需显式返工事务、下游失效、统一配置与弹窗、真实场景验收，以及计划内 skill 评测迭代。

最终全量服务回归通过（109.639s），轮次隔离与审核路径 race 检查通过（17.941s），内置 skill 模板测试通过（5.187s），git diff --check 通过。

## 第四十九轮：显式返工事务与下游失效

2026-09-09 新增内部 ReworkExecution 服务，由 Run 发起人使用明确 source execution、目标阶段、计划修订与原因发起。允许当前智能体阶段重做或回退到该轮次冻结的 rollback_stage_key。308–309 记录返工来源及 actor／原因，按来源执行建立并发唯一索引；相同请求幂等，目标或原因不同则冲突。

事务内锁 Run，选取目标及后续阶段的最新执行；把 legacy 无轮次任务绑定到旧执行，取消未完成调用和派工 outbox，删除调用 token，失效所有审核请求，将旧执行标为 cancelled/rework_invalidated。旧验收、产物和决定保留，但输入解析不再接受。随后切换 Run 并创建目标新轮次；下游再次到达时依据失效标记创建更高 attempt。通知只在提交后发送。新增返工字段也从 v3 审核投影排除，维持已有摘要格式。

两阶段专项通过（5.592s），覆盖并发请求只创建一个新轮次、调用取消、token 撤销、旧输入／结果拒绝、重新派工使用新任务。补充目标执行者归档时原事务回滚验证后，全量 service 与 race 进行中。308–309 独立 schema up → down → up 通过，git diff --check 通过。

当前仍为内部服务，HTTP／CLI／界面入口、失败可视化与人工恢复、统一配置表单、返工策略配置及真实浏览器场景验收仍未完成；最终交付保持用户指定的统一设计语言和人工回归步骤要求。

最终全量 service 通过（54.304s），返工／验收 race 通过（9.496s），Go vet 与内置 skill（3.972s）通过。新增返工字段不进入 v3 摘要的回归断言复测通过（4.108s）。最终 git diff --check 通过。

## 第五十轮：返工 API 与共享客户端

2026-09-09 接入 POST /api/workflow-instances/{run}/executions/{source}/rework。复用成员身份校验并拒绝 agent/task 请求头，严格限制请求字段、大小、计划修订、目标 UUID 和非空原因。调用已验证的返工事务，返回 Run／来源／目标／新执行／轮次／当前状态，不暴露内部快照。

共享 core 增加请求和响应类型、schema、客户端及 useReworkWorkflowExecution。响应必须匹配发起时的 Run、原轮次和目标阶段，新 ID 不能等于原 ID；未知或不完整响应不视为成功。mutation 禁止自动重试及乐观阶段修改，onSettled 失效工作区工作流查询。

handler 集成通过（6.669s），覆盖 agent 拒绝、非法字段与参数、过期修订、成功与重复请求只建一个轮次，并确认旧 pending outbox 已取消。客户端与 mutation 合计 73 项测试通过（9.59s），覆盖身份错配、畸形响应、无自动重试及工作区缓存隔离。core 类型检查与 Go vet 进行中，git diff --check 通过。

返工操作弹窗和完整配置表单尚未完成，继续按同类弹窗等宽、统一布局与设计语言实施；真实场景验收后再交付人工回归入口和步骤。

core 类型检查、Go vet、内置 skill 模板测试（3.236s）通过，最终 git diff --check 通过。

## 第五十一轮：返工弹窗与操作界面一致性

2026-09-09 在阶段交付面板接入返工入口，仅当前 Run 发起人可操作。弹窗提供当前阶段与配置的合法智能体回退目标，列出受影响的目标及后续阶段，说明停止活动工作、保留历史，并要求填写原因和勾选确认。变更目标取消原确认；提交期间禁用字段与关闭；冲突后锁定表单，查询刷新不会把失败表单重置为可直接重试。

新增 workflow-dialog.ts 统一审核、输入恢复、返工操作弹窗的 sm:max-w-2xl 宽度、85vh 高度及滚动；人工阶段审核同步到相同宽度。输入恢复与人工审核的底部操作统一右侧布局及分隔边框。完整计划／配置编辑弹窗随后单独统一，不宣称全部页面已完成视觉验收。

返工、交付审核、输入恢复、人工阶段审核合计 31 项组件测试通过，覆盖目标影响范围、精确请求、权限、刷新与冲突、提交锁定、原因长度。views 类型检查仍在运行。真实浏览器视觉和端到端验收待完整配置链路接通后执行，再提供用户人工回归步骤。

内置 skill 校验通过（3.854s）。类型检查首轮捕获测试夹具 status 的 string 推断问题；工作文件已加 as const，首轮编译读取的是修正前快照，终止后重新执行增量类型检查。返工对混合手动／智能体阶段的完整下游工作覆盖仍需真实场景专项检查，不能以当前智能体链路测试推断已全部覆盖。

修正后 views 增量类型检查通过。31 项组件测试、内置 skill 校验及最终 git diff --check 均通过。未做本轮真实浏览器截图验收，不将组件测试等同于视觉验收。

## 第五十二轮：统一执行配置控件

2026-09-09 新增共享 StageExecutionFields，接入 SOP 定义编辑器和 Run 计划编辑器。执行模式与审核者独立：智能体执行需明确选择可用执行者，保存时校验未归档且仍可见。启用 agent 模式设置严格 validated_result 规则，并保留既有输出契约；没有契约时初始化最小 object 契约，不声称已代替业务契约设计。Run 过去／当前阶段的执行字段锁定，变更事件也检查 disabled。

配置弹窗原来仅声明 max-w-3xl／4xl，会被 Dialog 默认 sm:max-w-sm 覆盖；现共用 sm:max-w-4xl，统一桌面宽度。聚焦操作弹窗保持已统一的 2xl，配置弹窗使用更宽的编辑布局。

首轮组件测试发现只读 radio 的程序触发仍进入 handler，已补事件层保护。既有继承／保存测试缺少引用的 agent 列表，在新增可用性校验下无法保存，夹具补齐真实引用后复测。测试与 types 仍在运行。契约数据／产物、输入绑定等完整表单尚需继续；仍保留真实场景和视觉验收后交付人工回归的要求。

第五十二轮最终验证：16 项组件测试通过（55.34s），views 增量类型检查通过。

## 第五十三轮：契约配置表单与回退目标修正

2026-09-09 新增 StageContractFields，共用于 SOP 和 Run 编辑器。普通表单配置字段类型／必填、输出文件要求和输入来源绑定；高级 JSON 保留嵌套结构、枚举和范围约束。未应用的编辑保留在弹窗草稿内，取消不修改父表单；应用仍需父表单发布／保存，服务端最终校验。表单拒绝缺失文件元数据、无效嵌套结构及失效绑定；仅提供已支持的 Run 输入和前序智能体阶段结果来源。

修正 agent → manual 时残留 executor_id 导致服务端拒绝。阶段稳定标识在模板创建时固定，新增阶段避免冲突。继承时保留明确回退目标，移除 Run 保存时静默替换为上一阶段的行为；agent 配置新增回退选择，失效引用需显式修复。SOP 编辑内容宽度统一到 4xl，与契约／Run 配置弹窗一致。

首批新增与现有编辑器合计 28 项组件测试通过（85.39s），两次 views 类型检查通过；随后回退目标修改的最终复测进行中。已创建独立 silieco_sop_acceptance 数据库，从现有测试库复制结构（不复制数据），补齐 pgcrypto/pg_trgm。实际浏览器连接本地 Web 65315 与 API 18094，正在等待首次编译并执行页面验收。尚不能宣称整个真实执行链或视觉验收通过。

后续仍需混合手动／智能体阶段的完整下游失效、审核失败恢复可视化、Run 输入表单与完整场景回归，以及原计划内 skill 评测迭代闭环。

追加 Run 启动输入 JSON 表单、解析校验与同步防重复提交，启动弹窗采用统一操作宽度；智能体阶段隐藏不适用的手动完成规则控件。回退校验复测发现既有 Run 测试夹具引用了手动回退目标，已将测试改为先确认保存被阻止，再显式选择仅重做当前阶段，检查请求中的空回退目标，不通过静默替换掩盖问题。

浏览器首轮在冷编译期间超过 180 秒，未进入 SOP 页面；Web 编译进程仍活跃，保留现场继续验收。内置 skill 模板测试通过（3.019s）。

追加修改后 28 项组件测试通过（82.45s），涵盖输入草稿、契约保留、回退目标修复。类型检查定位到删除旧回退选择逻辑后遗留的未使用 map index，已移除并重新检查。浏览器复测改为 TestApiClient 登录后直接打开 SOP，避免额外 issue 页导航；测试使用真实 API 发布并通过 TestApiClient 归档所创建的 SOP。

补充同步重复点击／Enter 提交断言及畸形 schema 类型保护后，契约与 SOP 编辑器专项 22 项测试通过。最终 types 与浏览器仍在运行，未标记目标完成。

浏览器配置发布路径通过（1 项，2.5m），真实 API 创建并归档 SOP。已查看桌面／窄屏截图，发现窄屏 fieldset 滚动内容穿透底栏，改为独立滚动容器并为底栏设置背景，下拉控件统一填满所在列，增加滚动范围与底部文件按钮可见性断言。手动阶段也提供回退目标修复入口，避免重排后留下无法在表单修复的引用。对应 28 项组件测试再通过（90.34s）。最终截图复测进行中。

验收环境 MCP 列表因缺少加密 key 返回 503，已配置独立测试用 key 并重启 API；一次复测抢在 API 编译启动之前导致连接失败，后续先等待健康检查通过再执行浏览器。

最终配置浏览器复测通过（58.6s；整次 1.1m），包括滚动容器与按钮栏不重叠、底部文件按钮可见、桌面／窄屏边界及真实发布／清理。再次查看两张截图，确认底栏穿透修复和统一控件宽度；重启后的 MCP 列表返回 200，截图不再有环境错误提示。最终 views 类型检查通过，git diff --check 通过。详细验收范围与待办见 [验收清单](2026-09-09-sop-acceptance.md)。完整目标仍未完成，下一步继续审核失败恢复与混合阶段返工隔离，再交付最终人工回归。

## 第五十四轮：混合下游失效与并发重试

返工现在同时标记目标及下游人工／智能体阶段的 issue 和人工审核决定失效，保留原状态、结果与内容。取消这些 issue 的活动调用并撤销凭证；失效工作不能继续编辑、创建子任务或技术重试。当前看板和人工完成检查排除失效工作，阶段历史单独展示。列表、分组、搜索和实时刷新保持相同语义。

新增迁移 310 已在独立测试库和浏览器验收库应用，最小 schema 的 up/down/up 验证通过。返工先锁定 issue 再查询调用，等待并发插入的重试提交后将它一起取消。失败回报及 sweeper 重试先按 Run／issue 顺序取锁，再更新调用，避免 task → Run 与返工 Run → task 的锁循环。PostgreSQL 实际阻塞回归检查失败回报等待 Run 时尚未占用调用行，并验证已取消工作不产生重试子调用。

验证通过：完整 service 52.569s；返工／重试专项 race 8.655s；相关 HTTP 11.714s；核心 schema/API/缓存 207 项；WebSocket／返工 16 项；人工审核组件 7 项；views 增量类型检查；内置 skill 2.336s；service/handler go vet。真实浏览器配置验收沿用第五十三轮结果，本轮历史展示尚未作浏览器验收。

仍需完成：人工阶段发起退回并创建新业务轮次、人工审核旧表单预条件、审核失败恢复界面、完整重做后交付的浏览器链，以及计划中的 skill 评测发布闭环。本轮不作为最终人工回归交付。

## 第五十五轮：人工审核 Run 状态预条件

人工阶段 transition 现在要求已查看 Run 的 `expected_state_token`。Run 响应提供 `state_token`，基于 Run、阶段、计划修订、状态及数据库完整精度的更新时间生成；事务持有 Run 锁后再次检查。缺失、非法或过期凭据返回 409。同阶段重新进入／状态变化也不能复用旧凭据；不将该凭据当作结果证据摘要或身份授权。

人工审核弹窗在打开时固定凭据，实时更新改变状态后禁用通过和退回。响应缺失／格式错误、提交失败或结果不确定同样锁定操作，需要关闭后重新查看。同步提交锁防止 pending 渲染前双击，mutation 明确禁止自动重试。操作弹窗继续使用统一 2xl 宽度。

已通过：相关 workflow HTTP 7.369s，含一微秒状态变化的旧凭据拒绝及原有推进链；schema 106 项；人工审核组件 10 项；内置 skill 2.704s；handler go vet。真实浏览器两客户端状态变化、桌面／窄屏布局及重新打开后提交正在验收。完整人工退回并创建新轮次仍需下一步实现。

第五十五轮最终补验：views 类型检查及独立 E2E 文件类型检查通过。浏览器首轮误走 SOP 定义编辑器，修正为项目 Run 路由；第二轮首次编译完成并打开弹窗后发现测试把原生 Response.status 写成方法，修复后最终复测通过（20.0s／整次 29.1s）。真实后端记录旧凭据 409、重开审核后完成 Run 200，实时刷新确实禁用旧弹窗。桌面／390px 窄屏截图已检查，按钮可达、没有横向溢出。最终 git diff --check 通过；目标仍在进行，未将这项审核状态保护等同于完整人工返工链或 P3 闭环完成。

## 第五十六轮：人工退回与独立返工记录（验收中）

新增 workflow_rework，返工身份不再依赖来源智能体轮次。迁移 311–315 回填既有智能体返工来源，并将 issue／人工决定失效字段更名为 workflow_invalidated_by_rework_id／invalidated_by_rework_id。四个索引各自使用独立单语句 CONCURRENTLY 迁移。升级、回滚、再升级已在隔离 schema 验证，旧来源和历史指针保持一致，缺失来源的记录被约束拒绝。测试库及独立浏览器库均已升级至 315。

人工审核退回配置中的早先阶段时，先在 Run 锁内记录返工及原因，再共同失效目标及下游的人工／智能体工作、审批、调用、凭证和派工。目标为智能体阶段时，同一事务内建立新执行轮次和派工意图；目标为人工阶段时不伪造智能体执行。旧状态凭据仍不可复用。退回要求原因（非空，最多 4000 字节）；弹窗展示影响范围并要求确认，当前 2xl 宽度不变。

Run 详情提供安全的 reworks 历史，阶段记录显示来源、目标、原因、操作者和时间，不返回内部状态凭据。项目／工作区删除显式清理返工记录；项目删除解除遗留 issue 的失效指针，避免留下无法编辑的悬空状态。

已验证：service 完整测试 62.387s；相关 HTTP 14.197s；返工 race 9.812s；混合链专项 6.534s；纯人工退回专项 5.812s；组件 20 项；核心 API／缓存 191 项；新增历史 schema 后 schema 107 项；内置 skill 5.814s；service/handler vet。项目删除补充断言、最终类型检查和真实浏览器退回再完成仍在运行。

剩余范围仍包括：智能体源阶段退回人工目标、审核失败恢复界面与完整智能体审核浏览器链、未来计划配置约束整理，以及 P3 skill 评测／发布闭环。当前不构成最终人工回归交付。

第五十六轮补验：项目删除及悬空 issue 标记清理 HTTP 6.597s 通过。手动 transition 的 issue 缓存遗漏先由两个失败测试复现，再修正为等待同工作区 workflow／issue 缓存全部失效；成功、失败及不自动重试共 6 项测试通过。浏览器首轮完成退回和历史断言后在重载等待中超时；最终复测完整通过（41.8s／整次 59.4s），包含再次交付、重新审核和最终完成。桌面及窄屏最终截图已查看，确认区／按钮可达，无横向溢出。迁移新增有人工返工数据时拒绝降级的保护，隔离 schema 已验证拒绝不会删除历史，并再次验证旧智能体历史 up/down/up。最终类型检查仍在执行，不能据空日志宣称通过。

第五十六轮最终类型检查通过（包含最后的缓存失效修改）；最终内置 skill 校验 3.341s 通过，git diff --check 通过。本轮服务、HTTP、race、组件、API／缓存、迁移和真实浏览器证据均已收尾。完整目标仍保持进行中。


## 第五十七轮：失败审核的人工恢复与统一弹窗

新增会员审核状态接口及 Run 发起人的恢复接口，失败调用必须匹配当前审核请求、结果修订和证据摘要。自动重试尚有预算时拒绝额外恢复；每次显式恢复授权最多 3 次调用（包括技术重试），新调用使用独立 session 并要求新的审核结论。Run 锁内记录恢复原因、操作者、旧／新调用及预算，重复或并发提交相同请求只创建一条记录。审核者更换运行环境时丢弃旧主机工作目录。已完成但没有结论的调用仅显示诊断，尚不属于失败调用恢复接口。

新增迁移 316–319，均不引入外键，索引各用单语句 CONCURRENTLY 文件；项目／工作区删除明确清理恢复记录。两个本地测试库均已升级，隔离 schema 的 up/down/up/down、重复恢复唯一性及预算约束通过。状态响应不返回原始错误、session、运行时 overlay 或证据快照。

共享页面展示准备、排队、执行、自动重试、失败与审核结论；恢复弹窗沿用 2xl 操作宽度、语义配色及统一页脚。填写原因和确认后才可提交，固定打开时的调用身份，旧状态和结果不确定时锁定，mutation 不自动重试、不乐观推进 Run。新 API 通过 schema 解析，未知状态不会开放恢复。

验证已通过：workflow service 55.675s；审核失败／恢复 race 15.484s；workflow HTTP 8.145s；内置 skill 3.303s；共享组件 24 项；新增 schema／mutation 6 项；views 类型检查。真实浏览器使用无 daemon 的受控 provider，完成失败审核到新调用排队（56.3s／整次 1.1m），未执行用户已安装 Agent CLI。桌面及 390px 截图均已查看，宽度一致、内容不横向溢出，确认和提交按钮可达。补充 API 请求／异常响应测试及 vet 正在收尾。

剩余：无结论的已完成审核恢复、智能体来源返工到人工目标、完整配置策略与未来阶段约束、完整审核／返工链的组合浏览器验收，以及原计划 P3 的 skill 评测发布闭环。尚不作为最终人工回归交付。

第五十七轮最终补验：API client／schema／mutation 合计 82 项通过，service／handler go vet 通过。独立 E2E 类型检查首次误用 NodeNext，触发现有无扩展名导入的解析错误；改为项目使用的 Bundler 模式后通过，无需修改产品导入。最终 git diff --check 通过。验收环境 API 已更新为本轮实现，测试 fixture 已清理。完整 goal 仍在进行。


## 第五十八轮：智能体来源返工到人工目标

返工结果改为独立记录加可选 Agent 执行，重复请求从 workflow_rework 查回原操作。配置中的早先人工阶段可作为回退目标；同一事务保留原因和来源、失效目标及下游工作与审核、取消旧调用，再将 Run 移回人工阶段。人工目标不创建 Agent 轮次、派工或虚假 attempt；响应明确返回 rework_id、awaiting_work、空 execution_id／attempt。Agent 目标保留原执行返回语义。前端 schema 区分两种结果，缺失记录或混杂执行字段的人工结果不能当作成功解析。

共享返工弹窗开放已配置人工目标，沿用原有原因、影响范围、确认、旧状态及重复提交防护。修复确认前误用“已记录返工”的历史标题，独立说明旧工作与审批仍保留但不能用于新交付；选择控件改用主题强调色。

已通过：返工 service 5.865s；并发 race 10.014s；含人工目标与重放断言的 HTTP 6.573s；完整 workflow service／builtin 62.992s；API／schema／mutation 86 项；返工组件首次 10 项；E2E 类型检查、service／handler vet。真实浏览器首次完整通过（1.7m／整次 1.9m）：Agent 退回人工输入、旧任务标记失效、创建新人工交付、审核推进到 Agent 第 2 次执行且结果修订归零。首次桌面／390px 截图已查看，无横向溢出、确认与提交可达。提示文案和主题色调整后的组件／浏览器复测及 views 类型检查仍在运行。

仍需完成：审核结束无结论的恢复、配置策略与未来阶段执行约束、组合链路及界面的最终验收、P3 skill 评测发布闭环。原始完整 goal 保持进行中。

第五十八轮补验：最终返工组件 10 项通过；浏览器完整复测再次通过（2.3m／整次 2.5m），提示文案及主题单选色已在窄屏截图检查，复选框随后补齐相同 accent-primary 类。首次 views 类型检查发现新增测试直接访问可选 run.stages，已改成空数组兜底；完整类型复测仍在执行，日志 `/tmp/silieco-agent-manual-rework-types-final.log`，进程句柄 70698。此前失败只涉及测试类型，不能把当前复测空日志算通过。最终 diff --check 通过。


第五十八轮最终类型复测通过（句柄 70698 已终止且 exit 0），跨类型返工验证收尾。

## 第五十九轮：审核进程完成但无结论的显式恢复

审核进程 completed 而当前请求／结果摘要／调用没有 agent 决定时，Run 发起人可显式恢复。旧进程状态保持 completed，新调用使用新 session 和原有 3 次预算。服务在准备运行环境前与 Run 事务锁内分别检查决定不存在，创建新调用的 SQL 再次检查；已 completed 且有 approved／rejected 决定的调用不能恢复。自动技术重试仍要求进程 failed，无结论恢复必须由成员明确操作。请求 failed_task_id 表示未完成审核职责的原调用，不会借此改写原进程结果。

状态接口的 missing_decision 现在结合当前 Run／权限提供恢复能力，界面只接受匹配的 phase 和调用状态，不能凭过期 can_recover=true 恢复已通过／拒绝的调用。现有原因、确认、版本／摘要／调用预条件和统一 2xl 弹窗继续生效。

已通过：服务专项 12.127s；恢复／迟到决定并发 race 18.820s；HTTP 7.086s；界面首轮 11 项。迟到决定竞争测试最初沿用“已决定”请求状态却删除了决定，导致不真实的 no rows；已改为 queued 的真实无结论请求并通过。HTTP 新测试修正一处上下文变量遗漏后通过。浏览器首轮在 server 重编译未启动时认证连接失败；server 健康检查 200 后重新运行 failed／missing 两种场景，尚待完成。完整 service／builtin、最终界面测试、E2E 类型及增量 views 类型检查也在执行。

本轮未新增数据库字段；内置 skill 与 source map 已同步。剩余重点为完整配置策略、未来阶段约束与最终组合验收，以及原计划 P3 skill 评测／发布闭环。

第五十九轮补验：完整 workflow service／builtin 64.964s、最终界面 13 项、E2E 类型、service／handler vet 通过。服务启动完成后浏览器两场景均通过（failed 1.7m、missing 59.5s，整次 2.8m），均调用真实恢复 API 并显示新审核排队。missing 场景桌面与窄屏截图已查看，统一宽度、无横向溢出、确认与提交可达。测试使用无 daemon 的受控 provider，没有执行真实 Agent CLI。增量 views 类型检查仍在句柄 99829 执行，日志 `/tmp/silieco-missing-review-types.log`；不得因日志为空而判通过。最终 diff --check 通过。


第五十九轮最终增量 views 类型检查通过（句柄 99829 exit 0），无结论审核恢复验证收尾。

## 第六十轮：跨类型返工配置入口

执行服务支持 Agent 退回人工阶段后，SOP 设计器和 Run 计划表单仍过滤人工目标并禁止保存。现已删除两处目标列表和两处保存校验中的 Agent-only 条件，保留前序阶段顺序及稳定键验证。结果输入绑定仍只使用前序 Agent 结果；手动回退目标不等于可引用的 Agent 结果。

设计器继承发布与 Run 表单选择人工目标均已有测试。原复杂模板用例在并行运行时超过默认 5 秒，单线程并提高单次测试上限后通过。Run 下拉选择测试补齐真实鼠标移入高亮事件后通过；这是 Base UI 点击非高亮选项不提交的测试交互问题。真实浏览器已选中人工目标，首轮因下拉箭头加入文本导致精确断言失败，改为验证标签文本后正在复测完整发布。

未来阶段编辑审计确认：当前任意执行轮次都会使整个 Run 计划不可编辑；服务、SQL 和 UI 多处依赖执行 plan_revision 与 Run revision 相等。完成未来阶段编辑需要系统性保留正在运行和历史执行的快照，不能单独移除接口保护。retry_policy/rework_policy 完整配置以及 P3 skill 评测发布仍待实现，完整 goal 不缩小。


第六十轮补验：设计器 12 项和 Run 表单 1 项通过，共 13 项；共享 views 与 E2E 类型检查通过，内置 skill 3.099s 通过。Run 选择测试需要 Base UI 的鼠标高亮再点击，补齐后实际提交目标稳定键 stage_1。浏览器首次选择成功但文本断言包含下拉箭头而失败；修正断言后完整发布通过（41.3s／整次 51.7s），真实 POST 返回 201，提交的 Agent 阶段引用前序人工稳定键。桌面／390px 截图已查看，内容没有横向溢出，顶栏发布按钮可达。截图暴露旧帮助文字仍声称只能回退 Agent，现已更新中英文提示并将执行模式单选改为主题强调色，最后显示复测正在进行。

第六十轮最终复测通过（浏览器 1.5m／整次 1.6m）。最终窄屏截图已重新查看：帮助文字明确支持人工或 Agent 目标、主题单选色正确、发布按钮可达。类型、13 项组件、内置 skill 与 diff --check 均已通过，本轮无挂起检查。完整 goal 继续进行。


## Retry policy contract foundation (round 62, integration pending)

`core/pkg/workflowcontract/retry_policy.go` now defines a strict, independently tested contract: optional `max_attempts` (1–10 total calls, first call included), `timeout_seconds` (1–86400), `backoff_seconds` (0–3600), `backoff_multiplier` (1–10), and `max_backoff_seconds` (0–3600, at least the initial delay). Multiplier/cap require an explicit initial delay. Missing fields preserve task defaults. Explicit zero backoff stays zero; exponential delay saturates at its cap without unbounded iteration or integer overflow. The explicit attempt ceiling overrides provider heuristics.

This contract is not yet connected to API persistence or execution. Do not describe the retry-policy form or runtime enforcement as implemented. Integration must cover initial task creation, failure-time transactional child creation, sweeper retry, review-specific retry/status/recovery and timeout cancellation. Existing `retryAttemptCeiling` raises provider-network calls to three unless retry is disabled; explicit SOP limits must bypass this widening. Existing stale-task timeout deliberately protects healthy long-running agents; an explicit SOP execution deadline must be separate from runtime-liveness recovery and cancel actual running work. Preserve ordinary task behavior when no SOP policy is set. Review recovery's explicitly granted budget must remain consistent with any frozen policy and history.


## Retry policy storage and snapshot (round 63)

Migration 321 adds `retry_policy` to template and Run stages. Create/update handlers validate and canonicalize the policy, Run creation copies it, and execution creation freezes it into `config_snapshot`. Shared response schemas accept optional policy and reject malformed scalar values; both existing edit flows preserve loaded policy when saving other fields. Controls for editing these values and runtime enforcement remain pending; this storage work alone does not change scheduling.

Verified: focused execution snapshot regression (6.815s), 16 response-schema cases, and migration up/down/up in an isolated transactional schema (object defaults and array rejection). The service test confirms changing live stage policy from two to five attempts leaves the original execution snapshot at two. HTTP template-to-Run regression passed (8.766s), and builtin skill checks passed (3.314s). Views typecheck remains running in session 83347; log: `/tmp/silieco-retry-snapshot-types.log`. Acceptance API still runs the prior compiled backend; restart it before the next runtime/browser verification. Both local databases have schema 321, while the migration ledger remains at 290; do not run the all-migrations runner against them.


## Runtime execution retry integration (round 64)

`taskRetrySettings` resolves immutable execution policy for execution-bound tasks. Both `FailTask` transactional retry and `MaybeRetryFailedTask` use the same effective ceiling and delay, including provider-network failures. Child rows receive the explicit budget and scheduled `fire_at`. Ordinary tasks retain reason-specific defaults; review calls remain excluded from ordinary cloning. Missing or malformed execution policy cannot authorize an extra retry. Existing Run/issue lock ordering and SQL invalidation guards remain in place.

Focused workflow/retry tests passed (5.473s), including both failure-callback and sweeper paths with explicit max 2, 30-second delay, and a live-stage change to max 5 that must not affect the frozen limit. Broader workflow/retry/task/builtin service regression completed successfully (`/tmp/silieco-retry-runtime-regression.log`). Prior views typecheck also completed successfully (session 83347). No checks remain running from this round. Initial queue max-attempt display, reviewer-specific budgets/backoff, deadline cancellation, UI controls and full browser acceptance remain pending. The retry feature is not complete.


## Initial execution-task budget (round 65)

Dispatch now parses the execution's frozen retry policy before creating an issue or task. Invalid policy blocks dispatch as invalid configuration. `BindWorkflowExecutionTask` stores the explicit `max_attempts` atomically with the task/execution binding; omission preserves existing task defaults. The first task therefore reports the same budget that subsequent retries enforce.

Dispatch and retry regressions passed (8.931s), including initial limits 1 and 5 with the live stage changed to 10 before dispatch. Snapshot values remain authoritative, task attempt starts at 1, and existing exactly-once/rollback dispatch tests continue to pass. No migration was needed for the existing task column. Review-specific retries, deadline enforcement, controls, future-stage editing and P3 remain unfinished.


## Reviewer retry policy (round 66)

Initial reviewer calls now store the frozen explicit attempt budget in the same transaction that binds the review request. Review retry reads the execution snapshot before overlay preparation and again under the Run lock, enforcing configured backoff and preserving explicit ceilings against provider-network widening. Status diagnostics and manual-recovery exclusion use the same effective ceiling/delay. Review task budgets preserve separately authorized recovery grants instead of resetting to the original cap.

New tests cover initial reviewer budget 2, a 60-second backoff, denial before the delay, a second call with max 2, rejection of a third provider-network call, and preservation of the manual recovery grant with an explicit frozen policy. Initial review/retry regression passed (16.883s); The first grant test correctly hit an evidence conflict because it edited frozen configuration after review preparation. The fixture now sets policy before preparing the evidence basis. Corrected review/retry/builtin regression passed (`/tmp/silieco-review-retry-policy-corrected.log`); no checks remain running. Timeout enforcement and retry controls are still pending. Acceptance API requires a rebuild before browser acceptance.


## Explicit call deadlines (round 67)

A server worker now scans execution-bound and reviewer-bound running calls every two seconds, in pages of 100. It applies frozen `timeout_seconds` from `started_at`, so queued calls and stages with no explicit timeout retain existing behavior. Expired calls enter `FailTask` with `timeout`, preserving configured retry budgets/backoff and reviewer-only retry ownership. Retries have their own start/deadline. Malformed snapshots are logged and skipped rather than blocking other tenants. The existing daemon terminal-state watcher cancels the local run and discards late output; interruption is observed on its next status check, not synchronously at the exact deadline.

Focused deadline tests passed (5.207s): due/not-due/omitted limits, repeated scans, disabled retry, late result rejection and reviewer timeout without an ordinary clone. Existing daemon interruption/watch tests passed (2.060s). Broader deadline/review/retry/builtin regression, now including one permitted retry remaining queued across a repeat scan, passed (`/tmp/silieco-deadline-regression.log`). Server build check also passed (`/tmp/silieco-deadline-server-build.log`). No checks remain running. API restart and actual browser acceptance are still needed; configuration controls, future-stage editing and P3 remain unfinished.


## Retry policy configuration controls (round 68)

The shared `StageRetryFields` now exposes all five retry fields with integer ranges, dependent backoff validation and explicit default semantics. Controls use equal-width columns on larger screens and a single column on narrow screens. Both SOP designer and Run-plan save validation reject invalid drafts. Frozen/current stages remain disabled. Clearing initial delay restores the default backoff schedule by also removing its multiplier/cap. English and Chinese translations explain first-call accounting and timeout start semantics.

Component tests passed: 19 cases across retry/execution fields. Builtin checks and E2E typecheck passed. Designer/Run-plan regression passed (13 tests). First browser publication with all five fields passed (1.5m / total 1.7m), and the narrow screenshot was inspected. Views typecheck passed. The closed-listbox assertion exposed a persistent executor popup. The two execution-field selects now use below-trigger positioning instead of selected-item alignment; browser recheck is session 75259 (`/tmp/silieco-retry-fields-browser-alignment.log`), with focused component recheck logged in `/tmp/silieco-retry-fields-ui-alignment.log`. Logs use `/tmp/silieco-retry-fields-*`. Acceptance API has been rebuilt and is healthy on port 18094, with the deadline worker active (long-lived session 25967). Full goal still requires further acceptance, future-stage editing and P3.


Round 68 dropdown follow-up: below-trigger positioning alone did not remove the stale executor listbox. The form now controls each selector open state and unmounts its popup on selection/close. A focused regression asserts selected executor options disappear and the trigger closes. Current checks: UI/forms session 24784 (started before the additional focused test), browser 7713, views types 9850. No final visual acceptance is claimed until the closed-listbox assertion and fresh screenshots pass.


Round 68 final verification: browser passed (45.5s / total 53.8s), including the closed-listbox assertion and publication of all five policy values. Actual DOM inspection returned zero remaining listboxes; desktop and 390px screenshots were inspected, with no popup remnant or horizontal overflow and the publish action reachable. The diagnostic console output was removed after inspection. Controlled-selector/execution/designer/Run tests passed (21), retry-field tests passed (12), and views/builtin/E2E type checks passed. No checks remain running. Full-goal acceptance is still pending.


## Preserve skill pins during plan edits (round 69)

The existing Run-plan editor was resolving every stage's selected skills again on every save, even for a title/output-only edit. It now retains the original `required_skill_snapshots` whenever an existing stage's skill references are unchanged. New stages or explicitly changed skill selections still resolve their requested content. This prevents an unrelated plan edit from silently adopting a newer builtin/workspace skill revision.

The HTTP plan regression passed (4.329s): a valid historical builtin snapshot remains byte-equivalent as JSON after changing the current manual-stage name, modifying a future output and removing an unused stage. Existing audit, stale revision and locked-stage checks continue to pass. Builtin checks passed (`/tmp/silieco-plan-pinned-skills-builtin.log`); no checks remain running. Execution-time plan editing remains disabled; relaxing it still requires compatibility across Run revision, execution/result revision, review evidence and input-binding predicates. No claim of future-stage editing completion is made.


## P3 revision manifest foundation (round 70)

`core/pkg/skillbundle/revision.go` defines `silieco.skill-revision/v1` on top of the existing bundle manifest. The revision digest includes the existing content/package digest, a canonical compatibility-contract set and ordered declared tool dependencies. Existing bundle hashes remain unchanged for installed clients. Inputs require explicit skill identity/source, SKILL.md content, distinct canonical relative file paths, distinct contracts and uniquely named versioned tool declarations. Workspace, builtin and runtime sources are represented without claiming cross-source editing permission. Returned metadata owns its copies.

The manifest deliberately has no evaluation-case or held-out-answer fields. This structural boundary is not yet an access-control implementation: P3 still needs immutable revision storage, scoped ownership, published pointers, proposal/evaluation schemas, fixed environments, scoring, held-out isolation and UI/CLI workflows. Declared tool versions enter the digest; actual environment/tool binary pinning must be verified by the evaluation runner. Runtime-local source authorization remains to be designed at the API boundary. Future-stage revision compatibility also remains unfinished.

Tests cover sensitivity to content/files/contracts/tools, contract-order determinism, unchanged bundle identity, owned metadata copies, invalid paths and ambiguous dependencies. Final package tests passed, including incomplete identity/contract cases and case-insensitive path collisions for portable bundles. No checks remain running.


## P3 immutable revision storage (round 71)

Migrations 322–324 add workspace-scoped `skill_revision` records with source kind/identity, revision digest, bundle, manifest and creator audit. An UPDATE trigger rejects in-place mutation. Separate single-statement concurrent indexes enforce revision UUID and per-workspace/source/content identity. Queries create with conflict-do-nothing semantics and read by workspace-scoped identity/hash. Workspace deletion explicitly removes revisions in the existing cleanup statement; no foreign keys or cascading relationships were introduced.

The DB integration test passed: duplicate content is deduplicated, another workspace cannot read by UUID, an attempted update fails without changing content, and workspace deletion removes revisions. Migration up/down/up/down passed in an isolated schema, including actual concurrent index builds. Both local databases now have schema 324; the migration ledger is still 290 and the all-migrations runner must not be used against them. API needs a rebuild before relying on its updated workspace cleanup query.

This is storage infrastructure, not a candidate/publication API. Source authorization, manifest/hash validation at the service boundary, reviewable proposals, published pointers, evaluation records/runner, held-out access controls, frontend and CLI are still required. Future-stage revision compatibility remains unfinished.


## P3 workspace candidate service boundary (round 72)

`SkillRevisionService.CreateWorkspaceRevision` now validates workspace membership and matches the existing skill-management rule (owner/admin or member creator), resolves the source skill in that workspace, and derives source identity/description and all hashes on the server. Candidate content/files/contracts/tool declarations are validated before insertion. The persisted bundle uses the existing agent bundle format. Identical content returns the existing revision; creation does not edit live skill content or publish anything. A workspace key-share lock participates in deletion's existing exclusive-lock protocol, preventing revision creation from orphaning rows during workspace deletion.

Initial service/storage regression passed. Final rerun passed (2.966s), including noncreator-member denial and member-creator acceptance; no checks remain running. Results are logged in `/tmp/silieco-skill-revision-service-final.log`. Builtin/runtime source flows, candidate HTTP/CLI/UI, proposals, evaluation and publication remain pending. This service only implements the workspace-source branch of the full goal. Future-stage revision compatibility is still unfinished.


## P3 candidate creation/read API (round 73)

Workspace skills now have POST `/api/skills/{id}/revisions` and GET `/api/skills/{id}/revisions/{revisionId}`. Creation accepts candidate content/files/contracts/tool declarations under existing skill-management permission. Unknown fields, including client-supplied hashes/authors, are rejected; task/agent headers cannot impersonate the member creation path. The read endpoint verifies workspace membership and both source kind and source ID. Shared client methods parse responses, compare route identity with returned identity and reject mismatched bundle/manifest hashes; an unreadable successful write is not automatically retried.

Verified: HTTP regression (5.305s), 11 schema/client tests, core TypeScript check, builtin skill checks and server route compilation (4.594s). The HTTP test confirms candidate creation/replay, source-bound reads, forged-field and agent-header rejection, and unchanged live skill content. No checks remain running. Local acceptance API still needs restart to expose these new routes.

Pending: revision listing and UI, proposal provenance/reasons, agent proposal authority, builtin/runtime source branches, evaluation and publication. Future-stage plan compatibility remains unfinished. Creating these candidate records is not publishing a skill revision.


## P3 revision summary listing (round 74)

GET `/api/skills/{id}/revisions` now provides bounded cursor pages (default 25, maximum 100) ordered by creation timestamp and revision UUID. The cursor is resolved within the workspace and validated against the source skill before querying older rows. List and cursor queries omit bundle content. Migration 325 adds the matching concurrent index; both local DB schemas are now 325 while the ledger remains 290. Existing candidate/detail routes remain unchanged.

The shared client validates summary hashes, duplicate IDs, cursor position and source identity, and rejects a repeating cursor. First HTTP regression passed (5.239s), including insertion during pagination without skipped/repeated old records, foreign-source cursor rejection and absence of bundle payloads. All 17 schema/client tests and core TypeScript checks passed. Migration up/down/up/down passed in an isolated schema. Final metadata-only cursor HTTP check and server/builtin checks passed; no checks remain running. Logs: `/tmp/silieco-revision-list-*`. UI/CLI listing, proposal provenance and agent authority, builtin/runtime sources, evaluation/publication and future-stage compatibility remain pending.

### 第 75–76 轮：工作区绑定的修订查询与候选界面

- `packages/core/skills/revisions.ts` 提供分页查询、详情查询及创建 mutation。请求显式绑定发起工作区 ID，并覆盖默认 slug；mutation 的缓存更新使用请求变量中的工作区与 skill，导航不能把写入或刷新转移到另一个工作区。创建不自动重试，失败也刷新来源列表，成功只缓存不可变修订详情。
- skill 详情新增“版本”页签，支持分页查看、只读详情和候选创建。表单以打开时已保存的内容与配套文件为基线，支持编辑内容、增删文件、契约版本引用、工具及精确版本；未确认成功时保留草稿。候选创建与详情弹窗统一使用响应式 `sm:max-w-4xl`。
- 核心修订相关测试 22 项通过；界面与原详情页测试 11 项通过；views 类型检查、E2E 类型检查、内置 skill 检查通过。新增 `e2e/skill-revisions.spec.ts`，真实浏览器验收结果另行记录。
- 本轮仍不代表 P3 完成。提案来源与原因、内置/运行时来源、固定评测集和环境、保留集隔离、报告、人工发布及回滚仍需实现；执行中后续计划兼容修改仍待处理。

第 76 轮真实浏览器验收最终通过（26.5 秒）：创建 201、不可变详情、刷新回读、当前 skill 未变化、桌面等宽及窄屏边距均有直接断言。尺寸切换等待布局稳定；两个弹窗显式设置 viewport 减 32px 的宽度并限制桌面 4xl。详见验收记录与 `e2e/skill-revisions.spec.ts`。

### 第 77 轮：skill 改进提案的不可变来源记录

- 新增 `skill_improvement_proposal`，把提案分析与交付给智能体的修订内容分开。提案固定基线/候选修订、分类、理由、改进假设、适用范围和作者；不修改当前 skill，也不代表发布批准。
- 服务 `SkillRevisionService.CreateImprovementProposal` 沿用 skill 管理权限，校验两个不同修订都来自同一工作区和同一个 skill。来源执行和具体结果版本由服务端查询，记录 Run、阶段、轮次、计划版本、结果 ID 和摘要；不复制原始运行轨迹、输入或产物正文。
- 支持人工反馈（没有执行证据）、仅执行来源（不编造结果）和具体结果来源。提案分类区分指令问题、输入不足、工具故障、契约问题、模型能力、人工修正及优秀样例。
- 作者与请求 ID 组成幂等键；同一请求并发重试只创建一条提案，修改内容复用请求 ID 则冲突。重试仍检查当前权限；已固定的来源不会随新结果版本变化。数据库禁止 UPDATE，工作区删除显式清理提案。
- 迁移 326–329：无外键；所有索引独立 CONCURRENTLY。隔离 schema 的 up/down/up/down 实测通过，开发库和验收库均逐条应用至 329，未重跑旧迁移。
- 本轮先完成存储、服务及数据库集成测试。提案 API/界面、改进智能体授权、知识/契约提案审核、评测与人工发布闭环仍待接入，不据此宣称 P3 完成。

本轮最终验证：`TestSkillImprovementProposalPinsEvidenceAndChecksBoundaries`、现有工作区修订服务和修订存储测试全部通过（4.253 秒；`/tmp/silieco-skill-proposal-service-final.log`）。覆盖并发去重、请求冲突、来源固定、权限撤销、同工作区不同 skill 拒绝、人工/仅执行证据、禁止改写及工作区清理。

### 第 78 轮：改进提案 API 与共享客户端

- 在 skill 路由下接入 `POST /proposals`、`GET /proposals`、`GET /proposals/{proposalId}`。创建严格限制 JSON 字段和大小，作者与证据由服务端派生，拒绝智能体/task 身份头冒充成员；UUID 在边界解析，写入始终使用已解析的 skill 身份。
- 同一作者/请求重复提交返回同一提案；请求内容变化返回 409。读取、列表及 cursor 均限制到当前工作区和 skill。分页使用创建时间/ID 的游标，新提案插入不移动已遍历的旧页。
- 客户端使用 Zod 和 `parseWithFallback`，验证完整证据组、基线与候选差异、响应来源、请求关联及分页游标；创建还校验返回的来源执行/结果版本与请求一致。保留对未来分类及额外字段的读取兼容性。
- React Query 查询和 mutation 显式绑定工作区，覆盖默认 slug；提交不自动重试，成功缓存不可变详情，成功或失败均只刷新发起工作区的提案列表，不修改 live skill。
- HTTP 集成测试通过（5.543 秒），覆盖创建、重复提交、冲突、伪造作者/证据、错误 UUID、智能体身份拒绝、来源绑定读取、分页插入稳定性及 live skill 不变。提案界面、Run 证据入口和后续评测发布流程仍待接入。

第 78 轮最终验证：客户端与缓存测试 19 项通过（8.76 秒），core TypeScript 类型检查通过；服务端构建测试 7.914 秒、内置 skill 检查 3.266 秒均通过。日志位于 `/tmp/silieco-proposal-client-final.log`、`/tmp/silieco-proposal-types-final.log` 和 `/tmp/silieco-proposal-build-builtin.log`。当前验收服务尚未重启加载本轮提案 API，接入页面做真实浏览器验收前需重建该本地服务。

### 第 79 轮：改进提案表单与详情

- skill 详情新增“改进提案”页签，成员可读取列表、分页和详情，现有 skill 编辑者可创建提案。选择两个不同的不可变修订，填写反馈分类、理由、假设与适用范围，可选填写来源执行轮次和结果版本；服务端仍负责来源核验。
- 创建与详情都提供基线/候选内容并排比较，并保留内容包查看；详情展示人工反馈或服务端固定的执行/结果证据。使用共享 React Query，Web/Desktop 共用同一组件，没有新增平台路由。
- 提交结果不确定时锁定已提交字段，重试复用同一个请求 ID 和 payload；明确的 400/403/404 拒绝允许修改。提交中禁用关闭和重复操作。表单与详情采用和修订页一致的响应式 4xl 宽度，小屏保留左右 16px 边距。
- 新增共享界面测试和 `e2e/skill-proposals.spec.ts`。本轮真实浏览器验收结果完成后单独记录。Run 页面直接选取证据的入口、改进智能体授权、评测及人工发布/回滚仍待完成。

第 79 轮最终验证完成：5 项提案交互测试通过（含未知结果重试与明确拒绝后修改），8 项既有详情页回归测试通过；修复共享 Select 必填 `items` 参数及测试未使用变量后，views 类型检查通过（`/tmp/silieco-proposal-ui-types-verified.log`）。E2E 类型检查、内置 skill 检查通过；真实浏览器最终复验通过（47.9 秒，总计 57.9 秒，`/tmp/silieco-proposal-browser-final.log`）。桌面/窄屏和详情截图已查看，验收服务已重启加载提案 API，数据库保持 329。

### 第 80 轮：固定评测集与确定性校验模型

新增 `core/pkg/skilleval`，作为评测服务的纯逻辑基础，尚未对外暴露 API。

- `SuiteSpec` 固定 schema/validator 版本、样例顺序、改进/回归/保留分区、输入、输出契约、命名检查及来源引用。来源引用只通过格式检查，实际来源授权与数据使用权限必须由后续服务验证。
- 构建时深拷贝并冻结定义，摘要覆盖所有样例、规则、关键性和来源。输入对象顺序与空白规范化，使用 `json.Number` 避免大整数被浮点舍入。未知字段、重复 JSON 键、未知版本、重复 case/check ID、非法来源及复杂度/大小超限会被拒绝。
- `ImprovementCases` 只返回改进集，不返回回归集与保留集。受信任 runner 使用 `Trial` 取得 case ID 与输入，不携带检查规则、预期答案、输出评分契约或来源；默认序列化 `Suite` 不会展开私有样例。`StorageJSON` 明确为私有持久化用途。以上是数据投影边界，尚不能替代服务层对保留集、报告及 runner 调用的权限隔离。
- `Score` 复用现有阶段契约校验，分别返回关键输出契约、命名数据检查、全部检查是否通过及关键检查是否通过；不会生成“允许发布”结论。产物校验仅覆盖清单，实际字节、资源归属和领域正确性需要 runner/验证器后续核验。
- 通过固定规则可以检查必填、类型、枚举、数值范围及数组约束。当前只支持显式版本的确定性验证器；模型评分、重复运行统计、固定环境、消耗、预算/超时/并发限制、服务持久化及人工发布门槛仍待接入。

验证：`go test ./pkg/skilleval ./pkg/workflowcontract -count=1` 全部通过，分别 1.793 秒和 3.174 秒。覆盖内容冻结/摘要敏感性、保留答案投影、大整数、坏定义/坏输出、关键与一般回归、必需产物清单。日志：`/tmp/silieco-skill-evaluation-contract.log`。没有调用真实智能体或外部工具。

### 第 81 轮：私有评测集持久化与服务权限

- 新增不可变 `skill_evaluation_suite`，固定工作区、skill、摘要、验证器版本、分区数量及作者。私有定义使用 BYTEA 保存规范化后的精确 JSON 字节，避免 JSONB 对科学计数法等数字表示的改写影响摘要；读取时重建定义并核对摘要及摘要元数据。
- `SkillEvaluationService.CreateSuite` 当前限制工作区 owner/admin，返回不含定义的摘要。相同 skill/摘要去重；已有版本可在原始运行历史移除后重试读取，不重写旧版本。新版本中的执行与结果来源引用必须在当前工作区解析成功；引用不等于自动复制或证明输入内容来自原始运行。
- `ReadSuiteForCustodian` 只允许 owner/admin 读取完整私有定义；`ReadImprovementCases` 沿用 skill 编辑权限，仅返回改进集；普通工作区成员可 `ListSuites` 读取元数据。列表 SQL 不查询定义，cursor 同时限制工作区与 skill。成员被移除后不再允许任何列表读取。
- 工作区删除在应用查询中显式清理评测集；迁移 330–333 无外键，所有索引均为独立 CONCURRENTLY。隔离 schema 的 up/down/up/down 通过，开发库和验收库逐条应用至 333，未重跑旧迁移。
- 首轮数据库集成测试通过（4.783 秒），覆盖原始字节往返、内容摘要/元数据损坏拒绝、禁止 UPDATE、来源校验、列表不含私有数据、分页期间新版本插入、作者不能读取保留答案、非编辑者拒绝、移除成员与工作区清理。
- 本轮服务尚未暴露 API 或界面；后续接入时必须使用这些权限入口，禁止直接向普通读取返回 `definition`。评测执行的环境、预算、隔离与报告，以及人工发布/回滚仍待完成。当前验收服务未为本轮服务代码重启，下一次相关浏览器验收前需重新加载。

第 81 轮最终回归通过（6.881 秒）：评测集服务/权限测试，加上既有修订存储、候选创建和改进提案测试，确认新增工作区清理查询未破坏这些路径。日志 `/tmp/silieco-evaluation-suite-regression.log`；本轮检查均已结束。


## 当前范围调整与核心复核（2026-09-09）

按用户最新指示暂停评测相关扩展，优先 SOP 核心执行、审核恢复、返工失效、配置表单与人工回归准备；已实现的 P3 代码保留。本条覆盖历史记录中的“完整 goal 不缩小”等当时范围说明。原计划顶部与验收汇总已同步调整。

重新运行当前代码：Workflow／Builtin 服务测试通过（84.755s），HTTP 测试通过（13.421s），契约包独立全测通过（2.177s）；6 个 SOP 浏览器文件共 7 个场景全部通过（5.3m）。日志分别为 `/tmp/silieco-core-scope-regression.log` 和 `/tmp/silieco-core-scope-browser.log`。未运行真实模型。

核心主体已实现，剩余完整输入补正到产物交付推进的浏览器串联，以及视觉巡检和最终人工回归清单。已有智能体执行记录后禁止编辑整个 Run 计划，仍是明确功能限制；未来阶段不得提前执行已有保护，二者不能混淆。细节以验收记录顶部为准。本轮仅更新范围与验证记录，没有修改业务逻辑。
