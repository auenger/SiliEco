package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// validateReviewCallInTx holds the Run and evidence locks through claim/token
// finalization/start, closing the gap between preparation and actual execution.
func (s *WorkflowExecutionService) validateReviewCallInTx(ctx context.Context, q *db.Queries, agentRun db.AgentRun) error {
	agent, err := q.GetAgent(ctx, agentRun.AgentID)
	if err != nil {
		return err
	}
	observed, err := q.GetWorkflowReviewRequest(ctx, db.GetWorkflowReviewRequestParams{WorkspaceID: agent.WorkspaceID, ID: agentRun.WorkflowReviewRequestID})
	if err != nil {
		return err
	}
	request, err := s.prepareAgentReviewInTx(ctx, q, agent.WorkspaceID, observed.WorkflowInstanceID, observed.ExecutionID)
	if err != nil {
		return err
	}
	current, err := q.GetWorkflowResultRun(ctx, db.GetWorkflowResultRunParams{WorkspaceID: agent.WorkspaceID, ID: agentRun.ID})
	if err != nil {
		return err
	}
	if current.WorkflowReviewRequestID != agentRun.WorkflowReviewRequestID || current.AgentID != agentRun.AgentID || current.TaskID != agentRun.TaskID || current.RuntimeID != agentRun.RuntimeID || current.Status != agentRun.Status || current.DispatchedAt != agentRun.DispatchedAt {
		return ErrWorkflowExecutionConflict
	}
	if request.ID != observed.ID || request.Status != "queued" || request.RunID != agentRun.ID || request.ReviewerID != agentRun.AgentID || !agentRun.RuntimeID.Valid || agentRun.RuntimeID != agent.RuntimeID {
		return ErrWorkflowExecutionConflict
	}
	e, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: agent.WorkspaceID, ID: request.ExecutionID})
	if err != nil {
		return err
	}
	if agentRun.TaskID != e.MainTaskID || agentRun.WorkflowStageExecutionID.Valid {
		return ErrWorkflowExecutionConflict
	}
	return nil
}

func (s *AgentRunService) claimNextWorkflowReview(ctx context.Context, agentID pgtype.UUID) (*db.AgentRun, error) {
	requests, err := s.Queries.ListQueuedWorkflowReviewsForAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	for _, request := range requests {
		var claimed *db.AgentRun
		err := s.runInTx(ctx, func(q *db.Queries) error {
			agentRun, err := q.GetAgentRun(ctx, request.RunID)
			if err != nil {
				return err
			}
			workflow := WorkflowExecutionService{Queries: s.Queries, TxStarter: s.TxStarter}
			if err := workflow.validateReviewCallInTx(ctx, q, agentRun); err != nil {
				return err
			}
			agent, err := q.GetAgentForClaimUpdate(ctx, agentID)
			if err != nil {
				return err
			}
			running, err := q.CountRunningRuns(ctx, agentID)
			if err != nil {
				return err
			}
			if running >= int64(agent.MaxConcurrentRuns) {
				return ErrWorkflowExecutionConflict
			}
			agentRun, err = q.ClaimWorkflowReviewCall(ctx, db.ClaimWorkflowReviewCallParams{RunID: agentRun.ID, RequestID: request.ID, PrepareLeaseSecs: prepareLeaseDuration.Seconds()})
			if err != nil {
				return err
			}
			claimed = &agentRun
			return nil
		})
		if err == nil && claimed != nil {
			return claimed, nil
		}
		if err != nil && !errors.Is(err, ErrWorkflowExecutionConflict) && !errors.Is(err, ErrWorkflowReviewAccess) && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	return nil, nil
}

func (s *AgentRunService) startRunWithReviewValidation(ctx context.Context, runID pgtype.UUID) (db.AgentRun, error) {
	agentRun, err := s.Queries.GetAgentRun(ctx, runID)
	if err != nil {
		return agentRun, err
	}
	if !agentRun.WorkflowReviewRequestID.Valid {
		return s.Queries.StartAgentRun(ctx, runID)
	}
	err = s.runInTx(ctx, func(q *db.Queries) error {
		workflow := WorkflowExecutionService{Queries: s.Queries, TxStarter: s.TxStarter}
		if err := workflow.validateReviewCallInTx(ctx, q, agentRun); err != nil {
			return err
		}
		var err error
		agentRun, err = q.StartAgentRun(ctx, runID)
		return err
	})
	return agentRun, err
}
