package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// ResumeInputBlockedExecution revalidates current sources against the execution's
// frozen configuration. It never rewrites Run inputs, accepted predecessors, or
// the business attempt. Input correction and the user-facing endpoint are separate.
func (s *WorkflowExecutionService) ResumeInputBlockedExecution(ctx context.Context, workspaceID, instanceID, executionID, actorID pgtype.UUID) (db.WorkflowStageExecution, error) {
	return s.resumeInputBlockedExecution(ctx, workspaceID, instanceID, executionID, actorID, nil, nil)
}

// CorrectBlockedExecutionInputs atomically corrects Run inputs and resumes the
// blocked execution, rejecting stale edits and changes to consumed values.
func (s *WorkflowExecutionService) CorrectBlockedExecutionInputs(ctx context.Context, workspaceID, instanceID, executionID, actorID pgtype.UUID, expected, proposed json.RawMessage) (db.WorkflowStageExecution, error) {
	if len(expected) == 0 || len(proposed) == 0 {
		return db.WorkflowStageExecution{}, ErrWorkflowExecutionConflict
	}
	return s.resumeInputBlockedExecution(ctx, workspaceID, instanceID, executionID, actorID, expected, proposed)
}

func (s *WorkflowExecutionService) resumeInputBlockedExecution(ctx context.Context, workspaceID, instanceID, executionID, actorID pgtype.UUID, expected, proposed json.RawMessage) (db.WorkflowStageExecution, error) {
	empty := db.WorkflowStageExecution{}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	run, err := lockWorkflowExecutionRun(ctx, q, workspaceID, instanceID)
	if err != nil {
		return empty, err
	}
	if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: workspaceID, UserID: actorID}); err != nil {
		return empty, ErrWorkflowReviewAccess
	}
	// Until delegated Run management is defined, only the initiator can resume.
	if actorID != run.CreatedBy {
		return empty, ErrWorkflowReviewAccess
	}
	e, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: workspaceID, ID: executionID})
	if err != nil {
		return empty, err
	}
	if e.WorkflowInstanceID != instanceID || e.WorkflowStageID != run.CurrentStageID || e.PlanRevision != run.Revision || run.Status != "active" || run.ArchivedAt.Valid {
		return empty, ErrWorkflowExecutionConflict
	}
	executor, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{WorkspaceID: workspaceID, ID: e.ExecutorID})
	if err != nil || executor.ArchivedAt.Valid {
		return empty, ErrWorkflowReviewAccess
	}
	allowed, err := workflowMemberCanInvoke(ctx, q, executor, actorID)
	if err != nil {
		return empty, err
	}
	if !allowed {
		return empty, ErrWorkflowReviewAccess
	}
	if proposed != nil && e.Status != "blocked" {
		return empty, ErrWorkflowExecutionConflict
	}
	if e.Status == "pending" || e.Status == "queued" || e.Status == "running" || e.Status == "awaiting_review" {
		if err := tx.Commit(ctx); err != nil {
			return empty, err
		}
		return e, nil
	}
	if e.Status != "blocked" || e.FailureCode.String != "input_unavailable" || e.MainIssueID.Valid || e.MainTaskID.Valid || e.ResultRevision != 0 {
		return empty, ErrWorkflowExecutionConflict
	}
	if proposed != nil {
		// Even a Run with no consumed executions must accept only strict JSON objects.
		if err := workflowcontract.CheckRunInputCorrection(nil, proposed, proposed); err != nil {
			return empty, err
		}
		executions, err := q.ListWorkflowStageExecutions(ctx, db.ListWorkflowStageExecutionsParams{WorkspaceID: workspaceID, WorkflowInstanceID: instanceID})
		if err != nil {
			return empty, err
		}
		for _, previous := range executions {
			if previous.ID == e.ID || previous.Status == "blocked" && previous.FailureCode.String == "input_unavailable" {
				continue
			}
			var config struct {
				InputBindings json.RawMessage `json:"input_bindings"`
			}
			if err := json.Unmarshal(previous.ConfigSnapshot, &config); err != nil {
				return empty, err
			}
			if err := workflowcontract.CheckRunInputCorrection(config.InputBindings, previous.InputSnapshot, proposed); err != nil {
				return empty, fmt.Errorf("%w: %s", ErrWorkflowExecutionConflict, err)
			}
		}
		run, err = q.CorrectWorkflowRunInputs(ctx, db.CorrectWorkflowRunInputsParams{WorkspaceID: workspaceID, ID: instanceID, ExpectedInputs: expected, Inputs: proposed})
		if errors.Is(err, pgx.ErrNoRows) {
			return empty, ErrWorkflowExecutionConflict
		}
		if err != nil {
			return empty, err
		}
	}
	var frozen struct {
		InputSpec     json.RawMessage `json:"input_spec"`
		InputBindings json.RawMessage `json:"input_bindings"`
		Position      int32           `json:"position"`
	}
	if err := json.Unmarshal(e.ConfigSnapshot, &frozen); err != nil {
		return empty, err
	}
	stage := db.WorkflowInstanceStage{ID: e.WorkflowStageID, WorkspaceID: workspaceID, WorkflowInstanceID: instanceID, InputSpec: frozen.InputSpec, InputBindings: frozen.InputBindings, Position: frozen.Position}
	input, failure, err := s.resolveWorkflowExecutionInput(ctx, q, run, stage, run.Inputs)
	if err != nil {
		return empty, err
	}
	if failure != "" {
		return empty, fmt.Errorf("%w: %s", ErrWorkflowExecutionConflict, failure)
	}
	resumed, err := q.ResumeWorkflowInputBlockedExecution(ctx, db.ResumeWorkflowInputBlockedExecutionParams{WorkspaceID: workspaceID, ID: executionID, InputSnapshot: input})
	if err != nil {
		return empty, err
	}
	if _, err := q.CreateWorkflowExecutionAction(ctx, db.CreateWorkflowExecutionActionParams{WorkspaceID: workspaceID, WorkflowInstanceID: instanceID, ExecutionID: executionID, Kind: "dispatch"}); err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	return resumed, nil
}
