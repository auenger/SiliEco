-- =====================
-- Autopilot CRUD
-- =====================

-- name: ListAutopilots :many
-- List rows carry three derived columns the list UI needs (trigger badges,
-- next run, last-run outcome) so the page never has to N+1 into the detail
-- endpoint. trigger_kinds/next_run_at only consider ENABLED triggers — the
-- columns answer "how does this fire today", not "what is configured".
-- last_run_status is COALESCEd to '' (never ran) because sqlc cannot infer
-- nullability through a scalar subquery; the handler maps '' back to omitted.
SELECT
  sqlc.embed(a),
  (
    SELECT array_agg(DISTINCT t.kind ORDER BY t.kind)
    FROM autopilot_trigger t
    WHERE t.autopilot_id = a.id AND t.enabled
  )::text[] AS trigger_kinds,
  (
    SELECT min(t.next_run_at)
    FROM autopilot_trigger t
    WHERE t.autopilot_id = a.id AND t.enabled AND t.kind = 'schedule'
  )::timestamptz AS next_run_at,
  COALESCE((
    SELECT r.status
    FROM autopilot_run r
    WHERE r.autopilot_id = a.id
    ORDER BY r.triggered_at DESC
    LIMIT 1
  ), '')::text AS last_run_status
FROM autopilot a
WHERE a.workspace_id = $1
  AND (
    (sqlc.narg('status')::text IS NULL AND a.status <> 'archived')
    OR a.status = sqlc.narg('status')
  )
ORDER BY a.created_at DESC;

-- name: GetAutopilot :one
SELECT * FROM autopilot
WHERE id = $1;

-- name: GetAutopilotInWorkspace :one
SELECT * FROM autopilot
WHERE id = $1 AND workspace_id = $2;

-- name: CreateAutopilot :one
INSERT INTO autopilot (
    workspace_id, title, description, assignee_type, assignee_id,
    status, execution_mode, task_title_template, project_id,
    created_by_type, created_by_id
) SELECT
    $1 AS workspace_id,
    $2 AS title,
    sqlc.narg('description') AS description,
    $3 AS assignee_type,
    $4 AS assignee_id,
    $5 AS status,
    $6 AS execution_mode,
    sqlc.narg('task_title_template') AS task_title_template,
    sqlc.narg('project_id') AS project_id,
    $7 AS created_by_type,
    $8 AS created_by_id
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM workspace AS ref_workspace_id WHERE ref_workspace_id.id = ($1) FOR KEY SHARE))
  AND ((sqlc.narg('project_id'))::uuid IS NULL OR EXISTS (SELECT 1 FROM project AS ref_project_id WHERE ref_project_id.id = (sqlc.narg('project_id')) FOR KEY SHARE))
 RETURNING *;

-- name: UpdateAutopilot :one
UPDATE autopilot SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    assignee_type = COALESCE(sqlc.narg('assignee_type'), assignee_type),
    assignee_id = COALESCE(sqlc.narg('assignee_id')::uuid, assignee_id),
    status = COALESCE(sqlc.narg('status'), status),
    execution_mode = COALESCE(sqlc.narg('execution_mode'), execution_mode),
    task_title_template = sqlc.narg('task_title_template'),
    project_id = sqlc.narg('project_id'),
    updated_at = now()
WHERE autopilot.id = $1

  AND ((sqlc.narg('project_id'))::uuid IS NULL OR EXISTS (SELECT 1 FROM project AS ref_project_id WHERE ref_project_id.id = (sqlc.narg('project_id')) FOR KEY SHARE))
RETURNING *;

-- name: ArchiveAutopilot :exec
UPDATE autopilot
SET status = 'archived', updated_at = now()
WHERE id = $1;

-- name: UpdateAutopilotLastRunAt :exec
UPDATE autopilot SET last_run_at = now(), updated_at = now()
WHERE id = $1;

-- =====================
-- Autopilot Rule Version (rule_owner attribution, SILI-4302 §3.4)
-- =====================

-- name: CreateAutopilotRuleVersion :one
-- Append one immutable rule-version snapshot on a substantive publish (create /
-- enable / resume / target / execution-mode change). published_by_* is the acting
-- member (or 'system' with NULL id for the failure monitor); config_summary is the
-- effective config at publish time. Dispatch reads the latest row for the autopilot
-- as the run's rule_owner accountable human.
INSERT INTO autopilot_rule_version (
    autopilot_id, workspace_id, published_by_type, published_by_id, config_summary
)
VALUES (
    @autopilot_id, @workspace_id, @published_by_type, sqlc.narg(published_by_id),
    COALESCE(sqlc.narg(config_summary), '{}'::jsonb)
)
RETURNING *;

-- name: GetActiveAutopilotRuleVersion :one
-- The active version is the newest published row for the autopilot. Scoped by
-- workspace_id per the workspace query rule; autopilot_id is globally unique so the
-- workspace filter is a guard, not the selector.
SELECT * FROM autopilot_rule_version
WHERE workspace_id = $1 AND autopilot_id = $2
ORDER BY created_at DESC
LIMIT 1;

-- =====================
-- Autopilot Trigger CRUD
-- =====================

-- name: ListAutopilotTriggers :many
SELECT * FROM autopilot_trigger
WHERE autopilot_id = $1
ORDER BY created_at ASC;

-- name: GetAutopilotTrigger :one
SELECT * FROM autopilot_trigger
WHERE id = $1;

-- name: CreateAutopilotTrigger :one
INSERT INTO autopilot_trigger (
    autopilot_id, kind, enabled, cron_expression, timezone,
    next_run_at, webhook_token, label, provider, event_filters,
    published_by_type, published_by_id
) SELECT
    $1 AS autopilot_id,
    $2 AS kind,
    $3 AS enabled,
    sqlc.narg('cron_expression') AS cron_expression,
    sqlc.narg('timezone') AS timezone,
    sqlc.narg('next_run_at') AS next_run_at,
    sqlc.narg('webhook_token') AS webhook_token,
    sqlc.narg('label') AS label,
    COALESCE(sqlc.narg('provider')::text, 'generic') AS provider,
    sqlc.narg('event_filters') AS event_filters,
    sqlc.narg('published_by_type') AS published_by_type,
    sqlc.narg('published_by_id') AS published_by_id
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM autopilot AS ref_autopilot_id WHERE ref_autopilot_id.id = ($1) FOR KEY SHARE))
 RETURNING *;

-- name: SetAutopilotTriggerPublisher :exec
-- Re-stamp a single trigger's responsible publisher after a substantive edit of
-- THAT trigger (cron / filter / enabled / webhook security). Future runs it fires
-- become accountable to this member (SILI-4302 trigger_owner transfer).
UPDATE autopilot_trigger
SET published_by_type = $2, published_by_id = $3, updated_at = now()
WHERE id = $1;

-- name: SetAutopilotTriggerPublishersByAutopilot :exec
-- Re-stamp ALL of an autopilot's triggers' responsible publisher after a substantive
-- AUTOPILOT-level edit (target / instructions / assignee / execution-mode / enable).
-- Such a change governs every trigger's future runs, so responsibility transfers to
-- the editing member for all of them; a per-trigger edit uses the single-trigger
-- variant so it never reassigns another trigger (SILI-4302).
UPDATE autopilot_trigger
SET published_by_type = $2, published_by_id = $3, updated_at = now()
WHERE autopilot_id = $1;

-- name: UpdateAutopilotTrigger :one
UPDATE autopilot_trigger SET
    enabled = COALESCE(sqlc.narg('enabled')::boolean, enabled),
    cron_expression = COALESCE(sqlc.narg('cron_expression'), cron_expression),
    timezone = COALESCE(sqlc.narg('timezone'), timezone),
    next_run_at = sqlc.narg('next_run_at'),
    label = COALESCE(sqlc.narg('label'), label),
    event_filters = COALESCE(sqlc.narg('event_filters'), event_filters),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteAutopilotTrigger :exec
WITH RECURSIVE cleanup_target_autopilot_trigger AS MATERIALIZED (
    SELECT autopilot_trigger.* FROM "autopilot_trigger" autopilot_trigger WHERE autopilot_trigger.id = $1
),
cleanup_target_webhook_delivery AS MATERIALIZED (
    SELECT child.* FROM "webhook_delivery" child WHERE EXISTS (SELECT 1 FROM cleanup_target_autopilot_trigger parent WHERE child."trigger_id" = parent."id")
),
cleanup_delete_webhook_delivery AS (
    DELETE FROM "webhook_delivery" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_webhook_delivery)
),
cleanup_detach_autopilot_run AS (
    UPDATE "autopilot_run" child SET "trigger_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_autopilot_trigger parent WHERE child."trigger_id" = parent."id") THEN NULL ELSE child."trigger_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_autopilot_trigger parent WHERE child."trigger_id" = parent."id"))
),
cleanup_detach_webhook_delivery AS (
    UPDATE "webhook_delivery" child SET "replayed_from_delivery_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_webhook_delivery parent WHERE child."replayed_from_delivery_id" = parent."id") THEN NULL ELSE child."replayed_from_delivery_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_webhook_delivery parent WHERE child."replayed_from_delivery_id" = parent."id")) AND NOT ((child."id") IN (SELECT "id" FROM cleanup_target_webhook_delivery))
)
DELETE FROM "autopilot_trigger" autopilot_trigger WHERE (autopilot_trigger."id") IN (SELECT "id" FROM cleanup_target_autopilot_trigger) ;


-- name: AdvanceTriggerNextRun :exec
UPDATE autopilot_trigger
SET next_run_at = sqlc.narg('next_run_at'),
    last_fired_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: GetWebhookTriggerByToken :one
-- Look up a webhook trigger by its public bearer token. Joined to autopilot
-- so the webhook handler can derive the workspace from the trigger's parent
-- without trusting any request header. The handler still re-loads the
-- Autopilot via GetAutopilot and cross-checks WorkspaceID matches the row's
-- autopilot_workspace_id.
SELECT t.*, a.workspace_id AS autopilot_workspace_id
FROM autopilot_trigger t
JOIN autopilot a ON a.id = t.autopilot_id
WHERE t.kind = 'webhook'
  AND t.webhook_token = $1;

-- name: TouchAutopilotTriggerFiredAt :exec
-- Bumps last_fired_at after a webhook fires, regardless of whether the
-- dispatch succeeded, was admission-skipped, or even if Autopilot status
-- transitioned to paused/disabled at exactly the wrong moment. Disabled /
-- paused early-return paths in the handler never call this.
UPDATE autopilot_trigger
SET last_fired_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: RotateAutopilotTriggerWebhookToken :one
-- Rotates the bearer token for a webhook trigger. Restricted to kind='webhook'
-- so an accidental call against a schedule/api trigger is a no-op (returns no
-- rows) rather than corrupting unrelated state.
UPDATE autopilot_trigger
SET webhook_token = $2,
    updated_at = now()
WHERE id = $1
  AND kind = 'webhook'
RETURNING *;

-- name: SetAutopilotTriggerWebhookToken :one
-- Sets the webhook token at creation time. CreateAutopilotTrigger inserts the
-- row first (using its full 8-arg signature), then this query attaches the
-- token. Splitting the create + token-set keeps the existing CreateAutopilotTrigger
-- query usable by the schedule path without forcing every caller to think
-- about webhook_token.
UPDATE autopilot_trigger
SET webhook_token = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetAutopilotTriggerSigningSecret :one
-- Writes the signing secret for a webhook trigger. Kept as a dedicated query
-- (not a field on UpdateAutopilotTrigger) so the request body for the
-- write-only endpoint only ever carries the secret value, with no risk of an
-- accidental log line leaking it alongside other fields. Restricted to
-- webhook triggers to avoid corrupting unrelated state.
UPDATE autopilot_trigger
SET signing_secret = sqlc.narg('signing_secret'),
    updated_at = now()
WHERE id = $1
  AND kind = 'webhook'
RETURNING *;

-- =====================
-- Autopilot Run Management
-- =====================

-- name: CreateAutopilotRun :one
-- squad_id is an attribution hook: set to the assignee squad when the
-- parent autopilot has assignee_type='squad', NULL otherwise. The executing
-- agent_id on agent_run still records who actually ran the work
-- (the squad leader); squad_id lets reports group by squad without a join.
--
-- planned_at carries the canonical UTC fire time for scheduled triggers
-- (source='schedule'); it stays NULL for manual / webhook / api sources
-- which have no canonical occurrence. Combined with the partial unique
-- index uq_autopilot_run_trigger_planned, this gives dispatch-layer
-- idempotency: a stale-steal retry at the same plan_time cannot create
-- a second run for the same (trigger_id, planned_at) pair (SILI-3551).
INSERT INTO autopilot_run (
    autopilot_id, trigger_id, source, status, trigger_payload, squad_id, planned_at,
    webhook_delivery_id
) SELECT
    $1 AS autopilot_id,
    sqlc.narg('trigger_id') AS trigger_id,
    $2 AS source,
    $3 AS status,
    sqlc.narg('trigger_payload') AS trigger_payload,
    sqlc.narg('squad_id') AS squad_id,
    sqlc.narg('planned_at') AS planned_at,
    sqlc.narg('webhook_delivery_id') AS webhook_delivery_id
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM autopilot AS ref_autopilot_id WHERE ref_autopilot_id.id = ($1) FOR KEY SHARE))
  AND ((sqlc.narg('trigger_id'))::uuid IS NULL OR EXISTS (SELECT 1 FROM autopilot_trigger AS ref_trigger_id WHERE ref_trigger_id.id = (sqlc.narg('trigger_id')) FOR KEY SHARE))
  AND ((sqlc.narg('squad_id'))::uuid IS NULL OR EXISTS (SELECT 1 FROM squad AS ref_squad_id WHERE ref_squad_id.id = (sqlc.narg('squad_id')) FOR KEY SHARE))
 RETURNING *;

-- name: GetAutopilotRunByTriggerAndPlanned :one
-- Idempotent lookup used by DispatchAutopilotForPlan to detect a
-- crash-during-dispatch retry: if a row already exists for this
-- (trigger_id, planned_at), the caller reuses it instead of creating a
-- duplicate. The partial unique index covers the same key, so a race
-- between "look up then insert" still resolves to a single row — this
-- query is just the fast path that lets us skip the INSERT when we
-- can see the prior row clearly. Returns no rows for the (much more
-- common) first-time dispatch.
SELECT * FROM autopilot_run
WHERE trigger_id = $1
  AND planned_at = $2
LIMIT 1;

-- name: GetAutopilotRunByWebhookDelivery :one
SELECT * FROM autopilot_run
WHERE webhook_delivery_id = $1
LIMIT 1;

-- name: RecoverPartialAutopilotRun :exec
-- Recovers a partial-state autopilot_run from a crashed first attempt
-- (the runner wrote the run row but died before creating the downstream
-- task/run) so that a subsequent DispatchAutopilotForPlan call can
-- create a fresh run at the same (trigger_id, planned_at).
--
-- Setting planned_at = NULL clears the partial-unique slot held by
-- uq_autopilot_run_trigger_planned, letting the new INSERT proceed.
-- The row stays in autopilot_run as a FAILED record (with a recovery
-- reason) so ops still see the abandoned attempt in the run history —
-- it is not silently deleted.
UPDATE autopilot_run
SET status = 'failed',
    completed_at = now(),
    failure_reason = 'recovered partial dispatch (crashed before downstream creation)',
    planned_at = NULL
WHERE id = $1;

-- name: GetAutopilotRun :one
SELECT * FROM autopilot_run
WHERE id = $1;

-- name: ListAutopilotRuns :many
SELECT * FROM autopilot_run
WHERE autopilot_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: UpdateAutopilotRunTaskCreated :one
UPDATE autopilot_run
SET status = 'task_created', task_id = $2
WHERE autopilot_run.id = $1

  AND (($2)::uuid IS NULL OR EXISTS (SELECT 1 FROM task AS ref_task_id WHERE ref_task_id.id = ($2) FOR KEY SHARE))
RETURNING *;

-- name: UpdateAutopilotRunRunning :one
UPDATE autopilot_run
SET status = 'running', run_id = $2
WHERE autopilot_run.id = $1

  AND (($2)::uuid IS NULL OR EXISTS (SELECT 1 FROM agent_run AS ref_run_id WHERE ref_run_id.id = ($2) FOR KEY SHARE))
RETURNING *;

-- name: UpdateAutopilotRunCompleted :one
UPDATE autopilot_run
SET status = 'completed', completed_at = now(), result = sqlc.narg('result')
WHERE id = $1
RETURNING *;

-- name: UpdateAutopilotRunFailed :one
UPDATE autopilot_run
SET status = 'failed', completed_at = now(), failure_reason = $2
WHERE id = $1
RETURNING *;

-- name: UpdateAutopilotRunSkipped :one
-- Marks an autopilot_run as skipped without enqueueing any run. Used by the
-- pre-flight admission check when the assignee agent's runtime is offline:
-- creating an task / run in that state would just pile a doomed job onto
-- agent_run (the canonical "持续给离线 local agent 入队" symptom from
-- SILI-1899). Recording the skip + reason gives the UI / failure monitor / ops
-- a paper trail without polluting the failure ratio.
UPDATE autopilot_run
SET status = 'skipped', completed_at = now(), failure_reason = $2
WHERE id = $1
RETURNING *;

-- name: UpdateAutopilotRunSkippedWithResult :one
UPDATE autopilot_run
SET status = 'skipped',
    completed_at = now(),
    failure_reason = $2,
    result = sqlc.narg('result')
WHERE id = $1
RETURNING *;

-- =====================
-- Scheduler Queries
-- =====================

-- name: ListSchedulableAutopilotTriggers :many
-- Lists every schedule trigger the autopilot_schedule_dispatch JobSpec
-- should consider this tick. Returns just the columns the scheduler's
-- scope provider + PlansForScope hook need; the full trigger row is
-- re-loaded by the handler so a trigger update between scope-list and
-- handler-run sees the latest enabled / cron values.
--
-- last_fired_at is read so the planner hook can anchor cold-start
-- enumeration on the most recent successful fire (set by either the
-- legacy goroutine before the new scheduler took over, or the new
-- scheduler's own post-dispatch advance — AdvanceTriggerNextRun, falling
-- back to TouchAutopilotTriggerFiredAt on a cron parse error). Without it,
-- a trigger that was created days ago and fired by the legacy code
-- looks like a brand-new trigger to the new scheduler on first tick
-- and the half-open `(created_at, now]` enumeration replays the most
-- recent already-fired occurrence — exactly the post-deploy
-- spurious-fire reported on SILI-3551 dev.
--
-- Filters out webhook / api triggers, disabled triggers, paused/archived
-- autopilots, and any trigger missing its cron expression. ORDER BY id
-- keeps the per-tick scope list stable across replicas.
SELECT t.id, t.autopilot_id, t.cron_expression, t.timezone, t.created_at, t.last_fired_at
FROM autopilot_trigger t
JOIN autopilot a ON a.id = t.autopilot_id
WHERE t.kind = 'schedule'
  AND t.enabled = TRUE
  AND a.status = 'active'
  AND t.cron_expression IS NOT NULL
  AND t.cron_expression <> ''
ORDER BY t.id;

-- =====================
-- Run Queue (run_only mode)
-- =====================

-- name: CreateAutopilotAgentRun :one
-- run_only autopilot dispatch. Attribution depends on the trigger:
--   * schedule / webhook / api: no human authorized the run, so originator_user_id
--     stays NULL and accountable_user_id is the rule_owner (the publisher of the
--     autopilot's active rule version), with rule_version_id recording the snapshot
--     (SILI-4302 §3.4) — the accountable-diverges-from-originator case.
--   * manual: a member clicked "run now", a direct human action, so originator and
--     accountable are BOTH that member (originator_source='direct_human'); no rule
--     version is involved (SILI-4302 §4).
-- When no version/publisher resolves on the non-manual path, the caller passes NULL
-- accountable + originator_source='unattributed' so the row is still not a
-- NULL-source bypass (SILI-4302 §2).
INSERT INTO agent_run (
    agent_id, runtime_id, task_id, status, priority, autopilot_run_id, trigger_summary,
    originator_user_id, accountable_user_id, rule_version_id,
    originator_source, trigger_evidence_kind, trigger_evidence_ref_id
) SELECT
    $1 AS agent_id,
    $2 AS runtime_id,
    NULL AS task_id,
    'queued' AS status,
    $3 AS priority,
    $4 AS autopilot_run_id,
    sqlc.narg(trigger_summary) AS trigger_summary,
    sqlc.narg(originator_user_id) AS originator_user_id,
    sqlc.narg(accountable_user_id) AS accountable_user_id,
    sqlc.narg(rule_version_id) AS rule_version_id,
    sqlc.narg(originator_source) AS originator_source,
    sqlc.narg(trigger_evidence_kind) AS trigger_evidence_kind,
    sqlc.narg(trigger_evidence_ref_id) AS trigger_evidence_ref_id
WHERE (($2)::uuid IS NULL OR EXISTS (SELECT 1 FROM agent_runtime AS ref_runtime_id WHERE ref_runtime_id.id = ($2) FOR KEY SHARE))
  AND (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM agent AS ref_agent_id WHERE ref_agent_id.id = ($1) FOR KEY SHARE))
  AND (($4)::uuid IS NULL OR EXISTS (SELECT 1 FROM autopilot_run AS ref_autopilot_run_id WHERE ref_autopilot_run_id.id = ($4) FOR KEY SHARE))

RETURNING *;

-- name: GetAutopilotAgentRunByRun :one
-- Repairs the narrow run_only crash window where the run INSERT committed
-- but the following autopilot_run.run_id update did not.
SELECT * FROM agent_run
WHERE autopilot_run_id = $1
ORDER BY created_at
LIMIT 1;

-- =====================
-- Run lookup by linked entities
-- =====================

-- name: GetAutopilotRunByTask :one
SELECT * FROM autopilot_run
WHERE task_id = $1 AND status IN ('task_created', 'running')
LIMIT 1;

-- name: FailAutopilotRunsByTask :exec
-- Fails active autopilot runs linked to a given task.
-- Must be called BEFORE task deletion (ON DELETE SET NULL clears task_id).
UPDATE autopilot_run
SET status = 'failed', completed_at = now(), failure_reason = 'linked task was deleted'
WHERE task_id = $1
  AND status IN ('task_created', 'running');

-- =====================
-- Failure-rate auto-pause
-- =====================

-- name: SelectAutopilotsExceedingFailureThreshold :many
-- Find active autopilots whose recent run failure rate exceeds the threshold.
-- Counts only "real" terminal runs (completed | failed). 'skipped' is
-- excluded from BOTH numerator and denominator: an admission-skipped run
-- (e.g. assignee runtime offline at dispatch time, SILI-1899) is neither a
-- success nor a failure, so it must not dilute the failure ratio (which
-- would let a 100%-failing autopilot mask itself behind a wall of skips)
-- nor inflate it. task_created/running are still excluded so in-flight
-- work isn't penalised.
-- Used by the failure monitor to auto-pause sustained-failure autopilots
-- (the canonical example from SILI-1336 was an autopilot scheduled every 5 min
-- that 100% failed for days, burning ~1.5k useless runs per week).
WITH stats AS (
    SELECT autopilot_id,
           count(*) FILTER (WHERE status IN ('completed', 'failed')) AS total,
           count(*) FILTER (WHERE status = 'failed') AS failed
    FROM autopilot_run
    WHERE created_at >= sqlc.arg('since')::timestamptz
    GROUP BY autopilot_id
)
SELECT a.id, a.workspace_id, a.title, a.assignee_id,
       a.created_by_type, a.created_by_id,
       s.total::bigint  AS total_runs,
       s.failed::bigint AS failed_runs
FROM autopilot a
JOIN stats s ON s.autopilot_id = a.id
WHERE a.status = 'active'
  AND s.total >= sqlc.arg('min_runs')::bigint
  AND s.failed::float8 / NULLIF(s.total, 0)::float8 >= sqlc.arg('fail_ratio_threshold')::float8
ORDER BY s.failed DESC, a.id ASC;

-- name: SystemPauseAutopilot :one
-- Atomically pauses an autopilot only if it is currently active. Returns no
-- rows when the autopilot was already paused/archived (or another worker
-- raced first), letting the caller treat that as a benign no-op rather than
-- an error.
UPDATE autopilot
SET status = 'paused', updated_at = now()
WHERE id = $1 AND status = 'active'
RETURNING *;

-- =====================
-- Autopilot Subscribers
-- =====================

-- name: ListAutopilotSubscribers :many
-- ORDER BY created_at keeps chip rendering stable across refreshes.
SELECT * FROM autopilot_subscriber
WHERE autopilot_id = $1
ORDER BY created_at ASC, user_id ASC;

-- name: AddAutopilotSubscriber :exec
INSERT INTO autopilot_subscriber (autopilot_id, user_type, user_id)
VALUES ($1, $2, $3)
ON CONFLICT (autopilot_id, user_type, user_id) DO NOTHING;

-- name: DeleteAutopilotSubscribersForAutopilot :exec
-- Paired with a re-insert loop to implement full-replace PATCH semantics.
DELETE FROM autopilot_subscriber
WHERE autopilot_id = $1;

-- =====================
-- Autopilot Collaborators
-- =====================

-- name: ListAutopilotCollaborators :many
-- ORDER BY created_at keeps row rendering stable across refreshes.
SELECT * FROM autopilot_collaborator
WHERE autopilot_id = $1
ORDER BY created_at ASC, user_id ASC;

-- name: AddAutopilotCollaborator :one
-- Re-granting an existing collaborator is a no-op that refreshes granted_by,
-- so the call is idempotent from the API boundary.
INSERT INTO autopilot_collaborator (autopilot_id, user_type, user_id, granted_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (autopilot_id, user_type, user_id)
    DO UPDATE SET granted_by = EXCLUDED.granted_by
RETURNING *;

-- name: DeleteAutopilotCollaborator :exec
DELETE FROM autopilot_collaborator
WHERE autopilot_id = $1 AND user_type = $2 AND user_id = $3;

-- name: DeleteAutopilotCollaboratorsForAutopilot :exec
-- Application-layer cleanup run inside the autopilot delete transaction.
DELETE FROM autopilot_collaborator
WHERE autopilot_id = $1;

-- name: IsAutopilotCollaborator :one
SELECT EXISTS (
    SELECT 1 FROM autopilot_collaborator
    WHERE autopilot_id = $1 AND user_type = 'member' AND user_id = $2
) AS is_collaborator;

-- name: ListAutopilotIDsForCollaborator :many
-- Powers the per-row can_write flag on the list endpoint without an N+1.
SELECT autopilot_id FROM autopilot_collaborator
WHERE user_type = 'member' AND user_id = $1;
