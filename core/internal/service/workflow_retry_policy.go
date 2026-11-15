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
func runRetrySettings(ctx context.Context, q *db.Queries, agentRun db.AgentRun, reason string) (int32, time.Duration, error) {
	ceiling := retryAttemptCeiling(reason, agentRun.MaxAttempts)
	delay := retryDelayForAttempt(reason, agentRun.Attempt)
	if !agentRun.WorkflowStageExecutionID.Valid && !agentRun.WorkflowReviewRequestID.Valid {
		return ceiling, delay, nil
	}
	agent, err := q.GetAgent(ctx, agentRun.AgentID)
	if err != nil {
		return 0, 0, err
	}
	executionID := agentRun.WorkflowStageExecutionID
	if agentRun.WorkflowReviewRequestID.Valid {
		request, err := q.GetWorkflowReviewRequest(ctx, db.GetWorkflowReviewRequestParams{WorkspaceID: agent.WorkspaceID, ID: agentRun.WorkflowReviewRequestID})
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
	if agentRun.WorkflowReviewRequestID.Valid && policy.MaxAttempts != nil {
		// Review task budgets also include explicit member recovery grants.
		ceiling = agentRun.MaxAttempts
	} else {
		ceiling = policy.AttemptCeiling(ceiling)
	}
	return ceiling, policy.RetryDelay(agentRun.Attempt, delay), nil
}
