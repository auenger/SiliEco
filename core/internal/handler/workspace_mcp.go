package handler

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

type executionMCPRef struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	Source   string `json:"source"`
	Revision string `json:"revision,omitempty"`
}

func (h *Handler) resolveWorkspaceMCPForTask(ctx context.Context, task db.AgentTaskQueue, workspaceID pgtype.UUID) (json.RawMessage, []executionMCPRef, error) {
	rows, err := h.Queries.ListAgentMCPServers(ctx, db.ListAgentMCPServersParams{AgentID: task.AgentID, WorkspaceID: workspaceID})
	if err != nil {
		return nil, nil, err
	}
	var allowed []pgtype.UUID
	stageRestricted := false
	if task.IssueID.Valid {
		issue, err := h.Queries.GetIssue(ctx, task.IssueID)
		if err != nil {
			return nil, nil, fmt.Errorf("load task SOP scope: %w", err)
		}
		if issue.WorkflowInstanceID.Valid && issue.WorkflowStageID.Valid {
			stage, err := h.Queries.GetWorkflowInstanceStage(ctx, db.GetWorkflowInstanceStageParams{ID: issue.WorkflowStageID, WorkflowInstanceID: issue.WorkflowInstanceID, WorkspaceID: workspaceID})
			if err != nil {
				return nil, nil, err
			}
			if stage.AllowedMcpServerIds != nil {
				stageRestricted = true
				allowed = stage.AllowedMcpServerIds
			}
		}
	}
	rows, err = restrictMCPServersForStage(rows, allowed, stageRestricted)
	if err != nil {
		return nil, nil, err
	}
	config := map[string]json.RawMessage{}
	refs := make([]executionMCPRef, 0, len(rows))
	for _, row := range rows {
		id := uuidToString(row.ID)
		if h.VCSSecretBox == nil {
			return nil, nil, errors.New("MCP secret encryption is not configured")
		}
		decoded, err := h.openMCPConfig(row.ConfigEncrypted)
		if err != nil {
			return nil, nil, err
		}
		config[row.Name] = decoded
		digest := sha256.Sum256(decoded)
		refs = append(refs, executionMCPRef{ID: id, Name: row.Name, Source: "workspace", Revision: hex.EncodeToString(digest[:])})
	}
	if len(config) == 0 {
		return nil, refs, nil
	}
	raw, err := json.Marshal(config)
	return raw, refs, err
}

func restrictMCPServersForStage(rows []db.WorkspaceMcpServer, allowed []pgtype.UUID, restricted bool) ([]db.WorkspaceMcpServer, error) {
	if !restricted {
		return rows, nil
	}
	assigned := make(map[string]bool, len(rows))
	for _, row := range rows {
		assigned[uuidToString(row.ID)] = true
	}
	allowSet := make(map[string]bool, len(allowed))
	for _, id := range allowed {
		key := uuidToString(id)
		if !assigned[key] {
			return nil, errors.New("SOP stage requests an MCP server not assigned to the agent")
		}
		allowSet[key] = true
	}
	filtered := make([]db.WorkspaceMcpServer, 0, len(allowed))
	for _, row := range rows {
		if allowSet[uuidToString(row.ID)] {
			filtered = append(filtered, row)
		}
	}
	return filtered, nil
}

type WorkspaceMCPServerResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Configured  bool    `json:"configured"`
	CreatedBy   *string `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type workspaceMCPServerRequest struct {
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config"`
}

type agentMCPAssignmentsRequest struct {
	ServerIDs []string `json:"server_ids"`
}

func workspaceMCPResponse(row db.WorkspaceMcpServer) WorkspaceMCPServerResponse {
	return WorkspaceMCPServerResponse{ID: uuidToString(row.ID), WorkspaceID: uuidToString(row.WorkspaceID), Name: row.Name,
		Configured: row.ConfigEncrypted != "", CreatedBy: uuidToPtr(row.CreatedBy), CreatedAt: timestampToString(row.CreatedAt), UpdatedAt: timestampToString(row.UpdatedAt)}
}

func workspaceMCPListResponse(row db.ListWorkspaceMCPServersRow) WorkspaceMCPServerResponse {
	return WorkspaceMCPServerResponse{ID: uuidToString(row.ID), WorkspaceID: uuidToString(row.WorkspaceID), Name: row.Name,
		Configured: true, CreatedBy: uuidToPtr(row.CreatedBy), CreatedAt: timestampToString(row.CreatedAt), UpdatedAt: timestampToString(row.UpdatedAt)}
}

func (h *Handler) requireMCPWorkspace(w http.ResponseWriter, r *http.Request, manage bool) (db.Member, string, bool) {
	workspaceID := ctxWorkspaceID(r.Context())
	member, ok := ctxMember(r.Context())
	if workspaceID == "" || !ok {
		writeError(w, http.StatusUnauthorized, "workspace context is required")
		return db.Member{}, "", false
	}
	if manage && !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "workspace admin permission is required")
		return db.Member{}, "", false
	}
	if h.VCSSecretBox == nil {
		writeError(w, http.StatusServiceUnavailable, "MCP secret encryption is not configured")
		return db.Member{}, "", false
	}
	return member, workspaceID, true
}

func validateWorkspaceMCPRequest(req workspaceMCPServerRequest) (string, []byte, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 120 {
		return "", nil, errors.New("name is required and must not exceed 120 characters")
	}
	var config map[string]any
	if len(req.Config) == 0 || json.Unmarshal(req.Config, &config) != nil || config == nil {
		return "", nil, errors.New("config must be a JSON object")
	}
	command, hasCommand := config["command"].(string)
	url, hasURL := config["url"].(string)
	if (!hasCommand || strings.TrimSpace(command) == "") && (!hasURL || strings.TrimSpace(url) == "") {
		return "", nil, errors.New("config must include a non-empty command or url")
	}
	plain, err := json.Marshal(config)
	return name, plain, err
}

func (h *Handler) sealMCPConfig(plain []byte) (string, error) {
	sealed, err := h.VCSSecretBox.Seal(plain)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (h *Handler) openMCPConfig(encoded string) (json.RawMessage, error) {
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	plain, err := h.VCSSecretBox.Open(sealed)
	return json.RawMessage(plain), err
}

func decodeWorkspaceMCPJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeWorkspaceMCPInternalError(w http.ResponseWriter) {
	writeError(w, http.StatusInternalServerError, "MCP configuration operation failed")
}

func (h *Handler) ListWorkspaceMCPServers(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.requireMCPWorkspace(w, r, false)
	if !ok {
		return
	}
	rows, err := h.Queries.ListWorkspaceMCPServers(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeWorkspaceMCPInternalError(w)
		return
	}
	result := make([]WorkspaceMCPServerResponse, len(rows))
	for i, row := range rows {
		result[i] = workspaceMCPListResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": result})
}

func (h *Handler) CreateWorkspaceMCPServer(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, ok := h.requireMCPWorkspace(w, r, true)
	if !ok {
		return
	}
	var req workspaceMCPServerRequest
	if !decodeWorkspaceMCPJSON(w, r, &req) {
		return
	}
	name, plain, err := validateWorkspaceMCPRequest(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sealed, err := h.sealMCPConfig(plain)
	if err != nil {
		writeWorkspaceMCPInternalError(w)
		return
	}
	row, err := h.Queries.CreateWorkspaceMCPServer(r.Context(), db.CreateWorkspaceMCPServerParams{WorkspaceID: parseUUID(workspaceID), Name: name, ConfigEncrypted: sealed, CreatedBy: member.UserID})
	if err != nil {
		writeError(w, http.StatusConflict, "an MCP server with this name already exists")
		return
	}
	writeJSON(w, http.StatusCreated, workspaceMCPResponse(row))
}

func (h *Handler) UpdateWorkspaceMCPServer(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.requireMCPWorkspace(w, r, true)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "MCP server id")
	if !ok {
		return
	}
	var req workspaceMCPServerRequest
	if !decodeWorkspaceMCPJSON(w, r, &req) {
		return
	}
	name, plain, err := validateWorkspaceMCPRequest(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sealed, err := h.sealMCPConfig(plain)
	if err != nil {
		writeWorkspaceMCPInternalError(w)
		return
	}
	row, err := h.Queries.UpdateWorkspaceMCPServer(r.Context(), db.UpdateWorkspaceMCPServerParams{ID: id, WorkspaceID: parseUUID(workspaceID), Name: name, ConfigEncrypted: sealed})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "MCP server not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, "an MCP server with this name already exists")
		return
	}
	writeJSON(w, http.StatusOK, workspaceMCPResponse(row))
}

func (h *Handler) DeleteWorkspaceMCPServer(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.requireMCPWorkspace(w, r, true)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "MCP server id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeWorkspaceMCPInternalError(w)
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	if err = q.DeleteAgentMCPAssignmentsByServer(r.Context(), id); err != nil {
		writeWorkspaceMCPInternalError(w)
		return
	}
	n, err := q.DeleteWorkspaceMCPServer(r.Context(), db.DeleteWorkspaceMCPServerParams{ID: id, WorkspaceID: parseUUID(workspaceID)})
	if err != nil {
		writeWorkspaceMCPInternalError(w)
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "MCP server not found")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeWorkspaceMCPInternalError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListAgentMCPAssignments(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.requireMCPWorkspace(w, r, false)
	if !ok {
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "agent id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: parseUUID(workspaceID)}); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	} else if err != nil {
		writeWorkspaceMCPInternalError(w)
		return
	}
	ids, err := h.Queries.ListAgentMCPServerIDs(r.Context(), agentID)
	if err != nil {
		writeWorkspaceMCPInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"server_ids": uuidsToStrings(ids)})
}

func (h *Handler) SetAgentMCPAssignments(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.requireMCPWorkspace(w, r, true)
	if !ok {
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "agent id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: parseUUID(workspaceID)}); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	} else if err != nil {
		writeWorkspaceMCPInternalError(w)
		return
	}
	var req agentMCPAssignmentsRequest
	if !decodeWorkspaceMCPJSON(w, r, &req) {
		return
	}
	serverIDs, ok := parseUUIDSliceOrBadRequest(w, req.ServerIDs, "MCP server id")
	if !ok {
		return
	}
	for _, id := range serverIDs {
		if _, err := h.Queries.GetWorkspaceMCPServer(r.Context(), db.GetWorkspaceMCPServerParams{ID: id, WorkspaceID: parseUUID(workspaceID)}); err != nil {
			writeError(w, http.StatusBadRequest, "MCP server does not belong to this workspace")
			return
		}
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeWorkspaceMCPInternalError(w)
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	if err = q.DeleteAgentMCPAssignments(r.Context(), agentID); err != nil {
		writeWorkspaceMCPInternalError(w)
		return
	}
	for _, id := range serverIDs {
		if err = q.AssignAgentMCPServer(r.Context(), db.AssignAgentMCPServerParams{AgentID: agentID, ServerID: id}); err != nil {
			writeWorkspaceMCPInternalError(w)
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeWorkspaceMCPInternalError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"server_ids": req.ServerIDs})
}
