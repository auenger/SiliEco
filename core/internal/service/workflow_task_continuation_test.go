package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func TestTaskWorkflowContinuationBoundaries(t *testing.T) {
	pool, svc, p := workflowExecutionFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name, status, mode string
		archived, allowed  bool
	}{
		{"manual complete", "completed", "manual", false, true},
		{"agent complete", "completed", "agent", false, false},
		{"archived", "completed", "manual", true, false},
		{"cancelled", "cancelled", "manual", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			q := svc.Queries.WithTx(tx)
			if _, err := tx.Exec(ctx, `UPDATE workflow_instance SET status=$2,completed_at=now(),archived_at=CASE WHEN $3 THEN now() ELSE NULL END WHERE id=$1`, p.InstanceID, tc.status, tc.archived); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `UPDATE workflow_instance_stage SET execution_mode=$2,executor_id=CASE WHEN $2='manual' THEN NULL ELSE executor_id END WHERE id=$1`, p.StageID, tc.mode); err != nil {
				t.Fatal(err)
			}
			run, err := lockWorkflowExecutionRun(ctx, q, p.WorkspaceID, p.InstanceID)
			if err != nil {
				t.Fatal(err)
			}
			err = prepareWorkflowForNewTask(ctx, q, run, p.StageID)
			if tc.allowed && err != nil {
				t.Fatal(err)
			}
			if !tc.allowed && !errors.Is(err, ErrWorkflowExecutionConflict) {
				t.Fatalf("expected conflict, got %v", err)
			}
			updated, err := q.GetWorkflowInstanceInWorkspace(ctx, db.GetWorkflowInstanceInWorkspaceParams{ID: p.InstanceID, WorkspaceID: p.WorkspaceID})
			if err != nil {
				t.Fatal(err)
			}
			if tc.allowed && (updated.Status != "active" || updated.CompletedAt.Valid) {
				t.Fatalf("not resumed: %+v", updated)
			}
			if !tc.allowed && updated.Status != tc.status {
				t.Fatal("failed continuation mutated run")
			}
			if err := prepareWorkflowForNewTask(ctx, q, run, pgtype.UUID{}); !errors.Is(err, ErrWorkflowExecutionConflict) {
				t.Fatalf("invalid stage accepted: %v", err)
			}
		})
	}
}
