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
func (s *WorkflowExecutionService) validateReviewCallInTx(ctx context.Context, q *db.Queries, task db.AgentTaskQueue) error {
	agent, err := q.GetAgent(ctx, task.AgentID)
	if err != nil {
		return err
	}
	observed, err := q.GetWorkflowReviewRequest(ctx, db.GetWorkflowReviewRequestParams{WorkspaceID: agent.WorkspaceID, ID: task.WorkflowReviewRequestID})
	if err != nil {
		return err
	}
	request, err := s.prepareAgentReviewInTx(ctx, q, agent.WorkspaceID, observed.WorkflowInstanceID, observed.ExecutionID)
	if err != nil {
		return err
	}
	current, err := q.GetWorkflowResultTask(ctx, db.GetWorkflowResultTaskParams{WorkspaceID: agent.WorkspaceID, ID: task.ID})
	if err != nil {
		return err
	}
	if current.WorkflowReviewRequestID != task.WorkflowReviewRequestID || current.AgentID != task.AgentID || current.IssueID != task.IssueID || current.RuntimeID != task.RuntimeID || current.Status != task.Status || current.DispatchedAt != task.DispatchedAt {
		return ErrWorkflowExecutionConflict
	}
	if request.ID != observed.ID || request.Status != "queued" || request.TaskID != task.ID || request.ReviewerID != task.AgentID || !task.RuntimeID.Valid || task.RuntimeID != agent.RuntimeID {
		return ErrWorkflowExecutionConflict
	}
	e, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: agent.WorkspaceID, ID: request.ExecutionID})
	if err != nil {
		return err
	}
	if task.IssueID != e.MainIssueID || task.WorkflowStageExecutionID.Valid {
		return ErrWorkflowExecutionConflict
	}
	return nil
}

func (s *TaskService) claimNextWorkflowReview(ctx context.Context, agentID pgtype.UUID) (*db.AgentTaskQueue, error) {
	requests, err := s.Queries.ListQueuedWorkflowReviewsForAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	for _, request := range requests {
		var claimed *db.AgentTaskQueue
		err := s.runInTx(ctx, func(q *db.Queries) error {
			task, err := q.GetAgentTask(ctx, request.TaskID)
			if err != nil {
				return err
			}
			workflow := WorkflowExecutionService{Queries: s.Queries, TxStarter: s.TxStarter}
			if err := workflow.validateReviewCallInTx(ctx, q, task); err != nil {
				return err
			}
			agent, err := q.GetAgentForClaimUpdate(ctx, agentID)
			if err != nil {
				return err
			}
			running, err := q.CountRunningTasks(ctx, agentID)
			if err != nil {
				return err
			}
			if running >= int64(agent.MaxConcurrentTasks) {
				return ErrWorkflowExecutionConflict
			}
			task, err = q.ClaimWorkflowReviewCall(ctx, db.ClaimWorkflowReviewCallParams{TaskID: task.ID, RequestID: request.ID, PrepareLeaseSecs: prepareLeaseDuration.Seconds()})
			if err != nil {
				return err
			}
			claimed = &task
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

func (s *TaskService) startTaskWithReviewValidation(ctx context.Context, taskID pgtype.UUID) (db.AgentTaskQueue, error) {
	task, err := s.Queries.GetAgentTask(ctx, taskID)
	if err != nil {
		return task, err
	}
	if !task.WorkflowReviewRequestID.Valid {
		return s.Queries.StartAgentTask(ctx, taskID)
	}
	err = s.runInTx(ctx, func(q *db.Queries) error {
		workflow := WorkflowExecutionService{Queries: s.Queries, TxStarter: s.TxStarter}
		if err := workflow.validateReviewCallInTx(ctx, q, task); err != nil {
			return err
		}
		var err error
		task, err = q.StartAgentTask(ctx, taskID)
		return err
	})
	return task, err
}
