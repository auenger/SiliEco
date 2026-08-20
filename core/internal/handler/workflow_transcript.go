package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

type WorkflowStageRunResponse struct {
	ID                     string          `json:"id"`
	TaskID                 string          `json:"task_id"`
	TaskTitle              string          `json:"task_title"`
	AgentID                string          `json:"agent_id"`
	AgentName              *string         `json:"agent_name"`
	Status                 string          `json:"status"`
	TriggerSummary         *string         `json:"trigger_summary"`
	TriggerSource          *string         `json:"trigger_source"`
	TriggerUserID          *string         `json:"trigger_user_id"`
	AccountableUserID      *string         `json:"accountable_user_id"`
	StartedAt              *string         `json:"started_at"`
	CompletedAt            *string         `json:"completed_at"`
	Result                 json.RawMessage `json:"result,omitempty"`
	Error                  *string         `json:"error"`
	FailureReason          *string         `json:"failure_reason"`
	ExecutionManifest      json.RawMessage `json:"execution_manifest,omitempty"`
	CreatedAt              string          `json:"created_at"`
	MessagesPath           string          `json:"messages_path"`
	WorktreeBranch         *string         `json:"worktree_branch"`
	WorktreeBaseSHA        *string         `json:"worktree_base_sha"`
	WorktreeFinalSHA       *string         `json:"worktree_final_sha"`
	WorktreeDeliveryStatus *string         `json:"worktree_delivery_status"`
}

func transcriptPageValue(raw string, fallback, max int) int32 {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		value = fallback
	}
	if value > max {
		value = max
	}
	return int32(value)
}

func (h *Handler) ListWorkflowStageTranscript(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	if _, ok := ctxMember(r.Context()); workspaceID == "" || !ok {
		writeError(w, http.StatusUnauthorized, "workspace context is required")
		return
	}
	instanceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow instance id")
	if !ok {
		return
	}
	stageID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "stageId"), "workflow stage id")
	if !ok {
		return
	}
	wsID := parseUUID(workspaceID)
	if _, err := h.Queries.GetWorkflowInstanceInWorkspace(r.Context(), db.GetWorkflowInstanceInWorkspaceParams{ID: instanceID, WorkspaceID: wsID}); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "workflow run not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow run")
		return
	}
	if _, err := h.Queries.GetWorkflowInstanceStage(r.Context(), db.GetWorkflowInstanceStageParams{ID: stageID, WorkflowInstanceID: instanceID, WorkspaceID: wsID}); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "workflow stage not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow stage")
		return
	}
	limit := transcriptPageValue(r.URL.Query().Get("limit"), 25, 100)
	offset := transcriptPageValue(r.URL.Query().Get("offset"), 0, 1000000)
	rows, err := h.Queries.ListWorkflowStageRuns(r.Context(), db.ListWorkflowStageRunsParams{WorkspaceID: wsID, WorkflowInstanceID: instanceID, WorkflowStageID: stageID, Limit: limit, Offset: offset})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load stage transcript")
		return
	}
	total, err := h.Queries.CountWorkflowStageRuns(r.Context(), db.CountWorkflowStageRunsParams{WorkspaceID: wsID, WorkflowInstanceID: instanceID, WorkflowStageID: stageID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count stage transcript")
		return
	}
	runs := make([]WorkflowStageRunResponse, len(rows))
	for i, row := range rows {
		result := json.RawMessage(row.Result)
		if len(result) == 0 {
			result = nil
		}
		manifest := json.RawMessage(row.ExecutionManifest)
		if len(manifest) == 0 {
			manifest = nil
		}
		runs[i] = WorkflowStageRunResponse{ID: uuidToString(row.ID), TaskID: uuidToString(row.IssueID), TaskTitle: row.TaskTitle,
			AgentID: uuidToString(row.AgentID), AgentName: textToPtr(row.AgentName), Status: row.Status, TriggerSummary: textToPtr(row.TriggerSummary),
			TriggerSource: textToPtr(row.OriginatorSource), TriggerUserID: uuidToPtr(row.OriginatorUserID), AccountableUserID: uuidToPtr(row.AccountableUserID),
			StartedAt: timestampToPtr(row.StartedAt), CompletedAt: timestampToPtr(row.CompletedAt), Result: result, Error: textToPtr(row.Error), FailureReason: textToPtr(row.FailureReason),
			ExecutionManifest: manifest, CreatedAt: timestampToString(row.CreatedAt), MessagesPath: "/api/tasks/" + uuidToString(row.ID) + "/messages",
			WorktreeBranch: textToPtr(row.WorktreeBranch), WorktreeBaseSHA: textToPtr(row.WorktreeBaseSha), WorktreeFinalSHA: textToPtr(row.WorktreeFinalSha),
			WorktreeDeliveryStatus: textToPtr(row.WorktreeDeliveryStatus)}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs, "total": total, "limit": limit, "offset": offset})
}
