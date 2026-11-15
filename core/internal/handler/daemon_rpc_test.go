package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/silieco-ai/silieco/core/internal/daemonws"
)

// TestDaemonRPCHandler_TasksClaim pins the WS-first claim binding (SILI-4257):
// a runs.claim RPC, driven with the WS connection's identity, reuses the HTTP
// claim handler and claims a queued task for the daemon's runtime.
func TestDaemonRPCHandler_RunsClaim(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "WS claim rt")
	a, i := createClaimReclaimAgentAndTask(t, ctx, rt, "WS claim agent")
	runID := seedQueuedTaskRun(t, ctx, a, rt, i)

	identity := daemonws.ClientIdentity{
		DaemonID:     "ws-daemon",
		UserID:       testUserID,
		WorkspaceID:  testWorkspaceID,
		WorkspaceIDs: []string{testWorkspaceID},
		RuntimeIDs:   []string{rt},
	}
	body, _ := json.Marshal(map[string]any{
		"daemon_id":   "ws-daemon",
		"runtime_ids": []string{rt},
		"max_runs":    5,
	})

	status, respBody, err := testHandler.DaemonRPCHandler(ctx, identity, "runs.claim", body)
	if err != nil {
		t.Fatalf("DaemonRPCHandler: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, string(respBody))
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Runs) != 1 || resp.Runs[0].ID != runID {
		t.Fatalf("claimed %+v, want the queued task %s", resp.Runs, runID)
	}
	if resp.Runs[0].RuntimeID != rt {
		t.Fatalf("claimed runtime = %s, want %s", resp.Runs[0].RuntimeID, rt)
	}

	var dbStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_run WHERE id = $1`, runID).Scan(&dbStatus); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if dbStatus != "dispatched" {
		t.Fatalf("task status = %s, want dispatched", dbStatus)
	}
}

// TestDaemonRPCHandler_UnknownMethod returns 404 for an unknown method.
func TestDaemonRPCHandler_UnknownMethod(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	status, _, err := testHandler.DaemonRPCHandler(context.Background(),
		daemonws.ClientIdentity{DaemonID: "ws-daemon"}, "does.not.exist", nil)
	if status != http.StatusNotFound || err == nil {
		t.Fatalf("status=%d err=%v, want 404 + error", status, err)
	}
}
