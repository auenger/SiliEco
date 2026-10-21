package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// CleanupArtifacts keeps tombstones for two days, repeating deletes hourly to
// catch an abandoned PUT that materializes after its first DELETE. Storage keys
// are unique per upload and are never reused, including across result retries.
func (s *WorkflowExecutionService) CleanupArtifacts(ctx context.Context) error {
	if s.Storage == nil {
		return nil
	}
	uploads, err := s.Queries.ListWorkflowArtifactCleanup(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, upload := range uploads {
		attemptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		count, err := s.Queries.ClaimWorkflowArtifactCleanup(attemptCtx, db.ClaimWorkflowArtifactCleanupParams{ID: upload.ID, WorkspaceID: upload.WorkspaceID})
		if err == nil && count == 1 {
			err = s.Storage.DeleteObject(attemptCtx, upload.StorageKey)
			if err == nil {
				err = s.Queries.DeleteWorkflowArtifactUploadTombstone(attemptCtx, db.DeleteWorkflowArtifactUploadTombstoneParams{ID: upload.ID, WorkspaceID: upload.WorkspaceID})
			}
			if err == nil {
				err = s.Queries.RescheduleWorkflowArtifactCleanup(attemptCtx, db.RescheduleWorkflowArtifactCleanupParams{ID: upload.ID, WorkspaceID: upload.WorkspaceID})
			}
		}
		cancel()
		if err != nil {
			failures = append(failures, err)
		}
		if ctx.Err() != nil {
			break
		}
	}
	return errors.Join(failures...)
}

func (s *WorkflowExecutionService) RunArtifactCleanupWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := s.CleanupArtifacts(ctx); err != nil && ctx.Err() == nil {
			slog.Error("workflow artifact cleanup failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
