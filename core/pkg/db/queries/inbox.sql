-- name: ListInboxItems :many
SELECT i.*,
       iss.status as task_status
FROM inbox_item i
LEFT JOIN task iss ON iss.id = i.task_id
WHERE i.workspace_id = $1 AND i.recipient_type = $2 AND i.recipient_id = $3 AND i.archived = false
ORDER BY i.created_at DESC;

-- name: ListArchivedInboxItems :many
-- Archived counterpart of ListInboxItems, backing the inbox's "Archived"
-- sub-view (SILI-3736).
--
-- An task whose group still has an active row is excluded: archiving is
-- task-level, so a NEW notification on an already-archived task leaves the
-- old archived rows in place alongside the fresh active one. The task belongs
-- in the main inbox at that point, and the two lists must stay mutually
-- exclusive per task group — otherwise the same task renders in both. The
-- exclusion lives here rather than in the client so neither list depends on
-- the other's cache being loaded. Items without an task_id group on their own
-- id and can never have an active sibling, hence the IS NULL short-circuit.
--
-- LIMIT bounds the response while the archive grows without end (v1 ships no
-- pagination). Rows are newest-first, so truncation drops the OLDEST rows and
-- can never hide a group's newest row — the one the deduplicated UI renders.
SELECT i.*,
       iss.status as task_status
FROM inbox_item i
LEFT JOIN task iss ON iss.id = i.task_id
WHERE i.workspace_id = $1 AND i.recipient_type = $2 AND i.recipient_id = $3 AND i.archived = true
  AND (i.task_id IS NULL OR NOT EXISTS (
      SELECT 1
      FROM inbox_item active
      WHERE active.workspace_id = i.workspace_id
        AND active.recipient_type = i.recipient_type
        AND active.recipient_id = i.recipient_id
        AND active.task_id = i.task_id
        AND active.archived = false
  ))
ORDER BY i.created_at DESC
LIMIT 200;

-- name: GetInboxItem :one
SELECT * FROM inbox_item
WHERE id = $1;

-- name: GetInboxItemInWorkspace :one
SELECT * FROM inbox_item
WHERE id = $1 AND workspace_id = $2;

-- name: CreateInboxItem :one
INSERT INTO inbox_item (
    workspace_id, recipient_type, recipient_id,
    type, severity, task_id, title, body,
    actor_type, actor_id, details
) SELECT
    $1 AS workspace_id,
    $2 AS recipient_type,
    $3 AS recipient_id,
    $4 AS type,
    $5 AS severity,
    $6 AS task_id,
    $7 AS title,
    $8 AS body,
    $9 AS actor_type,
    $10 AS actor_id,
    $11 AS details
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM workspace AS ref_workspace_id WHERE ref_workspace_id.id = ($1) FOR KEY SHARE))
  AND (($6)::uuid IS NULL OR EXISTS (SELECT 1 FROM task AS ref_task_id WHERE ref_task_id.id = ($6) FOR KEY SHARE))

RETURNING *;

-- name: MarkInboxRead :one
UPDATE inbox_item SET read = true
WHERE id = $1
RETURNING *;

-- name: ArchiveInboxItem :one
UPDATE inbox_item SET archived = true
WHERE id = $1
RETURNING *;

-- name: ArchiveInboxByTask :execrows
UPDATE inbox_item SET archived = true
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND task_id = $4 AND archived = false;

-- name: UnarchiveInboxItem :one
-- Deliberately does not touch `read`: unarchiving restores an item to the main
-- inbox in the exact read/unread state it was archived in, so restoring an
-- unread item legitimately raises the unread badge again (SILI-3736).
UPDATE inbox_item SET archived = false
WHERE id = $1
RETURNING *;

-- name: UnarchiveInboxByTask :execrows
-- Task-level restore, mirroring ArchiveInboxByTask: archiving one item
-- archives every sibling for the same task, so unarchiving must bring the
-- whole group back. Leaves `read` untouched for the same reason as above.
UPDATE inbox_item SET archived = false
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND task_id = $4 AND archived = true;

-- name: ArchiveInboxByTaskAndType :many
UPDATE inbox_item SET archived = true
WHERE workspace_id = $1 AND task_id = $2 AND type = $3 AND archived = false
RETURNING recipient_type, recipient_id;

-- name: CountUnreadInbox :one
SELECT count(*) FROM inbox_item
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND read = false AND archived = false;

-- name: CountUnreadInboxByWorkspace :many
-- Per-workspace unread inbox counts for a recipient member, matching the
-- inbox UI's deduplicated view: notifications are grouped per task
-- (Linear-style, one row per task) and an task counts as unread only when
-- its NEWEST non-archived item is unread. Opening an task marks just that
-- newest item read, so counting raw unread rows would keep older siblings
-- alive and light the switcher dot for a workspace whose inbox the user sees
-- as empty (SILI-3695). Items without an task group on their own id. The
-- member join keeps counts scoped to workspaces the user still belongs to,
-- so a stale item left behind in a workspace the user has since left cannot
-- light the dot.
SELECT newest.workspace_id, count(*) AS count
FROM (
    SELECT DISTINCT ON (i.workspace_id, COALESCE(i.task_id, i.id))
        i.workspace_id, i.read
    FROM inbox_item i
    JOIN member m ON m.workspace_id = i.workspace_id AND m.user_id = i.recipient_id
    WHERE i.recipient_type = 'member'
      AND i.recipient_id = $1
      AND i.archived = false
    ORDER BY i.workspace_id, COALESCE(i.task_id, i.id), i.created_at DESC
) newest
WHERE newest.read = false
GROUP BY newest.workspace_id;

-- name: MarkAllInboxRead :execrows
UPDATE inbox_item SET read = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND archived = false AND read = false;

-- name: ArchiveAllInbox :execrows
UPDATE inbox_item SET archived = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND archived = false;

-- name: ArchiveAllReadInbox :execrows
UPDATE inbox_item SET archived = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND read = true AND archived = false;

-- name: ArchiveCompletedInbox :execrows
UPDATE inbox_item i SET archived = true
WHERE i.workspace_id = $1 AND i.recipient_type = 'member' AND i.recipient_id = $2 AND i.archived = false
  AND i.task_id IN (SELECT id FROM task WHERE status IN ('done', 'cancelled'));
