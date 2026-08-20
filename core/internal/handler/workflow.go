package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/logger"
	"github.com/silieco-ai/silieco/core/internal/service"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

const maxWorkflowStages = 50

var validWorkflowInstanceStatuses = map[string]bool{
	"draft": true, "active": true, "waiting": true,
	"completed": true, "cancelled": true,
}

type WorkflowStageInput struct {
	ID                  *string         `json:"id,omitempty"`
	StableKey           string          `json:"stable_key"`
	Name                string          `json:"name"`
	Description         *string         `json:"description"`
	CompletionRule      json.RawMessage `json:"completion_rule"`
	InputSpec           json.RawMessage `json:"input_spec"`
	OutputSpec          json.RawMessage `json:"output_spec"`
	RequiredSkills      []string        `json:"required_skills"`
	AllowedMCPServerIDs []string        `json:"allowed_mcp_server_ids"`
	Gate                json.RawMessage `json:"gate"`
	RollbackStageKey    *string         `json:"rollback_stage_key"`
}

type CreateWorkflowRequest struct {
	ProjectID   *string              `json:"project_id"`
	Name        string               `json:"name"`
	Description *string              `json:"description"`
	Publish     bool                 `json:"publish"`
	Stages      []WorkflowStageInput `json:"stages"`
}

type UpdateWorkflowRequest struct {
	ProjectID   *string `json:"project_id"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type CreateWorkflowVersionRequest struct {
	Publish bool                 `json:"publish"`
	Stages  []WorkflowStageInput `json:"stages"`
}

type CreateWorkflowInstanceRequest struct {
	Title       string  `json:"title"`
	Description *string `json:"description"`
	VersionID   *string `json:"version_id"`
	ProjectID   *string `json:"project_id"`
	Start       *bool   `json:"start"`
}

type TransitionWorkflowInstanceRequest struct {
	TargetStageID *string `json:"target_stage_id"`
	Outcome       string  `json:"outcome"`
	Note          *string `json:"note"`
}

type AttachWorkflowTaskRequest struct {
	StageID string `json:"stage_id"`
}

type WorkflowResponse struct {
	ID               string                   `json:"id"`
	WorkspaceID      string                   `json:"workspace_id"`
	ProjectID        *string                  `json:"project_id"`
	Name             string                   `json:"name"`
	Description      *string                  `json:"description"`
	Status           string                   `json:"status"`
	CurrentVersionID *string                  `json:"current_version_id"`
	CurrentVersion   *WorkflowVersionResponse `json:"current_version,omitempty"`
	CreatedBy        string                   `json:"created_by"`
	CreatedAt        string                   `json:"created_at"`
	UpdatedAt        string                   `json:"updated_at"`
}

type WorkflowVersionResponse struct {
	ID          string                  `json:"id"`
	WorkspaceID string                  `json:"workspace_id"`
	WorkflowID  string                  `json:"workflow_id"`
	Version     int32                   `json:"version"`
	Status      string                  `json:"status"`
	CreatedBy   string                  `json:"created_by"`
	PublishedAt *string                 `json:"published_at"`
	CreatedAt   string                  `json:"created_at"`
	Stages      []WorkflowStageResponse `json:"stages"`
}

type WorkflowStageResponse struct {
	ID                  string          `json:"id"`
	WorkspaceID         string          `json:"workspace_id"`
	WorkflowVersionID   string          `json:"workflow_version_id"`
	StableKey           string          `json:"stable_key"`
	Name                string          `json:"name"`
	Description         *string         `json:"description"`
	Position            int32           `json:"position"`
	CompletionRule      json.RawMessage `json:"completion_rule"`
	InputSpec           json.RawMessage `json:"input_spec"`
	OutputSpec          json.RawMessage `json:"output_spec"`
	RequiredSkills      []string        `json:"required_skills"`
	AllowedMCPServerIDs []string        `json:"allowed_mcp_server_ids"`
	Gate                json.RawMessage `json:"gate"`
	RollbackStageKey    *string         `json:"rollback_stage_key"`
	CreatedAt           string          `json:"created_at"`
}

type WorkflowInstanceResponse struct {
	ID                string                           `json:"id"`
	WorkspaceID       string                           `json:"workspace_id"`
	WorkflowID        string                           `json:"workflow_id"`
	WorkflowVersionID string                           `json:"workflow_version_id"`
	Title             string                           `json:"title"`
	Description       *string                          `json:"description"`
	Revision          int32                            `json:"revision"`
	CanEdit           bool                             `json:"can_edit"`
	SourceVersion     int32                            `json:"source_version"`
	Status            string                           `json:"status"`
	CurrentStageID    *string                          `json:"current_stage_id"`
	CurrentStageName  *string                          `json:"current_stage_name"`
	CurrentStageIndex *int32                           `json:"current_stage_index"`
	StageCount        int32                            `json:"stage_count"`
	ProjectID         *string                          `json:"project_id"`
	CreatedBy         string                           `json:"created_by"`
	StartedAt         *string                          `json:"started_at"`
	CompletedAt       *string                          `json:"completed_at"`
	TaskCount         int64                            `json:"task_count"`
	ArchivedAt        *string                          `json:"archived_at"`
	ArchivedBy        *string                          `json:"archived_by"`
	CreatedAt         string                           `json:"created_at"`
	UpdatedAt         string                           `json:"updated_at"`
	Stages            []WorkflowInstanceStageResponse  `json:"stages,omitempty"`
	Tasks             []IssueResponse                  `json:"tasks,omitempty"`
	Decisions         []WorkflowGateDecisionResponse   `json:"decisions,omitempty"`
	Changes           []WorkflowInstanceChangeResponse `json:"changes,omitempty"`
}

type WorkflowInstanceStageResponse struct {
	ID                     string            `json:"id"`
	WorkspaceID            string            `json:"workspace_id"`
	WorkflowInstanceID     string            `json:"workflow_instance_id"`
	SourceStageID          *string           `json:"source_stage_id"`
	StableKey              string            `json:"stable_key"`
	Name                   string            `json:"name"`
	Description            *string           `json:"description"`
	Position               int32             `json:"position"`
	CompletionRule         json.RawMessage   `json:"completion_rule"`
	InputSpec              json.RawMessage   `json:"input_spec"`
	OutputSpec             json.RawMessage   `json:"output_spec"`
	RequiredSkills         []string          `json:"required_skills"`
	RequiredSkillRevisions map[string]string `json:"required_skill_revisions"`
	AllowedMCPServerIDs    []string          `json:"allowed_mcp_server_ids"`
	Gate                   json.RawMessage   `json:"gate"`
	RollbackStageKey       *string           `json:"rollback_stage_key"`
	CreatedAt              string            `json:"created_at"`
	UpdatedAt              string            `json:"updated_at"`
}

type WorkflowInstanceChangeResponse struct {
	ID         string          `json:"id"`
	Revision   int32           `json:"revision"`
	ChangedBy  string          `json:"changed_by"`
	ChangeNote *string         `json:"change_note"`
	BeforePlan json.RawMessage `json:"before_plan"`
	AfterPlan  json.RawMessage `json:"after_plan"`
	CreatedAt  string          `json:"created_at"`
}

type WorkflowGateDecisionResponse struct {
	ID                 string  `json:"id"`
	WorkflowInstanceID string  `json:"workflow_instance_id"`
	FromStageID        string  `json:"from_stage_id"`
	ToStageID          *string `json:"to_stage_id"`
	Outcome            string  `json:"outcome"`
	ActorType          string  `json:"actor_type"`
	ActorID            string  `json:"actor_id"`
	Note               *string `json:"note"`
	CreatedAt          string  `json:"created_at"`
}

func workflowToResponse(row db.Workflow) WorkflowResponse {
	return WorkflowResponse{
		ID:               uuidToString(row.ID),
		WorkspaceID:      uuidToString(row.WorkspaceID),
		ProjectID:        uuidToPtr(row.ProjectID),
		Name:             row.Name,
		Description:      textToPtr(row.Description),
		Status:           row.Status,
		CurrentVersionID: uuidToPtr(row.CurrentVersionID),
		CreatedBy:        uuidToString(row.CreatedBy),
		CreatedAt:        timestampToString(row.CreatedAt),
		UpdatedAt:        timestampToString(row.UpdatedAt),
	}
}

func workflowVersionToResponse(row db.WorkflowVersion, stages []db.WorkflowStage) WorkflowVersionResponse {
	resp := WorkflowVersionResponse{
		ID:          uuidToString(row.ID),
		WorkspaceID: uuidToString(row.WorkspaceID),
		WorkflowID:  uuidToString(row.WorkflowID),
		Version:     row.Version,
		Status:      row.Status,
		CreatedBy:   uuidToString(row.CreatedBy),
		PublishedAt: timestampToPtr(row.PublishedAt),
		CreatedAt:   timestampToString(row.CreatedAt),
		Stages:      make([]WorkflowStageResponse, len(stages)),
	}
	for i, stage := range stages {
		resp.Stages[i] = workflowStageToResponse(stage)
	}
	return resp
}

func workflowStageToResponse(row db.WorkflowStage) WorkflowStageResponse {
	requiredSkills := row.RequiredSkills
	if requiredSkills == nil {
		requiredSkills = []string{}
	}
	return WorkflowStageResponse{
		ID:                  uuidToString(row.ID),
		WorkspaceID:         uuidToString(row.WorkspaceID),
		WorkflowVersionID:   uuidToString(row.WorkflowVersionID),
		StableKey:           row.StableKey,
		Name:                row.Name,
		Description:         textToPtr(row.Description),
		Position:            row.Position,
		CompletionRule:      jsonOrEmptyObject(row.CompletionRule),
		InputSpec:           jsonOrEmptyObject(row.InputSpec),
		OutputSpec:          jsonOrEmptyObject(row.OutputSpec),
		RequiredSkills:      requiredSkills,
		AllowedMCPServerIDs: uuidsToStrings(row.AllowedMcpServerIds),
		Gate:                jsonOrEmptyObject(row.Gate),
		RollbackStageKey:    textToPtr(row.RollbackStageKey),
		CreatedAt:           timestampToString(row.CreatedAt),
	}
}

func workflowInstanceToResponse(row db.WorkflowInstance) WorkflowInstanceResponse {
	return WorkflowInstanceResponse{
		ID:                uuidToString(row.ID),
		WorkspaceID:       uuidToString(row.WorkspaceID),
		WorkflowID:        uuidToString(row.WorkflowID),
		WorkflowVersionID: uuidToString(row.WorkflowVersionID),
		Title:             row.Title,
		Description:       textToPtr(row.Description),
		Revision:          row.Revision,
		Status:            row.Status,
		CurrentStageID:    uuidToPtr(row.CurrentStageID),
		ProjectID:         uuidToPtr(row.ProjectID),
		CreatedBy:         uuidToString(row.CreatedBy),
		StartedAt:         timestampToPtr(row.StartedAt),
		CompletedAt:       timestampToPtr(row.CompletedAt),
		ArchivedAt:        timestampToPtr(row.ArchivedAt),
		ArchivedBy:        uuidToPtr(row.ArchivedBy),
		CreatedAt:         timestampToString(row.CreatedAt),
		UpdatedAt:         timestampToString(row.UpdatedAt),
	}
}

func workflowInstanceStageToResponse(row db.WorkflowInstanceStage) WorkflowInstanceStageResponse {
	requiredSkills := row.RequiredSkills
	if requiredSkills == nil {
		requiredSkills = []string{}
	}
	revisions := map[string]string{}
	var snapshots []service.AgentSkillData
	if json.Unmarshal(row.RequiredSkillSnapshots, &snapshots) == nil {
		_, refs := service.BuildAgentSkillBundles(snapshots)
		for _, ref := range refs {
			revisions[ref.ID] = ref.Hash
		}
	}
	return WorkflowInstanceStageResponse{
		ID: uuidToString(row.ID), WorkspaceID: uuidToString(row.WorkspaceID),
		WorkflowInstanceID: uuidToString(row.WorkflowInstanceID), SourceStageID: uuidToPtr(row.SourceStageID),
		StableKey: row.StableKey, Name: row.Name, Description: textToPtr(row.Description), Position: row.Position,
		CompletionRule: jsonOrEmptyObject(row.CompletionRule), InputSpec: jsonOrEmptyObject(row.InputSpec),
		OutputSpec: jsonOrEmptyObject(row.OutputSpec), RequiredSkills: requiredSkills, RequiredSkillRevisions: revisions,
		AllowedMCPServerIDs: uuidsToStrings(row.AllowedMcpServerIds),
		Gate:                jsonOrEmptyObject(row.Gate), RollbackStageKey: textToPtr(row.RollbackStageKey),
		CreatedAt: timestampToString(row.CreatedAt), UpdatedAt: timestampToString(row.UpdatedAt),
	}
}

func workflowInstanceChangeToResponse(row db.WorkflowInstanceChange) WorkflowInstanceChangeResponse {
	return WorkflowInstanceChangeResponse{
		ID: uuidToString(row.ID), Revision: row.Revision, ChangedBy: uuidToString(row.ChangedBy),
		ChangeNote: textToPtr(row.ChangeNote), BeforePlan: jsonOrEmptyObject(row.BeforePlan),
		AfterPlan: jsonOrEmptyObject(row.AfterPlan), CreatedAt: timestampToString(row.CreatedAt),
	}
}

func workflowDecisionToResponse(row db.WorkflowGateDecision) WorkflowGateDecisionResponse {
	return WorkflowGateDecisionResponse{
		ID:                 uuidToString(row.ID),
		WorkflowInstanceID: uuidToString(row.WorkflowInstanceID),
		FromStageID:        uuidToString(row.FromStageID),
		ToStageID:          uuidToPtr(row.ToStageID),
		Outcome:            row.Outcome,
		ActorType:          row.ActorType,
		ActorID:            uuidToString(row.ActorID),
		Note:               textToPtr(row.Note),
		CreatedAt:          timestampToString(row.CreatedAt),
	}
}

func jsonOrEmptyObject(raw []byte) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(raw)
}

func normalizeObject(raw json.RawMessage, fallback string) ([]byte, error) {
	if len(raw) == 0 {
		return []byte(fallback), nil
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return nil, errors.New("must be a JSON object")
	}
	return raw, nil
}

func normalizeWorkflowStages(stages []WorkflowStageInput) ([]WorkflowStageInput, error) {
	if len(stages) == 0 {
		return nil, errors.New("at least one stage is required")
	}
	if len(stages) > maxWorkflowStages {
		return nil, fmt.Errorf("a workflow may contain at most %d stages", maxWorkflowStages)
	}

	seen := make(map[string]int, len(stages))
	for i := range stages {
		stages[i].Name = strings.TrimSpace(stages[i].Name)
		if stages[i].Name == "" {
			return nil, fmt.Errorf("stages[%d].name is required", i)
		}
		stages[i].StableKey = strings.TrimSpace(stages[i].StableKey)
		if stages[i].StableKey == "" {
			stages[i].StableKey = fmt.Sprintf("stage_%d", i+1)
		}
		if previous, ok := seen[stages[i].StableKey]; ok {
			return nil, fmt.Errorf("stages[%d].stable_key duplicates stages[%d]", i, previous)
		}
		seen[stages[i].StableKey] = i

		if _, err := normalizeObject(stages[i].CompletionRule, `{"type":"all_tasks_terminal"}`); err != nil {
			return nil, fmt.Errorf("stages[%d].completion_rule %w", i, err)
		}
		if _, err := normalizeObject(stages[i].InputSpec, `{}`); err != nil {
			return nil, fmt.Errorf("stages[%d].input_spec %w", i, err)
		}
		if _, err := normalizeObject(stages[i].OutputSpec, `{}`); err != nil {
			return nil, fmt.Errorf("stages[%d].output_spec %w", i, err)
		}
		gate, err := normalizeObject(stages[i].Gate, `{"type":"none"}`)
		if err != nil {
			return nil, fmt.Errorf("stages[%d].gate %w", i, err)
		}
		var gateValue struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(gate, &gateValue); err != nil {
			return nil, fmt.Errorf("stages[%d].gate is invalid", i)
		}
		switch gateValue.Type {
		case "", "none", "human", "agent", "hybrid":
		default:
			return nil, fmt.Errorf("stages[%d].gate.type must be none, human, agent, or hybrid", i)
		}
		if stages[i].RollbackStageKey != nil {
			key := strings.TrimSpace(*stages[i].RollbackStageKey)
			if key == "" {
				stages[i].RollbackStageKey = nil
			} else {
				stages[i].RollbackStageKey = &key
			}
		}
		seenMCP := make(map[string]struct{}, len(stages[i].AllowedMCPServerIDs))
		for j, id := range stages[i].AllowedMCPServerIDs {
			id = strings.TrimSpace(id)
			if _, err := util.ParseUUID(id); err != nil {
				return nil, fmt.Errorf("stages[%d].allowed_mcp_server_ids[%d] is invalid", i, j)
			}
			if _, duplicate := seenMCP[id]; duplicate {
				return nil, fmt.Errorf("stages[%d].allowed_mcp_server_ids contains a duplicate", i)
			}
			seenMCP[id] = struct{}{}
			stages[i].AllowedMCPServerIDs[j] = id
		}
	}

	for i, stage := range stages {
		if stage.RollbackStageKey == nil {
			continue
		}
		target, ok := seen[*stage.RollbackStageKey]
		if !ok {
			return nil, fmt.Errorf("stages[%d].rollback_stage_key does not exist", i)
		}
		if target >= i {
			return nil, fmt.Errorf("stages[%d].rollback_stage_key must point to an earlier stage", i)
		}
	}
	return stages, nil
}

func parseUUIDArray(ids []string) []pgtype.UUID {
	if ids == nil {
		return nil
	}
	out := make([]pgtype.UUID, len(ids))
	for i, id := range ids {
		out[i] = parseUUID(id)
	}
	return out
}

func createWorkflowStages(ctx context.Context, q *db.Queries, workspaceID, versionID pgtype.UUID, stages []WorkflowStageInput) ([]db.WorkflowStage, error) {
	rows := make([]db.WorkflowStage, 0, len(stages))
	for i, stage := range stages {
		completionRule, _ := normalizeObject(stage.CompletionRule, `{"type":"all_tasks_terminal"}`)
		inputSpec, _ := normalizeObject(stage.InputSpec, `{}`)
		outputSpec, _ := normalizeObject(stage.OutputSpec, `{}`)
		gate, _ := normalizeObject(stage.Gate, `{"type":"none"}`)
		requiredSkills := stage.RequiredSkills
		if requiredSkills == nil {
			requiredSkills = []string{}
		}
		row, err := q.CreateWorkflowStage(ctx, db.CreateWorkflowStageParams{
			WorkspaceID:         workspaceID,
			WorkflowVersionID:   versionID,
			StableKey:           stage.StableKey,
			Name:                stage.Name,
			Description:         ptrToText(stage.Description),
			Position:            int32(i),
			CompletionRule:      completionRule,
			InputSpec:           inputSpec,
			OutputSpec:          outputSpec,
			RequiredSkills:      requiredSkills,
			Gate:                gate,
			RollbackStageKey:    ptrToText(stage.RollbackStageKey),
			AllowedMcpServerIds: parseUUIDArray(stage.AllowedMCPServerIDs),
		})
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (h *Handler) requireWorkflowAdmin(w http.ResponseWriter, r *http.Request) (string, string, pgtype.UUID, pgtype.UUID, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return "", "", pgtype.UUID{}, pgtype.UUID{}, false
	}
	if actorType, _ := h.resolveActor(r, userID, workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot manage workflow definitions")
		return "", "", pgtype.UUID{}, pgtype.UUID{}, false
	}
	if _, roleOK := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !roleOK {
		return "", "", pgtype.UUID{}, pgtype.UUID{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return "", "", pgtype.UUID{}, pgtype.UUID{}, false
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return "", "", pgtype.UUID{}, pgtype.UUID{}, false
	}
	return workspaceID, userID, wsUUID, userUUID, true
}

func (h *Handler) requireWorkflowMember(w http.ResponseWriter, r *http.Request) (string, string, pgtype.UUID, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return "", "", pgtype.UUID{}, false
	}
	if _, memberOK := h.workspaceMember(w, r, workspaceID); !memberOK {
		return "", "", pgtype.UUID{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return "", "", pgtype.UUID{}, false
	}
	return workspaceID, userID, wsUUID, true
}

func (h *Handler) loadWorkflowCurrentVersion(ctx context.Context, workflow db.Workflow) (*WorkflowVersionResponse, error) {
	if !workflow.CurrentVersionID.Valid {
		return nil, nil
	}
	version, err := h.Queries.GetWorkflowVersionInWorkspace(ctx, db.GetWorkflowVersionInWorkspaceParams{
		ID: workflow.CurrentVersionID, WorkflowID: workflow.ID, WorkspaceID: workflow.WorkspaceID,
	})
	if err != nil {
		return nil, err
	}
	stages, err := h.Queries.ListWorkflowStages(ctx, db.ListWorkflowStagesParams{
		WorkflowVersionID: version.ID, WorkspaceID: workflow.WorkspaceID,
	})
	if err != nil {
		return nil, err
	}
	resp := workflowVersionToResponse(version, stages)
	return &resp, nil
}

func (h *Handler) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	_, _, wsUUID, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return
	}
	var projectID pgtype.UUID
	if value := strings.TrimSpace(r.URL.Query().Get("project_id")); value != "" {
		projectID, ok = parseUUIDOrBadRequest(w, value, "project_id")
		if !ok {
			return
		}
	}
	rows, err := h.Queries.ListWorkflows(r.Context(), db.ListWorkflowsParams{
		WorkspaceID: wsUUID, ProjectID: projectID,
		IncludeArchived: r.URL.Query().Get("include_archived") == "true",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflows")
		return
	}
	resp := make([]WorkflowResponse, len(rows))
	for i, row := range rows {
		resp[i] = workflowToResponse(row)
		current, loadErr := h.loadWorkflowCurrentVersion(r.Context(), row)
		if loadErr != nil {
			slog.Warn("failed to load current workflow version", "workflow_id", resp[i].ID, "error", loadErr)
		} else {
			resp[i].CurrentVersion = current
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": resp, "total": len(resp)})
}

func (h *Handler) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	_, _, wsUUID, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return
	}
	workflowID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	row, err := h.Queries.GetWorkflowInWorkspace(r.Context(), db.GetWorkflowInWorkspaceParams{
		ID: workflowID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	resp := workflowToResponse(row)
	current, err := h.loadWorkflowCurrentVersion(r.Context(), row)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow version")
		return
	}
	resp.CurrentVersion = current
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, wsUUID, userUUID, ok := h.requireWorkflowAdmin(w, r)
	if !ok {
		return
	}
	var req CreateWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	var projectID pgtype.UUID
	if req.ProjectID != nil && strings.TrimSpace(*req.ProjectID) != "" {
		projectID, ok = parseUUIDOrBadRequest(w, strings.TrimSpace(*req.ProjectID), "project_id")
		if !ok {
			return
		}
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
			ID: projectID, WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "project not found")
			return
		}
	}
	stages, err := normalizeWorkflowStages(req.Stages)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	workflow, err := qtx.CreateWorkflow(r.Context(), db.CreateWorkflowParams{
		WorkspaceID: wsUUID, ProjectID: projectID,
		Name: req.Name, Description: ptrToText(req.Description),
		Status: "draft", CreatedBy: userUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workflow")
		return
	}
	version, err := qtx.CreateWorkflowVersion(r.Context(), db.CreateWorkflowVersionParams{
		WorkspaceID: wsUUID, WorkflowID: workflow.ID, Version: 1,
		Status: "draft", CreatedBy: userUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workflow version")
		return
	}
	stageRows, err := createWorkflowStages(r.Context(), qtx, wsUUID, version.ID, stages)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workflow stages")
		return
	}
	if req.Publish {
		version, err = qtx.PublishWorkflowVersion(r.Context(), db.PublishWorkflowVersionParams{
			ID: version.ID, WorkflowID: workflow.ID, WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to publish workflow")
			return
		}
		workflow, err = qtx.SetWorkflowCurrentVersion(r.Context(), db.SetWorkflowCurrentVersionParams{
			ID: workflow.ID, WorkspaceID: wsUUID, CurrentVersionID: version.ID, Status: "published",
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to activate workflow version")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit workflow")
		return
	}
	resp := workflowToResponse(workflow)
	versionResp := workflowVersionToResponse(version, stageRows)
	resp.CurrentVersion = &versionResp
	if !req.Publish {
		resp.CurrentVersionID = nil
	}
	h.publish(protocol.EventWorkflowCreated, workspaceID, "member", userID, map[string]any{"workflow": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, wsUUID, _, ok := h.requireWorkflowAdmin(w, r)
	if !ok {
		return
	}
	workflowID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	current, err := h.Queries.GetWorkflowInWorkspace(r.Context(), db.GetWorkflowInWorkspaceParams{
		ID: workflowID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	var req UpdateWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		req.Name = &name
	}
	projectID := current.ProjectID
	if req.ProjectID != nil {
		value := strings.TrimSpace(*req.ProjectID)
		if value == "" {
			writeError(w, http.StatusBadRequest, "project_id cannot be empty")
			return
		}
		projectID, ok = parseUUIDOrBadRequest(w, value, "project_id")
		if !ok {
			return
		}
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
			ID: projectID, WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "project not found")
			return
		}
		if current.ProjectID.Valid && current.ProjectID != projectID {
			instanceCount, countErr := h.Queries.CountWorkflowInstances(r.Context(), db.CountWorkflowInstancesParams{
				WorkflowID: workflowID, WorkspaceID: wsUUID,
			})
			if countErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to validate workflow project")
				return
			}
			if instanceCount > 0 {
				writeError(w, http.StatusConflict, "workflow cannot move projects after a run has been created")
				return
			}
		}
	}
	description := textToPtr(current.Description)
	if req.Description != nil {
		description = req.Description
	}
	row, err := h.Queries.UpdateWorkflowMetadata(r.Context(), db.UpdateWorkflowMetadataParams{
		ID: workflowID, WorkspaceID: wsUUID, ProjectID: projectID,
		Name: ptrToText(req.Name), Description: ptrToText(description),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update workflow")
		return
	}
	resp := workflowToResponse(row)
	resp.CurrentVersion, _ = h.loadWorkflowCurrentVersion(r.Context(), row)
	h.publish(protocol.EventWorkflowUpdated, workspaceID, "member", userID, map[string]any{"workflow": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ArchiveWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, wsUUID, _, ok := h.requireWorkflowAdmin(w, r)
	if !ok {
		return
	}
	workflowID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	row, err := h.Queries.ArchiveWorkflow(r.Context(), db.ArchiveWorkflowParams{
		ID: workflowID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	resp := workflowToResponse(row)
	h.publish(protocol.EventWorkflowUpdated, workspaceID, "member", userID, map[string]any{"workflow": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListWorkflowVersions(w http.ResponseWriter, r *http.Request) {
	_, _, wsUUID, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return
	}
	workflowID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetWorkflowInWorkspace(r.Context(), db.GetWorkflowInWorkspaceParams{
		ID: workflowID, WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	rows, err := h.Queries.ListWorkflowVersions(r.Context(), db.ListWorkflowVersionsParams{
		WorkflowID: workflowID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflow versions")
		return
	}
	resp := make([]WorkflowVersionResponse, 0, len(rows))
	for _, row := range rows {
		stages, loadErr := h.Queries.ListWorkflowStages(r.Context(), db.ListWorkflowStagesParams{
			WorkflowVersionID: row.ID, WorkspaceID: wsUUID,
		})
		if loadErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to load workflow stages")
			return
		}
		resp = append(resp, workflowVersionToResponse(row, stages))
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": resp, "total": len(resp)})
}

func (h *Handler) CreateWorkflowVersion(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, wsUUID, userUUID, ok := h.requireWorkflowAdmin(w, r)
	if !ok {
		return
	}
	workflowID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	var req CreateWorkflowVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	stages, err := normalizeWorkflowStages(req.Stages)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	workflow, err := qtx.LockWorkflowForVersion(r.Context(), db.LockWorkflowForVersionParams{
		ID: workflowID, WorkspaceID: wsUUID,
	})
	if err != nil || workflow.Status == "archived" {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	next, err := qtx.NextWorkflowVersionNumber(r.Context(), db.NextWorkflowVersionNumberParams{
		WorkflowID: workflowID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to allocate workflow version")
		return
	}
	version, err := qtx.CreateWorkflowVersion(r.Context(), db.CreateWorkflowVersionParams{
		WorkspaceID: wsUUID, WorkflowID: workflowID, Version: next,
		Status: "draft", CreatedBy: userUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workflow version")
		return
	}
	stageRows, err := createWorkflowStages(r.Context(), qtx, wsUUID, version.ID, stages)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workflow stages")
		return
	}
	if req.Publish {
		if err := qtx.SupersedeOtherWorkflowVersions(r.Context(), db.SupersedeOtherWorkflowVersionsParams{
			WorkflowID: workflowID, WorkspaceID: wsUUID, ID: version.ID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to supersede workflow version")
			return
		}
		version, err = qtx.PublishWorkflowVersion(r.Context(), db.PublishWorkflowVersionParams{
			ID: version.ID, WorkflowID: workflowID, WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to publish workflow version")
			return
		}
		if _, err = qtx.SetWorkflowCurrentVersion(r.Context(), db.SetWorkflowCurrentVersionParams{
			ID: workflowID, WorkspaceID: wsUUID, CurrentVersionID: version.ID, Status: "published",
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to activate workflow version")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit workflow version")
		return
	}
	resp := workflowVersionToResponse(version, stageRows)
	h.publish(protocol.EventWorkflowUpdated, workspaceID, "member", userID, map[string]any{
		"workflow_id": uuidToString(workflowID), "version": resp,
	})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) PublishWorkflowVersion(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, wsUUID, _, ok := h.requireWorkflowAdmin(w, r)
	if !ok {
		return
	}
	workflowID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	versionID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "versionId"), "workflow version id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if _, err := qtx.LockWorkflowForVersion(r.Context(), db.LockWorkflowForVersionParams{
		ID: workflowID, WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	stages, err := qtx.ListWorkflowStages(r.Context(), db.ListWorkflowStagesParams{
		WorkflowVersionID: versionID, WorkspaceID: wsUUID,
	})
	if err != nil || len(stages) == 0 {
		writeError(w, http.StatusBadRequest, "workflow version has no stages")
		return
	}
	version, err := qtx.PublishWorkflowVersion(r.Context(), db.PublishWorkflowVersionParams{
		ID: versionID, WorkflowID: workflowID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow version not found")
		return
	}
	if err := qtx.SupersedeOtherWorkflowVersions(r.Context(), db.SupersedeOtherWorkflowVersionsParams{
		WorkflowID: workflowID, WorkspaceID: wsUUID, ID: versionID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to supersede workflow version")
		return
	}
	if _, err := qtx.SetWorkflowCurrentVersion(r.Context(), db.SetWorkflowCurrentVersionParams{
		ID: workflowID, WorkspaceID: wsUUID, CurrentVersionID: versionID, Status: "published",
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to activate workflow version")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit workflow publication")
		return
	}
	resp := workflowVersionToResponse(version, stages)
	h.publish(protocol.EventWorkflowUpdated, workspaceID, "member", userID, map[string]any{
		"workflow_id": uuidToString(workflowID), "version": resp,
	})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListWorkflowInstances(w http.ResponseWriter, r *http.Request) {
	_, userID, wsUUID, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return
	}
	var workflowID pgtype.UUID
	if value := r.URL.Query().Get("workflow_id"); value != "" {
		workflowID, ok = parseUUIDOrBadRequest(w, value, "workflow_id")
		if !ok {
			return
		}
	}
	var projectID pgtype.UUID
	if value := r.URL.Query().Get("project_id"); value != "" {
		projectID, ok = parseUUIDOrBadRequest(w, value, "project_id")
		if !ok {
			return
		}
	}
	var status pgtype.Text
	if value := r.URL.Query().Get("status"); value != "" {
		if !validWorkflowInstanceStatuses[value] {
			writeError(w, http.StatusBadRequest, "invalid workflow instance status")
			return
		}
		status = pgtype.Text{String: value, Valid: true}
	}
	rows, err := h.Queries.ListWorkflowInstances(r.Context(), db.ListWorkflowInstancesParams{
		WorkspaceID: wsUUID, WorkflowID: workflowID, ProjectID: projectID, Status: status,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflow instances")
		return
	}
	resp := make([]WorkflowInstanceResponse, len(rows))
	for i, row := range rows {
		resp[i] = workflowInstanceToResponse(row)
		if err := h.decorateWorkflowInstanceResponse(r.Context(), r, wsUUID, userID, row, &resp[i]); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load workflow instance plan")
			return
		}
		count, countErr := h.Queries.CountIssuesInWorkflowInstance(r.Context(), db.CountIssuesInWorkflowInstanceParams{
			WorkspaceID: wsUUID, WorkflowInstanceID: row.ID,
		})
		if countErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to count workflow tasks")
			return
		}
		resp[i].TaskCount = count
	}
	writeJSON(w, http.StatusOK, map[string]any{"instances": resp, "total": len(resp)})
}

func (h *Handler) CreateWorkflowInstance(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, wsUUID, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return
	}
	workflowID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	var req CreateWorkflowInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	workflow, err := h.Queries.GetWorkflowInWorkspace(r.Context(), db.GetWorkflowInWorkspaceParams{
		ID: workflowID, WorkspaceID: wsUUID,
	})
	if err != nil || workflow.Status == "archived" {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	projectID := workflow.ProjectID
	if workflow.ProjectID.Valid {
		if req.ProjectID != nil && strings.TrimSpace(*req.ProjectID) != "" {
			requestProjectID, parsed := parseUUIDOrBadRequest(w, *req.ProjectID, "project_id")
			if !parsed {
				return
			}
			if requestProjectID != workflow.ProjectID {
				writeError(w, http.StatusBadRequest, "project_id must match the workflow project")
				return
			}
		}
	} else {
		if req.ProjectID == nil || strings.TrimSpace(*req.ProjectID) == "" {
			writeError(w, http.StatusBadRequest, "project_id is required when starting a Space SOP")
			return
		}
		var parsed bool
		projectID, parsed = parseUUIDOrBadRequest(w, strings.TrimSpace(*req.ProjectID), "project_id")
		if !parsed {
			return
		}
	}
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projectID, WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusBadRequest, "project not found")
		return
	}
	versionID := workflow.CurrentVersionID
	if req.VersionID != nil {
		versionID, ok = parseUUIDOrBadRequest(w, *req.VersionID, "version_id")
		if !ok {
			return
		}
	}
	if !versionID.Valid {
		writeError(w, http.StatusConflict, "workflow has no published version")
		return
	}
	version, err := h.Queries.GetWorkflowVersionInWorkspace(r.Context(), db.GetWorkflowVersionInWorkspaceParams{
		ID: versionID, WorkflowID: workflowID, WorkspaceID: wsUUID,
	})
	if err != nil || version.Status == "draft" {
		writeError(w, http.StatusConflict, "workflow version is not published")
		return
	}
	templateStages, err := h.Queries.ListWorkflowStages(r.Context(), db.ListWorkflowStagesParams{
		WorkflowVersionID: versionID, WorkspaceID: wsUUID,
	})
	if err != nil || len(templateStages) == 0 {
		writeError(w, http.StatusConflict, "workflow version has no stages")
		return
	}
	start := true
	if req.Start != nil {
		start = *req.Start
	}
	status := "draft"
	if start {
		status = "active"
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start workflow instance transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	row, err := qtx.CreateWorkflowInstance(r.Context(), db.CreateWorkflowInstanceParams{
		WorkspaceID: wsUUID, WorkflowID: workflowID, WorkflowVersionID: versionID,
		Title: req.Title, Description: ptrToText(req.Description), Status: status,
		CurrentStageID: pgtype.UUID{}, ProjectID: projectID, CreatedBy: userUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workflow instance")
		return
	}
	instanceStages := make([]db.WorkflowInstanceStage, 0, len(templateStages))
	for _, templateStage := range templateStages {
		required, missing := h.TaskService.ResolveRequiredSkills(r.Context(), wsUUID, templateStage.RequiredSkills)
		if len(missing) > 0 {
			writeError(w, http.StatusConflict, "required SOP skill is unavailable: "+strings.Join(missing, ", "))
			return
		}
		snapshots, marshalErr := json.Marshal(required)
		if marshalErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to snapshot workflow skills")
			return
		}
		instanceStage, createErr := qtx.CreateWorkflowInstanceStage(r.Context(), db.CreateWorkflowInstanceStageParams{
			WorkspaceID: wsUUID, WorkflowInstanceID: row.ID, SourceStageID: templateStage.ID,
			StableKey: templateStage.StableKey, Name: templateStage.Name, Description: templateStage.Description,
			Position: templateStage.Position, CompletionRule: templateStage.CompletionRule,
			InputSpec: templateStage.InputSpec, OutputSpec: templateStage.OutputSpec,
			RequiredSkills: templateStage.RequiredSkills, RequiredSkillSnapshots: snapshots, Gate: templateStage.Gate,
			RollbackStageKey: templateStage.RollbackStageKey, AllowedMcpServerIds: templateStage.AllowedMcpServerIds,
		})
		if createErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to create workflow instance plan")
			return
		}
		instanceStages = append(instanceStages, instanceStage)
	}
	if start {
		row, err = qtx.UpdateWorkflowInstanceState(r.Context(), db.UpdateWorkflowInstanceStateParams{
			ID: row.ID, WorkspaceID: wsUUID, CurrentStageID: instanceStages[0].ID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to start workflow instance")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit workflow instance")
		return
	}
	resp := workflowInstanceToResponse(row)
	resp.Stages = make([]WorkflowInstanceStageResponse, len(instanceStages))
	for i, stage := range instanceStages {
		resp.Stages[i] = workflowInstanceStageToResponse(stage)
	}
	resp.StageCount = int32(len(instanceStages))
	resp.SourceVersion = version.Version
	resp.CanEdit = h.canManageWorkflowInstance(r, userID, projectID)
	if start {
		resp.CurrentStageName = &instanceStages[0].Name
		position := instanceStages[0].Position
		resp.CurrentStageIndex = &position
	}
	h.publish(protocol.EventWorkflowInstanceCreated, workspaceID, "member", userID, map[string]any{"instance": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) GetWorkflowInstance(w http.ResponseWriter, r *http.Request) {
	_, userID, wsUUID, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return
	}
	instanceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow instance id")
	if !ok {
		return
	}
	row, err := h.Queries.GetWorkflowInstanceInWorkspace(r.Context(), db.GetWorkflowInstanceInWorkspaceParams{
		ID: instanceID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow instance not found")
		return
	}
	resp := workflowInstanceToResponse(row)
	stages, err := h.Queries.ListWorkflowInstanceStages(r.Context(), db.ListWorkflowInstanceStagesParams{
		WorkflowInstanceID: row.ID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow stages")
		return
	}
	resp.Stages = make([]WorkflowInstanceStageResponse, len(stages))
	for i, stage := range stages {
		resp.Stages[i] = workflowInstanceStageToResponse(stage)
	}
	if err := h.decorateWorkflowInstanceResponse(r.Context(), r, wsUUID, userID, row, &resp); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow instance plan")
		return
	}
	tasks, err := h.Queries.ListIssuesForWorkflowInstance(r.Context(), db.ListIssuesForWorkflowInstanceParams{
		WorkspaceID: wsUUID, WorkflowInstanceID: instanceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow tasks")
		return
	}
	workspace, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workspace")
		return
	}
	resp.Tasks = make([]IssueResponse, len(tasks))
	resp.TaskCount = int64(len(tasks))
	for i, task := range tasks {
		resp.Tasks[i] = issueToResponse(task, workspace.IssuePrefix)
	}
	decisions, err := h.Queries.ListWorkflowGateDecisions(r.Context(), db.ListWorkflowGateDecisionsParams{
		WorkflowInstanceID: instanceID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow decisions")
		return
	}
	resp.Decisions = make([]WorkflowGateDecisionResponse, len(decisions))
	for i, decision := range decisions {
		resp.Decisions[i] = workflowDecisionToResponse(decision)
	}
	changes, err := h.Queries.ListWorkflowInstanceChanges(r.Context(), db.ListWorkflowInstanceChangesParams{
		WorkflowInstanceID: instanceID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow changes")
		return
	}
	resp.Changes = make([]WorkflowInstanceChangeResponse, len(changes))
	for i, change := range changes {
		resp.Changes[i] = workflowInstanceChangeToResponse(change)
	}
	writeJSON(w, http.StatusOK, resp)
}

type workflowGateConfig struct {
	Type         string `json:"type"`
	DeciderType  string `json:"decider_type"`
	Decider      string `json:"decider"`
	RequireHuman bool   `json:"require_human"`
	HumanDecider string `json:"human_decider"`
}

func parseWorkflowGate(raw []byte) workflowGateConfig {
	var gate workflowGateConfig
	if json.Unmarshal(raw, &gate) != nil || gate.Type == "" {
		gate.Type = "none"
	}
	return gate
}

func validateWorkflowGateActor(gate workflowGateConfig, actorType, actorID string) error {
	switch gate.Type {
	case "human", "hybrid":
		if actorType != "member" {
			return errors.New("this stage requires a human decision")
		}
		expected := gate.HumanDecider
		if expected == "" && gate.Type == "human" {
			expected = gate.Decider
		}
		if expected != "" && expected != actorID {
			return errors.New("only the configured human decider can review this stage")
		}
	case "agent":
		if actorType != "agent" {
			return errors.New("this stage requires an agent decision")
		}
		if gate.Decider != "" && gate.Decider != "@self" && gate.Decider != actorID {
			return errors.New("only the configured agent can review this stage")
		}
	}
	return nil
}

func (h *Handler) TransitionWorkflowInstance(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, wsUUID, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return
	}
	instanceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow instance id")
	if !ok {
		return
	}
	var req TransitionWorkflowInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Outcome == "" {
		req.Outcome = "approved"
	}
	if req.Outcome != "approved" && req.Outcome != "rejected" {
		writeError(w, http.StatusBadRequest, "outcome must be approved or rejected")
		return
	}
	instance, err := h.Queries.GetWorkflowInstanceInWorkspace(r.Context(), db.GetWorkflowInstanceInWorkspaceParams{
		ID: instanceID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow instance not found")
		return
	}
	if instance.Status != "active" && instance.Status != "waiting" {
		writeError(w, http.StatusConflict, "workflow instance is not active")
		return
	}
	if instance.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "archived workflow instances cannot be transitioned")
		return
	}
	if !instance.CurrentStageID.Valid {
		writeError(w, http.StatusConflict, "workflow instance has no current stage")
		return
	}
	current, err := h.Queries.GetWorkflowInstanceStage(r.Context(), db.GetWorkflowInstanceStageParams{
		ID: instance.CurrentStageID, WorkflowInstanceID: instance.ID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusConflict, "current workflow stage is invalid")
		return
	}
	openTasks, err := h.Queries.CountOpenIssuesInWorkflowStage(r.Context(), db.CountOpenIssuesInWorkflowStageParams{
		WorkspaceID: wsUUID, WorkflowInstanceID: instanceID, WorkflowStageID: current.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate workflow stage")
		return
	}
	if openTasks > 0 && req.Outcome == "approved" {
		writeError(w, http.StatusConflict, fmt.Sprintf("%d task(s) in the current stage are not complete", openTasks))
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	gate := parseWorkflowGate(current.Gate)
	if err := validateWorkflowGateActor(gate, actorType, actorID); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	actorUUID, err := util.ParseUUID(actorID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid actor id")
		return
	}

	var target db.WorkflowInstanceStage
	var targetID pgtype.UUID
	completing := false
	if req.Outcome == "rejected" {
		if req.TargetStageID != nil {
			targetID, ok = parseUUIDOrBadRequest(w, *req.TargetStageID, "target_stage_id")
			if !ok {
				return
			}
			target, err = h.Queries.GetWorkflowInstanceStage(r.Context(), db.GetWorkflowInstanceStageParams{
				ID: targetID, WorkflowInstanceID: instance.ID, WorkspaceID: wsUUID,
			})
			if err != nil || current.RollbackStageKey.String != target.StableKey {
				writeError(w, http.StatusBadRequest, "target stage is not the configured rollback stage")
				return
			}
		}
	} else if req.TargetStageID != nil {
		targetID, ok = parseUUIDOrBadRequest(w, *req.TargetStageID, "target_stage_id")
		if !ok {
			return
		}
		target, err = h.Queries.GetWorkflowInstanceStage(r.Context(), db.GetWorkflowInstanceStageParams{
			ID: targetID, WorkflowInstanceID: instance.ID, WorkspaceID: wsUUID,
		})
		if err != nil || target.Position != current.Position+1 {
			writeError(w, http.StatusBadRequest, "target stage must be the next stage")
			return
		}
	} else {
		target, err = h.Queries.GetNextWorkflowInstanceStage(r.Context(), db.GetNextWorkflowInstanceStageParams{
			WorkflowInstanceID: instance.ID, WorkspaceID: wsUUID, Position: current.Position,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			completing = true
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve next workflow stage")
			return
		} else {
			targetID = target.ID
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if gate.Type != "none" || req.Outcome == "rejected" || req.Note != nil {
		if _, err := qtx.CreateWorkflowGateDecision(r.Context(), db.CreateWorkflowGateDecisionParams{
			WorkspaceID: wsUUID, WorkflowInstanceID: instanceID, FromStageID: current.ID,
			ToStageID: targetID, Outcome: req.Outcome, ActorType: actorType,
			ActorID: actorUUID, Note: ptrToText(req.Note),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record workflow decision")
			return
		}
	}
	nextStatus := "active"
	nextStageID := targetID
	if req.Outcome == "rejected" && !targetID.Valid {
		nextStatus = "waiting"
		nextStageID = current.ID
	} else if completing {
		nextStatus = "completed"
		nextStageID = current.ID
	}
	updated, err := qtx.UpdateWorkflowInstanceState(r.Context(), db.UpdateWorkflowInstanceStateParams{
		ID: instanceID, WorkspaceID: wsUUID,
		Status:         pgtype.Text{String: nextStatus, Valid: true},
		CurrentStageID: nextStageID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to transition workflow instance")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit workflow transition")
		return
	}
	resp := workflowInstanceToResponse(updated)
	h.publish(protocol.EventWorkflowInstanceUpdated, workspaceID, actorType, actorID, map[string]any{"instance": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ArchiveWorkflowInstance(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, wsUUID, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return
	}
	instanceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow instance id")
	if !ok {
		return
	}
	instance, err := h.Queries.GetWorkflowInstanceInWorkspace(r.Context(), db.GetWorkflowInstanceInWorkspaceParams{
		ID: instanceID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow instance not found")
		return
	}
	if instance.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "workflow instance is already archived")
		return
	}
	taskCount, err := h.Queries.CountIssuesInWorkflowInstance(r.Context(), db.CountIssuesInWorkflowInstanceParams{
		WorkspaceID: wsUUID, WorkflowInstanceID: instanceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count workflow tasks")
		return
	}
	if taskCount > 0 {
		writeError(w, http.StatusConflict, "workflow instances with tasks cannot be archived")
		return
	}
	archivedBy, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	archived, err := h.Queries.ArchiveWorkflowInstance(r.Context(), db.ArchiveWorkflowInstanceParams{
		ID: instanceID, WorkspaceID: wsUUID, ArchivedBy: archivedBy,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive workflow instance")
		return
	}
	resp := workflowInstanceToResponse(archived)
	resp.TaskCount = taskCount
	h.publish(protocol.EventWorkflowInstanceUpdated, workspaceID, "member", userID, map[string]any{"instance": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListWorkflowTasks(w http.ResponseWriter, r *http.Request) {
	_, _, wsUUID, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return
	}
	instanceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow instance id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetWorkflowInstanceInWorkspace(r.Context(), db.GetWorkflowInstanceInWorkspaceParams{
		ID: instanceID, WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "workflow instance not found")
		return
	}
	rows, err := h.Queries.ListIssuesForWorkflowInstance(r.Context(), db.ListIssuesForWorkflowInstanceParams{
		WorkspaceID: wsUUID, WorkflowInstanceID: instanceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflow tasks")
		return
	}
	workspace, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workspace")
		return
	}
	resp := make([]IssueResponse, len(rows))
	for i, row := range rows {
		resp[i] = issueToResponse(row, workspace.IssuePrefix)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": resp, "total": len(resp)})
}

func (h *Handler) AttachWorkflowTask(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, wsUUID, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return
	}
	instanceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow instance id")
	if !ok {
		return
	}
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task id")
	if !ok {
		return
	}
	var req AttachWorkflowTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	stageID, ok := parseUUIDOrBadRequest(w, req.StageID, "stage_id")
	if !ok {
		return
	}
	instance, err := h.Queries.GetWorkflowInstanceInWorkspace(r.Context(), db.GetWorkflowInstanceInWorkspaceParams{
		ID: instanceID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow instance not found")
		return
	}
	if instance.Status == "completed" || instance.Status == "cancelled" {
		writeError(w, http.StatusConflict, "workflow instance is closed")
		return
	}
	if instance.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "archived workflow instances cannot accept tasks")
		return
	}
	if !instance.ProjectID.Valid {
		writeError(w, http.StatusConflict, "workflow run is not assigned to a project")
		return
	}
	if _, err := h.Queries.GetWorkflowInstanceStage(r.Context(), db.GetWorkflowInstanceStageParams{
		ID: stageID, WorkflowInstanceID: instance.ID, WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusBadRequest, "stage does not belong to this workflow instance")
		return
	}
	task, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID: taskID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if task.ProjectID.Valid && task.ProjectID != instance.ProjectID {
		writeError(w, http.StatusConflict, "task belongs to a different project")
		return
	}
	row, err := h.Queries.AttachIssueToWorkflowStage(r.Context(), db.AttachIssueToWorkflowStageParams{
		ID: taskID, WorkspaceID: wsUUID, WorkflowInstanceID: instanceID,
		WorkflowStageID: stageID, ProjectID: instance.ProjectID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "task not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to attach workflow task")
		}
		return
	}
	workspace, _ := h.Queries.GetWorkspace(r.Context(), wsUUID)
	resp := issueToResponse(row, workspace.IssuePrefix)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventWorkflowTaskChanged, workspaceID, actorType, actorID, map[string]any{
		"instance_id": uuidToString(instanceID), "task": resp,
	})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DetachWorkflowTask(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, wsUUID, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return
	}
	instanceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow instance id")
	if !ok {
		return
	}
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task id")
	if !ok {
		return
	}
	row, err := h.Queries.DetachIssueFromWorkflow(r.Context(), db.DetachIssueFromWorkflowParams{
		ID: taskID, WorkspaceID: wsUUID, WorkflowInstanceID: instanceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow task not found")
		return
	}
	workspace, _ := h.Queries.GetWorkspace(r.Context(), wsUUID)
	resp := issueToResponse(row, workspace.IssuePrefix)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventWorkflowTaskChanged, workspaceID, actorType, actorID, map[string]any{
		"instance_id": uuidToString(instanceID), "task": resp,
	})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) logWorkflowError(r *http.Request, action string, err error) {
	slog.Error("workflow "+action+" failed", append(logger.RequestAttrs(r), "error", err)...)
}
