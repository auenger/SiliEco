-- name: CreateWorkflowArtifactUpload :one
INSERT INTO workflow_artifact_upload (id, workspace_id, workflow_instance_id, storage_key)
VALUES ($1,$2,$3,$4) RETURNING *;

-- name: CommitWorkflowArtifactUpload :execrows
UPDATE workflow_artifact_upload SET status = 'committed'
WHERE id=$1 AND workspace_id=$2 AND status='pending' AND expires_at > now();

-- name: AbandonWorkflowArtifactUpload :exec
UPDATE workflow_artifact_upload SET status='deleting', expires_at=now()
WHERE id=$1 AND workspace_id=$2 AND status='pending';

-- name: ListWorkflowArtifactCleanup :many
-- System storage reconciliation also handles tombstones after workspace deletion.
SELECT * FROM workflow_artifact_upload
WHERE status IN ('deleting','pending') AND expires_at <= now()
ORDER BY expires_at LIMIT 100;

-- name: ClaimWorkflowArtifactCleanup :execrows
UPDATE workflow_artifact_upload SET status='deleting', expires_at=now()+INTERVAL '2 minutes'
WHERE id=$1 AND workspace_id=$2 AND status IN ('deleting','pending') AND expires_at <= now();

-- name: DeleteWorkflowArtifactUploadTombstone :exec
DELETE FROM workflow_artifact_upload WHERE id=$1 AND workspace_id=$2 AND status='deleting' AND created_at < now()-INTERVAL '2 days';

-- name: CreateWorkflowExecutionResult :one
INSERT INTO workflow_execution_result (workspace_id,workflow_instance_id,execution_id,revision,data,submitted_by_type,submitted_by_id)
VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING *;

-- name: GetWorkflowExecutionResult :one
SELECT * FROM workflow_execution_result WHERE workspace_id=$1 AND execution_id=$2 AND revision=$3;

-- name: GetOrCreateWorkflowArtifact :one
INSERT INTO workflow_artifact (workspace_id,workflow_instance_id,workflow_stage_id,name)
VALUES ($1,$2,$3,$4)
ON CONFLICT (workspace_id,workflow_instance_id,workflow_stage_id,name) DO UPDATE SET name=EXCLUDED.name
RETURNING *;

-- name: NextWorkflowArtifactRevision :one
-- Caller holds the Run lock for revision allocation.
SELECT COALESCE(max(revision),0)::integer + 1 AS revision FROM workflow_artifact_revision
WHERE workspace_id=$1 AND artifact_id=$2;

-- name: CreateWorkflowArtifactRevision :one
INSERT INTO workflow_artifact_revision (workspace_id,workflow_instance_id,artifact_id,execution_id,result_id,upload_id,revision,source_attachment_id,kind,media_type,schema_version,checksum,size_bytes)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING *;

-- name: ListWorkflowResultArtifacts :many
SELECT revision.*, artifact.name, upload.storage_key
FROM workflow_artifact_revision AS revision
JOIN workflow_artifact AS artifact ON artifact.id=revision.artifact_id AND artifact.workspace_id=revision.workspace_id
JOIN workflow_artifact_upload AS upload ON upload.id=revision.upload_id AND upload.workspace_id=revision.workspace_id AND upload.status='committed'
WHERE revision.workspace_id=$1 AND revision.result_id=$2 ORDER BY artifact.name;

-- name: SubmitWorkflowExecutionResult :one
UPDATE workflow_stage_execution SET result_revision=$3, status='awaiting_review', updated_at=now()
WHERE workspace_id=$1 AND id=$2 AND result_revision=$4 AND status IN ('queued','running','awaiting_review')
RETURNING *;

-- name: GetWorkflowExecutionAttachment :one
SELECT attachment.* FROM attachment
WHERE attachment.id=sqlc.arg(attachment_id) AND attachment.workspace_id=sqlc.arg(workspace_id)
AND (attachment.issue_id=sqlc.arg(issue_id) OR attachment.task_id=sqlc.arg(task_id)
 OR EXISTS (SELECT 1 FROM comment WHERE comment.id=attachment.comment_id
 AND comment.workspace_id=attachment.workspace_id AND comment.issue_id=sqlc.arg(issue_id)));

-- name: DeleteProjectWorkflowResults :exec
WITH runs AS (
 SELECT id FROM workflow_instance WHERE workflow_instance.workspace_id=$1 AND project_id=$2
), uploads AS (
 UPDATE workflow_artifact_upload SET status='deleting', expires_at=now()
 WHERE workflow_artifact_upload.workspace_id=$1 AND workflow_instance_id IN (SELECT id FROM runs)
), revisions AS (
 DELETE FROM workflow_artifact_revision WHERE workflow_artifact_revision.workspace_id=$1 AND workflow_instance_id IN (SELECT id FROM runs)
), artifacts AS (
 DELETE FROM workflow_artifact WHERE workflow_artifact.workspace_id=$1 AND workflow_instance_id IN (SELECT id FROM runs)
)
DELETE FROM workflow_execution_result WHERE workflow_execution_result.workspace_id=$1 AND workflow_instance_id IN (SELECT id FROM runs);

-- name: RescheduleWorkflowArtifactCleanup :exec
UPDATE workflow_artifact_upload SET expires_at=now()+INTERVAL '1 hour'
WHERE id=$1 AND workspace_id=$2 AND status='deleting';

-- name: GetWorkflowResultIssue :one
SELECT * FROM issue WHERE id=$1 AND workspace_id=$2 FOR SHARE;

-- name: GetWorkflowResultTask :one
SELECT task.* FROM agent_task_queue AS task
JOIN agent ON agent.id=task.agent_id
WHERE task.id=$1 AND agent.workspace_id=$2 FOR SHARE OF task;

-- name: GetWorkflowResultArtifact :one
SELECT revision.*, artifact.name, upload.storage_key
FROM workflow_artifact_revision AS revision
JOIN workflow_artifact AS artifact ON artifact.id=revision.artifact_id AND artifact.workspace_id=revision.workspace_id
JOIN workflow_artifact_upload AS upload ON upload.id=revision.upload_id AND upload.workspace_id=revision.workspace_id AND upload.status='committed'
JOIN workflow_execution_result AS result ON result.id=revision.result_id AND result.workspace_id=revision.workspace_id AND result.execution_id=revision.execution_id
WHERE revision.workspace_id=sqlc.arg(workspace_id)
AND revision.workflow_instance_id=sqlc.arg(instance_id)
AND revision.execution_id=sqlc.arg(execution_id)
AND result.revision=sqlc.arg(result_revision)
AND revision.id=sqlc.arg(artifact_revision_id);

-- name: GetWorkflowExecutionReadiness :one
-- One statement provides a consistent diagnostic snapshot. It is not an
-- acceptance lock; a decision transaction must recheck the completion barrier.
SELECT execution.id, execution.result_revision, execution.status,
       (instance.status='active' AND instance.archived_at IS NULL
         AND instance.current_stage_id=execution.workflow_stage_id
         AND instance.revision=execution.plan_revision) AS current_execution,
       EXISTS (SELECT 1 FROM workflow_execution_result result
         WHERE result.workspace_id=execution.workspace_id AND result.execution_id=execution.id
         AND result.revision=execution.result_revision) AS has_result,
       EXISTS (SELECT 1 FROM issue main_issue
         WHERE main_issue.workspace_id=execution.workspace_id AND main_issue.id=execution.main_issue_id
         AND main_issue.workflow_instance_id=execution.workflow_instance_id
         AND main_issue.workflow_stage_id=execution.workflow_stage_id AND main_issue.status='done') AS main_issue_done,
       EXISTS (SELECT 1 FROM agent_task_queue task
         WHERE task.id=execution.main_task_id AND task.workflow_stage_execution_id=execution.id
         AND task.issue_id=execution.main_issue_id AND task.agent_id=execution.executor_id
         AND task.status='completed') AS main_task_completed,
       (SELECT count(*) FROM issue required_issue
         WHERE required_issue.workspace_id=execution.workspace_id
         AND required_issue.workflow_instance_id=execution.workflow_instance_id
         AND required_issue.workflow_stage_id=execution.workflow_stage_id
         AND (required_issue.workflow_stage_execution_id IS NULL OR required_issue.workflow_stage_execution_id=execution.id)
         AND required_issue.workflow_invalidated_by_rework_id IS NULL
         AND required_issue.status <> 'done')::bigint AS incomplete_issues,
       (SELECT count(*) FROM agent_task_queue task
         JOIN issue task_issue ON task_issue.id=task.issue_id AND task_issue.workspace_id=execution.workspace_id
         WHERE task.workflow_review_request_id IS NULL AND (task.workflow_stage_execution_id=execution.id OR
           (task.workflow_stage_execution_id IS NULL AND task_issue.workflow_instance_id=execution.workflow_instance_id
             AND task_issue.workflow_stage_id=execution.workflow_stage_id
             AND task_issue.workflow_invalidated_by_rework_id IS NULL
             AND (task_issue.workflow_stage_execution_id IS NULL OR task_issue.workflow_stage_execution_id=execution.id)))
         AND task.status IN ('queued','dispatched','running','waiting_local_directory','deferred'))::bigint AS active_calls
FROM workflow_stage_execution execution
JOIN workflow_instance instance ON instance.id=execution.workflow_instance_id AND instance.workspace_id=execution.workspace_id
WHERE execution.workspace_id=$1 AND execution.workflow_instance_id=$2 AND execution.id=$3;
