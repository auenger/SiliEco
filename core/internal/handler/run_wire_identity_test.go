package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/middleware"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

// A chat execution has no Task ID. Both live events and catch-up responses must
// carry its chat identity so clients can route the stream to the open session.
func TestRunWireIdentityForTaskAndChat(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, kind := range []string{"task", "chat"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			runtimeID := createClaimReclaimRuntime(t, ctx, "wire "+kind)
			agentID, taskID := createClaimReclaimAgentAndTask(t, ctx, runtimeID, "wire "+kind)
			var taskRef, chatRef pgtype.UUID
			var chatID string
			if kind == "task" {
				taskRef = parseUUID(taskID)
			} else {
				taskID = ""
				if err := testPool.QueryRow(ctx, `INSERT INTO chat_session (workspace_id, agent_id, creator_id, title) VALUES ($1,$2,$3,'wire identity') RETURNING id`, testWorkspaceID, agentID, testUserID).Scan(&chatID); err != nil {
					t.Fatal(err)
				}
				chatRef = parseUUID(chatID)
				t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM chat_session WHERE id=$1`, chatID) })
			}
			var runID string
			if err := testPool.QueryRow(ctx, `INSERT INTO agent_run (agent_id,runtime_id,task_id,chat_session_id,status) VALUES ($1,$2,$3,$4,'running') RETURNING id`, agentID, runtimeID, taskRef, chatRef).Scan(&runID); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				testPool.Exec(ctx, `DELETE FROM agent_run_message WHERE run_id=$1`, runID)
				testPool.Exec(ctx, `DELETE FROM agent_run WHERE id=$1`, runID)
			})
			bus := events.New()
			h := *testHandler
			runs := *testHandler.AgentRunService
			runs.Bus = bus
			h.AgentRunService = &runs
			h.Bus = bus
			var progress events.Event
			var message events.Event
			bus.Subscribe(protocol.EventRunProgress, func(e events.Event) { progress = e })
			bus.Subscribe(protocol.EventAgentRunMessage, func(e events.Event) { message = e })

			w := httptest.NewRecorder()
			req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runs/"+runID+"/progress", map[string]any{"summary": "working", "step": 1, "total": 2}, testWorkspaceID, "wire-test")
			h.ReportRunProgress(w, withURLParam(req, "runId", runID))
			if w.Code != http.StatusOK {
				t.Fatalf("progress: %d %s", w.Code, w.Body.String())
			}
			p, ok := progress.Payload.(protocol.RunProgressPayload)
			if !ok || progress.WorkspaceID != testWorkspaceID || p.RunID != runID || p.TaskID != taskID || p.ChatSessionID != chatID {
				t.Fatalf("progress identity: %+v, payload %+v", progress, p)
			}

			w = httptest.NewRecorder()
			req = newDaemonTokenRequest(http.MethodPost, "/api/daemon/runs/"+runID+"/messages", map[string]any{"messages": []map[string]any{{"seq": 1, "type": "text", "content": "working"}}}, testWorkspaceID, "wire-test")
			h.ReportAgentRunMessages(w, withURLParam(req, "runId", runID))
			if w.Code != http.StatusOK {
				t.Fatalf("messages: %d %s", w.Code, w.Body.String())
			}
			m, ok := message.Payload.(protocol.AgentRunMessagePayload)
			if !ok || message.WorkspaceID != testWorkspaceID || m.RunID != runID || m.TaskID != taskID || m.ChatSessionID != chatID {
				t.Fatalf("message identity: %+v, payload %+v", message, m)
			}
			w = httptest.NewRecorder()
			req = withURLParam(newRequest(http.MethodGet, "/api/runs/"+runID+"/messages", nil), "runId", runID)
			req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
			h.ListAgentRunMessagesByUser(w, req)
			var catchup []protocol.AgentRunMessagePayload
			if err := json.Unmarshal(w.Body.Bytes(), &catchup); err != nil || w.Code != http.StatusOK || len(catchup) != 1 {
				t.Fatalf("catch-up: %d %s (%v)", w.Code, w.Body.String(), err)
			}
			if catchup[0].RunID != runID || catchup[0].TaskID != taskID || catchup[0].ChatSessionID != chatID {
				t.Fatalf("catch-up identity: %+v", catchup[0])
			}
		})
	}
}
