package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

type ReworkWorkflowExecutionParams struct {
	WorkspaceID, InstanceID, SourceExecutionID, TargetStageID, ActorID pgtype.UUID
	ExpectedRevision                                                   int32
	Reason                                                             string
}

type workflowReworkMetadata struct {
	ActorID pgtype.UUID `json:"actor_id"`
	Reason  string      `json:"reason"`
}

type WorkflowExecutionReworkResult struct {
	Record    db.WorkflowRework
	Execution *db.WorkflowStageExecution
}

// ReworkExecution invalidates the target and all following stages in this
// linear SOP. Retained results and decisions remain historical evidence only.
func (s *WorkflowExecutionService) ReworkExecution(ctx context.Context, p ReworkWorkflowExecutionParams) (WorkflowExecutionReworkResult, error) {
	empty := WorkflowExecutionReworkResult{}
	if !p.ActorID.Valid || !p.SourceExecutionID.Valid || !p.TargetStageID.Valid || p.ExpectedRevision < 1 || strings.TrimSpace(p.Reason) == "" || len(p.Reason) > 4000 {
		return empty, ErrWorkflowExecutionConflict
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	run, err := lockWorkflowExecutionRun(ctx, q, p.WorkspaceID, p.InstanceID)
	if err != nil {
		return empty, err
	}
	if run.CreatedBy != p.ActorID {
		return empty, ErrWorkflowReviewAccess
	}
	if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: p.WorkspaceID, UserID: p.ActorID}); err != nil {
		return empty, ErrWorkflowReviewAccess
	}
	prior, err := q.GetWorkflowReworkByExecution(ctx, db.GetWorkflowReworkByExecutionParams{WorkspaceID: p.WorkspaceID, SourceExecutionID: p.SourceExecutionID})
	if err == nil {
		if prior.WorkflowInstanceID != run.ID || prior.TargetStageID != p.TargetStageID || prior.PlanRevision != p.ExpectedRevision || prior.ActorType != "member" || prior.ActorID != p.ActorID || prior.Reason != p.Reason {
			return empty, ErrWorkflowExecutionConflict
		}
		result := WorkflowExecutionReworkResult{Record: prior}
		execution, lookupErr := q.GetWorkflowExecutionRework(ctx, db.GetWorkflowExecutionReworkParams{WorkspaceID: p.WorkspaceID, ReworkSourceExecutionID: p.SourceExecutionID})
		if lookupErr == nil {
			result.Execution = &execution
		} else if !errors.Is(lookupErr, pgx.ErrNoRows) {
			return empty, lookupErr
		}
		if err := tx.Commit(ctx); err != nil {
			return empty, err
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	if run.ArchivedAt.Valid || run.Revision != p.ExpectedRevision || (run.Status != "active" && run.Status != "waiting" && run.Status != "completed") {
		return empty, ErrWorkflowExecutionConflict
	}
	source, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.SourceExecutionID})
	if err != nil {
		return empty, err
	}
	if source.WorkflowInstanceID != run.ID || source.WorkflowStageID != run.CurrentStageID || source.PlanRevision != run.Revision || source.FailureCode.String == "rework_invalidated" {
		return empty, ErrWorkflowExecutionConflict
	}
	latest, err := q.GetLatestWorkflowStageExecution(ctx, db.GetLatestWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: run.ID, WorkflowStageID: source.WorkflowStageID})
	if err != nil {
		return empty, err
	}
	if latest.ID != source.ID {
		return empty, ErrWorkflowExecutionConflict
	}
	current, err := q.GetWorkflowInstanceStage(ctx, db.GetWorkflowInstanceStageParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: run.ID, ID: source.WorkflowStageID})
	if err != nil {
		return empty, err
	}
	target, err := q.GetWorkflowInstanceStage(ctx, db.GetWorkflowInstanceStageParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: run.ID, ID: p.TargetStageID})
	if err != nil {
		return empty, err
	}
	var frozen struct {
		RollbackStageKey *string `json:"rollback_stage_key"`
	}
	if json.Unmarshal(source.ConfigSnapshot, &frozen) != nil {
		return empty, ErrWorkflowExecutionConflict
	}
	if (target.ExecutionMode != "agent" && target.ExecutionMode != "manual") || target.Position > current.Position || (target.ID != current.ID && (frozen.RollbackStageKey == nil || *frozen.RollbackStageKey != target.StableKey)) {
		return empty, ErrWorkflowExecutionConflict
	}
	effects, err := InvalidateWorkflowForReworkInTx(ctx, q, WorkflowReworkInvalidationParams{Run: run, Source: current, Target: target, SourceExecutionID: source.ID, ActorType: "member", ActorID: p.ActorID, Reason: p.Reason})
	if err != nil {
		return empty, err
	}
	if _, err := q.UpdateWorkflowInstanceState(ctx, db.UpdateWorkflowInstanceStateParams{WorkspaceID: p.WorkspaceID, ID: run.ID, Status: pgtype.Text{String: "active", Valid: true}, CurrentStageID: target.ID}); err != nil {
		return empty, err
	}
	result := WorkflowExecutionReworkResult{Record: effects.Record}
	if target.ExecutionMode == "agent" {
		next, err := s.EnsureStageExecutionInTx(ctx, q, EnsureWorkflowExecutionParams{WorkspaceID: p.WorkspaceID, InstanceID: run.ID, StageID: target.ID, ExpectedRevision: run.Revision, ExecutorID: target.ExecutorID, InputSnapshot: run.Inputs})
		if err != nil {
			return empty, err
		}
		metadata, err := json.Marshal(workflowReworkMetadata{ActorID: p.ActorID, Reason: p.Reason})
		if err != nil {
			return empty, err
		}
		next, err = q.MarkWorkflowExecutionRework(ctx, db.MarkWorkflowExecutionReworkParams{WorkspaceID: p.WorkspaceID, ID: next.ID, ReworkSourceExecutionID: source.ID, ReworkMetadata: metadata})
		if err != nil {
			return empty, err
		}
		result.Execution = &next
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	if s.TaskService != nil {
		s.TaskService.BroadcastCancelledTasks(ctx, effects.CancelledTasks)
	}
	s.publishAcceptedWorkflow(p.WorkspaceID, run.ID)
	return result, nil
}

func cancelWorkflowReworkCalls(ctx context.Context, q *db.Queries, calls []db.AgentTaskQueue) ([]db.AgentTaskQueue, error) {
	cancelled := []db.AgentTaskQueue{}
	for _, call := range calls {
		if err := q.DeleteTaskTokensByTask(ctx, call.ID); err != nil {
			return nil, err
		}
		switch call.Status {
		case "queued", "dispatched", "running", "waiting_local_directory", "deferred":
			stopped, err := q.CancelAgentTask(ctx, call.ID)
			if err != nil {
				return nil, err
			}
			cancelled = append(cancelled, stopped)
		}
	}
	return cancelled, nil
}

// WorkflowReworkInvalidationParams describes an authorized rollback under the
// caller's existing Run lock. State update and dispatch must share that transaction.
type WorkflowReworkInvalidationParams struct {
	Run               db.WorkflowInstance
	Source, Target    db.WorkflowInstanceStage
	SourceExecutionID pgtype.UUID
	SourceStateToken  pgtype.Text
	ActorType         string
	ActorID           pgtype.UUID
	Reason            string
}

type WorkflowReworkEffects struct {
	Record         db.WorkflowRework
	CancelledTasks []db.AgentTaskQueue
}

func InvalidateWorkflowForReworkInTx(ctx context.Context, q *db.Queries, p WorkflowReworkInvalidationParams) (WorkflowReworkEffects, error) {
	empty := WorkflowReworkEffects{}
	if p.Source.WorkflowInstanceID != p.Run.ID || p.Target.WorkflowInstanceID != p.Run.ID || p.Source.WorkspaceID != p.Run.WorkspaceID || p.Target.WorkspaceID != p.Run.WorkspaceID || p.Run.CurrentStageID != p.Source.ID || p.Target.Position > p.Source.Position || !p.ActorID.Valid || strings.TrimSpace(p.Reason) == "" || len(p.Reason) > 4000 {
		return empty, ErrWorkflowExecutionConflict
	}
	policyRaw := p.Source.ReworkPolicy
	if p.SourceExecutionID.Valid {
		execution, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.Run.WorkspaceID, ID: p.SourceExecutionID})
		if err != nil {
			return empty, err
		}
		if execution.WorkflowInstanceID != p.Run.ID || execution.WorkflowStageID != p.Source.ID {
			return empty, ErrWorkflowExecutionConflict
		}
		var frozen struct {
			ReworkPolicy json.RawMessage `json:"rework_policy"`
		}
		if err := json.Unmarshal(execution.ConfigSnapshot, &frozen); err != nil {
			return empty, err
		}
		policyRaw = frozen.ReworkPolicy
	}
	policy, err := workflowcontract.ParseReworkPolicy(policyRaw)
	if err != nil {
		return empty, fmt.Errorf("%w: invalid rework policy", ErrWorkflowExecutionConflict)
	}
	if policy.MaxReworks != nil {
		count, err := q.CountWorkflowStageReworks(ctx, db.CountWorkflowStageReworksParams{WorkspaceID: p.Run.WorkspaceID, WorkflowInstanceID: p.Run.ID, SourceStageID: p.Source.ID})
		if err != nil {
			return empty, err
		}
		if count >= int64(*policy.MaxReworks) {
			return empty, fmt.Errorf("%w: stage rework limit reached", ErrWorkflowExecutionConflict)
		}
	}
	record, err := q.CreateWorkflowRework(ctx, db.CreateWorkflowReworkParams{WorkspaceID: p.Run.WorkspaceID, WorkflowInstanceID: p.Run.ID, SourceStageID: p.Source.ID, TargetStageID: p.Target.ID, SourceExecutionID: p.SourceExecutionID, SourceStateToken: p.SourceStateToken, PlanRevision: p.Run.Revision, ActorType: p.ActorType, ActorID: p.ActorID, Reason: p.Reason})
	if err != nil {
		return empty, err
	}
	executions, err := q.ListWorkflowReworkExecutions(ctx, db.ListWorkflowReworkExecutionsParams{WorkspaceID: p.Run.WorkspaceID, WorkflowInstanceID: p.Run.ID, Position: p.Target.Position})
	if err != nil {
		return empty, err
	}
	if err := q.InvalidateWorkflowStageIssuesForRework(ctx, db.InvalidateWorkflowStageIssuesForReworkParams{WorkspaceID: p.Run.WorkspaceID, InstanceID: p.Run.ID, Position: p.Target.Position, ReworkID: record.ID}); err != nil {
		return empty, err
	}
	calls, err := q.ListInvalidatedWorkflowCallsForRework(ctx, db.ListInvalidatedWorkflowCallsForReworkParams{WorkspaceID: p.Run.WorkspaceID, WorkflowInstanceID: p.Run.ID, WorkflowInvalidatedByReworkID: record.ID})
	if err != nil {
		return empty, err
	}
	cancelled, err := cancelWorkflowReworkCalls(ctx, q, calls)
	if err != nil {
		return empty, err
	}
	if err := q.InvalidateWorkflowStageDecisionsForRework(ctx, db.InvalidateWorkflowStageDecisionsForReworkParams{WorkspaceID: p.Run.WorkspaceID, InstanceID: p.Run.ID, Position: p.Target.Position, ReworkID: record.ID}); err != nil {
		return empty, err
	}
	for _, execution := range executions {
		if err := q.BindLegacyIssuesForRework(ctx, db.BindLegacyIssuesForReworkParams{WorkspaceID: p.Run.WorkspaceID, ID: execution.ID}); err != nil {
			return empty, err
		}
		calls, err := q.ListWorkflowCallsForRework(ctx, db.ListWorkflowCallsForReworkParams{WorkspaceID: p.Run.WorkspaceID, ExecutionID: execution.ID})
		if err != nil {
			return empty, err
		}
		stopped, err := cancelWorkflowReworkCalls(ctx, q, calls)
		if err != nil {
			return empty, err
		}
		cancelled = append(cancelled, stopped...)

		if err := q.CancelWorkflowActionsForRework(ctx, db.CancelWorkflowActionsForReworkParams{WorkspaceID: p.Run.WorkspaceID, ExecutionID: execution.ID}); err != nil {
			return empty, err
		}
		if err := q.InvalidateWorkflowReviewsForRework(ctx, db.InvalidateWorkflowReviewsForReworkParams{WorkspaceID: p.Run.WorkspaceID, ExecutionID: execution.ID}); err != nil {
			return empty, err
		}
		if err := q.InvalidateWorkflowExecutionForRework(ctx, db.InvalidateWorkflowExecutionForReworkParams{WorkspaceID: p.Run.WorkspaceID, ID: execution.ID, FailureDetail: pgtype.Text{String: p.Reason, Valid: true}}); err != nil {
			return empty, err
		}
	}
	return WorkflowReworkEffects{Record: record, CancelledTasks: cancelled}, nil
}
