package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// TestCommentMentionsAnyone covers the pure helper that drives the
// "skip leader on @<anyone>" behavior. Routing-style mentions
// (agent/member/squad/all) count; task cross-references do not. Kept as a
// unit test so it runs without a database connection.
func TestCommentMentionsAnyone(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "empty", content: "", want: false},
		{name: "plain text", content: "please take a look", want: false},
		{name: "literal at sign only", content: "ping @alice", want: false},
		{name: "agent mention", content: "[@A](mention://agent/11111111-1111-1111-1111-111111111111) handle this", want: true},
		{name: "member mention", content: "[@Bob](mention://member/22222222-2222-2222-2222-222222222222)", want: true},
		{name: "squad mention", content: "[@Squad](mention://squad/44444444-4444-4444-4444-444444444444)", want: true},
		{name: "mention all", content: "[@all](mention://all/all)", want: true},
		{name: "task mention only", content: "see [SILI-1](mention://task/33333333-3333-3333-3333-333333333333)", want: false},
		{name: "task + plain text", content: "see [SILI-1](mention://task/33333333-3333-3333-3333-333333333333) for context", want: false},
		{name: "agent plus member", content: "[@A](mention://agent/11111111-1111-1111-1111-111111111111) cc [@B](mention://member/22222222-2222-2222-2222-222222222222)", want: true},
		{name: "task plus member", content: "blocks [SILI-1](mention://task/33333333-3333-3333-3333-333333333333) — [@Bob](mention://member/22222222-2222-2222-2222-222222222222)", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commentMentionsAnyone(tc.content); got != tc.want {
				t.Fatalf("commentMentionsAnyone(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

// shouldEnqueueSquadLeaderOnCommentForTest reports whether the shared cascade
// would wake the task's assigned squad leader.
func shouldEnqueueSquadLeaderOnCommentForTest(ctx context.Context, task db.Task, content, authorType, authorID string) bool {
	triggers, _ := testHandler.computeCommentAgentTriggers(ctx, task, content, nil, authorType, authorID, commentTriggerComputeOptions{})
	return triggersContainTaskAssigneeSquadLeader(triggers)
}

func shouldEnqueueSquadLeaderOnReplyForTest(ctx context.Context, task db.Task, content string, parent *db.Comment, authorType, authorID string) bool {
	triggers, _ := testHandler.computeCommentAgentTriggers(ctx, task, content, parent, authorType, authorID, commentTriggerComputeOptions{})
	return triggersContainTaskAssigneeSquadLeader(triggers)
}

func triggersContainTaskAssigneeSquadLeader(triggers []commentAgentTrigger) bool {
	for _, trigger := range triggers {
		if trigger.Source == commentTriggerSourceTaskAssignee && trigger.Squad != nil {
			return true
		}
	}
	return false
}

// squadCommentTriggerFixture wires a squad assigned to a fresh task and
// returns the loaded db.Task plus the leader agent UUID for use in
// cascade integration tests.
type squadCommentTriggerFixture struct {
	Task     db.Task
	SquadID  string
	LeaderID string
	OtherID  string // second agent in workspace (with runtime), used as a non-leader @mention target
}

func newSquadCommentTriggerFixture(t *testing.T) squadCommentTriggerFixture {
	t.Helper()
	ctx := context.Background()

	// Reuse the seeded "Handler Test Agent" as the leader — it has a runtime.
	var leaderID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&leaderID); err != nil {
		t.Fatalf("load leader agent: %v", err)
	}

	// Spin up a second agent in the same workspace as a non-leader mention
	// target. createHandlerTestAgent installs a t.Cleanup row deletion.
	otherID := createHandlerTestAgent(t, "Squad Comment Other", nil)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Squad Comment Trigger", leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id)
		VALUES ($1, 'member', $2, $3, 'squad', $4)
		RETURNING id
	`, testWorkspaceID, testUserID, "squad comment trigger", squadID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, taskID)
	})

	task, err := testHandler.Queries.GetTask(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("load task: %v", err)
	}

	return squadCommentTriggerFixture{
		Task:     task,
		SquadID:  squadID,
		LeaderID: leaderID,
		OtherID:  otherID,
	}
}

// TestShouldEnqueueSquadLeaderOnComment_SkipsWhenCommentRoutesElsewhere
// pins the cascade: explicit participant mentions do not also wake the assigned
// squad leader. Task cross-references are not routing and do not suppress the
// leader.
func TestShouldEnqueueSquadLeaderOnComment_SkipsWhenMemberMentionsAnyone(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSquadCommentTriggerFixture(t)
	ctx := context.Background()

	cases := []struct {
		name        string
		content     string
		authorType  string
		authorID    string
		want        bool
		description string
	}{
		{
			name:        "member plain comment triggers leader",
			content:     "what is the latest on this?",
			authorType:  "member",
			authorID:    testUserID,
			want:        true,
			description: "no @ in body → leader must coordinate as today",
		},
		{
			name:        "member task cross-reference only triggers leader",
			content:     "blocked by [SILI-1](mention://task/" + testUserID + ")",
			authorType:  "member",
			authorID:    testUserID,
			want:        true,
			description: "task mentions are not routing — leader still owns dispatch",
		},
		{
			name:        "member mentions another member skips leader",
			content:     "[@self](mention://member/" + testUserID + ") please weigh in",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "user routed at a human — leader stays out (extended rule)",
		},
		{
			name:        "member mentions non-leader agent skips leader",
			content:     "[@Other](mention://agent/" + fx.OtherID + ") please take this",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "user routed at an agent — leader stays out",
		},
		{
			name:        "member mentions leader skips leader on comment path",
			content:     "[@Leader](mention://agent/" + fx.LeaderID + ") your call",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "even @leader is dispatched via the mention path; comment path must not double-enqueue",
		},
		{
			name:        "member mention all skips leader",
			content:     "[@all](mention://all/all) heads up",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "@all is a broadcast — leader does not need to wake to evaluate routing",
		},
		{
			name:        "member mentions a squad skips leader",
			content:     "handing to [@Other Squad](mention://squad/" + fx.SquadID + ")",
			authorType:  "member",
			authorID:    testUserID,
			want:        false,
			description: "@squad routes the task to that squad's leader — current leader stays out",
		},
		{
			name:        "agent comment with @agent does not also trigger leader",
			content:     "delegating to [@Other](mention://agent/" + fx.OtherID + ")",
			authorType:  "agent",
			authorID:    fx.OtherID,
			want:        false,
			description: "explicit @agent routes only to the mentioned target; the assigned squad-leader fallback must not also fire (no double-enqueue)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldEnqueueSquadLeaderOnCommentForTest(ctx, fx.Task, tc.content, tc.authorType, tc.authorID)
			if got != tc.want {
				t.Fatalf("%s\n  content=%q author=%s/%s\n  got=%v want=%v",
					tc.description, tc.content, tc.authorType, tc.authorID, got, tc.want)
			}
		})
	}
}

// TestShouldEnqueueSquadLeaderOnComment_AgentAuthoredWorkerCommentsWakeLeader
// pins the SILI-3879 restored behavior in the new SILI-3794 cascade: an
// agent-authored worker-result comment on a squad-assigned task wakes the
// assigned squad leader so the leader→worker→leader coordination loop stays
// closed, while the leader's own self-trigger loop stays suppressed.
func TestShouldEnqueueSquadLeaderOnComment_AgentAuthoredWorkerCommentsWakeLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newSquadCommentTriggerFixture(t)
	ctx := context.Background()
	taskID := uuidToString(fx.Task.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, taskID)
	})

	clearRuns := func() {
		if _, err := testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, taskID); err != nil {
			t.Fatalf("clear tasks: %v", err)
		}
	}
	// insertLeaderTask seeds a same-squad task for the leader agent so the
	// self-trigger guard can read the agent's most recent role on the task.
	// Separate Exec calls get distinct created_at values, so the last inserted
	// row is the "latest" task.
	insertLeaderRun := func(isLeader bool, status string) {
		t.Helper()
		var runtimeID string
		if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&runtimeID); err != nil {
			t.Fatalf("load runtime: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_run (agent_id, runtime_id, task_id, status, is_leader_run, squad_id)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, fx.LeaderID, runtimeID, taskID, status, isLeader, fx.SquadID); err != nil {
			t.Fatalf("insert run: %v", err)
		}
	}

	// Case 1: a worker agent (not the leader) posts a result comment on the
	// squad-assigned task — the assigned leader must wake to coordinate.
	t.Run("worker agent comment wakes squad leader", func(t *testing.T) {
		clearRuns()
		if got := shouldEnqueueSquadLeaderOnCommentForTest(ctx, fx.Task, "pushed the fix, PR is up", "agent", fx.OtherID); !got {
			t.Fatalf("worker agent comment: expected leader to wake, got skip")
		}
	})

	// Case 2: a dual-role agent (leader of the squad, also runs worker tasks)
	// posts while its latest task on the task was a worker task — the leader
	// role must still wake because the comment is a worker result, not a
	// leader self-trigger.
	t.Run("dual-role worker comment wakes leader when latest task is worker", func(t *testing.T) {
		clearRuns()
		insertLeaderRun(true, "completed")  // older leader task
		insertLeaderRun(false, "completed") // newer worker task → latest role is worker
		if got := shouldEnqueueSquadLeaderOnCommentForTest(ctx, fx.Task, "done with my worker slice", "agent", fx.LeaderID); !got {
			t.Fatalf("dual-role worker comment: expected leader to wake, got skip")
		}
	})

	// Case 3: the leader posts while its latest task was a leader task — this
	// is a self-trigger loop and must stay suppressed.
	t.Run("leader comment from latest leader task does not self-trigger", func(t *testing.T) {
		clearRuns()
		insertLeaderRun(false, "completed") // older worker task
		insertLeaderRun(true, "completed")  // newer leader task → latest role is leader
		if got := shouldEnqueueSquadLeaderOnCommentForTest(ctx, fx.Task, "coordinating next steps", "agent", fx.LeaderID); got {
			t.Fatalf("leader self-trigger: expected skip, got wake")
		}
	})

	// Case 4: an agent-authored comment carrying an explicit @agent mention
	// routes only to the mentioned target — the assigned squad leader must NOT
	// also be enqueued via the fallback path (no double-enqueue).
	t.Run("explicit mention does not double-enqueue assigned leader", func(t *testing.T) {
		clearRuns()
		content := "handing to [@Other](mention://agent/" + fx.OtherID + ")"
		if got := shouldEnqueueSquadLeaderOnCommentForTest(ctx, fx.Task, content, "agent", fx.LeaderID); got {
			t.Fatalf("explicit mention: expected no assigned-leader fallback, got wake")
		}
	})
}

// TestCreateComment_SquadPlainReplyToMemberParentKeepsRootMentionOwner drives the
// full CreateComment handler to lock the cascade's reply behavior:
//
//   - A member top-level comment that @mentions another agent does NOT
//     enqueue the squad leader (the mentioned agent owns the next step).
//   - A subsequent member reply to that member-authored root with no explicit
//     agent mention continues to the root owner instead of the assignee.
func TestCreateComment_SquadPlainReplyToMemberParentKeepsRootMentionOwner(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadCommentTriggerFixture(t)
	taskID := uuidToString(fx.Task.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE task_id = $1`, taskID)
	})

	countQueued := func(agentID string) int {
		var n int
		if err := testPool.QueryRow(ctx,
			`SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'`,
			taskID, agentID,
		).Scan(&n); err != nil {
			t.Fatalf("count tasks for %s: %v", agentID, err)
		}
		return n
	}

	postMemberComment := func(body map[string]any) CommentResponse {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/tasks/"+taskID+"/comments", body)
		r = withURLParam(r, "id", taskID)
		testHandler.CreateComment(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp CommentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode comment: %v", err)
		}
		return resp
	}

	// 1. Member top-level comment mentions OtherAgent.
	//    Leader must be skipped; OtherAgent must be enqueued via the mention path.
	parent := postMemberComment(map[string]any{
		"content": "[@Other](mention://agent/" + fx.OtherID + ") please take this",
	})
	if got := countQueued(fx.LeaderID); got != 0 {
		t.Fatalf("after parent (@OtherAgent): expected 0 leader tasks (skipped), got %d", got)
	}
	if got := countQueued(fx.OtherID); got != 1 {
		t.Fatalf("after parent (@OtherAgent): expected 1 OtherAgent task (mention path), got %d", got)
	}

	// 2. Mark OtherAgent's parent task done so queued-task counts below only
	//    reflect what the plain reply does.
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_run SET status = 'completed'
		WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'
	`, taskID, fx.OtherID); err != nil {
		t.Fatalf("complete OtherAgent parent run: %v", err)
	}

	// 3. Member posts a reply in the same thread with NO mentions.
	//    The root's @OtherAgent mention owns the thread, so the reply returns
	//    to OtherAgent instead of falling back to the assigned squad leader.
	postMemberComment(map[string]any{
		"content":   "any update?",
		"parent_id": parent.ID,
	})
	if got := countQueued(fx.LeaderID); got != 0 {
		t.Fatalf("after plain reply: expected 0 leader tasks, got %d", got)
	}
	if got := countQueued(fx.OtherID); got != 1 {
		t.Fatalf("after plain reply: expected 1 OtherAgent task, got %d", got)
	}
}

// TestCreateComment_DualRoleAgentWorkerCommentWakesLeader pins the SILI-3879
// restored coordination loop at the full-handler level. Scenario:
//
//   - Agent L is the leader of squad S and also runs worker tasks on tasks
//     belonging to S.
//   - L is woken in its worker role (is_leader_run=false) and posts a result
//     comment.
//   - A leader-role task IS enqueued so the squad leader can coordinate the
//     next step — the worker result must not silently strand the task.
func TestCreateComment_DualRoleAgentWorkerCommentWakesLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadCommentTriggerFixture(t)
	taskID := uuidToString(fx.Task.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE task_id = $1`, taskID)
	})

	// Seed a same-squad worker task for the leader agent on this task so the
	// guard infers "agent's last activity was a worker task" — i.e. L is
	// running in its worker role when it posts the comment. We make it running
	// (not completed) so we can hand its ID back through X-Run-ID for the
	// resolveActor agent-identity check.
	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	var workerRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, is_leader_run, squad_id)
		VALUES ($1, $2, $3, 'running', FALSE, $4)
		RETURNING id
	`, fx.LeaderID, runtimeID, taskID, fx.SquadID).Scan(&workerRunID); err != nil {
		t.Fatalf("seed worker run: %v", err)
	}

	// L posts a comment in its agent identity (X-Agent-ID + X-Run-ID, the
	// pair required by resolveActor to trust the agent header).
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/tasks/"+taskID+"/comments", map[string]any{
		"content": "done — pushed the change",
	})
	r.Header.Set("X-Agent-ID", fx.LeaderID)
	r.Header.Set("X-Run-ID", workerRunID)
	r = withURLParam(r, "id", taskID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// A new leader-role task is enqueued so the leader coordinates next steps.
	var leaderRuns int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_run
		WHERE task_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_run = TRUE
	`, taskID, fx.LeaderID).Scan(&leaderRuns); err != nil {
		t.Fatalf("count leader tasks: %v", err)
	}
	if leaderRuns != 1 {
		t.Fatalf("after worker comment from dual-role agent: expected 1 queued leader task, got %d", leaderRuns)
	}
}

// TestCreateComment_SquadLeaderMentionTaskDoesNotSelfTriggerAssignedFallback
// pins SILI-4024's direct-mention gap:
//
//   - A member explicitly @mentions the task's assigned squad leader by agent
//     id, which queues a generic mention task for L (is_leader_run=false,
//     squad_id=NULL).
//   - L posts a plain reply while running that mention task.
//   - The assigned-squad fallback must not treat that generic mention task as a
//     same-squad worker result and queue L again as the leader.
func TestCreateComment_SquadLeaderMentionRunDoesNotSelfTriggerAssignedFallback(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadCommentTriggerFixture(t)
	taskID := uuidToString(fx.Task.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE task_id = $1`, taskID)
	})

	postMemberComment := func(body map[string]any) CommentResponse {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/tasks/"+taskID+"/comments", body)
		r = withURLParam(r, "id", taskID)
		testHandler.CreateComment(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment(member): expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp CommentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode member comment: %v", err)
		}
		return resp
	}
	postAgentComment := func(runID string, body map[string]any) {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/tasks/"+taskID+"/comments", body)
		r.Header.Set("X-Agent-ID", fx.LeaderID)
		r.Header.Set("X-Run-ID", runID)
		r = withURLParam(r, "id", taskID)
		testHandler.CreateComment(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment(agent): expected 201, got %d: %s", w.Code, w.Body.String())
		}
	}
	countQueuedLeaderRuns := func() int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(ctx, `
			SELECT count(*) FROM agent_run
			WHERE task_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_run = TRUE
		`, taskID, fx.LeaderID).Scan(&n); err != nil {
			t.Fatalf("count queued leader tasks: %v", err)
		}
		return n
	}

	trigger := postMemberComment(map[string]any{
		"content": "[@Leader](mention://agent/" + fx.LeaderID + ") can you check this?",
	})

	var mentionRunID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_run
		WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'
		  AND is_leader_run = FALSE AND squad_id IS NULL
		ORDER BY created_at DESC
		LIMIT 1
	`, taskID, fx.LeaderID).Scan(&mentionRunID); err != nil {
		t.Fatalf("load leader mention run: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_run SET status = 'running' WHERE id = $1`, mentionRunID); err != nil {
		t.Fatalf("mark mention task running: %v", err)
	}

	postAgentComment(mentionRunID, map[string]any{
		"content":   "checked, no action needed",
		"parent_id": trigger.ID,
	})

	if got := countQueuedLeaderRuns(); got != 0 {
		t.Fatalf("leader reply from generic mention task queued %d leader tasks, want 0", got)
	}
}

// TestCreateComment_SquadLeaderThreadParentTaskDoesNotSelfTriggerAssignedFallback
// pins SILI-4024's thread-parent gap: a member reply to the leader's earlier
// comment queues L through EnqueueTaskForThreadParent (is_leader_run=false,
// squad_id=NULL). L's reply from that generic task must not queue L again as
// the assigned squad leader.
func TestCreateComment_SquadLeaderThreadParentRunDoesNotSelfTriggerAssignedFallback(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadCommentTriggerFixture(t)
	taskID := uuidToString(fx.Task.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE task_id = $1`, taskID)
	})

	var leaderRuntimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&leaderRuntimeID); err != nil {
		t.Fatalf("load leader runtime: %v", err)
	}
	var leaderRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, is_leader_run, squad_id)
		VALUES ($1, $2, $3, 'running', TRUE, $4)
		RETURNING id
	`, fx.LeaderID, leaderRuntimeID, taskID, fx.SquadID).Scan(&leaderRunID); err != nil {
		t.Fatalf("seed leader run: %v", err)
	}

	postAgentComment := func(runID string, body map[string]any) CommentResponse {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/tasks/"+taskID+"/comments", body)
		r.Header.Set("X-Agent-ID", fx.LeaderID)
		r.Header.Set("X-Run-ID", runID)
		r = withURLParam(r, "id", taskID)
		testHandler.CreateComment(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment(agent): expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp CommentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode agent comment: %v", err)
		}
		return resp
	}
	postMemberComment := func(body map[string]any) CommentResponse {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/tasks/"+taskID+"/comments", body)
		r = withURLParam(r, "id", taskID)
		testHandler.CreateComment(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment(member): expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp CommentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode member comment: %v", err)
		}
		return resp
	}
	countQueuedLeaderRuns := func() int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(ctx, `
			SELECT count(*) FROM agent_run
			WHERE task_id = $1 AND agent_id = $2 AND status = 'queued' AND is_leader_run = TRUE
		`, taskID, fx.LeaderID).Scan(&n); err != nil {
			t.Fatalf("count queued leader tasks: %v", err)
		}
		return n
	}

	parent := postAgentComment(leaderRunID, map[string]any{
		"content": "coordinating this task",
	})
	if _, err := testPool.Exec(ctx, `UPDATE agent_run SET status = 'completed' WHERE id = $1`, leaderRunID); err != nil {
		t.Fatalf("complete leader run: %v", err)
	}

	trigger := postMemberComment(map[string]any{
		"content":   "any update?",
		"parent_id": parent.ID,
	})

	var threadParentRunID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_run
		WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'
		  AND is_leader_run = FALSE AND squad_id IS NULL
		ORDER BY created_at DESC
		LIMIT 1
	`, taskID, fx.LeaderID).Scan(&threadParentRunID); err != nil {
		t.Fatalf("load leader thread-parent run: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_run SET status = 'running' WHERE id = $1`, threadParentRunID); err != nil {
		t.Fatalf("mark thread-parent task running: %v", err)
	}

	postAgentComment(threadParentRunID, map[string]any{
		"content":   "replying from the thread-parent task",
		"parent_id": trigger.ID,
	})

	if got := countQueuedLeaderRuns(); got != 0 {
		t.Fatalf("leader reply from generic thread-parent task queued %d leader tasks, want 0", got)
	}
}

// TestCreateRetryTask_InheritsIsLeaderTask locks the retry-clone contract for
// SILI-2218: auto-retry of a leader-role task must produce a child task that is
// also is_leader_run=true. Without this, MaybeRetryFailedTask silently
// demotes a retried leader task to a worker task, and role-specific claim-time
// briefing/self-mention guards lose the leader provenance.
func TestCreateRetryRun_InheritsIsLeaderRun(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSquadCommentTriggerFixture(t)
	taskID := uuidToString(fx.Task.ID)

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, taskID)
	})

	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, fx.LeaderID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}

	cases := []struct {
		name     string
		isLeader bool
	}{
		{name: "leader task retry stays leader", isLeader: true},
		{name: "worker task retry stays worker", isLeader: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var parentID string
			if err := testPool.QueryRow(ctx, `
				INSERT INTO agent_run (agent_id, runtime_id, task_id, status, attempt, max_attempts, is_leader_run)
				VALUES ($1, $2, $3, 'failed', 1, 3, $4)
				RETURNING id
			`, fx.LeaderID, runtimeID, taskID, tc.isLeader).Scan(&parentID); err != nil {
				t.Fatalf("seed parent run: %v", err)
			}
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1 OR parent_run_id = $1`, parentID)
			})

			child, err := testHandler.Queries.CreateRetryRun(ctx, db.CreateRetryRunParams{ID: util.MustParseUUID(parentID)})
			if err != nil {
				t.Fatalf("CreateRetryTask: %v", err)
			}
			if child.IsLeaderRun != tc.isLeader {
				t.Fatalf("child.IsLeaderTask = %v, want %v (parent role must be inherited)", child.IsLeaderRun, tc.isLeader)
			}
		})
	}
}

// TestCreateComment_SquadMentionPrivateLeaderBlocksPlainMember verifies that
// a plain workspace member cannot trigger a private squad leader via @squad
// mention. This is the regression test for the P1 finding: without the
// canAccessPrivateAgent gate in the squad mention branch, a member could
// bypass the private-agent restriction by mentioning the squad instead of
// the agent directly.
func TestCreateComment_SquadMentionPrivateLeaderBlocksPlainMember(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Use privateAgentTestFixture to get a private agent + plain member.
	agentID, _, memberID := privateAgentTestFixture(t)

	// Create a squad with the private agent as leader.
	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Private Leader Squad', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// Create an task.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, 'private leader squad mention test')
		RETURNING id
	`, testWorkspaceID, memberID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, taskID)
	})

	// Plain member posts a comment mentioning the squad.
	w := httptest.NewRecorder()
	r := newRequestAs(memberID, "POST", "/api/tasks/"+taskID+"/comments", map[string]any{
		"content": "[@Squad](mention://squad/" + squadID + ") please handle",
	})
	r = withURLParam(r, "id", taskID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// The private leader must NOT have a queued task — plain member lacks access.
	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'`,
		taskID, agentID,
	).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 0 {
		t.Fatalf("private leader got %d queued tasks from plain member squad mention; want 0 (access denied)", count)
	}
}

// TestCreateComment_SquadMentionTriggersLeader verifies that @mentioning a
// squad in a comment triggers the squad's leader agent via the mention path,
// even when the task is NOT assigned to that squad.
func TestCreateComment_SquadMentionTriggersLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Create a squad with a leader agent.
	var leaderID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&leaderID); err != nil {
		t.Fatalf("load leader agent: %v", err)
	}

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Mention Trigger Squad", leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// Create an task NOT assigned to the squad (assigned to nobody).
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, 'squad mention trigger test')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, taskID)
	})

	countQueued := func(agentID string) int {
		var n int
		if err := testPool.QueryRow(ctx,
			`SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'`,
			taskID, agentID,
		).Scan(&n); err != nil {
			t.Fatalf("count tasks for %s: %v", agentID, err)
		}
		return n
	}

	// Post a comment that @mentions the squad.
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/tasks/"+taskID+"/comments", map[string]any{
		"content": "[@Squad](mention://squad/" + squadID + ") please handle this",
	})
	r = withURLParam(r, "id", taskID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// The squad's leader should have a queued task.
	if got := countQueued(leaderID); got != 1 {
		t.Fatalf("after @squad mention: expected 1 leader task, got %d", got)
	}
}
