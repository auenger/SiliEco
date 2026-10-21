package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func TestWorkflowRetryPreservesExecutionAndResultAuthority(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	execution, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	parentID := execution.MainTaskID
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='failed' WHERE id=$1`, parentID); err != nil {
		t.Fatal(err)
	}
	constraint := fmt.Sprintf("workflow_retry_fail_%x", execution.ID.Bytes)
	if _, err := pool.Exec(ctx, "ALTER TABLE agent_task_queue ADD CONSTRAINT "+constraint+" CHECK (retry_of_task_id IS NULL OR retry_of_task_id <> '"+util.UUIDToString(parentID)+"'::uuid) NOT VALID"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), "ALTER TABLE agent_task_queue DROP CONSTRAINT IF EXISTS "+constraint)
	})
	if _, err := svc.Queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: parentID}); err == nil {
		t.Fatal("injected retry failure accepted")
	}
	unchanged, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil || unchanged.MainTaskID != parentID {
		t.Fatalf("failed insert advanced execution: %+v %v", unchanged, err)
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE agent_task_queue DROP CONSTRAINT "+constraint); err != nil {
		t.Fatal(err)
	}
	child, err := svc.Queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: parentID})
	if err != nil {
		t.Fatal(err)
	}
	if child.WorkflowStageExecutionID != execution.ID || child.RetryOfTaskID != parentID || child.Attempt != 2 {
		t.Fatalf("retry lost lineage: %+v", child)
	}
	execution, err = svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil || execution.MainTaskID != child.ID || execution.Attempt != 1 {
		t.Fatalf("business attempt changed: %+v %v", execution, err)
	}
	if _, err := svc.Queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: parentID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stale parent retried: %v", err)
	}
	claimed, err := svc.Queries.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{AgentID: execution.ExecutorID, PrepareLeaseSecs: 60})
	if err != nil || claimed.ID != child.ID {
		t.Fatalf("retry not claimable: %+v %v", claimed, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running' WHERE id=$1`, child.ID); err != nil {
		t.Fatal(err)
	}
	p.ActorType = "agent"
	p.ActorID = execution.ExecutorID
	p.SourceTaskID = child.ID
	if _, err := svc.SubmitResult(ctx, p); err != nil {
		t.Fatalf("retry could not submit: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, child.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, execution.MainIssueID); err != nil {
		t.Fatal(err)
	}
	readiness, err := svc.InspectReadiness(ctx, p.WorkspaceID, p.InstanceID, p.ExecutionID)
	if err != nil || !readiness.ReadyForReview {
		t.Fatalf("retry completion not recognized: %+v %v", readiness, err)
	}
}

func TestWorkflowRetryRejectsInactiveRun(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	execution, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running' WHERE id=$1`, execution.MainTaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status='cancelled' WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	failed, err := svc.TaskService.FailTask(ctx, execution.MainTaskID, "network unavailable", "", "", "provider_network", false, "")
	if err != nil || failed.Status != "failed" {
		t.Fatalf("declined retry rolled back failure: %+v %v", failed, err)
	}
	if _, err := svc.Queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: execution.MainTaskID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("inactive retry: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE retry_of_task_id=$1`, execution.MainTaskID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("orphan retry %d %v", count, err)
	}
}

func TestWorkflowRetryPolicyEnforcedAcrossFailureAndSweeper(t *testing.T) {
	for _, mode := range []string{"failure", "sweeper"} {
		t.Run(mode, func(t *testing.T) {
			pool, svc, p, _ := workflowResultFixture(t)
			ctx := context.Background()
			e, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(config_snapshot,'{retry_policy}','{"max_attempts":2,"backoff_seconds":30}') WHERE id=$1`, e.ID); err != nil {
				t.Fatal(err)
			}
			retry := func(id pgtype.UUID) {
				t.Helper()
				if mode == "failure" {
					if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running' WHERE id=$1`, id); err != nil {
						t.Fatal(err)
					}
					if _, err := svc.TaskService.FailTask(ctx, id, "Connection closed mid-response", "", "", "agent_error.provider_network", false, ""); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='failed',failure_reason='agent_error.provider_network',completed_at=now() WHERE id=$1`, id); err != nil {
						t.Fatal(err)
					}
					parent, err := svc.Queries.GetAgentTask(ctx, id)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := svc.TaskService.MaybeRetryFailedTask(ctx, parent); err != nil {
						t.Fatal(err)
					}
				}
			}
			before := time.Now()
			retry(e.MainTaskID)
			var childID pgtype.UUID
			var attempts int32
			var fireAt time.Time
			if err := pool.QueryRow(ctx, `SELECT id,max_attempts,fire_at FROM agent_task_queue WHERE retry_of_task_id=$1`, e.MainTaskID).Scan(&childID, &attempts, &fireAt); err != nil {
				t.Fatal(err)
			}
			if attempts != 2 || fireAt.Before(before.Add(29*time.Second)) {
				t.Fatalf("lost explicit policy: attempts=%d fire_at=%v", attempts, fireAt)
			}
			if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET retry_policy='{"max_attempts":5}' WHERE id=$1`, e.WorkflowStageID); err != nil {
				t.Fatal(err)
			}
			retry(childID)
			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE retry_of_task_id=$1`, childID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("provider widened frozen limit: %d %v", count, err)
			}
		})
	}
}
