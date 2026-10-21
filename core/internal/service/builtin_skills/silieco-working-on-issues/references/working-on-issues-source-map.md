# working-on-issues source map

Evidence layer for `SKILL.md`. Every contract the skill states is traced to a
current `file:line` here. Lines were re-derived against `feat/builtin-skills`
after the latest `main` merge; the prior skill cited pre-merge lines that have
since moved (see the "drifted" column). Re-confirm with the verification command
at the bottom before relying on an exact line.

## `silieco issue pull-requests` — read PR links from Silieco

| Behavior | File:line | Drifted from |
|---|---|---|
| CLI command `pull-requests <id>` (alias `prs`) | `core/cmd/silieco/cmd_issue.go:105` | `:104` |
| `runIssuePullRequests` handler | `core/cmd/silieco/cmd_issue.go:507` | new citation |
| Calls `GET /api/issues/<id>/pull-requests` | `core/cmd/silieco/cmd_issue.go:522` | `:522` (unchanged) |
| API route registration | `core/cmd/core/router.go:480` | `:480` (unchanged) |
| Handler `ListPullRequestsForIssue` → `Queries.ListPullRequestsByIssue` | `core/internal/handler/github.go:687,692` | `:466` |
| Row → response mapper `issuePullRequestRowToResponse` | `core/internal/handler/github.go:205` | `:149` |

The CLI resolves the issue ref, GETs the endpoint, and (for `--output json`)
prints the raw `{"pull_requests": [...]}` body. Only `--output` is accepted; the
default `table` shows `NUMBER STATE TITLE URL`.

## PR response shape

`GitHubPullRequestResponse` struct: `core/internal/handler/github.go:58`. JSON
fields the agent can read off each element of `pull_requests`:

- `provider` (`json:"provider"`, line 63)
- `number` (`json:"number"`, line 67)
- `html_url` (`json:"html_url"`, line 70)
- `title` (`json:"title"`, line 68)
- `state` (`json:"state"`, line 69) — the folded lifecycle enum (see below)
- `merged_at` (`json:"merged_at"`, line 74), `closed_at` (line 75)
- `mergeable_state` (`json:"mergeable_state"`, line 80) — mirrors GitHub; UI only
  surfaces `clean`/`dirty`, other values round-trip as unknown
- `snapshot_available` (`json:"snapshot_available"`, line 100) — for GitHub,
  true only when the App snapshot feature is enabled and the snapshot head
  matches the current PR head (`currentGitHubSnapshotAvailable`, lines 258-265)
- `mergeable` / `merge_state_status` (lines 90, 94) — conflict-only verdict vs
  the complete merge gate; "ready" requires `merge_state_status == "clean"`
- `checks_rollup` (`json:"checks_rollup"`, line 105) and run-level
  `checks_total` / `checks_passed` / `checks_failed` / `checks_running`
  (lines 111-114), plus `failed_check_names` (line 118)
- `checks_conclusion` (`json:"checks_conclusion"`, line 108) — coarse
  `"passed"`/`"failed"`/`"pending"` or `null`; GitHub derives it only from an
  available current-head snapshot (mapper lines 242-254), while self-hosted VCS
  providers use `aggregateChecksConclusion` (line 275)

There is **no** standalone `draft` or `merged` boolean in the response. The
PR lifecycle is encoded in the single `state` string by `derivePRState`
(`core/internal/handler/github.go:1317`):

```
merged   → if PullRequest.Merged
closed   → else if PullRequest.State == "closed"
draft    → else if PullRequest.Draft
open     → otherwise
```

`derivePRState` is called when the webhook upserts the row
(`core/internal/handler/github.go:1115`), so `state` is what the list endpoint
returns. "Is it merged?" = `state == "merged"` (or `merged_at != null`); "is it a
draft?" = `state == "draft"`. Combine with `checks_conclusion` for CI status.

## Two distinct webhook paths: link vs close-intent

Both run inside the `pull_request` webhook handler, gated by the workspace
auto-link flag (`workspaceAutoLinkPRsEnabled`, `github.go:1074`).

### Path 1 — link (title OR body OR branch)

- `extractIdentifiers` regex helper: `core/internal/handler/github.go:1028`
- driving regex `identifierRe` (`\b([a-z][a-z0-9]{1,9})-(\d+)\b`, case-insensitive):
  `core/internal/handler/github.go:490`
- call site: `core/internal/handler/github.go:727` —
  `extractIdentifiers(p.PullRequest.Title, p.PullRequest.Body, p.PullRequest.Head.Ref)`

Every `PREFIX-NUMBER` mention in **title, body, or branch** resolves to an issue
in the workspace and writes a link row (`LinkIssueToPullRequest`, ~`github.go:762`).
This is what `silieco issue pull-requests` later reads back.

**Reference-only flag (SILI-3739).** The link row carries a `reference_only`
boolean (`migrations/127_issue_pull_request_reference_only.up.sql`). The handler
computes a `qualifyingIdents` set = identifiers in **title or branch** (any
`extractIdentifiers` match) ∪ **body closing keywords** (`closingIdents`). A
linked identifier NOT in that set was matched only by a bare body mention, so its
row is written with `reference_only = true`. Both `ListPullRequestsByIssue` and
`GetIssuePullRequestCloseAggregate` filter `AND NOT reference_only`, so
reference-only links are hidden from the CLI / UI PR list **and** excluded from
the auto-advance gate (an open body-only mention must not silently block the
issue from reaching `done` while invisible in the list). The row still exists for
edit-time close-intent tracking. `reference_only` follows the same
`preserve_close_intent` terminal gate as `close_intent`.

Drifted from the prior skill's `github.go:727` citation, which pointed at the old
call-site location for the link logic.

### Path 2 — close intent (title OR body only, keyword-adjacent)

- `extractClosingIdentifiers` regex helper: `core/internal/handler/github.go:1051`
- driving regex `closingIdentifierRe`
  (`\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)[:\s]+([a-z][a-z0-9]{1,9})-(\d+)\b`):
  `core/internal/handler/github.go:501`
- call site: `core/internal/handler/github.go:736` —
  `extractClosingIdentifiers(p.PullRequest.Title, p.PullRequest.Body)` (no branch arg)

Only a `PREFIX-NUMBER` immediately after a closing keyword
(`Closes`/`Fixes`/`Resolves`, optional `:` then whitespace) sets the link row's
`close_intent` flag — the gate that auto-advances the issue to `done` on merge.
`Fix SILI-1` closes; `Fix login SILI-1` does not (adjacency). Branch names are
deliberately excluded (function doc, `github.go:1044-1050`): a branch like
`sili-1/fix-login` links but must never declare close intent.

Drifted from the prior skill's `github.go:736` citation.

Net: a bare title prefix (`SILI-2759: ...`) or a branch ref links only (shown in
the PR list); `Closes SILI-2759` links **and** records close intent; a bare body
mention with no title/branch ref and no closing keyword links as `reference_only`
and is hidden from the PR list.

## Status side effects (enqueue contracts)

| Behavior | File:line | Drifted from |
|---|---|---|
| Create-time: agent-assigned, non-backlog issue enqueues immediately | `core/internal/handler/issue.go:2263-2264` | new citation |
| `shouldEnqueueAgentTask` returns false for `backlog` (parking lot) | `core/internal/handler/issue.go:2644-2648` | new citation |
| Backlog → non-backlog (not done/cancelled) enqueues on update | `core/internal/handler/issue.go:2537-2540` | `:2523` |
| Same contract in batch update | `core/internal/handler/issue.go:3021-3024` | new citation |
| Child → `done` notifies + wakes the parent, gated by the stage barrier | `core/internal/handler/issue_child_done.go:66` (`notifyParentOfChildDone`; doc comment at `:15`; barrier gate at `:115`) | func def `:51` |
| Status change (incl. → `cancelled`) does NOT cancel in-flight tasks; only issue deletion does (SILI-4465) | no-cancel note in `core/internal/handler/issue.go:2652-2658` (`UpdateIssue`) and `:3170-3171` (`BatchUpdateIssues`); deletion still cancels at `:2863` (`DeleteIssue`) / `:3239` (`BatchDeleteIssues`) via `CancelTasksForIssue` (`core/internal/service/task.go:1229`) | new citation |
| `StartTask` / `CompleteTask` do not write issue status (agent CLI owns progress) | `core/internal/service/task.go` (`StartTask` / `CompleteTask` comments) | new citation |
| Assignment brief: ordinary agent `in_progress` then `in_review`; squad leader `in_progress` only on first dispatch | `core/internal/daemon/execenv/runtime_config_sections.go` (`writeWorkflowAssignment`) | new citation |
| Failed task may roll `in_progress` → `todo` when no active task remains | `core/internal/service/task.go` (`HandleFailedTasks`) | new citation |

Creation with `--status todo` (or any non-backlog status) on an agent-assigned
issue fires the agent immediately; `--status backlog` parks it with the assignee
set but no trigger. Promoting `backlog → todo` later fires it then (update path,
line 2537).

Moving an issue to `cancelled` used to call `CancelTasksForIssue` and stop every
active task on it (the old #940 behavior). SILI-4465 removed that from both
`UpdateIssue` and `BatchUpdateIssues`: a status flip — `cancelled` included —
never cancels tasks now. `CancelTasksForIssue` fires only from the issue-deletion
paths (`DeleteIssue` / `BatchDeleteIssues`), where the owning issue row is going
away, so no task is left orphaned.

## Sub-issue stages (barrier wake)

| Behavior | File:line |
|---|---|
| `issue.stage` column (nullable, `>= 1`) | `core/migrations/123_issue_stage.up.sql` |
| Stage barrier: notify+wake fire only when the lowest unfinished stage is all-terminal; unstaged set = one implicit stage | `core/internal/handler/issue_child_done.go:231` (`stageBarrierClosed`) |
| Per-stage summary + next stage for the wake comment | `core/internal/handler/issue_child_done.go:254` (`stageProgressSummary`) |
| `--stage` on `issue create` / `issue update` | `core/cmd/silieco/cmd_issue.go:328,350` |
| `silieco issue children <id>` (sub-issues grouped by stage) | `core/cmd/silieco/cmd_issue.go:114,678`; route `GET /api/issues/{id}/children` → `ListChildIssues` |

Advancement is agent-driven: the server only detects the closed barrier and
wakes the parent assignee. Promoting the next stage's `backlog` sub-issues to
`todo` is the woken agent's decision, not a server side effect. When the woken
assignee (often a squad leader) decides the parent is complete, the system
comment explicitly asks for `silieco issue status <parent-id> in_review` —
comment-triggered runs otherwise must not change status unless asked.

## SOP workflow assignment

| Behavior | File:line |
|---|---|
| `--workflow-instance` + `--workflow-stage` on Task create/update | `core/cmd/silieco/cmd_issue.go` (`issueCreateCmd`, `issueUpdateCmd`, `runIssueCreate`, `runIssueUpdate`) |
| Task create validates Project alignment and membership of the immutable Run Stage copy | `core/internal/service/issue.go` (`IssueService.Create`, `GetWorkflowInstanceStage`) |
| Task update validates Project alignment and the same Run + Run Stage atomic pair | `core/internal/handler/issue.go` (`UpdateIssue`, `validateWorkflowTaskAssignment`) |
| Explicit null pair detaches a Task from its SOP run | `core/internal/handler/issue.go` (`UpdateIssue`); `core/internal/handler/workflow.go` (`DetachWorkflowTask`) |
| Claimed SOP Task exposes the immutable Run Stage catalog for semantic target selection | `core/internal/handler/daemon.go` (`buildClaimedTaskResponse`); `core/internal/daemon/prompt.go` (`buildWorkflowStagePromptBlock`) |

The continuation-inheritance rule in `SKILL.md` is an agent execution contract:
the runtime prompt supplies the same Run UUID plus all immutable Stage UUIDs,
while create/update validation remains enforced by the server paths above.
Backlog is independently handled by `shouldEnqueueAgentTask`, so it never
implies detaching workflow coordinates.

## Metadata CLI

| Behavior | File:line |
|---|---|
| `silieco issue metadata set <issue-id> --key --value [--type]` | `core/cmd/silieco/cmd_issue_metadata.go:80,109-111` |
| `silieco issue metadata delete <issue-id> --key` | `core/cmd/silieco/cmd_issue_metadata.go:93,113` |
| API routes (PUT/DELETE `/metadata/{key}`) | `core/cmd/core/router.go:478-479` |

`--value` is JSON-parsed by default (bool/number sniff); `--type` forces
`string`/`number`/`bool`.

## Custom properties CLI

| Behavior | File:line |
|---|---|
| `silieco property list/get/create/update/archive/unarchive` | `core/cmd/silieco/cmd_property.go` |
| `silieco issue property list/set/unset` (name→id translation) | `core/cmd/silieco/cmd_property.go` (`encodeIssuePropertyValue`) |
| Definition CRUD, admin gate, agent-actor rejection | `core/internal/handler/property.go` (`requirePropertyAdmin`) |
| Optional catalog icon field and allowlist validation | `core/internal/handler/property.go` (`PropertyResponse`, `validatePropertyIcon`) |
| Per-type value validation (self-correcting errors) | `core/internal/handler/property.go` (`validatePropertyValue`) |
| API routes (`/api/properties`, PUT/DELETE `/api/issues/{id}/properties/{propertyId}`) | `core/cmd/core/router.go` |

## Verification command

Re-derive any line above before depending on it:

```bash
cd core
grep -n 'pull-requests <id>'                 cmd/silieco/cmd_issue.go
grep -n 'ListPullRequestsForIssue'           cmd/core/router.go internal/handler/github.go
grep -n 'func issuePullRequestRowToResponse\|type GitHubPullRequestResponse struct\|func derivePRState\|func extractIdentifiers\|func extractClosingIdentifiers\|closingIdentifierRe' internal/handler/github.go
grep -n 'extractIdentifiers(\|extractClosingIdentifiers(\|derivePRState(' internal/handler/github.go
grep -n 'qualifyingIdents\|reference_only\|ReferenceOnly' internal/handler/github.go pkg/db/queries/github.sql
grep -n 'prevIssue.Status == "backlog"\|func (h \*Handler) shouldEnqueueAgentTask' internal/handler/issue.go
grep -n 'func notifyParentOfChildDone'       internal/handler/issue_child_done.go
```


## Local SOP contract preflight

| Behavior | Source |
|---|---|
| `workflow preflight --contract --result`, local file limits, structured JSON output and nonzero failure | `core/cmd/silieco/cmd_workflow.go` (`newWorkflowCmd`, `readWorkflowJSON`) |
| Restricted v1 contract, unknown-key rejection, exact numbers, JSON Pointer violations | `core/pkg/workflowcontract/contract.go` (`Parse`, `ValidateData`) |
| Client result fields, required artifacts, attachment identities and checksum format | `core/pkg/workflowcontract/artifact.go` (`Submission`, `ValidateSubmission`, `ValidateArtifacts`) |
| No server mutation or approval; malformed result and forged approval regression tests | `core/cmd/silieco/cmd_workflow_test.go` (`TestWorkflowPreflight`) |

Preflight has no API call or storage dependency. Its result does not attest to
attachment content or authorization. Server submission is a separate API; acceptance remains pending.

## Durable SOP dispatch

| Behavior | Source |
|---|---|
| Atomic main issue, task, execution binding and intent acknowledgement | `core/internal/service/workflow_dispatch.go` (`DispatchStageAction`) |
| Permission and runtime checks; persistent missing-runtime/permission block | `core/internal/service/workflow_dispatch.go` (`workflowMemberCanInvoke`, `DispatchStageAction`) |
| Recovery polling registered with server lifetime | `core/cmd/server/main.go`, `core/internal/service/workflow_dispatch.go` (`RunDispatchWorker`) |
| Queue binding and claim guards; task completion remains separate from stage acceptance | `core/migrations/275_workflow_task_execution_binding.up.sql`, `core/pkg/db/queries/agent.sql` (`ClaimAgentTask`) |
| Duplicate delivery, rollback, Run/issue guards and admin invocation tests | `core/internal/service/workflow_dispatch_test.go` |

## Execution configuration and startup

| Behavior | Source |
|---|---|
| `execution_mode`, `executor_id`, persisted Run `inputs` | `core/migrations/277_workflow_execution_configuration.up.sql` |
| Validate stage mode, executor identity, output/input contract and completion rule | `core/internal/handler/workflow.go` (`normalizeWorkflowStages`, `validateWorkflowStageExecutors`) |
| Run creation and entering an automatic stage commit dispatch intent atomically | `core/internal/handler/workflow.go` (`CreateWorkflowInstance`, `TransitionWorkflowInstance`), `core/internal/service/workflow_execution.go` (`EnsureStageExecutionInTx`) |
| Manual transition cannot accept an automatic stage | `core/internal/handler/workflow.go` (`TransitionWorkflowInstance`) |
| Executed plan adjustment rejection and `can_edit` projection | `core/internal/handler/workflow_plan.go` |
| Contract extraction from descriptive specs | `core/pkg/workflowcontract/spec.go` (`FromSpec`) |
| Startup validation, rollback and initial/next-stage dispatch coverage | `core/internal/handler/workflow_test.go` (`TestWorkflowStartCreatesDispatchIntentAtomically`) |


## Versioned SOP results

| Behavior | Source |
|---|---|
| Execution listing, result submit/read, authenticated actor and revision boundaries | `core/internal/handler/workflow_result.go`, `core/cmd/server/router.go` |
| Frozen contract, attachment scope, byte verification, immutable revisions and final Run revalidation | `core/internal/service/workflow_result.go` |
| Supported artifact formats, UTF-8/JSON validation and size bound | `core/pkg/workflowcontract/content.go` |
| Result/artifact metadata and upload intent ledger | `core/migrations/278_workflow_results.up.sql`, `core/pkg/db/queries/workflow_result.sql` |
| Delayed repeated storage cleanup and server worker wiring | `core/internal/service/workflow_artifact_cleanup.go`, `core/cmd/server/main.go` |
| Project/workspace deletion preserves storage tombstones | `core/internal/handler/project.go`, `core/pkg/db/queries/workspace.sql` |
| Immutable copies, CAS races, malformed content, lost upload acknowledgements and late PUT cleanup | `core/internal/service/workflow_result_test.go` |
| Authenticated exact-version artifact download and integrity check | `core/internal/handler/workflow_result.go` (`GetWorkflowResultArtifact`), `core/pkg/db/queries/workflow_result.sql` (`GetWorkflowResultArtifact`) |
| Artifact manifest, route mismatch and corrupt/missing storage tests | `core/internal/handler/workflow_result_test.go` |
| `workflow executions/submit/result/artifact`, explicit revisions, exclusive output file creation | `core/cmd/silieco/cmd_workflow.go` (`addWorkflowResultCommands`, `runWorkflowResultCommand`) |
| CLI authenticated requests, payloads, revision requirements and overwrite refusal | `core/cmd/silieco/cmd_workflow_test.go` (`TestWorkflowResultCommands`) |
| Execution-bound transcript identity survives issue movement; dangling bindings do not fall back to manual history | `core/pkg/db/queries/workflow.sql` (`ListWorkflowStageRuns`, `CountWorkflowStageRuns`), `core/internal/service/workflow_dispatch_test.go` (`TestWorkflowTranscriptKeepsExecutionIdentityAfterIssueMove`) |
| Consistent completion diagnostics; distinct result, issue and invocation evidence; cancellation and reopening rules | `core/pkg/db/queries/workflow_result.sql` (`GetWorkflowExecutionReadiness`), `core/internal/service/workflow_readiness.go`, `core/internal/service/workflow_readiness_test.go` |
| Readiness API and CLI (diagnostics only, not acceptance) | `core/internal/handler/workflow_result.go` (`GetWorkflowExecutionReadiness`), `core/cmd/silieco/cmd_workflow.go` |
| Manual transition rechecks locked delivery rows and rejects cancelled required issues | `core/internal/handler/workflow.go` (`TransitionWorkflowInstance`), `core/pkg/db/queries/workflow.sql` (`LockWorkflowStageIssues`), `core/internal/handler/workflow_test.go` |
| Transactional SOP attach/detach, ordered cross-Run locking and closure revalidation | `core/internal/service/workflow_issue.go`, `core/internal/handler/workflow.go` (`AttachWorkflowTask`, `DetachWorkflowTask`), `core/internal/service/workflow_issue_test.go` |
| Generic and batch issue update transaction protocol, stale row rejection and project/stage revalidation | `core/internal/service/workflow_issue.go` (`UpdateIssueWithWorkflowLock`), `core/internal/handler/issue.go` (`UpdateIssue`, `BatchUpdateIssues`), `core/internal/service/workflow_issue_test.go` (`TestWorkflowGenericIssueUpdateRevalidatesState`) |
| SOP issue creation shares Run/parent locks with transitions and rejects closure before insertion | `core/internal/service/issue.go` (`Create`), `core/internal/service/workflow_issue_test.go` (`TestWorkflowIssueCreationWaitsForTransition`) |
| Locked automatic status writes preserve assignment and reject stale snapshots | `core/internal/service/workflow_issue.go` (`UpdateIssueStatusWithWorkflowLock`), `core/internal/service/task.go`, `core/internal/handler/github.go` |
| Single-issue deletion cancels calls and linked Autopilot runs transactionally; notifications follow commit | `core/internal/handler/issue.go` (`DeleteIssue`), `core/internal/handler/workflow_result_test.go` (`testWorkflowDeleteFailurePreservesCalls`) |
| Shared single/batch deletion transaction and per-item rollback | `core/internal/service/workflow_issue.go` (`DeleteIssueWithWorkflowLock`), `core/internal/handler/issue.go` (`DeleteIssue`, `BatchDeleteIssues`), `core/internal/handler/workflow_result_test.go` (`testWorkflowDeleteFailurePreservesCalls`) |
| Manual SOP queue claims wait for the active current stage; unbound calls cannot bypass automatic execution | `core/pkg/db/queries/agent.sql` (`ClaimAgentTask`), `core/internal/service/workflow_dispatch_test.go` (`TestWorkflowManualQueueWaitsForCurrentStage`) |
| Technical retry inherits execution binding and atomically advances current main call, preserving business attempt | `core/pkg/db/queries/agent.sql` (`CreateRetryTask`) |
| Ineligible SOP retries preserve committed failure; sweeper skips stale parents | `core/internal/service/task.go` (`FailTaskWithDelivery`, `MaybeRetryFailedTask`) |
| Retry lineage, claim, result authority, readiness, insertion rollback and inactive Run tests | `core/internal/service/workflow_retry_test.go` |
| Ordinary assignee enqueue locks Run/issue, prepares overlays outside transaction, rechecks runtime and notifies after commit | `core/internal/service/task.go` (`enqueueIssueTaskWithCommentPlan`, `prepareIssueTaskWithCommentPlan`), `core/internal/service/workflow_enqueue_test.go` |
| Explicit mention/thread-parent/squad-leader enqueue shares Run transaction while retaining target and handoff metadata | `core/internal/service/task.go` (`insertPreparedIssueTask`, `enqueueMentionTaskWithCommentPlan`), `core/internal/service/workflow_enqueue_test.go` (`TestWorkflowMentionEnqueueSharesRunTransaction`) |
| Deferred fallback insertion shares Run validation, retains schedule and escalation identity, and emits no early queued event | `core/internal/service/task.go` (`insertIssueCall`, `EnqueueDeferredAssigneeFallback`), `core/internal/service/workflow_enqueue_test.go` (`TestWorkflowDeferredEnqueueSharesRunTransaction`) |
| Human review service locks delivery evidence, validates configured reviewer and records idempotent revision/basis-bound decisions | `core/internal/service/workflow_review.go`, `core/pkg/db/queries/workflow_review.sql`, `core/internal/service/workflow_review_test.go` |
| Execution decision basis fields and concurrent identity index | `core/migrations/291_workflow_decision_basis.up.sql`, `core/migrations/292_workflow_decision_basis_identity.up.sql` |
| Human review GET and decision POST, strict member identity, bounded payload, safe evidence response and error mapping | `core/internal/handler/workflow_review.go`, `core/cmd/server/router.go`, `core/internal/handler/workflow_review_test.go` |
| Review/decision API parsing and malformed-response fallback | `packages/core/api/client.ts`, `packages/core/api/schemas.ts`, `packages/core/api/client.test.ts`, `packages/core/api/schemas.test.ts`, `packages/core/types/workflow.ts` |
| CLI human review and explicit evidence-bound decision; conflicts never refresh approval credentials | `core/cmd/silieco/cmd_workflow.go`, `core/cmd/silieco/cmd_workflow_test.go` (`TestWorkflowHumanReviewCommands`) |
| Shared review query and decision mutation, workspace/execution cache isolation and no automatic retry | `packages/core/workflows/queries.ts`, `packages/core/workflows/mutations.ts`, `packages/core/workflows/review.test.tsx` |
| Shared automatic-stage delivery review UI, exact-version artifacts, acknowledgement reset and safe decision failures | `packages/views/workflows/components/execution-review-control.tsx`, `packages/views/workflows/components/execution-review-control.test.tsx` |
| Run panel chooses execution review for automatic stages; manual cancelled tasks block approval | `packages/views/workflows/components/stage-review-control.tsx`, `packages/views/workflows/components/stage-review-control.test.tsx` |
| Execution/result schemas, parsed client reads and authenticated artifact downloads | `packages/core/api/schemas.ts`, `packages/core/api/client.ts`, `packages/core/workflows/queries.ts`, `packages/core/api/client.test.ts` |
| Explicit input projections, source-revision evidence and input contract validation | `core/pkg/workflowcontract/bindings.go`, `core/pkg/workflowcontract/bindings_test.go`, `sop-input-bindings.md` |
| Stage input_bindings storage, normalization, backward dependency validation and Run plan preservation | `core/migrations/293_workflow_input_bindings.up.sql`, `core/pkg/db/queries/workflow.sql`, `core/internal/handler/workflow.go`, `core/internal/handler/workflow_plan.go` |
| Locked Run input projection, frozen provenance, idempotent recovery and failed-input rollback | `core/internal/service/workflow_execution.go`, `core/internal/service/workflow_input_bindings_test.go` |
| Designer/Run editor binding preservation and stable stage identity | `packages/views/workflows/components/project-sop-designer.tsx`, `packages/views/workflows/components/workflow-run-plan-dialog.tsx`, corresponding tests |
| Atomic human-reviewed acceptance, immutable result handoff and accepted-stage mutation guards | `core/internal/service/workflow_acceptance.go`, `core/internal/service/workflow_acceptance_test.go`, `core/internal/service/workflow_review.go`, `core/migrations/294_workflow_execution_acceptance.up.sql` |
| Decision API atomically records approval and advances; same-decision retries and failure rollback | `core/internal/service/workflow_review.go` (`DecideHumanExecution`), `core/internal/handler/workflow_review.go`, `core/internal/service/workflow_acceptance_test.go`, `core/internal/handler/workflow_review_test.go` |
| Durable none-gate acceptance scan, completion/permission guards and restart-safe advancement | `core/internal/service/workflow_automatic_acceptance.go`, `core/internal/service/workflow_automatic_acceptance_test.go`, `core/pkg/db/queries/workflow_execution.sql`, `core/cmd/server/main.go` |
| Missing input persists a blocked attempt without dispatch; predecessor acceptance remains committed | `core/internal/service/workflow_execution.go`, `core/internal/service/workflow_input_bindings_test.go`, `core/internal/handler/workflow_test.go` |
| Execution API exposes nullable failure diagnostics with malformed-field fallback | `core/internal/handler/workflow_result.go`, `packages/core/api/schemas.ts`, `packages/core/api/schemas.test.ts`, `packages/core/types/workflow.ts` |
| Internal blocked-input recovery uses frozen contract and atomically restores same attempt plus dispatch intent | `core/internal/service/workflow_input_recovery.go`, `core/internal/service/workflow_input_recovery_test.go`, `core/internal/service/workflow_execution.go` |
| Input correction CAS preserves consumed values and commits with recovery; review basis uses frozen input | `core/internal/service/workflow_input_recovery.go`, `core/pkg/workflowcontract/bindings.go` (`CheckRunInputCorrection`), `core/internal/service/workflow_review.go`, corresponding tests |
| Member input-correction API, strict payload, conflict behavior and parsed client identity checks | `core/internal/handler/workflow_input_recovery.go`, `core/internal/handler/workflow_input_recovery_test.go`, `core/cmd/server/router.go`, `packages/core/api/client.ts`, `packages/core/api/client.test.ts`, `packages/core/api/schemas.ts` |
| Shared input recovery mutation and Run panel with identity guards, exact old/new input and failed-submit protection | `packages/core/workflows/mutations.ts`, `packages/core/workflows/input-recovery.test.tsx`, `packages/views/workflows/components/input-recovery-control.tsx`, corresponding test, `execution-review-control.tsx` |
| CLI correct-inputs uses explicit old/new files, strict local JSON checks and no conflict retry | `core/cmd/silieco/cmd_workflow.go`, `core/cmd/silieco/cmd_workflow_test.go` (`TestWorkflowInputCorrectionCommand`) |
| Agent/hybrid reviewer identity validation and @self resolution frozen at execution creation | `core/internal/service/workflow_gate.go`, `core/internal/service/workflow_gate_test.go`, `core/internal/service/workflow_execution.go` |
| Durable agent/hybrid review preparation binds exact result/evidence and invalidates older pending requests | `core/internal/service/workflow_agent_review.go`, `core/internal/service/workflow_agent_review_test.go`, `core/pkg/db/queries/workflow_review.sql`, `core/migrations/295_workflow_review_request.up.sql`, migrations 296–297 |
| Distinct review call binding, exclusive roles, delivery barrier exclusion and ordinary claim/retry isolation | migrations 298–299, `core/pkg/db/queries/workflow_review.sql`, `workflow_result.sql`, `agent.sql`, `core/internal/service/workflow_agent_review_test.go` |
| Atomic reviewer queue creation, fresh-session context, runtime/evidence recheck and dispatch rollback | `core/internal/service/workflow_review_dispatch.go`, `core/internal/service/workflow_agent_review.go`, `core/internal/service/workflow_agent_review_test.go` |
| Bound agent decisions, completed-review acceptance and hybrid human barrier | `core/internal/service/workflow_agent_decision.go`, `core/internal/service/workflow_agent_decision_test.go`, `core/internal/service/workflow_review.go`, `core/pkg/db/queries/workflow_review.sql` |
| Agent review read/decision API with bound-call identity, safe projection and execution/Run scope checks | `core/internal/handler/workflow_agent_review.go`, `core/internal/handler/workflow_agent_review_test.go`, `core/internal/service/workflow_agent_review.go` (`GetAgentReview`), `core/cmd/server/router.go` |
| Dedicated reviewer claim, capacity and evidence guards, token/start revalidation | `core/internal/service/workflow_review_claim.go`, `core/internal/service/task.go`, `core/internal/service/workflow_agent_decision_test.go`, `core/pkg/db/queries/workflow_review.sql`, migration 300 |

- `core/internal/service/workflow_review_worker.go`: paginated durable agent/hybrid review preparation, dispatch, and agent acceptance; invoked by the acceptance worker.

- `core/cmd/silieco/cmd_workflow.go`: bound agent-review/agent-decision CLI, identity requirements and exact evidence arguments; `cmd_workflow_test.go` verifies request identity, payloads and no conflict retry.

- `packages/views/workflows/components/execution-review-control.tsx`: hybrid human reviewer entry, explicit readiness refresh and fail-closed decisions after evidence/result refresh errors; corresponding component tests cover these transitions.

- `core/internal/service/workflow_agent_decision_replay.go`: exact accepted approval replay under the Run lock, bound-call authorization and agent/hybrid acceptance-record matching.

- `core/internal/service/workflow_review_failure_test.go`: actual FailTask persistence, no ordinary clone, unchanged evidence/request after failure, and failed-call acceptance rejection.

- `core/internal/service/workflow_review_retry.go`: evidence-locked reviewer retry, budget/backoff and fresh-call dispatch; migrations 301–305 retain per-call decisions and multiple review-call history.

- Migrations 306–307 and `core/pkg/db/queries/issue.sql`: issue business-attempt bindings; `workflow_issue_attempt_test.go` checks review isolation and historical mutation rejection.

- `core/internal/service/workflow_rework.go`, `core/pkg/db/queries/workflow_rework.sql`, migrations 308–309: atomic business rework, retained history, downstream invalidation, call cancellation/token revocation and source-request idempotency.

- `core/internal/handler/workflow_rework.go`: strict member rework HTTP boundary; shared `packages/core` rework schema/client/mutation validate response identities and disable automatic retries.

- `packages/views/workflows/components/execution-rework-control.tsx`: initiator-only rework target/impact/reason confirmation and conflict/pending guards; `workflow-dialog.ts` centralizes operation dialog width.

- `packages/views/workflows/components/stage-execution-fields.tsx`: shared mode/executor selection, explicit availability and contract preservation; configuration dialog width is centralized in `workflow-dialog.ts`.

- `packages/views/workflows/components/stage-contract-fields.tsx` and
  `stage-contract-validation.ts`: shared input/output draft form, structured
  fields and artifacts, accepted-result bindings, advanced JSON and preflight.
- `e2e/sop-configuration.spec.ts`: real browser configuration and publish path,
  desktop/mobile dialog bounds and screenshot artifacts.

- Migration 310, `workflow_rework.sql` and `workflow_rework.go`: invalidate target/downstream manual or agent issues and manual decisions, cancel all affected calls, and retain original history.
- `workflow_task_retry.go`, `workflow_issue.go` and `task.go`: Run/issue locks precede failure retry call locks; historical work cannot spawn retry or child work. `workflow_rework_test.go` covers held retry versus rework and failure retry lock order using real PostgreSQL blockers.
- `handler/issue.go`, `queries/issue.sql`, shared issue/decision schemas, and `workflows-page.tsx`: invalidation markers survive list/group/search responses; current board and review exclude invalidated work, while the transcript retains it. `use-realtime-sync.ts` and the rework mutation refresh both Run and issue queries.

- `handler/workflow.go` (`workflowInstanceStateToken`, `TransitionWorkflowInstance`) and `workflow_test.go`: manual transition state preconditions rechecked under the Run lock, including sub-second timestamp changes. Shared workflow type/schema and `stage-review-control.tsx` freeze the reviewed state token, disable stale/malformed/uncertain submissions and prevent duplicate clicks. `e2e/sop-manual-review.spec.ts` exercises the real API and two-client state change.

- Migrations 311–315 and `workflow_rework.sql`: independent rework audit identity, historical source backfill, unique origin indexes and Run history query. Project/workspace deletion explicitly removes these records.
- `workflow_rework.go` (`InvalidateWorkflowForReworkInTx`), `handler/workflow.go` and `workflow_manual_rework_test.go`: manual configured rollback invalidates affected history and enters an agent target with a new dispatch intent atomically. `workflow_rework_test.go` covers accepted agent → manual review → agent attempt 2.
- Shared Run rework history schema and transcript rendering; `stage-review-control.tsx` requires a reason and impact acknowledgement. `e2e/sop-manual-rework.spec.ts` covers real browser rollback and subsequent completion.

- `packages/core/workflows/transition.test.tsx` and the transition mutation: manual rollback invalidates both Run/history and issue caches on success or uncertainty, stays workspace-scoped, and cannot inherit global automatic retry.

- Failed reviewer recovery/status: `core/internal/service/workflow_review_recovery.go`, `workflow_review_status.go`, `core/internal/handler/workflow_review_recovery.go`, `packages/views/workflows/components/agent-review-recovery-control.tsx`; audit migrations 316–319.


Cross-mode rework: `core/internal/service/workflow_rework.go`, `core/internal/handler/workflow_rework.go`, `packages/core/api/schemas.ts`, `packages/views/workflows/components/execution-rework-control.tsx`.

- Missing-decision recovery and late-decision serialization: `workflow_review_recovery.go`, `workflow_review_status.go`, `core/pkg/db/queries/workflow_review.sql`, and `workflow_review_recovery_test.go`; UI and controlled browser cases in `agent-review-recovery-control.test.tsx` / `e2e/sop-review-recovery.spec.ts`.


Cross-mode rollback configuration: packages/views/workflows/components/project-sop-designer.tsx and workflow-run-plan-dialog.tsx; actual form publishing in e2e/sop-cross-mode-configuration.spec.ts.


Rework policy evidence: `core/pkg/workflowcontract/rework_policy.go` validates the contract; `core/internal/service/workflow_rework.go` enforces source-stage budgets under the Run lock; `core/pkg/db/queries/workflow_rework.sql` counts initiations; `core/internal/service/workflow_execution.go` freezes policy; `packages/views/workflows/components/stage-execution-fields.tsx` edits bounded policy; `core/internal/service/workflow_rework_test.go` verifies frozen limits and idempotent replay.


Retry policy persistence: `core/pkg/workflowcontract/retry_policy.go` validates inputs; `core/internal/handler/workflow.go` normalizes and copies configuration; `core/internal/handler/workflow_plan.go` preserves plan edits; `core/internal/service/workflow_execution.go` freezes the policy. Migration 321 adds object-valued storage to both stage tables. Execution-task failure callbacks and sweepers now use `core/internal/service/workflow_retry_policy.go` through `task.go`; review-specific retry is connected through workflow_review_retry.go, workflow_review_status.go and workflow_review_recovery.go; explicit per-call deadlines are enforced by workflow_deadline.go.


Initial execution retry budget: `workflow_dispatch.go` parses the frozen policy before creating work; `BindWorkflowExecutionTask` in `core/pkg/db/queries/workflow_execution.sql` stores the explicit maximum atomically with the execution binding. Omitted limits retain task defaults. `TestWorkflowDispatchUsesFrozenInitialRetryBudget` covers disabled retry and a larger explicit budget after live configuration changes.


Deadline enforcement: `core/internal/service/workflow_deadline.go` scans running execution and reviewer calls every two seconds, reads the frozen timeout, and finalizes expired calls with failure reason `timeout`. Queue waiting does not consume this limit; retries start a new call deadline. `core/internal/daemon/daemon.go` (`shouldInterruptAgent` / `watchTaskCancellation`) already stops local execution on terminal server state and discards late output. The scan does not alter calls without an explicit timeout.


Retry configuration UI: `packages/views/workflows/components/stage-retry-fields.tsx` defines equal-width responsive controls and integer/dependency validation. `stage-execution-fields.tsx` shares them between the SOP designer and Run plan form; both parents reject invalid policy before save. Clearing the initial delay also clears its multiplier and cap.


Execution and rollback selectors use controlled open state and unmount closed popup content. The component regression and `e2e/sop-cross-mode-configuration.spec.ts` verify that selected options leave no residual listbox.


Run plan edits preserve an existing stage’s `required_skill_snapshots` when its selected skill references are unchanged. Changing a title, output setting or another stage must not refresh pinned skill content from the latest builtin/workspace version. `core/internal/handler/workflow_plan.go` enforces this; `TestWorkflowInstancePlanCanAdjustCurrentAndEmptyFutureStages` verifies an earlier skill revision survives a plan edit. Existing execution-based plan restrictions remain in place pending revision compatibility work.


Workspace skill candidates: members with existing skill-management permission may POST `/api/skills/{id}/revisions` with content, files, compatible_contracts and tool_dependencies. Source identity, author and hashes are derived by the server; extra identity/hash fields are rejected. This member endpoint does not accept agent/task identity headers. GET `/api/skills/{id}/revisions/{revisionId}` requires workspace membership and verifies the revision belongs to that source skill. Creating a candidate neither edits the live skill nor publishes it. Evidence: `core/internal/handler/skill_revision.go`, `core/internal/service/skill_revision.go`, `core/cmd/server/router.go` and `packages/core/api/skill-revision-schema.ts`. Evaluation/publication flows and agent proposal authority are still pending.


GET `/api/skills/{id}/revisions` returns summaries with `next_cursor`. Pass that UUID as `cursor` for older revisions; optional `page_size` is 1–100 (default 25). Both the list and cursor must belong to the source skill/workspace. Summaries omit bundle content; use revision detail to read it. Ordering uses creation time and UUID, so new inserts do not shift older cursor pages. Evidence: `ListSkillRevisionSummaries` and `GetSkillRevisionCursorInWorkspace` in `core/pkg/db/queries/skill_revision.sql`, listing index migration 325, and the shared client page schema.


The shared skill detail page exposes a Versions tab with cursor pagination, immutable revision detail, and candidate creation for existing skill editors. The candidate form starts from saved content and supporting files, accepts contract references and explicit tool versions, and submits through workspace-bound React Query mutations without automatic retry or live-skill writes. Creation and inspection dialogs share the responsive 4xl width. Sources: `packages/views/skills/components/skill-revisions-tab.tsx`, `packages/core/skills/revisions.ts`; UI coverage: `skill-revisions-tab.test.tsx`. Evaluation, publication and agent proposal authority remain separate pending work.


Workspace skill improvement proposals use POST `/api/skills/{id}/proposals` with `request_id`, distinct `baseline_revision_id` and `candidate_revision_id`, `category`, `reason`, `hypothesis`, and `applicable_scope`. Optional `source_execution_id` and `source_result_revision` identify evidence; a result revision requires an execution. The server resolves provenance and author, checks both revisions belong to the skill/workspace, and rejects supplied evidence/author fields. Categories are `skill_instruction`, `insufficient_input`, `tool_failure`, `contract_issue`, `model_capability`, `human_correction`, and `positive_example`. This endpoint currently requires a member with skill-management permission and rejects agent/task identity headers. An identical author/request replay returns the existing proposal; reusing the request ID with different content returns 409. GET `/api/skills/{id}/proposals/{proposalId}` and GET `/api/skills/{id}/proposals` are workspace-member reads; list pagination uses `cursor` and `page_size` (1–100, default 25). A proposal records analysis only; it neither publishes a skill nor approves an action or stage. Sources: `core/internal/handler/skill_improvement_proposal.go`, `core/internal/service/skill_improvement_proposal.go`, `packages/core/api/skill-proposal-schema.ts`, and `packages/core/skills/proposals.ts`. API boundary coverage: `TestSkillImprovementProposalHTTPBoundariesAndPagination`.


The shared skill detail page exposes Improvement proposals with list/detail views and a member creation form. Select distinct baseline/candidate revisions, compare their pinned content, classify feedback, and enter reason/hypothesis/scope. Optional execution/result references are verified by the API. Ambiguous submissions lock the submitted fields and reuse the same request ID and payload on retry; definitive 400/403/404 rejections allow correction. Form/detail dialogs share responsive 4xl sizing with the Versions flow. Evidence: `packages/views/skills/components/skill-proposals-tab.tsx`, its colocated tests, and `e2e/skill-proposals.spec.ts`. Run-origin selection links, evaluation and publication review are still separate pending work.
