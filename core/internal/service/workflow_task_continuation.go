package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// TaskWorkflowIsReusable identifies task-driven plans. Agent-stage executions
// retain their accepted-result locks and must not inherit a previous delivery.
func TaskWorkflowIsReusable(stages []db.WorkflowInstanceStage) bool {
	if len(stages) == 0 {
		return false
	}
	for _, stage := range stages {
		if stage.ExecutionMode != "manual" {
			return false
		}
	}
	return true
}

// prepareWorkflowForNewTask runs under the Run lock in the task transaction.
// Completion describes the delivered work, not permanent closure of a manual
// task pipeline. New membership reopens its review cursor without deleting old
// tasks or decisions. Moving to an earlier stage also requires fresh review.
func prepareWorkflowForNewTask(ctx context.Context, q *db.Queries, run db.WorkflowInstance, stageID pgtype.UUID) error {
	if run.ArchivedAt.Valid || run.Status == "cancelled" {
		return ErrWorkflowExecutionConflict
	}
	stages, err := q.ListWorkflowInstanceStages(ctx, db.ListWorkflowInstanceStagesParams{WorkspaceID: run.WorkspaceID, WorkflowInstanceID: run.ID})
	if err != nil {
		return err
	}
	var target, current *db.WorkflowInstanceStage
	for i := range stages {
		if stages[i].ID == stageID {
			target = &stages[i]
		}
		if stages[i].ID == run.CurrentStageID {
			current = &stages[i]
		}
	}
	if target == nil {
		return ErrWorkflowExecutionConflict
	}
	if !TaskWorkflowIsReusable(stages) {
		if run.Status == "completed" {
			return ErrWorkflowExecutionConflict
		}
		return nil
	}
	nextStageID, nextStatus := stageID, "active"
	if run.Status != "completed" && (current == nil || target.Position >= current.Position) {
		nextStageID, nextStatus = run.CurrentStageID, run.Status
	}
	_, err = q.UpdateWorkflowInstanceState(ctx, db.UpdateWorkflowInstanceStateParams{
		ID: run.ID, WorkspaceID: run.WorkspaceID,
		Status: pgtype.Text{String: nextStatus, Valid: true}, CurrentStageID: nextStageID,
	})
	return err
}
