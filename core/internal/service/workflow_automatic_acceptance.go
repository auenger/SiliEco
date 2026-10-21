package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

// AcceptAutomaticExecution trusts only the frozen none gate and rechecks all
// delivery and invocation conditions under the Run lock. No human decision is
// fabricated. Concurrent or stale scans cannot accept a different execution.
func (s *WorkflowExecutionService) AcceptAutomaticExecution(ctx context.Context, workspaceID, instanceID, executionID pgtype.UUID) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	run, err := lockWorkflowExecutionRun(ctx, q, workspaceID, instanceID)
	if err != nil {
		return err
	}
	execution, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: workspaceID, ID: executionID})
	if err != nil {
		return err
	}
	if execution.WorkflowInstanceID != run.ID || execution.WorkflowStageID != run.CurrentStageID || execution.PlanRevision != run.Revision || run.Status != "active" || run.ArchivedAt.Valid || execution.Status != "awaiting_review" {
		return ErrWorkflowExecutionConflict
	}
	var config struct {
		Gate struct {
			Type string `json:"type"`
		} `json:"gate"`
	}
	if err := json.Unmarshal(execution.ConfigSnapshot, &config); err != nil {
		return err
	}
	if config.Gate.Type != "none" {
		return ErrWorkflowReviewAccess
	}
	if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: workspaceID, UserID: run.CreatedBy}); err != nil {
		return ErrWorkflowReviewAccess
	}
	executor, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{WorkspaceID: workspaceID, ID: execution.ExecutorID})
	if err != nil || executor.ArchivedAt.Valid {
		return ErrWorkflowReviewAccess
	}
	allowed, err := workflowMemberCanInvoke(ctx, q, executor, run.CreatedBy)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrWorkflowReviewAccess
	}
	basis, err := s.captureWorkflowReviewBasis(ctx, q, run, execution, false)
	if err != nil {
		return err
	}
	if _, err := s.advanceAcceptedExecutionInTx(ctx, q, run, basis, pgtype.UUID{}, "none"); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.publishAcceptedWorkflow(workspaceID, instanceID)
	return nil
}

func (s *WorkflowExecutionService) publishAcceptedWorkflow(workspaceID, instanceID pgtype.UUID) {
	if s.TaskService != nil && s.TaskService.Bus != nil {
		s.TaskService.Bus.Publish(events.Event{Type: protocol.EventWorkflowInstanceUpdated, WorkspaceID: util.UUIDToString(workspaceID), ActorType: "system", Payload: map[string]any{"instance_id": util.UUIDToString(instanceID)}})
	}
}

// ScanAutomaticAcceptance pages past incomplete deliveries so one waiting Run
// cannot starve ready executions. Durable execution state is the recovery queue.
func (s *WorkflowExecutionService) ScanAutomaticAcceptance(ctx context.Context) error {
	after := pgtype.UUID{Valid: true}
	for {
		rows, err := s.Queries.ListWorkflowAutomaticAcceptanceCandidates(ctx, after)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			attemptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := s.AcceptAutomaticExecution(attemptCtx, row.WorkspaceID, row.WorkflowInstanceID, row.ID)
			cancel()
			var validation *WorkflowResultValidationError
			if err != nil && !errors.Is(err, ErrWorkflowExecutionConflict) && !errors.Is(err, ErrWorkflowReviewAccess) && !errors.Is(err, pgx.ErrNoRows) && !errors.As(err, &validation) {
				slog.Error("workflow automatic acceptance failed", "execution_id", util.UUIDToString(row.ID), "error", err)
			}
			after = row.ID
		}
		if len(rows) < 100 {
			return ctx.Err()
		}
	}
}

func (s *WorkflowExecutionService) RunAcceptanceWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if err := s.ScanAutomaticAcceptance(ctx); err != nil && ctx.Err() == nil {
			slog.Error("workflow acceptance scan failed", "error", err)
		}
		if err := s.ScanAgentReviews(ctx); err != nil && ctx.Err() == nil {
			slog.Error("workflow agent review scan failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
