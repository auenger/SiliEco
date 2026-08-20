ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS execution_manifest;
ALTER TABLE workflow_instance_stage DROP COLUMN IF EXISTS allowed_mcp_server_ids;
ALTER TABLE workflow_stage DROP COLUMN IF EXISTS allowed_mcp_server_ids;
DROP TABLE IF EXISTS agent_mcp_server;
DROP TABLE IF EXISTS workspace_mcp_server;
