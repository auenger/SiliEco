package handler

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/service"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

type WorkflowHumanReviewResponse struct {
	ExecutionID    string                        `json:"execution_id"`
	ResultRevision int32                         `json:"result_revision"`
	BasisHash      string                        `json:"basis_hash"`
	Issues         []WorkflowReviewIssueResponse `json:"issues"`
	Calls          []WorkflowReviewCallResponse  `json:"calls"`
}

type WorkflowReviewIssueResponse struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	UpdatedAt string `json:"updated_at"`
}

type WorkflowReviewCallResponse struct {
	ID      string  `json:"id"`
	IssueID *string `json:"issue_id"`
	AgentID string  `json:"agent_id"`
	Status  string  `json:"status"`
}

// Human review never treats an agent-intended request as a member action.
func (h *Handler) requireWorkflowHumanReview(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, pgtype.UUID, pgtype.UUID, bool) {
	_, userID, wsID, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	if r.Header.Get("X-Agent-ID") != "" || r.Header.Get("X-Task-ID") != "" {
		writeError(w, http.StatusForbidden, "human review requires a member identity")
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	actorID, ok := parseUUIDOrBadRequest(w, userID, "actor id")
	if !ok {
		return wsID, pgtype.UUID{}, pgtype.UUID{}, actorID, false
	}
	runID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow instance id")
	if !ok {
		return wsID, runID, pgtype.UUID{}, actorID, false
	}
	executionID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "executionId"), "execution id")
	return wsID, runID, executionID, actorID, ok
}

func (h *Handler) GetWorkflowHumanReview(w http.ResponseWriter, r *http.Request) {
	wsID, runID, executionID, actorID, ok := h.requireWorkflowHumanReview(w, r)
	if !ok {
		return
	}
	svc := service.WorkflowExecutionService{Queries: h.Queries, TxStarter: h.TxStarter}
	basis, err := svc.PrepareHumanReview(r.Context(), wsID, runID, executionID, actorID)
	if err != nil {
		writeWorkflowReviewError(w, err)
		return
	}
	response := WorkflowHumanReviewResponse{ExecutionID: uuidToString(executionID), ResultRevision: basis.ResultRevision, BasisHash: basis.Hash, Issues: []WorkflowReviewIssueResponse{}, Calls: []WorkflowReviewCallResponse{}}
	for _, issue := range basis.Issues {
		response.Issues = append(response.Issues, WorkflowReviewIssueResponse{ID: uuidToString(issue.ID), Title: issue.Title, Status: issue.Status, UpdatedAt: timestampToString(issue.UpdatedAt)})
	}
	for _, call := range basis.Calls {
		response.Calls = append(response.Calls, WorkflowReviewCallResponse{ID: uuidToString(call.ID), IssueID: uuidToPtr(call.IssueID), AgentID: uuidToString(call.AgentID), Status: call.Status})
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) RecordWorkflowHumanDecision(w http.ResponseWriter, r *http.Request) {
	wsID, runID, executionID, actorID, ok := h.requireWorkflowHumanReview(w, r)
	if !ok {
		return
	}
	var request struct {
		ExpectedResultRevision int32  `json:"expected_result_revision"`
		ExpectedBasisHash      string `json:"expected_basis_hash"`
		Outcome                string `json:"outcome"`
		Note                   string `json:"note"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid review request")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "expected one review request")
		return
	}
	hash, err := hex.DecodeString(request.ExpectedBasisHash)
	if err != nil || len(hash) != 32 || request.ExpectedBasisHash != strings.ToLower(request.ExpectedBasisHash) || request.ExpectedResultRevision < 1 || (request.Outcome != "approved" && request.Outcome != "rejected") {
		writeError(w, http.StatusBadRequest, "result revision, lowercase basis hash and approved/rejected outcome are required")
		return
	}
	svc := service.WorkflowExecutionService{Queries: h.Queries, TxStarter: h.TxStarter}
	decision, err := svc.DecideHumanExecution(r.Context(), service.RecordWorkflowHumanDecisionParams{WorkspaceID: wsID, InstanceID: runID, ExecutionID: executionID, ActorID: actorID, ExpectedResultRevision: request.ExpectedResultRevision, ExpectedBasisHash: request.ExpectedBasisHash, Outcome: request.Outcome, Note: request.Note})
	if err != nil {
		writeWorkflowReviewError(w, err)
		return
	}
	if run, err := h.Queries.GetWorkflowInstanceInWorkspace(r.Context(), db.GetWorkflowInstanceInWorkspaceParams{ID: runID, WorkspaceID: wsID}); err == nil {
		h.publish(protocol.EventWorkflowInstanceUpdated, uuidToString(wsID), "member", uuidToString(actorID), map[string]any{"instance": workflowInstanceToResponse(run)})
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, workflowDecisionToResponse(decision))
}

func writeWorkflowReviewError(w http.ResponseWriter, err error) {
	var validation *service.WorkflowResultValidationError
	switch {
	case errors.Is(err, service.ErrWorkflowReviewAccess):
		writeError(w, http.StatusForbidden, "review is not authorized")
	case errors.Is(err, service.ErrWorkflowExecutionConflict):
		writeError(w, http.StatusConflict, "review evidence changed or delivery is incomplete")
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "workflow execution not found")
	case errors.As(err, &validation):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": validation.Error(), "violations": validation.Violations})
	default:
		writeError(w, http.StatusInternalServerError, "failed to process workflow review")
	}
}
