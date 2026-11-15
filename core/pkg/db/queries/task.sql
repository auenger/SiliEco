-- name: ListTasks :many
-- involves_user_id widens the assignee filter to surface tasks where the user
-- is *indirectly* the assignee — via an owned agent or a squad they belong to /
-- lead / have an agent inside. The semantics intentionally exclude direct
-- member assignment (`assignee_type='member' AND assignee_id=involves_user_id`)
-- because that is already the meaning of the `assignee_id` filter (tab 1
-- "Assigned to me"), and the two filters must produce disjoint result sets.
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_task_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at, i.number, i.project_id, i.metadata, i.stage, i.properties,
       i.workflow_instance_id, i.workflow_stage_id, i.workflow_invalidated_by_rework_id
FROM task i
WHERE i.workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR i.status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR i.priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR i.assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR i.assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR i.creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('scheduled')::bool IS NULL OR (i.start_date IS NOT NULL OR i.due_date IS NOT NULL))
  AND (sqlc.narg('metadata_filter')::jsonb IS NULL OR i.metadata @> sqlc.narg('metadata_filter')::jsonb)
  AND (
    sqlc.narg('involves_user_id')::uuid IS NULL
    -- (1) assignee is an agent owned by the user
    OR (i.assignee_type = 'agent' AND i.assignee_id IN (
          SELECT a.id FROM agent a
           WHERE a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
    -- (2)(3)(4) assignee is a squad related to the user — three relations
    OR (i.assignee_type = 'squad' AND i.assignee_id IN (
          -- (2) the user is a human member of the squad
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'member'
             AND sm.member_id   = sqlc.narg('involves_user_id')::uuid
          UNION
          -- (3) the squad's canonical leader is an agent owned by the user.
          -- We read squad.leader_id directly rather than relying on a
          -- squad_member row, because the leader copy in squad_member is
          -- best-effort (see squad.go AddSquadMember error handling).
          SELECT s.id
            FROM squad s
            JOIN agent a ON a.id = s.leader_id
           WHERE s.workspace_id = $1
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
          UNION
          -- (4) the squad has an agent member owned by the user
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
            JOIN agent a ON a.id = sm.member_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'agent'
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
  )
ORDER BY i.position ASC, i.created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetTask :one
SELECT * FROM task
WHERE id = $1;

-- name: GetTaskGCStatus :one
SELECT workspace_id, status, updated_at
FROM task
WHERE id = $1;

-- name: ListTaskGCStatuses :many
SELECT id, status, updated_at
FROM task
WHERE workspace_id = sqlc.arg('workspace_id')
  AND id = ANY(sqlc.arg('task_ids')::uuid[]);

-- name: GetTaskInWorkspace :one
SELECT * FROM task
WHERE id = $1 AND workspace_id = $2;

-- name: CreateTask :one
INSERT INTO task (
    workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_task_id, position, start_date, due_date, number, project_id,
    stage, workflow_instance_id, workflow_stage_id, workflow_stage_execution_id
) SELECT
    $1 AS workspace_id,
    $2 AS title,
    $3 AS description,
    $4 AS status,
    $5 AS priority,
    $6 AS assignee_type,
    $7 AS assignee_id,
    $8 AS creator_type,
    $9 AS creator_id,
    $10 AS parent_task_id,
    $11 AS position,
    $12 AS start_date,
    $13 AS due_date,
    $14 AS number,
    $15 AS project_id,
    sqlc.narg('stage') AS stage,
    sqlc.narg('workflow_instance_id') AS workflow_instance_id,
    sqlc.narg('workflow_stage_id') AS workflow_stage_id,
    (SELECT e.id FROM workflow_stage_execution e WHERE e.workspace_id=$1 AND e.workflow_instance_id=sqlc.narg('workflow_instance_id') AND e.workflow_stage_id=sqlc.narg('workflow_stage_id') ORDER BY e.attempt DESC LIMIT 1) AS workflow_stage_execution_id
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM workspace AS ref_workspace_id WHERE ref_workspace_id.id = ($1) FOR KEY SHARE))
  AND (($10)::uuid IS NULL OR EXISTS (SELECT 1 FROM task AS ref_parent_task_id WHERE ref_parent_task_id.id = ($10) FOR KEY SHARE))
  AND (($15)::uuid IS NULL OR EXISTS (SELECT 1 FROM project AS ref_project_id WHERE ref_project_id.id = ($15) FOR KEY SHARE))
 RETURNING *;

-- name: GetTaskByNumber :one
SELECT * FROM task
WHERE workspace_id = $1 AND number = $2;

-- name: UpdateTask :one
UPDATE task SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    status = COALESCE(sqlc.narg('status'), status),
    priority = COALESCE(sqlc.narg('priority'), priority),
    assignee_type = sqlc.narg('assignee_type'),
    assignee_id = sqlc.narg('assignee_id'),
    position = COALESCE(sqlc.narg('position'), position),
    start_date = sqlc.narg('start_date'),
    due_date = sqlc.narg('due_date'),
    parent_task_id = sqlc.narg('parent_task_id'),
    project_id = sqlc.narg('project_id'),
    stage = sqlc.narg('stage'),
    workflow_instance_id = sqlc.narg('workflow_instance_id'),
    workflow_stage_id = sqlc.narg('workflow_stage_id'),
    workflow_stage_execution_id = CASE WHEN workflow_instance_id IS NOT DISTINCT FROM sqlc.narg('workflow_instance_id') AND workflow_stage_id IS NOT DISTINCT FROM sqlc.narg('workflow_stage_id') THEN workflow_stage_execution_id ELSE (SELECT e.id FROM workflow_stage_execution e WHERE e.workspace_id=task.workspace_id AND e.workflow_instance_id=sqlc.narg('workflow_instance_id') AND e.workflow_stage_id=sqlc.narg('workflow_stage_id') ORDER BY e.attempt DESC LIMIT 1) END,
    updated_at = now()
WHERE task.id = $1
  AND (sqlc.narg('expected_revision')::timestamptz IS NULL OR task.updated_at = sqlc.narg('expected_revision'))

  AND ((sqlc.narg('parent_task_id'))::uuid IS NULL OR EXISTS (SELECT 1 FROM task AS ref_parent_task_id WHERE ref_parent_task_id.id = (sqlc.narg('parent_task_id')) FOR KEY SHARE))
  AND ((sqlc.narg('project_id'))::uuid IS NULL OR EXISTS (SELECT 1 FROM project AS ref_project_id WHERE ref_project_id.id = (sqlc.narg('project_id')) FOR KEY SHARE))
RETURNING task.*;

-- name: UpdateTaskStatus :one
-- Workspace_id in the WHERE clause is a SQL-layer tenant guard; see DeleteTask.
UPDATE task SET
    status = $2,
    updated_at = now()
WHERE id = $1 AND workspace_id = $3
RETURNING *;

-- name: CreateTaskWithOrigin :one
INSERT INTO task (
    workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_task_id, position, start_date, due_date, number, project_id,
    origin_type, origin_id, stage, workflow_instance_id, workflow_stage_id, workflow_stage_execution_id
) SELECT
    $1 AS workspace_id,
    $2 AS title,
    $3 AS description,
    $4 AS status,
    $5 AS priority,
    $6 AS assignee_type,
    $7 AS assignee_id,
    $8 AS creator_type,
    $9 AS creator_id,
    $10 AS parent_task_id,
    $11 AS position,
    $12 AS start_date,
    $13 AS due_date,
    $14 AS number,
    $15 AS project_id,
    sqlc.narg('origin_type') AS origin_type,
    sqlc.narg('origin_id') AS origin_id,
    sqlc.narg('stage') AS stage,
    sqlc.narg('workflow_instance_id') AS workflow_instance_id,
    sqlc.narg('workflow_stage_id') AS workflow_stage_id,
    (SELECT e.id FROM workflow_stage_execution e WHERE e.workspace_id=$1 AND e.workflow_instance_id=sqlc.narg('workflow_instance_id') AND e.workflow_stage_id=sqlc.narg('workflow_stage_id') ORDER BY e.attempt DESC LIMIT 1) AS workflow_stage_execution_id
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM workspace AS ref_workspace_id WHERE ref_workspace_id.id = ($1) FOR KEY SHARE))
  AND (($10)::uuid IS NULL OR EXISTS (SELECT 1 FROM task AS ref_parent_task_id WHERE ref_parent_task_id.id = ($10) FOR KEY SHARE))
  AND (($15)::uuid IS NULL OR EXISTS (SELECT 1 FROM project AS ref_project_id WHERE ref_project_id.id = ($15) FOR KEY SHARE))
 RETURNING *;

-- name: LockTaskDuplicateKey :exec
SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0));

-- name: FindActiveDuplicateTask :one
SELECT * FROM task
WHERE workspace_id = $1
  AND status NOT IN ('done', 'cancelled')
  AND project_id IS NOT DISTINCT FROM sqlc.arg('project_id')::uuid
  AND parent_task_id IS NOT DISTINCT FROM sqlc.arg('parent_task_id')::uuid
  AND lower(btrim(regexp_replace(title, '[[:space:]]+', ' ', 'g'))) = sqlc.arg('normalized_title')
ORDER BY created_at ASC
LIMIT 1;

-- name: FindRecentAutopilotDuplicateTask :one
SELECT i.* FROM task i
WHERE i.workspace_id = $1
  AND i.status NOT IN ('done', 'cancelled')
  AND i.origin_type = 'autopilot'
  AND i.origin_id = $2
  AND i.project_id IS NOT DISTINCT FROM sqlc.arg('project_id')::uuid
  AND lower(btrim(regexp_replace(i.title, '[[:space:]]+', ' ', 'g'))) = sqlc.arg('normalized_title')
  AND i.created_at >= sqlc.arg('created_after')::timestamptz
  AND EXISTS (
    SELECT 1
    FROM autopilot_run r
    WHERE r.task_id = i.id
      AND r.autopilot_id = i.origin_id
      AND r.status IN ('task_created', 'running', 'completed')
  )
ORDER BY i.created_at ASC
LIMIT 1;

-- name: DeleteTask :exec
-- Resolve the tenant-scoped target once, then remove its dependent records and
-- detach surviving children in the same statement. Shared PR mirrors survive;
-- only this task's links are removed. A foreign workspace ID matches no target.
WITH RECURSIVE target AS (
    SELECT task.id FROM task WHERE task.id = $1 AND task.workspace_id = $2
),
cleared_vcs_pr_links AS (
    DELETE FROM task_vcs_pull_request WHERE task_id IN (SELECT target.id FROM target)
),
cleanup_target_activity_log AS MATERIALIZED (
    SELECT child.* FROM "activity_log" child WHERE EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id")
),
cleanup_target_agent_run AS MATERIALIZED (
    SELECT child.* FROM "agent_run" child WHERE EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id")
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
cleanup_target_attachment AS MATERIALIZED (
    SELECT child.* FROM "attachment" child WHERE EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_comment parent WHERE child."comment_id" = parent."id")
),
cleanup_target_comment AS MATERIALIZED (
    SELECT child.* FROM "comment" child WHERE EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id")
    UNION
    SELECT child.* FROM "comment" child JOIN cleanup_target_comment parent ON child."parent_id" = parent."id"
),
cleanup_target_comment_reaction AS MATERIALIZED (
    SELECT child.* FROM "comment_reaction" child WHERE EXISTS (SELECT 1 FROM cleanup_target_comment parent WHERE child."comment_id" = parent."id")
),
cleanup_target_inbox_item AS MATERIALIZED (
    SELECT child.* FROM "inbox_item" child WHERE EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id")
),
cleanup_target_task AS MATERIALIZED (
    SELECT task.* FROM "task" task WHERE task.id IN (SELECT target.id FROM target)
),
cleanup_target_task_dependency AS MATERIALIZED (
    SELECT child.* FROM "task_dependency" child WHERE EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."depends_on_task_id" = parent."id")
),
cleanup_target_task_pull_request AS MATERIALIZED (
    SELECT child.* FROM "task_pull_request" child WHERE EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id")
),
cleanup_target_task_reaction AS MATERIALIZED (
    SELECT child.* FROM "task_reaction" child WHERE EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id")
),
cleanup_target_task_subscriber AS MATERIALIZED (
    SELECT child.* FROM "task_subscriber" child WHERE EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id")
),
cleanup_target_task_to_label AS MATERIALIZED (
    SELECT child.* FROM "task_to_label" child WHERE EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id")
),
cleanup_delete_activity_log AS (
    DELETE FROM "activity_log" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_activity_log)
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
cleanup_delete_attachment AS (
    DELETE FROM "attachment" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_attachment)
),
cleanup_delete_comment AS (
    DELETE FROM "comment" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_comment)
),
cleanup_delete_comment_reaction AS (
    DELETE FROM "comment_reaction" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_comment_reaction)
),
cleanup_delete_inbox_item AS (
    DELETE FROM "inbox_item" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_inbox_item)
),
cleanup_delete_task_dependency AS (
    DELETE FROM "task_dependency" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_task_dependency)
),
cleanup_delete_task_pull_request AS (
    DELETE FROM "task_pull_request" child WHERE (child."task_id", child."pull_request_id") IN (SELECT "task_id", "pull_request_id" FROM cleanup_target_task_pull_request)
),
cleanup_delete_task_reaction AS (
    DELETE FROM "task_reaction" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_task_reaction)
),
cleanup_delete_task_subscriber AS (
    DELETE FROM "task_subscriber" child WHERE (child."task_id", child."user_type", child."user_id") IN (SELECT "task_id", "user_type", "user_id" FROM cleanup_target_task_subscriber)
),
cleanup_delete_task_to_label AS (
    DELETE FROM "task_to_label" child WHERE (child."task_id", child."label_id") IN (SELECT "task_id", "label_id" FROM cleanup_target_task_to_label)
),
cleanup_detach_agent_run AS (
    UPDATE "agent_run" child SET "trigger_comment_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_comment parent WHERE child."trigger_comment_id" = parent."id") THEN NULL ELSE child."trigger_comment_id" END,
        "parent_run_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."parent_run_id" = parent."id") THEN NULL ELSE child."parent_run_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_comment parent WHERE child."trigger_comment_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."parent_run_id" = parent."id")) AND NOT ((child."id") IN (SELECT "id" FROM cleanup_target_agent_run))
),
cleanup_detach_autopilot_run AS (
    UPDATE "autopilot_run" child SET "run_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id") THEN NULL ELSE child."run_id" END,
        "task_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id") THEN NULL ELSE child."task_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id"))
),
cleanup_detach_lark_outbound_card_message AS (
    UPDATE "lark_outbound_card_message" child SET "run_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id") THEN NULL ELSE child."run_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id"))
),
cleanup_detach_task AS (
    UPDATE "task" child SET "parent_task_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."parent_task_id" = parent."id") THEN NULL ELSE child."parent_task_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."parent_task_id" = parent."id")) AND NOT ((child."id") IN (SELECT "id" FROM cleanup_target_task))
)
DELETE FROM "task" task WHERE (task."id") IN (SELECT "id" FROM cleanup_target_task) ;


-- name: ListOpenTasks :many
-- See ListTasks for the semantics of involves_user_id (mirrors the 4-branch
-- filter; member-direct assignment is intentionally excluded).
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_task_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at, i.number, i.project_id, i.metadata, i.stage, i.properties,
       i.workflow_instance_id, i.workflow_stage_id, i.workflow_invalidated_by_rework_id
FROM task i
WHERE i.workspace_id = $1
  AND i.status NOT IN ('done', 'cancelled')
  AND (sqlc.narg('priority')::text IS NULL OR i.priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR i.assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR i.assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR i.creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('metadata_filter')::jsonb IS NULL OR i.metadata @> sqlc.narg('metadata_filter')::jsonb)
  -- properties_filter is a jsonb array of groups, each group an array of
  -- containment patterns (built by parsePropertiesFilterParam): the task
  -- must match at least one pattern from EVERY group (AND of ORs). The
  -- correlated form skips the GIN index, which is fine here: open_only is
  -- an unpaginated workspace scan already narrowed by status.
  AND (
    sqlc.narg('properties_filter')::jsonb IS NULL
    OR NOT EXISTS (
      SELECT 1
      FROM jsonb_array_elements(sqlc.narg('properties_filter')::jsonb) AS pf(alternatives)
      WHERE NOT EXISTS (
        SELECT 1
        FROM jsonb_array_elements(pf.alternatives) AS alt(pattern)
        WHERE i.properties @> alt.pattern
      )
    )
  )
  AND (
    sqlc.narg('involves_user_id')::uuid IS NULL
    OR (i.assignee_type = 'agent' AND i.assignee_id IN (
          SELECT a.id FROM agent a
           WHERE a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
    OR (i.assignee_type = 'squad' AND i.assignee_id IN (
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'member'
             AND sm.member_id   = sqlc.narg('involves_user_id')::uuid
          UNION
          SELECT s.id
            FROM squad s
            JOIN agent a ON a.id = s.leader_id
           WHERE s.workspace_id = $1
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
          UNION
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
            JOIN agent a ON a.id = sm.member_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'agent'
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
  )
ORDER BY i.position ASC, i.created_at DESC;

-- name: CountTasks :one
-- See ListTasks for the semantics of involves_user_id.
SELECT count(*) FROM task i
WHERE i.workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR i.status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR i.priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR i.assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR i.assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR i.creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('scheduled')::bool IS NULL OR (i.start_date IS NOT NULL OR i.due_date IS NOT NULL))
  AND (sqlc.narg('metadata_filter')::jsonb IS NULL OR i.metadata @> sqlc.narg('metadata_filter')::jsonb)
  AND (
    sqlc.narg('involves_user_id')::uuid IS NULL
    OR (i.assignee_type = 'agent' AND i.assignee_id IN (
          SELECT a.id FROM agent a
           WHERE a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
    OR (i.assignee_type = 'squad' AND i.assignee_id IN (
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'member'
             AND sm.member_id   = sqlc.narg('involves_user_id')::uuid
          UNION
          SELECT s.id
            FROM squad s
            JOIN agent a ON a.id = s.leader_id
           WHERE s.workspace_id = $1
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
          UNION
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
            JOIN agent a ON a.id = sm.member_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'agent'
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
  );

-- name: ListChildTasks :many
-- Order by number ASC so sub-tasks display in stable creation order
-- (oldest first), matching how a parent's plan reads top-to-bottom. The
-- position column is computed per-(workspace, status) by NextTopPosition,
-- not relative to siblings, so ordering by it interleaves children
-- unpredictably across batches and statuses; number is a per-workspace
-- monotonic counter and is sibling-stable.
SELECT * FROM task
WHERE parent_task_id = $1
ORDER BY number ASC;

-- name: ListChildrenByParents :many
-- Batched variant of ListChildTasks: returns all children for the given
-- parent set in one round trip. Used by Swimlane to avoid an N+1 fan-out
-- (one request per visible parent lane). Result is grouped client-side by
-- parent_task_id; the workspace filter is also enforced so callers can't
-- enumerate children of parents in workspaces they don't belong to.
-- Within each parent, order by number ASC for the same sibling-stable
-- creation order as ListChildTasks.
SELECT * FROM task
WHERE workspace_id = sqlc.arg('workspace_id')
  AND parent_task_id = ANY(sqlc.arg('parent_ids')::uuid[])
ORDER BY parent_task_id, number ASC;

-- name: GetTaskByOrigin :one
-- Finds the task stamped with a specific (origin_type, origin_id) pair.
-- Used by quick-create completion to deterministically locate the task
-- produced by a given agent_run.id — robust against concurrent
-- task creates by the same agent (assignment run + quick-create both
-- running with max_concurrent_runs > 1).
SELECT * FROM task
WHERE workspace_id = $1
  AND origin_type = $2
  AND origin_id = $3
LIMIT 1;

-- name: CountCreatedTaskAssignees :many
-- Count assignees on tasks created by a specific user.
SELECT
  assignee_type,
  assignee_id,
  COUNT(*)::bigint as frequency
FROM task
WHERE workspace_id = $1
  AND creator_id = $2
  AND creator_type = 'member'
  AND assignee_type IS NOT NULL
  AND assignee_id IS NOT NULL
GROUP BY assignee_type, assignee_id;

-- name: ChildTaskProgress :many
SELECT parent_task_id,
       COUNT(*)::bigint AS total,
       COUNT(*) FILTER (WHERE status IN ('done', 'cancelled'))::bigint AS done
FROM task
WHERE workspace_id = $1
  AND parent_task_id IS NOT NULL
GROUP BY parent_task_id;

-- SearchTasks: moved to handler (dynamic SQL for multi-word search support).

-- name: SetTaskMetadataKey :one
-- Atomically sets a single key in the task's metadata JSONB. The
-- workspace_id filter is the authorization gate — handler resolves the
-- task first so this is also the tenant check.
UPDATE task SET
    metadata = jsonb_set(metadata, ARRAY[sqlc.arg('key')::text], sqlc.arg('value')::jsonb),
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: DeleteTaskMetadataKey :one
-- Atomically removes a single key from the task's metadata JSONB.
-- Deleting a missing key is a no-op (still returns the row).
UPDATE task SET
    metadata = metadata - sqlc.arg('key')::text,
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: MarkTaskFirstExecuted :one
-- Flips first_executed_at from NULL to now() atomically. Returns the row if
-- this was the first time the task was executed; no rows otherwise. The
-- analytics task_executed event fires exactly when this returns a row —
-- retries and re-assignments hit the WHERE clause and no-op.
UPDATE task
SET first_executed_at = now()
WHERE id = $1 AND first_executed_at IS NULL
RETURNING id, workspace_id, creator_type, creator_id, first_executed_at;
