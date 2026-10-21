package service

import (
	"context"
	"encoding/hex"
	"errors"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

// ManualReviewRecoveryAttempts includes the first fresh call and its possible
// automatic technical retries. Each expansion requires a new explicit action.
const ManualReviewRecoveryAttempts int32 = 3

type RecoverWorkflowAgentReviewParams struct {
	WorkspaceID, InstanceID, ExecutionID, RequestID, FailedTaskID, ActorID pgtype.UUID
	ExpectedResultRevision                                                 int32
	ExpectedBasisHash                                                      string
	Reason                                                                 string
}

// RecoverAgentReview permits the Run initiator to recover a failed reviewer
// after automatic recovery is unavailable. It never reuses an old decision.
func (s *WorkflowExecutionService) RecoverAgentReview(ctx context.Context, p RecoverWorkflowAgentReviewParams) (db.WorkflowReviewRecovery, error) {
	empty := db.WorkflowReviewRecovery{}
	if s.TaskService == nil || !p.ActorID.Valid || !p.FailedTaskID.Valid || p.ExpectedResultRevision < 1 || len(p.ExpectedBasisHash) != 64 || strings.TrimSpace(p.Reason) == "" || len(p.Reason) > 4000 {
		return empty, ErrWorkflowExecutionConflict
	}
	if _, err := hex.DecodeString(p.ExpectedBasisHash); err != nil {
		return empty, ErrWorkflowExecutionConflict
	}
	observed, err := s.Queries.GetWorkflowReviewRequest(ctx, db.GetWorkflowReviewRequestParams{WorkspaceID: p.WorkspaceID, ID: p.RequestID})
	if err != nil {
		return empty, err
	}
	if observed.WorkflowInstanceID != p.InstanceID || observed.ExecutionID != p.ExecutionID || observed.ResultRevision != p.ExpectedResultRevision || observed.BasisHash != p.ExpectedBasisHash {
		return empty, ErrWorkflowExecutionConflict
	}
	run, err := s.Queries.GetWorkflowInstanceInWorkspace(ctx, db.GetWorkflowInstanceInWorkspaceParams{WorkspaceID: p.WorkspaceID, ID: p.InstanceID})
	if err != nil {
		return empty, err
	}
	if run.CreatedBy != p.ActorID {
		return empty, ErrWorkflowReviewAccess
	}
	prior, priorErr := s.Queries.GetWorkflowReviewRecoveryByFailedCall(ctx, db.GetWorkflowReviewRecoveryByFailedCallParams{WorkspaceID: p.WorkspaceID, FailedTaskID: p.FailedTaskID})
	if priorErr != nil && !errors.Is(priorErr, pgx.ErrNoRows) {
		return empty, priorErr
	}
	var reviewer db.Agent
	var overlay runtimeMCPOverlayData
	if priorErr != nil {
		reviewer, err = s.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{WorkspaceID: p.WorkspaceID, ID: observed.ReviewerID})
		if err != nil {
			return empty, err
		}
		if reviewer.ArchivedAt.Valid || !reviewer.RuntimeID.Valid {
			return empty, ErrWorkflowReviewAccess
		}
		parent, err := s.Queries.GetAgentTask(ctx, p.FailedTaskID)
		if err != nil {
			return empty, err
		}
		if parent.WorkflowReviewRequestID != p.RequestID || (parent.Status != "failed" && parent.Status != "completed") || observed.TaskID != parent.ID {
			return empty, ErrWorkflowExecutionConflict
		}
		allowed, checkErr := reviewCallCanRecover(ctx, s.Queries, observed, parent)
		if checkErr != nil {
			return empty, checkErr
		}
		if !allowed {
			return empty, ErrWorkflowExecutionConflict
		}
		overlay = s.TaskService.buildRuntimeMCPOverlay(ctx, parent.OriginatorUserID, reviewer)
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	run, err = lockWorkflowExecutionRun(ctx, q, p.WorkspaceID, p.InstanceID)
	if err != nil {
		return empty, err
	}
	if run.CreatedBy != p.ActorID {
		return empty, ErrWorkflowReviewAccess
	}
	if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: p.WorkspaceID, UserID: p.ActorID}); err != nil {
		return empty, ErrWorkflowReviewAccess
	}
	prior, priorErr = q.GetWorkflowReviewRecoveryByFailedCall(ctx, db.GetWorkflowReviewRecoveryByFailedCallParams{WorkspaceID: p.WorkspaceID, FailedTaskID: p.FailedTaskID})
	if priorErr == nil {
		if prior.WorkflowInstanceID != p.InstanceID || prior.ExecutionID != p.ExecutionID || prior.RequestID != p.RequestID || prior.ResultRevision != p.ExpectedResultRevision || prior.BasisHash != p.ExpectedBasisHash || prior.ActorID != p.ActorID || prior.Reason != p.Reason {
			return empty, ErrWorkflowExecutionConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return empty, err
		}
		return prior, nil
	}
	if !errors.Is(priorErr, pgx.ErrNoRows) {
		return empty, priorErr
	}
	request, err := s.prepareAgentReviewInTx(ctx, q, p.WorkspaceID, p.InstanceID, p.ExecutionID)
	if err != nil {
		return empty, err
	}
	if request.ID != p.RequestID || request.TaskID != p.FailedTaskID || request.ResultRevision != p.ExpectedResultRevision || request.BasisHash != p.ExpectedBasisHash || (request.Status != "queued" && request.Status != "decided") {
		return empty, ErrWorkflowExecutionConflict
	}
	parent, err := q.GetWorkflowResultTask(ctx, db.GetWorkflowResultTaskParams{WorkspaceID: p.WorkspaceID, ID: p.FailedTaskID})
	if err != nil {
		return empty, err
	}
	currentReviewer, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{WorkspaceID: p.WorkspaceID, ID: request.ReviewerID})
	if err != nil {
		return empty, err
	}
	if (parent.Status != "failed" && parent.Status != "completed") || parent.WorkflowReviewRequestID != request.ID || parent.WorkflowStageExecutionID.Valid || parent.AgentID != request.ReviewerID || currentReviewer.ArchivedAt.Valid || !currentReviewer.RuntimeID.Valid || currentReviewer.RuntimeID != reviewer.RuntimeID || parent.Attempt > math.MaxInt32-ManualReviewRecoveryAttempts {
		return empty, ErrWorkflowExecutionConflict
	}
	allowed, checkErr := reviewCallCanRecover(ctx, q, request, parent)
	if checkErr != nil {
		return empty, checkErr
	}
	if !allowed {
		return empty, ErrWorkflowExecutionConflict
	}
	// A scheduled automatic retry must finish instead of racing a manual one.
	// Rebinding a moved reviewer runtime requires this explicit recovery path.
	automaticCeiling, _, policyErr := taskRetrySettings(ctx, q, parent, parent.FailureReason.String)
	if policyErr != nil {
		return empty, policyErr
	}
	if automaticReviewRetryAvailable(parent, currentReviewer.RuntimeID, automaticCeiling) {
		return empty, ErrWorkflowExecutionConflict
	}
	if err := q.DeleteTaskTokensByTask(ctx, parent.ID); err != nil {
		return empty, err
	}
	ceiling := parent.Attempt + ManualReviewRecoveryAttempts
	child, err := q.RetryWorkflowReviewCall(ctx, db.RetryWorkflowReviewCallParams{WorkspaceID: p.WorkspaceID, RequestID: p.RequestID, ParentTaskID: p.FailedTaskID, RuntimeID: currentReviewer.RuntimeID, MaxAttempts: ceiling, RuntimeMcpOverlay: overlay.Overlay, RuntimeConnectedApps: overlay.ConnectedApps})
	if err != nil {
		return empty, err
	}
	recovery, err := q.CreateWorkflowReviewRecovery(ctx, db.CreateWorkflowReviewRecoveryParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: p.InstanceID, ExecutionID: p.ExecutionID, RequestID: p.RequestID, FailedTaskID: p.FailedTaskID, NewTaskID: child.ID, ResultRevision: p.ExpectedResultRevision, BasisHash: p.ExpectedBasisHash, ActorID: p.ActorID, Reason: p.Reason, AllowedAttempts: ManualReviewRecoveryAttempts, MaxAttempts: ceiling})
	if err != nil {
		return empty, err
	}
	task, err := q.GetAgentTask(ctx, child.ID)
	if err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	s.TaskService.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
	s.TaskService.NotifyTaskEnqueued(ctx, task)
	s.publishAcceptedWorkflow(p.WorkspaceID, p.InstanceID)
	return recovery, nil
}

func automaticReviewRetryAvailable(task db.AgentTaskQueue, runtimeID pgtype.UUID, ceiling int32) bool {
	return runtimeID.Valid && task.Status == "failed" && task.RuntimeID == runtimeID && retryableReasons[task.FailureReason.String] && task.Attempt < ceiling
}

// Process completion without a review decision is an incomplete review. Its
// historical task status remains completed when an explicit recovery is created.
func reviewCallCanRecover(ctx context.Context, q *db.Queries, request db.WorkflowReviewRequest, task db.AgentTaskQueue) (bool, error) {
	if task.Status == "failed" {
		return true, nil
	}
	if task.Status != "completed" {
		return false, nil
	}
	_, err := q.GetWorkflowAgentDecisionByBasis(ctx, db.GetWorkflowAgentDecisionByBasisParams{WorkspaceID: request.WorkspaceID, ExecutionID: request.ExecutionID, BasisHash: pgtype.Text{String: request.BasisHash, Valid: true}, ReviewTaskID: task.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	return false, err
}
