package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// PrepareAgentReview fixes reviewer identity and exact result/evidence before
// any review call can be dispatched. The durable pending request is idempotent
// by execution and basis; a changed delivery invalidates older pending requests.
// This internal entry point does not enqueue or execute a reviewer yet.
func (s *WorkflowExecutionService) PrepareAgentReview(ctx context.Context, workspaceID, instanceID, executionID pgtype.UUID) (db.WorkflowReviewRequest, error) {
	empty := db.WorkflowReviewRequest{}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	request, err := s.prepareAgentReviewInTx(ctx, s.Queries.WithTx(tx), workspaceID, instanceID, executionID)
	if err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	return request, nil
}

func (s *WorkflowExecutionService) prepareAgentReviewInTx(ctx context.Context, q *db.Queries, workspaceID, instanceID, executionID pgtype.UUID) (db.WorkflowReviewRequest, error) {
	empty := db.WorkflowReviewRequest{}
	run, err := lockWorkflowExecutionRun(ctx, q, workspaceID, instanceID)
	if err != nil {
		return empty, err
	}
	e, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: workspaceID, ID: executionID})
	if err != nil {
		return empty, err
	}
	if e.WorkflowInstanceID != instanceID || e.WorkflowStageID != run.CurrentStageID || e.PlanRevision != run.Revision || e.Status != "awaiting_review" || run.Status != "active" || run.ArchivedAt.Valid {
		return empty, ErrWorkflowExecutionConflict
	}
	var config struct {
		Gate struct {
			Type         string `json:"type"`
			Decider      string `json:"decider"`
			HumanDecider string `json:"human_decider"`
		} `json:"gate"`
	}
	if err := json.Unmarshal(e.ConfigSnapshot, &config); err != nil {
		return empty, err
	}
	if config.Gate.Type != "agent" && config.Gate.Type != "hybrid" {
		return empty, ErrWorkflowReviewAccess
	}
	reviewerID, err := util.ParseUUID(config.Gate.Decider)
	if err != nil {
		return empty, ErrWorkflowReviewAccess
	}
	if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: workspaceID, UserID: run.CreatedBy}); err != nil {
		return empty, ErrWorkflowReviewAccess
	}
	for _, agentID := range []pgtype.UUID{e.ExecutorID, reviewerID} {
		agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{WorkspaceID: workspaceID, ID: agentID})
		if err != nil || agent.ArchivedAt.Valid {
			return empty, ErrWorkflowReviewAccess
		}
		allowed, err := workflowMemberCanInvoke(ctx, q, agent, run.CreatedBy)
		if err != nil {
			return empty, err
		}
		if !allowed {
			return empty, ErrWorkflowReviewAccess
		}
	}
	if config.Gate.Type == "hybrid" {
		humanID, err := util.ParseUUID(config.Gate.HumanDecider)
		if err != nil {
			return empty, ErrWorkflowReviewAccess
		}
		if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: workspaceID, UserID: humanID}); err != nil {
			return empty, ErrWorkflowReviewAccess
		}
	}
	basis, err := s.captureWorkflowReviewBasis(ctx, q, run, e, false)
	if err != nil {
		return empty, err
	}
	existing, err := q.GetWorkflowReviewRequestByBasis(ctx, db.GetWorkflowReviewRequestByBasisParams{WorkspaceID: workspaceID, ExecutionID: executionID, BasisHash: basis.Hash})
	if err == nil {
		if existing.Status == "invalidated" || existing.ReviewerID != reviewerID || existing.ResultRevision != basis.ResultRevision {
			return empty, ErrWorkflowExecutionConflict
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	result, err := q.GetWorkflowExecutionResult(ctx, db.GetWorkflowExecutionResultParams{WorkspaceID: workspaceID, ExecutionID: executionID, Revision: basis.ResultRevision})
	if err != nil {
		return empty, err
	}
	if err := q.InvalidateWorkflowReviewRequests(ctx, db.InvalidateWorkflowReviewRequestsParams{WorkspaceID: workspaceID, ExecutionID: executionID, BasisHash: basis.Hash}); err != nil {
		return empty, err
	}
	request, err := q.CreateWorkflowReviewRequest(ctx, db.CreateWorkflowReviewRequestParams{WorkspaceID: workspaceID, WorkflowInstanceID: instanceID, ExecutionID: executionID, ReviewerID: reviewerID, ResultID: result.ID, ResultRevision: result.Revision, BasisHash: basis.Hash, BasisSnapshot: basis.Snapshot})
	if err != nil {
		return empty, err
	}
	return request, nil
}

// GetAgentReview exposes only a currently valid request to its bound reviewer
// call. Callers must project a safe response instead of exposing BasisSnapshot.
func (s *WorkflowExecutionService) GetAgentReview(ctx context.Context, workspaceID, requestID, actorID, taskID pgtype.UUID) (db.WorkflowReviewRequest, error) {
	empty := db.WorkflowReviewRequest{}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	observed, err := q.GetWorkflowReviewRequest(ctx, db.GetWorkflowReviewRequestParams{WorkspaceID: workspaceID, ID: requestID})
	if err != nil {
		return empty, err
	}
	if observed.ReviewerID != actorID || observed.TaskID != taskID || !taskID.Valid {
		return empty, ErrWorkflowReviewAccess
	}
	request, err := s.prepareAgentReviewInTx(ctx, q, workspaceID, observed.WorkflowInstanceID, observed.ExecutionID)
	if err != nil {
		return empty, err
	}
	if request.ID != observed.ID {
		return empty, ErrWorkflowExecutionConflict
	}
	task, err := q.GetWorkflowResultTask(ctx, db.GetWorkflowResultTaskParams{WorkspaceID: workspaceID, ID: taskID})
	if err != nil {
		return empty, err
	}
	if task.AgentID != actorID || task.WorkflowReviewRequestID != requestID || (task.Status != "running" && task.Status != "completed") {
		return empty, ErrWorkflowReviewAccess
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	return request, nil
}
