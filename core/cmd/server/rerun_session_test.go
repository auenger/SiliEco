package main

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/realtime"
	"github.com/silieco-ai/silieco/core/internal/service"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// requireSessionExcluded asserts a resume lookup found nothing BECAUSE the
// session was excluded — not because the query itself blew up.
//
// The bare `if err == nil && prior.SessionID.Valid` form these tests shared is
// false-green: any real fault (undefined column, syntax error, dead
// connection) makes err non-nil, the condition false, and the test pass. Run
// against a database missing this PR's new column, the exclusion tests
// reported PASS on a SQLSTATE 42703. Requiring pgx.ErrNoRows specifically is
// what makes a passing test mean "the filter worked".
func requireSessionExcluded(t *testing.T, sessionID pgtype.Text, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected the session to be excluded, but the lookup returned %q", sessionID.String)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("lookup failed for an unrelated reason, so this test proves nothing: %v", err)
	}
}

// setupRerunTestFixture creates an task assigned to the integration test
// agent and returns (taskID, agentID, runtimeID).
func setupRerunTestFixture(t *testing.T) (string, string, string) {
	t.Helper()
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		  AND a.archived_at IS NULL
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	var taskID string
	// Pick the next per-workspace number to avoid colliding with the
	// uq_task_workspace_number unique constraint when multiple fixtures
	// coexist in the same test (e.g. TestRerunTaskRejectsCrossTaskRun).
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number)
		SELECT $1, 'Rerun test task', 'todo', 'none', 'member', m.user_id, 'agent', $2,
		       (SELECT COALESCE(MAX(number), 0) + 1 FROM task WHERE workspace_id = $1)
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&taskID); err != nil {
		t.Fatalf("failed to create test task: %v", err)
	}

	return taskID, agentID, runtimeID
}

func cleanupRerunFixture(t *testing.T, taskID string) {
	t.Helper()
	ctx := context.Background()
	testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, taskID)
	testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, taskID)
}

// TestGetLastTaskSessionExcludesPoisonedFailures asserts that the
// (agent_id, task_id) resume lookup skips failed tasks whose
// failure_reason classifies them as poisoned terminal output. This is the
// SQL-level half of the rerun-poisoned-session fix: without the filter, a
// rerun would inherit the same session and replay the same bad output.
func TestGetLastRunSessionExcludesPoisonedFailures(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()

	// Insert an older failed task with a poisoned classifier and a session_id.
	// The poisoned task is the *most recent* one, so without the filter the
	// resume lookup would return its session_id.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'HEALTHY-SESSION', '/tmp/healthy', 'timeout')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert healthy failed run: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'POISONED-SESSION', '/tmp/poisoned', 'iteration_limit')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert poisoned failed run: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastRunSession(ctx, db.GetLastRunSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		TaskID:  pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if !prior.SessionID.Valid {
		t.Fatal("expected to fall back to the healthy failed session, got no session")
	}
	if prior.SessionID.String == "POISONED-SESSION" {
		t.Fatal("rerun would inherit poisoned session — filter is not active")
	}
	if prior.SessionID.String != "HEALTHY-SESSION" {
		t.Fatalf("expected HEALTHY-SESSION, got %q", prior.SessionID.String)
	}
}

// TestGetLastTaskSessionFallsBackWhenLatestSessionBlanked is the claim-side
// half of SILI-5305: when the most recent task recorded NO session_id (the
// daemon withheld an unresumable Codex session so it would not poison the next
// follow-up), the resume lookup skips that row via `session_id IS NOT NULL`
// and falls back to the most recent task that does have a session — so the next
// follow-up continues from the last real conversation instead of the dropped
// pointer that produced the reported regression.
func TestGetLastRunSessionFallsBackWhenLatestSessionBlanked(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()

	// Older completed task with a good, recorded session (its rollout is real).
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'OLD-GOOD-SESSION', '/tmp/good')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert older completed run: %v", err)
	}

	// Newer failed task whose unresumable session was withheld (session_id NULL).
	// This is the row a naive lookup would return, cold-starting the next run.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', NULL, '/tmp/blanked', 'timeout')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert newer blanked run: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastRunSession(ctx, db.GetLastRunSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		TaskID:  pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if !prior.SessionID.Valid {
		t.Fatal("expected fallback to the older recorded session, got no session")
	}
	if prior.SessionID.String != "OLD-GOOD-SESSION" {
		t.Fatalf("expected OLD-GOOD-SESSION, got %q", prior.SessionID.String)
	}
}

// TestGetLastTaskSessionExcludesEmptyHistoryMessage is the SQL half of the
// GH #6066 fix. The daemon now classifies an empty-message rejection as
// api_invalid_request, but daemons upgrade on their own cadence — a self-host
// install still running an older one writes agent_error.unknown, and only the
// query's text guard keeps the next task off the poisoned session. This drives
// that exact row shape.
func TestGetLastRunSessionExcludesEmptyHistoryMessage(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()

	// The row an un-upgraded daemon writes for GH #6066: the reason is the
	// catchall, so the failure_reason blacklist does not fire.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'POISONED-EMPTY-MSG', '/tmp/poisoned', 'agent_error.unknown',
		        'Invalid request: the message at position 37 with role ''assistant'' must not be empty')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert poisoned run: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastRunSession(ctx, db.GetLastRunSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		TaskID:  pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
	})
	requireSessionExcluded(t, prior.SessionID, err)
}

// TestGetLastTaskSessionKeepsToolEmptinessError is the narrowness half: the
// text guard must not swallow a healthy session because some tool complained
// that a field was empty. A false positive here silently drops conversation
// context on every follow-up, which is worse than the bug it guards against.
func TestGetLastRunSessionKeepsToolEmptinessError(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'HEALTHY-SESSION', '/tmp/healthy', 'agent_error.unknown',
		        'validation error: field must not be empty')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert run: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastRunSession(ctx, db.GetLastRunSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		TaskID:  pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if !prior.SessionID.Valid || prior.SessionID.String != "HEALTHY-SESSION" {
		t.Fatalf("a tool emptiness error must not retire the session, got %+v", prior.SessionID)
	}
}

// TestCompletedTaskRolloutMissingWithholdsAndDisclosesGap is the cross-layer
// regression for SILI-5305 Must-fix 1: a COMPLETED follow-up whose Codex rollout
// is missing (the #5934 case — the user waits for each turn to finish) must (1)
// NOT be handed to the next follow-up as its resume pointer, and (2) NOT be
// resumed silently — the next claim must still disclose the continuity gap. It
// drives the REAL terminal write (CompleteAgentTask with session_rollout_missing,
// which clears session_id and flags the row in one statement) rather than a
// manual marker, then checks GetLastTaskSession (falls back to the older good
// session) + GetLatestTaskRolloutMissing (the disclosure signal
// buildClaimedTaskResponse reads).
func TestCompletedRunRolloutMissingWithholdsAndDisclosesGap(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()
	queries := db.New(testPool)

	// Older completed turn with a real, recorded session.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'OLD-GOOD-SESSION', '/tmp/good')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert older completed run: %v", err)
	}

	// Newer turn still 'running' with a mid-flight-pinned session — exactly the
	// pointer the reported bug propagates. CompleteAgentTask below finishes it
	// with session_rollout_missing=true (the daemon found no rollout).
	var newerRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '1 minute', 'NEW-ROLLOUT-MISSING', '/tmp/newer')
		RETURNING id
	`, agentID, runtimeID, taskID).Scan(&newerRunID); err != nil {
		t.Fatalf("insert newer running run: %v", err)
	}

	// The real terminal write withholds the session and flags the row atomically.
	done, err := queries.CompleteAgentRun(ctx, db.CompleteAgentRunParams{
		ID:                    pgtype.UUID{Bytes: parseUUIDBytes(newerRunID), Valid: true},
		Result:                []byte(`{"output":"done"}`),
		SessionID:             pgtype.Text{String: "NEW-ROLLOUT-MISSING", Valid: true},
		WorkDir:               pgtype.Text{String: "/tmp/newer", Valid: true},
		SessionRolloutMissing: true,
	})
	if err != nil {
		t.Fatalf("CompleteAgentTask: %v", err)
	}
	// The terminal SQL forced the pinned session NULL and flagged the row in one
	// statement — not two writes that a concurrent retry could interleave.
	if done.SessionID.Valid {
		t.Fatalf("expected withheld session_id to be NULL, got %q", done.SessionID.String)
	}
	if !done.SessionRolloutMissing {
		t.Fatal("expected session_rollout_missing to be set on the terminal row")
	}

	// (1) The bad session is NOT handed out — resume falls back to the older good one.
	prior, err := queries.GetLastRunSession(ctx, db.GetLastRunSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		TaskID:  pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if !prior.SessionID.Valid || prior.SessionID.String != "OLD-GOOD-SESSION" {
		t.Fatalf("expected fallback to OLD-GOOD-SESSION, got valid=%v %q", prior.SessionID.Valid, prior.SessionID.String)
	}

	// (2) The gap is disclosed — the resume is NOT silently claimed as complete.
	missing, err := queries.GetLatestRunRolloutMissing(ctx, db.GetLatestRunRolloutMissingParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		TaskID:  pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLatestTaskRolloutMissing failed: %v", err)
	}
	if !missing {
		t.Fatal("expected the continuity gap to be flagged so the next claim discloses it")
	}
}

// TestFailedTaskRolloutMissingForcesNullOverMidFlightPin covers the failure-path
// half of SILI-5305 Must-fix 1: FailAgentTask normally COALESCE-preserves a
// mid-flight-pinned session, which would let an auto-retry created in the SAME
// transaction inherit a rollout-missing pointer. With session_rollout_missing
// the terminal UPDATE forces session_id NULL and flags the row in ONE statement,
// so there is no window between the fail committing and a separate marker where
// the retry could observe the bad pointer or a false gap flag.
func TestFailedRunRolloutMissingForcesNullOverMidFlightPin(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()
	queries := db.New(testPool)

	// Running task with a mid-flight-pinned session, as UpdateAgentTaskSession
	// would have left it before the run failed with no rollout on disk.
	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '1 minute', 'PINNED-BUT-NO-ROLLOUT', '/tmp/wd')
		RETURNING id
	`, agentID, runtimeID, taskID).Scan(&runID); err != nil {
		t.Fatalf("insert running run: %v", err)
	}

	// The daemon withheld it: FailTask sends an empty session_id (Valid:false)
	// AND session_rollout_missing=true. The CASE must win over the COALESCE.
	failed, err := queries.FailAgentRun(ctx, db.FailAgentRunParams{
		ID:                    pgtype.UUID{Bytes: parseUUIDBytes(runID), Valid: true},
		Error:                 pgtype.Text{String: "runtime went offline", Valid: true},
		FailureReason:         pgtype.Text{String: "timeout", Valid: true},
		SessionID:             pgtype.Text{Valid: false},
		WorkDir:               pgtype.Text{Valid: false},
		SessionRolloutMissing: true,
	})
	if err != nil {
		t.Fatalf("FailAgentTask: %v", err)
	}
	if failed.SessionID.Valid {
		t.Fatalf("expected the mid-flight pin to be forced NULL, got %q", failed.SessionID.String)
	}
	if !failed.SessionRolloutMissing {
		t.Fatal("expected session_rollout_missing to be set on the failed row")
	}
}

// TestGetLastTaskSessionFallbackPoisonedClassifier covers the second
// poisoned classifier so adding a third doesn't silently break this rule.
func TestGetLastRunSessionFallbackPoisonedClassifier(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '5 seconds', now() - interval '5 seconds', 'POISONED-FALLBACK', '/tmp/poisoned', 'agent_fallback_message')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert poisoned failed run: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastRunSession(ctx, db.GetLastRunSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		TaskID:  pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
	})
	requireSessionExcluded(t, prior.SessionID, err)
}

// TestGetLastTaskSessionExcludesAPIInvalidRequest covers the SILI-1921
// case: an Anthropic 400 invalid_request_error (e.g. an oversized or
// malformed image baked into the conversation) bakes the bad message
// into the session history, so resuming would replay the same 400
// forever. The daemon classifies these as 'api_invalid_request' and the
// SQL filter must skip them on the resume lookup.
func TestGetLastRunSessionExcludesAPIInvalidRequest(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '5 seconds', now() - interval '5 seconds', 'POISONED-API400', '/tmp/poisoned', 'api_invalid_request')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert poisoned failed run: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastRunSession(ctx, db.GetLastRunSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		TaskID:  pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
	})
	requireSessionExcluded(t, prior.SessionID, err)
}

// TestGetLastTaskSessionExcludesCodexSemanticInactivity covers Codex
// semantic inactivity timeouts: the failed task did establish a session, but
// resuming it can replay the same stuck Codex state. The resume lookup must
// skip that session.
func TestGetLastRunSessionExcludesCodexSemanticInactivity(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'HEALTHY-SESSION', '/tmp/healthy', 'timeout')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert healthy failed run: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'CODEX-STUCK-SESSION', '/tmp/codex-stuck', 'codex_semantic_inactivity',
		        'codex semantic inactivity timeout after 10m0s without agent progress (last activity: tool-result:exec_command)')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert codex semantic inactivity run: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastRunSession(ctx, db.GetLastRunSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		TaskID:  pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if prior.SessionID.String != "HEALTHY-SESSION" {
		t.Fatalf("expected HEALTHY-SESSION, got %q", prior.SessionID.String)
	}
}

func TestCreateRetryRunFreshensCodexSemanticInactivity(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()

	var parentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (
			agent_id, runtime_id, task_id, status, priority,
			started_at, completed_at, session_id, work_dir, failure_reason,
			attempt, max_attempts
		)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute',
		        'CODEX-STUCK-SESSION', '/tmp/codex-stuck', 'codex_semantic_inactivity', 1, 2)
		RETURNING id
	`, agentID, runtimeID, taskID).Scan(&parentID); err != nil {
		t.Fatalf("insert codex semantic inactivity parent run: %v", err)
	}

	queries := db.New(testPool)
	child, err := queries.CreateRetryRun(ctx, db.CreateRetryRunParams{ID: pgtype.UUID{Bytes: parseUUIDBytes(parentID), Valid: true}})
	if err != nil {
		t.Fatalf("CreateRetryTask failed: %v", err)
	}
	if child.SessionID.Valid {
		t.Fatalf("expected retry child to drop poisoned session_id, got %q", child.SessionID.String)
	}
	if child.WorkDir.Valid {
		t.Fatalf("expected retry child to drop poisoned work_dir, got %q", child.WorkDir.String)
	}
	if !child.ForceFreshSession {
		t.Fatal("expected retry child to force a fresh session")
	}
	if child.Attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", child.Attempt)
	}
}

func TestCreateRetryRunKeepsOrdinaryTimeoutSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()

	var parentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (
			agent_id, runtime_id, task_id, status, priority,
			started_at, completed_at, session_id, work_dir, failure_reason,
			attempt, max_attempts
		)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute',
		        'ORDINARY-TIMEOUT-SESSION', '/tmp/ordinary-timeout', 'timeout', 1, 2)
		RETURNING id
	`, agentID, runtimeID, taskID).Scan(&parentID); err != nil {
		t.Fatalf("insert ordinary timeout parent run: %v", err)
	}

	queries := db.New(testPool)
	child, err := queries.CreateRetryRun(ctx, db.CreateRetryRunParams{ID: pgtype.UUID{Bytes: parseUUIDBytes(parentID), Valid: true}})
	if err != nil {
		t.Fatalf("CreateRetryTask failed: %v", err)
	}
	if !child.SessionID.Valid || child.SessionID.String != "ORDINARY-TIMEOUT-SESSION" {
		t.Fatalf("expected retry child to inherit session_id, got %+v", child.SessionID)
	}
	if !child.WorkDir.Valid || child.WorkDir.String != "/tmp/ordinary-timeout" {
		t.Fatalf("expected retry child to inherit work_dir, got %+v", child.WorkDir)
	}
	if child.ForceFreshSession {
		t.Fatal("expected ordinary timeout retry child to keep resume enabled")
	}
	if child.Attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", child.Attempt)
	}
}

// TestGetLastTaskSessionExcludesLegacyAPI400 is the SILI-1921 legacy
// regression: pre-fix rows are tagged failure_reason='agent_error' even
// though their error text contains the canonical Anthropic 400
// invalid_request_error marker. The daemon-side classifier only fires
// on new failures, so without a defensive ILIKE clause the resume query
// would happily return one of those rows on the next claim and
// re-poison every retry of an already-broken task (e.g. SILI-1918,
// which already has three poisoned 'agent_error' rows when this PR
// merges). The SQL must skip the bad row on text shape alone.
func TestGetLastRunSessionExcludesLegacyAPI400(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()

	// Legacy poisoned row: failure_reason was the pre-fix default
	// 'agent_error' but the error text shows it was an API 400
	// invalid_request_error. Migration 079 backfills these to
	// 'api_invalid_request', but the SQL filter must still exclude
	// them via ILIKE on the off chance a row escapes the migration
	// (deploy window, manual relabel, etc.).
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'LEGACY-POISONED', '/tmp/legacy', 'agent_error',
		        'API Error: 400 {"type":"error","error":{"type":"invalid_request_error","message":"Could not process image"}}')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert legacy poisoned run: %v", err)
	}

	// Newly classified poisoned row coexisting with the legacy one.
	// Without the ILIKE clause, ORDER BY completed_at DESC would
	// skip this row (failure_reason filter fires) and fall back to
	// the legacy row (failure_reason filter MISSES) — the exact
	// wormhole GPT-Boy flagged on PR review.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'NEW-POISONED', '/tmp/new', 'api_invalid_request',
		        'API Error: 400 {"type":"error","error":{"type":"invalid_request_error","message":"Could not process image"}}')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert new poisoned run: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastRunSession(ctx, db.GetLastRunSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		TaskID:  pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
	})
	requireSessionExcluded(t, prior.SessionID, err)
}

// TestGetLastTaskSessionKeepsBenignAgentErrorWithSession asserts the
// ILIKE clause is narrow enough that ordinary 'agent_error' failures
// (timeouts, tool errors, transient glue failures) still let the next
// task resume the prior session. Without this guard rail, the SILI-1921
// fix would regress SILI-1128's resume contract for everything else.
func TestGetLastRunSessionKeepsBenignAgentErrorWithSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '30 seconds', now() - interval '30 seconds', 'HEALTHY-RESUMABLE', '/tmp/healthy', 'agent_error',
		        'tool execution failed: connection refused')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert benign failed run: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastRunSession(ctx, db.GetLastRunSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		TaskID:  pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if !prior.SessionID.Valid || prior.SessionID.String != "HEALTHY-RESUMABLE" {
		t.Fatalf("expected to resume HEALTHY-RESUMABLE, got %q (valid=%v)", prior.SessionID.String, prior.SessionID.Valid)
	}
}

// TestRerunTaskSetsForceFreshSession asserts the manual rerun flow flags
// the new task so the daemon claim handler skips the resume lookup. This
// is the call-site half of the fix: even if the SQL filter ever misses a
// poisoned classifier, manual rerun never resumes.
func TestRerunTaskSetsForceFreshSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, _, _ := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()
	queries := db.New(testPool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	agentRunService := service.NewAgentRunService(queries, nil, hub, bus)

	agentRun, err := agentRunService.RerunTask(ctx, pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true}, pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, nil)
	if err != nil {
		t.Fatalf("RerunTask failed: %v", err)
	}
	if agentRun == nil {
		t.Fatal("RerunTask returned nil task")
	}
	if !agentRun.ForceFreshSession {
		t.Fatal("expected manual rerun to set force_fresh_session=true")
	}
}

// TestRerunTaskTargetsSourceRunAgent asserts that when a source task ID is
// supplied (the execution-log retry-button path), the rerun targets the agent
// that ran that specific past task — not the task's current assignee.
// Without this, clicking retry on a row whose agent has since been displaced
// (squad worker, @-mention agent, or a prior assignee) re-fires the new
// assignee instead, which is the SILI-2457 bug.
func TestRerunTaskTargetsSourceRunAgent(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, primaryAgentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()

	// Create a second agent in the same workspace + runtime so we can stand
	// in as a "row whose agent is no longer the task assignee" — e.g. a
	// squad worker or an @-mentioned agent. The task's assignee is still
	// the primary agent; the rerun must target this secondary one because
	// that's whose task row the user clicked.
	var secondaryAgentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_runs, owner_id
		)
		SELECT a.workspace_id, 'Rerun Secondary Agent', '', 'cloud', '{}'::jsonb,
		       a.runtime_id, 'workspace', 1, a.owner_id
		FROM agent a WHERE a.id = $1
		RETURNING id
	`, primaryAgentID).Scan(&secondaryAgentID); err != nil {
		t.Fatalf("create secondary agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_run WHERE agent_id = $1`, secondaryAgentID)
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, secondaryAgentID)
	})

	// Insert a failed past task on this task under the secondary agent —
	// the row the user is about to click retry on.
	var sourceRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority,
		                              started_at, completed_at, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0,
		        now() - interval '1 minute', now() - interval '30 seconds', 'agent_error')
		RETURNING id
	`, secondaryAgentID, runtimeID, taskID).Scan(&sourceRunID); err != nil {
		t.Fatalf("insert source run: %v", err)
	}

	queries := db.New(testPool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	agentRunService := service.NewAgentRunService(queries, nil, hub, bus)

	agentRun, err := agentRunService.RerunTask(
		ctx,
		pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
		pgtype.UUID{Bytes: parseUUIDBytes(sourceRunID), Valid: true},
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("RerunTask failed: %v", err)
	}
	if agentRun == nil {
		t.Fatal("RerunTask returned nil task")
	}

	gotAgent := util.UUIDToString(agentRun.AgentID)
	if gotAgent != secondaryAgentID {
		t.Fatalf("rerun targeted wrong agent: got %s, want %s (task assignee is %s — must not be picked)",
			gotAgent, secondaryAgentID, primaryAgentID)
	}
	if !agentRun.ForceFreshSession {
		t.Fatal("expected per-row rerun to also set force_fresh_session=true")
	}
}

// TestRerunTaskRejectsCrossTaskRun asserts a source task whose TaskID
// does not match the rerun target is rejected — both as defense-in-depth
// against malicious requests and because picking up an unrelated task's
// agent would silently misroute the rerun.
func TestRerunTaskRejectsCrossTaskRun(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskAID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskAID) })

	ctx := context.Background()

	// Second task in the same workspace, with a task that does NOT belong
	// to task A. The handler must reject this. Take the next available
	// per-workspace number so the uq_task_workspace_number constraint
	// (both tasks default to number=0 otherwise) doesn't fire before the
	// rerun assertion can.
	var taskBID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number)
		SELECT $1, 'Rerun cross-task test', 'todo', 'none', 'member', m.user_id, 'agent', $2,
		       (SELECT COALESCE(MAX(number), 0) + 1 FROM task WHERE workspace_id = $1)
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&taskBID); err != nil {
		t.Fatalf("create second task: %v", err)
	}
	t.Cleanup(func() { cleanupRerunFixture(t, taskBID) })

	var crossRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority,
		                              started_at, completed_at, failure_reason)
		VALUES ($1, $2, $3, 'failed', 0,
		        now() - interval '1 minute', now() - interval '30 seconds', 'agent_error')
		RETURNING id
	`, agentID, runtimeID, taskBID).Scan(&crossRunID); err != nil {
		t.Fatalf("insert cross run: %v", err)
	}

	queries := db.New(testPool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	agentRunService := service.NewAgentRunService(queries, nil, hub, bus)

	_, err := agentRunService.RerunTask(
		ctx,
		pgtype.UUID{Bytes: parseUUIDBytes(taskAID), Valid: true},
		pgtype.UUID{Bytes: parseUUIDBytes(crossRunID), Valid: true},
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err == nil {
		t.Fatal("expected RerunTask to reject a source task from a different task")
	}
}

// TestRerunTaskInheritsTriggerCommentFromSourceRun locks the trigger
// provenance contract: a per-row rerun of a comment- or mention-triggered
// task must carry the original trigger_comment_id through to the new task.
// Otherwise the daemon's buildCommentPrompt path (which keys on
// TriggerCommentID) is skipped and the rerun degrades into a generic
// task run that has lost the original comment context — see SILI-2457
// review feedback.
func TestRerunTaskInheritsTriggerCommentFromSourceRun(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()

	// Create a comment to stand in as the original mention / reply trigger.
	var triggerCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (task_id, workspace_id, author_type, author_id, content, type)
		SELECT $1, $2, 'member', m.user_id, 'please retry this', 'comment'
		FROM member m WHERE m.workspace_id = $2 LIMIT 1
		RETURNING id
	`, taskID, testWorkspaceID).Scan(&triggerCommentID); err != nil {
		t.Fatalf("insert trigger comment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, triggerCommentID)
	})

	// Source task carries the trigger_comment_id — this is the row whose
	// retry button the user clicks in the execution log.
	var sourceRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority,
		                              started_at, completed_at, failure_reason,
		                              trigger_comment_id)
		VALUES ($1, $2, $3, 'failed', 0,
		        now() - interval '1 minute', now() - interval '30 seconds', 'agent_error',
		        $4)
		RETURNING id
	`, agentID, runtimeID, taskID, triggerCommentID).Scan(&sourceRunID); err != nil {
		t.Fatalf("insert source run: %v", err)
	}

	queries := db.New(testPool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	agentRunService := service.NewAgentRunService(queries, nil, hub, bus)

	agentRun, err := agentRunService.RerunTask(
		ctx,
		pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
		pgtype.UUID{Bytes: parseUUIDBytes(sourceRunID), Valid: true},
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	)
	if err != nil {
		t.Fatalf("RerunTask failed: %v", err)
	}
	if agentRun == nil {
		t.Fatal("RerunTask returned nil task")
	}
	if !agentRun.TriggerCommentID.Valid {
		t.Fatal("expected per-row rerun to inherit trigger_comment_id from source task, got NULL")
	}
	if got := util.UUIDToString(agentRun.TriggerCommentID); got != triggerCommentID {
		t.Fatalf("trigger_comment_id mismatch: got %s, want %s", got, triggerCommentID)
	}
}

// TestEnqueueRunForTaskDoesNotForceFreshSession is the negative control
// for the rerun flag: the normal enqueue path must leave the flag false so
// auto-retry / comment-triggered tasks keep resuming the prior session
// (SILI-1128 contract).
func TestEnqueueRunForTaskDoesNotForceFreshSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, _, _ := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()
	queries := db.New(testPool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	agentRunService := service.NewAgentRunService(queries, nil, hub, bus)

	task, err := queries.GetTask(ctx, pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true})
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	agentRun, err := agentRunService.EnqueueRunForTask(ctx, task)
	if err != nil {
		t.Fatalf("EnqueueTaskForTask failed: %v", err)
	}
	if agentRun.ForceFreshSession {
		t.Fatal("expected normal enqueue to leave force_fresh_session=false")
	}
}

// TestGetLastTaskSessionExcludesPoisonedOriginSession is the GH #5975 core
// regression: the SAME session_id appears on an older 'completed' row (the turn
// that first baked the oversized image into history) AND a newer poisoned
// 'failed' row (a later text-only turn that resumed it and hit the same
// rejection). A row-level filter would drop the poisoned row and fall back to
// the completed row — which points at the exact same dead session. The
// per-session-latest-state selection must invalidate the whole session.
func TestGetLastRunSessionExcludesPoisonedOriginSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()

	// Older completed row: the successful turn that consumed the oversized
	// screenshot and stored the session id.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'POISON-ORIGIN', '/tmp/origin')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert completed origin run: %v", err)
	}

	// Newer poisoned row on the SAME session: a later text-only turn resumed
	// it and hit the oversized-history-image rejection.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'POISON-ORIGIN', '/tmp/origin', 'api_invalid_request',
		        'kiro session/prompt failed: session/prompt: Internal error (code=-32603, data=Encountered an error in the response stream: messages.14.content.0.image.source.base64.data: At least one of the image dimensions exceed max allowed size: 8000 pixels)')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert poisoned resume run: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastRunSession(ctx, db.GetLastRunSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		TaskID:  pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
	})
	requireSessionExcluded(t, prior.SessionID, err)
}

// TestGetLastTaskSessionFallsBackToHealthyDistinctSession asserts that while a
// poisoned session (completed origin + newer poisoned resume, same id) is
// invalidated, a DIFFERENT healthy session on the same task is still eligible.
func TestGetLastRunSessionFallsBackToHealthyDistinctSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '3 minutes', now() - interval '3 minutes', 'POISON-ORIGIN', '/tmp/origin')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert completed origin run: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'POISON-ORIGIN', '/tmp/origin', 'api_invalid_request',
		        'session/prompt: Internal error (code=-32603, data=messages.14.content.0.image.source.base64.data: At least one of the image dimensions exceed max allowed size: 8000 pixels)')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert poisoned resume run: %v", err)
	}
	// A separate, healthy session (distinct id) — the eligible fallback.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '1 minute', now() - interval '1 minute', 'HEALTHY-DISTINCT', '/tmp/healthy')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert healthy distinct run: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastRunSession(ctx, db.GetLastRunSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		TaskID:  pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if prior.SessionID.String != "HEALTHY-DISTINCT" {
		t.Fatalf("expected fallback to HEALTHY-DISTINCT, got %q", prior.SessionID.String)
	}
}

// TestGetLastTaskSessionExcludesKiroOversizedImageByText is the GH #5975
// defense-in-depth: a row that escaped the daemon classifier (still tagged
// 'agent_error', e.g. a new-server + old-daemon deploy window) must still be
// excluded on error-text shape alone via the oversized-image ILIKE clause.
func TestGetLastRunSessionExcludesKiroOversizedImageByText(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '1 minute', now() - interval '1 minute', 'KIRO-OVERSIZED', '/tmp/kiro', 'agent_error',
		        'kiro session/prompt failed: session/prompt: Internal error (code=-32603, data=Encountered an error in the response stream: messages.14.content.0.image.source.base64.data: At least one of the image dimensions exceed max allowed size: 8000 pixels)')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert unclassified kiro oversized run: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastRunSession(ctx, db.GetLastRunSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		TaskID:  pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
	})
	requireSessionExcluded(t, prior.SessionID, err)
}

// TestGetLastTaskSessionRestoresRecoveredSession asserts the per-session-latest
// rule is symmetric: if a session that once failed poisoned later terminates
// cleanly again (a newer 'completed' row on the same id), it becomes resumable
// once more.
func TestGetLastRunSessionRestoresRecoveredSession(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '2 minutes', now() - interval '2 minutes', 'RECOVER-SESS', '/tmp/recover', 'api_invalid_request',
		        'session/prompt: Internal error (code=-32603, data=messages.0.content.0.image.source.base64.data: At least one of the image dimensions exceed max allowed size: 8000 pixels)')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert earlier poisoned run: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '1 minute', now() - interval '1 minute', 'RECOVER-SESS', '/tmp/recover')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert later completed run: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastRunSession(ctx, db.GetLastRunSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		TaskID:  pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if prior.SessionID.String != "RECOVER-SESS" {
		t.Fatalf("expected the recovered session to be resumable again, got %q", prior.SessionID.String)
	}
}

// TestGetLastTaskSessionKeepsDimensionPhraseWithoutImageMarker is the
// review-tightening negative control: the oversized-image ILIKE now requires
// BOTH the dimension phrase AND the image-content marker, matching the Kiro
// detector / classifyPoisonedError precision. An unrelated error that merely
// mentions image dimensions (without the image.source.base64.data marker) must
// NOT be filtered — otherwise a benign failure would needlessly drop a
// resumable session.
func TestGetLastRunSessionKeepsDimensionPhraseWithoutImageMarker(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runtimeID := setupRerunTestFixture(t)
	t.Cleanup(func() { cleanupRerunFixture(t, taskID) })

	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_id, work_dir, failure_reason, error)
		VALUES ($1, $2, $3, 'failed', 0, now() - interval '30 seconds', now() - interval '30 seconds', 'DIM-ONLY-RESUMABLE', '/tmp/dim', 'agent_error',
		        'tool reported: image dimensions exceed max allowed size: 8000 pixels while generating a thumbnail')
	`, agentID, runtimeID, taskID); err != nil {
		t.Fatalf("insert dimension-phrase-only run: %v", err)
	}

	queries := db.New(testPool)
	prior, err := queries.GetLastRunSession(ctx, db.GetLastRunSessionParams{
		AgentID: pgtype.UUID{Bytes: parseUUIDBytes(agentID), Valid: true},
		TaskID:  pgtype.UUID{Bytes: parseUUIDBytes(taskID), Valid: true},
	})
	if err != nil {
		t.Fatalf("GetLastTaskSession failed: %v", err)
	}
	if !prior.SessionID.Valid || prior.SessionID.String != "DIM-ONLY-RESUMABLE" {
		t.Fatalf("expected the dimension-phrase-only session to stay resumable, got %q (valid=%v)", prior.SessionID.String, prior.SessionID.Valid)
	}
}
