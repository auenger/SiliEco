-- name: CreateAttachmentReference :one
INSERT INTO attachment_reference (
    workspace_id, attachment_id, target_type, target_id, created_by
) VALUES (
    sqlc.arg(workspace_id), sqlc.arg(attachment_id), sqlc.arg(target_type),
    sqlc.arg(target_id), sqlc.arg(created_by)
)
ON CONFLICT (workspace_id, attachment_id, target_type, target_id)
DO UPDATE SET attachment_id = EXCLUDED.attachment_id
RETURNING *;

-- name: DeleteAttachmentReference :execrows
DELETE FROM attachment_reference
WHERE workspace_id = sqlc.arg(workspace_id)
  AND attachment_id = sqlc.arg(attachment_id)
  AND target_type = sqlc.arg(target_type)
  AND target_id = sqlc.arg(target_id);

-- name: ListAttachmentReferences :many
SELECT * FROM attachment_reference
WHERE workspace_id = sqlc.arg(workspace_id)
  AND attachment_id = sqlc.arg(attachment_id)
ORDER BY created_at ASC;

-- name: ListAttachmentReferencesByTarget :many
SELECT * FROM attachment_reference
WHERE workspace_id = sqlc.arg(workspace_id)
  AND target_type = sqlc.arg(target_type)
  AND target_id = sqlc.arg(target_id)
ORDER BY created_at ASC;

-- name: ListWorkspaceAttachmentReferences :many
SELECT
    ar.*,
    COALESCE(target_task.title, target_project.title, '') AS target_title,
    target_task.number AS target_task_number,
    COALESCE(target_task.project_id, target_project.id) AS target_project_id,
    COALESCE(task_project.title, target_project.title, '') AS target_project_title
FROM attachment_reference ar
LEFT JOIN task target_task
    ON ar.target_type = 'task'
   AND target_task.id = ar.target_id
   AND target_task.workspace_id = ar.workspace_id
LEFT JOIN project target_project
    ON ar.target_type = 'project'
   AND target_project.id = ar.target_id
   AND target_project.workspace_id = ar.workspace_id
LEFT JOIN project task_project
    ON task_project.id = target_task.project_id
   AND task_project.workspace_id = ar.workspace_id
WHERE ar.workspace_id = sqlc.arg(workspace_id)
ORDER BY ar.created_at ASC;

-- name: CountAttachmentReferences :one
SELECT count(*) FROM attachment_reference
WHERE workspace_id = sqlc.arg(workspace_id)
  AND attachment_id = sqlc.arg(attachment_id);

-- name: DeleteAttachmentReferencesByTarget :exec
DELETE FROM attachment_reference
WHERE workspace_id = sqlc.arg(workspace_id)
  AND target_type = sqlc.arg(target_type)
  AND target_id = sqlc.arg(target_id);

-- name: DeleteAttachmentReferencesByWorkspace :exec
DELETE FROM attachment_reference
WHERE workspace_id = sqlc.arg(workspace_id);

-- name: DetachReferencedAttachmentsFromTask :many
UPDATE attachment AS a
SET task_id = NULL,
    comment_id = NULL
WHERE a.workspace_id = sqlc.arg(workspace_id)
  AND (
      a.task_id = sqlc.arg(task_id)
      OR a.comment_id IN (
          SELECT c.id
          FROM comment c
          JOIN task ci ON ci.id = c.task_id
          WHERE ci.workspace_id = a.workspace_id
            AND c.task_id = sqlc.arg(task_id)
      )
  )
  AND EXISTS (
      SELECT 1 FROM attachment_reference ar
      WHERE ar.workspace_id = a.workspace_id
        AND ar.attachment_id = a.id
  )
RETURNING id;

-- name: DetachReferencedAttachmentsFromComment :many
UPDATE attachment AS a
SET comment_id = NULL,
    task_id = NULL
WHERE a.workspace_id = sqlc.arg(workspace_id)
  AND a.comment_id = sqlc.arg(comment_id)
  AND EXISTS (
      SELECT 1 FROM attachment_reference ar
      WHERE ar.workspace_id = a.workspace_id
        AND ar.attachment_id = a.id
  )
RETURNING id;

-- name: ListWorkspaceFileResources :many
WITH candidates AS (
    SELECT a.id
    FROM attachment a
    WHERE a.workspace_id = sqlc.arg(workspace_id)
      AND a.task_id IS NOT NULL
      AND a.chat_session_id IS NULL
      AND a.chat_message_id IS NULL
    UNION
    SELECT ar.attachment_id
    FROM attachment_reference ar
    WHERE ar.workspace_id = sqlc.arg(workspace_id)
)
SELECT
    a.*,
    i.title AS source_task_title,
    i.number AS source_task_number,
    i.project_id AS source_project_id,
    p.title AS source_project_title,
    (SELECT count(*) FROM attachment_reference ar
     WHERE ar.workspace_id = a.workspace_id AND ar.attachment_id = a.id) AS reference_count
FROM candidates c
JOIN attachment a ON a.id = c.id AND a.workspace_id = sqlc.arg(workspace_id)
LEFT JOIN task i ON i.id = a.task_id AND i.workspace_id = a.workspace_id
LEFT JOIN project p ON p.id = i.project_id AND p.workspace_id = a.workspace_id
WHERE a.chat_session_id IS NULL
  AND a.chat_message_id IS NULL
ORDER BY a.created_at DESC, a.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountWorkspaceFileResources :one
SELECT count(DISTINCT a.id)
FROM attachment a
WHERE a.workspace_id = sqlc.arg(workspace_id)
  AND a.chat_session_id IS NULL
  AND a.chat_message_id IS NULL
  AND (
      a.task_id IS NOT NULL
      OR EXISTS (
          SELECT 1 FROM attachment_reference ar
          WHERE ar.workspace_id = a.workspace_id AND ar.attachment_id = a.id
      )
  );

-- name: ListProjectFileResources :many
WITH candidates AS (
    SELECT a.id
    FROM attachment a
    JOIN task i ON i.id = a.task_id AND i.workspace_id = a.workspace_id
    WHERE a.workspace_id = sqlc.arg(workspace_id)
      AND i.project_id = sqlc.arg(project_id)
      AND a.chat_session_id IS NULL
      AND a.chat_message_id IS NULL
    UNION
    SELECT ar.attachment_id
    FROM attachment_reference ar
    WHERE ar.workspace_id = sqlc.arg(workspace_id)
      AND (
          (ar.target_type = 'project' AND ar.target_id = sqlc.arg(project_id))
          OR (ar.target_type = 'task' AND EXISTS (
              SELECT 1 FROM task target_task
              WHERE target_task.id = ar.target_id
                AND target_task.workspace_id = ar.workspace_id
                AND target_task.project_id = sqlc.arg(project_id)
          ))
      )
)
SELECT
    a.*,
    i.title AS source_task_title,
    i.number AS source_task_number,
    i.project_id AS source_project_id,
    p.title AS source_project_title,
    (SELECT count(*) FROM attachment_reference ar
     WHERE ar.workspace_id = a.workspace_id AND ar.attachment_id = a.id) AS reference_count
FROM candidates c
JOIN attachment a ON a.id = c.id AND a.workspace_id = sqlc.arg(workspace_id)
LEFT JOIN task i ON i.id = a.task_id AND i.workspace_id = a.workspace_id
LEFT JOIN project p ON p.id = i.project_id AND p.workspace_id = a.workspace_id
WHERE a.chat_session_id IS NULL
  AND a.chat_message_id IS NULL
ORDER BY a.created_at DESC, a.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountProjectFileResources :one
SELECT count(DISTINCT a.id)
FROM attachment a
WHERE a.workspace_id = sqlc.arg(workspace_id)
  AND a.chat_session_id IS NULL
  AND a.chat_message_id IS NULL
  AND (
      EXISTS (
          SELECT 1 FROM task source_task
          WHERE source_task.id = a.task_id
            AND source_task.workspace_id = a.workspace_id
            AND source_task.project_id = sqlc.arg(project_id)
      )
      OR EXISTS (
          SELECT 1 FROM attachment_reference ar
          WHERE ar.workspace_id = a.workspace_id
            AND ar.attachment_id = a.id
            AND (
                (ar.target_type = 'project' AND ar.target_id = sqlc.arg(project_id))
                OR (ar.target_type = 'task' AND EXISTS (
                    SELECT 1 FROM task target_task
                    WHERE target_task.id = ar.target_id
                      AND target_task.workspace_id = ar.workspace_id
                      AND target_task.project_id = sqlc.arg(project_id)
                ))
            )
      )
  );
