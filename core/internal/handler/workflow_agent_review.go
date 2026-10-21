package handler

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/service"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func (h *Handler) requireWorkflowAgentReview(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, pgtype.UUID, pgtype.UUID, bool) {
	empty := pgtype.UUID{}
	workspace, user, ws, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return empty, empty, empty, empty, false
	}
	actorType, actor := h.resolveActor(r, user, workspace)
	if actorType != "agent" || r.Header.Get("X-Agent-ID") == "" || r.Header.Get("X-Task-ID") == "" {
		writeError(w, http.StatusForbidden, "a bound reviewer call is required")
		return empty, empty, empty, empty, false
	}
	actorID, ok := parseUUIDOrBadRequest(w, actor, "reviewer id")
	if !ok {
		return empty, empty, empty, empty, false
	}
	taskID, ok := parseUUIDOrBadRequest(w, r.Header.Get("X-Task-ID"), "review call id")
	if !ok {
		return empty, empty, empty, empty, false
	}
	requestID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "requestId"), "review request id")
	if !ok {
		return empty, empty, empty, empty, false
	}
	runID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "Run id")
	if !ok {
		return empty, empty, empty, empty, false
	}
	executionID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "executionId"), "execution id")
	if !ok {
		return empty, empty, empty, empty, false
	}
	request, err := h.Queries.GetWorkflowReviewRequest(r.Context(), db.GetWorkflowReviewRequestParams{WorkspaceID: ws, ID: requestID})
	if err != nil {
		writeWorkflowReviewError(w, err)
		return empty, empty, empty, empty, false
	}
	if request.WorkflowInstanceID != runID || request.ExecutionID != executionID {
		writeError(w, http.StatusNotFound, "review request not found")
		return empty, empty, empty, empty, false
	}
	return ws, requestID, actorID, taskID, true
}

func (h *Handler) GetWorkflowAgentReview(w http.ResponseWriter, r *http.Request) {
	ws, requestID, actorID, taskID, ok := h.requireWorkflowAgentReview(w, r)
	if !ok {
		return
	}
	svc := service.WorkflowExecutionService{Queries: h.Queries, TxStarter: h.TxStarter}
	request, err := svc.GetAgentReview(r.Context(), ws, requestID, actorID, taskID)
	if err != nil {
		writeWorkflowReviewError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"id": uuidToString(request.ID), "instance_id": uuidToString(request.WorkflowInstanceID), "execution_id": uuidToString(request.ExecutionID), "reviewer_id": uuidToString(request.ReviewerID), "result_id": uuidToString(request.ResultID), "result_revision": request.ResultRevision, "basis_hash": request.BasisHash, "status": request.Status})
}

func (h *Handler) RecordWorkflowAgentDecision(w http.ResponseWriter, r *http.Request) {
	ws, requestID, actorID, taskID, ok := h.requireWorkflowAgentReview(w, r)
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
		writeError(w, http.StatusBadRequest, "invalid review decision")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "expected one review decision")
		return
	}
	hash, err := hex.DecodeString(request.ExpectedBasisHash)
	if err != nil || len(hash) != 32 || strings.ToLower(request.ExpectedBasisHash) != request.ExpectedBasisHash || request.ExpectedResultRevision < 1 || (request.Outcome != "approved" && request.Outcome != "rejected") {
		writeError(w, http.StatusBadRequest, "exact result revision, basis hash and outcome are required")
		return
	}
	svc := service.WorkflowExecutionService{Queries: h.Queries, TxStarter: h.TxStarter}
	decision, err := svc.RecordAgentDecision(r.Context(), service.RecordWorkflowAgentDecisionParams{WorkspaceID: ws, RequestID: requestID, ActorID: actorID, TaskID: taskID, ExpectedResultRevision: request.ExpectedResultRevision, ExpectedBasisHash: request.ExpectedBasisHash, Outcome: request.Outcome, Note: request.Note})
	if err != nil {
		writeWorkflowReviewError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, workflowDecisionToResponse(decision))
}
