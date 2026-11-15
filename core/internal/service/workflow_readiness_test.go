package service

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func TestWorkflowCompletionRequiresDistinctEvidence(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	inspect := func(wantReady bool, blockers ...string) {
		t.Helper()
		r, err := svc.InspectReadiness(ctx, p.WorkspaceID, p.InstanceID, p.ExecutionID)
		if err != nil {
			t.Fatal(err)
		}
		if r.ReadyForReview != wantReady || r.Scope != "readiness_snapshot" {
			t.Fatalf("unexpected readiness: %+v", r)
		}
		for _, blocker := range blockers {
			if !slices.Contains(r.Blockers, blocker) {
				t.Fatalf("missing blocker %s: %+v", blocker, r)
			}
		}
	}
	inspect(false, "result_not_submitted", "main_task_not_done", "main_call_not_completed", "calls_still_active")
	if _, err := svc.SubmitResult(ctx, p); err != nil {
		t.Fatal(err)
	}
	inspect(false, "main_task_not_done", "main_call_not_completed")
	execution, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE task SET status='done' WHERE id=$1`, execution.MainTaskID); err != nil {
		t.Fatal(err)
	}
	inspect(false, "main_call_not_completed", "calls_still_active")
	if _, err := pool.Exec(ctx, `UPDATE agent_run SET status='cancelled' WHERE id=$1`, execution.MainRunID); err != nil {
		t.Fatal(err)
	}
	inspect(false, "main_call_not_completed")
	if _, err := pool.Exec(ctx, `UPDATE agent_run SET status='completed' WHERE id=$1`, execution.MainRunID); err != nil {
		t.Fatal(err)
	}
	inspect(true)
	// A required task cancelled after submission remains an incomplete delivery.
	additionalID := util.MustParseUUID(uuid.NewString())
	if _, err := pool.Exec(ctx, `INSERT INTO task(id,workspace_id,title,status,workflow_instance_id,workflow_stage_id,creator_type,creator_id) VALUES ($1,$2,'Additional delivery','cancelled',$3,$4,'member',$5)`, additionalID, p.WorkspaceID, p.InstanceID, execution.WorkflowStageID, p.ActorID); err != nil {
		t.Fatal(err)
	}
	inspect(false, "required_tasks_not_done")
	if _, err := pool.Exec(ctx, `UPDATE task SET status='done' WHERE id=$1`, additionalID); err != nil {
		t.Fatal(err)
	}
	inspect(true)
	if _, err := pool.Exec(ctx, `INSERT INTO agent_run(agent_id,task_id,runtime_id,status) SELECT agent_id,$2,runtime_id,'running' FROM agent_run WHERE id=$1`, execution.MainRunID, additionalID); err != nil {
		t.Fatal(err)
	}
	inspect(false, "calls_still_active")
	if _, err := pool.Exec(ctx, `UPDATE agent_run SET status='completed' WHERE task_id=$1`, additionalID); err != nil {
		t.Fatal(err)
	}
	inspect(true)
	if _, err := pool.Exec(ctx, `UPDATE task SET status='in_progress' WHERE id=$1`, execution.MainTaskID); err != nil {
		t.Fatal(err)
	}
	inspect(false, "main_task_not_done", "required_tasks_not_done")
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET revision=revision+1 WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	inspect(false, "execution_not_current")
	if _, err := svc.InspectReadiness(ctx, p.WorkspaceID, util.MustParseUUID(uuid.NewString()), p.ExecutionID); err != pgx.ErrNoRows {
		t.Fatalf("cross-Run scope: %v", err)
	}
	if _, err := svc.InspectReadiness(ctx, util.MustParseUUID(uuid.NewString()), p.InstanceID, p.ExecutionID); err != pgx.ErrNoRows {
		t.Fatalf("cross-workspace scope: %v", err)
	}
}

func TestWorkflowStageDeliveryLocksExistingTasks(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	execution, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	rows, err := svc.Queries.WithTx(tx).LockWorkflowStageTasks(ctx, db.LockWorkflowStageTasksParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: p.InstanceID, WorkflowStageID: execution.WorkflowStageID})
	if err != nil || len(rows) != 1 {
		t.Fatalf("delivery rows: %v %v", rows, err)
	}
	timeout, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if _, err := pool.Exec(timeout, `UPDATE task SET status='done' WHERE id=$1`, execution.MainTaskID); err == nil {
		t.Fatal("status changed while delivery transaction held task lock")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE task SET status='done' WHERE id=$1`, execution.MainTaskID); err != nil {
		t.Fatal(err)
	}
}
