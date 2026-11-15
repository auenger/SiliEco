package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestGroupChatCreateAndMentionTrigger(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "GroupChatReviewAgent", []byte("[]"))
	secondAgentID := createHandlerTestAgent(t, "GroupChatPlanningAgent", []byte("[]"))
	var invitedUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('Invited Group Member', $1) RETURNING id
	`, "group-member-"+uuid.NewString()+"@silieco.test").Scan(&invitedUserID); err != nil {
		t.Fatalf("create invited user: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, invitedUserID); err != nil {
		t.Fatalf("create invited member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, invitedUserID)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, invitedUserID)
	})

	createW := httptest.NewRecorder()
	createReq := withChatTestWorkspaceCtx(t, newRequest(http.MethodPost, "/api/chat/sessions", map[string]any{
		"kind":       "group",
		"title":      "Launch room",
		"member_ids": []string{invitedUserID},
		"agent_ids":  []string{agentID},
	}))
	testHandler.CreateChatSession(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create group chat: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var session ChatSessionResponse
	if err := json.NewDecoder(createW.Body).Decode(&session); err != nil {
		t.Fatalf("decode group session: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, session.ID) })
	if session.Kind != "group" {
		t.Fatalf("kind = %q, want group", session.Kind)
	}
	participants := make(map[string]bool, len(session.Participants))
	for _, participant := range session.Participants {
		participants[participant.Type+":"+participant.ID] = true
	}
	for _, expected := range []string{
		"member:" + testUserID,
		"member:" + invitedUserID,
		"agent:" + agentID,
	} {
		if !participants[expected] {
			t.Fatalf("created group missing participant %s: %#v", expected, session.Participants)
		}
	}

	updateW := httptest.NewRecorder()
	updateReq := withURLParam(
		withChatTestWorkspaceCtx(t, newRequest(http.MethodPatch, "/api/chat/sessions/"+session.ID+"/participants", map[string]any{
			"member_ids": []string{},
			"agent_ids":  []string{secondAgentID},
		})),
		"sessionId",
		session.ID,
	)
	testHandler.UpdateGroupChatParticipants(updateW, updateReq)
	if updateW.Code != http.StatusOK {
		t.Fatalf("add group participant: expected 200, got %d: %s", updateW.Code, updateW.Body.String())
	}
	var updated ChatSessionResponse
	if err := json.NewDecoder(updateW.Body).Decode(&updated); err != nil {
		t.Fatalf("decode participant update: %v", err)
	}
	updatedParticipants := make(map[string]bool, len(updated.Participants))
	for _, participant := range updated.Participants {
		updatedParticipants[participant.Type+":"+participant.ID] = true
	}
	if !updatedParticipants["agent:"+agentID] || !updatedParticipants["agent:"+secondAgentID] {
		t.Fatalf("participant update must be additive: %#v", updated.Participants)
	}

	send := func(content string) SendChatMessageResponse {
		t.Helper()
		w := httptest.NewRecorder()
		req := withURLParam(
			withChatTestWorkspaceCtx(t, newRequest(http.MethodPost, "/api/chat/sessions/"+session.ID+"/messages", map[string]any{
				"content": content,
			})),
			"sessionId",
			session.ID,
		)
		testHandler.SendChatMessage(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("send group message: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var response SendChatMessageResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("decode send response: %v", err)
		}
		return response
	}

	plain := send("Human-only planning note")
	if plain.RunID != "" || len(plain.RunIDs) != 0 {
		t.Fatalf("ordinary group message unexpectedly triggered tasks: %#v", plain)
	}
	var plainRunCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_run WHERE chat_session_id = $1
	`, session.ID).Scan(&plainRunCount); err != nil {
		t.Fatalf("count plain group tasks: %v", err)
	}
	if plainRunCount != 0 {
		t.Fatalf("ordinary group message created %d tasks, want 0", plainRunCount)
	}

	mentioned := send("[@GroupChatReviewAgent](mention://agent/" + agentID + ") please review the plan")
	if mentioned.RunID == "" || len(mentioned.RunIDs) != 1 || mentioned.RunIDs[0] != mentioned.RunID {
		t.Fatalf("mentioned group message task ids = %#v", mentioned)
	}
	var storedAgentID, triggerMessageID string
	if err := testPool.QueryRow(ctx, `
		SELECT agent_id::text, trigger_chat_message_id::text
		FROM agent_run WHERE id = $1
	`, mentioned.RunID).Scan(&storedAgentID, &triggerMessageID); err != nil {
		t.Fatalf("load mentioned group run: %v", err)
	}
	if storedAgentID != agentID || triggerMessageID != mentioned.MessageID {
		t.Fatalf("group task target/trigger = %s/%s, want %s/%s", storedAgentID, triggerMessageID, agentID, mentioned.MessageID)
	}
}
