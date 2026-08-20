-- name: ListWorkspaceMCPServers :many
SELECT id, workspace_id, name, created_by, created_at, updated_at
FROM workspace_mcp_server
WHERE workspace_id = $1
ORDER BY name ASC;

-- name: GetWorkspaceMCPServer :one
SELECT * FROM workspace_mcp_server
WHERE id = $1 AND workspace_id = $2;

-- name: CreateWorkspaceMCPServer :one
INSERT INTO workspace_mcp_server (workspace_id, name, config_encrypted, created_by)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateWorkspaceMCPServer :one
UPDATE workspace_mcp_server
SET name = $3, config_encrypted = $4, updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteWorkspaceMCPServer :execrows
DELETE FROM workspace_mcp_server
WHERE id = $1 AND workspace_id = $2;

-- name: DeleteAgentMCPAssignmentsByServer :exec
DELETE FROM agent_mcp_server WHERE server_id = $1;

-- name: ListAgentMCPServers :many
SELECT s.*
FROM workspace_mcp_server s
JOIN agent_mcp_server a ON a.server_id = s.id
WHERE a.agent_id = $1 AND s.workspace_id = $2 AND a.enabled = TRUE
ORDER BY s.name ASC;

-- name: ListAgentMCPServerIDs :many
SELECT server_id FROM agent_mcp_server
WHERE agent_id = $1 AND enabled = TRUE
ORDER BY server_id;

-- name: DeleteAgentMCPAssignments :exec
DELETE FROM agent_mcp_server WHERE agent_id = $1;

-- name: AssignAgentMCPServer :exec
INSERT INTO agent_mcp_server (agent_id, server_id, enabled)
VALUES ($1, $2, TRUE)
ON CONFLICT (agent_id, server_id) DO UPDATE SET enabled = TRUE;

-- name: SetTaskExecutionManifest :exec
UPDATE agent_task_queue
SET execution_manifest = $2
WHERE id = $1 AND execution_manifest IS NULL;
