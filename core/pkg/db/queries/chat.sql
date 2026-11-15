-- name: CreateChatSession :one
INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, runtime_id, is_agent_intro, project_id) SELECT
    $1 AS workspace_id,
    $2 AS agent_id,
    $3 AS creator_id,
    $4 AS title,
    (SELECT runtime_id FROM agent WHERE agent.id = $2) AS runtime_id,
    $5 AS is_agent_intro,
    sqlc.narg('project_id') AS project_id
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM workspace AS ref_workspace_id WHERE ref_workspace_id.id = ($1) FOR KEY SHARE))
  AND (($2)::uuid IS NULL OR EXISTS (SELECT 1 FROM agent AS ref_agent_id WHERE ref_agent_id.id = ($2) FOR KEY SHARE))
  AND (($3)::uuid IS NULL OR EXISTS (SELECT 1 FROM "user" AS ref_creator_id WHERE ref_creator_id.id = ($3) FOR KEY SHARE))
  AND (((SELECT runtime_id FROM agent WHERE agent.id = $2))::uuid IS NULL OR EXISTS (SELECT 1 FROM agent_runtime AS ref_runtime_id WHERE ref_runtime_id.id = ((SELECT runtime_id FROM agent WHERE agent.id = $2)) FOR KEY SHARE))

RETURNING *;

-- name: CreateGroupChatSession :one
INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, runtime_id, is_agent_intro, project_id, kind) SELECT
    sqlc.arg(workspace_id) AS workspace_id,
    sqlc.narg(agent_id) AS agent_id,
    sqlc.arg(creator_id) AS creator_id,
    sqlc.arg(title) AS title,
    NULL AS runtime_id,
    FALSE AS is_agent_intro,
    sqlc.narg(project_id) AS project_id,
    'group' AS kind
WHERE ((sqlc.arg(workspace_id))::uuid IS NULL OR EXISTS (SELECT 1 FROM workspace AS ref_workspace_id WHERE ref_workspace_id.id = (sqlc.arg(workspace_id)) FOR KEY SHARE))
  AND ((sqlc.narg(agent_id))::uuid IS NULL OR EXISTS (SELECT 1 FROM agent AS ref_agent_id WHERE ref_agent_id.id = (sqlc.narg(agent_id)) FOR KEY SHARE))
  AND ((sqlc.arg(creator_id))::uuid IS NULL OR EXISTS (SELECT 1 FROM "user" AS ref_creator_id WHERE ref_creator_id.id = (sqlc.arg(creator_id)) FOR KEY SHARE))

RETURNING *;

-- name: AddChatSessionParticipant :exec
INSERT INTO chat_session_participant (
    chat_session_id, participant_type, participant_id, created_by
)
VALUES ($1, $2, $3, $4)
ON CONFLICT (chat_session_id, participant_type, participant_id) DO NOTHING;

-- name: DeleteChatSessionParticipants :exec
DELETE FROM chat_session_participant WHERE chat_session_participant.chat_session_id = $1;

-- name: DeleteChatSessionParticipantsByWorkspace :exec
DELETE FROM chat_session_participant
WHERE chat_session_id IN (
    SELECT id FROM chat_session WHERE chat_session.workspace_id = $1
);

-- name: DeleteMemberFromGroupChatsInWorkspace :exec
DELETE FROM chat_session_participant
WHERE participant_type = 'member'
  AND participant_id = $1
  AND chat_session_id IN (
      SELECT id FROM chat_session WHERE chat_session.workspace_id = $2 AND kind = 'group'
  );

-- name: ListChatSessionParticipants :many
SELECT participant_type, participant_id, created_by, last_read_at, created_at
FROM chat_session_participant
WHERE chat_session_id = $1
ORDER BY participant_type ASC, created_at ASC;

-- name: IsChatSessionMemberParticipant :one
SELECT EXISTS (
    SELECT 1 FROM chat_session_participant
    WHERE chat_session_id = $1
      AND participant_type = 'member'
      AND participant_id = $2
) AS is_participant;

-- name: IsChatSessionAgentParticipant :one
SELECT EXISTS (
    SELECT 1 FROM chat_session_participant
    WHERE chat_session_id = $1
      AND participant_type = 'agent'
      AND participant_id = $2
) AS is_participant;

-- name: ClearChatSessionProjectByProject :exec
-- Project references are intentionally soft (no database FK). Keep chat
-- history while removing the context selection when a project is deleted.
-- Do not touch updated_at: context cleanup is not chat activity.
UPDATE chat_session
SET project_id = NULL
WHERE project_id = $1 AND workspace_id = $2;

-- name: GetChatSession :one
SELECT * FROM chat_session
WHERE id = $1;

-- name: GetChatSessionInWorkspace :one
SELECT * FROM chat_session
WHERE id = $1 AND workspace_id = $2;

-- name: ListChatSessionsByCreator :many
-- IM-style list: each active session with its unread *count* (assistant
-- messages after the read cursor), a preview of the latest message, and
-- ordered by most-recent activity so a new reply bumps a session to the top.
SELECT cs.*,
       (SELECT count(*) FROM chat_message m
          WHERE m.chat_session_id = cs.id
            AND (
              (cs.kind = 'direct' AND m.role = 'assistant')
              OR (cs.kind = 'group' AND (m.sender_user_id IS NULL OR m.sender_user_id <> sqlc.arg(participant_id)))
            )
            AND m.created_at > CASE
              WHEN cs.kind = 'group' THEN COALESCE(self_participant.last_read_at, cs.last_read_at)
              ELSE cs.last_read_at
            END)::int AS unread_count,
       COALESCE(lm.content, '') AS last_message_content,
       COALESCE(lm.role, '') AS last_message_role,
       lm.created_at AS last_message_at,
       lm.failure_reason AS last_message_failure_reason,
       COALESCE(lm.message_kind, '') AS last_message_kind
FROM chat_session cs
LEFT JOIN chat_session_participant self_participant
  ON self_participant.chat_session_id = cs.id
 AND self_participant.participant_type = 'member'
 AND self_participant.participant_id = sqlc.arg(participant_id)
LEFT JOIN LATERAL (
  SELECT content, role, created_at, failure_reason, message_kind
    FROM chat_message m
   WHERE m.chat_session_id = cs.id
   ORDER BY m.created_at DESC
   LIMIT 1
) lm ON true
WHERE cs.workspace_id = sqlc.arg(workspace_id)
  AND cs.status = 'active'
  AND (
    (cs.kind = 'direct' AND cs.creator_id = sqlc.arg(participant_id))
    OR (cs.kind = 'group' AND self_participant.participant_id IS NOT NULL)
  )
ORDER BY (cs.pinned_at IS NOT NULL) DESC, cs.pinned_at DESC, COALESCE(lm.created_at, cs.updated_at) DESC;

-- name: ListAllChatSessionsByCreator :many
-- Unlike ListChatSessionsByCreator this returns archived sessions too (for the
-- "Archived" view), so unread must be forced to 0 for archived rows: archiving
-- deliberately does NOT advance last_read_at (so unarchive can restore the true
-- unread state), but an archived session is read-only and hidden from history,
-- so any residual unread is uncleanable and must not light up any badge. Gating
-- on status here is the single source of truth for all unread surfaces (FAB,
-- sidebar Chat tab, chat-window header) — see SILI-4360.
SELECT cs.*,
       CASE WHEN cs.status = 'archived' THEN 0
            ELSE (SELECT count(*) FROM chat_message m
                    WHERE m.chat_session_id = cs.id
                      AND (
                        (cs.kind = 'direct' AND m.role = 'assistant')
                        OR (cs.kind = 'group' AND (m.sender_user_id IS NULL OR m.sender_user_id <> sqlc.arg(participant_id)))
                      )
                      AND m.created_at > CASE
                        WHEN cs.kind = 'group' THEN COALESCE(self_participant.last_read_at, cs.last_read_at)
                        ELSE cs.last_read_at
                      END)
       END::int AS unread_count,
       COALESCE(lm.content, '') AS last_message_content,
       COALESCE(lm.role, '') AS last_message_role,
       lm.created_at AS last_message_at,
       lm.failure_reason AS last_message_failure_reason,
       COALESCE(lm.message_kind, '') AS last_message_kind
FROM chat_session cs
LEFT JOIN chat_session_participant self_participant
  ON self_participant.chat_session_id = cs.id
 AND self_participant.participant_type = 'member'
 AND self_participant.participant_id = sqlc.arg(participant_id)
LEFT JOIN LATERAL (
  SELECT content, role, created_at, failure_reason, message_kind
    FROM chat_message m
   WHERE m.chat_session_id = cs.id
   ORDER BY m.created_at DESC
   LIMIT 1
) lm ON true
WHERE cs.workspace_id = sqlc.arg(workspace_id)
  AND (
    (cs.kind = 'direct' AND cs.creator_id = sqlc.arg(participant_id))
    OR (cs.kind = 'group' AND self_participant.participant_id IS NOT NULL)
  )
ORDER BY (cs.pinned_at IS NOT NULL) DESC, cs.pinned_at DESC, COALESCE(lm.created_at, cs.updated_at) DESC;

-- name: UpdateChatSessionTitle :one
UPDATE chat_session SET title = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateChatSessionProject :one
-- Project context is user-editable session metadata. Do not touch updated_at:
-- changing context is not conversation activity and must not reorder history.
UPDATE chat_session
SET project_id = sqlc.narg('project_id')
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: UpdateChatSessionTitleIfCurrent :one
-- Compare-and-swap the title: only overwrite it when it still equals the
-- value the caller observed (@expected_title). This is the idempotency /
-- no-clobber guard behind LLM auto-titling (SILI-4295): the async generator
-- captures the session's current (default/original) title before calling the
-- model, and this write lands only if a manual rename or a competing writer
-- has not changed the title in the meantime. A mismatch returns pgx.ErrNoRows
-- (zero rows updated), which the caller treats as "someone renamed it — leave
-- it alone", NOT as an error.
UPDATE chat_session SET title = @new_title, updated_at = now()
WHERE id = @id AND title = @expected_title
RETURNING *;

-- name: SetChatSessionPinned :one
-- Pin/unpin a chat. Deliberately does NOT touch updated_at: pinning is a
-- list-ordering preference, not activity, so it must not bump the session's
-- last-activity sort key (which would make an unpinned chat jump the list).
-- pinned = true stamps pinned_at only when it was NULL, so re-pinning keeps
-- the original pin order; pinned = false clears it.
UPDATE chat_session
SET pinned_at = CASE WHEN @pinned::bool THEN COALESCE(pinned_at, now()) ELSE NULL END
WHERE id = $1
RETURNING *;

-- name: SetChatSessionArchived :one
-- Archive/unarchive a chat session by flipping status between 'active' and
-- 'archived'. Bumps updated_at so the row re-sorts on the receiving list. The
-- send-message path refuses archived sessions (see SendChatMessage), so the
-- conversation is effectively read-only until it is unarchived.
UPDATE chat_session
SET status = CASE WHEN @archived::bool THEN 'archived' ELSE 'active' END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateChatSessionSession :exec
-- Updates the resume pointer for a chat session. Empty/NULL inputs are
-- ignored via COALESCE so a run that completes without a session_id (e.g.
-- the agent crashed before establishing one) cannot wipe out a previously
-- recorded resume pointer. This makes the chat memory robust against
-- intermittent agent failures.
UPDATE chat_session
SET session_id = COALESCE(sqlc.narg('session_id'), session_id),
    work_dir = COALESCE(sqlc.narg('work_dir'), work_dir),
    runtime_id = COALESCE(sqlc.narg('runtime_id'), runtime_id),
    updated_at = now()
WHERE chat_session.id = sqlc.arg('id')
  AND ((sqlc.narg('runtime_id'))::uuid IS NULL OR EXISTS (SELECT 1 FROM agent_runtime AS ref_runtime_id WHERE ref_runtime_id.id = (sqlc.narg('runtime_id')) FOR KEY SHARE))
;

-- name: ClearChatSessionSessionIfMatches :exec
-- Drops the chat session's resume pointer, but only while it still points at
-- the exact session the caller proved unresumable.
--
-- The claim handler reads chat_session.session_id FIRST and only falls back to
-- GetLastChatRunSession when it is empty, so a poisoned pointer here bypasses
-- every filter that query applies. Declining to OVERWRITE the pointer on a
-- resume-unsafe failure — which is all the fail path used to do — leaves the
-- dead session in place and the next turn resumes it (GH #6066).
--
-- The session_id + runtime_id predicate is what makes this safe to run in the
-- fail transaction: a concurrent turn that has already written a NEW pointer
-- does not match, so its healthy session survives instead of being cleared by
-- a slower sibling's failure. work_dir is deliberately left alone — the
-- directory is still reusable, only the conversation is not.
UPDATE chat_session
SET session_id = NULL,
    runtime_id = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND session_id = sqlc.arg('session_id')
  AND runtime_id = sqlc.arg('runtime_id');

-- name: LockChatSessionForDelete :one
-- Acquires an exclusive (FOR UPDATE) row lock on chat_session(id). Used by
-- the delete path so that a concurrent SendChatMessage cannot enqueue a new
-- agent_run row referencing this session between our cancel and
-- delete steps. The FK from agent_run.chat_session_id takes a
-- KEY SHARE lock on the parent row during INSERT validation, which
-- conflicts with FOR UPDATE — concurrent inserts block here and then fail
-- their FK check after we commit the delete.
SELECT id FROM chat_session
WHERE id = $1
FOR UPDATE;

-- name: LockChatSessionForRuntimeBind :one
-- Acquires an exclusive (FOR UPDATE) row lock on chat_session(id), serialising
-- "which runtime does this session execute on" against "enqueue the next run".
--
-- Both SendDirectChatMessage and the agent-builder runtime switch take this lock
-- for their whole transaction. Without it the two are a read-then-write race: a
-- send reads the carrier agent's runtime_id, the switch then passes its
-- pending-run check and rebinds the carrier, and the send finally inserts a run
-- still stamped with the pre-switch runtime — so the user is told the switch
-- succeeded while their message runs on the old runtime (SILI-5163).
--
-- The lock alone is not sufficient: the send path must also re-read the agent
-- INSIDE the locked transaction, because a send blocked at INSERT would otherwise
-- resume and write the runtime_id it read before blocking.
--
-- Same row and same lock mode as LockChatSessionForDelete, and both take it as
-- their first statement, so the delete path and this one cannot deadlock.
SELECT id FROM chat_session
WHERE id = $1
FOR UPDATE;

-- name: DeleteChatSession :exec
-- Hard delete. chat_message rows cascade via FK ON DELETE CASCADE; the
-- chat_session_id on agent_run is set NULL by FK so completed/failed
-- run history survives the session being removed. Callers MUST run inside
-- the same transaction that holds LockChatSessionForDelete and that has
-- already cancelled any in-flight runs (see CancelAgentRunsByChatSession)
-- so the daemon does not keep running work whose result has nowhere to
-- land. workspace_id in the WHERE clause is a SQL-layer tenant guard; see
-- DeleteTask.
WITH RECURSIVE cleanup_target_attachment AS MATERIALIZED (
    SELECT child.* FROM "attachment" child WHERE EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_chat_message parent WHERE child."chat_message_id" = parent."id")
),
cleanup_target_chat_message AS MATERIALIZED (
    SELECT child.* FROM "chat_message" child WHERE EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id")
),
cleanup_target_chat_session AS MATERIALIZED (
    SELECT chat_session.* FROM "chat_session" chat_session WHERE chat_session.id = $1 AND chat_session.workspace_id = $2
),
cleanup_target_lark_chat_session_binding AS MATERIALIZED (
    SELECT child.* FROM "lark_chat_session_binding" child WHERE EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id")
),
cleanup_target_lark_outbound_card_message AS MATERIALIZED (
    SELECT child.* FROM "lark_outbound_card_message" child WHERE EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id")
),
cleanup_delete_attachment AS (
    DELETE FROM "attachment" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_attachment)
),
cleanup_delete_chat_message AS (
    DELETE FROM "chat_message" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_chat_message)
),
cleanup_delete_lark_chat_session_binding AS (
    DELETE FROM "lark_chat_session_binding" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_lark_chat_session_binding)
),
cleanup_delete_lark_outbound_card_message AS (
    DELETE FROM "lark_outbound_card_message" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_lark_outbound_card_message)
),
cleanup_detach_agent_run AS (
    UPDATE "agent_run" child SET "chat_session_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id") THEN NULL ELSE child."chat_session_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id"))
)
DELETE FROM "chat_session" chat_session WHERE (chat_session."id") IN (SELECT "id" FROM cleanup_target_chat_session) ;


-- name: TouchChatSession :exec
UPDATE chat_session SET updated_at = now()
WHERE id = $1;

-- name: CreateChatMessage :one
-- message_kind defaults to 'message' via COALESCE so every existing caller
-- (which omits it) keeps writing ordinary messages; the empty-reply path passes
-- 'no_response' to mark a visible turn with no text output (SILI-4351).
INSERT INTO chat_message (
    chat_session_id, role, content, run_id, failure_reason, elapsed_ms,
    message_kind, channel_media_pending_until, channel_ingested,
    sender_user_id, sender_agent_id, mentioned_agent_ids
) SELECT
    $1 AS chat_session_id,
    $2 AS role,
    $3 AS content,
    sqlc.narg(run_id) AS run_id,
    sqlc.narg(failure_reason) AS failure_reason,
    sqlc.narg(elapsed_ms) AS elapsed_ms,
    COALESCE(sqlc.narg(message_kind)::text, 'message') AS message_kind,
    -- The media deadline is DB-clock time: every consumer compares it against
    -- SQL now() (GetChannelMediaPendingUntil, the deferred promote, the
    -- trailing-message guard), so the writer must use the same clock. The
    -- caller passes a relative budget in seconds; an application-clock
    -- timestamp here would let a skewed app node shrink or stretch the
    -- fallback window.
    CASE WHEN sqlc.narg(channel_media_pending_secs)::float8 IS NULL THEN NULL
         ELSE now() + make_interval(secs => sqlc.narg(channel_media_pending_secs)::float8) END AS channel_media_pending_until,
    COALESCE(sqlc.narg(channel_ingested)::boolean, FALSE) AS channel_ingested,
    sqlc.narg(sender_user_id) AS sender_user_id,
    sqlc.narg(sender_agent_id) AS sender_agent_id,
    COALESCE(sqlc.narg(mentioned_agent_ids)::uuid[], '{}') AS mentioned_agent_ids
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM chat_session AS ref_chat_session_id WHERE ref_chat_session_id.id = ($1) FOR KEY SHARE))

RETURNING *;

-- name: RunHasChannelIngestedMessages :one
-- Immutable channel provenance for a run's user-message input batch:
-- channel_ingested is stamped inside the channel append transaction and never
-- mutated afterwards, so it survives session archiving and installation
-- rebinds that delete the channel_chat_session_binding row. Callers pass the
-- batch OWNER id (chat_input_run_id, which auto-retry clones inherit), not
-- necessarily the run's own id. The cancel restore-delete and the
-- empty-completion silent-drop both gate on this — a channel sender has no
-- Silieco composer for a restored draft, and the no_response fallback body
-- must never be pushed to an external channel.
SELECT EXISTS (
    SELECT 1 FROM chat_message
    WHERE run_id = $1
      AND role = 'user'
      AND channel_ingested
) AS channel_ingested;

-- name: GetChannelMediaPendingUntil :one
-- The latest unexpired media deadline gates a channel run. Using a durable
-- run fire_at means a process restart still produces the placeholder fallback.
SELECT channel_media_pending_until
FROM chat_message
WHERE chat_session_id = $1
  AND role = 'user'
  AND channel_media_pending_until > now()
ORDER BY channel_media_pending_until DESC
LIMIT 1;

-- name: ClearChatMessageChannelMediaPending :exec
UPDATE chat_message
SET channel_media_pending_until = NULL
WHERE id = $1 AND chat_session_id = $2;

-- name: LinkChatMessageToRun :exec
UPDATE chat_message
SET run_id = $2
WHERE id = $1 AND role = 'user';

-- name: LinkUnownedChannelChatMessagesToRun :exec
-- Seals the trailing channel-message batch to its run. The run row and these
-- links are committed together, so an older in-flight run cannot absorb a
-- newer media message and a later assistant row cannot hide that message.
UPDATE chat_message AS message
SET run_id = @run_id
WHERE message.chat_session_id = @chat_session_id
  AND message.role = 'user'
  AND message.run_id IS NULL
  AND NOT EXISTS (
      SELECT 1
      FROM chat_message AS prior
      WHERE prior.chat_session_id = @chat_session_id
        AND prior.role != 'user'
        AND (prior.created_at, prior.id) > (message.created_at, message.id)
  );

-- name: DeferChatRunForSealedPendingMedia :one
-- Closes the enqueue-vs-append race: under READ COMMITTED a media message can
-- commit between GetChannelMediaPendingUntil and the batch seal above, landing
-- an unexpired media marker inside a run the deadline read decided was
-- 'queued'. Re-derive the deferral from the sealed batch itself, in the same
-- transaction, so a run is never claimable while its own input still has an
-- unexpired marker. No row (ErrNoRows) means no correction was needed.
UPDATE agent_run AS run
SET status = 'deferred', fire_at = pending.max_until
FROM (
    SELECT max(message.channel_media_pending_until) AS max_until
    FROM chat_message AS message
    WHERE message.run_id = @run_id
      AND message.role = 'user'
      AND message.channel_media_pending_until > now()
) AS pending
WHERE run.id = @run_id
  AND pending.max_until IS NOT NULL
  AND (run.fire_at IS NULL OR run.fire_at < pending.max_until)
RETURNING run.*;

-- name: DeleteUserChatMessageByRun :one
WITH RECURSIVE cleanup_target_attachment AS MATERIALIZED (
    SELECT child.* FROM "attachment" child WHERE EXISTS (SELECT 1 FROM cleanup_target_chat_message parent WHERE child."chat_message_id" = parent."id")
),
cleanup_target_chat_message AS MATERIALIZED (
    SELECT chat_message.* FROM "chat_message" chat_message WHERE chat_message.run_id = $1 AND chat_message.role = 'user'
),
cleanup_delete_attachment AS (
    DELETE FROM "attachment" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_attachment)
)
DELETE FROM "chat_message" chat_message WHERE (chat_message."id") IN (SELECT "id" FROM cleanup_target_chat_message) RETURNING *;


-- name: ListChatMessages :many
SELECT * FROM chat_message
WHERE chat_session_id = $1
ORDER BY created_at ASC, id ASC;

-- name: ListChatInputMessages :many
-- Loads the immutable user-message input batch owned by a direct-chat run.
-- The caller passes the run's chat_input_run_id (itself for an original send,
-- the root run for an auto-retry child), so a claim reads exactly the messages
-- the user sent for this turn — and never absorbs a message that arrived after
-- the batch was sealed, no matter what the assistant wrote or when. Only used
-- for new run-owned direct-chat runs; legacy/channel (chat_input_run_id
-- NULL) runs keep using ListChatMessages + trailingUserMessages.
SELECT * FROM chat_message
WHERE run_id = $1 AND role = 'user'
ORDER BY created_at ASC, id ASC;

-- name: ListChatMessagesPage :many
SELECT * FROM chat_message
WHERE chat_session_id = $1
  AND (
    sqlc.narg('before_created_at')::timestamptz IS NULL
    OR (created_at, id) < (sqlc.narg('before_created_at')::timestamptz, sqlc.narg('before_id')::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: GetChatMessage :one
SELECT * FROM chat_message
WHERE id = $1;

-- name: CreateChatRun :one
-- The chat sender (initiator) is a direct_human originator and accountable;
-- attribution provenance is stamped so this path is not a NULL-source enqueue
-- bypass (SILI-4302 §2).
INSERT INTO agent_run (
    agent_id, runtime_id, task_id, status, priority, chat_session_id,
    initiator_user_id, originator_user_id, accountable_user_id, force_fresh_session, runtime_mcp_overlay,
    runtime_connected_apps, originator_source, trigger_evidence_kind, trigger_evidence_ref_id,
    fire_at
) SELECT
    $1 AS agent_id,
    $2 AS runtime_id,
    NULL AS task_id,
    CASE WHEN sqlc.narg('fire_at')::timestamptz IS NULL THEN 'queued' ELSE 'deferred' END AS status,
    $3 AS priority,
    $4 AS chat_session_id,
    $5 AS initiator_user_id,
    sqlc.narg(originator_user_id) AS originator_user_id,
    sqlc.narg(accountable_user_id) AS accountable_user_id,
    COALESCE(sqlc.narg('force_fresh_session')::boolean, FALSE) AS force_fresh_session,
    sqlc.narg(runtime_mcp_overlay) AS runtime_mcp_overlay,
    sqlc.narg(runtime_connected_apps) AS runtime_connected_apps,
    sqlc.narg(originator_source) AS originator_source,
    sqlc.narg(trigger_evidence_kind) AS trigger_evidence_kind,
    sqlc.narg(trigger_evidence_ref_id) AS trigger_evidence_ref_id,
    sqlc.narg('fire_at')::timestamptz AS fire_at
WHERE (($2)::uuid IS NULL OR EXISTS (SELECT 1 FROM agent_runtime AS ref_runtime_id WHERE ref_runtime_id.id = ($2) FOR KEY SHARE))
  AND (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM agent AS ref_agent_id WHERE ref_agent_id.id = ($1) FOR KEY SHARE))
  AND (($4)::uuid IS NULL OR EXISTS (SELECT 1 FROM chat_session AS ref_chat_session_id WHERE ref_chat_session_id.id = ($4) FOR KEY SHARE))

RETURNING *;

-- name: CreateGroupChatRun :one
INSERT INTO agent_run (
    agent_id, runtime_id, task_id, status, priority, chat_session_id,
    initiator_user_id, originator_user_id, accountable_user_id,
    force_fresh_session, runtime_mcp_overlay, runtime_connected_apps,
    originator_source, trigger_evidence_kind, trigger_evidence_ref_id,
    trigger_chat_message_id
) SELECT
    sqlc.arg(agent_id) AS agent_id,
    sqlc.arg(runtime_id) AS runtime_id,
    NULL AS task_id,
    'queued' AS status,
    2 AS priority,
    sqlc.arg(chat_session_id) AS chat_session_id,
    sqlc.arg(initiator_user_id) AS initiator_user_id,
    sqlc.narg(originator_user_id) AS originator_user_id,
    sqlc.narg(accountable_user_id) AS accountable_user_id,
    TRUE AS force_fresh_session,
    sqlc.narg(runtime_mcp_overlay) AS runtime_mcp_overlay,
    sqlc.narg(runtime_connected_apps) AS runtime_connected_apps,
    sqlc.narg(originator_source) AS originator_source,
    sqlc.narg(trigger_evidence_kind) AS trigger_evidence_kind,
    sqlc.narg(trigger_evidence_ref_id) AS trigger_evidence_ref_id,
    sqlc.arg(trigger_chat_message_id) AS trigger_chat_message_id
WHERE ((sqlc.arg(runtime_id))::uuid IS NULL OR EXISTS (SELECT 1 FROM agent_runtime AS ref_runtime_id WHERE ref_runtime_id.id = (sqlc.arg(runtime_id)) FOR KEY SHARE))
  AND ((sqlc.arg(agent_id))::uuid IS NULL OR EXISTS (SELECT 1 FROM agent AS ref_agent_id WHERE ref_agent_id.id = (sqlc.arg(agent_id)) FOR KEY SHARE))
  AND ((sqlc.arg(chat_session_id))::uuid IS NULL OR EXISTS (SELECT 1 FROM chat_session AS ref_chat_session_id WHERE ref_chat_session_id.id = (sqlc.arg(chat_session_id)) FOR KEY SHARE))

RETURNING *;

-- name: PromoteChannelChatRunsIfMediaReady :many
-- Media completion may race with the 3s run batcher. Promote every original
-- channel run waiting for this session only after all unexpired media markers
-- are gone; retry/escalation/direct-chat deferred runs are excluded.
UPDATE agent_run AS run
SET status = 'queued', fire_at = NULL
WHERE run.chat_session_id = @chat_session_id
  AND run.status = 'deferred'
  AND run.task_id IS NULL
  AND run.parent_run_id IS NULL
  AND run.escalation_for_run_id IS NULL
  AND NOT EXISTS (
      SELECT 1
      FROM chat_message AS message
      WHERE message.chat_session_id = @chat_session_id
        AND message.role = 'user'
        AND message.channel_media_pending_until > now()
  )
RETURNING run.*;

-- name: SetChatRunInputOwnerSelf :one
-- Stamps a freshly-created direct-chat run as the owner of its own input batch
-- (chat_input_run_id = id), so a later claim loads exactly the user messages
-- tagged with this run id (ListChatInputMessages) rather than scanning trailing
-- history. Runs in the same transaction as CreateChatRun + message ownership:
-- direct-send inserts one owned message, while channel enqueue seals its
-- trailing unowned batch. Legacy runs keep chat_input_run_id NULL and retain
-- the trailing-history fallback during rolling deploys.
UPDATE agent_run
SET chat_input_run_id = id
WHERE id = $1
RETURNING *;

-- name: GetLastChatRunSession :one
-- Returns the most recent run in this chat session that managed to record a
-- session_id. Includes both completed and failed runs: even a failed run
-- may have established a real agent session before failing, and we'd rather
-- resume there than start over and lose conversation memory. Used as a
-- fallback when chat_session.session_id is NULL. Resume-unsafe failures are
-- excluded because replaying those sessions deterministically reproduces the
-- same terminal state. Keep this list in sync with resumeUnsafeFailureReason
-- and GetLastRunSession.
--
-- The regex pair mirrors GetLastRunSession's provider-agnostic guard for an
-- empty message baked into the conversation history: both must match, and
-- both track emptyContentRe / historyMessageLocatorRe in
-- pkg/taskfailure/resume.go (GH #6066).
--
-- Selection is per-session, not per-row, and retired sessions are excluded —
-- both mirroring GetLastRunSession, which this query had drifted away from.
-- A plain row-level filter reopens the poisoning wormhole GH #5975 closed on
-- the task side: it drops the newest poisoned row for a session and then
-- happily falls back to an OLDER completed row carrying the same dead
-- session_id. Judging each session by its LATEST terminal state means a newer
-- poisoned row invalidates the whole session, while a genuinely different
-- healthy session stays eligible.
WITH retired_sessions AS (
    SELECT DISTINCT r.retired_session_id AS session_id
    FROM agent_run r
    WHERE r.chat_session_id = $1
      AND r.retired_session_id IS NOT NULL
), latest_per_session AS (
    SELECT DISTINCT ON (t.session_id)
        t.session_id, t.work_dir, t.runtime_id, t.status, t.failure_reason, t.error, t.completed_at
    FROM agent_run t
    WHERE t.chat_session_id = $1
      AND t.session_id IS NOT NULL
      AND t.status IN ('completed', 'failed')
    ORDER BY t.session_id, t.completed_at DESC
)
SELECT session_id, work_dir, runtime_id FROM latest_per_session
WHERE session_id NOT IN (SELECT session_id FROM retired_sessions)
  AND (
    status = 'completed'
    OR (
      status = 'failed'
      AND COALESCE(failure_reason, '') NOT IN ('iteration_limit', 'agent_fallback_message', 'api_invalid_request', 'codex_semantic_inactivity', 'agent_error.context_overflow')
      AND NOT (COALESCE(error, '') ILIKE '%400%' AND COALESCE(error, '') ILIKE '%invalid_request_error%')
      AND NOT (COALESCE(error, '') ~* 'must not be empty|must be non-?empty|must have non-?empty|non-?empty content|cannot be empty|should not be empty'
               AND COALESCE(error, '') ~* 'role[^a-z0-9]{0,2}assistant|assistant message|message at position|messages\.[0-9]|messages\[[0-9]')
    )
  )
ORDER BY completed_at DESC
LIMIT 1;

-- name: GetPendingChatRun :one
-- Returns the most recent in-flight run for a chat session, if any.
-- Used by the frontend to recover pending state after refresh / reopen.
-- created_at is the anchor for the chat StatusPill timer (it computes
-- elapsed = now - run.created_at), so the pill survives refresh / reopen
-- without "resetting to 0s".
SELECT id, status, created_at FROM agent_run
WHERE chat_session_id = $1 AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
ORDER BY created_at DESC
LIMIT 1;

-- name: ListPendingChatRunsByCreator :many
-- Aggregate view of all in-flight chat runs owned by a given creator in a
-- workspace. Drives the FAB's "running" indicator when the chat window is
-- closed and no single session's query is active.
--
-- Returns cs.agent_id so the handler can filter runs belonging to private
-- agents the caller has lost access to using the already-loaded `allowed`
-- set — no second ListAllChatSessionsByCreator scan on the hot path.
--
-- atq.chat_session_id IS NOT NULL is redundant given the JOIN, but stated
-- explicitly so the planner can prove the query predicate is a subset of the
-- idx_agent_run_chat_pending_v2 partial-index predicate and use it.
SELECT atq.id AS run_id, atq.status, atq.chat_session_id, atq.agent_id
FROM agent_run atq
JOIN chat_session cs ON cs.id = atq.chat_session_id
LEFT JOIN chat_session_participant self_participant
  ON self_participant.chat_session_id = cs.id
 AND self_participant.participant_type = 'member'
 AND self_participant.participant_id = $2
WHERE atq.chat_session_id IS NOT NULL
  AND atq.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
  AND cs.workspace_id = $1
  AND (
    (cs.kind = 'direct' AND cs.creator_id = $2)
    OR (cs.kind = 'group' AND self_participant.participant_id IS NOT NULL)
  )
ORDER BY atq.created_at DESC;

-- name: HasPendingChatRunsByCreator :one
-- Boolean fast-path for the FAB's "running" indicator. Returns a single
-- EXISTS row instead of the full run list, so the planner can stop at the
-- first matching in-flight run (LIMIT 1 semantics via EXISTS).
--
-- Permission filtering is baked into the query: agent_id = ANY($3) restricts
-- the result to the agents the caller may currently see, so a member who lost
-- access to a private agent never gets a true from a run they can no longer
-- reach. The handler must pass its resolved accessible-agent id set as $3;
-- an empty array yields false.
SELECT EXISTS (
  SELECT 1
  FROM agent_run atq
  JOIN chat_session cs ON cs.id = atq.chat_session_id
  LEFT JOIN chat_session_participant self_participant
    ON self_participant.chat_session_id = cs.id
   AND self_participant.participant_type = 'member'
   AND self_participant.participant_id = sqlc.arg(creator_id)
  WHERE atq.chat_session_id IS NOT NULL
    AND atq.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
    AND cs.workspace_id = sqlc.arg(workspace_id)
    AND (
      (cs.kind = 'direct' AND cs.creator_id = sqlc.arg(creator_id))
      OR (cs.kind = 'group' AND self_participant.participant_id IS NOT NULL)
    )
    AND atq.agent_id = ANY(sqlc.arg(agent_ids)::uuid[])
) AS has_pending;

-- name: MarkChatSessionRead :exec
-- Advances the read cursor to now, dropping the session's unread_count to 0.
UPDATE chat_session SET last_read_at = now()
WHERE id = $1;

-- name: MarkGroupChatSessionRead :exec
UPDATE chat_session_participant SET last_read_at = now()
WHERE chat_session_id = $1
  AND participant_type = 'member'
  AND participant_id = $2;

-- name: GetMostRecentUserChatMessage :one
-- Returns the most recent role='user' message in a session. Used by the
-- Lark `/task` command parser: when the user types `/task` with no
-- title, the spec falls back to "use the previous user message as the
-- title". Bot replies (role='assistant') are excluded — only human
-- input qualifies as a fallback title source.
SELECT * FROM chat_message
WHERE chat_session_id = $1 AND role = 'user'
ORDER BY created_at DESC
LIMIT 1;

-- name: ChatSessionHasUserMessage :one
-- Reports whether a session has any human (role='user') message yet. Used to
-- scope the is_agent_intro self-introduction prompt to the very first,
-- server-driven turn: an intro session starts with zero user messages, so the
-- opening run gets the "introduce yourself" prompt. Once the creator replies,
-- later turns in the same session must fall back to the normal reply prompt
-- instead of repeating the introduction every turn (SILI-4259).
SELECT EXISTS (
    SELECT 1 FROM chat_message
    WHERE chat_session_id = $1 AND role = 'user'
) AS has_user_message;

-- name: CreateChatDraftRestore :one
-- Persists the deferred-cancellation draft restore (#5219) in the same tx
-- that deletes the triggering user message: the chat:cancel_finalized
-- broadcast is best-effort, so an offline client recovers the draft from
-- this row instead. id is the deleted message's id.
INSERT INTO chat_draft_restore (id, chat_session_id, run_id, content, attachment_ids)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListChatDraftRestoresBySession :many
SELECT * FROM chat_draft_restore
WHERE chat_session_id = $1
ORDER BY created_at ASC;

-- name: DeleteChatDraftRestore :execrows
-- Idempotent consume: deleting an already-consumed restore matches no row.
DELETE FROM chat_draft_restore
WHERE id = $1 AND chat_session_id = $2;

-- name: DeleteChatDraftRestoresBySession :exec
-- chat_draft_restore carries no chat_session FK (SILI-3515), so DeleteChatSession
-- prunes its pending restores in the same tx that deletes the session.
DELETE FROM chat_draft_restore
WHERE chat_session_id = $1;

-- The chat_session row lock is the mutual-exclusion protocol between the
-- draft-restore writer (FinalizeDeferredCancelledChat) and every deleter of a
-- chat_session or one of its cascade parents. Without an FK, an INSERT into
-- chat_draft_restore takes no lock on its session, so a prune-then-delete would
-- otherwise miss a restore committed after its snapshot and strand it — with the
-- user's prompt in it — forever (#5219).
--
-- The contract, held by all five paths below:
--   deleter:   lock the sessions FOR UPDATE -> prune restores -> delete the parent
--   finalizer: lock the session FOR UPDATE  -> insert the restore
-- Whoever locks first wins: a finalizer that got there first commits its row
-- before the deleter's prune statement takes its snapshot, so the prune sweeps
-- it; a deleter that got there first leaves no session for the finalizer to
-- lock, so it never inserts.
--
-- Lock order is chat_session -> agent_run everywhere (the finalizer locks
-- the session before claiming its run) so the deleters' cascade into
-- agent_run cannot deadlock against it.
--
-- The single-session delete path needs no new query: it already holds
-- LockChatSessionForDelete (same FOR UPDATE row lock) across its prune.

-- name: LockChatSessionForRun :one
-- The finalizer's half of the protocol. No rows means the session is already
-- gone (its cascade NULLs agent_run.chat_session_id), so there is nothing
-- to lock and nothing to restore into.
SELECT cs.id
FROM agent_run t
JOIN chat_session cs ON cs.id = t.chat_session_id
WHERE t.id = $1
FOR UPDATE OF cs;

-- name: LockChatSessionsByWorkspace :many
-- ORDER BY id: a stable lock order keeps two concurrent deleters from
-- deadlocking against each other.
SELECT id FROM chat_session
WHERE workspace_id = $1
ORDER BY id
FOR UPDATE;

-- name: LockChatSessionsByArchivedRuntimeAgents :many
SELECT cs.id FROM chat_session cs
JOIN agent a ON a.id = cs.agent_id
WHERE a.runtime_id = $1 AND a.archived_at IS NOT NULL
ORDER BY cs.id
FOR UPDATE OF cs;

-- name: LockChatSessionsBySystemRuntimeAgents :many
SELECT cs.id FROM chat_session cs
JOIN agent a ON a.id = cs.agent_id
WHERE a.runtime_id = $1 AND a.kind = 'system'
ORDER BY cs.id
FOR UPDATE OF cs;

-- name: DeleteChatDraftRestoresByArchivedRuntimeAgents :exec
-- chat_session cascades from agent, so hard-deleting a runtime's archived agents
-- silently drops their sessions — and, without an FK, would strand the pending
-- restores (which still hold the user's prompt text) forever. Prune them in the
-- same tx, BEFORE the agent rows go: the join below needs them. Mirrors
-- DeleteChannelInstallationsByArchivedRuntimeAgents.
DELETE FROM chat_draft_restore
WHERE chat_session_id IN (
    SELECT cs.id FROM chat_session cs
    JOIN agent a ON a.id = cs.agent_id
    WHERE a.runtime_id = $1 AND a.archived_at IS NOT NULL
);

-- name: DeleteChatDraftRestoresBySystemRuntimeAgents :exec
-- Same cascade, for the system agents a runtime teardown also hard-deletes
-- (DeleteSystemAgentsByRuntime). Split from the archived-agent prune because the
-- runtime-profile teardown deletes only archived agents: pruning system-agent
-- sessions there would destroy restores whose session survives.
DELETE FROM chat_draft_restore
WHERE chat_session_id IN (
    SELECT cs.id FROM chat_session cs
    JOIN agent a ON a.id = cs.agent_id
    WHERE a.runtime_id = $1 AND a.kind = 'system'
);
