package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/silieco-ai/silieco/core/internal/service"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
)

// CorrectWorkflowExecutionInputs is a member action with explicit old/new input
// documents. A conflict must never trigger an automatic refresh-and-resubmit.
func (h *Handler) CorrectWorkflowExecutionInputs(w http.ResponseWriter, r *http.Request) {
	wsID, runID, executionID, actorID, ok := h.requireWorkflowHumanReview(w, r)
	if !ok {
		return
	}
	var request struct {
		ExpectedInputs json.RawMessage `json:"expected_inputs"`
		Inputs         json.RawMessage `json:"inputs"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid input correction")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "expected one input correction")
		return
	}
	for _, value := range []json.RawMessage{request.ExpectedInputs, request.Inputs} {
		if err := workflowcontract.CheckRunInputCorrection(nil, value, value); err != nil {
			writeError(w, http.StatusBadRequest, "inputs and expected_inputs must be valid JSON objects")
			return
		}
	}
	svc := service.WorkflowExecutionService{Queries: h.Queries, TxStarter: h.TxStarter}
	execution, err := svc.CorrectBlockedExecutionInputs(r.Context(), wsID, runID, executionID, actorID, request.ExpectedInputs, request.Inputs)
	if err != nil {
		writeWorkflowReviewError(w, err)
		return
	}
	h.publish(protocol.EventWorkflowInstanceUpdated, uuidToString(wsID), "member", uuidToString(actorID), map[string]any{"instance_id": uuidToString(runID)})
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"execution_id": uuidToString(execution.ID), "instance_id": uuidToString(runID), "attempt": execution.Attempt, "status": execution.Status})
}
