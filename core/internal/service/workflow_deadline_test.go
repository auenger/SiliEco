package service

import (
	"context"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"testing"
)

func TestWorkflowDeadlineRespectsExplicitTimeout(t *testing.T) {
	for _, tc := range []struct{ name, policy, started, status string }{{"expired", `{"max_attempts":1,"timeout_seconds":1}`, "2 seconds", "failed"}, {"expired_retry", `{"max_attempts":2,"timeout_seconds":1}`, "2 seconds", "failed"}, {"not_due", `{"max_attempts":1,"timeout_seconds":3600}`, "2 seconds", "running"}, {"unconfigured", `{}`, "2 days", "running"}} {
		t.Run(tc.name, func(t *testing.T) {
			pool, svc, p, _ := workflowResultFixture(t)
			ctx := context.Background()
			e, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(config_snapshot,'{retry_policy}',$2::jsonb) WHERE id=$1`, e.ID, tc.policy); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running',started_at=now()-$2::interval WHERE id=$1`, e.MainTaskID, tc.started); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := svc.ExpireStageCalls(ctx); err != nil {
					t.Fatal(err)
				}
			}
			task, err := svc.Queries.GetAgentTask(ctx, e.MainTaskID)
			if err != nil || task.Status != tc.status {
				t.Fatalf("deadline status=%s want=%s err=%v", task.Status, tc.status, err)
			}
			if tc.status == "failed" {
				if task.FailureReason.String != "timeout" {
					t.Fatalf("wrong reason: %s", task.FailureReason.String)
				}
				p.ActorType = "agent"
				p.ActorID = e.ExecutorID
				p.SourceTaskID = e.MainTaskID
				if _, err := svc.SubmitResult(ctx, p); err == nil {
					t.Fatal("accepted late result after timeout")
				}
				wantChildren := 0
				if tc.name == "expired_retry" {
					wantChildren = 1
				}
				var count int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE retry_of_task_id=$1`, e.MainTaskID).Scan(&count); err != nil || count != wantChildren {
					t.Fatalf("deadline retry created %d children, want %d: %v", count, wantChildren, err)
				}
			}
		})
	}
}

func TestWorkflowDeadlineExpiresReviewerWithoutOrdinaryClone(t *testing.T) {
	pool, svc, p := workflowReviewRecoveryFixture(t, `{"max_attempts":1,"timeout_seconds":1}`)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running',started_at=now()-interval '2 seconds' WHERE id=$1`, p.FailedTaskID); err != nil {
		t.Fatal(err)
	}
	if err := svc.ExpireStageCalls(ctx); err != nil {
		t.Fatal(err)
	}
	task, err := svc.Queries.GetAgentTask(ctx, p.FailedTaskID)
	if err != nil || task.Status != "failed" || task.FailureReason.String != "timeout" {
		t.Fatalf("review timeout: %+v %v", task, err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE retry_of_task_id=$1`, task.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("ordinary review clone: %d %v", count, err)
	}
}
