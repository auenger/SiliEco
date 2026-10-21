package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func TestWorkflowManualRollbackStartsFreshAgentAttempt(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	if _, err := svc.SubmitResult(ctx, p); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(config_snapshot,'{gate}','{"type":"none"}') WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	var manualID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO workflow_instance_stage(workspace_id,workflow_instance_id,stable_key,name,position,execution_mode,rollback_stage_key) VALUES($1,$2,'manual_review','Manual review',1,'manual','requirements') RETURNING id`, p.WorkspaceID, p.InstanceID).Scan(&manualID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, first.MainIssueID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, first.MainTaskID); err != nil {
		t.Fatal(err)
	}
	if err := svc.AcceptAutomaticExecution(ctx, p.WorkspaceID, p.InstanceID, first.ID); err != nil {
		t.Fatal(err)
	}
	manualIssue, err := svc.Queries.CreateIssue(ctx, db.CreateIssueParams{WorkspaceID: p.WorkspaceID, Title: "Manual evidence before rollback", Status: "done", Priority: "none", CreatorType: "member", CreatorID: p.ActorID, Number: 9903, WorkflowInstanceID: p.InstanceID, WorkflowStageID: manualID})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	q := svc.Queries.WithTx(tx)
	run, err := lockWorkflowExecutionRun(ctx, q, p.WorkspaceID, p.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	source, err := q.GetWorkflowInstanceStage(ctx, db.GetWorkflowInstanceStageParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: p.InstanceID, ID: manualID})
	if err != nil {
		t.Fatal(err)
	}
	target, err := q.GetWorkflowInstanceStage(ctx, db.GetWorkflowInstanceStageParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: p.InstanceID, ID: first.WorkflowStageID})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := InvalidateWorkflowForReworkInTx(ctx, q, WorkflowReworkInvalidationParams{Run: run, Source: source, Target: target, SourceStateToken: pgtype.Text{String: strings.Repeat("a", 64), Valid: true}, ActorType: "member", ActorID: p.ActorID, Reason: "Correct requirements after manual review"})
	if err != nil {
		t.Fatal(err)
	}
	if effects.Record.SourceExecutionID.Valid || effects.Record.SourceStageID != manualID || effects.Record.TargetStageID != target.ID {
		t.Fatalf("manual rollback has incorrect audit origin: %+v", effects.Record)
	}
	if _, err := q.UpdateWorkflowInstanceState(ctx, db.UpdateWorkflowInstanceStateParams{WorkspaceID: p.WorkspaceID, ID: run.ID, Status: pgtype.Text{String: "active", Valid: true}, CurrentStageID: target.ID}); err != nil {
		t.Fatal(err)
	}
	next, err := svc.EnsureStageExecutionInTx(ctx, q, EnsureWorkflowExecutionParams{WorkspaceID: p.WorkspaceID, InstanceID: run.ID, StageID: target.ID, ExpectedRevision: run.Revision, ExecutorID: target.ExecutorID, InputSnapshot: run.Inputs})
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == first.ID || next.Attempt != 2 || next.ResultRevision != 0 || next.Acceptance != nil {
		t.Fatalf("manual rollback reused accepted delivery: %+v", next)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	old, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: first.ID})
	if err != nil || old.Status != "cancelled" || old.FailureCode.String != "rework_invalidated" {
		t.Fatalf("previous delivery still accepted: %+v %v", old, err)
	}
	issue, err := svc.Queries.GetIssue(ctx, manualIssue.ID)
	if err != nil || issue.WorkflowInvalidatedByReworkID != effects.Record.ID || issue.Status != "done" {
		t.Fatalf("manual history lost: %+v %v", issue, err)
	}
	var intents int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_execution_action WHERE execution_id=$1 AND kind='dispatch' AND status='pending'`, next.ID).Scan(&intents); err != nil || intents != 1 {
		t.Fatalf("fresh dispatch missing: %d %v", intents, err)
	}
}

func TestWorkflowReworkFailureRetryLocksRunBeforeCall(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	execution, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running',max_attempts=3 WHERE id=$1`, execution.MainTaskID); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	q := svc.Queries.WithTx(tx)
	if _, err := lockWorkflowExecutionRun(ctx, q, p.WorkspaceID, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	var holderPID int32
	if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := svc.TaskService.FailTask(ctx, execution.MainTaskID, "runtime offline", "", "", "runtime_offline", false, "")
		done <- err
	}()
	blocked := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, holderPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("failure retry did not wait for the Run lock")
	}
	// A task-first implementation already holds this row while waiting for
	// the Run. Rework must be able to cancel it without a lock cycle.
	if _, err := tx.Exec(ctx, `SELECT id FROM agent_task_queue WHERE id=$1 FOR UPDATE NOWAIT`, execution.MainTaskID); err != nil {
		t.Fatalf("failure retry locked its call before the Run: %v", err)
	}
	if err := q.InvalidateWorkflowStageIssuesForRework(ctx, db.InvalidateWorkflowStageIssuesForReworkParams{WorkspaceID: p.WorkspaceID, InstanceID: p.InstanceID, Position: 0, ReworkID: p.ExecutionID}); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE agent_task_queue SET status='cancelled' WHERE id=$1`, execution.MainTaskID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var children int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id=$1`, execution.MainTaskID).Scan(&children); err != nil || children != 0 {
		t.Fatalf("cancelled work retried: children=%d error=%v", children, err)
	}
}

func TestWorkflowReworkInvalidatesDownstreamAndCreatesOneAttempt(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	if _, err := svc.SubmitResult(ctx, p); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(config_snapshot,'{gate}','{"type":"none"}') WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET gate='{"type":"none"}' WHERE id=$1`, first.WorkflowStageID); err != nil {
		t.Fatal(err)
	}
	var secondStage pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO workflow_instance_stage(workspace_id,workflow_instance_id,stable_key,name,position,execution_mode,executor_id,output_spec,gate,rollback_stage_key) VALUES($1,$2,'implementation','Implementation',1,'agent',$3,'{"contract":{"schema_version":"silieco.stage/v1","data":{"type":"object"}}}','{"type":"none"}','requirements') RETURNING id`, p.WorkspaceID, p.InstanceID, first.ExecutorID).Scan(&secondStage); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, first.MainIssueID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, first.MainTaskID); err != nil {
		t.Fatal(err)
	}
	if err := svc.AcceptAutomaticExecution(ctx, p.WorkspaceID, p.InstanceID, first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := svc.Queries.GetLatestWorkflowStageExecution(ctx, db.GetLatestWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: p.InstanceID, WorkflowStageID: secondStage})
	if err != nil {
		t.Fatal(err)
	}
	action, err := svc.Queries.ClaimWorkflowExecutionAction(ctx, p.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DispatchStageAction(ctx, p.WorkspaceID, action.ID, action.ClaimToken); err != nil {
		t.Fatal(err)
	}
	second, err = svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: second.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_token(token_hash,task_id,agent_id,workspace_id,user_id,expires_at) VALUES('rework-test-token',$1,$2,$3,$4,now()+interval '1 hour')`, second.MainTaskID, first.ExecutorID, p.WorkspaceID, p.ActorID); err != nil {
		t.Fatal(err)
	}
	request := ReworkWorkflowExecutionParams{WorkspaceID: p.WorkspaceID, InstanceID: p.InstanceID, SourceExecutionID: second.ID, TargetStageID: first.WorkflowStageID, ActorID: p.ActorID, ExpectedRevision: 1, Reason: "Revise requirements"}
	// Invalid target configuration must roll back cancellation and token revocation.
	if _, err := pool.Exec(ctx, `UPDATE agent SET archived_at=now() WHERE id=$1`, first.ExecutorID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReworkExecution(ctx, request); err == nil {
		t.Fatal("archived target executor accepted")
	}
	unchanged, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: first.ID})
	if err != nil || unchanged.Status != "succeeded" {
		t.Fatalf("failed rework invalidated source: %+v %v", unchanged, err)
	}
	unchangedCall, err := svc.Queries.GetAgentTask(ctx, second.MainTaskID)
	if err != nil || unchangedCall.Status != "queued" {
		t.Fatalf("failed rework cancelled call: %+v %v", unchangedCall, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent SET archived_at=NULL WHERE id=$1`, first.ExecutorID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan db.WorkflowStageExecution, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			next, err := svc.ReworkExecution(ctx, request)
			if err != nil {
				t.Error(err)
			}
			if err != nil || next.Execution == nil {
				return
			}
			results <- *next.Execution
		}()
	}
	wg.Wait()
	close(results)
	var next db.WorkflowStageExecution
	for row := range results {
		if !row.ID.Valid || (next.ID.Valid && row.ID != next.ID) {
			t.Fatal("rework duplicated or failed")
		}
		next = row
	}
	if next.Attempt != 2 || next.Status != "pending" || next.ResultRevision != 0 || next.MainIssueID.Valid || next.ReworkSourceExecutionID != second.ID {
		t.Fatalf("invalid rework attempt: %+v", next)
	}
	for _, id := range []pgtype.UUID{first.ID, second.ID} {
		stale, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: id})
		if err != nil || stale.Status != "cancelled" || stale.FailureCode.String != "rework_invalidated" {
			t.Fatalf("old execution active: %+v %v", stale, err)
		}
	}
	oldCall, err := svc.Queries.GetAgentTask(ctx, second.MainTaskID)
	if err != nil || oldCall.Status != "cancelled" {
		t.Fatalf("old call active: %+v %v", oldCall, err)
	}
	var tokens int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM task_token WHERE task_id=$1`, second.MainTaskID).Scan(&tokens); err != nil || tokens != 0 {
		t.Fatalf("old credentials retained: %d %v", tokens, err)
	}
	if _, err := svc.SubmitResult(ctx, p); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("old result accepted: %v", err)
	}
	request.Reason = "different request"
	if _, err := svc.ReworkExecution(ctx, request); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("replay changed reason: %v", err)
	}
	// A downstream execution cannot be resumed against the invalidated predecessor.
	run, err := svc.Queries.GetWorkflowInstanceInWorkspace(ctx, db.GetWorkflowInstanceInWorkspaceParams{WorkspaceID: p.WorkspaceID, ID: p.InstanceID})
	if err != nil {
		t.Fatal(err)
	}
	stage, err := svc.Queries.GetWorkflowInstanceStage(ctx, db.GetWorkflowInstanceStageParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: p.InstanceID, ID: secondStage})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.resolveAcceptedInputResult(ctx, svc.Queries, run, stage, "requirements"); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("stale predecessor input consumed: %v", err)
	}
	action, err = svc.Queries.ClaimWorkflowExecutionAction(ctx, p.WorkspaceID)
	if err != nil || action.ExecutionID != next.ID {
		t.Fatalf("wrong new dispatch: %+v %v", action, err)
	}
	if err := svc.DispatchStageAction(ctx, p.WorkspaceID, action.ID, action.ClaimToken); err != nil {
		t.Fatal(err)
	}
	fresh, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: next.ID})
	if err != nil || fresh.MainIssueID == first.MainIssueID {
		t.Fatalf("rework reused old task: %+v %v", fresh, err)
	}
}

func TestWorkflowReworkInvalidatesManualDownstreamWork(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	current, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	var manualStage pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO workflow_instance_stage(workspace_id,workflow_instance_id,stable_key,name,position,execution_mode,gate) VALUES($1,$2,'manual-check','Manual check',1,'manual','{"type":"none"}') RETURNING id`, p.WorkspaceID, p.InstanceID).Scan(&manualStage); err != nil {
		t.Fatal(err)
	}
	agent, err := svc.Queries.GetAgent(ctx, current.ExecutorID)
	if err != nil {
		t.Fatal(err)
	}
	oldIssue, err := svc.Queries.CreateIssue(ctx, db.CreateIssueParams{WorkspaceID: p.WorkspaceID, Title: "Manual delivery before rework", Status: "todo", Priority: "none", CreatorType: "member", CreatorID: p.ActorID, Number: 9901, WorkflowInstanceID: p.InstanceID, WorkflowStageID: manualStage})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := svc.Queries.CreateAgentTask(ctx, db.CreateAgentTaskParams{AgentID: current.ExecutorID, RuntimeID: agent.RuntimeID, IssueID: oldIssue.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='failed',failure_reason='daemon_offline' WHERE id=$1`, failed.ID); err != nil {
		t.Fatal(err)
	}
	call, err := svc.Queries.CreateAgentTask(ctx, db.CreateAgentTaskParams{AgentID: current.ExecutorID, RuntimeID: agent.RuntimeID, IssueID: oldIssue.ID})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := svc.Queries.CreateWorkflowGateDecision(ctx, db.CreateWorkflowGateDecisionParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: p.InstanceID, FromStageID: manualStage, Outcome: "approved", ActorType: "member", ActorID: p.ActorID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO task_token(token_hash,task_id,agent_id,workspace_id,user_id,expires_at) VALUES('manual-rework-token',$1,$2,$3,$4,now()+interval '1 hour')`, call.ID, current.ExecutorID, p.WorkspaceID, p.ActorID); err != nil {
		t.Fatal(err)
	}
	request := ReworkWorkflowExecutionParams{WorkspaceID: p.WorkspaceID, InstanceID: p.InstanceID, SourceExecutionID: current.ID, TargetStageID: current.WorkflowStageID, ActorID: p.ActorID, ExpectedRevision: 1, Reason: "Restart with new requirements"}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running' WHERE id=$1`, call.ID); err != nil {
		t.Fatal(err)
	}
	retryTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer retryTx.Rollback(ctx)
	child, err := svc.Queries.WithTx(retryTx).CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: failed.ID})
	if err != nil {
		t.Fatal(err)
	}
	var retryPID int32
	if err := retryTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&retryPID); err != nil {
		t.Fatal(err)
	}
	reworkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := svc.ReworkExecution(reworkCtx, request); done <- err }()
	blocked := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, retryPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("rework did not wait for the concurrent retry's issue lock")
	}
	if err := retryTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	retried, err := svc.Queries.GetAgentTask(ctx, child.ID)
	if err != nil || retried.Status != "cancelled" {
		t.Fatalf("concurrent retry escaped rework: %+v %v", retried, err)
	}

	invalid, err := svc.Queries.GetIssue(ctx, oldIssue.ID)
	if err != nil || !invalid.WorkflowInvalidatedByReworkID.Valid || invalid.Status != "todo" {
		t.Fatalf("manual issue history lost or not invalidated: %+v %v", invalid, err)
	}
	stopped, err := svc.Queries.GetAgentTask(ctx, call.ID)
	if err != nil || stopped.Status != "cancelled" {
		t.Fatalf("manual downstream call not cancelled: %+v %v", stopped, err)
	}
	var tokens int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM task_token WHERE task_id=$1`, call.ID).Scan(&tokens); err != nil || tokens != 0 {
		t.Fatalf("manual credentials survived: %d %v", tokens, err)
	}
	var invalidating pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT invalidated_by_rework_id FROM workflow_gate_decision WHERE id=$1`, decision.ID).Scan(&invalidating); err != nil || invalidating != invalid.WorkflowInvalidatedByReworkID {
		t.Fatalf("manual decision not invalidated: %v %v", invalidating, err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = LockIssueWorkflowMutation(ctx, svc.Queries.WithTx(tx), p.WorkspaceID, oldIssue.ID, pgtype.UUID{})
	_ = tx.Rollback(ctx)
	if !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("invalidated manual issue mutable: %v", err)
	}
	issueSvc := &IssueService{Queries: svc.Queries, TxStarter: pool}
	if _, err := issueSvc.Create(ctx, IssueCreateParams{WorkspaceID: p.WorkspaceID, Title: "Child of invalidated work", Status: "todo", Priority: "none", CreatorType: "member", CreatorID: p.ActorID, ParentIssueID: oldIssue.ID}, IssueCreateOpts{}); !errors.Is(err, ErrWorkflowAssignmentInvalid) {
		t.Fatalf("invalidated parent accepted new work: %v", err)
	}
	if _, err := svc.Queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: failed.ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("invalidated manual issue retried: %v", err)
	}
	fresh, err := svc.Queries.CreateIssue(ctx, db.CreateIssueParams{WorkspaceID: p.WorkspaceID, Title: "Manual delivery after rework", Status: "done", Priority: "none", CreatorType: "member", CreatorID: p.ActorID, Number: 9902, WorkflowInstanceID: p.InstanceID, WorkflowStageID: manualStage})
	if err != nil || fresh.WorkflowInvalidatedByReworkID.Valid {
		t.Fatalf("new manual work invalid: %+v %v", fresh, err)
	}
	tasks, err := svc.Queries.LockWorkflowStageIssues(ctx, db.LockWorkflowStageIssuesParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: p.InstanceID, WorkflowStageID: manualStage})
	if err != nil || len(tasks) != 1 || tasks[0].ID != fresh.ID {
		t.Fatalf("stale manual issue counted as required delivery: %+v %v", tasks, err)
	}
	if _, err := svc.ReworkExecution(ctx, request); err != nil {
		t.Fatal(err)
	}
	fresh, err = svc.Queries.GetIssue(ctx, fresh.ID)
	if err != nil || fresh.WorkflowInvalidatedByReworkID.Valid {
		t.Fatal("idempotent replay invalidated newly created work")
	}
}

func TestWorkflowAgentReworkToManualPreservesHistoryWithoutFakeExecution(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	source, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET position=1,rollback_stage_key='manual_input' WHERE id=$1`, source.WorkflowStageID); err != nil {
		t.Fatal(err)
	}
	var manualID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO workflow_instance_stage(workspace_id,workflow_instance_id,stable_key,name,position,execution_mode) VALUES($1,$2,'manual_input','Manual input',0,'manual') RETURNING id`, p.WorkspaceID, p.InstanceID).Scan(&manualID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(config_snapshot,'{rollback_stage_key}','"manual_input"') WHERE id=$1`, source.ID); err != nil {
		t.Fatal(err)
	}
	historical, err := svc.Queries.CreateIssue(ctx, db.CreateIssueParams{WorkspaceID: p.WorkspaceID, Title: "Previous manual input", Status: "done", Priority: "none", CreatorType: "member", CreatorID: p.ActorID, Number: 9908, WorkflowInstanceID: p.InstanceID, WorkflowStageID: manualID})
	if err != nil {
		t.Fatal(err)
	}
	request := ReworkWorkflowExecutionParams{WorkspaceID: p.WorkspaceID, InstanceID: p.InstanceID, SourceExecutionID: source.ID, TargetStageID: manualID, ActorID: p.ActorID, ExpectedRevision: source.PlanRevision, Reason: "Correct the manual input"}
	var wg sync.WaitGroup
	records := make(chan WorkflowExecutionReworkResult, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := svc.ReworkExecution(ctx, request)
			if err != nil {
				t.Error(err)
				return
			}
			records <- result
		}()
	}
	wg.Wait()
	close(records)
	var recordID pgtype.UUID
	for result := range records {
		if result.Execution != nil || !result.Record.ID.Valid || (recordID.Valid && recordID != result.Record.ID) {
			t.Fatalf("duplicate or fabricated execution: %+v", result)
		}
		recordID = result.Record.ID
	}
	if !recordID.Valid {
		t.Fatal("no rework record")
	}
	run, err := svc.Queries.GetWorkflowInstanceInWorkspace(ctx, db.GetWorkflowInstanceInWorkspaceParams{WorkspaceID: p.WorkspaceID, ID: p.InstanceID})
	if err != nil || run.CurrentStageID != manualID || run.Status != "active" {
		t.Fatalf("manual target not active: %+v %v", run, err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_stage_execution WHERE workflow_instance_id=$1`, p.InstanceID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("fabricated execution: %d %v", count, err)
	}
	old, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: source.ID})
	if err != nil || old.Status != "cancelled" || old.FailureCode.String != "rework_invalidated" {
		t.Fatalf("old execution survived: %+v %v", old, err)
	}
	for _, id := range []pgtype.UUID{historical.ID, source.MainIssueID} {
		issue, err := svc.Queries.GetIssue(ctx, id)
		if err != nil || issue.WorkflowInvalidatedByReworkID != recordID {
			t.Fatalf("old work still valid: %+v %v", issue, err)
		}
	}
	if _, err := svc.SubmitResult(ctx, p); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("old executor submitted after rollback: %v", err)
	}
	request.Reason = "Changed reason"
	if _, err := svc.ReworkExecution(ctx, request); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("changed replay accepted: %v", err)
	}
}

func TestWorkflowReworkPolicyUsesFrozenLimitAndCountsInitiations(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	source, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET rework_policy='{"max_reworks":1}' WHERE id=$1`, source.WorkflowStageID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(config_snapshot,'{rework_policy}','{"max_reworks":1}') WHERE id=$1`, source.ID); err != nil {
		t.Fatal(err)
	}
	request := ReworkWorkflowExecutionParams{WorkspaceID: p.WorkspaceID, InstanceID: p.InstanceID, SourceExecutionID: source.ID, TargetStageID: source.WorkflowStageID, ActorID: p.ActorID, ExpectedRevision: source.PlanRevision, Reason: "Use the single allowed rework"}
	first, err := svc.ReworkExecution(ctx, request)
	if err != nil || first.Execution == nil {
		t.Fatalf("allowed rework failed: %+v %v", first, err)
	}
	replay, err := svc.ReworkExecution(ctx, request)
	if err != nil || replay.Record.ID != first.Record.ID {
		t.Fatalf("budget changed replay: %+v %v", replay, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET rework_policy='{"max_reworks":100}' WHERE id=$1`, source.WorkflowStageID); err != nil {
		t.Fatal(err)
	}
	request.SourceExecutionID = first.Execution.ID
	request.Reason = "Exceed the frozen budget"
	if _, err := svc.ReworkExecution(ctx, request); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("exhausted limit bypassed: %v", err)
	}
	current, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: first.Execution.ID})
	if err != nil || current.Status != "pending" {
		t.Fatalf("rejected rework changed current execution: %+v %v", current, err)
	}
	count, err := svc.Queries.CountWorkflowStageReworks(ctx, db.CountWorkflowStageReworksParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: p.InstanceID, SourceStageID: source.WorkflowStageID})
	if err != nil || count != 1 {
		t.Fatalf("rejected rework changed audit: %d %v", count, err)
	}
}
