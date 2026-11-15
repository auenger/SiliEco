package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// involvesFixture seeds, for a single test, the data needed to exercise every
// branch of the `involves_user_id` 4-branch filter — owned agent, squad human
// member, squad canonical leader (via squad.leader_id, NOT a squad_member copy
// row), and squad agent member — plus a parallel set in a second workspace so
// the cross-workspace negative tests can prove subquery-level isolation.
type involvesFixture struct {
	// All IDs are in the primary handler-test workspace unless noted otherwise.
	userID  string // the "me" user the filter is keyed on (== testUserID)
	otherID string // a different user in the same workspace

	ownedAgentID    string // agent.owner_id = userID — branch (1) seed
	otherAgentID    string // agent.owner_id = otherID — must NOT match
	squadMemberID   string // squad with userID as human member — branch (2)
	squadLeaderID   string // squad whose leader_id is an agent owned by userID — branch (3)
	squadAgentMemID string // squad with an owned-agent as squad_member row — branch (4)

	// Other workspace, mirror objects — used by ExcludesOtherWorkspace* tests
	otherWsID            string
	otherWsAgent         string // owned by userID but in other workspace
	otherWsSquadMember   string // squad with userID as human member, in other ws
	otherWsSquadLeader   string // squad whose leader is userID's agent (in other ws)
	otherWsSquadAgentMem string // squad with userID's agent as member (in other ws)
}

func setupInvolvesFixture(t *testing.T) *involvesFixture {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	fx := &involvesFixture{userID: testUserID}

	// --- second user inside the primary workspace ---
	var otherUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, "Involves Other User", fmt.Sprintf("involves-other-%d@silieco.ai", suffix)).Scan(&otherUserID); err != nil {
		t.Fatalf("create other user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, otherUserID) })
	fx.otherID = otherUserID
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, otherUserID); err != nil {
		t.Fatalf("create other member: %v", err)
	}

	runtimeID := handlerTestRuntimeID(t)

	// --- agents in primary workspace ---
	fx.ownedAgentID = insertAgent(t, ctx, testWorkspaceID, runtimeID, fx.userID,
		fmt.Sprintf("Involves Owned Agent %d", suffix))
	fx.otherAgentID = insertAgent(t, ctx, testWorkspaceID, runtimeID, fx.otherID,
		fmt.Sprintf("Involves Other Agent %d", suffix))

	// --- a squad we already have to satisfy NOT NULL leader_id ---
	leaderForMemberSquad := insertAgent(t, ctx, testWorkspaceID, runtimeID, fx.otherID,
		fmt.Sprintf("Involves Leader-for-MemberSquad %d", suffix))
	fx.squadMemberID = insertSquad(t, ctx, testWorkspaceID, leaderForMemberSquad,
		fmt.Sprintf("InvolvesSquadMember-%d", suffix))
	// Add the test user as a human member.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO squad_member (squad_id, member_type, member_id) VALUES ($1, 'member', $2)
	`, fx.squadMemberID, fx.userID); err != nil {
		t.Fatalf("add squad human member: %v", err)
	}

	// --- squad with leader = our owned agent — branch (3). Critically, we do
	// NOT insert a squad_member row for the leader, so the test exercises the
	// canonical squad.leader_id path. ---
	fx.squadLeaderID = insertSquad(t, ctx, testWorkspaceID, fx.ownedAgentID,
		fmt.Sprintf("InvolvesSquadLeader-%d", suffix))

	// --- squad whose agent member is our owned agent — branch (4) ---
	leaderForAgentMemSquad := insertAgent(t, ctx, testWorkspaceID, runtimeID, fx.otherID,
		fmt.Sprintf("Involves Leader-for-AgentMemSquad %d", suffix))
	fx.squadAgentMemID = insertSquad(t, ctx, testWorkspaceID, leaderForAgentMemSquad,
		fmt.Sprintf("InvolvesSquadAgentMem-%d", suffix))
	// Use a fresh owned agent so the squad_member row is the only signal —
	// keeps branch (4) independent from branch (1)/(3).
	branch4Agent := insertAgent(t, ctx, testWorkspaceID, runtimeID, fx.userID,
		fmt.Sprintf("Involves Branch4 Agent %d", suffix))
	if _, err := testPool.Exec(ctx, `
		INSERT INTO squad_member (squad_id, member_type, member_id) VALUES ($1, 'agent', $2)
	`, fx.squadAgentMemID, branch4Agent); err != nil {
		t.Fatalf("add squad agent member: %v", err)
	}

	// --- second workspace, mirroring all four shapes for cross-ws negatives ---
	var otherWsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, task_prefix)
		VALUES ($1, $2, '', 'OTH')
		RETURNING id
	`, fmt.Sprintf("InvolvesOtherWs-%d", suffix), fmt.Sprintf("involves-other-ws-%d", suffix)).Scan(&otherWsID); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, otherWsID) })
	fx.otherWsID = otherWsID

	// Membership in other workspace (so the user could legitimately be assigned
	// there too — exercises whether subquery workspace_id clause filters it out).
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
	`, otherWsID, fx.userID); err != nil {
		t.Fatalf("create other-ws member: %v", err)
	}

	var otherRuntimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		) VALUES ($1, NULL, $2, 'cloud', 'other_ws_runtime', 'online', $3, '{}'::jsonb, now())
		RETURNING id
	`, otherWsID, fmt.Sprintf("OtherWs Runtime %d", suffix), "other-ws-runtime").Scan(&otherRuntimeID); err != nil {
		t.Fatalf("create other-ws runtime: %v", err)
	}

	fx.otherWsAgent = insertAgent(t, ctx, otherWsID, otherRuntimeID, fx.userID,
		fmt.Sprintf("OtherWs Owned Agent %d", suffix))

	leaderForOtherWsMemberSquad := insertAgent(t, ctx, otherWsID, otherRuntimeID, fx.otherID,
		fmt.Sprintf("OtherWs Leader-for-MemberSquad %d", suffix))
	fx.otherWsSquadMember = insertSquad(t, ctx, otherWsID, leaderForOtherWsMemberSquad,
		fmt.Sprintf("OtherWsSquadMember-%d", suffix))
	if _, err := testPool.Exec(ctx, `
		INSERT INTO squad_member (squad_id, member_type, member_id) VALUES ($1, 'member', $2)
	`, fx.otherWsSquadMember, fx.userID); err != nil {
		t.Fatalf("add other-ws squad member: %v", err)
	}

	fx.otherWsSquadLeader = insertSquad(t, ctx, otherWsID, fx.otherWsAgent,
		fmt.Sprintf("OtherWsSquadLeader-%d", suffix))

	leaderForOtherWsAgentMemSquad := insertAgent(t, ctx, otherWsID, otherRuntimeID, fx.otherID,
		fmt.Sprintf("OtherWs Leader-for-AgentMemSquad %d", suffix))
	fx.otherWsSquadAgentMem = insertSquad(t, ctx, otherWsID, leaderForOtherWsAgentMemSquad,
		fmt.Sprintf("OtherWsSquadAgentMem-%d", suffix))
	otherWsBranch4Agent := insertAgent(t, ctx, otherWsID, otherRuntimeID, fx.userID,
		fmt.Sprintf("OtherWs Branch4 Agent %d", suffix))
	if _, err := testPool.Exec(ctx, `
		INSERT INTO squad_member (squad_id, member_type, member_id) VALUES ($1, 'agent', $2)
	`, fx.otherWsSquadAgentMem, otherWsBranch4Agent); err != nil {
		t.Fatalf("add other-ws squad agent member: %v", err)
	}

	return fx
}

func insertAgent(t *testing.T, ctx context.Context, workspaceID, runtimeID, ownerID, name string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_runs, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
		RETURNING id
	`, workspaceID, name, runtimeID, ownerID).Scan(&id); err != nil {
		t.Fatalf("create agent %q: %v", name, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, id) })
	return id
}

func insertSquad(t *testing.T, ctx context.Context, workspaceID, leaderAgentID, name string) string {
	t.Helper()
	var id string
	// creator_id is loose (no FK) — reuse testUserID to keep the row valid.
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, leader_id, creator_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, workspaceID, name, leaderAgentID, testUserID).Scan(&id); err != nil {
		t.Fatalf("create squad %q: %v", name, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, id) })
	return id
}

// insertTaskTo creates an task in the given workspace assigned to the given
// (assigneeType, assigneeID) pair and returns its UUID. Task rows are
// best-effort-cleaned up by the test.
func insertTaskTo(t *testing.T, ctx context.Context, workspaceID, title, assigneeType, assigneeID string) string {
	t.Helper()
	var number int32
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET task_counter = GREATEST(
			task_counter,
			(SELECT COALESCE(MAX(number), 0) FROM task WHERE workspace_id = $1)
		) + 1
		WHERE id = $1
		RETURNING task_counter
	`, workspaceID).Scan(&number); err != nil {
		t.Fatalf("next task number: %v", err)
	}
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (
			workspace_id, title, description, status, priority,
			assignee_type, assignee_id, creator_type, creator_id,
			position, number
		)
		VALUES ($1, $2, NULL, 'todo', 'none', $3, $4, 'member', $5, 0, $6)
		RETURNING id
	`, workspaceID, title, assigneeType, assigneeID, testUserID, number).Scan(&id); err != nil {
		t.Fatalf("create task %q: %v", title, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, id) })
	return id
}

// listTasksInvolves runs ListTasks with `involves_user_id` set to userID
// (against testWorkspaceID) and returns the resulting task IDs.
func listTasksInvolves(t *testing.T, userID string) []string {
	t.Helper()
	path := fmt.Sprintf("/api/tasks?workspace_id=%s&involves_user_id=%s&limit=500",
		testWorkspaceID, userID)
	w := httptest.NewRecorder()
	testHandler.ListTasks(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListTasks: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Tasks []TaskResponse `json:"tasks"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	ids := make([]string, 0, len(resp.Tasks))
	for _, iss := range resp.Tasks {
		ids = append(ids, iss.ID)
	}
	return ids
}

// listGroupedTasksInvolves runs ListGroupedTasks with `involves_user_id`
// set to userID and returns the flattened set of task IDs across all groups.
func listGroupedTasksInvolves(t *testing.T, userID string) []string {
	t.Helper()
	path := fmt.Sprintf(
		"/api/tasks/grouped?workspace_id=%s&group_by=assignee&statuses=todo&involves_user_id=%s&limit=100",
		testWorkspaceID, userID)
	w := httptest.NewRecorder()
	testHandler.ListGroupedTasks(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListGroupedTasks: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp GroupedTasksResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode grouped response: %v", err)
	}
	ids := []string{}
	for _, g := range resp.Groups {
		for _, iss := range g.Tasks {
			ids = append(ids, iss.ID)
		}
	}
	return ids
}

func containsTaskID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

// ---- positive branches ----

func TestListTasks_InvolvesUserID_MatchesOwnedAgentAssignee(t *testing.T) {
	ctx := context.Background()
	fx := setupInvolvesFixture(t)
	wantID := insertTaskTo(t, ctx, testWorkspaceID,
		"task assigned to my owned agent", "agent", fx.ownedAgentID)
	if got := listTasksInvolves(t, fx.userID); !containsTaskID(got, wantID) {
		t.Fatalf("branch (1) miss: owned-agent assignee not surfaced (want %s, got %v)", wantID, got)
	}
}

func TestListTasks_InvolvesUserID_MatchesSquadMember(t *testing.T) {
	ctx := context.Background()
	fx := setupInvolvesFixture(t)
	wantID := insertTaskTo(t, ctx, testWorkspaceID,
		"task assigned to a squad I'm a member of", "squad", fx.squadMemberID)
	if got := listTasksInvolves(t, fx.userID); !containsTaskID(got, wantID) {
		t.Fatalf("branch (2) miss: human-member squad assignee not surfaced (want %s, got %v)", wantID, got)
	}
}

func TestListTasks_InvolvesUserID_MatchesLeaderViaCanonicalRelation(t *testing.T) {
	ctx := context.Background()
	fx := setupInvolvesFixture(t)
	// Fixture deliberately omits the squad_member leader-copy row, so this
	// can only match if the SQL reads squad.leader_id directly (branch 3).
	wantID := insertTaskTo(t, ctx, testWorkspaceID,
		"task assigned to a squad my agent leads", "squad", fx.squadLeaderID)
	if got := listTasksInvolves(t, fx.userID); !containsTaskID(got, wantID) {
		t.Fatalf("branch (3) miss: squad-leader-via-canonical assignee not surfaced (want %s, got %v)", wantID, got)
	}
}

func TestListTasks_InvolvesUserID_MatchesSquadAgentMember(t *testing.T) {
	ctx := context.Background()
	fx := setupInvolvesFixture(t)
	wantID := insertTaskTo(t, ctx, testWorkspaceID,
		"task assigned to a squad my agent is a member of", "squad", fx.squadAgentMemID)
	if got := listTasksInvolves(t, fx.userID); !containsTaskID(got, wantID) {
		t.Fatalf("branch (4) miss: squad agent-member assignee not surfaced (want %s, got %v)", wantID, got)
	}
}

// ---- the critical negative: tab 3 must be disjoint from tab 1 ----

// Nails the semantics: `involves_user_id` MUST NOT surface tasks whose
// assignee is the user themself (member type). Direct member assignment is
// the meaning of `assignee_id` (tab 1 "Assigned to me"); the two tabs must
// produce disjoint result sets. If anyone adds a fifth UNION branch
// `(assignee_type='member' AND assignee_id=involves_user_id)` back in, this
// test fails.
func TestListTasks_InvolvesUserID_ExcludesDirectMemberAssignee(t *testing.T) {
	ctx := context.Background()
	fx := setupInvolvesFixture(t)
	taskID := insertTaskTo(t, ctx, testWorkspaceID,
		"tab 3 must NOT surface member-direct assignment", "member", fx.userID)
	if got := listTasksInvolves(t, fx.userID); containsTaskID(got, taskID) {
		t.Fatalf("tab 3 semantics violated: involves_user_id surfaced a member-direct assignee task (id=%s); that belongs to tab 1. Full result: %v",
			taskID, got)
	}
}

// Same negative on the grouped (dynamic SQL) path — the dynamic builder is a
// separate code path from sqlc, so it gets its own regression.
func TestListGroupedTasks_InvolvesUserID_ExcludesDirectMemberAssignee(t *testing.T) {
	ctx := context.Background()
	fx := setupInvolvesFixture(t)
	taskID := insertTaskTo(t, ctx, testWorkspaceID,
		"grouped tab 3 must NOT surface member-direct assignment", "member", fx.userID)
	if got := listGroupedTasksInvolves(t, fx.userID); containsTaskID(got, taskID) {
		t.Fatalf("grouped tab 3 semantics violated: involves_user_id surfaced a member-direct assignee task (id=%s); full result: %v",
			taskID, got)
	}
}

// ---- workspace isolation negatives — each subquery must clamp workspace_id ----

func TestListTasks_InvolvesUserID_ExcludesOtherWorkspaceAgent(t *testing.T) {
	ctx := context.Background()
	fx := setupInvolvesFixture(t)
	// Task lives in the *primary* workspace but is assigned to an agent UUID
	// that only exists in the OTHER workspace and is owned by our user. If the
	// agent subquery is missing `a.workspace_id = $1`, this match would leak.
	taskID := insertTaskTo(t, ctx, testWorkspaceID,
		"cross-ws agent assignee must not leak", "agent", fx.otherWsAgent)
	if got := listTasksInvolves(t, fx.userID); containsTaskID(got, taskID) {
		t.Fatalf("workspace isolation violated: cross-workspace agent surfaced (id=%s); full result: %v",
			taskID, got)
	}
}

func TestListTasks_InvolvesUserID_ExcludesOtherWorkspaceLeader(t *testing.T) {
	ctx := context.Background()
	fx := setupInvolvesFixture(t)
	taskID := insertTaskTo(t, ctx, testWorkspaceID,
		"cross-ws squad-leader assignee must not leak", "squad", fx.otherWsSquadLeader)
	if got := listTasksInvolves(t, fx.userID); containsTaskID(got, taskID) {
		t.Fatalf("workspace isolation violated: cross-workspace squad-leader surfaced (id=%s); full result: %v",
			taskID, got)
	}
}

func TestListTasks_InvolvesUserID_ExcludesOtherWorkspaceSquadMember(t *testing.T) {
	ctx := context.Background()
	fx := setupInvolvesFixture(t)
	taskID := insertTaskTo(t, ctx, testWorkspaceID,
		"cross-ws squad-human-member assignee must not leak", "squad", fx.otherWsSquadMember)
	if got := listTasksInvolves(t, fx.userID); containsTaskID(got, taskID) {
		t.Fatalf("workspace isolation violated: cross-workspace squad-human-member surfaced (id=%s); full result: %v",
			taskID, got)
	}
}

func TestListTasks_InvolvesUserID_ExcludesOtherWorkspaceSquadAgentMember(t *testing.T) {
	ctx := context.Background()
	fx := setupInvolvesFixture(t)
	taskID := insertTaskTo(t, ctx, testWorkspaceID,
		"cross-ws squad-agent-member assignee must not leak", "squad", fx.otherWsSquadAgentMem)
	if got := listTasksInvolves(t, fx.userID); containsTaskID(got, taskID) {
		t.Fatalf("workspace isolation violated: cross-workspace squad-agent-member surfaced (id=%s); full result: %v",
			taskID, got)
	}
}

// ---- combo + boundary ----

// involves_user_id and creator_id must AND together — combining narrowing
// filters should never widen the result.
func TestListTasks_InvolvesUserID_CombinesWithCreatorID(t *testing.T) {
	ctx := context.Background()
	fx := setupInvolvesFixture(t)
	// Task with creator = otherID: involves matches (branch 1) but creator
	// filter must exclude it.
	exclude := insertTaskTo(t, ctx, testWorkspaceID,
		"involves matches but creator does not", "agent", fx.ownedAgentID)
	// Patch the creator to otherID directly.
	if _, err := testPool.Exec(ctx, `UPDATE task SET creator_id = $1 WHERE id = $2`, fx.otherID, exclude); err != nil {
		t.Fatalf("patch creator: %v", err)
	}

	path := fmt.Sprintf("/api/tasks?workspace_id=%s&involves_user_id=%s&creator_id=%s&limit=500",
		testWorkspaceID, fx.userID, fx.userID)
	w := httptest.NewRecorder()
	testHandler.ListTasks(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListTasks: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Tasks []TaskResponse `json:"tasks"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	got := make([]string, 0, len(resp.Tasks))
	for _, iss := range resp.Tasks {
		got = append(got, iss.ID)
	}
	if containsTaskID(got, exclude) {
		t.Fatalf("combined filter widened result: task %s with non-matching creator surfaced; full result: %v",
			exclude, got)
	}
}

func TestListTasks_InvolvesUserID_InvalidUUIDReturns400(t *testing.T) {
	path := fmt.Sprintf("/api/tasks?workspace_id=%s&involves_user_id=not-a-uuid", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.ListTasks(w, newRequest("GET", path, nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid UUID, got %d: %s", w.Code, w.Body.String())
	}
}

// Grouped path also exercises canonical-leader resolution, so a single positive
// guards the dynamic SQL builder against accidentally dropping branch (3).
func TestListGroupedTasks_InvolvesUserID_MatchesLeaderViaCanonicalRelation(t *testing.T) {
	ctx := context.Background()
	fx := setupInvolvesFixture(t)
	wantID := insertTaskTo(t, ctx, testWorkspaceID,
		"grouped: squad my agent leads", "squad", fx.squadLeaderID)
	if got := listGroupedTasksInvolves(t, fx.userID); !containsTaskID(got, wantID) {
		t.Fatalf("grouped branch (3) miss: squad-leader-via-canonical not surfaced (want %s, got %v)", wantID, got)
	}
}
