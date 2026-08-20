export interface WorkspaceMCPServer {
  id: string;
  workspace_id: string;
  name: string;
  configured: boolean;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface WorkspaceMCPServerRequest {
  name: string;
  config: Record<string, unknown>;
}

export interface WorkspaceMCPServerList {
  servers: WorkspaceMCPServer[];
}

export interface AgentMCPAssignments {
  server_ids: string[];
}
