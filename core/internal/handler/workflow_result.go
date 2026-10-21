package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/silieco-ai/silieco/core/internal/service"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

type SubmitWorkflowResultRequest struct {
	ExpectedRevision *int32          `json:"expected_revision"`
	Submission       json.RawMessage `json:"submission"`
}

type WorkflowResultResponse struct {
	ID              string                           `json:"id"`
	ExecutionID     string                           `json:"execution_id"`
	Revision        int32                            `json:"revision"`
	Data            json.RawMessage                  `json:"data"`
	SubmittedByType string                           `json:"submitted_by_type"`
	SubmittedByID   string                           `json:"submitted_by_id"`
	Artifacts       []WorkflowResultArtifactResponse `json:"artifacts"`
}

func workflowResultResponse(result db.WorkflowExecutionResult) WorkflowResultResponse {
	return WorkflowResultResponse{ID: uuidToString(result.ID), ExecutionID: uuidToString(result.ExecutionID), Revision: result.Revision, Data: json.RawMessage(result.Data), SubmittedByType: result.SubmittedByType, SubmittedByID: uuidToString(result.SubmittedByID), Artifacts: []WorkflowResultArtifactResponse{}}
}

func (h *Handler) SubmitWorkflowResult(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, wsID, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return
	}
	instanceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow instance id")
	if !ok {
		return
	}
	executionID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "executionId"), "execution id")
	if !ok {
		return
	}
	var request SubmitWorkflowResultRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid result request")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "expected one result request")
		return
	}
	if request.ExpectedRevision == nil || *request.ExpectedRevision < 0 {
		writeError(w, http.StatusBadRequest, "expected_revision is required and must be nonnegative")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	// Agent-intended writes must never silently become member submissions.
	if r.Header.Get("X-Agent-ID") != "" && actorType != "agent" {
		writeError(w, http.StatusForbidden, "invalid agent task identity")
		return
	}
	actorUUID, ok := parseUUIDOrBadRequest(w, actorID, "actor id")
	if !ok {
		return
	}
	params := service.SubmitWorkflowResultParams{WorkspaceID: wsID, InstanceID: instanceID, ExecutionID: executionID, ActorType: actorType, ActorID: actorUUID, ExpectedRevision: *request.ExpectedRevision, Submission: request.Submission}
	if actorType == "agent" {
		taskID, ok := parseUUIDOrBadRequest(w, r.Header.Get("X-Task-ID"), "source task id")
		if !ok {
			return
		}
		params.SourceTaskID = taskID
	}
	svc := service.WorkflowExecutionService{Queries: h.Queries, TxStarter: h.TxStarter, Storage: h.Storage}
	result, err := svc.SubmitResult(r.Context(), params)
	if err != nil {
		var validation *service.WorkflowResultValidationError
		switch {
		case errors.As(err, &validation):
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": validation.Error(), "violations": validation.Violations})
		case errors.Is(err, service.ErrWorkflowResultAccess):
			writeError(w, http.StatusForbidden, "result submission is not authorized")
		case errors.Is(err, service.ErrWorkflowExecutionConflict):
			writeError(w, http.StatusConflict, "execution or result revision changed")
		case errors.Is(err, pgx.ErrNoRows):
			writeError(w, http.StatusNotFound, "workflow execution not found")
		default:
			writeError(w, http.StatusInternalServerError, "failed to save workflow result")
		}
		return
	}
	h.writeWorkflowResult(w, r, http.StatusCreated, result)
}

func (h *Handler) GetWorkflowResult(w http.ResponseWriter, r *http.Request) {
	_, _, wsID, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return
	}
	instanceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow instance id")
	if !ok {
		return
	}
	executionID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "executionId"), "execution id")
	if !ok {
		return
	}
	revision, err := strconv.ParseInt(chi.URLParam(r, "revision"), 10, 32)
	if err != nil || revision < 1 {
		writeError(w, http.StatusBadRequest, "invalid result revision")
		return
	}
	execution, err := h.Queries.GetWorkflowStageExecution(r.Context(), db.GetWorkflowStageExecutionParams{WorkspaceID: wsID, ID: executionID})
	if err != nil || execution.WorkflowInstanceID != instanceID {
		writeError(w, http.StatusNotFound, "workflow execution not found")
		return
	}
	result, err := h.Queries.GetWorkflowExecutionResult(r.Context(), db.GetWorkflowExecutionResultParams{WorkspaceID: wsID, ExecutionID: executionID, Revision: int32(revision)})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow result not found")
		return
	}
	h.writeWorkflowResult(w, r, http.StatusOK, result)
}

func (h *Handler) ListWorkflowExecutions(w http.ResponseWriter, r *http.Request) {
	_, _, wsID, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return
	}
	instanceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow instance id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetWorkflowInstanceInWorkspace(r.Context(), db.GetWorkflowInstanceInWorkspaceParams{WorkspaceID: wsID, ID: instanceID}); err != nil {
		writeError(w, http.StatusNotFound, "workflow instance not found")
		return
	}
	executions, err := h.Queries.ListWorkflowStageExecutions(r.Context(), db.ListWorkflowStageExecutionsParams{WorkspaceID: wsID, WorkflowInstanceID: instanceID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workflow executions")
		return
	}
	items := make([]map[string]any, 0, len(executions))
	for _, execution := range executions {
		items = append(items, map[string]any{"id": uuidToString(execution.ID), "stage_id": uuidToString(execution.WorkflowStageID), "attempt": execution.Attempt, "plan_revision": execution.PlanRevision, "executor_id": uuidToString(execution.ExecutorID), "main_issue_id": uuidToPtr(execution.MainIssueID), "main_task_id": uuidToPtr(execution.MainTaskID), "status": execution.Status, "failure_code": textToPtr(execution.FailureCode), "failure_detail": textToPtr(execution.FailureDetail), "result_revision": execution.ResultRevision, "config_snapshot": json.RawMessage(execution.ConfigSnapshot), "input_snapshot": json.RawMessage(execution.InputSnapshot)})
	}
	writeJSON(w, http.StatusOK, items)
}

type WorkflowResultArtifactResponse struct {
	ID            string `json:"id"`
	ArtifactID    string `json:"artifact_id"`
	Revision      int32  `json:"revision"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	MediaType     string `json:"media_type"`
	SchemaVersion string `json:"schema_version"`
	Checksum      string `json:"checksum"`
	SizeBytes     int64  `json:"size_bytes"`
	ContentURL    string `json:"content_url"`
}

func (h *Handler) writeWorkflowResult(w http.ResponseWriter, r *http.Request, status int, result db.WorkflowExecutionResult) {
	rows, err := h.Queries.ListWorkflowResultArtifacts(r.Context(), db.ListWorkflowResultArtifactsParams{WorkspaceID: result.WorkspaceID, ResultID: result.ID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read result artifacts")
		return
	}
	response := workflowResultResponse(result)
	for _, row := range rows {
		response.Artifacts = append(response.Artifacts, WorkflowResultArtifactResponse{ID: uuidToString(row.ID), ArtifactID: uuidToString(row.ArtifactID), Revision: row.Revision, Name: row.Name, Kind: row.Kind, MediaType: row.MediaType, SchemaVersion: row.SchemaVersion, Checksum: row.Checksum, SizeBytes: row.SizeBytes, ContentURL: fmt.Sprintf("/api/workflow-instances/%s/executions/%s/results/%d/artifacts/%s", uuidToString(result.WorkflowInstanceID), uuidToString(result.ExecutionID), result.Revision, uuidToString(row.ID))})
	}
	writeJSON(w, status, response)
}

// GetWorkflowResultArtifact serves the frozen revision, never the original
// attachment URL. Buffering is bounded and permits digest verification before
// any unverified bytes are sent to a reviewer or downstream stage.
func (h *Handler) GetWorkflowResultArtifact(w http.ResponseWriter, r *http.Request) {
	_, _, wsID, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return
	}
	instanceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow instance id")
	if !ok {
		return
	}
	executionID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "executionId"), "execution id")
	if !ok {
		return
	}
	artifactID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "artifactRevisionId"), "artifact revision id")
	if !ok {
		return
	}
	revision, err := strconv.ParseInt(chi.URLParam(r, "revision"), 10, 32)
	if err != nil || revision < 1 {
		writeError(w, http.StatusBadRequest, "invalid result revision")
		return
	}
	artifact, err := h.Queries.GetWorkflowResultArtifact(r.Context(), db.GetWorkflowResultArtifactParams{WorkspaceID: wsID, InstanceID: instanceID, ExecutionID: executionID, ResultRevision: int32(revision), ArtifactRevisionID: artifactID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "result artifact not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read result artifact")
		return
	}
	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "artifact storage unavailable")
		return
	}
	if artifact.SizeBytes < 1 || artifact.SizeBytes > workflowcontract.MaxArtifactBytes {
		writeError(w, http.StatusConflict, "invalid stored artifact size")
		return
	}
	reader, err := h.Storage.GetReader(r.Context(), artifact.StorageKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "artifact storage read failed")
		return
	}
	content, readErr := io.ReadAll(io.LimitReader(reader, workflowcontract.MaxArtifactBytes+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		writeError(w, http.StatusBadGateway, "artifact storage read failed")
		return
	}
	if int64(len(content)) != artifact.SizeBytes || workflowcontract.Checksum(content) != artifact.Checksum {
		writeError(w, http.StatusConflict, "stored artifact integrity check failed")
		return
	}
	w.Header().Set("Content-Type", artifact.MediaType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": artifact.Name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (h *Handler) GetWorkflowExecutionReadiness(w http.ResponseWriter, r *http.Request) {
	_, _, workspaceID, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return
	}
	instanceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow instance id")
	if !ok {
		return
	}
	executionID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "executionId"), "execution id")
	if !ok {
		return
	}
	svc := service.WorkflowExecutionService{Queries: h.Queries}
	readiness, err := svc.InspectReadiness(r.Context(), workspaceID, instanceID, executionID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "workflow execution not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect workflow completion")
		return
	}
	writeJSON(w, http.StatusOK, readiness)
}
