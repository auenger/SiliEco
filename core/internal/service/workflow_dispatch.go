package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/issueposition"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
)

// DispatchStageAction consumes a leased intent. The main issue, queue task,
// immutable execution binding and acknowledgement commit together. Delivery is
// safe to retry after an ambiguous commit: the old token cannot create work.
func (s *WorkflowExecutionService) DispatchStageAction(ctx context.Context, workspaceID, actionID, claimToken pgtype.UUID) error {
	if s.TaskService == nil {
		return errors.New("workflow TaskService is required")
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	action, err := q.GetWorkflowExecutionAction(ctx, db.GetWorkflowExecutionActionParams{WorkspaceID: workspaceID, ID: actionID})
	if err != nil {
		return err
	}
	instance, err := lockWorkflowExecutionRun(ctx, q, workspaceID, action.WorkflowInstanceID)
	if err != nil {
		return err
	}
	action, err = q.LockWorkflowExecutionAction(ctx, db.LockWorkflowExecutionActionParams{WorkspaceID: workspaceID, ID: actionID, ClaimToken: claimToken})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrWorkflowExecutionConflict
	}
	if err != nil {
		return err
	}
	if action.Kind != "dispatch" {
		return fmt.Errorf("unsupported workflow action %q", action.Kind)
	}
	execution, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: workspaceID, ID: action.ExecutionID})
	if err != nil {
		return err
	}
	if execution.WorkflowInstanceID != instance.ID || execution.WorkflowStageID != instance.CurrentStageID || execution.PlanRevision != instance.Revision || instance.Status != "active" || instance.ArchivedAt.Valid || execution.Status != "pending" {
		return ErrWorkflowExecutionConflict
	}
	block := func(code, detail string) error {
		if _, err := q.BlockWorkflowStageExecution(ctx, db.BlockWorkflowStageExecutionParams{WorkspaceID: workspaceID, ID: execution.ID, FailureCode: pgtype.Text{String: code, Valid: true}, FailureDetail: pgtype.Text{String: detail, Valid: true}}); err != nil {
			return err
		}
		count, err := q.CancelWorkflowExecutionAction(ctx, db.CancelWorkflowExecutionActionParams{WorkspaceID: workspaceID, ID: action.ID, ClaimToken: claimToken})
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrWorkflowExecutionConflict
		}
		return tx.Commit(ctx)
	}
	agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{WorkspaceID: workspaceID, ID: execution.ExecutorID})
	if errors.Is(err, pgx.ErrNoRows) {
		return block("executor_unavailable", "stage executor no longer exists")
	}
	if err != nil {
		return err
	}
	if agent.ArchivedAt.Valid {
		return block("executor_unavailable", "stage executor is archived")
	}
	if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: workspaceID, UserID: instance.CreatedBy}); errors.Is(err, pgx.ErrNoRows) {
		return block("permission_denied", "Run initiator is no longer a workspace member")
	} else if err != nil {
		return err
	}
	allowed, err := workflowMemberCanInvoke(ctx, q, agent, instance.CreatedBy)
	if err != nil {
		return err
	}
	if !allowed {
		return block("permission_denied", "Run initiator cannot invoke the stage executor")
	}
	if !agent.RuntimeID.Valid {
		return block("missing_runtime", "stage executor has no runtime")
	}
	// Offline runtimes retain queued work; disconnection is not permanent failure.
	runtime, err := q.GetAgentRuntime(ctx, agent.RuntimeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return block("missing_runtime", "stage executor runtime no longer exists")
	}
	if err != nil {
		return err
	}
	if runtime.WorkspaceID != workspaceID {
		return block("permission_denied", "stage executor runtime belongs to another workspace")
	}
	var snapshot struct {
		Name        string          `json:"name"`
		RetryPolicy json.RawMessage `json:"retry_policy"`
		Description *string         `json:"description"`
	}
	if err := json.Unmarshal(execution.ConfigSnapshot, &snapshot); err != nil {
		return err
	}
	policy, err := workflowcontract.ParseRetryPolicy(snapshot.RetryPolicy)
	if err != nil {
		return block("invalid_configuration", err.Error())
	}
	var maxAttempts pgtype.Int4
	if policy.MaxAttempts != nil {
		maxAttempts = pgtype.Int4{Int32: *policy.MaxAttempts, Valid: true}
	}
	if snapshot.Name == "" {
		return block("invalid_configuration", "stage snapshot has no name")
	}
	number, err := q.IncrementIssueCounter(ctx, workspaceID)
	if err != nil {
		return err
	}
	position, err := issueposition.NextTopPosition(ctx, tx, workspaceID, "todo")
	if err != nil {
		return err
	}
	description := ""
	if snapshot.Description != nil {
		description = *snapshot.Description + "\n\n"
	}
	description += "## Stage input snapshot\n\n```json\n" + string(execution.InputSnapshot) + "\n```"
	issue, err := q.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: workspaceID, Title: snapshot.Name, Description: pgtype.Text{String: description, Valid: true}, Status: "todo", Priority: "none",
		AssigneeType: pgtype.Text{String: "agent", Valid: true}, AssigneeID: agent.ID, CreatorType: "member", CreatorID: instance.CreatedBy,
		Position: position, Number: number, ProjectID: instance.ProjectID, WorkflowInstanceID: instance.ID, WorkflowStageID: execution.WorkflowStageID,
	})
	if err != nil {
		return err
	}
	if _, err := q.CreateActivity(ctx, db.CreateActivityParams{
		WorkspaceID: workspaceID, IssueID: issue.ID, ActorType: pgtype.Text{String: "member", Valid: true}, ActorID: instance.CreatedBy,
		Action: "created", Details: []byte(`{}`),
	}); err != nil {
		return err
	}
	task, err := s.TaskService.createIssueTaskWithCommentPlan(ctx, q, issue, pgtype.UUID{}, nil, false, "", instance.CreatedBy, pgtype.UUID{})
	if err != nil {
		return err
	}
	task, err = q.BindWorkflowExecutionTask(ctx, db.BindWorkflowExecutionTaskParams{WorkspaceID: workspaceID, ExecutionID: execution.ID, TaskID: task.ID, MaxAttempts: maxAttempts})
	if err != nil {
		return err
	}
	if _, err := q.MarkWorkflowExecutionQueued(ctx, db.MarkWorkflowExecutionQueuedParams{WorkspaceID: workspaceID, ID: execution.ID, MainIssueID: issue.ID, MainTaskID: task.ID}); err != nil {
		return err
	}
	count, err := q.CompleteWorkflowExecutionAction(ctx, db.CompleteWorkflowExecutionActionParams{WorkspaceID: workspaceID, ID: action.ID, ClaimToken: claimToken})
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrWorkflowExecutionConflict
	}
	workspace, err := q.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if s.TaskService.Bus != nil {
		payload := issueToMap(issue, workspace.IssuePrefix)
		payload["project_id"] = util.UUIDToPtr(issue.ProjectID)
		payload["workflow_instance_id"] = util.UUIDToPtr(issue.WorkflowInstanceID)
		payload["workflow_stage_id"] = util.UUIDToPtr(issue.WorkflowStageID)
		s.TaskService.Bus.Publish(events.Event{Type: protocol.EventIssueCreated, WorkspaceID: util.UUIDToString(workspaceID), ActorType: "member", ActorID: util.UUIDToString(instance.CreatedBy), Payload: map[string]any{"issue": payload}})
	}
	s.TaskService.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
	s.TaskService.NotifyTaskEnqueued(ctx, task)
	return nil
}

// The caller separately verifies workspace membership. No admin bypass and no
// unresolved system-principal exception apply to a human-started SOP Run.
func workflowMemberCanInvoke(ctx context.Context, q *db.Queries, agent db.Agent, userID pgtype.UUID) (bool, error) {
	if userID == agent.OwnerID {
		return true, nil
	}
	if agent.PermissionMode != "public_to" {
		return false, nil
	}
	targets, err := q.ListAgentInvocationTargets(ctx, agent.ID)
	if err != nil {
		return false, err
	}
	for _, target := range targets {
		if target.TargetType == "workspace" || target.TargetType == "member" && target.TargetID == userID {
			return true, nil
		}
	}
	return false, nil
}

// RunDispatchWorker recovers durable intents without depending on a best-effort
// event. One action per workspace per pass prevents one tenant monopolizing it.
func (s *WorkflowExecutionService) RunDispatchWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if err := s.DispatchPending(ctx); err != nil && ctx.Err() == nil {
			slog.Error("workflow dispatch scan failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *WorkflowExecutionService) DispatchPending(ctx context.Context) error {
	workspaces, err := s.Queries.ListWorkflowDispatchWorkspaces(ctx)
	if err != nil {
		return err
	}
	for _, workspaceID := range workspaces {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		action, err := s.Queries.ClaimWorkflowExecutionAction(ctx, workspaceID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = s.DispatchStageAction(attemptCtx, workspaceID, action.ID, action.ClaimToken)
		cancel()
		if err != nil {
			// If this process exits before releasing the lease, the next worker can
			// recover it after expiry. Retry never changes the business attempt.
			if _, retryErr := s.Queries.RetryWorkflowExecutionAction(ctx, db.RetryWorkflowExecutionActionParams{
				WorkspaceID: workspaceID, ID: action.ID, ClaimToken: action.ClaimToken,
				AvailableAt: pgtype.Timestamptz{Time: time.Now().Add(15 * time.Second), Valid: true}, LastError: pgtype.Text{String: err.Error(), Valid: true},
			}); retryErr != nil {
				return retryErr
			}
			slog.Warn("workflow dispatch deferred", "action_id", action.ID, "error", err)
		}
	}
	return nil
}
