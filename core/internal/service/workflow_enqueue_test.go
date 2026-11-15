package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/silieco-ai/silieco/core/internal/events"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

func TestWorkflowOrdinaryEnqueueSharesRunTransaction(t *testing.T) {
	testWorkflowEnqueueSharesRunTransaction(t, "ordinary")
}

func TestWorkflowMentionEnqueueSharesRunTransaction(t *testing.T) {
	testWorkflowEnqueueSharesRunTransaction(t, "mention")
}

func TestWorkflowDeferredEnqueueSharesRunTransaction(t *testing.T) {
	testWorkflowEnqueueSharesRunTransaction(t, "deferred")
}

func testWorkflowEnqueueSharesRunTransaction(t *testing.T, mode string) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	execution, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_run SET status='completed' WHERE id=$1`, execution.MainRunID); err != nil {
		t.Fatal(err)
	}
	task, err := svc.Queries.GetTaskInWorkspace(ctx, db.GetTaskInWorkspaceParams{ID: execution.MainTaskID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if mode == "mention" {
		// The explicit target remains the executor even when the task belongs
		// to a member. Mentions must not silently become assignee calls.
		if _, err := pool.Exec(ctx, `UPDATE task SET assignee_type='member',assignee_id=$1 WHERE id=$2`, p.ActorID, task.ID); err != nil {
			t.Fatal(err)
		}
		task, err = svc.Queries.GetTaskInWorkspace(ctx, db.GetTaskInWorkspaceParams{ID: task.ID, WorkspaceID: p.WorkspaceID})
		if err != nil {
			t.Fatal(err)
		}
	}
	fireAt := time.Now().Add(time.Hour).Truncate(time.Microsecond)
	enqueue := func(callCtx context.Context) (db.AgentRun, error) {
		if mode == "deferred" {
			return svc.AgentRunService.EnqueueDeferredAssigneeFallback(callCtx, task, execution.ExecutorID, pgtype.UUID{}, execution.MainRunID, pgtype.UUID{}, fireAt)
		}
		if mode == "mention" {
			return svc.AgentRunService.enqueueMentionRun(callCtx, task, execution.ExecutorID, pgtype.UUID{}, true, pgtype.UUID{}, true, "review handoff", p.ActorID, pgtype.UUID{})
		}
		return svc.AgentRunService.enqueueTaskRun(callCtx, task, pgtype.UUID{}, false, "", p.ActorID, pgtype.UUID{})
	}
	if _, err := enqueue(ctx); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("automatic stage got unbound call: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET execution_mode='manual',executor_id=NULL WHERE id=$1`, execution.WorkflowStageID); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := svc.Queries.WithTx(tx).LockWorkflowInstanceInWorkspace(ctx, db.LockWorkflowInstanceInWorkspaceParams{ID: p.InstanceID, WorkspaceID: p.WorkspaceID}); err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if _, err := enqueue(short); err == nil {
		t.Fatal("enqueue escaped Run lock")
	}
	if _, err := tx.Exec(ctx, `UPDATE workflow_instance SET status='completed' WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := enqueue(ctx); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("closed Run got call: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status='active' WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	// A single-connection pool exposes accidental out-of-transaction reads
	// while enqueue owns the only connection.
	config := pool.Config()
	config.MaxConns = 1
	config.MinConns = 0
	single, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(single.Close)
	svc.AgentRunService = NewAgentRunService(db.New(single), single, nil, svc.AgentRunService.Bus)
	notifications := 0
	svc.AgentRunService.Bus.Subscribe(protocol.EventRunQueued, func(event events.Event) {
		notifications++
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_run WHERE task_id=$1 AND status='queued'`, task.ID).Scan(&count); err != nil || count != 1 {
			t.Errorf("enqueue event preceded commit: %d %v", count, err)
		}
	})
	deadline, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	agentRun, err := enqueue(deadline)
	expectedNotifications := 1
	if mode == "deferred" {
		expectedNotifications = 0
		if agentRun.Status != "deferred" || agentRun.EscalationForRunID != execution.MainRunID || !agentRun.FireAt.Time.Equal(fireAt) {
			t.Fatalf("deferred metadata lost: %+v", agentRun)
		}
	}
	if err != nil || !agentRun.ID.Valid || notifications != expectedNotifications {
		t.Fatalf("manual enqueue: %+v %v events=%d", agentRun, err, notifications)
	}
	if mode == "mention" && (agentRun.AgentID != execution.ExecutorID || !agentRun.IsLeaderRun || !agentRun.ForceFreshSession || agentRun.HandoffNote.String != "review handoff") {
		t.Fatalf("mention metadata lost: %+v", agentRun)
	}
}
