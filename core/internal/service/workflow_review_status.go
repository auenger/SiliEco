package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// WorkflowAgentReviewStatus is diagnostic only. Recovery rechecks all evidence,
// identities and permissions transactionally instead of trusting this snapshot.
type WorkflowAgentReviewStatus struct {
	ExecutionID      pgtype.UUID
	Phase            string
	Request          *db.WorkflowReviewRequest
	Task             *db.AgentTaskQueue
	CanRecover       bool
	RecoveryAttempts int32
	RetryAt          *time.Time
}

func (s *WorkflowExecutionService) InspectAgentReviewStatus(ctx context.Context, workspaceID, instanceID, executionID, actorID pgtype.UUID) (WorkflowAgentReviewStatus, error) {
	result := WorkflowAgentReviewStatus{ExecutionID: executionID, Phase: "waiting_delivery", RecoveryAttempts: ManualReviewRecoveryAttempts}
	run, err := s.Queries.GetWorkflowInstanceInWorkspace(ctx, db.GetWorkflowInstanceInWorkspaceParams{WorkspaceID: workspaceID, ID: instanceID})
	if err != nil {
		return result, err
	}
	e, err := s.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: workspaceID, ID: executionID})
	if err != nil {
		return result, err
	}
	if e.WorkflowInstanceID != run.ID {
		return result, ErrWorkflowExecutionConflict
	}
	var config struct {
		Gate struct {
			Type string `json:"type"`
		} `json:"gate"`
	}
	if err := json.Unmarshal(e.ConfigSnapshot, &config); err != nil {
		return result, err
	}
	if config.Gate.Type != "agent" && config.Gate.Type != "hybrid" {
		result.Phase = "not_required"
		return result, nil
	}
	request, err := s.Queries.GetLatestWorkflowReviewRequest(ctx, db.GetLatestWorkflowReviewRequestParams{WorkspaceID: workspaceID, ExecutionID: executionID})
	if errors.Is(err, pgx.ErrNoRows) {
		if e.Status == "awaiting_review" {
			result.Phase = "preparing"
		}
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Request = &request
	if request.ResultRevision != e.ResultRevision {
		result.Phase = "stale"
		return result, nil
	}
	if !request.TaskID.Valid {
		result.Phase = "preparing"
		return result, nil
	}
	task, err := s.Queries.GetAgentTask(ctx, request.TaskID)
	if err != nil {
		return result, err
	}
	if task.WorkflowReviewRequestID != request.ID || task.AgentID != request.ReviewerID {
		return result, ErrWorkflowExecutionConflict
	}
	result.Task = &task
	switch task.Status {
	case "queued", "deferred":
		result.Phase = "queued"
	case "dispatched", "running", "waiting_local_directory":
		result.Phase = "running"
	case "completed":
		result.Phase = "missing_decision"
		decision, err := s.Queries.GetWorkflowAgentDecisionByBasis(ctx, db.GetWorkflowAgentDecisionByBasisParams{WorkspaceID: workspaceID, ExecutionID: executionID, BasisHash: pgtype.Text{String: request.BasisHash, Valid: true}, ReviewTaskID: task.ID})
		if err == nil {
			result.Phase = decision.Outcome
			return result, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return result, err
		}
		fallthrough
	case "failed":
		if task.Status == "failed" {
			result.Phase = "failed"
		}
		if run.Status != "active" || run.ArchivedAt.Valid || run.CurrentStageID != e.WorkflowStageID || run.Revision != e.PlanRevision || e.Status != "awaiting_review" {
			return result, nil
		}
		reviewer, err := s.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{WorkspaceID: workspaceID, ID: request.ReviewerID})
		if err != nil {
			return result, err
		}
		if reviewer.ArchivedAt.Valid || !reviewer.RuntimeID.Valid {
			return result, nil
		}
		allowed, err := workflowMemberCanInvoke(ctx, s.Queries, reviewer, run.CreatedBy)
		if err != nil {
			return result, err
		}
		if !allowed {
			return result, nil
		}
		ceiling, delay, policyErr := taskRetrySettings(ctx, s.Queries, task, task.FailureReason.String)
		if policyErr != nil {
			return result, policyErr
		}
		if automaticReviewRetryAvailable(task, reviewer.RuntimeID, ceiling) {
			result.Phase = "retry_scheduled"
			if task.CompletedAt.Valid {
				due := task.CompletedAt.Time.Add(delay)
				result.RetryAt = &due
			}
		} else {
			result.CanRecover = run.CreatedBy == actorID && task.Attempt <= 2147483647-ManualReviewRecoveryAttempts
		}
	default:
		result.Phase = "unavailable"
	}
	return result, nil
}
