package handler

import (
	"context"
	"testing"

	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// enqueueMentionedAgentTasksForTest mirrors the production comment path for
// @mention triggers: compute the cascade trigger set, then enqueue it. Kept as a
// test helper so these integration tests keep asserting enqueue side effects.
func enqueueMentionedAgentRunsForTest(t *testing.T, ctx context.Context, task db.Task, comment db.Comment, parentComment *db.Comment, authorType, authorID string) {
	t.Helper()
	triggers, _ := testHandler.computeCommentAgentTriggers(ctx, task, comment.Content, parentComment, authorType, authorID, commentTriggerComputeOptions{})
	testHandler.enqueueCommentAgentTriggers(ctx, task, comment.ID, triggers)
}

// selfMentionFixture wires the seeded "Handler Test Agent" as J plus two
// fresh tasks so we can exercise the agent-self-mention path on the @mention
// branch of computeCommentAgentTriggers. The three tests below cover
// the behavior we want post-SILI-2338:
//
//   - cross-task self-mention enqueues (child→parent handoff between tasks
//     assigned to the same agent must not be swallowed)
//   - same-task self-mention with an in-flight running task enqueues a
//     follow-up (queue coalescing already allows this — the comment handler
//     must not pre-empt it with an extra in-thread guard)
//   - same-task self-mention with a queued/dispatched task is deduped
//     (HasPendingRunForTaskAndAgent still does its job)
type selfMentionFixture struct {
	JID        string
	RuntimeID  string
	TaskAID    string // primary task (used for same-task scenarios)
	TaskA      db.Task
	TaskBID    string // a second task (used for the cross-task scenario)
	TaskB      db.Task
	CommentAID string // a comment on TaskA authored by J — used as the trigger
	CommentA   db.Comment
	CommentBID string // a comment on TaskB authored by J — used as the trigger
	CommentB   db.Comment
}

func newSelfMentionFixture(t *testing.T) selfMentionFixture {
	t.Helper()
	ctx := context.Background()

	// Reuse the seeded workspace-visible agent — it already has a runtime.
	var jID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&jID); err != nil {
		t.Fatalf("load seeded agent: %v", err)
	}
	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, jID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}

	insertTask := func(title string) string {
		t.Helper()
		// Pick the next per-workspace task number; without it both inserts
		// land on the default number=0 and trip uq_task_workspace_number.
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET task_counter = GREATEST(task_counter, (SELECT COALESCE(MAX(number), 0) FROM task WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING task_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next task number: %v", err)
		}
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO task (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id, number)
			VALUES ($1, 'member', $2, $3, 'agent', $4, $5)
			RETURNING id
		`, testWorkspaceID, testUserID, title, jID, number).Scan(&id); err != nil {
			t.Fatalf("create task %q: %v", title, err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, id)
			testPool.Exec(context.Background(), `DELETE FROM comment WHERE task_id = $1`, id)
			testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, id)
		})
		return id
	}

	insertJComment := func(taskID, content string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (workspace_id, task_id, author_type, author_id, content)
			VALUES ($1, $2, 'agent', $3, $4)
			RETURNING id
		`, testWorkspaceID, taskID, jID, content).Scan(&id); err != nil {
			t.Fatalf("create comment on %s: %v", taskID, err)
		}
		return id
	}

	taskAID := insertTask("self-mention test A (same-task scenarios)")
	taskBID := insertTask("self-mention test B (parent task, cross-task handoff)")

	commentAID := insertJComment(taskAID, "[@J](mention://agent/"+jID+") follow-up coming")
	commentBID := insertJComment(taskBID, "Child task done — [@J](mention://agent/"+jID+") please wrap up here")

	taskA, err := testHandler.Queries.GetTask(ctx, util.MustParseUUID(taskAID))
	if err != nil {
		t.Fatalf("load issueA: %v", err)
	}
	taskB, err := testHandler.Queries.GetTask(ctx, util.MustParseUUID(taskBID))
	if err != nil {
		t.Fatalf("load issueB: %v", err)
	}
	commentA, err := testHandler.Queries.GetComment(ctx, util.MustParseUUID(commentAID))
	if err != nil {
		t.Fatalf("load commentA: %v", err)
	}
	commentB, err := testHandler.Queries.GetComment(ctx, util.MustParseUUID(commentBID))
	if err != nil {
		t.Fatalf("load commentB: %v", err)
	}

	return selfMentionFixture{
		JID:        jID,
		RuntimeID:  runtimeID,
		TaskAID:    taskAID,
		TaskA:      taskA,
		TaskBID:    taskBID,
		TaskB:      taskB,
		CommentAID: commentAID,
		CommentA:   commentA,
		CommentBID: commentBID,
		CommentB:   commentB,
	}
}

// countQueuedOrDispatched returns the number of queued|dispatched tasks for
// (agent, task). Mirrors the predicate used by HasPendingRunForTaskAndAgent.
func countQueuedOrDispatched(t *testing.T, agentID, taskID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_run
		WHERE task_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched')
	`, taskID, agentID).Scan(&n); err != nil {
		t.Fatalf("count queued/dispatched tasks: %v", err)
	}
	return n
}

// TestEnqueueMentionedAgentRuns_SelfMentionCrossTaskEnqueues is the
// regression test for the SILI-2338 child→parent handoff. The same agent runs
// in a child task, then posts a top-level comment on the parent task (whose
// assignee is the same agent) that @mentions itself. The comment handler MUST
// enqueue a task on the parent task — silently dropping the trigger was the
// bug Bohan reported.
func TestEnqueueMentionedAgentRuns_SelfMentionCrossTaskEnqueues(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSelfMentionFixture(t)

	if got := countQueuedOrDispatched(t, fx.JID, fx.TaskBID); got != 0 {
		t.Fatalf("before: expected 0 pending tasks on parent task, got %d", got)
	}

	enqueueMentionedAgentRunsForTest(t, ctx, fx.TaskB, fx.CommentB, nil, "agent", fx.JID)

	if got := countQueuedOrDispatched(t, fx.JID, fx.TaskBID); got != 1 {
		t.Fatalf("after self-mention from another task: expected 1 queued task on parent task, got %d", got)
	}
}

// TestEnqueueMentionedAgentTasks_SelfMentionWhileRunningQueuesFollowup proves
// that a self-mention posted in the same task an agent is currently running
// in does NOT pre-empt the natural queue-coalescing behavior: a `running`
// task is not "pending" for dedup purposes, so a new queued follow-up is
// added and the agent picks it up on its next cycle.
func TestEnqueueMentionedAgentRuns_SelfMentionWhileRunningQueuesFollowup(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSelfMentionFixture(t)

	// Seed a running task for J on task A — this is the agent's current run.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status)
		VALUES ($1, $2, $3, 'running')
	`, fx.JID, fx.RuntimeID, fx.TaskAID); err != nil {
		t.Fatalf("seed running run: %v", err)
	}

	if got := countQueuedOrDispatched(t, fx.JID, fx.TaskAID); got != 0 {
		t.Fatalf("before: expected 0 queued/dispatched tasks (only the running task), got %d", got)
	}

	enqueueMentionedAgentRunsForTest(t, ctx, fx.TaskA, fx.CommentA, nil, "agent", fx.JID)

	if got := countQueuedOrDispatched(t, fx.JID, fx.TaskAID); got != 1 {
		t.Fatalf("after self-mention while running: expected 1 new queued follow-up, got %d", got)
	}
}

// TestEnqueueMentionedAgentTasks_SelfMentionDedupesAgainstPendingTask locks in
// that removing the self-trigger `continue` did NOT remove the standard
// HasPendingRunForTaskAndAgent dedupe. If a queued or dispatched task
// already exists for the same agent on the same task, a fresh self-mention
// must NOT pile on another duplicate.
func TestEnqueueMentionedAgentRuns_SelfMentionDedupesAgainstPendingRun(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSelfMentionFixture(t)

	cases := []struct {
		name   string
		status string
	}{
		{name: "queued task blocks duplicate", status: "queued"},
		{name: "dispatched task blocks duplicate", status: "dispatched"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, fx.TaskAID); err != nil {
				t.Fatalf("reset tasks: %v", err)
			}
			if _, err := testPool.Exec(ctx, `
				INSERT INTO agent_run (agent_id, runtime_id, task_id, status)
				VALUES ($1, $2, $3, $4)
			`, fx.JID, fx.RuntimeID, fx.TaskAID, tc.status); err != nil {
				t.Fatalf("seed %s run: %v", tc.status, err)
			}

			before := countQueuedOrDispatched(t, fx.JID, fx.TaskAID)
			if before != 1 {
				t.Fatalf("before: expected 1 pre-existing %s task, got %d", tc.status, before)
			}

			enqueueMentionedAgentRunsForTest(t, ctx, fx.TaskA, fx.CommentA, nil, "agent", fx.JID)

			after := countQueuedOrDispatched(t, fx.JID, fx.TaskAID)
			if after != 1 {
				t.Fatalf("after self-mention with pre-existing %s run: expected dedupe (still 1), got %d", tc.status, after)
			}
		})
	}
}
