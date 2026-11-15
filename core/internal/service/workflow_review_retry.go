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
func (s *WorkflowExecutionService) RetryAgentReview(ctx context.Context, workspaceID, requestID, failedRunID pgtype.UUID) error {
	if s.AgentRunService == nil {
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
	parent, err := s.Queries.GetAgentRun(ctx, failedRunID)
	if err != nil {
		return err
	}
	if parent.WorkflowReviewRequestID != requestID || parent.Status != "failed" || observed.RunID != failedRunID {
		return ErrWorkflowExecutionConflict
	}
	ceiling, delay, err := runRetrySettings(ctx, s.Queries, parent, parent.FailureReason.String)
	if err != nil {
		return err
	}
	if !reviewRetryDue(parent, ceiling, delay) {
		return ErrWorkflowExecutionConflict
	}
	overlay := s.AgentRunService.buildRuntimeMCPOverlay(ctx, parent.OriginatorUserID, reviewer)
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
	if request.ID != requestID || request.RunID != failedRunID || (request.Status != "queued" && request.Status != "decided") {
		return ErrWorkflowExecutionConflict
	}
	parent, err = q.GetWorkflowResultRun(ctx, db.GetWorkflowResultRunParams{WorkspaceID: workspaceID, ID: failedRunID})
	if err != nil {
		return err
	}
	currentReviewer, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{WorkspaceID: workspaceID, ID: request.ReviewerID})
	if err != nil {
		return err
	}
	reason := parent.FailureReason.String
	ceiling, delay, err = runRetrySettings(ctx, q, parent, reason)
	if err != nil {
		return err
	}
	if parent.Status != "failed" || parent.WorkflowReviewRequestID != request.ID || parent.AgentID != request.ReviewerID || parent.WorkflowStageExecutionID.Valid || !currentReviewer.RuntimeID.Valid || parent.RuntimeID != currentReviewer.RuntimeID || currentReviewer.RuntimeID != reviewer.RuntimeID || !retryableReasons[reason] || parent.Attempt >= ceiling {
		return ErrWorkflowExecutionConflict
	}
	if delay > 0 && (!parent.CompletedAt.Valid || time.Now().Before(parent.CompletedAt.Time.Add(delay))) {
		return ErrWorkflowExecutionConflict
	}
	child, err := q.RetryWorkflowReviewCall(ctx, db.RetryWorkflowReviewCallParams{WorkspaceID: workspaceID, RequestID: requestID, ParentRunID: failedRunID, MaxAttempts: ceiling, RuntimeID: currentReviewer.RuntimeID, RuntimeMcpOverlay: overlay.Overlay, RuntimeConnectedApps: overlay.ConnectedApps})
	if err != nil {
		return err
	}
	agentRun, err := q.GetAgentRun(ctx, child.ID)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.AgentRunService.broadcastRunEvent(ctx, protocol.EventRunQueued, agentRun)
	s.AgentRunService.NotifyRunEnqueued(ctx, agentRun)
	return nil
}

// reviewRetryDue keeps exhausted and deferred requests out of overlay preparation.
func reviewRetryDue(agentRun db.AgentRun, ceiling int32, delay time.Duration) bool {
	reason := agentRun.FailureReason.String
	if !retryableReasons[reason] || agentRun.Attempt >= ceiling {
		return false
	}
	return delay == 0 || (agentRun.CompletedAt.Valid && !time.Now().Before(agentRun.CompletedAt.Time.Add(delay)))
}
