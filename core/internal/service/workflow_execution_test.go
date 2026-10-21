package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func workflowExecutionFixture(t *testing.T) (*pgxpool.Pool, *WorkflowExecutionService, EnsureWorkflowExecutionParams) {
	t.Helper()
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	agentID := createClaimCapacityFixture(t, ctx, pool)
	// The capacity fixture seeds unrelated queued tasks; SOP tests own their
	// queue and must not accidentally claim those tasks as stage work.
	if _, err := pool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID); err != nil {
		t.Fatal(err)
	}
	q := db.New(pool)
	agent, err := q.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatal(err)
	}
	var runID, stageID pgtype.UUID
	// Minimal plan identities: the execution service uses the immutable Run copy,
	// not the source template, to create its snapshot.
	if err := pool.QueryRow(ctx, `INSERT INTO workflow_instance (workspace_id, workflow_id, workflow_version_id, title, status, created_by)
 VALUES ($1, gen_random_uuid(), gen_random_uuid(), 'Execution fixture', 'active', $2) RETURNING id`, agent.WorkspaceID, agent.OwnerID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, table := range []string{"workflow_review_recovery", "workflow_rework", "workflow_gate_decision", "workflow_review_request", "workflow_artifact_revision", "workflow_artifact", "workflow_artifact_upload", "workflow_execution_result", "workflow_execution_action", "workflow_stage_execution", "workflow_instance_stage", "workflow_instance"} {
			if _, err := pool.Exec(context.Background(), fmt.Sprintf("DELETE FROM %s WHERE workspace_id = $1", table), agent.WorkspaceID); err != nil {
				t.Errorf("cleanup %s: %v", table, err)
			}
		}
	})
	if err := pool.QueryRow(ctx, `INSERT INTO workflow_instance_stage (workspace_id, workflow_instance_id, stable_key, name, position, execution_mode, executor_id, output_spec)
 VALUES ($1, $2, 'requirements', 'Requirements', 0, 'agent', $3, '{"contract":{"schema_version":"silieco.stage/v1","data":{"type":"object"}}}') RETURNING id`, agent.WorkspaceID, runID, agent.ID).Scan(&stageID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET current_stage_id = $2 WHERE id = $1`, runID, stageID); err != nil {
		t.Fatal(err)
	}
	return pool, &WorkflowExecutionService{Queries: q, TxStarter: pool}, EnsureWorkflowExecutionParams{
		WorkspaceID: agent.WorkspaceID, InstanceID: runID, StageID: stageID, ExecutorID: agent.ID, ExpectedRevision: 1, InputSnapshot: []byte(`{"target":80}`),
	}
}

func TestWorkflowExecutionConcurrentEnsureAndRecovery(t *testing.T) {
	pool, svc, params := workflowExecutionFixture(t)
	ctx := context.Background()
	const workers = 8
	start := make(chan struct{})
	results := make(chan db.WorkflowStageExecution, workers)
	failures := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			execution, err := svc.EnsureStageExecution(ctx, params)
			if err != nil {
				failures <- err
			} else {
				results <- execution
			}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var identity pgtype.UUID
	for execution := range results {
		if !identity.Valid {
			identity = execution.ID
		}
		if execution.ID != identity || execution.Attempt != 1 {
			t.Fatalf("duplicate attempt: %+v", execution)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_execution_action WHERE workspace_id = $1`, params.WorkspaceID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("dispatch intent count %d: %v", count, err)
	}
	// A newly constructed service observes the durable intent after a restart.
	recovered := &WorkflowExecutionService{Queries: db.New(pool), TxStarter: pool}
	execution, err := recovered.EnsureStageExecution(ctx, params)
	if err != nil || execution.ID != identity {
		t.Fatalf("restart created a new attempt: %v", err)
	}
	action, err := recovered.Queries.ClaimWorkflowExecutionAction(ctx, params.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if action.ExecutionID != identity || action.Attempts != 1 || !action.ClaimToken.Valid {
		t.Fatalf("invalid durable action: %+v", action)
	}
	if _, err := recovered.Queries.ClaimWorkflowExecutionAction(ctx, params.WorkspaceID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("live lease claimed twice: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_execution_action SET lease_until = now() - interval '1 second' WHERE id = $1`, action.ID); err != nil {
		t.Fatal(err)
	}
	newLease, err := recovered.Queries.ClaimWorkflowExecutionAction(ctx, params.WorkspaceID)
	if err != nil || newLease.ID != action.ID || newLease.ClaimToken == action.ClaimToken || newLease.Attempts != 2 {
		t.Fatalf("failed to recover expired lease: %+v %v", newLease, err)
	}
	oldCount, err := recovered.Queries.CompleteWorkflowExecutionAction(ctx, db.CompleteWorkflowExecutionActionParams{WorkspaceID: params.WorkspaceID, ID: action.ID, ClaimToken: action.ClaimToken})
	if err != nil || oldCount != 0 {
		t.Fatalf("late worker acknowledged new lease: %d %v", oldCount, err)
	}
	delivered, err := recovered.Queries.CompleteWorkflowExecutionAction(ctx, db.CompleteWorkflowExecutionActionParams{WorkspaceID: params.WorkspaceID, ID: newLease.ID, ClaimToken: newLease.ClaimToken})
	if err != nil || delivered != 1 {
		t.Fatalf("current lease completion failed: %d %v", delivered, err)
	}
	if _, err := recovered.Queries.ClaimWorkflowExecutionAction(ctx, params.WorkspaceID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("delivered action claimed again: %v", err)
	}
}

func TestWorkflowExecutionRejectsStaleAndInactiveRun(t *testing.T) {
	pool, svc, params := workflowExecutionFixture(t)
	ctx := context.Background()
	stale := params
	stale.ExpectedRevision = 2
	if _, err := svc.EnsureStageExecution(ctx, stale); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("accepted stale plan: %v", err)
	}
	other := params
	other.WorkspaceID = util.MustParseUUID("12345678-1234-1234-1234-123456789abc")
	if _, err := svc.EnsureStageExecution(ctx, other); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-workspace access: %v", err)
	}
	if _, err := svc.EnsureStageExecution(ctx, params); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"waiting", "cancelled", "completed"} {
		if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status = $2 WHERE id = $1`, params.InstanceID, state); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.EnsureStageExecution(ctx, params); !errors.Is(err, ErrWorkflowExecutionConflict) {
			t.Fatalf("accepted %s Run: %v", state, err)
		}
		if _, err := svc.Queries.ClaimWorkflowExecutionAction(ctx, params.WorkspaceID); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("claimed %s Run: %v", state, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status = 'active', archived_at = now() WHERE id = $1`, params.InstanceID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Queries.ClaimWorkflowExecutionAction(ctx, params.WorkspaceID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("claimed archived Run: %v", err)
	}
}

func TestWorkflowExecutionAndIntentRollbackTogether(t *testing.T) {
	pool, svc, params := workflowExecutionFixture(t)
	ctx := context.Background()
	// Force only this fixture's intent insert to fail after the attempt insert.
	constraint := "workflow_test_" + fmt.Sprintf("%x", params.WorkspaceID.Bytes)
	statement := fmt.Sprintf(`ALTER TABLE workflow_execution_action ADD CONSTRAINT %s CHECK (workspace_id <> '%s'::uuid)`, constraint, util.UUIDToString(params.WorkspaceID))
	if _, err := pool.Exec(ctx, statement); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "ALTER TABLE workflow_execution_action DROP CONSTRAINT "+constraint); err != nil {
			t.Error(err)
		}
	})
	if _, err := svc.EnsureStageExecution(ctx, params); err == nil {
		t.Fatal("expected failed dispatch intent")
	}
	executions, err := svc.Queries.ListWorkflowStageExecutions(ctx, db.ListWorkflowStageExecutionsParams{WorkspaceID: params.WorkspaceID, WorkflowInstanceID: params.InstanceID})
	if err != nil || len(executions) != 0 {
		t.Fatalf("orphan attempt survived rollback: %+v %v", executions, err)
	}
}

func TestWorkflowExecutionWorkspaceCleanup(t *testing.T) {
	pool, svc, params := workflowExecutionFixture(t)
	ctx := context.Background()
	if _, err := svc.EnsureStageExecution(ctx, params); err != nil {
		t.Fatal(err)
	}
	if err := svc.Queries.DeleteWorkspace(ctx, params.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"workflow_stage_execution", "workflow_execution_action"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE workspace_id = $1", params.WorkspaceID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("orphan %s rows: %d %v", table, count, err)
		}
	}
}

func TestWorkflowExecutionProjectCleanup(t *testing.T) {
	pool, svc, params := workflowExecutionFixture(t)
	ctx := context.Background()
	var projectID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO project (workspace_id, title) VALUES ($1, 'SOP cleanup') RETURNING id`, params.WorkspaceID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID); err != nil {
			t.Error(err)
		}
	})
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET project_id = $2 WHERE id = $1`, params.InstanceID, projectID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EnsureStageExecution(ctx, params); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	q := svc.Queries.WithTx(tx)
	if err := q.DeleteProjectWorkflowExecutionActions(ctx, db.DeleteProjectWorkflowExecutionActionsParams{WorkspaceID: params.WorkspaceID, ProjectID: projectID}); err != nil {
		t.Fatal(err)
	}
	if err := q.DeleteProjectWorkflowStageExecutions(ctx, db.DeleteProjectWorkflowStageExecutionsParams{WorkspaceID: params.WorkspaceID, ProjectID: projectID}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"workflow_stage_execution", "workflow_execution_action"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE workspace_id = $1", params.WorkspaceID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("orphan %s rows: %d %v", table, count, err)
		}
	}
}

func TestWorkflowExecutionConcurrentLeaseAndCancellation(t *testing.T) {
	_, svc, params := workflowExecutionFixture(t)
	ctx := context.Background()
	if _, err := svc.EnsureStageExecution(ctx, params); err != nil {
		t.Fatal(err)
	}
	const workers = 8
	start := make(chan struct{})
	claims := make(chan db.WorkflowExecutionAction, workers)
	failures := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			claim, err := svc.Queries.ClaimWorkflowExecutionAction(ctx, params.WorkspaceID)
			if err == nil {
				claims <- claim
			} else if !errors.Is(err, pgx.ErrNoRows) {
				failures <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(claims)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	if len(claims) != 1 {
		t.Fatalf("expected one lease, got %d", len(claims))
	}
	claim := <-claims
	if err := svc.Queries.CancelWorkflowExecutionActions(ctx, db.CancelWorkflowExecutionActionsParams{WorkspaceID: params.WorkspaceID, WorkflowInstanceID: params.InstanceID}); err != nil {
		t.Fatal(err)
	}
	count, err := svc.Queries.CompleteWorkflowExecutionAction(ctx, db.CompleteWorkflowExecutionActionParams{WorkspaceID: params.WorkspaceID, ID: claim.ID, ClaimToken: claim.ClaimToken})
	if err != nil || count != 0 {
		t.Fatalf("cancelled lease acknowledged: %d %v", count, err)
	}
	if _, err := svc.Queries.ClaimWorkflowExecutionAction(ctx, params.WorkspaceID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cancelled action requeued: %v", err)
	}
}

func TestWorkflowExecutionWaitsForParentDeletion(t *testing.T) {
	for _, resource := range []string{"workspace", "project"} {
		t.Run(resource, func(t *testing.T) {
			pool, svc, params := workflowExecutionFixture(t)
			ctx := context.Background()
			var projectID pgtype.UUID
			if resource == "project" {
				if err := pool.QueryRow(ctx, `INSERT INTO project (workspace_id,title) VALUES ($1,'Deletion barrier') RETURNING id`, params.WorkspaceID).Scan(&projectID); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID) })
				if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET project_id = $2 WHERE id = $1`, params.InstanceID, projectID); err != nil {
					t.Fatal(err)
				}
			}
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			q := svc.Queries.WithTx(tx)
			if resource == "workspace" {
				if _, err := q.LockWorkspaceForDelete(ctx, params.WorkspaceID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := q.LockProjectForDelete(ctx, db.LockProjectForDeleteParams{ID: projectID, WorkspaceID: params.WorkspaceID}); err != nil {
					t.Fatal(err)
				}
			}
			deadline, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
			defer cancel()
			if _, err := svc.EnsureStageExecution(deadline, params); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("did not wait behind %s deletion: %v", resource, err)
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			executions, err := svc.Queries.ListWorkflowStageExecutions(ctx, db.ListWorkflowStageExecutionsParams{WorkspaceID: params.WorkspaceID, WorkflowInstanceID: params.InstanceID})
			if err != nil || len(executions) != 0 {
				t.Fatalf("execution escaped deletion barrier: %+v %v", executions, err)
			}
		})
	}
}

func TestWorkflowExecutionFreezesRetryPolicy(t *testing.T) {
	pool, svc, params := workflowExecutionFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET retry_policy='{"max_attempts":2,"backoff_seconds":0}' WHERE workflow_instance_id=$1`, params.InstanceID); err != nil {
		t.Fatal(err)
	}
	execution, err := svc.EnsureStageExecution(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	var ceiling int
	if err := pool.QueryRow(ctx, `SELECT (config_snapshot->'retry_policy'->>'max_attempts')::int FROM workflow_stage_execution WHERE id=$1`, execution.ID).Scan(&ceiling); err != nil || ceiling != 2 {
		t.Fatalf("missing frozen retry policy: %d %v", ceiling, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET retry_policy='{"max_attempts":5}' WHERE workflow_instance_id=$1`, params.InstanceID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT (config_snapshot->'retry_policy'->>'max_attempts')::int FROM workflow_stage_execution WHERE id=$1`, execution.ID).Scan(&ceiling); err != nil || ceiling != 2 {
		t.Fatalf("retry snapshot changed: %d %v", ceiling, err)
	}
}
