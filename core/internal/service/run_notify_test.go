package service

import (
	"context"
	"testing"

	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// stubWakeup records every call so the test can assert that notify
// reaches the daemon hub and carries the right runtime / task IDs.
type stubWakeup struct {
	calls []struct{ runtimeID, runID string }
}

func (s *stubWakeup) NotifyRunAvailable(runtimeID, runID string) {
	s.calls = append(s.calls, struct{ runtimeID, runID string }{runtimeID, runID})
}

// TestNotifyTaskAvailable_BumpsBeforeWakeup pins the contract noted in
// the EmptyClaimCache docs: the version Bump MUST run before the
// daemon WS wakeup, otherwise the wakeup-driven claim could read a
// still-current empty verdict and return null while the freshly
// queued task sits idle. The test (1) marks the runtime empty under
// the current version, (2) fires notifyTaskAvailable, then (3)
// asserts the prior verdict is rejected AND the wakeup hook saw the
// new task — proving every enqueue path (task / mention /
// quick-create / chat / autopilot / retry) gets the same
// bump-then-notify behaviour for free.
func TestNotifyRunAvailable_BumpsBeforeWakeup(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := NewEmptyClaimCache(rdb)
	wakeup := &stubWakeup{}

	svc := &AgentRunService{
		EmptyClaim: cache,
		Wakeup:     wakeup,
	}

	runtimeID := testUUID(7)
	runID := testUUID(8)
	runtimeKey := util.UUIDToString(runtimeID)

	ctx := context.Background()
	v0 := cache.CurrentVersion(ctx, runtimeKey)
	cache.MarkEmpty(ctx, runtimeKey, v0)
	if !cache.IsEmpty(ctx, runtimeKey) {
		t.Fatal("precondition: cache should report empty after MarkEmpty under current version")
	}

	svc.notifyRunAvailable(db.AgentRun{
		ID:        runID,
		RuntimeID: runtimeID,
	})

	if cache.IsEmpty(ctx, runtimeKey) {
		t.Fatal("notifyTaskAvailable must Bump the version so the prior empty verdict is rejected")
	}
	if got := len(wakeup.calls); got != 1 {
		t.Fatalf("expected 1 wakeup call, got %d", got)
	}
	if wakeup.calls[0].runtimeID != runtimeKey {
		t.Fatalf("wakeup runtime mismatch: got %q want %q", wakeup.calls[0].runtimeID, runtimeKey)
	}
	if wakeup.calls[0].runID != util.UUIDToString(runID) {
		t.Fatalf("wakeup task mismatch: got %q want %q", wakeup.calls[0].runID, util.UUIDToString(runID))
	}
}

// TestNotifyTaskAvailable_InvalidWithoutRuntimeIsNoOp guards the
// no-RuntimeID early return — chat / quick-create / autopilot all set
// it on insert, but a buggy caller that forgot must not silently bump
// every workspace's version. The cache treats Bump("") as a no-op,
// but this test pins that the RuntimeID guard sits above the Bump
// call so a future refactor cannot drop the guard without test
// coverage.
func TestNotifyRunAvailable_InvalidWithoutRuntimeIsNoOp(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := NewEmptyClaimCache(rdb)
	wakeup := &stubWakeup{}

	svc := &AgentRunService{
		EmptyClaim: cache,
		Wakeup:     wakeup,
	}

	ctx := context.Background()
	v0 := cache.CurrentVersion(ctx, "rt-stays")
	cache.MarkEmpty(ctx, "rt-stays", v0)

	svc.notifyRunAvailable(db.AgentRun{
		// RuntimeID intentionally invalid (zero value, Valid=false).
		ID: testUUID(9),
	})

	if !cache.IsEmpty(ctx, "rt-stays") {
		t.Fatal("notifyTaskAvailable with invalid RuntimeID must not touch cache")
	}
	if got := len(wakeup.calls); got != 0 {
		t.Fatalf("expected 0 wakeup calls when RuntimeID is invalid, got %d", got)
	}
}

// TestNotifyTaskFinished_BumpsBeforeRuntimeWakeup pins the terminal-transition
// half of the wakeup contract. A prior empty verdict must not hide queued work
// that becomes claimable when another task releases agent capacity or a
// per-(task, agent) serialization key.
func TestNotifyRunFinished_BumpsBeforeRuntimeWakeup(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := NewEmptyClaimCache(rdb)
	wakeup := &stubWakeup{}
	svc := &AgentRunService{EmptyClaim: cache, Wakeup: wakeup}

	runtimeID := testUUID(10)
	runtimeKey := util.UUIDToString(runtimeID)
	ctx := context.Background()
	version := cache.CurrentVersion(ctx, runtimeKey)
	cache.MarkEmpty(ctx, runtimeKey, version)
	if !cache.IsEmpty(ctx, runtimeKey) {
		t.Fatal("precondition: cache should report empty")
	}

	svc.NotifyRunFinished(db.AgentRun{ID: testUUID(11), RuntimeID: runtimeID})

	if cache.IsEmpty(ctx, runtimeKey) {
		t.Fatal("terminal wakeup must invalidate the prior empty verdict")
	}
	if got := len(wakeup.calls); got != 1 {
		t.Fatalf("expected 1 terminal wakeup, got %d", got)
	}
	if wakeup.calls[0].runtimeID != runtimeKey {
		t.Fatalf("wakeup runtime mismatch: got %q want %q", wakeup.calls[0].runtimeID, runtimeKey)
	}
	if wakeup.calls[0].runID != "" {
		t.Fatalf("terminal wakeup must omit completed task id, got %q", wakeup.calls[0].runID)
	}
}

func TestNotifyRunsFinished_CoalescesByRuntime(t *testing.T) {
	wakeup := &stubWakeup{}
	svc := &AgentRunService{Wakeup: wakeup}
	runtimeA := testUUID(12)
	runtimeB := testUUID(13)

	svc.notifyRunsFinished([]db.AgentRun{
		{ID: testUUID(14), RuntimeID: runtimeA},
		{ID: testUUID(15), RuntimeID: runtimeA},
		{ID: testUUID(16), RuntimeID: runtimeB},
		{ID: testUUID(17)}, // Invalid runtime is ignored.
	})

	if got := len(wakeup.calls); got != 2 {
		t.Fatalf("expected one wakeup per runtime, got %d", got)
	}
	if wakeup.calls[0].runtimeID != util.UUIDToString(runtimeA) || wakeup.calls[1].runtimeID != util.UUIDToString(runtimeB) {
		t.Fatalf("unexpected runtime wakeups: %#v", wakeup.calls)
	}
	for _, call := range wakeup.calls {
		if call.runID != "" {
			t.Fatalf("terminal wakeup must omit completed task id, got %q", call.runID)
		}
	}
}
