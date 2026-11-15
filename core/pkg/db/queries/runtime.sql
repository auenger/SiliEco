-- name: ListAgentRuntimes :many
SELECT * FROM agent_runtime
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetAgentRuntime :one
SELECT * FROM agent_runtime
WHERE id = $1;

-- name: GetAgentRuntimes :many
-- Batch variant of GetAgentRuntime (SILI-4257): loads every runtime in the
-- input set in one round trip so the machine-level batch claim handler can
-- resolve+authorize all of a daemon's runtimes without one point query per
-- runtime. Rows are returned only for ids that exist; the caller matches them
-- back by id and skips any that are missing.
SELECT * FROM agent_runtime
WHERE id = ANY(@ids::uuid[]);

-- name: LockAgentRuntime :one
-- Lock before reading runtime dependents. Runtime-binding writes explicitly
-- take KEY SHARE on this row, so they either finish before the cleanup snapshot
-- or wait and fail their existence guard after deletion commits.
SELECT * FROM agent_runtime
WHERE id = $1
FOR UPDATE;

-- name: GetAgentRuntimeForWorkspace :one
SELECT * FROM agent_runtime
WHERE id = $1 AND workspace_id = $2;

-- name: UpsertAgentRuntime :one
-- (xmax = 0) AS inserted distinguishes a fresh insert (true) from an upsert
-- that updated an existing row (false). Analytics reads this to fire
-- runtime_registered/runtime_ready only on first-time registration.
INSERT INTO agent_runtime (
    workspace_id,
    daemon_id,
    name,
    runtime_mode,
    provider,
    status,
    device_info,
    metadata,
    owner_id,
    last_seen_at
) SELECT
    $1 AS workspace_id,
    $2 AS daemon_id,
    $3 AS name,
    $4 AS runtime_mode,
    $5 AS provider,
    $6 AS status,
    $7 AS device_info,
    $8 AS metadata,
    $9 AS owner_id,
    now() AS last_seen_at
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM workspace AS ref_workspace_id WHERE ref_workspace_id.id = ($1) FOR KEY SHARE))
  AND (($9)::uuid IS NULL OR EXISTS (SELECT 1 FROM "user" AS ref_owner_id WHERE ref_owner_id.id = ($9) FOR KEY SHARE))

-- Built-in runtimes carry no profile_id. The arbiter is the partial unique
-- index from migration 121 (WHERE profile_id IS NULL); the predicate must be
-- spelled out so Postgres selects that partial index, not the custom-runtime
-- one on (workspace_id, daemon_id, profile_id).
ON CONFLICT (workspace_id, daemon_id, provider) WHERE profile_id IS NULL
DO UPDATE SET
    name = EXCLUDED.name,
    runtime_mode = EXCLUDED.runtime_mode,
    status = EXCLUDED.status,
    device_info = EXCLUDED.device_info,
    metadata = EXCLUDED.metadata,
    owner_id = COALESCE(EXCLUDED.owner_id, agent_runtime.owner_id),
    last_seen_at = now(),
    updated_at = now()
RETURNING *, (xmax = 0) AS inserted;

-- name: UpsertAgentRuntimeWithProfile :one
-- Custom-runtime registration: a daemon resolved a workspace runtime_profile's
-- command_name on PATH and is registering an instance of it. The arbiter is the
-- partial unique index from migration 120 (WHERE profile_id IS NOT NULL), so a
-- single daemon can host the built-in provider AND any number of custom
-- profiles of the same protocol family. provider stays the protocol family so
-- run routing (agent.New(provider)) is unchanged; profile_id is the stable
-- identity. (xmax = 0) AS inserted mirrors UpsertAgentRuntime.
WITH profile_guard AS MATERIALIZED (
    SELECT id FROM runtime_profile
    WHERE id = $10 AND workspace_id = $1
    FOR KEY SHARE
), workspace_guard AS MATERIALIZED (
    SELECT id FROM workspace WHERE id = $1 FOR KEY SHARE
), owner_guard AS MATERIALIZED (
    SELECT id FROM "user" WHERE id = $9 FOR KEY SHARE
)
INSERT INTO agent_runtime (
    workspace_id,
    daemon_id,
    name,
    runtime_mode,
    provider,
    status,
    device_info,
    metadata,
    owner_id,
    profile_id,
    last_seen_at
) SELECT $1 AS workspace_id, $2 AS daemon_id, $3 AS name, $4 AS runtime_mode,
    $5 AS provider, $6 AS status, $7 AS device_info, $8 AS metadata,
    $9 AS owner_id, $10 AS profile_id, now() AS last_seen_at
WHERE EXISTS (SELECT 1 FROM profile_guard)
  AND EXISTS (SELECT 1 FROM workspace_guard)
  AND ($9::uuid IS NULL OR EXISTS (SELECT 1 FROM owner_guard))
ON CONFLICT (workspace_id, daemon_id, profile_id) WHERE profile_id IS NOT NULL
DO UPDATE SET
    name = EXCLUDED.name,
    runtime_mode = EXCLUDED.runtime_mode,
    provider = EXCLUDED.provider,
    status = EXCLUDED.status,
    device_info = EXCLUDED.device_info,
    metadata = EXCLUDED.metadata,
    owner_id = COALESCE(EXCLUDED.owner_id, agent_runtime.owner_id),
    last_seen_at = now(),
    updated_at = now()
RETURNING *, (xmax = 0) AS inserted;

-- name: UpdateAgentRuntimeVisibility :one
-- Toggles a runtime between 'private' (only owner can bind agents) and
-- 'public' (any workspace member can). Default for new rows is 'private'
-- (see migration 083). Gated at the handler layer to owner / workspace
-- admin only.
UPDATE agent_runtime
SET visibility = @visibility, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: UpdateAgentRuntimeCustomName :one
-- Sets or clears a runtime's user-facing custom name (SILI-4217). custom_name
-- overrides the daemon-proposed `name` for display; passing NULL reverts to
-- the default. Kept separate from the registration upserts above (which do
-- name = EXCLUDED.name on every heartbeat) so a custom name is never
-- clobbered by the daemon. Gated at the handler to owner / workspace admin.
UPDATE agent_runtime
SET custom_name = @custom_name, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: UpdateAgentRuntimeCustomNameByDaemon :many
-- Machine-level rename (SILI-4217): applies one custom name to every runtime
-- sharing a daemon_id in the workspace, since a single machine hosts one
-- runtime per provider. @owner_id is NULL for workspace owners/admins (rename
-- the whole machine) or the actor's user id otherwise (only their own
-- runtimes on that machine), so a member cannot relabel someone else's
-- runtime that happens to share the host.
UPDATE agent_runtime
SET custom_name = @custom_name, updated_at = now()
WHERE workspace_id = @workspace_id
  AND daemon_id = @daemon_id
  AND (@owner_id::uuid IS NULL OR owner_id = @owner_id)
RETURNING *;

-- name: ListDaemonCustomNames :many
-- Lists the custom_name of every OTHER runtime on (workspace_id, daemon_id)
-- (SILI-4217). @exclude_id drops the just-registered row. The caller derives
-- the machine-level name in Go — the same "all runtimes share one non-null
-- name" rule the frontend applies in sharedCustomName — so a freshly-added
-- runtime on an already-named machine can inherit that name and keep the
-- machine's display name stable. A daemon hosts only a handful of runtimes
-- (one per provider), so this is a tiny read.
SELECT custom_name FROM agent_runtime
WHERE workspace_id = @workspace_id
  AND daemon_id = @daemon_id
  AND id <> @exclude_id;


-- name: TouchAgentRuntimeLastSeen :execrows
-- Bumps last_seen_at on an already-online runtime. Deliberately does NOT
-- touch status or updated_at: status is unchanged on the hot heartbeat path,
-- and avoiding updated_at keeps the row HOT-eligible (no index columns
-- change) and avoids invalidating any downstream consumer that watches
-- updated_at.
--
-- The status='online' predicate is load-bearing: callers read rt.Status from
-- a prior SELECT and may race with the sweeper, which can flip the row to
-- offline between that SELECT and this UPDATE. Without the predicate this
-- query would silently leave a freshly-heartbeated runtime stuck in offline.
-- Returning affected rows lets callers detect that race and fall back to
-- MarkAgentRuntimeOnline to flip the row back online.
UPDATE agent_runtime
SET last_seen_at = now()
WHERE id = $1 AND status = 'online';

-- name: TouchAgentRuntimesLastSeenBatch :execrows
-- Bulk variant of TouchAgentRuntimeLastSeen used by the BatchedHeartbeatScheduler:
-- coalesces N per-runtime "bump last_seen_at" requests into a single UPDATE so a
-- fleet beating every 15s costs ~1 DB transaction per batch tick instead of N.
--
-- Same load-bearing predicate as the single-id form: status='online' avoids
-- silently un-deleting a sweeper-flipped offline row, and we deliberately do
-- NOT touch updated_at so the rows stay HOT-eligible. Affected-rows < len(ids)
-- means some IDs raced to offline between Schedule and flush; their next beat
-- will fall through the recordHeartbeat sync path and call MarkAgentRuntimeOnline.
UPDATE agent_runtime
SET last_seen_at = now()
WHERE id = ANY(@ids::uuid[]) AND status = 'online';

-- name: MarkAgentRuntimeOnline :one
-- Used on the offline→online transition (and on first heartbeat after
-- registration). Writes status, last_seen_at, and updated_at because the
-- status flip is a real state change and we want updated_at to reflect it.
UPDATE agent_runtime
SET status = 'online', last_seen_at = now(), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetAgentRuntimeOffline :exec
UPDATE agent_runtime
SET status = 'offline', updated_at = now()
WHERE id = $1;

-- name: SelectStaleOnlineRuntimes :many
-- Lists online runtimes whose last_seen_at exceeds the stale window. The
-- sweeper uses this as a candidate set, then optionally filters via the
-- LivenessStore before flipping rows to offline (a fresh Redis liveness
-- record means the DB row is just lagging, not actually dead).
SELECT id, workspace_id, owner_id, daemon_id, provider FROM agent_runtime
WHERE status = 'online'
  AND last_seen_at < now() - make_interval(secs => @stale_seconds::double precision);

-- name: MarkRuntimesOfflineByIDs :many
-- Flips a known set of runtime IDs from online to offline. Paired with
-- SelectStaleOnlineRuntimes in the sweeper so the candidate selection and
-- the actual write are decoupled (the LivenessStore filter sits between).
--
-- Re-checks the stale predicate inside the UPDATE so a concurrent heartbeat
-- between the SELECT (candidate gather), the LivenessStore filter, and this
-- UPDATE cannot demote a runtime that just refreshed last_seen_at. The
-- legacy MarkStaleRuntimesOffline UPDATE had this property implicitly
-- because the predicate and the write lived in one statement; here we
-- carry it forward explicitly so the SELECT/filter/UPDATE pipeline retains
-- the same race-freedom.
UPDATE agent_runtime
SET status = 'offline', updated_at = now()
WHERE status = 'online'
  AND id = ANY(@ids::uuid[])
  AND last_seen_at < now() - make_interval(secs => @stale_seconds::double precision)
RETURNING id, workspace_id, owner_id, daemon_id, provider;

-- name: FailRunsForOfflineRuntimes :many
-- Marks in-flight runs failed only after a runtime has remained disconnected
-- for the configured reconnect grace. A short network partition leaves the
-- run owned by the same attempt and allows its daemon to finish normally.
WITH victims AS (
  SELECT run.id
  FROM agent_run run
  JOIN agent_runtime runtime ON runtime.id = run.runtime_id
  WHERE run.status IN ('dispatched', 'running', 'waiting_local_directory')
    AND runtime.status = 'offline'
    AND COALESCE(runtime.last_seen_at, runtime.updated_at) <
        now() - make_interval(secs => @reconnect_grace_secs::double precision)
  ORDER BY COALESCE(runtime.last_seen_at, runtime.updated_at), run.created_at
  LIMIT @max_per_tick::int
  FOR UPDATE OF run SKIP LOCKED
)
UPDATE agent_run AS run
SET status = 'failed', completed_at = now(), error = 'runtime did not reconnect within the configured grace period',
    failure_reason = 'runtime_reconnect_timeout',
    wait_reason = NULL
FROM victims
WHERE run.id = victims.id
  AND run.status IN ('dispatched', 'running', 'waiting_local_directory')
RETURNING run.*;

-- name: ListAgentRuntimesByOwner :many
SELECT * FROM agent_runtime
WHERE workspace_id = $1 AND owner_id = $2
ORDER BY created_at ASC;

-- name: ForceOfflineRuntimesByIDs :many
-- Unconditionally flips a known set of runtime IDs to offline. Distinct from
-- MarkRuntimesOfflineByIDs (which keeps a stale-window predicate so the
-- sweeper cannot demote a runtime that just heartbeated): this variant is
-- used by intentional revocation paths — e.g. removing a workspace member —
-- where the caller has already decided the runtime should be offline
-- regardless of recent liveness.
UPDATE agent_runtime
SET status = 'offline', updated_at = now()
WHERE id = ANY(@runtime_ids::uuid[]) AND status = 'online'
RETURNING id, workspace_id, owner_id, daemon_id, provider;

-- name: CancelAgentRunsByRuntimeOrAgent :many
-- Cancels every active run that either lives on one of the given runtimes
-- OR belongs to one of the given agents. Used by the member-revocation flow:
-- the runtime-side covers runs queued against the leaving member's runtimes;
-- the agent-side covers runs pinned to a different runtime that those agents
-- left behind from a prior UpdateAgent (agent.runtime_id can change, but
-- agent_run.runtime_id does not get rewritten when it does, so a run
-- queued on runtime A by agent X — later moved to runtime B — survives the
-- runtime-only revoke and could still be claimed because ClaimAgentRun does
-- not gate on agent.archived_at).
--
-- We use 'cancelled' rather than 'failed' so the daemon's per-run status
-- poller (watchRunCancellation) interrupts the running agent gracefully.
-- Returns the affected rows so the caller can broadcast run:cancelled and
-- reconcile per-agent status.
UPDATE agent_run
SET status = 'cancelled', completed_at = now()
WHERE (runtime_id = ANY(@runtime_ids::uuid[]) OR agent_id = ANY(@agent_ids::uuid[]))
  AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
RETURNING *;

-- name: DeleteAgentRuntime :exec
WITH RECURSIVE cleanup_target_agent_run AS MATERIALIZED (
    SELECT child.* FROM "agent_run" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent_runtime parent WHERE child."runtime_id" = parent."id")
),
cleanup_target_agent_run_message AS MATERIALIZED (
    SELECT child.* FROM "agent_run_message" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id")
),
cleanup_target_agent_run_token AS MATERIALIZED (
    SELECT child.* FROM "agent_run_token" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id")
),
cleanup_target_agent_run_usage AS MATERIALIZED (
    SELECT child.* FROM "agent_run_usage" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id")
),
cleanup_target_agent_runtime AS MATERIALIZED (
    SELECT agent_runtime.* FROM "agent_runtime" agent_runtime WHERE agent_runtime.id = $1
),
cleanup_delete_agent_run AS (
    DELETE FROM "agent_run" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_agent_run)
),
cleanup_delete_agent_run_message AS (
    DELETE FROM "agent_run_message" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_agent_run_message)
),
cleanup_delete_agent_run_token AS (
    DELETE FROM "agent_run_token" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_agent_run_token)
),
cleanup_delete_agent_run_usage AS (
    DELETE FROM "agent_run_usage" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_agent_run_usage)
),
cleanup_detach_agent_run AS (
    UPDATE "agent_run" child SET "parent_run_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."parent_run_id" = parent."id") THEN NULL ELSE child."parent_run_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."parent_run_id" = parent."id")) AND NOT ((child."id") IN (SELECT "id" FROM cleanup_target_agent_run))
),
cleanup_detach_autopilot_run AS (
    UPDATE "autopilot_run" child SET "run_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id") THEN NULL ELSE child."run_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id"))
),
cleanup_detach_chat_session AS (
    UPDATE "chat_session" child SET "runtime_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_agent_runtime parent WHERE child."runtime_id" = parent."id") THEN NULL ELSE child."runtime_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_agent_runtime parent WHERE child."runtime_id" = parent."id"))
),
cleanup_detach_lark_outbound_card_message AS (
    UPDATE "lark_outbound_card_message" child SET "run_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id") THEN NULL ELSE child."run_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id"))
)
DELETE FROM "agent_runtime" agent_runtime WHERE (agent_runtime."id") IN (SELECT "id" FROM cleanup_target_agent_runtime) ;


-- name: UnbindAgentsFromRuntime :execrows
-- Keeps agent configuration and all dependent history while retiring a runtime.
UPDATE agent SET runtime_id = NULL WHERE runtime_id = $1;

-- name: UnbindRunsFromRuntime :execrows
-- Active runs must be drained first; terminal history is detached explicitly
-- so the legacy runtime FK cascade cannot remove it.
UPDATE agent_run
SET runtime_id = NULL
WHERE runtime_id = $1 AND completed_at IS NOT NULL;

-- name: CountRunsByRuntime :one
SELECT count(*) FROM agent_run WHERE agent_run.runtime_id = $1;

-- name: CountUndrainedRunsByRuntime :one
SELECT count(*) FROM agent_run
WHERE runtime_id = $1 AND completed_at IS NULL;

-- name: DeleteSystemAgentsByRuntime :exec
-- System agents are invisible execution infrastructure (for example the Agent
-- Builder). Remove them before deleting their runtime so the RESTRICT runtime
-- FK cannot block an otherwise dependency-free delete.
WITH RECURSIVE cleanup_target_agent AS MATERIALIZED (
    SELECT agent.* FROM "agent" agent WHERE agent.runtime_id = $1 AND agent.kind = 'system'
),
cleanup_target_agent_run AS MATERIALIZED (
    SELECT child.* FROM "agent_run" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent parent WHERE child."agent_id" = parent."id")
),
cleanup_target_agent_run_message AS MATERIALIZED (
    SELECT child.* FROM "agent_run_message" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id")
),
cleanup_target_agent_run_token AS MATERIALIZED (
    SELECT child.* FROM "agent_run_token" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_agent parent WHERE child."agent_id" = parent."id")
),
cleanup_target_agent_run_usage AS MATERIALIZED (
    SELECT child.* FROM "agent_run_usage" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id")
),
cleanup_target_agent_skill AS MATERIALIZED (
    SELECT child.* FROM "agent_skill" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent parent WHERE child."agent_id" = parent."id")
),
cleanup_target_attachment AS MATERIALIZED (
    SELECT child.* FROM "attachment" child WHERE EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_chat_message parent WHERE child."chat_message_id" = parent."id")
),
cleanup_target_chat_message AS MATERIALIZED (
    SELECT child.* FROM "chat_message" child WHERE EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id")
),
cleanup_target_chat_session AS MATERIALIZED (
    SELECT child.* FROM "chat_session" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent parent WHERE child."agent_id" = parent."id")
),
cleanup_target_daemon_connection AS MATERIALIZED (
    SELECT child.* FROM "daemon_connection" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent parent WHERE child."agent_id" = parent."id")
),
cleanup_target_lark_binding_token AS MATERIALIZED (
    SELECT child.* FROM "lark_binding_token" child WHERE EXISTS (SELECT 1 FROM cleanup_target_lark_installation parent WHERE child."installation_id" = parent."id")
),
cleanup_target_lark_chat_session_binding AS MATERIALIZED (
    SELECT child.* FROM "lark_chat_session_binding" child WHERE EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_lark_installation parent WHERE child."installation_id" = parent."id")
),
cleanup_target_lark_inbound_message_dedup AS MATERIALIZED (
    SELECT child.* FROM "lark_inbound_message_dedup" child WHERE EXISTS (SELECT 1 FROM cleanup_target_lark_installation parent WHERE child."installation_id" = parent."id")
),
cleanup_target_lark_installation AS MATERIALIZED (
    SELECT child.* FROM "lark_installation" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent parent WHERE child."agent_id" = parent."id")
),
cleanup_target_lark_outbound_card_message AS MATERIALIZED (
    SELECT child.* FROM "lark_outbound_card_message" child WHERE EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id")
),
cleanup_target_lark_user_binding AS MATERIALIZED (
    SELECT child.* FROM "lark_user_binding" child WHERE EXISTS (SELECT 1 FROM cleanup_target_lark_installation parent WHERE child."installation_id" = parent."id" AND child."workspace_id" = parent."workspace_id")
),
cleanup_delete_agent_run AS (
    DELETE FROM "agent_run" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_agent_run)
),
cleanup_delete_agent_run_message AS (
    DELETE FROM "agent_run_message" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_agent_run_message)
),
cleanup_delete_agent_run_token AS (
    DELETE FROM "agent_run_token" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_agent_run_token)
),
cleanup_delete_agent_run_usage AS (
    DELETE FROM "agent_run_usage" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_agent_run_usage)
),
cleanup_delete_agent_skill AS (
    DELETE FROM "agent_skill" child WHERE (child."agent_id", child."skill_id") IN (SELECT "agent_id", "skill_id" FROM cleanup_target_agent_skill)
),
cleanup_delete_attachment AS (
    DELETE FROM "attachment" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_attachment)
),
cleanup_delete_chat_message AS (
    DELETE FROM "chat_message" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_chat_message)
),
cleanup_delete_chat_session AS (
    DELETE FROM "chat_session" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_chat_session)
),
cleanup_delete_daemon_connection AS (
    DELETE FROM "daemon_connection" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_daemon_connection)
),
cleanup_delete_lark_binding_token AS (
    DELETE FROM "lark_binding_token" child WHERE (child."token_hash") IN (SELECT "token_hash" FROM cleanup_target_lark_binding_token)
),
cleanup_delete_lark_chat_session_binding AS (
    DELETE FROM "lark_chat_session_binding" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_lark_chat_session_binding)
),
cleanup_delete_lark_inbound_message_dedup AS (
    DELETE FROM "lark_inbound_message_dedup" child WHERE (child."installation_id", child."message_id") IN (SELECT "installation_id", "message_id" FROM cleanup_target_lark_inbound_message_dedup)
),
cleanup_delete_lark_installation AS (
    DELETE FROM "lark_installation" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_lark_installation)
),
cleanup_delete_lark_outbound_card_message AS (
    DELETE FROM "lark_outbound_card_message" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_lark_outbound_card_message)
),
cleanup_delete_lark_user_binding AS (
    DELETE FROM "lark_user_binding" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_lark_user_binding)
),
cleanup_detach_agent_run AS (
    UPDATE "agent_run" child SET "chat_session_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id") THEN NULL ELSE child."chat_session_id" END,
        "parent_run_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."parent_run_id" = parent."id") THEN NULL ELSE child."parent_run_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."parent_run_id" = parent."id")) AND NOT ((child."id") IN (SELECT "id" FROM cleanup_target_agent_run))
),
cleanup_detach_autopilot_run AS (
    UPDATE "autopilot_run" child SET "run_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id") THEN NULL ELSE child."run_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id"))
),
cleanup_detach_lark_inbound_audit AS (
    UPDATE "lark_inbound_audit" child SET "installation_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_lark_installation parent WHERE child."installation_id" = parent."id") THEN NULL ELSE child."installation_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_lark_installation parent WHERE child."installation_id" = parent."id"))
),
cleanup_detach_lark_outbound_card_message AS (
    UPDATE "lark_outbound_card_message" child SET "run_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id") THEN NULL ELSE child."run_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id")) AND NOT ((child."id") IN (SELECT "id" FROM cleanup_target_lark_outbound_card_message))
)
DELETE FROM "agent" agent WHERE (agent."id") IN (SELECT "id" FROM cleanup_target_agent) ;


-- name: CountActiveAgentsByRuntime :one
SELECT count(*) FROM agent WHERE agent.runtime_id = $1 AND archived_at IS NULL;

-- name: CountActiveSquadsWithArchivedLeadersByRuntime :one
SELECT count(*)
FROM squad
WHERE archived_at IS NULL
  AND leader_id IN (
    SELECT id FROM agent WHERE agent.runtime_id = $1 AND archived_at IS NOT NULL
  );

-- name: DeleteArchivedAgentsByRuntime :exec
WITH RECURSIVE cleanup_target_agent AS MATERIALIZED (
    SELECT agent.* FROM "agent" agent WHERE agent.runtime_id = $1 AND agent.archived_at IS NOT NULL
),
cleanup_target_agent_run AS MATERIALIZED (
    SELECT child.* FROM "agent_run" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent parent WHERE child."agent_id" = parent."id")
),
cleanup_target_agent_run_message AS MATERIALIZED (
    SELECT child.* FROM "agent_run_message" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id")
),
cleanup_target_agent_run_token AS MATERIALIZED (
    SELECT child.* FROM "agent_run_token" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_agent parent WHERE child."agent_id" = parent."id")
),
cleanup_target_agent_run_usage AS MATERIALIZED (
    SELECT child.* FROM "agent_run_usage" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id")
),
cleanup_target_agent_skill AS MATERIALIZED (
    SELECT child.* FROM "agent_skill" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent parent WHERE child."agent_id" = parent."id")
),
cleanup_target_attachment AS MATERIALIZED (
    SELECT child.* FROM "attachment" child WHERE EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_chat_message parent WHERE child."chat_message_id" = parent."id")
),
cleanup_target_chat_message AS MATERIALIZED (
    SELECT child.* FROM "chat_message" child WHERE EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id")
),
cleanup_target_chat_session AS MATERIALIZED (
    SELECT child.* FROM "chat_session" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent parent WHERE child."agent_id" = parent."id")
),
cleanup_target_daemon_connection AS MATERIALIZED (
    SELECT child.* FROM "daemon_connection" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent parent WHERE child."agent_id" = parent."id")
),
cleanup_target_lark_binding_token AS MATERIALIZED (
    SELECT child.* FROM "lark_binding_token" child WHERE EXISTS (SELECT 1 FROM cleanup_target_lark_installation parent WHERE child."installation_id" = parent."id")
),
cleanup_target_lark_chat_session_binding AS MATERIALIZED (
    SELECT child.* FROM "lark_chat_session_binding" child WHERE EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_lark_installation parent WHERE child."installation_id" = parent."id")
),
cleanup_target_lark_inbound_message_dedup AS MATERIALIZED (
    SELECT child.* FROM "lark_inbound_message_dedup" child WHERE EXISTS (SELECT 1 FROM cleanup_target_lark_installation parent WHERE child."installation_id" = parent."id")
),
cleanup_target_lark_installation AS MATERIALIZED (
    SELECT child.* FROM "lark_installation" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent parent WHERE child."agent_id" = parent."id")
),
cleanup_target_lark_outbound_card_message AS MATERIALIZED (
    SELECT child.* FROM "lark_outbound_card_message" child WHERE EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id")
),
cleanup_target_lark_user_binding AS MATERIALIZED (
    SELECT child.* FROM "lark_user_binding" child WHERE EXISTS (SELECT 1 FROM cleanup_target_lark_installation parent WHERE child."installation_id" = parent."id" AND child."workspace_id" = parent."workspace_id")
),
cleanup_delete_agent_run AS (
    DELETE FROM "agent_run" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_agent_run)
),
cleanup_delete_agent_run_message AS (
    DELETE FROM "agent_run_message" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_agent_run_message)
),
cleanup_delete_agent_run_token AS (
    DELETE FROM "agent_run_token" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_agent_run_token)
),
cleanup_delete_agent_run_usage AS (
    DELETE FROM "agent_run_usage" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_agent_run_usage)
),
cleanup_delete_agent_skill AS (
    DELETE FROM "agent_skill" child WHERE (child."agent_id", child."skill_id") IN (SELECT "agent_id", "skill_id" FROM cleanup_target_agent_skill)
),
cleanup_delete_attachment AS (
    DELETE FROM "attachment" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_attachment)
),
cleanup_delete_chat_message AS (
    DELETE FROM "chat_message" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_chat_message)
),
cleanup_delete_chat_session AS (
    DELETE FROM "chat_session" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_chat_session)
),
cleanup_delete_daemon_connection AS (
    DELETE FROM "daemon_connection" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_daemon_connection)
),
cleanup_delete_lark_binding_token AS (
    DELETE FROM "lark_binding_token" child WHERE (child."token_hash") IN (SELECT "token_hash" FROM cleanup_target_lark_binding_token)
),
cleanup_delete_lark_chat_session_binding AS (
    DELETE FROM "lark_chat_session_binding" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_lark_chat_session_binding)
),
cleanup_delete_lark_inbound_message_dedup AS (
    DELETE FROM "lark_inbound_message_dedup" child WHERE (child."installation_id", child."message_id") IN (SELECT "installation_id", "message_id" FROM cleanup_target_lark_inbound_message_dedup)
),
cleanup_delete_lark_installation AS (
    DELETE FROM "lark_installation" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_lark_installation)
),
cleanup_delete_lark_outbound_card_message AS (
    DELETE FROM "lark_outbound_card_message" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_lark_outbound_card_message)
),
cleanup_delete_lark_user_binding AS (
    DELETE FROM "lark_user_binding" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_lark_user_binding)
),
cleanup_detach_agent_run AS (
    UPDATE "agent_run" child SET "chat_session_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id") THEN NULL ELSE child."chat_session_id" END,
        "parent_run_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."parent_run_id" = parent."id") THEN NULL ELSE child."parent_run_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."parent_run_id" = parent."id")) AND NOT ((child."id") IN (SELECT "id" FROM cleanup_target_agent_run))
),
cleanup_detach_autopilot_run AS (
    UPDATE "autopilot_run" child SET "run_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id") THEN NULL ELSE child."run_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id"))
),
cleanup_detach_lark_inbound_audit AS (
    UPDATE "lark_inbound_audit" child SET "installation_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_lark_installation parent WHERE child."installation_id" = parent."id") THEN NULL ELSE child."installation_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_lark_installation parent WHERE child."installation_id" = parent."id"))
),
cleanup_detach_lark_outbound_card_message AS (
    UPDATE "lark_outbound_card_message" child SET "run_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id") THEN NULL ELSE child."run_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id")) AND NOT ((child."id") IN (SELECT "id" FROM cleanup_target_lark_outbound_card_message))
)
DELETE FROM "agent" agent WHERE (agent."id") IN (SELECT "id" FROM cleanup_target_agent) ;


-- name: PauseAutopilotsByAgentAssignees :exec
-- Pauses every active autopilot whose agent assignee is in the supplied list.
-- Called before hard-deleting archived agents on runtime teardown so the rows
-- do not become dangling (autopilot.assignee_id no longer has an agent FK
-- since migration 096). Status='paused' makes the breakage visible in the UI
-- — operators can re-point the autopilot at a live agent or delete it —
-- rather than silently piling skipped runs.
UPDATE autopilot
SET status = 'paused', updated_at = now()
WHERE status = 'active'
  AND assignee_type = 'agent'
  AND assignee_id = ANY(@assignee_ids::uuid[]);

-- name: ListArchivedAgentIDsByRuntime :many
-- Companion to DeleteArchivedAgentsByRuntime: enumerates the archived agents
-- about to be hard-deleted so the runtime teardown can pause autopilots that
-- still point at them. Returns ids only — the caller only needs the set.
SELECT id FROM agent WHERE agent.runtime_id = $1 AND archived_at IS NOT NULL;

-- name: DeleteSquadsByArchivedAgentsOnRuntime :exec
-- Removes archived squads whose leader_id references an archived agent on the
-- given runtime. Must run before DeleteArchivedAgentsByRuntime so the RESTRICT
-- FK on squad.leader_id does not block the agent deletion. Active squads are
-- handled separately by CountActiveSquadsWithArchivedLeadersByRuntime, which
-- returns a 409 until the caller archives them or assigns a new leader.
WITH RECURSIVE cleanup_target_squad AS MATERIALIZED (
    SELECT squad.* FROM "squad" squad WHERE leader_id IN (
    SELECT id FROM agent WHERE agent.runtime_id = $1 AND archived_at IS NOT NULL
)
  AND archived_at IS NOT NULL
),
cleanup_target_squad_member AS MATERIALIZED (
    SELECT child.* FROM "squad_member" child WHERE EXISTS (SELECT 1 FROM cleanup_target_squad parent WHERE child."squad_id" = parent."id")
),
cleanup_delete_squad_member AS (
    DELETE FROM "squad_member" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_squad_member)
),
cleanup_detach_autopilot_run AS (
    UPDATE "autopilot_run" child SET "squad_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_squad parent WHERE child."squad_id" = parent."id") THEN NULL ELSE child."squad_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_squad parent WHERE child."squad_id" = parent."id"))
)
DELETE FROM "squad" squad WHERE (squad."id") IN (SELECT "id" FROM cleanup_target_squad) ;


-- name: FindLegacyRuntimesByDaemonID :many
-- Looks up runtime rows keyed on a prior (hostname-derived) daemon_id. Used
-- at register-time to find rows owned by the same machine under its old
-- identity so agents/runs can be re-pointed at the new UUID-keyed row.
--
-- Comparison is case-insensitive because os.Hostname() has been observed to
-- return different casings on the same machine (e.g. `Jiayuans-MacBook-Pro`
-- vs `jiayuans-macbook-pro`) across reboots/mDNS state changes. A case-
-- sensitive `=` would strand the old row; LOWER() on both sides handles drift
-- without forcing the daemon to enumerate cased permutations.
--
-- Returns many rather than one because case drift may have already minted
-- duplicate rows historically (e.g. `Foo.local` AND `foo.local` under the
-- same workspace+provider). A single-row lookup would consolidate only one
-- of them and leave the rest orphaned. Callers must merge every returned
-- row into the new UUID-keyed runtime.
SELECT * FROM agent_runtime
WHERE workspace_id = @workspace_id
  AND provider = @provider
  AND LOWER(daemon_id) = LOWER(@daemon_id);

-- name: ReassignAgentsToRuntime :execrows
-- Re-points every agent referencing old_runtime_id at new_runtime_id.
WITH runtime_guard AS MATERIALIZED (
    SELECT id, workspace_id FROM agent_runtime
    WHERE id = @new_runtime_id FOR KEY SHARE
)
UPDATE agent
SET runtime_id = @new_runtime_id
WHERE runtime_id = @old_runtime_id
  AND EXISTS (SELECT 1 FROM runtime_guard WHERE runtime_guard.workspace_id = agent.workspace_id);

-- name: ReassignRunsToRuntime :execrows
-- Re-points every queued/running/completed run referencing old_runtime_id.
-- Required before deleting the old runtime row because agent_run has
-- explicit dependent cleanup that would otherwise drop historical runs.
WITH runtime_guard AS MATERIALIZED (
    SELECT id, workspace_id FROM agent_runtime
    WHERE id = @new_runtime_id FOR KEY SHARE
)
UPDATE agent_run
SET runtime_id = @new_runtime_id
WHERE agent_run.runtime_id = @old_runtime_id
  AND EXISTS (
    SELECT 1 FROM runtime_guard JOIN agent ON agent.workspace_id = runtime_guard.workspace_id
    WHERE agent.id = agent_run.agent_id
  );

-- name: RecordRuntimeLegacyDaemonID :exec
-- Remembers the most recent hostname-derived daemon_id that was merged into
-- this row. Useful for debugging when tracing back why a given runtime row
-- subsumed an old one, and only overwrites NULL so the earliest merge is
-- preserved.
UPDATE agent_runtime
SET legacy_daemon_id = COALESCE(legacy_daemon_id, $2)
WHERE id = $1;

-- name: ListStaleOfflineRuntimeGCCandidates :many
-- Candidate discovery is bounded; correctness is rechecked transactionally.
SELECT id FROM agent_runtime
WHERE status = 'offline'
  AND last_seen_at < now() - make_interval(secs => @stale_seconds::double precision)
  AND id NOT IN (SELECT DISTINCT runtime_id FROM agent)
  AND NOT EXISTS (
    SELECT 1 FROM agent_run
    WHERE agent_run.runtime_id = agent_runtime.id
      AND completed_at IS NULL
  )
ORDER BY last_seen_at ASC, id ASC
LIMIT @max_per_tick::int;

-- name: IsAgentRuntimeEligibleForGC :one
SELECT EXISTS (
  SELECT 1 FROM agent_runtime
  WHERE agent_runtime.id = @id
    AND status = 'offline'
    AND last_seen_at < now() - make_interval(secs => @stale_seconds::double precision)
    AND NOT EXISTS (SELECT 1 FROM agent WHERE agent.runtime_id = agent_runtime.id)
) AS eligible;
