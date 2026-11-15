package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestTaskRunRouteIdentityContract(t *testing.T) {
	created := authRequest(t, http.MethodPost, "/api/tasks", map[string]any{"title": "Task/Run route identity", "status": "todo"})
	defer created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create status=%d", created.StatusCode)
	}
	var task struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(created.Body).Decode(&task); err != nil || task.ID == "" {
		t.Fatalf("task id missing: %v", err)
	}
	t.Cleanup(func() { res := authRequest(t, http.MethodDelete, "/api/tasks/"+task.ID, nil); res.Body.Close() })
	for _, path := range []string{"/api/issues/" + task.ID, "/api/tasks/" + task.ID + "/messages", "/api/runs/" + task.ID + "/messages"} {
		res := authRequest(t, http.MethodGet, path, nil)
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: want 404, got %d", path, res.StatusCode)
		}
	}
	active := authRequest(t, http.MethodGet, "/api/tasks/"+task.ID+"/active-run", nil)
	defer active.Body.Close()
	var body map[string]json.RawMessage
	if err := json.NewDecoder(active.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if active.StatusCode != http.StatusOK || string(body["runs"]) != "[]" {
		t.Fatalf("unexpected run envelope: %s", body)
	}
	if _, ok := body["tasks"]; ok {
		t.Fatal("execution list leaked old tasks key")
	}
}

func TestTaskAPIRejectsOldProtocol(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, testServer.URL+"/api/tasks", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUpgradeRequired {
		t.Fatalf("status=%d", res.StatusCode)
	}
}
