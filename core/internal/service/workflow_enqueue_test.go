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
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, execution.MainTaskID); err != nil {
		t.Fatal(err)
	}
	issue, err := svc.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: execution.MainIssueID, WorkspaceID: p.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if mode == "mention" {
		// The explicit target remains the executor even when the issue belongs
		// to a member. Mentions must not silently become assignee calls.
		if _, err := pool.Exec(ctx, `UPDATE issue SET assignee_type='member',assignee_id=$1 WHERE id=$2`, p.ActorID, issue.ID); err != nil {
			t.Fatal(err)
		}
		issue, err = svc.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issue.ID, WorkspaceID: p.WorkspaceID})
		if err != nil {
			t.Fatal(err)
		}
	}
	fireAt := time.Now().Add(time.Hour).Truncate(time.Microsecond)
	enqueue := func(callCtx context.Context) (db.AgentTaskQueue, error) {
		if mode == "deferred" {
			return svc.TaskService.EnqueueDeferredAssigneeFallback(callCtx, issue, execution.ExecutorID, pgtype.UUID{}, execution.MainTaskID, pgtype.UUID{}, fireAt)
		}
		if mode == "mention" {
			return svc.TaskService.enqueueMentionTask(callCtx, issue, execution.ExecutorID, pgtype.UUID{}, true, pgtype.UUID{}, true, "review handoff", p.ActorID, pgtype.UUID{})
		}
		return svc.TaskService.enqueueIssueTask(callCtx, issue, pgtype.UUID{}, false, "", p.ActorID, pgtype.UUID{})
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
	svc.TaskService = NewTaskService(db.New(single), single, nil, svc.TaskService.Bus)
	notifications := 0
	svc.TaskService.Bus.Subscribe(protocol.EventTaskQueued, func(event events.Event) {
		notifications++
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND status='queued'`, issue.ID).Scan(&count); err != nil || count != 1 {
			t.Errorf("enqueue event preceded commit: %d %v", count, err)
		}
	})
	deadline, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	task, err := enqueue(deadline)
	expectedNotifications := 1
	if mode == "deferred" {
		expectedNotifications = 0
		if task.Status != "deferred" || task.EscalationForTaskID != execution.MainTaskID || !task.FireAt.Time.Equal(fireAt) {
			t.Fatalf("deferred metadata lost: %+v", task)
		}
	}
	if err != nil || !task.ID.Valid || notifications != expectedNotifications {
		t.Fatalf("manual enqueue: %+v %v events=%d", task, err, notifications)
	}
	if mode == "mention" && (task.AgentID != execution.ExecutorID || !task.IsLeaderTask || !task.ForceFreshSession || task.HandoffNote.String != "review handoff") {
		t.Fatalf("mention metadata lost: %+v", task)
	}
}
