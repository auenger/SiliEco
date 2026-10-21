package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

type UpdateWorkflowInstancePlanRequest struct {
	ExpectedRevision int32                `json:"expected_revision"`
	Title            string               `json:"title"`
	Description      *string              `json:"description"`
	ChangeNote       *string              `json:"change_note"`
	Stages           []WorkflowStageInput `json:"stages"`
}

type workflowInstancePlanSnapshot struct {
	Title       string                          `json:"title"`
	Description *string                         `json:"description"`
	Stages      []WorkflowInstanceStageResponse `json:"stages"`
}

func (h *Handler) canManageWorkflowInstance(r *http.Request, userID string, projectID pgtype.UUID) bool {
	if !projectID.Valid || r.Header.Get("X-Actor-Source") == "task_token" {
		return false
	}
	member, ok := ctxMember(r.Context())
	if !ok {
		var err error
		member, err = h.getWorkspaceMember(r.Context(), userID, h.resolveWorkspaceID(r))
		if err != nil {
			return false
		}
	}
	if member.Role == "owner" || member.Role == "admin" {
		return true
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projectID, WorkspaceID: member.WorkspaceID,
	})
	if err != nil {
		return false
	}
	return project.LeadType.Valid && project.LeadType.String == "member" &&
		project.LeadID.Valid && project.LeadID == member.UserID
}

func (h *Handler) requireWorkflowInstanceManager(w http.ResponseWriter, r *http.Request, userID string, instance db.WorkflowInstance) bool {
	if h.canManageWorkflowInstance(r, userID, instance.ProjectID) {
		return true
	}
	writeError(w, http.StatusForbidden, "only workspace admins or the project lead can adjust this SOP run")
	return false
}

func (h *Handler) decorateWorkflowInstanceResponse(
	ctx context.Context,
	r *http.Request,
	workspaceID pgtype.UUID,
	userID string,
	instance db.WorkflowInstance,
	resp *WorkflowInstanceResponse,
) error {
	stages, err := h.Queries.ListWorkflowInstanceStages(ctx, db.ListWorkflowInstanceStagesParams{
		WorkflowInstanceID: instance.ID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return err
	}
	resp.StageCount = int32(len(stages))
	for _, stage := range stages {
		if instance.CurrentStageID.Valid && stage.ID == instance.CurrentStageID {
			name := stage.Name
			position := stage.Position
			resp.CurrentStageName = &name
			resp.CurrentStageIndex = &position
			break
		}
	}
	version, err := h.Queries.GetWorkflowVersionInWorkspace(ctx, db.GetWorkflowVersionInWorkspaceParams{
		ID: instance.WorkflowVersionID, WorkflowID: instance.WorkflowID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return err
	}
	resp.SourceVersion = version.Version
	resp.CanEdit = (instance.Status == "active" || instance.Status == "waiting") &&
		!instance.ArchivedAt.Valid && h.canManageWorkflowInstance(r, userID, instance.ProjectID)
	if resp.CanEdit {
		executions, err := h.Queries.ListWorkflowStageExecutions(ctx, db.ListWorkflowStageExecutionsParams{WorkspaceID: workspaceID, WorkflowInstanceID: instance.ID})
		if err != nil {
			return err
		}
		resp.CanEdit = len(executions) == 0
	}
	return nil
}

func instanceStageInput(stage db.WorkflowInstanceStage) WorkflowStageInput {
	id := uuidToString(stage.ID)
	return WorkflowStageInput{
		ID: &id, StableKey: stage.StableKey, Name: stage.Name, Description: textToPtr(stage.Description),
		ExecutionMode: stage.ExecutionMode, ExecutorID: uuidToPtr(stage.ExecutorID),
		CompletionRule: jsonOrEmptyObject(stage.CompletionRule), ReworkPolicy: jsonOrEmptyObject(stage.ReworkPolicy), RetryPolicy: jsonOrEmptyObject(stage.RetryPolicy), InputSpec: jsonOrEmptyObject(stage.InputSpec), InputBindings: jsonOrEmptyObject(stage.InputBindings),
		OutputSpec: jsonOrEmptyObject(stage.OutputSpec), RequiredSkills: stage.RequiredSkills,
		AllowedMCPServerIDs: uuidsToStrings(stage.AllowedMcpServerIds),
		Gate:                jsonOrEmptyObject(stage.Gate), RollbackStageKey: textToPtr(stage.RollbackStageKey),
	}
}

func sameWorkflowStageContent(a, b WorkflowStageInput) bool {
	a.ID = nil
	b.ID = nil
	return reflect.DeepEqual(a, b)
}

func workflowPlanJSON(title string, description *string, stages []db.WorkflowInstanceStage) ([]byte, error) {
	responses := make([]WorkflowInstanceStageResponse, len(stages))
	for i, stage := range stages {
		responses[i] = workflowInstanceStageToResponse(stage)
	}
	return json.Marshal(workflowInstancePlanSnapshot{Title: title, Description: description, Stages: responses})
}

func (h *Handler) UpdateWorkflowInstancePlan(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, wsUUID, ok := h.requireWorkflowMember(w, r)
	if !ok {
		return
	}
	instanceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow instance id")
	if !ok {
		return
	}
	var req UpdateWorkflowInstancePlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.ExpectedRevision < 1 {
		writeError(w, http.StatusBadRequest, "expected_revision is required")
		return
	}
	normalized, err := normalizeWorkflowStages(req.Stages)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start workflow plan transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	instance, err := qtx.LockWorkflowInstanceInWorkspace(r.Context(), db.LockWorkflowInstanceInWorkspaceParams{
		ID: instanceID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow instance not found")
		return
	}
	if !h.requireWorkflowInstanceManager(w, r, userID, instance) {
		return
	}
	if instance.Status != "active" && instance.Status != "waiting" {
		writeError(w, http.StatusConflict, "only active or waiting SOP runs can be adjusted")
		return
	}
	if instance.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "archived SOP runs cannot be adjusted")
		return
	}
	if instance.Revision != req.ExpectedRevision {
		writeError(w, http.StatusConflict, "this SOP run changed elsewhere; reload it before saving")
		return
	}
	executions, err := qtx.ListWorkflowStageExecutions(r.Context(), db.ListWorkflowStageExecutionsParams{WorkspaceID: wsUUID, WorkflowInstanceID: instanceID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load stage executions")
		return
	}
	if len(executions) > 0 {
		writeError(w, http.StatusConflict, "executed SOP plans require explicit rework before modification")
		return
	}
	if err := validateWorkflowStageExecutors(r.Context(), qtx, wsUUID, normalized); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	existing, err := qtx.ListWorkflowInstanceStages(r.Context(), db.ListWorkflowInstanceStagesParams{
		WorkflowInstanceID: instanceID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow plan")
		return
	}
	current, err := qtx.GetWorkflowInstanceStage(r.Context(), db.GetWorkflowInstanceStageParams{
		ID: instance.CurrentStageID, WorkflowInstanceID: instanceID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusConflict, "workflow instance has no valid current stage")
		return
	}
	taskRows, err := qtx.CountIssuesByWorkflowStage(r.Context(), db.CountIssuesByWorkflowStageParams{
		WorkspaceID: wsUUID, WorkflowInstanceID: instanceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate workflow tasks")
		return
	}
	taskCounts := make(map[string]int64, len(taskRows))
	for _, row := range taskRows {
		taskCounts[uuidToString(row.WorkflowStageID)] = row.TaskCount
	}
	decisions, err := qtx.ListWorkflowGateDecisions(r.Context(), db.ListWorkflowGateDecisionsParams{
		WorkflowInstanceID: instanceID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate workflow gates")
		return
	}
	decisionCounts := make(map[string]int)
	for _, decision := range decisions {
		decisionCounts[uuidToString(decision.FromStageID)]++
		if decision.ToStageID.Valid {
			decisionCounts[uuidToString(decision.ToStageID)]++
		}
	}
	existingByID := make(map[string]db.WorkflowInstanceStage, len(existing))
	for _, stage := range existing {
		existingByID[uuidToString(stage.ID)] = stage
	}
	desiredIDs := make(map[string]bool, len(normalized))
	for position, stage := range normalized {
		if stage.ID == nil || strings.TrimSpace(*stage.ID) == "" {
			if int32(position) <= current.Position {
				writeError(w, http.StatusConflict, "new stages must be added after the current stage")
				return
			}
			continue
		}
		stageID, parseOK := parseUUIDOrBadRequest(w, *stage.ID, fmt.Sprintf("stages[%d].id", position))
		if !parseOK {
			return
		}
		id := uuidToString(stageID)
		old, exists := existingByID[id]
		if !exists || desiredIDs[id] {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("stages[%d].id is invalid or duplicated", position))
			return
		}
		desiredIDs[id] = true
		if stage.StableKey != old.StableKey {
			writeError(w, http.StatusConflict, "existing stage stable keys cannot be changed")
			return
		}
		if old.Position < current.Position && (int32(position) != old.Position || !sameWorkflowStageContent(stage, instanceStageInput(old))) {
			writeError(w, http.StatusConflict, "completed stages are locked")
			return
		}
		if old.Position == current.Position && int32(position) != old.Position {
			writeError(w, http.StatusConflict, "the current stage cannot be reordered")
			return
		}
		if old.Position > current.Position && int32(position) != old.Position &&
			(taskCounts[id] > 0 || decisionCounts[id] > 0) {
			writeError(w, http.StatusConflict, "stages with tasks or gate decisions cannot be reordered")
			return
		}
		if old.Position == current.Position && decisionCounts[id] > 0 &&
			!reflect.DeepEqual(jsonOrEmptyObject(old.Gate), stage.Gate) {
			writeError(w, http.StatusConflict, "the current gate cannot change after a decision")
			return
		}
	}
	for _, old := range existing {
		id := uuidToString(old.ID)
		if desiredIDs[id] {
			continue
		}
		if old.Position <= current.Position {
			writeError(w, http.StatusConflict, "the current and completed stages cannot be deleted")
			return
		}
		if taskCounts[id] > 0 || decisionCounts[id] > 0 {
			writeError(w, http.StatusConflict, "stages with tasks or gate decisions cannot be deleted")
			return
		}
	}

	beforePlan, err := workflowPlanJSON(instance.Title, textToPtr(instance.Description), existing)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record workflow plan")
		return
	}
	if err := qtx.ShiftWorkflowInstanceStagePositions(r.Context(), db.ShiftWorkflowInstanceStagePositionsParams{
		WorkflowInstanceID: instanceID, WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare workflow stage order")
		return
	}
	for _, old := range existing {
		if desiredIDs[uuidToString(old.ID)] {
			continue
		}
		if _, err := qtx.DeleteWorkflowInstanceStage(r.Context(), db.DeleteWorkflowInstanceStageParams{
			ID: old.ID, WorkflowInstanceID: instanceID, WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete workflow stage")
			return
		}
	}
	updatedStages := make([]db.WorkflowInstanceStage, 0, len(normalized))
	for position, stage := range normalized {
		completionRule, _ := normalizeObject(stage.CompletionRule, `{"type":"all_tasks_terminal"}`)
		inputSpec, _ := normalizeObject(stage.InputSpec, `{}`)
		inputBindings, _ := normalizeObject(stage.InputBindings, `{}`)
		outputSpec, _ := normalizeObject(stage.OutputSpec, `{}`)
		gate, _ := normalizeObject(stage.Gate, `{"type":"none"}`)
		requiredSkills := stage.RequiredSkills
		if requiredSkills == nil {
			requiredSkills = []string{}
		}
		var snapshots []byte
		var previous db.WorkflowInstanceStage
		if stage.ID != nil {
			previous = existingByID[*stage.ID]
		}
		if previous.ID.Valid && slices.Equal(previous.RequiredSkills, requiredSkills) {
			// Editing plan metadata must not hot-update pinned skill content.
			snapshots = previous.RequiredSkillSnapshots
		} else {
			required, missing := h.TaskService.ResolveRequiredSkills(r.Context(), wsUUID, requiredSkills)
			if len(missing) > 0 {
				writeError(w, http.StatusConflict, "required SOP skill is unavailable: "+strings.Join(missing, ", "))
				return
			}
			var marshalErr error
			snapshots, marshalErr = json.Marshal(required)
			if marshalErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to snapshot workflow skills")
				return
			}
		}
		var updated db.WorkflowInstanceStage
		if stage.ID == nil || strings.TrimSpace(*stage.ID) == "" {
			updated, err = qtx.CreateWorkflowInstanceStage(r.Context(), db.CreateWorkflowInstanceStageParams{
				WorkspaceID: wsUUID, WorkflowInstanceID: instanceID, SourceStageID: pgtype.UUID{},
				StableKey: stage.StableKey, Name: stage.Name, Description: ptrToText(stage.Description),
				ExecutionMode: pgtype.Text{String: stage.ExecutionMode, Valid: true}, ExecutorID: workflowExecutorUUID(stage),
				Position: int32(position), CompletionRule: completionRule, ReworkPolicy: stage.ReworkPolicy, RetryPolicy: stage.RetryPolicy, InputSpec: inputSpec, InputBindings: inputBindings,
				OutputSpec: outputSpec, RequiredSkills: requiredSkills, RequiredSkillSnapshots: snapshots, Gate: gate,
				RollbackStageKey: ptrToText(stage.RollbackStageKey), AllowedMcpServerIds: parseUUIDArray(stage.AllowedMCPServerIDs),
			})
		} else {
			old := existingByID[*stage.ID]
			updated, err = qtx.UpdateWorkflowInstanceStage(r.Context(), db.UpdateWorkflowInstanceStageParams{
				ID: old.ID, WorkflowInstanceID: instanceID, WorkspaceID: wsUUID,
				StableKey: stage.StableKey, Name: stage.Name, Description: ptrToText(stage.Description),
				ExecutionMode: pgtype.Text{String: stage.ExecutionMode, Valid: true}, ExecutorID: workflowExecutorUUID(stage),
				Position: int32(position), CompletionRule: completionRule, ReworkPolicy: stage.ReworkPolicy, RetryPolicy: stage.RetryPolicy, InputSpec: inputSpec, InputBindings: inputBindings,
				OutputSpec: outputSpec, RequiredSkills: requiredSkills, RequiredSkillSnapshots: snapshots, Gate: gate,
				RollbackStageKey: ptrToText(stage.RollbackStageKey), AllowedMcpServerIds: parseUUIDArray(stage.AllowedMCPServerIDs),
			})
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update workflow stage")
			return
		}
		updatedStages = append(updatedStages, updated)
	}
	updatedInstance, err := qtx.UpdateWorkflowInstancePlanMetadata(r.Context(), db.UpdateWorkflowInstancePlanMetadataParams{
		ID: instanceID, WorkspaceID: wsUUID, Title: req.Title,
		Description: ptrToText(req.Description), Revision: req.ExpectedRevision,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "this SOP run changed elsewhere; reload it before saving")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update workflow instance")
		return
	}
	afterPlan, err := workflowPlanJSON(req.Title, req.Description, updatedStages)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record updated workflow plan")
		return
	}
	changedBy, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	change, err := qtx.CreateWorkflowInstanceChange(r.Context(), db.CreateWorkflowInstanceChangeParams{
		WorkspaceID: wsUUID, WorkflowInstanceID: instanceID, Revision: updatedInstance.Revision,
		ChangedBy: changedBy, ChangeNote: ptrToText(req.ChangeNote),
		BeforePlan: beforePlan, AfterPlan: afterPlan,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record workflow change")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit workflow plan")
		return
	}
	resp := workflowInstanceToResponse(updatedInstance)
	resp.Stages = make([]WorkflowInstanceStageResponse, len(updatedStages))
	for i, stage := range updatedStages {
		resp.Stages[i] = workflowInstanceStageToResponse(stage)
	}
	resp.Changes = []WorkflowInstanceChangeResponse{workflowInstanceChangeToResponse(change)}
	if err := h.decorateWorkflowInstanceResponse(r.Context(), r, wsUUID, userID, updatedInstance, &resp); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated workflow plan")
		return
	}
	h.publish(protocol.EventWorkflowInstanceUpdated, workspaceID, "member", userID, map[string]any{"instance": resp})
	writeJSON(w, http.StatusOK, resp)
}
