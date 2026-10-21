package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

// DispatchAgentReview queues one fresh-session reviewer call for exact evidence.
// Runtime overlays are prepared outside the Run transaction; evidence, reviewer
// access and runtime identity are rechecked before the atomic queue binding.
func (s *WorkflowExecutionService) DispatchAgentReview(ctx context.Context, workspaceID, requestID pgtype.UUID) (db.WorkflowReviewRequest, error) {
	empty := db.WorkflowReviewRequest{}
	if s.TaskService == nil {
		return empty, fmt.Errorf("workflow TaskService is required")
	}
	observed, err := s.Queries.GetWorkflowReviewRequest(ctx, db.GetWorkflowReviewRequestParams{WorkspaceID: workspaceID, ID: requestID})
	if err != nil {
		return empty, err
	}
	if observed.Status != "pending" && observed.Status != "queued" {
		return empty, ErrWorkflowExecutionConflict
	}
	var prepared db.CreateAgentTaskParams
	var maxAttempts pgtype.Int4
	if observed.Status == "pending" {
		e, err := s.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: workspaceID, ID: observed.ExecutionID})
		if err != nil {
			return empty, err
		}
		var snapshot struct {
			RetryPolicy json.RawMessage `json:"retry_policy"`
		}
		if err := json.Unmarshal(e.ConfigSnapshot, &snapshot); err != nil {
			return empty, err
		}
		policy, err := workflowcontract.ParseRetryPolicy(snapshot.RetryPolicy)
		if err != nil {
			return empty, err
		}
		if policy.MaxAttempts != nil {
			maxAttempts = pgtype.Int4{Int32: *policy.MaxAttempts, Valid: true}
		}
		issue, err := s.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{WorkspaceID: workspaceID, ID: e.MainIssueID})
		if err != nil {
			return empty, err
		}
		run, err := s.Queries.GetWorkflowInstanceInWorkspace(ctx, db.GetWorkflowInstanceInWorkspaceParams{WorkspaceID: workspaceID, ID: observed.WorkflowInstanceID})
		if err != nil {
			return empty, err
		}
		// This copy chooses the reviewer without changing the delivery's assignee.
		issue.AssigneeID = observed.ReviewerID
		issue.AssigneeType = pgtype.Text{String: "agent", Valid: true}
		note := fmt.Sprintf("SOP stage review. Review request: %s. Run: %s. Execution: %s. Result: %s revision %d. Evidence hash: %s. Review the exact version and its artifacts; do not rewrite delivery or submit a stage result. A review decision must bind this request and evidence.", util.UUIDToString(observed.ID), util.UUIDToString(run.ID), util.UUIDToString(e.ID), util.UUIDToString(observed.ResultID), observed.ResultRevision, observed.BasisHash)
		note += fmt.Sprintf("\nRead the bound request: silieco workflow agent-review %[1]s %[2]s %[3]s\nRead its result: silieco workflow result %[1]s %[2]s --revision %[4]d\nAfter inspecting the exact result and artifacts, record your decision: silieco workflow agent-decision %[1]s %[2]s %[3]s --result-revision %[4]d --basis-hash %[5]s --outcome <approved|rejected> --note <explanation>\nUse your existing call identity. A conflict requires a fresh review; do not refresh and resubmit automatically. Recording approval does not complete the stage until this review call finishes successfully.", util.UUIDToString(run.ID), util.UUIDToString(e.ID), util.UUIDToString(observed.ID), observed.ResultRevision, observed.BasisHash)

		prepared, err = s.TaskService.prepareIssueTaskWithCommentPlan(ctx, issue, pgtype.UUID{}, nil, true, note, run.CreatedBy, pgtype.UUID{})
		if err != nil {
			return empty, err
		}
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	request, err := s.prepareAgentReviewInTx(ctx, q, workspaceID, observed.WorkflowInstanceID, observed.ExecutionID)
	if err != nil {
		return empty, err
	}
	if request.ID != observed.ID {
		return empty, ErrWorkflowExecutionConflict
	}
	if request.Status == "queued" && request.TaskID.Valid {
		if err := tx.Commit(ctx); err != nil {
			return empty, err
		}
		return request, nil
	}
	if request.Status != "pending" || observed.Status != "pending" {
		return empty, ErrWorkflowExecutionConflict
	}
	reviewer, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{WorkspaceID: workspaceID, ID: request.ReviewerID})
	if err != nil {
		return empty, err
	}
	if !reviewer.RuntimeID.Valid || reviewer.RuntimeID != prepared.RuntimeID {
		return empty, ErrWorkflowExecutionConflict
	}
	runtime, err := q.GetAgentRuntime(ctx, reviewer.RuntimeID)
	if err != nil {
		return empty, err
	}
	if runtime.WorkspaceID != workspaceID {
		return empty, ErrWorkflowReviewAccess
	}
	task, err := q.CreateAgentTask(ctx, prepared)
	if err != nil {
		return empty, err
	}
	bound, err := q.BindWorkflowReviewCall(ctx, db.BindWorkflowReviewCallParams{WorkspaceID: workspaceID, RequestID: request.ID, TaskID: task.ID, MaxAttempts: maxAttempts})
	if err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	if maxAttempts.Valid {
		task.MaxAttempts = maxAttempts.Int32
	}
	task.WorkflowReviewRequestID = request.ID
	s.TaskService.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
	s.TaskService.NotifyTaskEnqueued(ctx, task)
	return bound, nil
}
