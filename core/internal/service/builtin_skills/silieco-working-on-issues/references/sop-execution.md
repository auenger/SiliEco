# SOP execution and results

## SOP result preflight

When a stage supplies a `silieco.stage/v1` contract, check its local result before
submission:

```bash
silieco workflow preflight --contract contract.json --result result.json
```

The result is an object with `data` and `artifacts`. Each artifact includes
`name`, `kind`, `media_type`, `schema_version`, `attachment_id`, and `checksum`
(`sha256:` plus the lowercase digest). Use uploaded attachment UUIDs, not URLs.
The command always prints JSON with `valid`, `scope: "local_preflight"`, and
`violations` (JSON Pointer `path`, `code`, `message`), and exits nonzero for
invalid results. Invalid contracts or unreadable files instead return an error.

This command only checks data and manifest structure. It does not upload, fetch
attachment bytes, authorize resources, submit results, approve gates, or advance
a Run. The server must verify resources and checksums and accept an immutable
result version separately. Never add approval, revision, or execution identity
fields to the result: these are server-owned. Current manual SOP transitions do
not yet implement this result protocol.

SOP dispatcher-created tasks carry an immutable execution-round binding in the
server queue. The dispatcher creates the main issue and task atomically; retrying
the same delivery does not create another main issue. The queue refuses this
bound task while its Run is inactive/archived, its stage or plan revision is
stale, its issue is parked/done/cancelled, or its initiating member has lost
invocation access. Completing a task still does not approve the stage or advance
the Run. Stage configuration and authoritative result submission are separate
capabilities; local preflight never triggers dispatch.

Workflow definition and version APIs accept independent stage fields
`execution_mode: "manual" | "agent"` and `executor_id`. Manual is the default
and has no executor. Agent execution requires an executor UUID in the workspace,
`output_spec.contract` in the `silieco.stage/v1` format, and
`completion_rule: {"type":"validated_result"}` (filled when omitted). Execution
and gate modes are independent. The definition editor preserves these fields
when inheriting a configured SOP; it does not yet provide an execution selector.

The Run creation API accepts an `inputs` object. Starting an automatic first
stage fixes those inputs and creates its execution and dispatch intent in the
same transaction as the Run. If an input data contract rejects the inputs, the
Run is not created. Entering an automatic stage from a manual stage also creates
a dispatch intent transactionally. Current input contracts support data fields;
artifact input bindings remain unsupported and are rejected.

Do not use the legacy transition API to bypass an automatic stage: it requires
accepted execution results. Validated result submission is available; stage acceptance and automatic
advancement are still being implemented. Plans that already have execution rounds cannot be edited
through the ordinary plan adjustment API; explicit rework is a separate path.


## Versioned SOP result submission

Read `GET /api/workflow-instances/{id}/executions` to obtain the execution ID,
frozen configuration and current `result_revision`. Submit to
`POST /api/workflow-instances/{id}/executions/{executionId}/results` with
`{"expected_revision":0,"submission":{"data":{},"artifacts":[]}}`, replacing
zero with the revision you read and supplying the stage's required data/artifacts.
Read a stored result using `GET .../results/{revision}`. The corresponding CLI commands are shown below; `workflow preflight` remains
an offline check and does not submit.

Only the Run initiator or its bound executor/main task can submit. Agent requests
must carry their authenticated agent and task identity; unrelated task identities
are rejected. A stale result revision returns 409. Contract/content failures
return 422 with violations. Every attachment must belong to the execution's main
issue, one of its comments, or main task, in the same workspace.

The server reads managed-storage bytes, verifies SHA-256 and byte count, checks
the declared wire format, and freezes a separate copy per revision. Supported
formats are UTF-8 plain text, Markdown, CSV, and JSON; files must be nonempty and
at most 16 MiB. JSON rejects duplicate keys. Format validation does not establish
business correctness or validate a domain-specific artifact schema.

A saved result moves the execution to `awaiting_review`; this is not acceptance.
The API returns result data, identity and an artifact manifest. Each manifest
entry has its stable artifact ID, immutable revision, checksum and authenticated
`content_url`. Download reads the frozen copy and verifies its checksum before
returning bytes. Review decisions, input-version handoff and the completion
barrier remain pending.


```bash
silieco workflow executions <run-id>
silieco workflow submit <run-id> <execution-id> --expected-revision 0 --result result.json
silieco workflow result <run-id> <execution-id> --revision 1
silieco workflow artifact <run-id> <execution-id> <artifact-revision-id> --revision 1 --file report.md
```

Use exact UUIDs. `--revision` refers to the result revision; the last positional
argument of `artifact` is the manifest entry's `id`, not its stable `artifact_id`.
Downloads create a new local file and refuse to overwrite an existing file.
A storage checksum mismatch fails the download; never substitute the original
attachment or a different revision when that happens.

Stage transcripts keep execution-bound calls under the stage that produced
that execution, even if the issue is later moved. Unbound manual calls follow
the issue's current stage. Moving an issue does not migrate or reclassify an
automatic execution's history.

Use `silieco workflow readiness <run-id> <execution-id>` (or
`GET /api/workflow-instances/{id}/executions/{executionId}/readiness`) to inspect
completion requirements. The response has `scope: "readiness_snapshot"`,
`result_revision`, `ready_for_review`, blocker codes and incomplete/active counts.
It checks the current plan/stage, submitted result, completed main issue,
successful main call, other required issues and active stage calls. Cancelled
issues/calls are not successful delivery. Reopened/new issues are checked anew.
This snapshot is not approval and cannot authorize advancement; a review or
advance transaction must recheck the completion barrier.

Manual stage approval requires every currently attached required issue to be
`done`; `cancelled` is not successful delivery. The transition rechecks existing
issue rows inside its transaction and holds their locks until commit. Concurrent
stage membership changes still need the broader execution mutation protocol;
do not treat this check as a completed automatic acceptance implementation.

The dedicated SOP task attach/detach APIs serialize membership changes with
Run transitions. They lock affected projects and Runs before the issue and
revalidate membership after waiting. Closed or archived source/destination Runs
reject these changes. Other generic issue mutation paths are still being brought
under the same protocol; this does not yet establish a complete acceptance barrier.

Ordinary issue updates and batch updates also acquire the SOP mutation locks.
They reject stale observed issue versions and recheck Run closure, stage identity
and project consistency before writing. Reload after a 409; do not repeatedly
apply an old whole-issue snapshot. Batch updates retain their existing partial
success behavior and omit rejected items from the updated set.

Creating an issue with an SOP assignment now locks the Run in the shared issue
creation transaction before allocating its number or writing dependent records.
A Run that closes while the request is waiting rejects the creation. This applies
to all entry points using IssueService.Create, including ordinary and quick-create
HTTP flows. Automatic state writers and deletion still need the remaining protocol.

GitHub merged-PR completion and failed-call issue resets use the same locked,
stale-version-checked status update as ordinary edits. A delayed automatic write
cannot overwrite a newer issue snapshot. Single-issue deletion now locks SOP
membership and commits issue deletion, active-call cancellation and linked
Autopilot cleanup together; cancellation notifications are sent only after commit.
Closed/archived SOP tasks reject deletion. Batch deletion and call-queue mutation
protocols remain separate pending work for complete stage acceptance.

Batch deletion now uses the same transactional deletion service as single-item
deletion. Each item commits independently: failed items preserve their issues,
calls and linked cleanup state, while successful items emit notifications after
commit. Closed or archived SOP Runs also reject batch deletion of their issues.

Unbound calls on manual SOP stages are only claimable while that stage is the
Run's current stage, the Run is active and unarchived, and the issue is neither
backlog nor terminal. Calls queued for a future stage wait. An automatic stage
requires an execution-bound call; manually enqueuing an unbound call cannot
bypass that requirement. Non-SOP issue and chat calls retain their normal rules.

Technical retries inherit the original `workflow_stage_execution_id` and keep
its business `attempt` unchanged. The execution's `main_task_id` moves atomically
to the new retry call; `retry_of_task_id` preserves call history. Use that current
call identity when submitting results. A stale parent cannot create another
retry, and an inactive or superseded Run does not create a retry. Refusing the
retry does not undo the failed call. If a call fails after submitting a result,
a retry returns the execution to queued and must submit a new result revision
before readiness can become true again.

Ordinary assignee, explicit mention, thread-parent and squad-leader call enqueue
now validates stage membership under the Run/issue
transaction and publishes queued notifications after commit. Automatic stages
reject this unbound path; dispatch and technical retry use their execution-bound
paths. Preparation of attribution and tool overlays happens before taking the
transaction connection; the executor/runtime is rechecked before insertion.
Explicit mention/delegation retains its target agent even when the issue is
assigned to a different agent or member. Deferred fallback insertion uses the
same Run/issue transaction and validation, preserving its fire time and primary
call reference. It stays deferred and emits no queued notification until
promotion. Readiness counts deferred calls as active blockers.

## Human review evidence foundation

The backend review service now records human decisions against a specific
execution/result revision and a hash of the locked delivery snapshot. It requires
an explicitly configured human reviewer and current workspace membership. The
snapshot includes required issues, related call identities/statuses, immutable
result/artifact revisions, execution configuration and frozen execution inputs. Changed
evidence requires a fresh review; identical retries return the existing decision.
Hybrid gates cannot bypass their agent step through this human-only service.

The decision API now records approval, accepts the exact result and advances the
Run in one transaction, resolving the next automatic stage's versioned inputs
and durable dispatch intent. A database/configuration failure rolls back approval as well. Missing next-stage
input instead preserves predecessor acceptance and enters a blocked next
execution without dispatch.
Rejection records a decision without advancing; rework is not implemented yet. Do not interpret historical gate-decision rows as
current acceptance without checking their execution, revision and basis.


Human review API (authenticated as the configured member, with no `X-Agent-ID`
or `X-Task-ID` headers):

- `GET /api/workflow-instances/{run}/executions/{execution}/review` returns
  `execution_id`, `result_revision`, `basis_hash`, reviewed issue summaries and
  call identities/statuses. Read that exact result revision using the result API.
- `POST /api/workflow-instances/{run}/executions/{execution}/decisions` accepts
  `expected_result_revision`, `expected_basis_hash`, `outcome` (`approved` or
  `rejected`) and optional `note`. Do not send actor identity or approval flags.
- The response identifies `execution_id`, `result_revision`, `review_step` and
  `basis_hash`. Identical retries return the same decision. Conflicts require
  refreshing and reviewing the changed evidence, not automatically retrying
  approval with a new hash. A historical decision alone is not stage acceptance.

Review responses omit internal configuration, call credentials and storage keys.
The shared Web/desktop Run panel now reads these endpoints for automatic-stage delivery review.


CLI human review:

```bash
silieco workflow review <run-id> <execution-id>
silieco workflow result <run-id> <execution-id> --revision <reviewed-revision>
silieco workflow decision <run-id> <execution-id> \
  --result-revision <reviewed-revision> --basis-hash <reviewed-hash> \
  --outcome approved --note "Evidence checked"
```

Use `--outcome rejected` to record rejection. Result revision, basis hash and
outcome must be explicit. The CLI does not fetch a new hash or retry approval
when the server reports changed evidence. Read and review the updated delivery
before issuing another decision. These commands use the configured member's
credentials; do not strip agent identity headers to impersonate a human.
Successful approval advances the Run, or completes it after the final stage.
Identical retries return the accepted decision without another dispatch.


In the Run panel, **Review delivery** opens the current execution's exact result
revision and artifact manifest. Downloads use the authenticated version route.
The configured human reviewer must acknowledge the displayed version before
selecting **Approve and advance** or recording rejection. Changed review evidence clears that
acknowledgement; invalid result responses disable decisions. A failed or
uncertain decision requires reopening and reviewing the delivery again.
Manual-stage validation also treats cancelled required tasks as incomplete.


For an execution whose frozen gate is `none`, the server periodically rechecks
submitted delivery, required issues, related calls, Run state and executor access.
It accepts and advances transactionally only after all completion checks pass.
Missing next-stage inputs persist a blocked execution without dispatch; the
accepted predecessor remains accepted.
Missing results, cancelled required work and paused Runs do not pass. Restarted
or concurrent scans use durable execution state and cannot duplicate acceptance.
No human decision is synthesized for this mode. Human, agent and hybrid gates
are excluded from the none-gate scan; the review scan handles agent/hybrid gates.


Agent/hybrid gate identity is now frozen during execution creation. `@self`
resolves to that execution's executor ID, independent of later issue assignment.
An explicit reviewer must exist in the workspace, be unarchived and invocable by
the Run initiator. Hybrid additionally requires an existing member in
`human_decider`. Invalid or unavailable review identities prevent new execution
creation; an already-created execution keeps its original snapshot on retry.
The background review scan uses these frozen identities for preparation and dispatch.


The internal PrepareAgentReview service persists a pending review request with
reviewer ID, execution ID, result ID/revision and a complete evidence hash and
snapshot. It rechecks delivery completeness and live permissions under the Run
lock. Identical preparations reuse the same request; changed evidence invalidates
older pending requests. The worker dispatches pending requests using the binding below.
Do not treat a pending review request as an approval or a running reviewer.


Review-call binding is distinct from stage-execution binding. A queue row cannot
carry both roles. A review request points to one current call and retains
previous calls for retry history. Review calls are
excluded from execution-completion counts and delivery fingerprints, so creating
a review call does not invalidate the evidence it reviews. Ordinary claim and
technical retry SQL paths exclude review calls. TaskService uses the dedicated
reviewer claim path below; the dedicated retry worker handles technical failures.


The internal DispatchAgentReview service can now queue and bind one reviewer
call. It prepares runtime overlays before taking the Run lock, then rechecks
current evidence, reviewer access and runtime identity. The call uses a fresh
session and a handoff identifying the exact request, result revision and evidence
hash. Delivery issue assignment stays unchanged. Queue creation and request
binding commit together; repeated dispatch returns the bound request. The background
worker invokes this service. The dedicated TaskService claim path can
run a queued review call, and the bound decision API is available below.


Internal agent decision services now validate request identity, reviewer identity,
bound call, result revision and evidence hash. Only a running/completed reviewer
call can record approval or rejection; queued calls and executor calls cannot.
Recording approval does not advance while the reviewer is running. Acceptance
requires a successfully completed reviewer call and unchanged evidence. Agent
mode then uses the atomic acceptance transaction. Hybrid unlocks human review
only for the same evidence after agent approval and call completion, and still
requires the human decision to advance. The acceptance worker scans at startup and
every two seconds, preparing/dispatching reviews and accepting completed agent approvals.


Agent review API, using the bound reviewer call's existing `X-Agent-ID` and
`X-Task-ID` identity:

- `GET /api/workflow-instances/{run}/executions/{execution}/reviews/{request}`
  returns safe request identity, result ID/revision, reviewer ID, status and
  evidence hash. It omits the internal evidence snapshot. Only the bound running
  or completed reviewer can read it; an executor call cannot substitute.
- `POST /api/workflow-instances/{run}/executions/{execution}/reviews/{request}/decisions`
  accepts `expected_result_revision`, `expected_basis_hash`, `outcome` and optional
  `note`. Never send actor identity in the body. Same-evidence repeated decisions
  return the recorded decision while the execution remains current. After acceptance,
  an identical approval can also return the existing decision when the same bound
  completed call, accepted result and evidence still match. This never advances
  again and does not authorize a different outcome or note. A conflict
  requires fresh review, not automatic refresh/resubmission.

Recording a decision while the reviewer is running still does not advance the
Run. Worker acceptance waits for call completion. Use the bound review commands below.


TaskService now has a dedicated reviewer claim fallback after ordinary task
claiming. It revalidates request evidence and permissions under the Run lock,
respects agent capacity and same-issue concurrency, and claims only the bound
queued call. Token finalization and start revalidate again, including the exact
claim timestamp, runtime and issue identity. A paused or changed Run cannot issue
new review credentials or start review work. Technical review retries use the dedicated review worker and remain
excluded from the ordinary execution retry path.

The paginated review scan skips incomplete or inaccessible deliveries and resumes
from durable state after restart. Repeated scans reuse requests and bound calls.
Agent acceptance publishes the workflow update only after its transaction commits.
Hybrid is never automatically accepted; its human API still rechecks completed
agent approval. The designated `human_decider` can now open the shared delivery
review dialog; the form appears only after the server returns valid review evidence.
Use Refresh delivery to recheck readiness after the reviewer finishes. The agent
`decider` never substitutes for `human_decider`. Technical failure recovery uses the bound-call retry path below.


Inside a dispatched reviewer call, preserve `SILIECO_AGENT_ID` and
`SILIECO_TASK_ID` and use the identities supplied in the handoff:

```bash
silieco workflow agent-review <run-id> <execution-id> <request-id>
silieco workflow result <run-id> <execution-id> --revision <result-revision>
silieco workflow artifact <run-id> <execution-id> <artifact-revision-id> --revision <result-revision> --file <new-path>
silieco workflow agent-decision <run-id> <execution-id> <request-id> --result-revision <result-revision> --basis-hash <basis-hash> --outcome approved --note "Evidence checked"
```

Use `--outcome rejected` with an explanation when evidence does not satisfy the
frozen contract. Read the actual result and artifacts before deciding; an evidence
hash alone is not a review. Never use the human `decision` command from a reviewer
call. The CLI requires both call identity fields, preserves the exact revision
and hash, and returns conflicts without refreshing or retrying a decision.
Approval is a recorded review, not proof of stage acceptance or Run completion.


Accepted agent-decision replay still requires an unarchived, invocable reviewer
and the original completed reviewer call. Hybrid replay also checks the accepted
human decision. This is response recovery for an already recorded approval;
it does not restart a failed reviewer or bypass expired task credentials.


Reviewer failure is persisted on its call even for infrastructure failures.
The ordinary task retry path excludes reviewer calls before preparing a clone;
the dedicated review scan handles retryable failed calls. Failure reporting does not add a delivery
issue comment, which would change the evidence hash and accidentally trigger a
new request. Inspect the failed call for its error. An approval recorded by a
failed call cannot pass acceptance.


The review worker retries infrastructure failures using a fresh call and session,
keeping the same request, result version and evidence. It rechecks current Run
state, reviewer access and runtime, and obeys the task retry budget and backoff.
Paused Runs and disabled/exhausted retries cannot dispatch. Non-retryable failures
remain failed and cannot pass acceptance. The retry records parent/retry lineage;
its new decision is stored separately with `review_task_id`. Old calls retain
their history but cannot decide for the current request. Even an old approved
decision from a failed call cannot satisfy the new call's review step.


New issues attached to an executed SOP stage carry a business execution binding.
Same-stage edits preserve it; explicit moves resolve the destination attempt and
detach clears the current binding. Current review completeness and evidence
exclude issues/calls bound to other attempts, while legacy unbound stage work
still counts. Ordinary edits and task enqueue cannot reuse an older attempt
after a newer one exists. This supplies the isolation needed for explicit rework;
the rework service invalidates target/downstream attempts; the Run initiator can
open Rework from the stage delivery panel.


The internal ReworkExecution service lets the Run initiator request a new business
attempt for the current agent stage or its frozen configured rollback target.
It invalidates latest target/downstream executions, cancels active calls and
outbox actions, revokes call credentials and invalidates review requests in one
transaction. Old artifacts and decisions remain as history, not usable inputs.
Repeated identical source-execution requests return the same new attempt; a
different target/reason conflicts. Later downstream dispatch creates fresh
attempts from the invalidation marker. The member HTTP rework entry is available below; the Run panel provides the Rework dialog.


Member rework API: `POST /api/workflow-instances/{run}/executions/{source}/rework`
with `expected_revision`, `target_stage_id` and a nonempty `reason` (at most
4000 bytes). Only the Run initiator can use this action; agent/task identity
headers are rejected. The response identifies the source, new execution, Run,
target stage, attempt and current status. Identical requests are idempotent.
Conflicts require another explicit review. The shared client validates all
returned identities, performs no optimistic transition and never retries
rework automatically; it invalidates workflow and issue queries on settle.


The Rework dialog offers the current stage and its configured eligible rollback
stage, lists stages that must advance again, and requires a reason plus explicit
acknowledgement of stopping affected work. Changing the target clears the
acknowledgement. Submission locks fields and dismissal; conflicts lock the form
until it is closed and the current Run is reviewed again. Query refreshes do not
clear a failed submission into an editable form. Review, input recovery and
rework operation dialogs share their width and aligned footer actions.


The definition editor and Run plan editor now share Stage execution fields.
Agent mode requires an available explicit execution agent, independent of the
reviewer. Enabling it preserves an existing output contract or initializes a
minimal object contract, and sets validated_result as the completion rule.
This starter contract is not a substitute for defining required data/artifacts.
Past/current Run execution fields are read-only; server plan revision and
executed-plan protections remain authoritative. The shared contract/binding editor is described below. Configuration dialogs
share a responsive wide layout.

The shared input/output editor now supports top-level typed fields and required
flags, output artifact requirements, and explicit Run-input/accepted-result
bindings. Advanced JSON retains nested schemas, enum/range constraints and
existing contract details. Applying a draft does not publish a SOP or revise a
Run; the parent save still passes authoritative backend validation. Invalid
references are never silently rebound. Switching to manual execution clears the
executor; explicit rollback targets survive inheritance and use stable keys.
Input artifact and project-resource binding resolvers remain unsupported.

The Run-start dialog accepts an initial JSON input object and rejects malformed
or non-object drafts. Its pending guard prevents duplicate creates. Missing
required stage inputs still use the server's blocked-input recovery path.
Automatic-stage forms show the validated-delivery completion rule; manual task
status/evaluation controls apply only to manual stages.

Rework marks affected issues with `workflow_invalidated_by_rework_id` and
manual gate decisions with `invalidated_by_rework_id`, including downstream
manual stages that have no agent execution yet. Original issue status, decision
outcome and content are retained for history. Invalidated issues are excluded
from current stage delivery checks and the active board; the stage transcript
shows them separately and labels invalidated decisions. Ordinary issue edits,
child creation and technical retries cannot reuse that work. Issue list, grouped
list and search responses preserve the marker. Workflow events also refresh
issue caches so another open client sees the invalidation.

Retry insertion and rework serialize on issue membership. Failure-triggered and
sweeper retries acquire the source/current Run locks before locking calls; a
late failure cannot resurrect a call cancelled by rework. An identical rework
replay does not invalidate work created after the original request committed.

Manual transition (`POST /api/workflow-instances/{run}/transition`) now requires
`expected_state_token`, copied from the reviewed Run response
(`state_token`). It identifies Run ID, current stage, plan revision, status and
full-precision state update time. The server checks it again under the Run lock.
Missing or stale tokens return 409, including when a Run has returned to the
same stage after a state change. This token guards the Run state; it is not an
execution result/evidence hash or an approval credential. Existing membership,
gate identity and required-work checks still apply.

The manual review dialog captures the token when opened and disables both
decisions if a refreshed Run changes it. Missing/malformed response tokens also
disable submission. Failed or uncertain submissions require closing and
reviewing again; neither the client nor mutation retries a decision. A synchronous
submission guard prevents a second click before pending state renders.

Rework now has its own durable `workflow_rework` identity, independent of agent
execution. Issue `workflow_invalidated_by_rework_id` and decision
`invalidated_by_rework_id` reference that record. Existing execution-sourced
invalidation is migrated with its origin retained. Run detail exposes safe
`reworks` history (source/target stages, optional source execution, plan revision,
actor, reason and timestamp), without exposing internal state-precondition tokens.

Manual rejection to the configured earlier stage requires `target_stage_id`,
`expected_state_token` and a nonempty `note` of at most 4000 bytes. The transaction
records rework, invalidates all affected manual/agent work and approval history,
cancels calls and review requests, revokes credentials and changes the current
stage. If the target is an agent stage it creates a fresh execution and dispatch
intent in that same transaction. A manual target does not fabricate an agent
execution. The dialog requires a reason and impact acknowledgement; approval
remains a separate action. Missing reasons and stale state cannot initiate rollback.
New manual work is checked normally; invalidated historical work remains visible
in the transcript and cannot satisfy a new stage delivery barrier.

Manual transition also refreshes all workspace Run/history and issue queries
on settlement, including an uncertain response. This does not rely on receiving
the initiating client's WebSocket echo and never changes cached stage state optimistically.


### Explicit recovery of failed reviewer calls

A member can inspect `GET /api/workflow-instances/{id}/executions/{executionId}/review-status`.
The response exposes the current review request, revision, basis, call identity,
phase and bounded recovery capability; it excludes raw errors, session IDs,
runtime overlays and evidence snapshots. Unknown phases must disable recovery.

Only the Run initiator may submit
`POST /api/workflow-instances/{id}/executions/{executionId}/reviews/{requestId}/recover`
with `failed_task_id`, `expected_result_revision`, `expected_basis_hash` and a
nonempty `reason` of at most 4000 bytes. Agent/task credentials cannot use either
member endpoint. Recovery rechecks the current Run, execution, evidence and live
reviewer access under the Run lock. Pending automatic retry prevents an extra
manual call. The action grants exactly three calls including technical retries,
records `workflow_review_recovery`, revokes old credentials and creates a fresh
review session. Moving the reviewer runtime clears the previous host workdir.
Identical concurrent/repeated requests return the same audit record and call;
a different reason or identity conflicts. The new call needs its own decision.
Completed calls without a decision are also explicitly recoverable. The server
checks absence of a decision for the same request, basis and call under the Run
lock, and the insert repeats that guard. Existing approved or rejected decisions
prevent recovery of completed calls. The old process status remains completed;
`failed_task_id` identifies the unsuccessful review call, which may have completed
without fulfilling its review protocol. Recovery and a late decision are serialized
so only one can take effect. Automatic technical retry still requires a failed
process call; missing decisions require an explicit member action.

The shared recovery dialog uses the same operation width as review/rework,
requires a reason and acknowledgement, freezes the selected call identity, and
locks after stale or uncertain results. Recovery mutations never automatically
resubmit or optimistically change Run state.


Agent-sourced rework now supports the configured earlier manual target. The same transaction records rework, invalidates affected work and moves the Run to the manual stage. The response includes `rework_id`, `status: awaiting_work`, `execution_id: null` and `attempt: null`; no fake agent execution or dispatch is created. Agent targets retain their execution identity, attempt and execution status. Replays resolve the independent rework record and never repeat invalidation or change the current stage again. After fresh manual work is reviewed, advancing into an invalidated agent stage creates a new agent attempt.


The SOP designer and Run plan form expose all preceding stages as rollback targets, including manual stages for agent execution. Save validation uses stage order and stable keys. Agent-result input bindings still list agent predecessors only; a manual rollback target does not create a result binding.


SOP and Run stages carry `rework_policy: {max_reworks: N}` with an integer from 0 through 100. Omitted limits retain existing behavior; each rework still requires an explicit authorized action. Zero disables rework. The shared transaction counts independent rework audit records by workspace, Run and source stage before any invalidation. Agent sources use the execution snapshot policy; manual sources use the current stage policy. Exhausted limits return a conflict without cancelling work or changing the Run. Identical request replay resolves the original audit before checking the limit. Configuration forms preserve the policy, validate integer bounds and expose it for both manual and agent stages.
