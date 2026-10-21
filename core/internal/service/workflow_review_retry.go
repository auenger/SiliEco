package service

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

// RetryAgentReview creates a fresh reviewer call for the same evidence. The
// request pointer is advanced atomically; decisions remain tied to their calls.
func (s *WorkflowExecutionService) RetryAgentReview(ctx context.Context, workspaceID, requestID, failedTaskID pgtype.UUID) error {
	if s.TaskService == nil {
		return ErrWorkflowExecutionConflict
	}
	observed, err := s.Queries.GetWorkflowReviewRequest(ctx, db.GetWorkflowReviewRequestParams{WorkspaceID: workspaceID, ID: requestID})
	if err != nil {
		return err
	}
	reviewer, err := s.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{WorkspaceID: workspaceID, ID: observed.ReviewerID})
	if err != nil {
		return err
	}
	// Network overlay preparation stays outside the Run transaction.
	parent, err := s.Queries.GetAgentTask(ctx, failedTaskID)
	if err != nil {
		return err
	}
	if parent.WorkflowReviewRequestID != requestID || parent.Status != "failed" || observed.TaskID != failedTaskID {
		return ErrWorkflowExecutionConflict
	}
	ceiling, delay, err := taskRetrySettings(ctx, s.Queries, parent, parent.FailureReason.String)
	if err != nil {
		return err
	}
	if !reviewRetryDue(parent, ceiling, delay) {
		return ErrWorkflowExecutionConflict
	}
	overlay := s.TaskService.buildRuntimeMCPOverlay(ctx, parent.OriginatorUserID, reviewer)
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	request, err := s.prepareAgentReviewInTx(ctx, q, workspaceID, observed.WorkflowInstanceID, observed.ExecutionID)
	if err != nil {
		return err
	}
	if request.ID != requestID || request.TaskID != failedTaskID || (request.Status != "queued" && request.Status != "decided") {
		return ErrWorkflowExecutionConflict
	}
	parent, err = q.GetWorkflowResultTask(ctx, db.GetWorkflowResultTaskParams{WorkspaceID: workspaceID, ID: failedTaskID})
	if err != nil {
		return err
	}
	currentReviewer, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{WorkspaceID: workspaceID, ID: request.ReviewerID})
	if err != nil {
		return err
	}
	reason := parent.FailureReason.String
	ceiling, delay, err = taskRetrySettings(ctx, q, parent, reason)
	if err != nil {
		return err
	}
	if parent.Status != "failed" || parent.WorkflowReviewRequestID != request.ID || parent.AgentID != request.ReviewerID || parent.WorkflowStageExecutionID.Valid || !currentReviewer.RuntimeID.Valid || parent.RuntimeID != currentReviewer.RuntimeID || currentReviewer.RuntimeID != reviewer.RuntimeID || !retryableReasons[reason] || parent.Attempt >= ceiling {
		return ErrWorkflowExecutionConflict
	}
	if delay > 0 && (!parent.CompletedAt.Valid || time.Now().Before(parent.CompletedAt.Time.Add(delay))) {
		return ErrWorkflowExecutionConflict
	}
	child, err := q.RetryWorkflowReviewCall(ctx, db.RetryWorkflowReviewCallParams{WorkspaceID: workspaceID, RequestID: requestID, ParentTaskID: failedTaskID, MaxAttempts: ceiling, RuntimeID: currentReviewer.RuntimeID, RuntimeMcpOverlay: overlay.Overlay, RuntimeConnectedApps: overlay.ConnectedApps})
	if err != nil {
		return err
	}
	task, err := q.GetAgentTask(ctx, child.ID)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.TaskService.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
	s.TaskService.NotifyTaskEnqueued(ctx, task)
	return nil
}

// reviewRetryDue keeps exhausted and deferred requests out of overlay preparation.
func reviewRetryDue(task db.AgentTaskQueue, ceiling int32, delay time.Duration) bool {
	reason := task.FailureReason.String
	if !retryableReasons[reason] || task.Attempt >= ceiling {
		return false
	}
	return delay == 0 || (task.CompletedAt.Valid && !time.Now().Before(task.CompletedAt.Time.Add(delay)))
}
