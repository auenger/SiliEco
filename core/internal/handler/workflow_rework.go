package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/silieco-ai/silieco/core/internal/service"
)

// ReworkWorkflowExecution requires an explicit source attempt and target. A
// conflict requires another user review, never refresh-and-resubmit behavior.
func (h *Handler) ReworkWorkflowExecution(w http.ResponseWriter, r *http.Request) {
	wsID, runID, sourceID, actorID, ok := h.requireWorkflowHumanReview(w, r)
	if !ok {
		return
	}
	var request struct {
		ExpectedRevision int32  `json:"expected_revision"`
		TargetStageID    string `json:"target_stage_id"`
		Reason           string `json:"reason"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid rework request")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "expected one rework request")
		return
	}
	if request.ExpectedRevision < 1 || strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 4000 {
		writeError(w, http.StatusBadRequest, "positive expected_revision and a reason of at most 4000 bytes are required")
		return
	}
	targetID, ok := parseUUIDOrBadRequest(w, request.TargetStageID, "target_stage_id")
	if !ok {
		return
	}
	svc := service.WorkflowExecutionService{Queries: h.Queries, TxStarter: h.TxStarter, TaskService: h.TaskService}
	result, err := svc.ReworkExecution(r.Context(), service.ReworkWorkflowExecutionParams{WorkspaceID: wsID, InstanceID: runID, SourceExecutionID: sourceID, TargetStageID: targetID, ActorID: actorID, ExpectedRevision: request.ExpectedRevision, Reason: request.Reason})
	if err != nil {
		writeWorkflowReviewError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	response := map[string]any{"rework_id": uuidToString(result.Record.ID), "execution_id": nil, "source_execution_id": uuidToString(sourceID), "instance_id": uuidToString(runID), "stage_id": uuidToString(result.Record.TargetStageID), "attempt": nil, "status": "awaiting_work"}
	if execution := result.Execution; execution != nil {
		response["execution_id"] = uuidToString(execution.ID)
		response["attempt"] = execution.Attempt
		response["status"] = execution.Status
	}
	writeJSON(w, http.StatusOK, response)
}
