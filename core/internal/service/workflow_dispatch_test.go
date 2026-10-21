package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

func TestWorkflowDispatchCreatesIssueAndTaskExactlyOnce(t *testing.T) {
	pool, svc, params := workflowExecutionFixture(t)
	ctx := context.Background()
	bus := events.New()
	svc.TaskService = NewTaskService(svc.Queries, pool, nil, bus)
	execution, err := svc.EnsureStageExecution(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	notifications := 0
	bus.Subscribe(protocol.EventIssueCreated, func(event events.Event) {
		notifications++
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE workflow_stage_execution_id = $1`, execution.ID).Scan(&count); err != nil || count != 1 {
			t.Errorf("notification preceded task commit: %d %v", count, err)
		}
	})
	action, err := svc.Queries.ClaimWorkflowExecutionAction(ctx, params.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DispatchStageAction(ctx, params.WorkspaceID, action.ID, action.ClaimToken); err != nil {
		t.Fatal(err)
	}
	if err := svc.DispatchStageAction(ctx, params.WorkspaceID, action.ID, action.ClaimToken); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("duplicate delivery accepted: %v", err)
	}
	execution, err = svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: params.WorkspaceID, ID: execution.ID})
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != "queued" || !execution.MainIssueID.Valid || !execution.MainTaskID.Valid || notifications != 1 {
		t.Fatalf("incomplete delivery: %+v, notifications=%d", execution, notifications)
	}
	var tasks int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE workflow_stage_execution_id = $1 AND issue_id = $2 AND originator_user_id = accountable_user_id`, execution.ID, execution.MainIssueID).Scan(&tasks); err != nil || tasks != 1 {
		t.Fatalf("wrong task binding/attribution: %d %v", tasks, err)
	}
	if err := svc.DispatchPending(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowDispatchRollbackAndRetry(t *testing.T) {
	pool, svc, params := workflowExecutionFixture(t)
	ctx := context.Background()
	bus := events.New()
	svc.TaskService = NewTaskService(svc.Queries, pool, nil, bus)
	execution, err := svc.EnsureStageExecution(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	action, err := svc.Queries.ClaimWorkflowExecutionAction(ctx, params.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	notifications := 0
	bus.Subscribe(protocol.EventIssueCreated, func(events.Event) { notifications++ })
	constraint := fmt.Sprintf("workflow_dispatch_%x", params.WorkspaceID.Bytes)
	if _, err := pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE workflow_stage_execution ADD CONSTRAINT %s CHECK (workspace_id <> '%s'::uuid OR status <> 'queued')`, constraint, util.UUIDToString(params.WorkspaceID))); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), "ALTER TABLE workflow_stage_execution DROP CONSTRAINT IF EXISTS "+constraint)
	})
	if err := svc.DispatchStageAction(ctx, params.WorkspaceID, action.ID, action.ClaimToken); err == nil {
		t.Fatal("expected dispatch rollback")
	}
	for _, query := range []string{`SELECT count(*) FROM issue WHERE workflow_instance_id = $1`, `SELECT count(*) FROM agent_task_queue WHERE workflow_stage_execution_id = $1`} {
		id := params.InstanceID
		if strings.Contains(query, "agent_task_queue") {
			id = execution.ID
		}
		var count int
		if err := pool.QueryRow(ctx, query, id).Scan(&count); err != nil || count != 0 {
			t.Fatalf("orphan after rollback: %d %v", count, err)
		}
	}
	if notifications != 0 {
		t.Fatal("rolled back work was published")
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE workflow_stage_execution DROP CONSTRAINT "+constraint); err != nil {
		t.Fatal(err)
	}
	if err := svc.DispatchStageAction(ctx, params.WorkspaceID, action.ID, action.ClaimToken); err != nil {
		t.Fatal(err)
	}
	if notifications != 1 {
		t.Fatalf("retry notification count: %d", notifications)
	}
}

func TestWorkflowQueuedTaskHonorsRunAndIssueGuards(t *testing.T) {
	pool, svc, params := workflowExecutionFixture(t)
	ctx := context.Background()
	svc.TaskService = NewTaskService(svc.Queries, pool, nil, events.New())
	execution, err := svc.EnsureStageExecution(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DispatchPending(ctx); err != nil {
		t.Fatal(err)
	}
	execution, err = svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: params.WorkspaceID, ID: execution.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"waiting", "cancelled", "completed"} {
		if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status = $2 WHERE id = $1`, params.InstanceID, status); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Queries.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{AgentID: params.ExecutorID, PrepareLeaseSecs: 60}); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("claimed %s Run: %v", status, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status = 'active', revision = 2 WHERE id = $1`, params.InstanceID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Queries.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{AgentID: params.ExecutorID, PrepareLeaseSecs: 60}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("claimed stale plan: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET revision = 1 WHERE id = $1`, params.InstanceID); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"backlog", "done", "cancelled"} {
		if _, err := pool.Exec(ctx, `UPDATE issue SET status = $2 WHERE id = $1`, execution.MainIssueID, status); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Queries.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{AgentID: params.ExecutorID, PrepareLeaseSecs: 60}); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("claimed %s issue: %v", status, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status = 'todo' WHERE id = $1`, execution.MainIssueID); err != nil {
		t.Fatal(err)
	}
	instance, err := svc.Queries.GetWorkflowInstanceInWorkspace(ctx, db.GetWorkflowInstanceInWorkspaceParams{ID: params.InstanceID, WorkspaceID: params.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, params.WorkspaceID, instance.CreatedBy); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Queries.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{AgentID: params.ExecutorID, PrepareLeaseSecs: 60}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("claimed after membership revocation: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id,user_id,role) VALUES ($1,$2,'owner')`, params.WorkspaceID, instance.CreatedBy); err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.TaskService.ClaimTask(ctx, params.ExecutorID)
	if err != nil || claimed == nil || claimed.WorkflowStageExecutionID != execution.ID {
		t.Fatalf("valid task not claimable: %+v %v", claimed, err)
	}
}

func TestWorkflowDispatchBlocksMissingRuntime(t *testing.T) {
	pool, svc, params := workflowExecutionFixture(t)
	ctx := context.Background()
	svc.TaskService = NewTaskService(svc.Queries, pool, nil, events.New())
	execution, err := svc.EnsureStageExecution(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent SET runtime_id = NULL WHERE id = $1`, params.ExecutorID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DispatchPending(ctx); err != nil {
		t.Fatal(err)
	}
	execution, err = svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: params.WorkspaceID, ID: execution.ID})
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != "blocked" || execution.FailureCode.String != "missing_runtime" || execution.MainIssueID.Valid {
		t.Fatalf("missing runtime did not block before issue creation: %+v", execution)
	}
}

func TestWorkflowDispatchHasNoAdminInvocationBypass(t *testing.T) {
	pool, svc, params := workflowExecutionFixture(t)
	ctx := context.Background()
	svc.TaskService = NewTaskService(svc.Queries, pool, nil, events.New())
	var adminID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name,email) VALUES ('Workflow admin',$1) RETURNING id`, "workflow-admin-"+util.UUIDToString(params.InstanceID)+"@example.com").Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id=$1`, adminID) })
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id,user_id,role) VALUES ($1,$2,'admin')`, params.WorkspaceID, adminID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET created_by=$2 WHERE id=$1`, params.InstanceID, adminID); err != nil {
		t.Fatal(err)
	}
	execution, err := svc.EnsureStageExecution(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DispatchPending(ctx); err != nil {
		t.Fatal(err)
	}
	execution, err = svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: params.WorkspaceID, ID: execution.ID})
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != "blocked" || execution.FailureCode.String != "permission_denied" || execution.MainIssueID.Valid {
		t.Fatalf("admin invoked private executor: %+v", execution)
	}
}

func TestWorkflowTranscriptKeepsExecutionIdentityAfterIssueMove(t *testing.T) {
	pool, svc, params := workflowExecutionFixture(t)
	ctx := context.Background()
	svc.TaskService = NewTaskService(svc.Queries, pool, nil, events.New())
	execution, err := svc.EnsureStageExecution(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	action, err := svc.Queries.ClaimWorkflowExecutionAction(ctx, params.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DispatchStageAction(ctx, params.WorkspaceID, action.ID, action.ClaimToken); err != nil {
		t.Fatal(err)
	}
	execution, err = svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: params.WorkspaceID, ID: execution.ID})
	if err != nil {
		t.Fatal(err)
	}
	newStage := util.MustParseUUID(uuid.NewString())
	if _, err := pool.Exec(ctx, `INSERT INTO workflow_instance_stage(id,workspace_id,workflow_instance_id,stable_key,name,position) VALUES ($1,$2,$3,'moved','Moved',1)`, newStage, params.WorkspaceID, params.InstanceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET workflow_stage_id=$2 WHERE id=$1`, execution.MainIssueID, newStage); err != nil {
		t.Fatal(err)
	}
	assertCount := func(stage pgtype.UUID, want int64) {
		t.Helper()
		count, err := svc.Queries.CountWorkflowStageRuns(ctx, db.CountWorkflowStageRunsParams{WorkspaceID: params.WorkspaceID, WorkflowInstanceID: params.InstanceID, WorkflowStageID: stage})
		if err != nil || count != want {
			t.Fatalf("count=%d want=%d err=%v", count, want, err)
		}
		rows, err := svc.Queries.ListWorkflowStageRuns(ctx, db.ListWorkflowStageRunsParams{WorkspaceID: params.WorkspaceID, WorkflowInstanceID: params.InstanceID, WorkflowStageID: stage, Limit: 25})
		if err != nil || int64(len(rows)) != want {
			t.Fatalf("list=%d want=%d err=%v", len(rows), want, err)
		}
	}
	assertCount(params.StageID, 1)
	assertCount(newStage, 0)
	// Manual, unbound invocations still use the issue's current stage.
	if _, err := pool.Exec(ctx, `INSERT INTO agent_task_queue(agent_id,issue_id,runtime_id,status) SELECT agent_id,issue_id,runtime_id,'completed' FROM agent_task_queue WHERE id=$1`, execution.MainTaskID); err != nil {
		t.Fatal(err)
	}
	assertCount(params.StageID, 1)
	assertCount(newStage, 1)
	// A dangling execution pointer must not silently become a manual invocation.
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET workflow_stage_execution_id=$2 WHERE id=$1`, execution.MainTaskID, util.MustParseUUID(uuid.NewString())); err != nil {
		t.Fatal(err)
	}
	assertCount(params.StageID, 0)
	assertCount(newStage, 1)
}

func TestWorkflowManualQueueWaitsForCurrentStage(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	execution, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET workflow_stage_execution_id=NULL WHERE id=$1`, execution.MainTaskID); err != nil {
		t.Fatal(err)
	}
	claim := func(want bool) {
		t.Helper()
		task, err := svc.Queries.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{AgentID: execution.ExecutorID, PrepareLeaseSecs: 60})
		if want {
			if err != nil || task.ID != execution.MainTaskID {
				t.Fatalf("claim current stage: %+v %v", task, err)
			}
			if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='queued',prepare_lease_expires_at=NULL WHERE id=$1`, task.ID); err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("ineligible task claimed: %+v %v", task, err)
		}
	}
	// An automatic stage cannot bypass the execution binding by enqueuing manually.
	claim(false)
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET execution_mode='manual',executor_id=NULL WHERE id=$1`, execution.WorkflowStageID); err != nil {
		t.Fatal(err)
	}
	claim(true)
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET current_stage_id=gen_random_uuid() WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	claim(false)
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET current_stage_id=$2 WHERE id=$1`, p.InstanceID, execution.WorkflowStageID); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"waiting", "completed", "cancelled"} {
		if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status=$2 WHERE id=$1`, p.InstanceID, status); err != nil {
			t.Fatal(err)
		}
		claim(false)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status='active',archived_at=now() WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	claim(false)
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET archived_at=NULL WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"backlog", "done", "cancelled"} {
		if _, err := pool.Exec(ctx, `UPDATE issue SET status=$2 WHERE id=$1`, execution.MainIssueID, status); err != nil {
			t.Fatal(err)
		}
		claim(false)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='todo' WHERE id=$1`, execution.MainIssueID); err != nil {
		t.Fatal(err)
	}
	claim(true)
	// Ordinary tasks retain their existing claim behavior outside an SOP.
	if _, err := pool.Exec(ctx, `UPDATE issue SET workflow_instance_id=NULL,workflow_stage_id=NULL WHERE id=$1`, execution.MainIssueID); err != nil {
		t.Fatal(err)
	}
	claim(true)
}

func TestWorkflowDispatchUsesFrozenInitialRetryBudget(t *testing.T) {
	for _, limit := range []int{1, 5} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			pool, svc, params := workflowExecutionFixture(t)
			ctx := context.Background()
			svc.TaskService = NewTaskService(svc.Queries, pool, nil, events.New())
			if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET retry_policy=jsonb_build_object('max_attempts',$2::int) WHERE id=$1`, params.StageID, limit); err != nil {
				t.Fatal(err)
			}
			execution, err := svc.EnsureStageExecution(ctx, params)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET retry_policy='{"max_attempts":10}' WHERE id=$1`, params.StageID); err != nil {
				t.Fatal(err)
			}
			action, err := svc.Queries.ClaimWorkflowExecutionAction(ctx, params.WorkspaceID)
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.DispatchStageAction(ctx, params.WorkspaceID, action.ID, action.ClaimToken); err != nil {
				t.Fatal(err)
			}
			var actual, attempt int
			if err := pool.QueryRow(ctx, `SELECT max_attempts,attempt FROM agent_task_queue WHERE workflow_stage_execution_id=$1`, execution.ID).Scan(&actual, &attempt); err != nil {
				t.Fatal(err)
			}
			if actual != limit || attempt != 1 {
				t.Fatalf("initial task lost frozen budget: max=%d attempt=%d", actual, attempt)
			}
		})
	}
}
