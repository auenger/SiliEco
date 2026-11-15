-- name: ListLabels :many
SELECT l.*,
    CASE l.resource_type
        WHEN 'task' THEN (SELECT COUNT(*) FROM task_to_label x WHERE x.label_id = l.id)
        WHEN 'agent' THEN (SELECT COUNT(*) FROM agent_to_label x WHERE x.label_id = l.id)
        WHEN 'skill' THEN (SELECT COUNT(*) FROM skill_to_label x WHERE x.label_id = l.id)
        ELSE 0
    END::bigint AS usage_count
FROM task_label l
WHERE l.workspace_id = sqlc.arg('workspace_id')::uuid
  AND l.resource_type = sqlc.arg('resource_type')::text
ORDER BY LOWER(name) ASC;

-- name: GetLabel :one
SELECT * FROM task_label
WHERE id = $1 AND workspace_id = $2;

-- name: CreateLabel :one
INSERT INTO task_label (workspace_id, resource_type, name, description, color) SELECT
    $1 AS workspace_id,
    $2 AS resource_type,
    $3 AS name,
    $4 AS description,
    $5 AS color
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM workspace AS ref_workspace_id WHERE ref_workspace_id.id = ($1) FOR KEY SHARE))

RETURNING *;

-- name: UpdateLabel :one
UPDATE task_label SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    color = COALESCE(sqlc.narg('color'), color),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteLabel :one
-- :one RETURNING id so the handler distinguishes pgx.ErrNoRows (→ 404) from
-- infrastructure errors (→ 500), and avoids a TOCTOU precheck.
WITH RECURSIVE cleanup_target_task_label AS MATERIALIZED (
    SELECT task_label.* FROM "task_label" task_label WHERE task_label.id = $1 AND task_label.workspace_id = $2
),
cleanup_target_task_to_label AS MATERIALIZED (
    SELECT child.* FROM "task_to_label" child WHERE EXISTS (SELECT 1 FROM cleanup_target_task_label parent WHERE child."label_id" = parent."id")
),
cleanup_delete_task_to_label AS (
    DELETE FROM "task_to_label" child WHERE (child."task_id", child."label_id") IN (SELECT "task_id", "label_id" FROM cleanup_target_task_to_label)
)
DELETE FROM "task_label" task_label WHERE (task_label."id") IN (SELECT "id" FROM cleanup_target_task_label) RETURNING id;


-- The resource-label junctions deliberately have no foreign keys. Keeping
-- their cleanup in the same application transaction as the owner deletion
-- avoids database-level cascades with unreviewed locking and audit behavior.

-- name: DeleteTaskLabelAssignmentsByLabel :exec
DELETE FROM task_to_label WHERE label_id = $1;

-- name: DeleteAgentLabelAssignmentsByLabel :exec
DELETE FROM agent_to_label WHERE label_id = $1;

-- name: DeleteSkillLabelAssignmentsByLabel :exec
DELETE FROM skill_to_label WHERE label_id = $1;

-- name: DeleteAgentLabelAssignmentsByAgent :exec
DELETE FROM agent_to_label WHERE agent_to_label.agent_id = $1;

-- name: DeleteSkillLabelAssignmentsBySkill :exec
DELETE FROM skill_to_label WHERE skill_to_label.skill_id = $1;

-- The single-entity cleanups above cover one agent/skill at a time. The runtime
-- variant below covers runtime and runtime-profile bulk hard deletes, where the
-- owning agents disappear without passing through a per-entity delete.
-- Workspace-wide cleanup lives in DeleteWorkspace so it is atomic with that
-- workspace's existing multi-table teardown.

-- name: DeleteAgentLabelAssignmentsByRuntime :exec
-- Runtime teardown hard-deletes every agent bound to the runtime (archived and
-- system; active agents are refused by a 409 guard). Clear their label links by
-- runtime so none survive the agent hard-delete.
DELETE FROM agent_to_label
WHERE agent_id IN (SELECT id FROM agent WHERE agent.runtime_id = $1);

-- name: AttachLabelToTask :exec
-- Workspace-guarded INSERT: the WHERE EXISTS clauses ensure both the task
-- and the label belong to the given workspace. A future caller that forgets
-- handler-level prechecks still cannot attach labels across workspaces.
INSERT INTO task_to_label (task_id, label_id)
SELECT sqlc.arg('task_id')::uuid, sqlc.arg('label_id')::uuid
WHERE EXISTS (
    SELECT 1 FROM task i
    WHERE i.id = sqlc.arg('task_id')::uuid
      AND i.workspace_id = sqlc.arg('workspace_id')::uuid
    FOR KEY SHARE
)
AND EXISTS (
    SELECT 1 FROM task_label l
    WHERE l.id = sqlc.arg('label_id')::uuid
      AND l.workspace_id = sqlc.arg('workspace_id')::uuid
      AND l.resource_type = 'task'
    FOR KEY SHARE
)
ON CONFLICT DO NOTHING;

-- name: DetachLabelFromTask :exec
-- Workspace-guarded DELETE: only deletes if the task is in the given
-- workspace. Mirror of the attach query.
DELETE FROM task_to_label
WHERE task_id = sqlc.arg('task_id')::uuid
  AND label_id = sqlc.arg('label_id')::uuid
  AND EXISTS (
      SELECT 1 FROM task i
      WHERE i.id = sqlc.arg('task_id')::uuid
        AND i.workspace_id = sqlc.arg('workspace_id')::uuid
  );

-- name: ListLabelsByTask :many
-- Workspace filter at the SQL layer (mirrors GetProjectInWorkspace). Any caller
-- that passes the wrong workspace gets an empty list rather than leaking labels.
SELECT l.*
FROM task_label l
JOIN task_to_label il ON il.label_id = l.id
WHERE il.task_id = sqlc.arg('task_id')::uuid
  AND l.workspace_id = sqlc.arg('workspace_id')::uuid
  AND l.resource_type = 'task'
ORDER BY LOWER(l.name) ASC;

-- name: ListLabelsForTasks :many
-- Bulk variant: fetch labels for many tasks in one round-trip so the task
-- list endpoints can fold labels into each row without N+1 queries from the
-- client. Workspace-guarded the same way as ListLabelsByTask.
SELECT il.task_id, l.*
FROM task_label l
JOIN task_to_label il ON il.label_id = l.id
WHERE il.task_id = ANY(sqlc.arg('task_ids')::uuid[])
  AND l.workspace_id = sqlc.arg('workspace_id')::uuid
  AND l.resource_type = 'task'
ORDER BY il.task_id, LOWER(l.name) ASC;

-- name: ListLabelsByAgent :many
SELECT l.*
FROM task_label l
JOIN agent_to_label atl ON atl.label_id = l.id
WHERE atl.agent_id = sqlc.arg('agent_id')::uuid
  AND l.workspace_id = sqlc.arg('workspace_id')::uuid
  AND l.resource_type = 'agent'
ORDER BY LOWER(l.name) ASC;

-- name: ListLabelsForAgents :many
SELECT atl.agent_id, l.*
FROM task_label l
JOIN agent_to_label atl ON atl.label_id = l.id
WHERE atl.agent_id = ANY(sqlc.arg('agent_ids')::uuid[])
  AND l.workspace_id = sqlc.arg('workspace_id')::uuid
  AND l.resource_type = 'agent'
ORDER BY atl.agent_id, LOWER(l.name) ASC;

-- name: AttachLabelToAgent :exec
INSERT INTO agent_to_label (agent_id, label_id)
SELECT sqlc.arg('agent_id')::uuid, sqlc.arg('label_id')::uuid
WHERE EXISTS (
    SELECT 1 FROM agent a
    WHERE a.id = sqlc.arg('agent_id')::uuid
      AND a.workspace_id = sqlc.arg('workspace_id')::uuid
)
AND EXISTS (
    SELECT 1 FROM task_label l
    WHERE l.id = sqlc.arg('label_id')::uuid
      AND l.workspace_id = sqlc.arg('workspace_id')::uuid
      AND l.resource_type = 'agent'
)
ON CONFLICT DO NOTHING;

-- name: DetachLabelFromAgent :exec
DELETE FROM agent_to_label
WHERE agent_id = sqlc.arg('agent_id')::uuid
  AND label_id = sqlc.arg('label_id')::uuid
  AND EXISTS (
      SELECT 1 FROM agent a
      WHERE a.id = sqlc.arg('agent_id')::uuid
        AND a.workspace_id = sqlc.arg('workspace_id')::uuid
  );

-- name: ListLabelsBySkill :many
SELECT l.*
FROM task_label l
JOIN skill_to_label stl ON stl.label_id = l.id
WHERE stl.skill_id = sqlc.arg('skill_id')::uuid
  AND l.workspace_id = sqlc.arg('workspace_id')::uuid
  AND l.resource_type = 'skill'
ORDER BY LOWER(l.name) ASC;

-- name: ListLabelsForSkills :many
SELECT stl.skill_id, l.*
FROM task_label l
JOIN skill_to_label stl ON stl.label_id = l.id
WHERE stl.skill_id = ANY(sqlc.arg('skill_ids')::uuid[])
  AND l.workspace_id = sqlc.arg('workspace_id')::uuid
  AND l.resource_type = 'skill'
ORDER BY stl.skill_id, LOWER(l.name) ASC;

-- name: AttachLabelToSkill :exec
INSERT INTO skill_to_label (skill_id, label_id)
SELECT sqlc.arg('skill_id')::uuid, sqlc.arg('label_id')::uuid
WHERE EXISTS (
    SELECT 1 FROM skill s
    WHERE s.id = sqlc.arg('skill_id')::uuid
      AND s.workspace_id = sqlc.arg('workspace_id')::uuid
)
AND EXISTS (
    SELECT 1 FROM task_label l
    WHERE l.id = sqlc.arg('label_id')::uuid
      AND l.workspace_id = sqlc.arg('workspace_id')::uuid
      AND l.resource_type = 'skill'
)
ON CONFLICT DO NOTHING;

-- name: DetachLabelFromSkill :exec
DELETE FROM skill_to_label
WHERE skill_id = sqlc.arg('skill_id')::uuid
  AND label_id = sqlc.arg('label_id')::uuid
  AND EXISTS (
      SELECT 1 FROM skill s
      WHERE s.id = sqlc.arg('skill_id')::uuid
        AND s.workspace_id = sqlc.arg('workspace_id')::uuid
  );
