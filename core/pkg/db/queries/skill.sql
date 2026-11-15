-- Skill CRUD

-- name: ListSkillsByWorkspace :many
SELECT * FROM skill
WHERE workspace_id = $1
ORDER BY name ASC;

-- name: ListSkillSummariesByWorkspace :many
-- Same as ListSkillsByWorkspace but omits the SKILL.md `content` column. Used
-- by list endpoints (CLI table, web list page) where the body is never read;
-- shipping it everywhere blew up payload size on workspaces with many skills
-- and caused 15s CLI timeouts from high-latency regions (GH silieco-ai/silieco#2174).
SELECT id, workspace_id, name, description, config, created_by, created_at, updated_at
FROM skill
WHERE workspace_id = $1
ORDER BY name ASC;

-- name: GetSkill :one
SELECT * FROM skill
WHERE id = $1;

-- name: GetSkillInWorkspace :one
SELECT * FROM skill
WHERE id = $1 AND workspace_id = $2;

-- name: GetSkillByWorkspaceAndName :one
-- Used by agent-template materialization to implement find-or-create: when a
-- template references a skill by name that already exists in the workspace,
-- reuse the existing skill_id rather than INSERT (which would fail the
-- UNIQUE(workspace_id, name) constraint from migration 008).
SELECT * FROM skill
WHERE workspace_id = $1 AND name = $2;

-- name: CreateSkill :one
INSERT INTO skill (workspace_id, name, description, content, config, created_by) SELECT
    $1 AS workspace_id,
    $2 AS name,
    $3 AS description,
    $4 AS content,
    $5 AS config,
    $6 AS created_by
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM workspace AS ref_workspace_id WHERE ref_workspace_id.id = ($1) FOR KEY SHARE))
  AND (($6)::uuid IS NULL OR EXISTS (SELECT 1 FROM "user" AS ref_created_by WHERE ref_created_by.id = ($6) FOR KEY SHARE))

RETURNING *;

-- name: UpdateSkill :one
UPDATE skill SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    content = COALESCE(sqlc.narg('content'), content),
    config = COALESCE(sqlc.narg('config'), config),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteSkill :exec
-- Defense-in-depth: workspace_id is a SQL-layer tenant guard. See DeleteTask.
WITH RECURSIVE cleanup_target_agent_skill AS MATERIALIZED (
    SELECT child.* FROM "agent_skill" child WHERE EXISTS (SELECT 1 FROM cleanup_target_skill parent WHERE child."skill_id" = parent."id")
),
cleanup_target_skill AS MATERIALIZED (
    SELECT skill.* FROM "skill" skill WHERE skill.id = $1 AND skill.workspace_id = $2
),
cleanup_target_skill_file AS MATERIALIZED (
    SELECT child.* FROM "skill_file" child WHERE EXISTS (SELECT 1 FROM cleanup_target_skill parent WHERE child."skill_id" = parent."id")
),
cleanup_delete_agent_skill AS (
    DELETE FROM "agent_skill" child WHERE (child."agent_id", child."skill_id") IN (SELECT "agent_id", "skill_id" FROM cleanup_target_agent_skill)
),
cleanup_delete_skill_file AS (
    DELETE FROM "skill_file" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_skill_file)
)
DELETE FROM "skill" skill WHERE (skill."id") IN (SELECT "id" FROM cleanup_target_skill) ;


-- Skill File CRUD

-- name: ListSkillFiles :many
SELECT * FROM skill_file
WHERE skill_id = $1
ORDER BY path ASC;

-- name: GetSkillFile :one
SELECT * FROM skill_file
WHERE id = $1;

-- name: UpsertSkillFile :one
INSERT INTO skill_file (skill_id, path, content) SELECT
    $1 AS skill_id,
    $2 AS path,
    $3 AS content
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM skill AS ref_skill_id WHERE ref_skill_id.id = ($1) FOR KEY SHARE))

ON CONFLICT (skill_id, path) DO UPDATE SET
    content = EXCLUDED.content,
    updated_at = now()
RETURNING *;

-- name: DeleteSkillFile :exec
DELETE FROM skill_file WHERE skill_file.id = $1;

-- name: DeleteSkillFilesBySkill :exec
DELETE FROM skill_file WHERE skill_file.skill_id = $1;

-- Agent-Skill junction

-- name: ListAgentSkills :many
SELECT s.* FROM skill s
JOIN agent_skill ask ON ask.skill_id = s.id
WHERE ask.agent_id = $1 AND ask.enabled = TRUE
ORDER BY s.name ASC;

-- name: ListAgentSkillSummaries :many
-- Summary variant for the agent skills list endpoint — omits `content` for
-- the same reason as ListSkillSummariesByWorkspace.
SELECT s.id, s.workspace_id, s.name, s.description, s.config, s.created_by, s.created_at, s.updated_at, ask.enabled
FROM skill s
JOIN agent_skill ask ON ask.skill_id = s.id
WHERE ask.agent_id = $1
ORDER BY s.name ASC;

-- name: ListAgentSkillNamesByAgentIDs :many
SELECT ask.agent_id, s.name
FROM agent_skill ask
JOIN skill s ON s.id = ask.skill_id
WHERE ask.agent_id = ANY(sqlc.arg('agent_ids')::uuid[])
  AND ask.enabled = TRUE
ORDER BY ask.agent_id, s.name ASC;

-- name: AddAgentSkill :exec
INSERT INTO agent_skill (agent_id, skill_id) SELECT
    $1 AS agent_id,
    $2 AS skill_id
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM agent AS ref_agent_id WHERE ref_agent_id.id = ($1) FOR KEY SHARE))
  AND (($2)::uuid IS NULL OR EXISTS (SELECT 1 FROM skill AS ref_skill_id WHERE ref_skill_id.id = ($2) FOR KEY SHARE))

ON CONFLICT DO NOTHING;

-- name: SetAgentSkillEnabled :execrows
UPDATE agent_skill
SET enabled = $3
WHERE agent_id = $1 AND skill_id = $2;

-- name: RemoveAgentSkill :exec
DELETE FROM agent_skill
WHERE agent_id = $1 AND skill_id = $2;

-- name: RemoveAllAgentSkills :exec
DELETE FROM agent_skill WHERE agent_skill.agent_id = $1;

-- name: ListAgentSkillsByWorkspace :many
SELECT ask.agent_id, s.id, s.name, s.description, ask.enabled
FROM agent_skill ask
JOIN skill s ON s.id = ask.skill_id
WHERE s.workspace_id = $1
ORDER BY s.name ASC;
