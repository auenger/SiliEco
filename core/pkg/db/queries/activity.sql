-- name: ListActivitiesForTask :many
-- All activities for an task in chronological order, capped at $2 (DB safety
-- net to bound the response).
SELECT * FROM activity_log
WHERE task_id = $1
ORDER BY created_at ASC, id ASC
LIMIT $2;

-- name: GetActivity :one
SELECT * FROM activity_log
WHERE id = $1;

-- name: CreateActivity :one
INSERT INTO activity_log (
    workspace_id, task_id, actor_type, actor_id, action, details
) SELECT
    $1 AS workspace_id,
    $2 AS task_id,
    $3 AS actor_type,
    $4 AS actor_id,
    $5 AS action,
    $6 AS details
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM workspace AS ref_workspace_id WHERE ref_workspace_id.id = ($1) FOR KEY SHARE))
  AND (($2)::uuid IS NULL OR EXISTS (SELECT 1 FROM task AS ref_task_id WHERE ref_task_id.id = ($2) FOR KEY SHARE))

RETURNING *;

-- name: HasSquadLeaderNoActionEvaluationForRun :one
SELECT EXISTS (
  SELECT 1
  FROM activity_log
  WHERE task_id = @task_id
    AND actor_type = 'agent'
    AND actor_id = @agent_id
    AND action = 'squad_leader_evaluated'
    AND details->>'outcome' = 'no_action'
    AND details->>'run_id' = @run_id::text
) AS exists;

-- name: CountAssigneeChangesByActor :many
-- Count how many times a user assigned each target via assignee_changed activities.
SELECT
  details->>'to_type' as assignee_type,
  details->>'to_id' as assignee_id,
  COUNT(*)::bigint as frequency
FROM activity_log
WHERE workspace_id = $1
  AND actor_id = $2
  AND actor_type = 'member'
  AND action = 'assignee_changed'
  AND details->>'to_type' IS NOT NULL
  AND details->>'to_id' IS NOT NULL
GROUP BY details->>'to_type', details->>'to_id';
