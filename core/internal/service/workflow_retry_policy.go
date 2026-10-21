package service

import (
	"context"
	"encoding/json"
	"time"

	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
)

// taskRetrySettings reads immutable execution configuration, never live stage
// settings. Non-workflow calls retain the ordinary reason-specific defaults.
func taskRetrySettings(ctx context.Context, q *db.Queries, task db.AgentTaskQueue, reason string) (int32, time.Duration, error) {
	ceiling := retryAttemptCeiling(reason, task.MaxAttempts)
	delay := retryDelayForAttempt(reason, task.Attempt)
	if !task.WorkflowStageExecutionID.Valid && !task.WorkflowReviewRequestID.Valid {
		return ceiling, delay, nil
	}
	agent, err := q.GetAgent(ctx, task.AgentID)
	if err != nil {
		return 0, 0, err
	}
	executionID := task.WorkflowStageExecutionID
	if task.WorkflowReviewRequestID.Valid {
		request, err := q.GetWorkflowReviewRequest(ctx, db.GetWorkflowReviewRequestParams{WorkspaceID: agent.WorkspaceID, ID: task.WorkflowReviewRequestID})
		if err != nil {
			return 0, 0, err
		}
		executionID = request.ExecutionID
	}
	execution, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: agent.WorkspaceID, ID: executionID})
	if err != nil {
		return 0, 0, err
	}
	var frozen struct {
		RetryPolicy json.RawMessage `json:"retry_policy"`
	}
	if err := json.Unmarshal(execution.ConfigSnapshot, &frozen); err != nil {
		return 0, 0, err
	}
	policy, err := workflowcontract.ParseRetryPolicy(frozen.RetryPolicy)
	if err != nil {
		return 0, 0, err
	}
	if task.WorkflowReviewRequestID.Valid && policy.MaxAttempts != nil {
		// Review task budgets also include explicit member recovery grants.
		ceiling = task.MaxAttempts
	} else {
		ceiling = policy.AttemptCeiling(ceiling)
	}
	return ceiling, policy.RetryDelay(task.Attempt, delay), nil
}
