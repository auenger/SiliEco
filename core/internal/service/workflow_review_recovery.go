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
	WorkspaceID, InstanceID, ExecutionID, RequestID, FailedRunID, ActorID pgtype.UUID
	ExpectedResultRevision                                                int32
	ExpectedBasisHash                                                     string
	Reason                                                                string
}

// RecoverAgentReview permits the Run initiator to recover a failed reviewer
// after automatic recovery is unavailable. It never reuses an old decision.
func (s *WorkflowExecutionService) RecoverAgentReview(ctx context.Context, p RecoverWorkflowAgentReviewParams) (db.WorkflowReviewRecovery, error) {
	empty := db.WorkflowReviewRecovery{}
	if s.AgentRunService == nil || !p.ActorID.Valid || !p.FailedRunID.Valid || p.ExpectedResultRevision < 1 || len(p.ExpectedBasisHash) != 64 || strings.TrimSpace(p.Reason) == "" || len(p.Reason) > 4000 {
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
	prior, priorErr := s.Queries.GetWorkflowReviewRecoveryByFailedCall(ctx, db.GetWorkflowReviewRecoveryByFailedCallParams{WorkspaceID: p.WorkspaceID, FailedRunID: p.FailedRunID})
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
		parent, err := s.Queries.GetAgentRun(ctx, p.FailedRunID)
		if err != nil {
			return empty, err
		}
		if parent.WorkflowReviewRequestID != p.RequestID || (parent.Status != "failed" && parent.Status != "completed") || observed.RunID != parent.ID {
			return empty, ErrWorkflowExecutionConflict
		}
		allowed, checkErr := reviewCallCanRecover(ctx, s.Queries, observed, parent)
		if checkErr != nil {
			return empty, checkErr
		}
		if !allowed {
			return empty, ErrWorkflowExecutionConflict
		}
		overlay = s.AgentRunService.buildRuntimeMCPOverlay(ctx, parent.OriginatorUserID, reviewer)
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
	prior, priorErr = q.GetWorkflowReviewRecoveryByFailedCall(ctx, db.GetWorkflowReviewRecoveryByFailedCallParams{WorkspaceID: p.WorkspaceID, FailedRunID: p.FailedRunID})
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
	if request.ID != p.RequestID || request.RunID != p.FailedRunID || request.ResultRevision != p.ExpectedResultRevision || request.BasisHash != p.ExpectedBasisHash || (request.Status != "queued" && request.Status != "decided") {
		return empty, ErrWorkflowExecutionConflict
	}
	parent, err := q.GetWorkflowResultRun(ctx, db.GetWorkflowResultRunParams{WorkspaceID: p.WorkspaceID, ID: p.FailedRunID})
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
	automaticCeiling, _, policyErr := runRetrySettings(ctx, q, parent, parent.FailureReason.String)
	if policyErr != nil {
		return empty, policyErr
	}
	if automaticReviewRetryAvailable(parent, currentReviewer.RuntimeID, automaticCeiling) {
		return empty, ErrWorkflowExecutionConflict
	}
	if err := q.DeleteAgentRunTokensByRun(ctx, parent.ID); err != nil {
		return empty, err
	}
	ceiling := parent.Attempt + ManualReviewRecoveryAttempts
	child, err := q.RetryWorkflowReviewCall(ctx, db.RetryWorkflowReviewCallParams{WorkspaceID: p.WorkspaceID, RequestID: p.RequestID, ParentRunID: p.FailedRunID, RuntimeID: currentReviewer.RuntimeID, MaxAttempts: ceiling, RuntimeMcpOverlay: overlay.Overlay, RuntimeConnectedApps: overlay.ConnectedApps})
	if err != nil {
		return empty, err
	}
	recovery, err := q.CreateWorkflowReviewRecovery(ctx, db.CreateWorkflowReviewRecoveryParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: p.InstanceID, ExecutionID: p.ExecutionID, RequestID: p.RequestID, FailedRunID: p.FailedRunID, NewRunID: child.ID, ResultRevision: p.ExpectedResultRevision, BasisHash: p.ExpectedBasisHash, ActorID: p.ActorID, Reason: p.Reason, AllowedAttempts: ManualReviewRecoveryAttempts, MaxAttempts: ceiling})
	if err != nil {
		return empty, err
	}
	agentRun, err := q.GetAgentRun(ctx, child.ID)
	if err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	s.AgentRunService.broadcastRunEvent(ctx, protocol.EventRunQueued, agentRun)
	s.AgentRunService.NotifyRunEnqueued(ctx, agentRun)
	s.publishAcceptedWorkflow(p.WorkspaceID, p.InstanceID)
	return recovery, nil
}

func automaticReviewRetryAvailable(agentRun db.AgentRun, runtimeID pgtype.UUID, ceiling int32) bool {
	return runtimeID.Valid && agentRun.Status == "failed" && agentRun.RuntimeID == runtimeID && retryableReasons[agentRun.FailureReason.String] && agentRun.Attempt < ceiling
}

// Process completion without a review decision is an incomplete review. Its
// historical task status remains completed when an explicit recovery is created.
func reviewCallCanRecover(ctx context.Context, q *db.Queries, request db.WorkflowReviewRequest, agentRun db.AgentRun) (bool, error) {
	if agentRun.Status == "failed" {
		return true, nil
	}
	if agentRun.Status != "completed" {
		return false, nil
	}
	_, err := q.GetWorkflowAgentDecisionByBasis(ctx, db.GetWorkflowAgentDecisionByBasisParams{WorkspaceID: request.WorkspaceID, ExecutionID: request.ExecutionID, BasisHash: pgtype.Text{String: request.BasisHash, Valid: true}, ReviewRunID: agentRun.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	return false, err
}
