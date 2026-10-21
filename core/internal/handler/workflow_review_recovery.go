package handler

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/silieco-ai/silieco/core/internal/service"
)

type WorkflowReviewStatusCallResponse struct {
	ID            string  `json:"id"`
	Status        string  `json:"status"`
	FailureReason *string `json:"failure_reason"`
	Attempt       int32   `json:"attempt"`
	MaxAttempts   int32   `json:"max_attempts"`
}

type WorkflowReviewStatusResponse struct {
	ExecutionID      string                            `json:"execution_id"`
	Phase            string                            `json:"phase"`
	RequestID        *string                           `json:"request_id"`
	ReviewerID       *string                           `json:"reviewer_id"`
	ResultRevision   int32                             `json:"result_revision"`
	BasisHash        *string                           `json:"basis_hash"`
	Call             *WorkflowReviewStatusCallResponse `json:"call"`
	CanRecover       bool                              `json:"can_recover"`
	RecoveryAttempts int32                             `json:"recovery_attempts"`
	RetryAt          *string                           `json:"retry_at"`
}

func workflowReviewStatusToResponse(status service.WorkflowAgentReviewStatus) WorkflowReviewStatusResponse {
	response := WorkflowReviewStatusResponse{ExecutionID: uuidToString(status.ExecutionID), Phase: status.Phase, CanRecover: status.CanRecover, RecoveryAttempts: status.RecoveryAttempts}
	if status.Request != nil {
		response.RequestID = uuidToPtr(status.Request.ID)
		response.ReviewerID = uuidToPtr(status.Request.ReviewerID)
		response.ResultRevision = status.Request.ResultRevision
		response.BasisHash = &status.Request.BasisHash
	}
	if status.Task != nil {
		response.Call = &WorkflowReviewStatusCallResponse{ID: uuidToString(status.Task.ID), Status: status.Task.Status, FailureReason: textToPtr(status.Task.FailureReason), Attempt: status.Task.Attempt, MaxAttempts: status.Task.MaxAttempts}
	}
	if status.RetryAt != nil {
		formatted := status.RetryAt.UTC().Format(time.RFC3339Nano)
		response.RetryAt = &formatted
	}
	return response
}

func (h *Handler) GetWorkflowReviewStatus(w http.ResponseWriter, r *http.Request) {
	wsID, runID, executionID, actorID, ok := h.requireWorkflowHumanReview(w, r)
	if !ok {
		return
	}
	svc := service.WorkflowExecutionService{Queries: h.Queries, TxStarter: h.TxStarter}
	status, err := svc.InspectAgentReviewStatus(r.Context(), wsID, runID, executionID, actorID)
	if err != nil {
		writeWorkflowReviewError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, workflowReviewStatusToResponse(status))
}

func (h *Handler) RecoverWorkflowAgentReview(w http.ResponseWriter, r *http.Request) {
	wsID, runID, executionID, actorID, ok := h.requireWorkflowHumanReview(w, r)
	if !ok {
		return
	}
	requestID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "requestId"), "review request id")
	if !ok {
		return
	}
	var request struct {
		FailedTaskID           string `json:"failed_task_id"`
		ExpectedResultRevision int32  `json:"expected_result_revision"`
		ExpectedBasisHash      string `json:"expected_basis_hash"`
		Reason                 string `json:"reason"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid recovery request")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "unexpected recovery request content")
		return
	}
	failedTaskID, ok := parseUUIDOrBadRequest(w, request.FailedTaskID, "failed_task_id")
	if !ok {
		return
	}
	if request.ExpectedResultRevision < 1 || len(request.ExpectedBasisHash) != 64 || strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 4000 {
		writeError(w, http.StatusBadRequest, "review identity and a reason of 1 to 4000 bytes are required")
		return
	}
	if _, err := hex.DecodeString(request.ExpectedBasisHash); err != nil {
		writeError(w, http.StatusBadRequest, "invalid review basis hash")
		return
	}
	svc := service.WorkflowExecutionService{Queries: h.Queries, TxStarter: h.TxStarter, TaskService: h.TaskService}
	recovered, err := svc.RecoverAgentReview(r.Context(), service.RecoverWorkflowAgentReviewParams{WorkspaceID: wsID, InstanceID: runID, ExecutionID: executionID, RequestID: requestID, FailedTaskID: failedTaskID, ActorID: actorID, ExpectedResultRevision: request.ExpectedResultRevision, ExpectedBasisHash: request.ExpectedBasisHash, Reason: request.Reason})
	if err != nil {
		writeWorkflowReviewError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recovery_id": uuidToString(recovered.ID), "instance_id": uuidToString(recovered.WorkflowInstanceID), "execution_id": uuidToString(recovered.ExecutionID), "request_id": uuidToString(recovered.RequestID), "failed_task_id": uuidToString(recovered.FailedTaskID), "task_id": uuidToString(recovered.NewTaskID), "allowed_attempts": recovered.AllowedAttempts, "max_attempts": recovered.MaxAttempts})
}
