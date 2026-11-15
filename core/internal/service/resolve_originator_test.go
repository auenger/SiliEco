package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/silieco-ai/silieco/core/internal/attribution"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/featureflags"
	"github.com/silieco-ai/silieco/core/internal/runtimeapps"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/featureflag"
)

// newResolveOriginatorPool mirrors the local-postgres pattern used in
// task_claim_race_test.go: skip when the test database is unreachable
// instead of failing, so `go test ./...` stays usable in CI / clean
// developer setups that don't run Postgres.
func newResolveOriginatorPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://silieco:silieco@localhost:5432/silieco?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type stubOverlayBuilder struct {
	calls     int
	lastUser  pgtype.UUID
	lastAgent db.Agent
	resp      json.RawMessage
	apps      []runtimeapps.ConnectedApp
	err       error
	respIsNil bool
}

func (s *stubOverlayBuilder) BuildRunOverlay(_ context.Context, originatorUserID pgtype.UUID, agent db.Agent) (runtimeapps.MCPOverlayResult, error) {
	s.calls++
	s.lastUser = originatorUserID
	s.lastAgent = agent
	if s.err != nil {
		return runtimeapps.MCPOverlayResult{}, s.err
	}
	if s.respIsNil {
		return runtimeapps.MCPOverlayResult{}, nil
	}
	return runtimeapps.MCPOverlayResult{MCPOverlay: s.resp, ConnectedApps: s.apps}, nil
}

func composioMCPAppsTestFlags(enabled bool) *featureflag.Service {
	provider := featureflag.NewStaticProvider()
	provider.Set(featureflags.ComposioMCPApps, featureflag.Rule{Default: enabled})
	return featureflag.NewService(provider)
}

func TestBuildRuntimeMCPOverlaySkipsBuilderWhenComposioFlagDisabled(t *testing.T) {
	builder := &stubOverlayBuilder{
		resp: json.RawMessage(`{"mcpServers":{"composio":{"type":"http","url":"https://mcp.example/session"}}}`),
		apps: []runtimeapps.ConnectedApp{{
			Provider:    "composio",
			ServerName:  "composio",
			ToolkitSlug: "notion",
			ToolkitName: "Notion",
		}},
	}
	svc := &AgentRunService{
		Composio:     builder,
		FeatureFlags: composioMCPAppsTestFlags(false),
	}
	var userBytes [16]byte
	userBytes[15] = 1
	var agentBytes [16]byte
	agentBytes[15] = 2
	got := svc.buildRuntimeMCPOverlay(context.Background(), pgtype.UUID{Bytes: userBytes, Valid: true}, db.Agent{
		ID: pgtype.UUID{Bytes: agentBytes, Valid: true},
	})
	if builder.calls != 0 {
		t.Fatalf("BuildTaskOverlay calls = %d, want 0", builder.calls)
	}
	if len(got.Overlay) != 0 || len(got.ConnectedApps) != 0 {
		t.Fatalf("flag-off overlay = %+v; want empty", got)
	}
}

// seedOriginatorFanout builds the minimal fixture for an agent→agent
// fanout chain:
//
//	human U → (member-authored comment on task I) →
//	agent A handles task T_A with originator_user_id = U →
//	agent A posts a reply comment C (author_type=agent, source_run_id=T_A) →
//	agent B picks up C as its trigger
//
// Returns: member-authored comment id (C0), agent-authored comment id (C1, with
// source_run_id=T_A), T_A's task id, and U as pgtype.UUID. T_A's
// originator_user_id is U so the fanout / quick-create branches can prove the
// inheritance.
func seedOriginatorFanout(t *testing.T, pool *pgxpool.Pool) (memberCommentID, agentCommentID, runAID, userID, workspaceUUID pgtype.UUID) {
	t.Helper()
	ctx := context.Background()

	var workspaceID, agentAID, agentBID, runtimeID, taskID, runAIDStr, userIDStr, commentMemberID, commentAgentID string

	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Resolve Originator User', 'resolve-originator-fanout@silieco.test')
		RETURNING id
	`).Scan(&userIDStr); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(),
			`DELETE FROM "user" WHERE email = 'resolve-originator-fanout@silieco.test'`)
	})

	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('resolve-orig-ws', 'resolve-orig-ws-' || gen_random_uuid())
		RETURNING id
	`).Scan(&workspaceID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(),
			`DELETE FROM workspace WHERE id = $1`, workspaceID)
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userIDStr); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id
		) VALUES ($1, 'r', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id
	`, workspaceID, userIDStr).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_runs, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'agent-A', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, workspaceID, runtimeID, userIDStr).Scan(&agentAID); err != nil {
		t.Fatalf("seed agent A: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_runs, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'agent-B', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, workspaceID, runtimeID, userIDStr).Scan(&agentBID); err != nil {
		t.Fatalf("seed agent B: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'fanout-task', 'member', $2)
		RETURNING id
	`, workspaceID, userIDStr).Scan(&taskID); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	// Agent A's task carries the originator (the human U). This is the
	// row the resolver must follow back through comment.source_run_id.
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_run (
			agent_id, runtime_id, task_id, status, priority, originator_user_id, accountable_user_id
		)
		VALUES ($1, $2, $3, 'completed', 0, $4, $4)
		RETURNING id
	`, agentAID, runtimeID, taskID, userIDStr).Scan(&runAIDStr); err != nil {
		t.Fatalf("seed task A: %v", err)
	}

	// Member-authored comment (no source_run_id).
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (task_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'human comment')
		RETURNING id
	`, taskID, workspaceID, userIDStr).Scan(&commentMemberID); err != nil {
		t.Fatalf("seed member comment: %v", err)
	}

	// Agent-authored comment whose source_run_id points at task A.
	// resolveOriginatorFromTriggerComment must inherit task A's originator.
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (task_id, workspace_id, author_type, author_id, content, source_run_id)
		VALUES ($1, $2, 'agent', $3, 'agent comment', $4)
		RETURNING id
	`, taskID, workspaceID, agentAID, runAIDStr).Scan(&commentAgentID); err != nil {
		t.Fatalf("seed agent comment: %v", err)
	}

	memberCommentID = util.MustParseUUID(commentMemberID)
	agentCommentID = util.MustParseUUID(commentAgentID)
	runAID = util.MustParseUUID(runAIDStr)
	userID = util.MustParseUUID(userIDStr)
	workspaceUUID = util.MustParseUUID(workspaceID)
	return
}

// TestResolveOriginatorFromTriggerComment_MemberAuthored — the base case:
// a comment authored by a workspace member IS the top-of-chain. The
// originator is the comment's own author_id.
func TestResolveOriginatorFromTriggerComment_MemberAuthored(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	memberCommentID, _, _, userID, workspaceID := seedOriginatorFanout(t, pool)
	svc := &AgentRunService{Queries: db.New(pool)}

	got := svc.resolveOriginatorFromTriggerComment(context.Background(), workspaceID, memberCommentID)
	if !got.Valid {
		t.Fatalf("expected valid originator for member-authored comment, got invalid")
	}
	if got.Bytes != userID.Bytes {
		t.Errorf("originator = %s, want %s", util.UUIDToString(got), util.UUIDToString(userID))
	}
}

// TestResolveOriginatorFromTriggerComment_AgentAuthoredInheritsFromParent
// — the load-bearing fanout case. Agent A finished a task it ran on
// behalf of human U; A then posts a comment that triggers agent B. The
// trigger comment's author is A (not a human), but resolving the
// originator must walk comment.source_run_id → parent task →
// parent.originator_user_id, yielding U.
func TestResolveOriginatorFromTriggerComment_AgentAuthoredInheritsFromParent(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	_, agentCommentID, _, userID, workspaceID := seedOriginatorFanout(t, pool)
	svc := &AgentRunService{Queries: db.New(pool)}

	got := svc.resolveOriginatorFromTriggerComment(context.Background(), workspaceID, agentCommentID)
	if !got.Valid {
		t.Fatalf("expected valid originator inherited from parent task, got invalid")
	}
	if got.Bytes != userID.Bytes {
		t.Errorf("originator = %s, want %s (parent task's originator_user_id)",
			util.UUIDToString(got), util.UUIDToString(userID))
	}
}

// TestAttributionForTaskRun_SystemCommentFallsThroughToTaskProvenance covers
// the Stage-completion cascade (SILI-4302; raised by Bohan). Closing the last
// sub-task in a Stage wakes the parent's assignee agent through a SYSTEM-authored
// child-done comment that threads no actor. That system comment carries no human,
// so attribution must NOT stop at it (which would degrade to owner_fallback, the
// agent's own owner) — it must fall through to the PARENT task's own provenance
// and attribute to the human who caused the parent task to exist. Here the parent
// was created by an agent on behalf of userID (agent_create origin), so the woken
// run is delegation-accountable to userID.
func TestAttributionForTaskRun_SystemCommentFallsThroughToTaskProvenance(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	_, _, parentRunID, userID, _ := seedOriginatorFanout(t, pool)
	ctx := context.Background()

	// Plant a system-authored comment on the seed's task (mirrors the child-done
	// comment). Its task/workspace are derived from the origin task's task.
	var taskIDStr, workspaceIDStr string
	if err := pool.QueryRow(ctx, `
		SELECT i.id::text, i.workspace_id::text
		FROM agent_run t JOIN task i ON i.id = t.task_id
		WHERE t.id = $1
	`, util.UUIDToString(parentRunID)).Scan(&taskIDStr, &workspaceIDStr); err != nil {
		t.Fatalf("load task/workspace: %v", err)
	}
	var systemCommentIDStr string
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (task_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'system', '00000000-0000-0000-0000-000000000000', 'child done', 'system')
		RETURNING id
	`, taskIDStr, workspaceIDStr).Scan(&systemCommentIDStr); err != nil {
		t.Fatalf("seed system comment: %v", err)
	}
	systemCommentID := util.MustParseUUID(systemCommentIDStr)

	svc := &AgentRunService{Queries: db.New(pool)}
	// Parent task created by an agent on behalf of userID (agent_create origin).
	// WorkspaceID is set so the workspace-scoped trigger-comment lookup (SILI-4252)
	// finds the system comment and classifies it (author_type=system) before the
	// fall-through to task provenance.
	task := db.Task{
		CreatorType: "agent",
		OriginType:  pgtype.Text{String: "agent_create", Valid: true},
		OriginID:    parentRunID,
		WorkspaceID: util.MustParseUUID(workspaceIDStr),
	}

	got := svc.attributionForTaskRun(ctx, task, systemCommentID, attribution.SourceDelegation, pgtype.UUID{})
	if got.Source != attribution.SourceDelegation {
		t.Fatalf("source = %q, want delegation (system comment must fall through to task provenance, not owner_fallback)", got.Source)
	}
	if !got.UserID.Valid || got.UserID.Bytes != userID.Bytes {
		t.Errorf("accountable = %s, want %s (the human who caused the parent task to exist)",
			util.UUIDToString(got.UserID), util.UUIDToString(userID))
	}
}

// TestResolveOriginatorForTaskRun_MemberCreatedNoComment covers direct task
// assignment/creation: there is no trigger comment to inspect, but the task's
// human creator is still the run originator for Composio overlay gating.
func TestResolveOriginatorForTaskRun_MemberCreatedNoComment(t *testing.T) {
	userID := util.MustParseUUID("11111111-1111-1111-1111-111111111111")
	svc := &AgentRunService{}
	task := db.Task{CreatorType: "member", CreatorID: userID}

	got := svc.resolveOriginatorForTaskRun(context.Background(), task, pgtype.UUID{})
	if !got.Valid {
		t.Fatalf("expected valid originator for member-created task, got invalid")
	}
	if got.Bytes != userID.Bytes {
		t.Errorf("originator = %s, want %s", util.UUIDToString(got), util.UUIDToString(userID))
	}
}

// TestResolveOriginatorForTaskRun_QuickCreateTaskInheritsParentRun covers
// agent-created tasks that have an explicit quick-create origin stamp. The
// task creator is the agent, but the top-of-chain human is stored on the
// parent quick-create task and must be inherited for downstream dispatch.
func TestResolveOriginatorForTaskRun_QuickCreateTaskInheritsParentRun(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	_, _, parentRunID, userID, _ := seedOriginatorFanout(t, pool)
	svc := &AgentRunService{Queries: db.New(pool)}
	task := db.Task{
		CreatorType: "agent",
		OriginType:  pgtype.Text{String: "quick_create", Valid: true},
		OriginID:    parentRunID,
	}

	got := svc.resolveOriginatorForTaskRun(context.Background(), task, pgtype.UUID{})
	if !got.Valid {
		t.Fatalf("expected quick-create task to inherit originator, got invalid")
	}
	if got.Bytes != userID.Bytes {
		t.Errorf("originator = %s, want %s", util.UUIDToString(got), util.UUIDToString(userID))
	}
}

// TestResolveOriginatorForTaskRun_AgentCreateTaskInheritsParentRun covers
// the SILI-4305 fix: an agent that creates an task through the ordinary
// `task create` path gets origin_type='agent_create' + origin_id=<acting
// task>. The task creator is the agent, but the top-of-chain human lives on
// that acting task and must be inherited so downstream assignment /
// squad-leader runs (and the A2A mentions they emit) keep the originator.
func TestResolveOriginatorForTaskRun_AgentCreateTaskInheritsParentRun(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	_, _, parentRunID, userID, _ := seedOriginatorFanout(t, pool)
	svc := &AgentRunService{Queries: db.New(pool)}
	task := db.Task{
		CreatorType: "agent",
		OriginType:  pgtype.Text{String: "agent_create", Valid: true},
		OriginID:    parentRunID,
	}

	got := svc.resolveOriginatorForTaskRun(context.Background(), task, pgtype.UUID{})
	if !got.Valid {
		t.Fatalf("expected agent_create task to inherit originator, got invalid")
	}
	if got.Bytes != userID.Bytes {
		t.Errorf("originator = %s, want %s", util.UUIDToString(got), util.UUIDToString(userID))
	}
}

// TestOriginatorForTaskRun_MatchesResolverForAgentCreate pins the gate/enqueue
// consistency guarantee from SILI-4305: the exported OriginatorForTaskRun
// (used by the squad-leader access gate) must return the SAME human the
// unexported resolver persists on the task row. If these drift, an
// agent-created task could be attributed correctly on the task row yet denied
// by a gate that computed a different (empty) originator.
func TestOriginatorForTaskRun_MatchesResolverForAgentCreate(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	_, _, parentRunID, userID, _ := seedOriginatorFanout(t, pool)
	svc := &AgentRunService{Queries: db.New(pool)}
	task := db.Task{
		CreatorType: "agent",
		OriginType:  pgtype.Text{String: "agent_create", Valid: true},
		OriginID:    parentRunID,
	}

	gate := svc.OriginatorForTaskRun(context.Background(), task, pgtype.UUID{})
	write := svc.resolveOriginatorForTaskRun(context.Background(), task, pgtype.UUID{})
	if gate.Bytes != write.Bytes || gate.Valid != write.Valid {
		t.Fatalf("gate originator %s != write originator %s",
			util.UUIDToString(gate), util.UUIDToString(write))
	}
	if !gate.Valid || gate.Bytes != userID.Bytes {
		t.Errorf("gate originator = %s, want %s", util.UUIDToString(gate), util.UUIDToString(userID))
	}
}

// TestEnqueueRunForTaskStoresRuntimeMCPOverlayInQueuedRow guards the race
// where the daemon could poll-claim a newly inserted queued task while the old
// post-insert overlay updater was still making the outbound Composio session
// call. The overlay must be computed before INSERT and returned on the queued
// row.
func TestEnqueueRunForTaskStoresRuntimeMCPOverlayInQueuedRow(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("runtime-overlay-insert-%d@silieco.test", suffix)
	workspaceSlug := fmt.Sprintf("runtime-overlay-insert-%d", suffix)

	var userIDStr, workspaceIDStr, runtimeIDStr, agentIDStr, taskIDStr string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Runtime Overlay Insert User', $1)
		RETURNING id
	`, email).Scan(&userIDStr); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userIDStr)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('runtime overlay insert ws', $1)
		RETURNING id
	`, workspaceSlug).Scan(&workspaceIDStr); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceIDStr)
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceIDStr, userIDStr); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id
		) VALUES ($1, 'runtime-overlay-r', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id
	`, workspaceIDStr, userIDStr).Scan(&runtimeIDStr); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_runs, owner_id,
			instructions, custom_env, custom_args, composio_toolkit_allowlist
		)
		VALUES ($1, 'runtime-overlay-agent', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb, ARRAY['notion'])
		RETURNING id
	`, workspaceIDStr, runtimeIDStr, userIDStr).Scan(&agentIDStr); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO task (
			workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority
		)
		VALUES ($1, 'runtime overlay task', 'member', $2, 'agent', $3, 'medium')
		RETURNING id
	`, workspaceIDStr, userIDStr, agentIDStr).Scan(&taskIDStr); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	overlay := json.RawMessage(`{"mcpServers":{"composio":{"type":"http","url":"https://mcp.example/session"}}}`)
	builder := &stubOverlayBuilder{
		resp: overlay,
		apps: []runtimeapps.ConnectedApp{{
			Provider:    "composio",
			ServerName:  "composio",
			ToolkitSlug: "notion",
			ToolkitName: "Notion",
		}},
	}
	svc := &AgentRunService{Queries: q, TxStarter: pool, Bus: events.New(), Composio: builder, FeatureFlags: composioMCPAppsTestFlags(true)}
	userID := util.MustParseUUID(userIDStr)
	agentRun, err := svc.EnqueueRunForTask(ctx, db.Task{
		ID:           util.MustParseUUID(taskIDStr),
		AssigneeID:   util.MustParseUUID(agentIDStr),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    userID,
		WorkspaceID:  util.MustParseUUID(workspaceIDStr),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	})
	if err != nil {
		t.Fatalf("EnqueueTaskForTask: %v", err)
	}
	if builder.calls != 1 {
		t.Fatalf("BuildTaskOverlay calls = %d, want 1", builder.calls)
	}
	if len(agentRun.RuntimeMcpOverlay) == 0 {
		t.Fatalf("returned queued task has empty runtime_mcp_overlay")
	}
	if len(agentRun.RuntimeConnectedApps) == 0 {
		t.Fatalf("returned queued task has empty runtime_connected_apps")
	}
	var storedOverlay []byte
	var storedApps []byte
	if err := pool.QueryRow(ctx, `SELECT runtime_mcp_overlay, runtime_connected_apps FROM agent_run WHERE id = $1`, agentRun.ID).Scan(&storedOverlay, &storedApps); err != nil {
		t.Fatalf("read stored overlay: %v", err)
	}
	if len(storedOverlay) == 0 {
		t.Fatal("stored queued task has empty runtime_mcp_overlay")
	}
	if len(storedApps) == 0 {
		t.Fatal("stored queued task has empty runtime_connected_apps")
	}
	var apps []runtimeapps.ConnectedApp
	if err := json.Unmarshal(storedApps, &apps); err != nil {
		t.Fatalf("unmarshal stored connected apps: %v", err)
	}
	if len(apps) != 1 || apps[0].ToolkitSlug != "notion" {
		t.Fatalf("stored connected apps = %+v; want notion", apps)
	}
}

// TestResolveOriginatorFromTriggerComment_InvalidCommentID — defensive
// branch. An invalid pgtype.UUID must short-circuit before any DB query
// and return invalid.
func TestResolveOriginatorFromTriggerComment_InvalidCommentID(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	svc := &AgentRunService{Queries: db.New(pool)}
	got := svc.resolveOriginatorFromTriggerComment(context.Background(), pgtype.UUID{}, pgtype.UUID{})
	if got.Valid {
		t.Errorf("invalid comment id must yield invalid originator, got %s", util.UUIDToString(got))
	}
}
