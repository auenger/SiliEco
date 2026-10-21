package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// WorkflowExecutionReadiness is diagnostic evidence, not an acceptance token.
// Review/advance must recheck these conditions in their mutation transaction.
type WorkflowExecutionReadiness struct {
	Scope            string   `json:"scope"`
	ResultRevision   int32    `json:"result_revision"`
	ReadyForReview   bool     `json:"ready_for_review"`
	Blockers         []string `json:"blockers"`
	IncompleteIssues int64    `json:"incomplete_issues"`
	ActiveCalls      int64    `json:"active_calls"`
}

func (s *WorkflowExecutionService) InspectReadiness(ctx context.Context, workspaceID, instanceID, executionID pgtype.UUID) (WorkflowExecutionReadiness, error) {
	row, err := s.Queries.GetWorkflowExecutionReadiness(ctx, db.GetWorkflowExecutionReadinessParams{WorkspaceID: workspaceID, WorkflowInstanceID: instanceID, ID: executionID})
	if err != nil {
		return WorkflowExecutionReadiness{}, err
	}
	return workflowCompletionConditions(row), nil
}

func workflowCompletionConditions(row db.GetWorkflowExecutionReadinessRow) WorkflowExecutionReadiness {
	result := WorkflowExecutionReadiness{Scope: "readiness_snapshot", ResultRevision: row.ResultRevision, Blockers: []string{}, IncompleteIssues: row.IncompleteIssues, ActiveCalls: row.ActiveCalls}
	if !row.CurrentExecution.Valid || !row.CurrentExecution.Bool {
		result.Blockers = append(result.Blockers, "execution_not_current")
	}
	if row.Status != "awaiting_review" {
		result.Blockers = append(result.Blockers, "execution_not_awaiting_review")
	}
	if !row.HasResult {
		result.Blockers = append(result.Blockers, "result_not_submitted")
	}
	if !row.MainIssueDone {
		result.Blockers = append(result.Blockers, "main_issue_not_done")
	}
	if !row.MainTaskCompleted {
		result.Blockers = append(result.Blockers, "main_call_not_completed")
	}
	if row.IncompleteIssues > 0 {
		result.Blockers = append(result.Blockers, "required_issues_not_done")
	}
	if row.ActiveCalls > 0 {
		result.Blockers = append(result.Blockers, "calls_still_active")
	}
	result.ReadyForReview = len(result.Blockers) == 0
	return result
}
