package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// lockWorkflowTaskRetry acquires Run and issue locks before a retry can lock its
// parent call or execution. Rework uses the same order. Historical calls may
// still finish; the retry SQL independently rejects invalidated work.
func lockWorkflowTaskRetry(ctx context.Context, q *db.Queries, parent db.AgentTaskQueue) error {
	if !parent.IssueID.Valid {
		return nil
	}
	agent, err := q.GetAgent(ctx, parent.AgentID)
	if err != nil {
		return err
	}
	var sourceRun pgtype.UUID
	if parent.WorkflowStageExecutionID.Valid {
		execution, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: agent.WorkspaceID, ID: parent.WorkflowStageExecutionID})
		if err != nil {
			return err
		}
		sourceRun = execution.WorkflowInstanceID
	}
	_, _, err = lockIssueWorkflow(ctx, q, agent.WorkspaceID, parent.IssueID, sourceRun, false)
	return err
}
