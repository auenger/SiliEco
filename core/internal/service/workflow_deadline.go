package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
)

// ExpireStageCalls applies explicit per-call execution deadlines. Queue wait
// does not consume the timeout; a retry gets its own started_at and deadline.
// Existing runtime-liveness watchdogs remain independent of this opt-in limit.
func (s *WorkflowExecutionService) ExpireStageCalls(ctx context.Context) error {
	if s.TaskService == nil {
		return nil
	}
	after := pgtype.UUID{Valid: true}
	for {
		calls, err := s.Queries.ListWorkflowRunningDeadlineCalls(ctx, after)
		if err != nil {
			return err
		}
		for _, call := range calls {
			if err := ctx.Err(); err != nil {
				return err
			}
			after = call.ID
			var frozen struct {
				RetryPolicy json.RawMessage `json:"retry_policy"`
			}
			if err := json.Unmarshal(call.ConfigSnapshot, &frozen); err != nil {
				slog.Error("workflow deadline invalid snapshot", "task_id", call.ID, "error", err)
				continue
			}
			policy, err := workflowcontract.ParseRetryPolicy(frozen.RetryPolicy)
			if err != nil {
				slog.Error("workflow deadline invalid policy", "task_id", call.ID, "error", err)
				continue
			}
			if policy.TimeoutSeconds == nil || time.Now().Before(call.StartedAt.Time.Add(time.Duration(*policy.TimeoutSeconds)*time.Second)) {
				continue
			}
			attemptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			_, err = s.TaskService.FailTask(attemptCtx, call.ID, "Configured SOP call timeout exceeded", "", "", "timeout", false, "")
			cancel()
			if err != nil {
				return err
			}
		}
		if len(calls) < 100 {
			return nil
		}
	}
}

func (s *WorkflowExecutionService) RunDeadlineWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if err := s.ExpireStageCalls(ctx); err != nil && ctx.Err() == nil {
			slog.Error("workflow deadline scan failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
