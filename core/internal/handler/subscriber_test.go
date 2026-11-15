package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func TestSubscriberAPI(t *testing.T) {
	ctx := context.Background()

	// Helper: create an task for subscriber tests
	createTask := func(t *testing.T) string {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
			"title": "Subscriber test task",
		})
		testHandler.CreateTask(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var task TaskResponse
		json.NewDecoder(w.Body).Decode(&task)
		return task.ID
	}

	// Helper: delete an task
	deleteTask := func(t *testing.T, taskID string) {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/tasks/"+taskID, nil)
		req = withURLParam(req, "id", taskID)
		testHandler.DeleteTask(w, req)
	}

	t.Run("Subscribe", func(t *testing.T) {
		taskID := createTask(t)
		defer deleteTask(t, taskID)

		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/tasks/"+taskID+"/subscribe", nil)
		req = withURLParam(req, "id", taskID)
		testHandler.SubscribeToTask(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SubscribeToTask: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]bool
		json.NewDecoder(w.Body).Decode(&resp)
		if !resp["subscribed"] {
			t.Fatal("SubscribeToTask: expected subscribed=true")
		}

		// Verify in DB
		subscribed, err := testHandler.Queries.IsTaskSubscriber(ctx, db.IsTaskSubscriberParams{
			TaskID:   parseUUID(taskID),
			UserType: "member",
			UserID:   parseUUID(testUserID),
		})
		if err != nil {
			t.Fatalf("IsTaskSubscriber: %v", err)
		}
		if !subscribed {
			t.Fatal("expected user to be subscribed in DB")
		}
	})

	t.Run("SubscribeIdempotent", func(t *testing.T) {
		taskID := createTask(t)
		defer deleteTask(t, taskID)

		// Subscribe first time
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/tasks/"+taskID+"/subscribe", nil)
		req = withURLParam(req, "id", taskID)
		testHandler.SubscribeToTask(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SubscribeToTask (1st): expected 200, got %d: %s", w.Code, w.Body.String())
		}

		// Subscribe second time — should also succeed
		w = httptest.NewRecorder()
		req = newRequest("POST", "/api/tasks/"+taskID+"/subscribe", nil)
		req = withURLParam(req, "id", taskID)
		testHandler.SubscribeToTask(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SubscribeToTask (2nd): expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("ListSubscribers", func(t *testing.T) {
		taskID := createTask(t)
		defer deleteTask(t, taskID)

		// Subscribe first
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/tasks/"+taskID+"/subscribe", nil)
		req = withURLParam(req, "id", taskID)
		testHandler.SubscribeToTask(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SubscribeToTask: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		// List
		w = httptest.NewRecorder()
		req = newRequest("GET", "/api/tasks/"+taskID+"/subscribers", nil)
		req = withURLParam(req, "id", taskID)
		testHandler.ListTaskSubscribers(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("ListTaskSubscribers: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var subscribers []SubscriberResponse
		json.NewDecoder(w.Body).Decode(&subscribers)
		if len(subscribers) == 0 {
			t.Fatal("ListTaskSubscribers: expected at least 1 subscriber")
		}
		found := false
		for _, s := range subscribers {
			if s.UserID == testUserID && s.UserType == "member" && s.Reason == "manual" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("ListTaskSubscribers: expected to find test user subscriber, got %+v", subscribers)
		}
	})

	t.Run("Unsubscribe", func(t *testing.T) {
		taskID := createTask(t)
		defer deleteTask(t, taskID)

		// Subscribe first
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/tasks/"+taskID+"/subscribe", nil)
		req = withURLParam(req, "id", taskID)
		testHandler.SubscribeToTask(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SubscribeToTask: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		// Unsubscribe
		w = httptest.NewRecorder()
		req = newRequest("POST", "/api/tasks/"+taskID+"/unsubscribe", nil)
		req = withURLParam(req, "id", taskID)
		testHandler.UnsubscribeFromTask(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("UnsubscribeFromTask: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]bool
		json.NewDecoder(w.Body).Decode(&resp)
		if resp["subscribed"] {
			t.Fatal("UnsubscribeFromTask: expected subscribed=false")
		}

		// Verify in DB
		subscribed, err := testHandler.Queries.IsTaskSubscriber(ctx, db.IsTaskSubscriberParams{
			TaskID:   parseUUID(taskID),
			UserType: "member",
			UserID:   parseUUID(testUserID),
		})
		if err != nil {
			t.Fatalf("IsTaskSubscriber: %v", err)
		}
		if subscribed {
			t.Fatal("expected user to NOT be subscribed in DB")
		}
	})

	t.Run("SubscribeCrossWorkspaceUser", func(t *testing.T) {
		taskID := createTask(t)
		defer deleteTask(t, taskID)

		foreignUserID := "00000000-0000-0000-0000-000000000099"
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/tasks/"+taskID+"/subscribe", map[string]any{
			"user_id":   foreignUserID,
			"user_type": "member",
		})
		req = withURLParam(req, "id", taskID)
		testHandler.SubscribeToTask(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("SubscribeToTask with cross-workspace user: expected 403, got %d: %s", w.Code, w.Body.String())
		}

		subscribed, err := testHandler.Queries.IsTaskSubscriber(ctx, db.IsTaskSubscriberParams{
			TaskID:   parseUUID(taskID),
			UserType: "member",
			UserID:   parseUUID(foreignUserID),
		})
		if err != nil {
			t.Fatalf("IsTaskSubscriber: %v", err)
		}
		if subscribed {
			t.Fatal("cross-workspace user should NOT be subscribed in DB")
		}
	})

	t.Run("UnsubscribeCrossWorkspaceUser", func(t *testing.T) {
		taskID := createTask(t)
		defer deleteTask(t, taskID)

		foreignUserID := "00000000-0000-0000-0000-000000000099"
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/tasks/"+taskID+"/unsubscribe", map[string]any{
			"user_id":   foreignUserID,
			"user_type": "member",
		})
		req = withURLParam(req, "id", taskID)
		testHandler.UnsubscribeFromTask(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("UnsubscribeFromTask with cross-workspace user: expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("AgentCallerSubscribesItself", func(t *testing.T) {
		taskID := createTask(t)
		defer deleteTask(t, taskID)

		// Look up the agent created by the handler test fixture.
		var agentID string
		err := testPool.QueryRow(ctx,
			`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, "Handler Test Agent",
		).Scan(&agentID)
		if err != nil {
			t.Fatalf("failed to find test agent: %v", err)
		}

		// Subscribe with X-Agent-ID set — no body, so the handler must default
		// to subscribing the agent itself (not the member behind X-User-ID).
		// resolveActor requires X-Run-ID alongside X-Agent-ID to grant the
		// "agent" identity (defense against header forgery), so seed a task.
		agentRun := createHandlerTestRunForAgent(t, agentID)
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/tasks/"+taskID+"/subscribe", nil)
		req = withURLParam(req, "id", taskID)
		req.Header.Set("X-Agent-ID", agentID)
		req.Header.Set("X-Run-ID", agentRun)
		testHandler.SubscribeToTask(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SubscribeToTask (agent caller): expected 200, got %d: %s", w.Code, w.Body.String())
		}

		agentSubscribed, err := testHandler.Queries.IsTaskSubscriber(ctx, db.IsTaskSubscriberParams{
			TaskID:   parseUUID(taskID),
			UserType: "agent",
			UserID:   parseUUID(agentID),
		})
		if err != nil {
			t.Fatalf("IsTaskSubscriber (agent): %v", err)
		}
		if !agentSubscribed {
			t.Fatal("expected agent to be subscribed in DB when X-Agent-ID is set")
		}

		memberSubscribed, err := testHandler.Queries.IsTaskSubscriber(ctx, db.IsTaskSubscriberParams{
			TaskID:   parseUUID(taskID),
			UserType: "member",
			UserID:   parseUUID(testUserID),
		})
		if err != nil {
			t.Fatalf("IsTaskSubscriber (member): %v", err)
		}
		if memberSubscribed {
			t.Fatal("member must not be auto-subscribed when caller is an agent")
		}

		// Unsubscribe with X-Agent-ID set — same default-to-caller expectation.
		// Re-use the same task as the subscribe call; resolveActor only
		// validates that the task belongs to the agent, not which task.
		w = httptest.NewRecorder()
		req = newRequest("POST", "/api/tasks/"+taskID+"/unsubscribe", nil)
		req = withURLParam(req, "id", taskID)
		req.Header.Set("X-Agent-ID", agentID)
		req.Header.Set("X-Run-ID", agentRun)
		testHandler.UnsubscribeFromTask(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("UnsubscribeFromTask (agent caller): expected 200, got %d: %s", w.Code, w.Body.String())
		}

		agentSubscribed, err = testHandler.Queries.IsTaskSubscriber(ctx, db.IsTaskSubscriberParams{
			TaskID:   parseUUID(taskID),
			UserType: "agent",
			UserID:   parseUUID(agentID),
		})
		if err != nil {
			t.Fatalf("IsTaskSubscriber (agent, after unsubscribe): %v", err)
		}
		if agentSubscribed {
			t.Fatal("expected agent to be unsubscribed in DB when X-Agent-ID is set")
		}
	})

	t.Run("ListAfterUnsubscribe", func(t *testing.T) {
		taskID := createTask(t)
		defer deleteTask(t, taskID)

		// Subscribe
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/tasks/"+taskID+"/subscribe", nil)
		req = withURLParam(req, "id", taskID)
		testHandler.SubscribeToTask(w, req)

		// Unsubscribe
		w = httptest.NewRecorder()
		req = newRequest("POST", "/api/tasks/"+taskID+"/unsubscribe", nil)
		req = withURLParam(req, "id", taskID)
		testHandler.UnsubscribeFromTask(w, req)

		// List should be empty
		w = httptest.NewRecorder()
		req = newRequest("GET", "/api/tasks/"+taskID+"/subscribers", nil)
		req = withURLParam(req, "id", taskID)
		testHandler.ListTaskSubscribers(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("ListTaskSubscribers: expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var subscribers []SubscriberResponse
		json.NewDecoder(w.Body).Decode(&subscribers)
		if len(subscribers) != 0 {
			t.Fatalf("ListTaskSubscribers: expected 0 subscribers after unsubscribe, got %d", len(subscribers))
		}
	})
}
