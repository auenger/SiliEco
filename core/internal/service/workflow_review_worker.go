package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// ScanAgentReviews recovers work from durable execution/request state. Each
// operation rechecks current evidence under the Run lock, including after any
// external task preparation. Concurrent scans share a single bound review call.
func (s *WorkflowExecutionService) ScanAgentReviews(ctx context.Context) error {
	after := pgtype.UUID{Valid: true}
	for {
		rows, err := s.Queries.ListWorkflowAgentReviewCandidates(ctx, after)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			attemptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := s.processAgentReviewCandidate(attemptCtx, row)
			cancel()
			var validation *WorkflowResultValidationError
			if err != nil && !errors.Is(err, ErrWorkflowExecutionConflict) && !errors.Is(err, ErrWorkflowReviewAccess) && !errors.Is(err, pgx.ErrNoRows) && !errors.As(err, &validation) {
				slog.Error("workflow agent review failed", "execution_id", util.UUIDToString(row.ID), "error", err)
			}
			after = row.ID
		}
		if len(rows) < 100 {
			return ctx.Err()
		}
	}
}

func (s *WorkflowExecutionService) processAgentReviewCandidate(ctx context.Context, row db.ListWorkflowAgentReviewCandidatesRow) error {
	request, err := s.PrepareAgentReview(ctx, row.WorkspaceID, row.WorkflowInstanceID, row.ID)
	if err != nil {
		return err
	}
	if request.Status == "pending" {
		_, err = s.DispatchAgentReview(ctx, row.WorkspaceID, request.ID)
		return err
	}
	if request.TaskID.Valid {
		task, err := s.Queries.GetAgentTask(ctx, request.TaskID)
		if err != nil {
			return err
		}
		if task.Status == "failed" {
			return s.RetryAgentReview(ctx, row.WorkspaceID, request.ID, task.ID)
		}
	}
	if request.Status == "decided" && row.GateType == "agent" {
		_, err = s.AcceptAgentReviewedExecution(ctx, row.WorkspaceID, row.WorkflowInstanceID, row.ID)
		return err
	}
	// Hybrid remains awaiting_review until its designated human approves.
	return nil
}
