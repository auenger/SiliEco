-- name: CreateSquad :one
WITH workspace_guard AS MATERIALIZED (
 SELECT id FROM workspace WHERE id=$1 FOR KEY SHARE
), leader_guard AS MATERIALIZED (
 SELECT id FROM agent WHERE id=$4 AND workspace_id=$1 FOR KEY SHARE
)
INSERT INTO squad (workspace_id, name, description, leader_id, creator_id, avatar_url)
SELECT $1, $2, $3, $4, $5, $6
WHERE EXISTS (SELECT 1 FROM workspace_guard) AND EXISTS (SELECT 1 FROM leader_guard)
RETURNING *;

-- name: GetSquad :one
SELECT * FROM squad WHERE squad.id = $1;

-- name: GetSquadInWorkspace :one
SELECT * FROM squad WHERE squad.id = $1 AND workspace_id = $2;

-- name: ListSquads :many
SELECT * FROM squad WHERE squad.workspace_id = $1 AND archived_at IS NULL ORDER BY created_at ASC;

-- name: ListSquadMemberPreviewRows :many
-- Static squad membership summary for list/hover previews. This deliberately
-- excludes derived runtime/run status; the squad detail members-status
-- endpoint owns live state.
SELECT
    sm.squad_id,
    sm.member_type,
    sm.member_id,
    sm.role
FROM squad_member sm
JOIN squad s ON s.id = sm.squad_id
WHERE s.workspace_id = $1 AND s.archived_at IS NULL
ORDER BY
    sm.squad_id ASC,
    (sm.member_type = 'agent' AND sm.member_id = s.leader_id) DESC,
    sm.created_at ASC;

-- name: ListSquadMemberPreviewRowsBySquad :many
SELECT
    sm.squad_id,
    sm.member_type,
    sm.member_id,
    sm.role
FROM squad_member sm
JOIN squad s ON s.id = sm.squad_id
WHERE sm.squad_id = $1
ORDER BY
    (sm.member_type = 'agent' AND sm.member_id = s.leader_id) DESC,
    sm.created_at ASC;

-- name: ListAllSquads :many
SELECT * FROM squad WHERE squad.workspace_id = $1 ORDER BY created_at ASC;

-- name: UpdateSquad :one
WITH leader_guard AS MATERIALIZED (
 SELECT agent.id FROM agent
 WHERE agent.id=sqlc.narg('leader_id')
 AND agent.workspace_id=(SELECT squad.workspace_id FROM squad WHERE squad.id=$1)
 FOR KEY SHARE
)
UPDATE squad SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    leader_id = COALESCE(sqlc.narg('leader_id'), leader_id),
    avatar_url = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    instructions = COALESCE(sqlc.narg('instructions'), instructions),
    updated_at = now()
WHERE squad.id = $1
 AND (sqlc.narg('leader_id')::uuid IS NULL OR EXISTS (SELECT 1 FROM leader_guard))
RETURNING *;

-- name: ArchiveSquad :one
UPDATE squad SET archived_at = now(), archived_by = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: AddSquadMember :one
INSERT INTO squad_member (squad_id, member_type, member_id, role) SELECT
    $1 AS squad_id,
    $2 AS member_type,
    $3 AS member_id,
    $4 AS role
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM squad AS ref_squad_id WHERE ref_squad_id.id = ($1) FOR KEY SHARE))

RETURNING *;

-- name: RemoveSquadMember :execrows
DELETE FROM squad_member
WHERE squad_id = $1 AND member_type = $2 AND member_id = $3;

-- name: ListSquadMembers :many
SELECT * FROM squad_member WHERE squad_id = $1 ORDER BY created_at ASC;

-- name: UpdateSquadMemberRole :one
UPDATE squad_member SET role = $4
WHERE squad_id = $1 AND member_type = $2 AND member_id = $3
RETURNING *;

-- name: IsSquadMember :one
SELECT EXISTS(
    SELECT 1 FROM squad_member
    WHERE squad_id = $1 AND member_type = $2 AND member_id = $3
) AS is_member;

-- name: CountSquadMembers :one
SELECT count(*) FROM squad_member WHERE squad_id = $1;

-- name: GetSquadByAssignee :one
-- Look up the squad when an task is assigned to a squad.
SELECT s.* FROM squad s WHERE s.id = $1 AND s.workspace_id = $2;

-- name: ListSquadsByMember :many
-- Find all squads a given entity belongs to in a workspace.
SELECT s.* FROM squad s
JOIN squad_member sm ON sm.squad_id = s.id
WHERE s.workspace_id = $1 AND sm.member_type = $2 AND sm.member_id = $3
ORDER BY s.created_at ASC;

-- name: TransferSquadAssignees :exec
-- Transfer all tasks assigned to a squad to the squad's leader agent.
UPDATE task SET assignee_type = 'agent', assignee_id = $2, updated_at = now()
WHERE assignee_type = 'squad' AND assignee_id = $1;

-- name: TransferSquadAutopilotsToLeader :exec
-- Mirrors TransferSquadAssignees for autopilot rows: when a squad is archived,
-- any autopilot still pointing at the squad would otherwise dangle and the
-- admission gate would skip every subsequent dispatch with "assignee squad
-- cannot be resolved". Rewrite the assignee in place to the leader agent so
-- the autopilot keeps firing under the same leader-only execution semantics
-- it had a moment before the archive (Path A from SILI-2429).
UPDATE autopilot
SET assignee_type = 'agent',
    assignee_id = $2,
    updated_at = now()
WHERE assignee_type = 'squad' AND assignee_id = $1;

-- name: ListSquadMemberStatusRows :many
-- Per-row join used to build the squad-members status view. One row per
-- (squad_member × active_run); members with no active run return a
-- single row with NULL run_* columns. Human members and agent members
-- with no agent row also return one row with NULL agent_/runtime_ columns.
-- The handler aggregates rows by member_id.
SELECT
    sm.id              AS squad_member_id,
    sm.member_type     AS member_type,
    sm.member_id       AS member_id,
    a.archived_at      AS agent_archived_at,
    ar.status          AS runtime_status,
    ar.last_seen_at    AS runtime_last_seen_at,
    atq.id             AS run_id,
    atq.status         AS run_status,
    atq.task_id       AS run_task_id,
    atq.dispatched_at  AS run_dispatched_at,
    i.number           AS task_number,
    i.title            AS task_title,
    i.status           AS task_status
FROM squad_member sm
LEFT JOIN agent a
       ON sm.member_type = 'agent' AND a.id = sm.member_id
LEFT JOIN agent_runtime ar
       ON ar.id = a.runtime_id
LEFT JOIN agent_run atq
       ON sm.member_type = 'agent'
      AND atq.agent_id = sm.member_id
      AND atq.status IN ('dispatched', 'running', 'waiting_local_directory')
LEFT JOIN task i
       ON i.id = atq.task_id
WHERE sm.squad_id = $1
ORDER BY sm.created_at ASC, atq.dispatched_at DESC NULLS LAST;
