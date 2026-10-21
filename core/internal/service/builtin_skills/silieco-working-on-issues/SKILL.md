---
name: silieco-working-on-issues
description: "Use when working on a Silieco issue after the runtime has provided the trigger context — to apply the product contracts the runtime brief does not encode: how PR linking differs from close intent, how to read a linked PR's real state via the pull-requests CLI, which metadata keys are high-signal, what status changes trigger on the server, and how sub-issue create status (todo vs backlog) controls whether assigned agents start immediately."
user-invocable: false
allowed-tools: Bash(silieco *), Bash(git *), Bash(gh *)
---

# Working on Silieco issues

Product contracts the runtime brief does not fully encode: PR linking vs close
intent, reading linked-PR state, metadata keys, status side effects, and
sub-issue enqueue behavior.

For building mention links, load `silieco-mentioning` instead — not this skill.

Every contract below is traced to source in
`references/working-on-issues-source-map.md`.

## PR linking and close intent are two distinct contracts

The GitHub webhook runs two separate scans over an incoming PR. They are not the
same gate and they read different fields.

**Linking** scans the PR **title, body, OR branch** for a routable issue key
(`PREFIX-NUMBER`, e.g. `SILI-2759`). Each match writes an issue ↔ PR link row.
This is the link that `silieco issue pull-requests` reads back — but see the
reference-only rule below: a key that appears **only** as a bare mention in the
body is linked yet hidden from that list.

```text
SILI-2759: add built-in issue working skill        # title prefix → links, shown
agent/matt/sili-2759-working-on-issues             # branch ref   → links, shown
```

**Close intent** is stricter and is a separate scan over **title or body only —
never the branch**. It fires only for a key placed immediately after a closing
keyword (`Closes` / `Fixes` / `Resolves`, optional `:` then whitespace). That
adjacency is what sets the link row's close-intent flag, the gate that
auto-advances the issue to `done` when the PR merges.

```text
Closes SILI-2759                                    # links AND records close intent
Fixes SILI-2759
Resolves SILI-2759
Fix login SILI-2759                                 # links only — keyword not adjacent
```

Consequence: a bare title prefix or a branch reference links the PR but does not
close the issue on merge. A closing keyword immediately adjacent to the issue key
records close intent; on merge, that close intent can move the linked issue to
`done`.

**Reference-only links (hidden from the PR list).** A key that appears **only**
as a bare mention in the body — no closing keyword, and not in the title or
branch — still writes a link row, but the row is flagged `reference_only` and
**excluded from `silieco issue pull-requests`** (and the issue's right-side PR
list in the UI). This keeps passing mentions like `Related SILI-2759` or
`Follow up in SILI-2759` from surfacing an unrelated PR as if it were working on
that issue. To make a PR show up for an issue, put the key in the title, the
branch, or after a closing keyword in the body — not as a loose body reference.

```text
Closes SILI-2759 in the body                        # links and shown
Related to SILI-2759 in the body (no title/branch)  # links but reference_only → hidden
```

### Default for code-changing issue work

When an issue run changes code in a checked-out GitHub repo, the default handoff
is to open or update a PR before posting the final Silieco issue comment, unless
the user explicitly asked for a local-only change or no PR. This is a default, not
an unconditional command: if no code changed, say no PR is needed; if PR creation
is blocked by auth, failing tests, or missing remote state, report that blocker
instead of pretending the run is complete.

Use a routable issue key in the PR title, body, or branch so the webhook can link
the PR back to the issue. If the PR should close the issue on merge, put the key
immediately after a closing keyword in the title or body, for example:

```text
SILI-2759: fix login redirect        # links only
Closes SILI-2759                     # links and records close intent
```

In the final issue comment, include the PR URL when a PR exists. If the task did
not produce a PR because no code changed or the user asked not to create one, say
that explicitly.

## Reading a linked PR's real state

When a step depends on PR state, query Silieco's link table — do not infer it
from branch names, GitHub search, memory, or `pr_url` metadata (which can be
stale).

```bash
silieco issue pull-requests <issue-id> --output json
```

Returns `{"pull_requests": [...]}`. Each element exposes:

- `number`, `html_url`, `title`
- `state` — the PR lifecycle as a **single enum**, one of `merged`, `closed`,
  `draft`, `open`. There is no separate `draft` or `merged` boolean in the
  response; the server folds them into `state` (merged wins, then closed, then
  draft, else open).
- `merged_at` — non-null once merged; a second confirmation of `state: merged`.
- `provider` — `github`, `forgejo`, `gitea`, or `gitlab`.
- `mergeable_state` — mirrors GitHub (`clean` / `dirty` surfaced; other values
  round-trip as unknown; retained for compatibility).
- GitHub API snapshot fields: `snapshot_available`, `mergeable`,
  `merge_state_status`, `checks_rollup`, `checks_total`, `checks_passed`,
  `checks_failed`, `checks_running`, `failed_check_names`,
  `snapshot_fetched_at`, and `snapshot_stale`. `snapshot_available == true`
  means the feature is enabled and the snapshot matches the PR's current head.
  Only then does `checks_rollup == null` mean "no checks"; false means the
  snapshot feature is disabled, has not fetched yet, or only has an old head.
- `checks_conclusion` — coarse CI compatibility status: `passed`, `failed`,
  `pending`, or `null`. GitHub derives it from the current API snapshot;
  Forgejo/Gitea/GitLab derive it from webhook commit statuses. Backed by the
  provider-appropriate check counts.

So "is it merged?" is `state == "merged"` (or `merged_at != null`); "is it still
a draft?" is `state == "draft"`; coarse CI status is `checks_conclusion`.

If the command returns no linked PRs after a PR was opened, the link scanner did
not observe a routable issue key in the PR title/body/branch — or the only match
was a bare body mention, which links as `reference_only` and is hidden from this
list (see the reference-only rule above).

## Metadata: high-signal keys only

Metadata is durable issue state. Reading metadata is safe. Writing a metadata key
is a state mutation and should be tied to an explicit task requirement to record
that state for later readers or runs.

High-signal keys (reuse these names so queries stay consistent):

- `pr_url`
- `pr_number`
- `pipeline_status`
- `deploy_url`
- `external_issue_url`
- `waiting_on`
- `blocked_reason`
- `decision`

Not metadata: logs, summaries, files touched, timestamps, attempt counts,
investigation notes. Those belong in the result comment.

```bash
silieco issue metadata set <issue-id> --key pr_url --value <url>
silieco issue metadata delete <issue-id> --key <stale-key>
```

`--value` is JSON-parsed by default (bool/number are sniffed); pass `--type
string|number|bool` to force a type.

## Custom properties: typed workflow state

Workspaces may define custom issue properties (Severity, Environment, QA
Status, ...). Properties are the typed, user-visible sibling of metadata:
values are validated against the definition (select options, date format,
http(s) URL), visible in the issue sidebar, and addressed by name.

- Read what exists before writing: `silieco property list` shows the catalog;
  `silieco issue property list <issue-id>` shows values set on the issue.
- Set values by property name and option name — the CLI translates to ids:

```bash
silieco issue property set <issue-id> --name Environment --value staging
silieco issue property set <issue-id> --name Platforms --value "iOS,Android"
silieco issue property unset <issue-id> --name Environment
```

- A validation error lists the legal options — fix the value and retry.
- Definitions may include an optional catalog icon for visual identification;
  it does not change the property's type or value validation.
- Agents cannot create or edit property definitions (owner/admin humans only).
  If a needed property does not exist, propose it in a comment instead.
- Property vs metadata: if the value is workflow state a human should see and
  filter by, and a definition exists, prefer the property. Metadata stays the
  free-form scratchpad for run state (`pr_url`, `waiting_on`, ...).

## Status changes have server side effects

A status change is not cosmetic — the server enqueues or skips agent work based
on it. These are the contracts, not advice:

- **`backlog`** parks an agent-assigned issue: the assignee is set but no task
  fires. Moving `backlog → todo` (or any non-done/non-cancelled status) enqueues
  the assigned agent then.
- **`in_progress` / `in_review` on assignment runs** are agent-managed CLI
  mutations, not `StartTask` / `CompleteTask` side effects. The assignment
  runtime brief asks ordinary agents for `todo`/`backlog` → `in_progress` then
  `in_review` when they have delivered. Squad leaders share the opening
  `in_progress` step on the first assignment turn, keep the parent there while
  members work, and only move to `in_review` when a later re-trigger confirms
  the overall goal is met.
- **`in_review`** is an accepted issue status. Some workflows use it while a PR
  is open and awaiting review; moving to it is an explicit mutation.
- **`done`** on a child issue posts a system comment on its parent. If a PR
  carries close intent (`Closes SILI-XXXX`), it advances the issue to `done`
  itself on merge — you do not also need to flip it manually.
- **`cancelled`** is a terminal, user-driven decision to close the issue. Like
  `done` it enqueues no new agent work, but it does **not** stop tasks already in
  flight — a run in progress keeps going (SILI-4465). To stop a running task,
  cancel the task itself.
- **Failed issue-triggered tasks** may roll an issue from `in_progress` back to
  `todo` when no active task / retry remains — that is the main server-owned
  status write on the agent-run path.

## Sub-issues: `todo` starts work now, `backlog` parks it

On an agent-assigned issue, create status decides whether the assignee fires
immediately. A non-backlog status (e.g. `todo`) enqueues the agent at create
time; `backlog` sets the assignee without triggering.

Parallel children — all start now:

```bash
silieco issue create --title "..." --parent <issue-id> --assignee <agent> --status todo
```

Strictly serial children — park later steps, promote one at a time:

```bash
silieco issue create --title "Step 2: ..." --parent <issue-id> --assignee <agent> --status backlog
silieco issue status <child-id> todo   # promote when the previous step is truly done
```

Creating every serial step as `todo` enqueues the whole chain at once.

### Stages: order sub-issues into barrier groups

`--stage <N>` (N ≥ 1) groups sub-issues under the same parent into ordered
stages. The parent assignee is woken **once, when a whole stage finishes** —
i.e. every sub-issue in the lowest unfinished stage has reached a terminal
status (`done`/`cancelled`). A completion that does not close a stage is silent
(no comment, no wake). A sibling set with **no** stages is one implicit stage,
so the parent is woken once when the *last* sub-issue finishes — not on every
child.

Advancement is agent-driven: the server only detects the closed barrier and
wakes the parent assignee, who then decides whether to promote the next stage's
`backlog` sub-issues to `todo`.

```bash
# Stage 1 runs now; later stages parked until promoted
silieco issue create --title "Research A" --parent <id> --assignee <agent> --stage 1 --status todo
silieco issue create --title "Research B" --parent <id> --assignee <agent> --stage 1 --status todo
silieco issue create --title "Build"      --parent <id> --assignee <agent> --stage 2 --status backlog
silieco issue create --title "Ship"       --parent <id> --assignee <agent> --stage 3 --status backlog
```

When both Stage 1 sub-issues finish you (the parent assignee) are woken with a
"Stage 1 complete" comment. Inspect the layout, then promote the next stage:

```bash
silieco issue children <parent-id>             # sub-issues grouped by stage
silieco issue status <stage-2-child-id> todo   # promote when its deps are met
```

Read each sub-issue's description before promoting and only promote items whose
stated dependencies are met; if a description conflicts with the parent's
breakdown, leave it `backlog` and comment to confirm first.

## SOP workflow assignment is an atomic pair

An SOP-bound Task carries both a workflow run UUID and a Stage UUID. Pass the
coordinates together when creating a Task or attaching an existing Task:

```bash
silieco task create --title "..." --workflow-instance <run-uuid> --workflow-stage <stage-uuid>
silieco task update <task-id> --workflow-instance <run-uuid> --workflow-stage <stage-uuid>
```

The Stage UUID is the immutable Stage copy owned by that specific workflow run,
not the source SOP version's Stage UUID. The server validates the Run and Stage
as one pair and aligns the Task to the run's Project. To detach an existing Task
cleanly while keeping the Task itself, explicitly clear both coordinates:

```bash
silieco task update <task-id> --workflow-instance "" --workflow-stage ""
```

Do not confuse `--workflow-stage <uuid>` with `--stage <N>`: the former places
a Task in an SOP run; the latter groups sub-tasks into parent wake barriers.

When the current Task is already SOP-bound, every continuation, child, or
follow-up Task inherits the same workflow run unless the user explicitly asks
for work outside that SOP. Choose the target Stage from the SOP Stage catalog in
the runtime context: prefer a phase named or semantically described by the user
(for example, “prototype”, “MVP”, or “原型” maps to the corresponding Stage),
then an explicit next/successor request maps to the reported next Stage, and
same-phase work stays in the current Stage. Always include both workflow flags
on create. A human assignee, `--status backlog`, or a request not to auto-run
changes execution behavior only; none of them authorizes dropping SOP binding.
Attaching the new Task to a later Stage does not transition the workflow run.

## Incorrect → correct

PR title (link the issue):

```text
Fix login redirect                  # incorrect — no issue key, won't link
SILI-2759: fix login redirect        # correct — links the PR
```

Serial / phased sub-issues (don't start the whole chain at once):

```bash
# incorrect — all fire immediately, no ordering
silieco issue create --title "Step 2" --parent <issue-id> --assignee <agent> --status todo
silieco issue create --title "Step 3" --parent <issue-id> --assignee <agent> --status todo

# correct — stage them; Stage 1 runs, later stages park and are promoted as
# each stage's barrier closes
silieco issue create --title "Step 1" --parent <issue-id> --assignee <agent> --stage 1 --status todo
silieco issue create --title "Step 2" --parent <issue-id> --assignee <agent> --stage 2 --status backlog
silieco issue create --title "Step 3" --parent <issue-id> --assignee <agent> --stage 3 --status backlog
```

## References

`references/working-on-issues-source-map.md` — accurate `file:line` for every
contract above: the `pull-requests` CLI and route, the PR response field list,
`derivePRState`, the two-path link (`extractIdentifiers`) vs close-intent
(`extractClosingIdentifiers`) proof, the backlog enqueue lines, child-done
notify, the stage column / `stageBarrierClosed` barrier and the `--stage` /
`issue children` CLI, and the metadata CLI. Re-derive before depending on an
exact line.


## SOP execution and results

For SOP stage work, read [SOP execution and results](references/sop-execution.md)
for preflight, versioned submission, artifact retrieval, readiness, retries and
Run mutation rules. Use the execution's current main call when submitting a
result; readiness is diagnostic and does not authorize advancing the stage.
Automatic stages require execution-bound dispatch or technical retry. Ordinary,
mention and deferred fallback calls validate Run/stage membership transactionally.

Human approval through the execution decision API atomically accepts and advances
the Run. Missing next-stage input preserves a blocked execution without dispatch;
database failures roll back approval. See the SOP execution reference.
Frozen `none` gates are checked by the server acceptance worker; completing a
call alone is insufficient without valid delivery and all required work done.
Input-blocked Runs support member correction with an expected-old-input check;
see [input bindings](references/sop-input-bindings.md) before using the API or
`workflow correct-inputs` command.
Agent/hybrid gates freeze their reviewer identity at execution creation; `@self`
means that execution’s executor. The review worker dispatches bound reviewer
calls and retries eligible technical failures without reusing an old approval.
Agent review endpoints require the bound reviewer call identity; ordinary member
or executor calls cannot submit a reviewer decision. See the SOP reference.

For a bound SOP reviewer call, use `silieco workflow agent-review` and
`silieco workflow agent-decision` with its exact request, result revision and
evidence hash. Read the result and artifacts first. Preserve call credentials;
see [SOP execution](references/sop-execution.md) for commands and acceptance rules.

SOP and Run editors share execution, contract and input-binding controls. Contract
drafts apply locally before publishing or saving the parent plan; existing
executions retain their frozen configuration. Rework targets use stable stage
keys, and a removed or reordered dependency must be repaired explicitly.

Rework also invalidates affected manual-stage issues and gate decisions. Their
original status, outcome and content remain historical. Do not edit, retry or
create child work under an invalidated issue; create work for the fresh stage
attempt. The Run transcript retains the invalidated history separately.

Manual stage transition requires the exact `state_token` from the reviewed Run
as `expected_state_token`. A changed Run requires reopening review. Never fetch
a replacement token and automatically resubmit an approval.

Manual rejection to a configured earlier stage now creates a rework record,
invalidates affected issue/approval history and cancels calls transactionally.
An agent target receives a fresh business attempt and dispatch intent. Supply a
nonempty rollback reason and review the impact; do not reuse old delivery.

Failed SOP reviewer calls have a member-only diagnostic and explicit Run-initiator recovery flow; see [SOP execution](references/sop-execution.md#explicit-recovery-of-failed-reviewer-calls). Agents must not invoke that recovery endpoint or reuse a previous call's decision.


Agent-stage rework may return to its configured earlier manual stage; a manual target has no agent execution identity. See the SOP execution reference for the response contract.

A completed SOP reviewer call without a decision may be explicitly recovered by the Run initiator; a completed call with an approved or rejected decision may not. Recovery never rewrites the historical process status.


SOP creation and Run plan configuration allow an agent stage to select an earlier manual stage as its rollback target. Rollback targets and accepted agent-result input sources are separate lists.


Stage `rework_policy.max_reworks` limits rework initiations per source stage in one Run (0–100; zero disables rework, omission leaves no configured cap). Agent execution uses its frozen policy. Replaying the same request does not consume another initiation. See the SOP execution reference.


Stage `retry_policy` is validated and preserved in template, Run and execution snapshots. Execution-task failure callbacks and background retries enforce frozen attempt limits and backoff. Initial execution tasks also record the frozen attempt budget. Review calls also use frozen retry budgets and backoff; explicit manual recovery retains its separately granted calls. Explicit call timeouts now run from started_at and enter the existing failure/retry path. Shared SOP and Run-plan forms expose retry counts, call timeout and bounded backoff, with validation before saving.


Initial execution retry budget: `workflow_dispatch.go` parses the frozen policy before creating work; `BindWorkflowExecutionTask` in `core/pkg/db/queries/workflow_execution.sql` stores the explicit maximum atomically with the execution binding. Omitted limits retain task defaults. `TestWorkflowDispatchUsesFrozenInitialRetryBudget` covers disabled retry and a larger explicit budget after live configuration changes.


Deadline enforcement: `core/internal/service/workflow_deadline.go` scans running execution and reviewer calls every two seconds, reads the frozen timeout, and finalizes expired calls with failure reason `timeout`. Queue waiting does not consume this limit; retries start a new call deadline. `core/internal/daemon/daemon.go` (`shouldInterruptAgent` / `watchTaskCancellation`) already stops local execution on terminal server state and discards late output. The scan does not alter calls without an explicit timeout.


Retry configuration UI: `packages/views/workflows/components/stage-retry-fields.tsx` defines equal-width responsive controls and integer/dependency validation. `stage-execution-fields.tsx` shares them between the SOP designer and Run plan form; both parents reject invalid policy before save. Clearing the initial delay also clears its multiplier and cap.


Run plan edits preserve an existing stage’s `required_skill_snapshots` when its selected skill references are unchanged. Changing a title, output setting or another stage must not refresh pinned skill content from the latest builtin/workspace version. `core/internal/handler/workflow_plan.go` enforces this; `TestWorkflowInstancePlanCanAdjustCurrentAndEmptyFutureStages` verifies an earlier skill revision survives a plan edit. Existing execution-based plan restrictions remain in place pending revision compatibility work.


Workspace skill candidates: members with existing skill-management permission may POST `/api/skills/{id}/revisions` with content, files, compatible_contracts and tool_dependencies. Source identity, author and hashes are derived by the server; extra identity/hash fields are rejected. This member endpoint does not accept agent/task identity headers. GET `/api/skills/{id}/revisions/{revisionId}` requires workspace membership and verifies the revision belongs to that source skill. Creating a candidate neither edits the live skill nor publishes it. Evidence: `core/internal/handler/skill_revision.go`, `core/internal/service/skill_revision.go`, `core/cmd/server/router.go` and `packages/core/api/skill-revision-schema.ts`. Evaluation/publication flows and agent proposal authority are still pending.


GET `/api/skills/{id}/revisions` returns summaries with `next_cursor`. Pass that UUID as `cursor` for older revisions; optional `page_size` is 1–100 (default 25). Both the list and cursor must belong to the source skill/workspace. Summaries omit bundle content; use revision detail to read it. Ordering uses creation time and UUID, so new inserts do not shift older cursor pages. Evidence: `ListSkillRevisionSummaries` and `GetSkillRevisionCursorInWorkspace` in `core/pkg/db/queries/skill_revision.sql`, listing index migration 325, and the shared client page schema.


The shared skill detail page exposes a Versions tab with cursor pagination, immutable revision detail, and candidate creation for existing skill editors. The candidate form starts from saved content and supporting files, accepts contract references and explicit tool versions, and submits through workspace-bound React Query mutations without automatic retry or live-skill writes. Creation and inspection dialogs share the responsive 4xl width. Sources: `packages/views/skills/components/skill-revisions-tab.tsx`, `packages/core/skills/revisions.ts`; UI coverage: `skill-revisions-tab.test.tsx`. Evaluation, publication and agent proposal authority remain separate pending work.


Workspace skill improvement proposals use POST `/api/skills/{id}/proposals` with `request_id`, distinct `baseline_revision_id` and `candidate_revision_id`, `category`, `reason`, `hypothesis`, and `applicable_scope`. Optional `source_execution_id` and `source_result_revision` identify evidence; a result revision requires an execution. The server resolves provenance and author, checks both revisions belong to the skill/workspace, and rejects supplied evidence/author fields. Categories are `skill_instruction`, `insufficient_input`, `tool_failure`, `contract_issue`, `model_capability`, `human_correction`, and `positive_example`. This endpoint currently requires a member with skill-management permission and rejects agent/task identity headers. An identical author/request replay returns the existing proposal; reusing the request ID with different content returns 409. GET `/api/skills/{id}/proposals/{proposalId}` and GET `/api/skills/{id}/proposals` are workspace-member reads; list pagination uses `cursor` and `page_size` (1–100, default 25). A proposal records analysis only; it neither publishes a skill nor approves an action or stage. Sources: `core/internal/handler/skill_improvement_proposal.go`, `core/internal/service/skill_improvement_proposal.go`, `packages/core/api/skill-proposal-schema.ts`, and `packages/core/skills/proposals.ts`. API boundary coverage: `TestSkillImprovementProposalHTTPBoundariesAndPagination`.


The shared skill detail page exposes Improvement proposals with list/detail views and a member creation form. Select distinct baseline/candidate revisions, compare their pinned content, classify feedback, and enter reason/hypothesis/scope. Optional execution/result references are verified by the API. Ambiguous submissions lock the submitted fields and reuse the same request ID and payload on retry; definitive 400/403/404 rejections allow correction. Form/detail dialogs share responsive 4xl sizing with the Versions flow. Evidence: `packages/views/skills/components/skill-proposals-tab.tsx`, its colocated tests, and `e2e/skill-proposals.spec.ts`. Run-origin selection links, evaluation and publication review are still separate pending work.
